package zig

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/treediag"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// maxDepth bounds the recursion over the syntax tree.
const maxDepth = 256

// nodeKey identifies a syntax node of the file by its range and type.
type nodeKey struct {
	start, end uint32
	typ        uint16
}

func keyOf(n *ts.Node) nodeKey {
	return nodeKey{n.StartByte(), n.EndByte(), uint16(n.Symbol())}
}

type extractor struct {
	lang *ts.Language
	src  []byte
	file source.FileID
	key  string // the file's identity: its clean, slash-separated path

	// containers are the file's containers by identity; nil for an identity
	// two containers share (each member is then ambiguous).
	containers map[string]*scope
	// scopeOf are the container scopes by their declaration's node; declOf
	// the containers' declarations by theirs.
	scopeOf map[nodeKey]*scope
	declOf  map[nodeKey]*decl
	// alternativeAlias are the constants that name several namespaces (a
	// comptime choice): their members are read through their aliases.
	alternativeAlias map[string]bool
	// build: the file is a build.zig, whose module declarations are read.
	build bool

	symbols []language.SymbolDraft
	aliases []language.AliasDraft
	refs    []language.ReferenceDraft
	diags   []language.Diagnostic
	deep    bool // a nesting_too_deep diagnostic was reported
}

func newExtractor(lang *ts.Language, src []byte, file source.FileID) *extractor {
	return &extractor{
		lang: lang, src: src, file: file, key: fileKey(file), build: path.Base(fileKey(file)) == "build.zig",
		containers: map[string]*scope{}, scopeOf: map[nodeKey]*scope{}, declOf: map[nodeKey]*decl{}, alternativeAlias: map[string]bool{},
	}
}

// fileKey is a file's identity: its repository-relative path, clean and
// slash-separated (the path an @import computes).
func fileKey(file source.FileID) string {
	return path.Clean(filepath.ToSlash(string(file)))
}

func (x *extractor) run(root *ts.Node) {
	file := newScope(nil, x.key, true)
	file.node, file.symbol, file.name = root, true, path.Base(x.key)
	x.register(x.key, file, root)
	x.symbols = append(x.symbols, language.SymbolDraft{
		Name: path.Base(x.key), Qualified: x.key, Kind: symbol.KindModule,
		Location: x.loc(root), StartByte: root.StartByte(), EndByte: root.EndByte(), Exported: true,
	})
	x.collect(file, root, 0)
	x.emitScope(file)
	x.walkContainer(file, root, ctx{s: file, container: x.key}, 0)
	sort.SliceStable(x.aliases, func(i, j int) bool {
		a, b := x.aliases[i], x.aliases[j]
		if a.Qualified != b.Qualified {
			return a.Qualified < b.Qualified
		}
		return a.Target < b.Target
	})
}

// register records a container scope by its identity and node.
func (x *extractor) register(ident string, s *scope, n *ts.Node) {
	if _, dup := x.containers[ident]; dup {
		x.containers[ident] = nil
	} else {
		x.containers[ident] = s
	}
	x.scopeOf[keyOf(n)] = s
}

// isContainer reports a container declaration node.
func (x *extractor) isContainer(n *ts.Node) bool {
	if n == nil {
		return false
	}
	switch x.typ(n) {
	case "struct_declaration", "union_declaration", "enum_declaration", "opaque_declaration", "error_set_declaration":
		return true
	}
	return false
}

// openContainer creates the scope of container declaration n, of identity
// ident, declared in parent, and collects its members.
func (x *extractor) openContainer(n *ts.Node, ident, name string, parent *scope, symbolic bool, depth int) *scope {
	if sc := x.scopeOf[keyOf(n)]; sc != nil {
		return sc
	}
	sc := newScope(parent, ident, true)
	sc.node, sc.name, sc.symbol = n, name, symbolic
	sc.enum = x.typ(n) == "enum_declaration" || x.typ(n) == "error_set_declaration"
	x.register(ident, sc, n)
	if x.typ(n) == "error_set_declaration" {
		for _, ch := range x.named(n) {
			if x.typ(ch) == "identifier" {
				if nm := identifierName(x.text(ch)); nm != "" {
					d := &decl{name: nm, ident: ident + "::" + nm, kind: symbol.KindConstant, scope: sc, field: true, enumField: true, node: ch}
					if sc.fields[nm] == nil {
						sc.fields[nm] = d
					}
					sc.members = append(sc.members, d)
				}
			}
		}
		return sc
	}
	x.collect(sc, n, depth+1)
	return sc
}

