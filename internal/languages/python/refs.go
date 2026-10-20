package python

import (
	"math"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
)

// The second pass reads the references against the scopes of the first.
// What a reference's name denotes is the resolver's to decide; the provider
// states what Python's scoping proves about it (capFor) and the receiver
// types it can prove (self, cls, super()).

const capCandidate = "candidate"

// reference runs the second pass over the module.
func (e *extractor) reference(root *ts.Node) {
	e.ref(root, e.mod, "")
}

func (e *extractor) ref(n *ts.Node, sc *scope, container string) {
	switch e.typ(n) {
	case "decorated_definition":
		def := e.field(n, "definition")
		if def == nil {
			e.refChildren(n, sc, container)
			return
		}
		qual := qualify(sc, e.text(e.field(def, "name")))
		for i := range n.ChildCount() {
			if c := n.Child(i); e.typ(c) == "decorator" {
				e.decorator(c, sc, qual)
			}
		}
		e.ref(def, sc, container)
		return
	case "function_definition":
		e.function(n, sc)
		return
	case "class_definition":
		e.class(n, sc)
		return
	case "lambda":
		body := e.scopeOf[e.key(n)]
		if body == nil {
			return
		}
		if params := e.field(n, "parameters"); params != nil {
			e.ref(params, sc, container)
		}
		if b := e.field(n, "body"); b != nil {
			e.ref(b, body, container)
		}
		return
	case "list_comprehension", "set_comprehension", "dictionary_comprehension", "generator_expression":
		comp := e.scopeOf[e.key(n)]
		if comp == nil {
			return
		}
		first := true
		for i := range n.ChildCount() {
			c := n.Child(i)
			if e.typ(c) == "for_in_clause" {
				for j := range c.ChildCount() {
					f := c.FieldNameForChild(j, e.lang)
					if f == "left" {
						continue
					}
					if first && f == "right" {
						e.ref(c.Child(j), sc, container)
						continue
					}
					e.ref(c.Child(j), comp, container)
				}
				first = false
				continue
			}
			e.ref(c, comp, container)
		}
		return
	case "call":
		e.call(n, sc, container)
		return
	case "type":
		e.typeRefs(n, sc, container)
		return
	case "type_alias_statement":
		if v := e.field(n, "right"); v != nil {
			e.typeRefs(v, sc, container)
		}
		return
	case "import_statement", "import_from_statement", "future_import_statement",
		"global_statement", "nonlocal_statement":
		return
	}
	for i := range n.ChildCount() {
		e.ref(n.Child(i), sc, container)
	}
}

// function reads a def: its parameters' defaults and annotations and its
// return annotation in the scope it is defined in (or its type parameters'
// annotation scope), its body in its own scope. The def is the container of
// all of them.
func (e *extractor) function(n *ts.Node, sc *scope) {
	body := e.scopeOf[e.key(n)]
	if body == nil {
		return
	}
	outer := body.parent
	for i := range n.ChildCount() {
		c := n.Child(i)
		switch n.FieldNameForChild(i, e.lang) {
		case "name":
		case "body":
			e.ref(c, body, body.qual)
		case "parameters":
			// A default is evaluated where the def is; an annotation in
			// the def's annotation scope (outer) when it has one.
			for j := range c.NamedChildCount() {
				p := c.NamedChild(j)
				for k := range p.ChildCount() {
					if p.FieldNameForChild(k, e.lang) == "value" {
						e.ref(p.Child(k), sc, body.qual)
					} else {
						e.ref(p.Child(k), outer, body.qual)
					}
				}
			}
		default:
			e.ref(c, outer, body.qual)
		}
	}
}

// class reads a class: its bases (inheritance references) and keyword
// arguments in the scope it is defined in, its body in its own scope.
func (e *extractor) class(n *ts.Node, sc *scope) {
	body := e.scopeOf[e.key(n)]
	if body == nil {
		return
	}
	outer := body.parent
	for i := range n.ChildCount() {
		c := n.Child(i)
		switch n.FieldNameForChild(i, e.lang) {
		case "name":
		case "body":
			e.ref(c, body, body.qual)
		case "superclasses":
			for j := range c.NamedChildCount() {
				e.base(c.NamedChild(j), outer, body.qual)
			}
		default:
			e.ref(c, outer, body.qual)
		}
	}
}

