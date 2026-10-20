package python

import (
	"fmt"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// The first pass (declare) builds the file's scopes, records every name
// binding in the scope Python puts it in, and states the declarations. The
// second pass (refs.go) reads the references against those scopes.

type scopeKind uint8

const (
	scopeModule scopeKind = iota
	scopeClass
	scopeFunction // a def or a lambda
	scopeComprehension
	scopeAnnotation // the type parameters of a generic def or class (PEP 695)
)

// bindKind classifies one binding of a name.
type bindKind uint8

const (
	bindDecl   bindKind = iota // a def this file states as a symbol
	bindClass                  // a class this file states as a symbol
	bindVar                    // an assignment target stated as a symbol (a module variable, a class attribute)
	bindImport                 // an import
	bindParam                  // a parameter or a type parameter
	bindOther                  // any binding no declaration states
)

// names counts the bindings of one name in one scope (decl counts the defs
// and the classes, classes the classes alone).
type names struct {
	decl, classes, vars, imports, params, other int
	first, last                                 uint32 // start bytes of the first and the last binding
}

func (b *names) symbols() int { return b.decl + b.vars }

type scope struct {
	kind   scopeKind
	parent *scope
	// qual is the qualified name of the declaration whose body this is: the
	// container of the references in it ("" at module level; a lambda's or
	// a comprehension's is the enclosing one's).
	qual      string
	binds     map[string]*names
	globals   map[string]bool
	nonlocals map[string]bool
	// lambda: a lambda's scope (a function scope that declares nothing).
	lambda bool

	// A class body: the class's simple name, and its one plain base (super()).
	className string
	plainBase string
	nBases    int

	// A function defined directly in a class body: its first parameter and
	// the class's scope; selfName is that parameter once it is proven the
	// instance (self) or the class (cls) — no unknown decorator, not a
	// staticmethod (applyDecorators).
	firstParam string
	inClass    *scope
	static     bool // a staticmethod: its first parameter is no instance
	selfName   string
	selfClass  *scope
	// anno is the annotation scope of a generic def or class (nil if none).
	anno *scope
}

func newScope(kind scopeKind, parent *scope, qual string) *scope {
	return &scope{kind: kind, parent: parent, qual: qual, binds: map[string]*names{}}
}

func (s *scope) bind(name string, k bindKind, at uint32) {
	if name == "" {
		return
	}
	b := s.binds[name]
	if b == nil {
		b = &names{first: at}
		s.binds[name] = b
	}
	if at < b.first {
		b.first = at
	}
	switch k {
	case bindDecl:
		b.decl++
	case bindClass:
		b.decl++
		b.classes++
	case bindVar:
		b.vars++
	case bindImport:
		b.imports++
	case bindParam:
		b.params++
	default:
		b.other++
	}
	if at >= b.last {
		b.last = at
	}
}

// nodeKey identifies a node of the tree independently of the Node values
// the parser hands out.
type nodeKey struct {
	start, end uint32
	typ        string
}

// importInfo is one import binding of the module or of a function
// (modules.go).
type importInfo struct {
	local    string // the name bound ("a.b" for the submodules of import a.b)
	module   string // the module as written: "a.b", ".m", ".."
	imported string // from-import: the imported name; "" for import M
	scope    string // the function the import is in ("" at module level)
	at       *ts.Node
}

// binding is one recorded binding before global / nonlocal declarations
// route it to its scope.
type binding struct {
	sc   *scope
	name string
	kind bindKind
	at   uint32
}

// attrAssign is a `C.x = …` (or del C.x) target: x of whatever C names.
type attrAssign struct {
	sc        *scope
	obj, attr string
	at        *ts.Node
}

// selfAttr is a `self.x = …` target of a method, a class field once its
// self is proven.
type selfAttr struct {
	fn   *scope
	name string
	at   *ts.Node
}

type extractor struct {
	lang *ts.Language
	src  []byte
	file source.FileID

	mod     *scope
	scopeOf map[nodeKey]*scope
	pending []binding

	symbols  []language.SymbolDraft
	refs     []language.ReferenceDraft
	imports  []language.ImportDraft
	bindings []language.BindingDraft
	exports  []language.ExportDraft

	moduleImports []importInfo
	submodules    []string // X of a module-level from .X import … (relative: proven)
	// maybeSubmodules: X of a module-level absolute import that may name
	// this package's submodule X (ownSubmodule).
	maybeSubmodules []string
	lastStar        uint32 // start byte of the last module-level star import
	// lazyAnnotations: from __future__ import annotations (PEP 563).
	lazyAnnotations bool
	hasStar         bool
	selfAttrs       []selfAttr
	defs            []definition
	attrAssigns     []attrAssign
	dropped         map[int]bool    // symbols a later pass takes back (typing overloads)
	fields          map[string]bool // class qual + "\x00" + field name already stated

	// symbolNames counts the file's symbols per name; children holds the
	// names declared directly in a declaration (parent qual + "\x00" + name);
	// parentsOf the parents ("" at module level) of the declarations of a
	// name that are no member — those the resolver's same-file stage sees.
	symbolNames map[string]int
	children    map[string]bool
	parentsOf   map[string][]string
}

func newExtractor(lang *ts.Language, src []byte, file source.FileID) *extractor {
	return &extractor{
		lang: lang, src: src, file: file,
		scopeOf:     map[nodeKey]*scope{},
		fields:      map[string]bool{},
		dropped:     map[int]bool{},
		symbolNames: map[string]int{},
		children:    map[string]bool{},
		parentsOf:   map[string][]string{},
	}
}

func (e *extractor) typ(n *ts.Node) string { return n.Type(e.lang) }
func (e *extractor) text(n *ts.Node) string {
	if n == nil {
		return ""
	}
	return n.Text(e.src)
}
func (e *extractor) field(n *ts.Node, name string) *ts.Node { return n.ChildByFieldName(name, e.lang) }
func (e *extractor) key(n *ts.Node) nodeKey {
	return nodeKey{n.StartByte(), n.EndByte(), e.typ(n)}
}

// declare runs the first pass over the module.
func (e *extractor) declare(root *ts.Node) {
	e.mod = newScope(scopeModule, nil, "")
	e.decl(root, e.mod)
	// Route the bindings of names a scope declares global or nonlocal: a
	// global one to the module; a nonlocal one, once every other binding is
	// in place, to the nearest enclosing function that binds the name (a
	// function declaring it nonlocal too passes it on).
	var nonlocal []binding
	for _, b := range e.pending {
		sc := b.sc
		switch {
		case sc.globals[b.name] && sc != e.mod:
			e.mod.bind(b.name, bindOther, b.at)
			continue
		case sc.nonlocals[b.name]:
			nonlocal = append(nonlocal, b)
			continue
		}
		sc.bind(b.name, b.kind, b.at)
	}
	for _, b := range nonlocal {
		for f := b.sc.parent; f != nil && f.kind != scopeModule; f = f.parent {
			if f.kind == scopeFunction && !f.nonlocals[b.name] && f.binds[b.name] != nil {
				f.bind(b.name, bindOther, b.at)
				break
			}
		}
	}
	e.applyDecorators()
	e.instanceAttributes()
	e.memberCaps()
	if len(e.dropped) > 0 {
		kept := e.symbols[:0]
		for i, s := range e.symbols {
			if !e.dropped[i] {
				kept = append(kept, s)
			}
		}
		e.symbols = kept
	}
	for _, s := range e.symbols {
		e.symbolNames[s.Name]++
		if s.Parent != "" {
			e.children[s.Parent+"\x00"+s.Name] = true
		}
		if s.Receiver == "" {
			e.parentsOf[s.Name] = append(e.parentsOf[s.Name], s.Parent)
		}
	}
}

// lookup returns the scope whose binding of name a reference at s denotes,
// by Python's rules: the scope itself, then the enclosing ones, class bodies
// skipped (a class body is no scope of the functions in it) — but for the
// class an annotation scope (PEP 695) of its body is in, whose names that
// scope sees; global and nonlocal declarations redirect. nil means nothing
// in the file binds it: a builtin, a name a star import brings, or an
// undefined name.
func (e *extractor) lookup(s *scope, name string) *scope {
	classVisible := true
	for cur := s; cur != nil; cur = cur.parent {
		if cur.kind == scopeClass && !classVisible {
			continue
		}
		classVisible = classVisible && cur.kind == scopeAnnotation
		if cur.globals[name] && cur != e.mod {
			if e.mod.binds[name] != nil {
				return e.mod
			}
			return nil
		}
		if cur.nonlocals[name] {
			for f := cur.parent; f != nil && f.kind != scopeModule; f = f.parent {
				if f.kind == scopeFunction && !f.nonlocals[name] && f.binds[name] != nil {
					return f
				}
			}
			return nil
		}
		if cur.binds[name] != nil {
			return cur
		}
	}
	return nil
}

// walrusScope is the scope an assignment expression binds in: the nearest
// enclosing scope that is no comprehension.
func walrusScope(s *scope) *scope {
	for s.kind == scopeComprehension && s.parent != nil {
		s = s.parent
	}
	return s
}

func (e *extractor) bindName(sc *scope, name string, k bindKind, at *ts.Node) {
	if name != "" {
		e.pending = append(e.pending, binding{sc, name, k, at.StartByte()})
	}
}

func (e *extractor) decl(n *ts.Node, sc *scope) {
	switch e.typ(n) {
	case "decorated_definition":
		var decorators []*ts.Node
		for i := range n.ChildCount() {
			if c := n.Child(i); e.typ(c) == "decorator" {
				decorators = append(decorators, c)
				e.declChildren(c, sc)
			}
		}
		if def := e.field(n, "definition"); def != nil {
			e.definition(def, sc, decorators)
		}
		return
	case "function_definition", "class_definition":
		e.definition(n, sc, nil)
		return
	case "lambda":
		e.lambda(n, sc)
		return
	case "list_comprehension", "set_comprehension", "dictionary_comprehension", "generator_expression":
		e.comprehension(n, sc)
		return
	case "assignment":
		kind := bindOther
		if sc.kind == scopeModule || sc.kind == scopeClass {
			kind = bindVar
		}
		if left := e.field(n, "left"); left != nil {
			e.targets(left, sc, kind, n)
		}
		e.declChildrenExcept(n, sc, "left")
		return
	case "augmented_assignment":
		if left := e.field(n, "left"); left != nil {
			e.targets(left, sc, bindOther, n)
		}
		e.declChildrenExcept(n, sc, "left")
		return
	case "named_expression":
		if name := e.field(n, "name"); name != nil {
			e.bindName(walrusScope(sc), e.text(name), bindOther, name)
		}
		e.declChildrenExcept(n, sc, "name")
		return
	case "for_statement", "for_in_clause":
		if left := e.field(n, "left"); left != nil {
			e.targets(left, sc, bindOther, n)
		}
		e.declChildrenExcept(n, sc, "left")
		return
	case "as_pattern":
		// with … as x, except E as x, case … as x
		for i := range n.ChildCount() {
			c := n.Child(i)
			if n.FieldNameForChild(i, e.lang) == "alias" {
				if e.typ(c) == "as_pattern_target" {
					for j := range c.NamedChildCount() {
						e.targets(c.NamedChild(j), sc, bindOther, n)
					}
				} else {
					e.targets(c, sc, bindOther, n)
				}
				continue
			}
			e.decl(c, sc)
		}
		return
	case "delete_statement":
		for i := range n.NamedChildCount() {
			e.targets(n.NamedChild(i), sc, bindOther, n)
		}
		return
	case "global_statement", "nonlocal_statement":
		m := &sc.globals
		if e.typ(n) == "nonlocal_statement" {
			m = &sc.nonlocals
		}
		if *m == nil {
			*m = map[string]bool{}
		}
		for i := range n.NamedChildCount() {
			if c := n.NamedChild(i); e.typ(c) == "identifier" {
				(*m)[e.text(c)] = true
			}
		}
		return
	case "import_statement", "import_from_statement", "future_import_statement":
		e.importStatement(n, sc)
		return
	case "case_clause":
		for i := range n.ChildCount() {
			c := n.Child(i)
			if e.typ(c) == "case_pattern" {
				e.captures(c, sc)
				continue
			}
			e.decl(c, sc)
		}
		return
	case "type_alias_statement":
		if left := e.field(n, "left"); left != nil {
			e.targets(left, sc, bindOther, n)
		}
		e.declChildrenExcept(n, sc, "left")
		return
	}
	e.declChildren(n, sc)
}

func (e *extractor) declChildren(n *ts.Node, sc *scope) {
	for i := range n.ChildCount() {
		e.decl(n.Child(i), sc)
	}
}

func (e *extractor) declChildrenExcept(n *ts.Node, sc *scope, skip string) {
	for i := range n.ChildCount() {
		if n.FieldNameForChild(i, e.lang) == skip {
			continue
		}
		e.decl(n.Child(i), sc)
	}
}

// targets binds the names an assignment target binds. An attribute or a
// subscript binds no name (its parts are expressions); a `self.x` target of a
// method is recorded as a possible instance attribute.
func (e *extractor) targets(n *ts.Node, sc *scope, k bindKind, stmt *ts.Node) {
	switch e.typ(n) {
	case "identifier":
		e.bindName(sc, e.text(n), k, n)
		if k == bindVar {
			e.variable(n, sc, stmt)
		}
	case "pattern_list", "tuple_pattern", "list_pattern", "tuple", "list", "parenthesized_expression",
		"list_splat_pattern", "list_splat", "as_pattern_target":
		for i := range n.NamedChildCount() {
			e.targets(n.NamedChild(i), sc, k, stmt)
		}
	case "attribute":
		obj, attr := e.field(n, "object"), e.field(n, "attribute")
		if obj != nil && attr != nil && e.typ(obj) == "identifier" {
			if sc.firstParam != "" && e.text(obj) == sc.firstParam && e.typ(stmt) == "assignment" {
				e.selfAttrs = append(e.selfAttrs, selfAttr{sc, e.text(attr), stmt})
			}
			e.attrAssigns = append(e.attrAssigns, attrAssign{sc, e.text(obj), e.text(attr), stmt})
		}
		e.decl(n, sc)
	case "type":
		for i := range n.NamedChildCount() {
			e.targets(n.NamedChild(i), sc, k, stmt)
		}
	default:
		e.decl(n, sc)
	}
}

// captures binds the capture patterns of a case pattern; the rest of the
// pattern (class names, value patterns, guards' expressions) is read as
// expressions.
func (e *extractor) captures(n *ts.Node, sc *scope) {
	switch e.typ(n) {
	case "dotted_name":
		p := n.Parent()
		if n.NamedChildCount() == 1 && p != nil && (e.typ(p) == "case_pattern" || e.typ(p) == "keyword_pattern") {
			if name := e.text(n.NamedChild(0)); name != "_" {
				e.bindName(sc, name, bindOther, n)
			}
		}
		return
	case "splat_pattern":
		for i := range n.NamedChildCount() {
			if c := n.NamedChild(i); e.typ(c) == "identifier" && e.text(c) != "_" {
				e.bindName(sc, e.text(c), bindOther, c)
			}
		}
		return
	case "as_pattern":
		for i := range n.ChildCount() {
			c := n.Child(i)
			if n.FieldNameForChild(i, e.lang) == "alias" || (e.typ(c) == "identifier" && i == n.ChildCount()-1) {
				if e.typ(c) == "identifier" {
					e.bindName(sc, e.text(c), bindOther, c)
				}
				continue
			}
			e.captures(c, sc)
		}
		return
	case "keyword_pattern":
		// name=pattern: the keyword is an attribute name, no binding.
		for i := range n.NamedChildCount() {
			if i == 0 {
				continue
			}
			e.captures(n.NamedChild(i), sc)
		}
		return
	}
	for i := range n.ChildCount() {
		e.captures(n.Child(i), sc)
	}
}

// definition states a def or class, binds its name and declares its body.
func (e *extractor) definition(n *ts.Node, sc *scope, decorators []*ts.Node) {
	name := e.text(e.field(n, "name"))
	isClass := e.typ(n) == "class_definition"

	outer := sc
	var anno *scope
	if tp := e.field(n, "type_parameters"); tp != nil {
		anno = newScope(scopeAnnotation, sc, sc.qual)
		for i := range tp.NamedChildCount() {
			e.typeParameter(tp.NamedChild(i), anno)
		}
		outer = anno
	}

	sym := -1
	qual := qualify(sc, name)
	if name != "" {
		k := bindDecl
		if isClass {
			k = bindClass
		}
		// The name is bound once the def's header (its decorators,
		// defaults, annotations, bases) is evaluated: at its body for a
		// def, after the whole statement for a class.
		at := n.EndByte()
		if b := e.field(n, "body"); b != nil && !isClass {
			at = b.StartByte()
		}
		e.pending = append(e.pending, binding{sc, name, k, at})
		sym = len(e.symbols)
		e.declaration(n, sc, name, qual, isClass)
	}

	kind := scopeFunction
	if isClass {
		kind = scopeClass
	}
	body := newScope(kind, outer, qual)
	body.anno = anno
	e.scopeOf[e.key(n)] = body
	e.defs = append(e.defs, definition{sym: sym, name: name, isClass: isClass, body: body, sc: sc, decorators: decorators})

	if isClass {
		body.className = name
		if sup := e.field(n, "superclasses"); sup != nil {
			for i := range sup.NamedChildCount() {
				c := sup.NamedChild(i)
				if e.typ(c) == "keyword_argument" {
					continue
				}
				body.nBases++
				if e.typ(c) == "identifier" {
					body.plainBase = e.text(c)
				}
			}
			e.declChildren(sup, outer)
		}
		if body.nBases != 1 {
			body.plainBase = ""
		}
	} else {
		params := e.field(n, "parameters")
		if params != nil {
			first := true
			for i := range params.NamedChildCount() {
				p := params.NamedChild(i)
				pname, splat := e.paramName(p)
				if pname != "" {
					e.bindName(body, pname, bindParam, p)
				}
				if first && (pname != "" || splat) {
					first = false
					if !splat && sc.kind == scopeClass {
						body.firstParam = pname
						body.inClass = sc
					}
				}
				// Defaults are evaluated where the def is, annotations in
				// its annotation scope (outer), both outside the body.
				e.declParamParts(p, sc, outer)
			}
		}
		if rt := e.field(n, "return_type"); rt != nil {
			e.decl(rt, outer)
		}
	}
	if b := e.field(n, "body"); b != nil {
		e.decl(b, body)
	}
}

// typeParameter binds the name of one PEP 695 type parameter: T, the T of
// T: bound, the Ts of *Ts and the P of **P — the parameter's first name.
func (e *extractor) typeParameter(n *ts.Node, anno *scope) {
	for e.typ(n) != "identifier" {
		if n.NamedChildCount() == 0 {
			return
		}
		n = n.NamedChild(0)
	}
	e.bindName(anno, e.text(n), bindParam, n)
}

// paramName is the name a parameter binds, and whether it is *args or
// **kwargs (or a bare * / /).
func (e *extractor) paramName(p *ts.Node) (string, bool) {
	switch e.typ(p) {
	case "identifier":
		return e.text(p), false
	case "default_parameter", "typed_default_parameter":
		if nm := e.field(p, "name"); nm != nil {
			return e.paramName(nm)
		}
	case "typed_parameter":
		for i := range p.NamedChildCount() {
			c := p.NamedChild(i)
			if e.typ(c) == "type" {
				continue
			}
			return e.paramName(c)
		}
	case "list_splat_pattern", "dictionary_splat_pattern":
		for i := range p.NamedChildCount() {
			if c := p.NamedChild(i); e.typ(c) == "identifier" {
				return e.text(c), true
			}
		}
		return "", true
	case "keyword_separator", "positional_separator":
		return "", true
	}
	return "", false
}

// declParamParts declares the default values (in values) and annotations
// (in types) of a parameter — lambdas or assignment expressions in them — in
// the scopes they are evaluated in.
func (e *extractor) declParamParts(p *ts.Node, values, types *scope) {
	for i := range p.ChildCount() {
		f := p.FieldNameForChild(i, e.lang)
		c := p.Child(i)
		switch {
		case f == "value":
			e.decl(c, values)
		case f == "type" || e.typ(c) == "type":
			e.decl(c, types)
		}
	}
}

func (e *extractor) lambda(n *ts.Node, sc *scope) {
	body := newScope(scopeFunction, sc, sc.qual)
	body.lambda = true
	e.scopeOf[e.key(n)] = body
	if params := e.field(n, "parameters"); params != nil {
		for i := range params.NamedChildCount() {
			p := params.NamedChild(i)
			if pname, _ := e.paramName(p); pname != "" {
				e.bindName(body, pname, bindParam, p)
			}
			e.declParamParts(p, sc, sc)
		}
	}
	if b := e.field(n, "body"); b != nil {
		e.decl(b, body)
	}
}

// comprehension declares a comprehension's scope: its iteration targets are
// its own; its first iterable is evaluated in the enclosing scope.
func (e *extractor) comprehension(n *ts.Node, sc *scope) {
	comp := newScope(scopeComprehension, sc, sc.qual)
	e.scopeOf[e.key(n)] = comp
	first := true
	for i := range n.ChildCount() {
		c := n.Child(i)
		if e.typ(c) == "for_in_clause" {
			if left := e.field(c, "left"); left != nil {
				e.targets(left, comp, bindOther, c)
			}
			for j := range c.ChildCount() {
				if c.FieldNameForChild(j, e.lang) == "left" {
					continue
				}
				if first && c.FieldNameForChild(j, e.lang) == "right" {
					e.decl(c.Child(j), sc)
					continue
				}
				e.decl(c.Child(j), comp)
			}
			first = false
			continue
		}
		e.decl(c, comp)
	}
}

// qualify is the qualified name of a declaration named name in sc.
func qualify(sc *scope, name string) string {
	if sc.qual == "" {
		return name
	}
	return sc.qual + "." + name
}

// declaration states a def or class symbol.
func (e *extractor) declaration(n *ts.Node, sc *scope, name, qual string, isClass bool) {
	d := language.SymbolDraft{
		Name:      name,
		Qualified: qual,
		Kind:      symbol.KindFunction,
		Location:  nodeLocation(n, e.file),
		StartByte: n.StartByte(),
		EndByte:   n.EndByte(),
		Exported:  !isPrivate(name),
		Parent:    sc.qual,
	}
	if isClass {
		d.Kind = symbol.KindClass
	}
	if sc.kind == scopeClass {
		d.Receiver = sc.className
		d.Visibility = memberVisibility(name)
		if !isClass {
			d.Kind = symbol.KindMethod
		}
	}
	e.symbols = append(e.symbols, d)
}

// variable states a plain or annotated assignment of a name at module level
// (a variable) or in a class body (a class attribute). Each assignment
// statement is a declaration of its own: which one the name holds is decided
// at run time (if / else branches), so a name assigned twice has two
// declarations, as a function defined twice has.
func (e *extractor) variable(id *ts.Node, sc *scope, stmt *ts.Node) {
	name := e.text(id)
	if name == "" {
		return
	}
	k := sc.qual + "\x00" + name
	if at := fmt.Sprintf("%s\x00%d", k, stmt.StartByte()); e.fields[at] {
		return // a, a = …: one statement, one declaration
	} else {
		e.fields[at] = true
	}
	e.fields[k] = true
	d := language.SymbolDraft{
		Name:      name,
		Qualified: qualify(sc, name),
		Kind:      symbol.KindVariable,
		Location:  nodeLocation(stmt, e.file),
		StartByte: stmt.StartByte(),
		EndByte:   stmt.EndByte(),
		Exported:  !isPrivate(name),
		Parent:    sc.qual,
	}
	if sc.kind == scopeClass {
		d.Kind = symbol.KindField
		d.Receiver = sc.className
		d.Visibility = memberVisibility(name)
	}
	e.symbols = append(e.symbols, d)
}

// instanceAttributes states the `self.x = …` targets of methods whose self
// is proven (the first parameter, never rebound in the method) as fields of
// the class, once per name; a class attribute of the name is already one. A
// method of the name stays beside the field: the instance attribute hides
// it, so a member lookup finds both (a Candidate).
//
// A method whose self is not proven (an unknown decorator, a name the class
// body rebinds) states its first parameter's attributes all the same, capped
// at Candidate: if the parameter is the instance, they hide a method of the
// name (MembersShareNames), and nothing is proven about them otherwise.
func (e *extractor) instanceAttributes() {
	for _, a := range e.selfAttrs {
		fn := a.fn
		b := fn.binds[fn.firstParam]
		if b == nil || b.params != 1 || b.symbols()+b.imports+b.other > 0 || fn.inClass == nil || fn.static {
			continue
		}
		cls := fn.inClass
		limit := ""
		if fn.selfName == "" {
			limit = capCandidate
		}
		k := cls.qual + "\x00" + a.name
		if e.fields[k] {
			continue
		}
		e.fields[k] = true
		e.symbols = append(e.symbols, language.SymbolDraft{
			Name:       a.name,
			Qualified:  cls.qual + "." + a.name,
			Kind:       symbol.KindField,
			Location:   nodeLocation(a.at, e.file),
			StartByte:  a.at.StartByte(),
			EndByte:    a.at.EndByte(),
			Exported:   !isPrivate(a.name),
			Parent:     cls.qual,
			Receiver:   cls.className,
			Visibility: memberVisibility(a.name),
			// An instance's own attribute: no class attribute, and it hides
			// a method of the name its subclasses declare.
			Placement:     symbol.PlacementInstance,
			ConfidenceCap: limit,
		})
	}
}

// isPrivate reports a name Python's conventions keep private: one leading
// underscore, a dunder name excepted.
func isPrivate(name string) bool {
	return strings.HasPrefix(name, "_") && !(strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__") && len(name) > 4)
}

// memberVisibility is "private" for a class member Python mangles
// (__x, not __x__): `self.__x` in another class names another attribute, so
// it is not inherited.
func memberVisibility(name string) string {
	if strings.HasPrefix(name, "__") && !strings.HasSuffix(name, "__") {
		return "private"
	}
	return ""
}

func nodeLocation(node *ts.Node, file source.FileID) source.Location {
	return source.Location{
		File: file,
		Range: source.Range{
			Start: source.Position{
				Line:   node.StartPoint().Row + 1,
				Column: node.StartPoint().Column + 1,
			},
			End: source.Position{
				Line:   node.EndPoint().Row + 1,
				Column: node.EndPoint().Column + 1,
			},
		},
	}
}