// collect records the members of container s, whose body is n: its
// declarations, fields and tests (in source order), and the containers
// they declare — a container's members are named from anywhere in it.
func (x *extractor) collect(s *scope, n *ts.Node, depth int) {
	if x.tooDeep(n, depth) {
		return
	}
	for _, ch := range x.named(n) {
		switch x.typ(ch) {
		case "variable_declaration":
			name := x.declName(ch)
			if name == "" {
				continue
			}
			d := &decl{name: name, ident: s.prefix + "::" + name, scope: s, node: ch,
				typ: x.field(ch, "type"), init: x.initializer(ch), mutable: x.hasToken(ch, "var")}
			if x.isContainer(d.init) {
				d.body = x.openContainer(d.init, d.ident, name, s, true, depth)
			} else if d.init != nil {
				x.collectAnonymous(d.init, d.ident, s, &d.anonymous, depth)
			}
			s.bind(d)
			s.members = append(s.members, d)
			x.declOf[keyOf(ch)] = d
		case "function_declaration":
			name := identifierName(x.text(x.field(ch, "name")))
			if name == "" {
				continue
			}
			d := &decl{name: name, ident: s.prefix + "::" + name, scope: s, node: ch, fn: ch}
			d.fnScope = x.parameterScope(ch, s, d.ident)
			x.collectReturn(d, depth)
			s.bind(d)
			s.members = append(s.members, d)
			x.declOf[keyOf(ch)] = d
		case "container_field":
			name := identifierName(x.text(x.field(ch, "name")))
			if name == "" || name == "_" {
				continue
			}
			ident := s.prefix + "." + name
			if s.enum {
				ident = s.prefix + "::" + name
			}
			d := &decl{name: name, ident: ident, scope: s, node: ch, field: true, enumField: s.enum, typ: x.field(ch, "type")}
			if s.fields[name] == nil {
				s.fields[name] = d
			}
			s.members = append(s.members, d)
		case "test_declaration":
			name := "test"
			if id := x.firstNamed(ch); id != nil {
				switch x.typ(id) {
				case "string":
					name = "test " + x.text(id)
				case "identifier":
					name = "test " + identifierName(x.text(id))
				}
			}
			d := &decl{name: name, ident: s.prefix + "::" + name, scope: s, node: ch, test: true}
			s.members = append(s.members, d)
			x.declOf[keyOf(ch)] = d
		case "using_namespace_declaration":
			s.opaque = true
		}
	}
}

// parameterScope is the scope of function fn's parameters, declared in s:
// its result type and body are written in it.
func (x *extractor) parameterScope(fn *ts.Node, s *scope, ident string) *scope {
	ps := newScope(s, ident, false)
	for _, p := range x.named(x.childOfType(fn, "parameters")) {
		if x.typ(p) != "parameter" {
			continue
		}
		if name := identifierName(x.text(x.field(p, "name"))); name != "" {
			ps.bind(&decl{name: name, scope: ps, local: true, typ: x.field(p, "type"), node: p})
		}
	}
	return ps
}

// collectReturn finds the container a function returns, when its one
// return is one — a container declaration, or a constant of the body's
// own block declared as one — and its result type is `type`: whatever the
// arguments, a call of it is an instance of that container declaration
// (identity F::()). The container's members are collected now: a call may
// be anywhere in the file.
func (x *extractor) collectReturn(d *decl, depth int) {
	rt, body := x.field(d.fn, "type"), x.field(d.fn, "body")
	if rt == nil || body == nil || !x.isTypeKeyword(rt) {
		return
	}
	var returns []*ts.Node
	x.findReturns(body, &returns, 0)
	if len(returns) != 1 {
		return
	}
	v := x.firstNamed(returns[0])
	// The container is declared in the body: a name of its own block,
	// unseen yet, may precede a name of the enclosing scopes — nothing
	// outside it is found until the body is walked.
	hidden := &scope{parent: d.fnScope, opaque: true, decls: map[string][]*decl{}}
	switch {
	case x.isContainer(v):
		d.returns = x.openContainer(v, d.ident+"::()", d.name+"()", hidden, true, depth)
	case v != nil && x.typ(v) == "identifier" && x.countDecls(body, identifierName(x.text(v)), 0) == 1:
		name := identifierName(x.text(v))
		for _, st := range x.named(body) {
			if x.typ(st) == "variable_declaration" && x.declName(st) == name && !x.hasToken(st, "var") && x.isContainer(x.initializer(st)) {
				local := x.openContainer(x.initializer(st), d.ident+"::"+name, name, hidden, true, depth)
				d.returns = local
				x.containers[d.ident+"::()"] = local
				x.aliases = append(x.aliases, language.AliasDraft{Qualified: d.ident + "::()", Target: local.prefix, Location: x.loc(v)})
				break
			}
		}
	}
}