// base states one base class of class qual. A base that is no name — a
// call, a starred expression — is still a base: it is stated as an
// unidentified one (Dynamic), so that no member lookup passes it by.
func (e *extractor) base(n *ts.Node, sc *scope, qual string) {
	switch e.typ(n) {
	case "keyword_argument":
		e.ref(n, sc, qual)
		return
	case "subscript":
		// Base[T]: the base is Base; T is a type argument.
		if v := e.field(n, "value"); v != nil && e.dotted(v) != "" {
			e.nameRef(v, reference.KindInheritance, sc, qual)
			for i := range n.ChildCount() {
				if n.FieldNameForChild(i, e.lang) != "value" {
					e.typeRefs(n.Child(i), sc, qual)
				}
			}
			return
		}
	default:
		if e.dotted(n) != "" {
			e.nameRef(n, reference.KindInheritance, sc, qual)
			return
		}
	}
	e.refs = append(e.refs, language.ReferenceDraft{
		Name:          e.text(n),
		Kind:          string(reference.KindInheritance),
		Container:     qual,
		Location:      nodeLocation(n, e.file),
		ConfidenceCap: capCandidate,
		Dynamic:       true,
	})
	e.ref(n, sc, qual)
}

// decorator states a decorator's application as a call of the decorator,
// evaluated in the scope the decorated definition is in; the decorated
// declaration is its container.
func (e *extractor) decorator(d *ts.Node, sc *scope, qual string) {
	if d.NamedChildCount() == 0 {
		return
	}
	expr := d.NamedChild(0)
	if e.typ(expr) == "call" {
		e.call(expr, sc, qual)
		return
	}
	if e.dotted(expr) != "" {
		e.nameRef(expr, reference.KindCall, sc, qual)
		return
	}
	e.ref(expr, sc, qual)
}

// call states a call: of a name (f(), a.b.f()), of a member of another
// expression (f().g(), x[0].m(), its receiver kept as written), or of a
// computed callee (getattr(o, n)(), o[k]()), which is Dynamic.
func (e *extractor) call(n *ts.Node, sc *scope, container string) {
	fn := e.field(n, "function")
	if fn == nil {
		e.refChildren(n, sc, container)
		return
	}
	switch e.typ(fn) {
	case "identifier", "attribute":
		if !e.superCall(fn, sc, container) {
			e.nameRef(fn, reference.KindCall, sc, container)
		}
		if e.typ(fn) == "attribute" && e.dotted(fn) == "" {
			if obj := e.field(fn, "object"); obj != nil {
				e.ref(obj, sc, container)
			}
		}
	case "subscript":
		e.dynamic(fn, container)
		e.ref(fn, sc, container)
	case "call":
		if inner := e.field(fn, "function"); inner != nil && e.typ(inner) == "identifier" && e.text(inner) == "getattr" && e.lookup(sc, "getattr") == nil {
			e.dynamic(fn, container)
		}
		e.ref(fn, sc, container)
	default:
		e.ref(fn, sc, container)
	}
	if args := e.field(n, "arguments"); args != nil {
		e.ref(args, sc, container)
	}
}

func (e *extractor) refChildren(n *ts.Node, sc *scope, container string) {
	for i := range n.ChildCount() {
		e.ref(n.Child(i), sc, container)
	}
}

// dynamic states a call whose callee is computed at run time.
func (e *extractor) dynamic(callee *ts.Node, container string) {
	e.refs = append(e.refs, language.ReferenceDraft{
		Name:      e.text(callee),
		Kind:      string(reference.KindCall),
		Container: container,
		Location:  nodeLocation(callee, e.file),
		IsCall:    true,
		Dynamic:   true,
	})
}