// countDecls counts the declarations of name in a function body, not in
// the containers it declares.
func (x *extractor) countDecls(n *ts.Node, name string, depth int) int {
	if n == nil || depth > maxDepth || name == "" {
		return 0
	}
	count := 0
	for _, ch := range x.named(n) {
		switch {
		case x.typ(ch) == "variable_declaration" && x.declName(ch) == name:
			count++
		case x.isContainer(ch):
			continue
		}
		count += x.countDecls(ch, name, depth+1)
	}
	return count
}

// findReturns collects the return expressions of a function body, not
// those of the functions of a container it declares.
func (x *extractor) findReturns(n *ts.Node, out *[]*ts.Node, depth int) {
	if n == nil || depth > maxDepth || len(*out) > 1 {
		return
	}
	for _, ch := range x.named(n) {
		switch {
		case x.typ(ch) == "return_expression":
			*out = append(*out, ch)
		case x.isContainer(ch):
			continue
		}
		x.findReturns(ch, out, depth+1)
	}
}

// emitScope states the symbols of container s's members, the aliases its
// constants are, and those of the containers they declare.
func (x *extractor) emitScope(s *scope) {
	parent := s.prefix
	if !s.symbol {
		parent = ""
	}
	for _, d := range s.members {
		x.emitDecl(d, parent, s)
	}
}

// emitDecl states declaration d's symbol (and its alias, its container's
// members).
func (x *extractor) emitDecl(d *decl, parent string, s *scope) {
	sd := language.SymbolDraft{
		Name: d.name, Qualified: d.ident, Kind: x.kindOf(d),
		Location: x.loc(d.node), StartByte: d.node.StartByte(), EndByte: d.node.EndByte(),
		Parent: parent, Signature: x.signature(d), Exported: d.field || x.hasToken(d.node, "pub") || x.hasToken(d.node, "export"),
	}
	if d.fn != nil && s.node != nil && x.typ(s.node) != "source_file" {
		sd.Receiver = s.name
	}
	x.symbols = append(x.symbols, sd)
	switch {
	case d.body != nil:
		x.emitScope(d.body)
	case d.returns != nil && x.scopeOf[keyOf(d.returns.node)] == d.returns && d.returns.prefix == d.ident+"::()":
		r := d.returns
		x.symbols = append(x.symbols, language.SymbolDraft{
			Name: r.name, Qualified: r.prefix, Kind: x.containerKind(r.node),
			Location: x.loc(r.node), StartByte: r.node.StartByte(), EndByte: r.node.EndByte(),
			Parent: d.ident, Signature: x.containerHeader(r.node), Exported: sd.Exported,
		})
		x.emitScope(r)
	case d.returns != nil:
		// A constant of the body declares the container: it is emitted
		// with the body's declarations, when the body is walked.
	case d.fn != nil:
		// The type of what a call returns, as the function declares it: a
		// call's members are that type's (another file reads them through
		// F::()).
		if t := x.result(d, 0).typ; t != "" && t != d.ident+"::()" {
			x.aliases = append(x.aliases, language.AliasDraft{Qualified: d.ident + "::()", Target: t, Location: x.loc(d.node), Members: true})
		}
	case d.init != nil:
		for _, a := range d.anonymous {
			x.emitScope(a)
		}
		if !d.mutable && (d.typ == nil || x.isTypeKeyword(d.typ)) {
			x.aliasesOf(d)
		}
	}
}

// collectAnonymous collects the containers a declaration's value declares
// without a name of their own (`if (c) struct {...} else struct {...}`):
// what the declaration denotes may be one. They are named ident::{struct}.
func (x *extractor) collectAnonymous(n *ts.Node, ident string, s *scope, out *[]*scope, depth int) {
	if n == nil || depth > maxDepth {
		return
	}
	for _, ch := range x.named(n) {
		switch {
		case x.isContainer(ch):
			if x.scopeOf[keyOf(ch)] == nil {
				word := strings.TrimSuffix(x.typ(ch), "_declaration")
				*out = append(*out, x.openContainer(ch, ident+"::{"+word+"}", word, s, false, depth))
			}
		case x.typ(ch) == "block" || x.typ(ch) == "function_declaration":
			// A block's containers are its declarations', collected when
			// it is walked.
		default:
			x.collectAnonymous(ch, ident, s, out, depth+1)
		}
	}
}

// aliasesOf states a constant whose value is a namespace as an alias of its
// members: each namespace it may be (several when a comptime choice picks
// one).
func (x *extractor) aliasesOf(d *decl) {
	iv := x.denote(d.init, d.scope, 0)
	cap := ""
	if len(iv.ns) > 1 {
		// Which alternative the build compiles is not chosen; one may lie
		// outside the repository or hold the member as a field: a member
		// found in one alternative alone is no more than a Candidate.
		x.alternativeAlias[d.ident] = true
		cap = "candidate"
	}
	for _, t := range iv.ns {
		if sc, known := x.containers[t]; known && sc == nil {
			// Containers of this file sharing the identity (unnamed
			// alternatives `if (c) struct {...} else struct {...}`): what
			// one declares may be the other's field.
			cap = "candidate"
		}
	}
	for _, t := range iv.ns {
		if t != d.ident {
			x.aliases = append(x.aliases, language.AliasDraft{Qualified: d.ident, Target: t, Location: x.loc(d.node), Members: true, ConfidenceCap: cap})
		}
	}
}

// kindOf is a declaration's symbol kind. A constant is a type when its
// value is one by syntax, or names a container of this file; a module when
// it is an import or names one.
func (x *extractor) kindOf(d *decl) symbol.SymbolKind {
	switch {
	case d.test:
		return symbol.KindFunction
	case d.fn != nil:
		if d.scope.node != nil && x.typ(d.scope.node) != "source_file" {
			return symbol.KindMethod
		}
		return symbol.KindFunction
	case d.enumField && x.typ(d.scope.node) == "error_set_declaration":
		return symbol.KindConstant
	case d.field:
		return symbol.KindField
	case d.body != nil:
		return x.containerKind(d.body.node)
	case d.mutable:
		return symbol.KindVariable
	case d.init == nil:
		return symbol.KindConstant
	}
	if x.typ(d.init) == "builtin_function" && x.builtinName(d.init) == "@import" {
		if args := x.builtinArgs(d.init); len(args) == 1 {
			if spec, ok := x.stringLiteral(args[0]); ok && !strings.HasSuffix(spec, ".zon") {
				return symbol.KindModule
			}
		}
		return symbol.KindConstant
	}
	if x.typeSyntax(d.init) || (d.typ != nil && x.isTypeKeyword(d.typ)) {
		return symbol.KindTypeAlias
	}
	if t := x.valOf(d, 0).nsOne(); t != "" {
		switch {
		case strings.HasSuffix(t, ".zig") && !strings.Contains(t, "::"):
			return symbol.KindModule
		case x.containers[t] != nil:
			return symbol.KindTypeAlias
		}
	}
	return symbol.KindConstant
}

// containerKind is the symbol kind of a container declaration.
func (x *extractor) containerKind(n *ts.Node) symbol.SymbolKind {
	switch x.typ(n) {
	case "struct_declaration", "union_declaration":
		return symbol.KindStruct
	case "enum_declaration":
		return symbol.KindEnum
	}
	return symbol.KindType // opaque, error set
}

// typeSyntax reports an expression that is a type by its syntax.
func (x *extractor) typeSyntax(n *ts.Node) bool {
	switch x.typ(n) {
	case "pointer_type", "nullable_type", "slice_type", "array_type", "error_union_type", "function_signature", "builtin_type":
		return true
	case "builtin_function":
		switch x.builtinName(n) {
		case "@This", "@Type", "@TypeOf", "@Vector", "@Int", "@Struct", "@Union", "@Enum", "@Tuple",
			"@Pointer", "@Fn", "@EnumLiteral", "@FieldType", "@SpirvType":
			return true
		}
	case "binary_expression":
		l, r := x.field(n, "left"), x.field(n, "right")
		return l != nil && r != nil && (x.typ(l) == "error_set_declaration" || x.typ(r) == "error_set_declaration")
	}
	return false
}