// superCall states super().m(…) in a method of a class with one plain base
// B as a call of B's m, capped at Strong: under cooperative multiple
// inheritance the method super() reaches is decided by the instance's class
// at run time. It reports false for any other callee.
func (e *extractor) superCall(fn *ts.Node, sc *scope, container string) bool {
	if e.typ(fn) != "attribute" {
		return false
	}
	obj, attr := e.field(fn, "object"), e.field(fn, "attribute")
	if obj == nil || attr == nil || e.typ(obj) != "call" {
		return false
	}
	callee, args := e.field(obj, "function"), e.field(obj, "arguments")
	if callee == nil || e.typ(callee) != "identifier" || e.text(callee) != "super" || args == nil || args.NamedChildCount() != 0 {
		return false
	}
	if e.text(attr) == "" {
		return true // super(). cut off by an error: no name to state
	}
	ref := language.ReferenceDraft{
		Name:         e.text(attr),
		Kind:         string(reference.KindCall),
		Container:    container,
		Location:     nodeLocation(attr, e.file),
		ReceiverExpr: "super()",
		IsCall:       true,
	}
	// super() names the class of the function it is written in directly:
	// a method, never a function nested in one.
	if cls := sc.inClass; cls != nil && sc.kind == scopeFunction && !sc.lambda && e.lookup(sc, "super") == nil && cls.plainBase != "" {
		b := cls.plainBase
		if e.lookup(sc, b) == e.lookup(cls.parent, b) && e.capFor(cls.parent, b, container, fn.StartByte()) == "" {
			ref.ReceiverType = b
			ref.ConfidenceCap = "strong"
		}
	}
	e.refs = append(e.refs, ref)
	return true
}

// nameRef states a reference to the name an identifier or a chain of
// attributes (a.b.f) names: the last name, with the rest as its receiver.
func (e *extractor) nameRef(n *ts.Node, kind reference.ReferenceKind, sc *scope, container string) {
	at := n
	name, recv := e.text(n), ""
	if e.typ(n) == "attribute" {
		attr := e.field(n, "attribute")
		obj := e.field(n, "object")
		if attr == nil || obj == nil {
			return
		}
		at, name = attr, e.text(attr)
		if recv = e.dotted(obj); recv == "" {
			recv = e.text(obj)
		}
	}
	if name == "" {
		return
	}
	ref := language.ReferenceDraft{
		Name:         name,
		Kind:         string(kind),
		Container:    container,
		Location:     nodeLocation(at, e.file),
		ReceiverExpr: recv,
		IsCall:       kind == reference.KindCall,
	}
	pos := at.StartByte()
	if kind == reference.KindTypeUse && e.lazyAnnotations {
		pos = math.MaxUint32 // an annotation is not evaluated where it is written
	}
	switch {
	case recv == "":
		ref.ConfidenceCap = e.capFor(sc, name, container, pos)
	case isIdentifier(recv):
		if t := e.selfType(sc, recv, container); t != "" {
			ref.ReceiverType = t
		} else {
			ref.ConfidenceCap = e.receiverCap(sc, recv, container, pos)
		}
	default:
		// a.b: the root a decides what the receiver names; f().g: no
		// receiver the resolver can read (R4).
		if root, _, _ := strings.Cut(recv, "."); isIdentifier(root) && e.dotted(e.field(n, "object")) != "" {
			ref.ConfidenceCap = e.capFor(sc, root, container, pos)
		}
	}
	e.refs = append(e.refs, ref)
}

// typeRefs states the type uses of an annotation: every name and chain of
// attributes in it. String annotations are not read.
func (e *extractor) typeRefs(n *ts.Node, sc *scope, container string) {
	switch e.typ(n) {
	case "identifier":
		e.nameRef(n, reference.KindTypeUse, sc, container)
		return
	case "attribute":
		if e.dotted(n) != "" {
			e.nameRef(n, reference.KindTypeUse, sc, container)
			return
		}
	case "string", "concatenated_string":
		return
	case "call":
		e.call(n, sc, container)
		return
	case "lambda", "list_comprehension", "set_comprehension", "dictionary_comprehension", "generator_expression":
		e.ref(n, sc, container)
		return
	}
	for i := range n.NamedChildCount() {
		e.typeRefs(n.NamedChild(i), sc, container)
	}
}

// selfType is the class a receiver name proves to be an instance (or the
// class itself, cls) of: the first parameter of a method whose self is
// proven, read where that binding is the one in scope, of a class whose
// name nothing else in the file declares or binds — so the resolver's type
// name lookup finds exactly that class. A class nested in a class body is a
// member of it, which that lookup (a name without a receiver) never finds:
// it gets no receiver type (a type the lookup cannot find would read as one
// outside the repository).
func (e *extractor) selfType(sc *scope, name, container string) string {
	fn := e.lookup(sc, name)
	if fn == nil || fn.kind != scopeFunction || fn.selfName != name || fn.selfClass == nil {
		return ""
	}
	if p := fn.selfClass.parent; p != nil && (p.kind == scopeClass || p.kind == scopeAnnotation && p.parent != nil && p.parent.kind == scopeClass) {
		return ""
	}
	if b := fn.binds[name]; b == nil || b.params != 1 || b.symbols()+b.imports+b.other > 0 {
		return ""
	}
	cls := fn.selfClass.className
	if cls == "" || e.symbolNames[cls] != 1 || e.children[container+"\x00"+cls] {
		return ""
	}
	if m := e.mod.binds[cls]; m != nil && m.imports+m.other+m.vars > 0 {
		return ""
	}
	return cls
}