// declName is the name a variable declaration declares.
func (x *extractor) declName(n *ts.Node) string {
	for i := 0; i < n.ChildCount(); i++ {
		ch := n.Child(i)
		if x.typ(ch) == "=" || x.typ(ch) == ":" {
			return ""
		}
		if ch.IsNamed() && x.typ(ch) == "identifier" {
			return identifierName(x.text(ch))
		}
	}
	return ""
}

// initializer is a variable declaration's value: the named child after "=".
func (x *extractor) initializer(n *ts.Node) *ts.Node {
	seen := false
	for i := 0; i < n.ChildCount(); i++ {
		ch := n.Child(i)
		if seen && ch.IsNamed() && x.typ(ch) != "comment" {
			return ch
		}
		if x.typ(ch) == "=" {
			seen = true
		}
	}
	return nil
}

// hasToken reports an anonymous keyword token among n's children.
func (x *extractor) hasToken(n *ts.Node, tok string) bool {
	for i := 0; i < n.ChildCount(); i++ {
		if ch := n.Child(i); !ch.IsNamed() && x.typ(ch) == tok {
			return true
		}
	}
	return false
}

// signature is a declaration's header: a function's text before its body,
// a container's before its members, another declaration's first line.
func (x *extractor) signature(d *decl) string {
	n := d.node
	end := n.EndByte()
	switch {
	case d.fn != nil:
		if b := x.field(n, "body"); b != nil {
			end = b.StartByte()
		}
	case d.body != nil:
		end = x.braceOf(d.body.node)
	}
	text := string(x.src[n.StartByte():end])
	if i := strings.IndexByte(text, '\n'); i >= 0 && d.fn == nil {
		text = text[:i]
	}
	return collapse(text)
}

// containerHeader is a container declaration's text before its members.
func (x *extractor) containerHeader(n *ts.Node) string {
	return collapse(string(x.src[n.StartByte():x.braceOf(n)]))
}

// braceOf is the offset of a container declaration's opening brace.
func (x *extractor) braceOf(n *ts.Node) uint32 {
	for i := 0; i < n.ChildCount(); i++ {
		if ch := n.Child(i); x.typ(ch) == "{" {
			return ch.StartByte()
		}
	}
	return n.EndByte()
}

// collapse joins a text's whitespace runs into single spaces, bounded.
func collapse(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 240 {
		cut := 240
		for cut > 0 && s[cut]&0xC0 == 0x80 {
			cut--
		}
		s = s[:cut] + "…"
	}
	return s
}

// importTarget is the identity an @import names: for a relative file, the
// file's path from the importing file's directory (inRepo false for a path
// leading out of the repository: no file of it is indexed); for a named
// module (std, builtin, root, a module the build maps), {module}name, which
// only a build.zig mapping names (buildzig.go). ok is false for a ZON
// import (data, no Zig declaration) and for a spec no module can have.
func importTarget(fromKey, spec string) (ident string, inRepo, ok bool) {
	if spec == "" || strings.ContainsAny(spec, "\\\x00") || strings.HasSuffix(spec, ".zon") {
		return "", false, false
	}
	if !strings.HasSuffix(spec, ".zig") {
		if strings.Contains(spec, "::") || strings.Contains(spec, "/") {
			return "", false, false
		}
		return moduleIdent(spec), true, true
	}
	if path.IsAbs(spec) {
		return "", false, false
	}
	p := path.Join(path.Dir(fromKey), spec)
	return p, p != ".." && !strings.HasPrefix(p, "../"), true
}

// builtinName is a builtin call's name (`@import`).
func (x *extractor) builtinName(n *ts.Node) string {
	for _, ch := range x.named(n) {
		if x.typ(ch) == "builtin_identifier" {
			return x.text(ch)
		}
	}
	return ""
}

// builtinArgs are a builtin call's arguments.
func (x *extractor) builtinArgs(n *ts.Node) []*ts.Node {
	for _, ch := range x.named(n) {
		if x.typ(ch) == "arguments" {
			return x.named(ch)
		}
	}
	return nil
}

// stringLiteral is the content of a string literal without escapes.
func (x *extractor) stringLiteral(n *ts.Node) (string, bool) {
	if n == nil || x.typ(n) != "string" {
		return "", false
	}
	parts := x.named(n)
	if len(parts) != 1 || x.typ(parts[0]) != "string_content" {
		return "", false
	}
	return x.text(parts[0]), true
}

func (x *extractor) text(n *ts.Node) string {
	if n == nil {
		return ""
	}
	return n.Text(x.src)
}

func (x *extractor) field(n *ts.Node, name string) *ts.Node {
	if n == nil {
		return nil
	}
	return n.ChildByFieldName(name, x.lang)
}

func (x *extractor) typ(n *ts.Node) string {
	if n == nil {
		return ""
	}
	return n.Type(x.lang)
}

// named returns n's named children in order, comments left out. The
// runtime's NamedChild(i) scans the children on every call, so a loop over
// it is quadratic in a long list; Child(i) is not.
func (x *extractor) named(n *ts.Node) []*ts.Node {
	if n == nil {
		return nil
	}
	out := make([]*ts.Node, 0, n.ChildCount())
	for i := 0; i < n.ChildCount(); i++ {
		if ch := n.Child(i); ch.IsNamed() && x.typ(ch) != "comment" {
			out = append(out, ch)
		}
	}
	return out
}

// childOfType is n's first child of node type t, or nil.
func (x *extractor) childOfType(n *ts.Node, t string) *ts.Node {
	for i := 0; n != nil && i < n.ChildCount(); i++ {
		if ch := n.Child(i); x.typ(ch) == t {
			return ch
		}
	}
	return nil
}

// firstNamed is n's first named child (comments aside), or nil.
func (x *extractor) firstNamed(n *ts.Node) *ts.Node {
	for i := 0; n != nil && i < n.ChildCount(); i++ {
		if ch := n.Child(i); ch.IsNamed() && x.typ(ch) != "comment" {
			return ch
		}
	}
	return nil
}

// lastNamed is n's last named child (comments aside), or nil.
func (x *extractor) lastNamed(n *ts.Node) *ts.Node {
	if n == nil {
		return nil
	}
	for i := n.ChildCount() - 1; i >= 0; i-- {
		if ch := n.Child(i); ch.IsNamed() && x.typ(ch) != "comment" {
			return ch
		}
	}
	return nil
}

func (x *extractor) loc(n *ts.Node) source.Location {
	return source.Location{
		File: x.file,
		Range: source.Range{
			Start: source.Position{Line: n.StartPoint().Row + 1, Column: n.StartPoint().Column + 1},
			End:   source.Position{Line: n.EndPoint().Row + 1, Column: n.EndPoint().Column + 1},
		},
	}
}

// emptyNames reports the names the parser supplied without text — a name
// its recovery inserted, which the runtime does not mark MISSING (`const
// = ;`): the code around it is not analyzed as written. An empty container
// (`struct {}`) is derived with an empty field of no text: valid source.
func (x *extractor) emptyNames(root *ts.Node) []language.Diagnostic {
	var out []language.Diagnostic
	stack := []*ts.Node{root}
	for len(stack) > 0 && len(out) < treediag.MaxParseErrors {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.StartByte() == n.EndByte() && x.typ(n) == "container_field" {
			continue
		}
		if n.IsNamed() && n.ChildCount() == 0 && n.StartByte() == n.EndByte() && x.typ(n) == "identifier" {
			out = append(out, language.Diagnostic{
				Severity: language.SeverityError,
				Code:     treediag.CodeParseError,
				Message:  "parse error: the parser expected identifier here; the surrounding code is not analyzed as written (the source itself may be valid)",
				Location: x.loc(n),
			})
			continue
		}
		for i := n.ChildCount() - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
	return out
}

func (x *extractor) tooDeep(n *ts.Node, depth int) bool {
	if depth < maxDepth {
		return false
	}
	if !x.deep {
		x.deep = true
		x.diags = append(x.diags, language.Diagnostic{
			Severity: language.SeverityWarning,
			Code:     diagNestingTooDeep,
			Message:  "zig: syntax nested too deep; the rest of it is not analyzed",
			Location: x.loc(n),
		})
	}
	return true
}