// capFor is the cap of a reference to name at scope sc whose container is
// container: "" when the resolver's lookup of the name, within the file,
// finds exactly what Python's scoping makes it denote — a declaration of the
// file (its same-container and same-file stages) or an import binding —
// and Candidate otherwise.
func (e *extractor) capFor(sc *scope, name, container string, at uint32) string {
	b := e.lookup(sc, name)
	// The resolver first looks for a declaration of the name directly in
	// the container: one that is not what the name denotes must not win.
	if container != "" && e.children[container+"\x00"+name] &&
		(b == nil || b.qual != container || b.lambda || (b.kind != scopeFunction && b.kind != scopeClass)) {
		return capCandidate
	}
	if b == nil {
		return capCandidate // a builtin, a name a star import brings, an undefined name
	}
	nb := b.binds[name]
	// A module or class body runs top to bottom: a reference it runs before
	// the name's first binding there reads another one (a builtin, the
	// module's name for a class body's).
	if x := executedIn(sc); (x.kind == scopeModule || x.kind == scopeClass) && (b.kind == scopeModule || b.kind == scopeClass) && at < nb.first {
		return capCandidate
	}
	if b == e.mod {
		switch {
		case nb.other > 0 || nb.params > 0:
			return capCandidate // rebound by a binding no declaration states
		case nb.symbols() > 0 && nb.imports > 0:
			return capCandidate // a declaration and an import: which is last is run-time order
		case e.hasStar && nb.last < e.lastStar:
			return capCandidate // a later star import may rebind it
		}
		return ""
	}
	// A local binding: only imports of a function (its scoped bindings,
	// unless a declaration of the name, which the same-file stage would find
	// first, exists in the file), or only nested declarations, read by the
	// resolver's same-container stage (or, from an inner function, its
	// same-file one).
	if nb.imports > 0 && nb.decl+nb.vars+nb.params+nb.other == 0 && b.kind == scopeFunction && !b.lambda && !e.sameFileVisible(name, container) {
		return ""
	}
	if nb.decl > 0 && nb.vars+nb.imports+nb.params+nb.other == 0 && !b.lambda {
		switch b.kind {
		case scopeFunction:
			return ""
		case scopeClass:
			if b.qual == container {
				return ""
			}
		}
	}
	return capCandidate
}

// executedIn is the scope whose code runs a reference at sc when that code
// runs: sc itself, or for a comprehension the scope it is in.
func executedIn(sc *scope) *scope {
	for sc.kind == scopeComprehension && sc.parent != nil {
		sc = sc.parent
	}
	return sc
}

// sameFileVisible reports whether the resolver's same-file stage offers a
// reference in container a declaration of name: one that is no member, at
// module level or nested in a declaration whose body holds the container
// (Extraction.LexicalNesting hides the others).
func (e *extractor) sameFileVisible(name, container string) bool {
	for _, p := range e.parentsOf[name] {
		if p == "" || container == p || strings.HasPrefix(container, p+".") {
			return true
		}
	}
	return false
}

// receiverCap is the cap of a reference whose receiver is the single name
// recv. The resolver reads such a receiver as an import binding, or — when
// a class of the name exists anywhere in the repository — as that type's
// name: it is left uncapped only where the name is bound to an import or
// to classes of the file alone.
func (e *extractor) receiverCap(sc *scope, recv, container string, at uint32) string {
	if c := e.capFor(sc, recv, container, at); c != "" {
		return c
	}
	b := e.lookup(sc, recv)
	nb := b.binds[recv]
	if nb.imports > 0 || (nb.decl == 1 && nb.classes == 1 && nb.vars == 0) {
		return ""
	}
	return capCandidate
}

// isIdentifier reports whether s is one Python identifier (ASCII or not;
// the grammar has already checked it is a name).
func isIdentifier(s string) bool {
	return s != "" && !strings.ContainsAny(s, ".()[]{} \t\n'\"+-*/%@<>=!,:;~^&|")
}
