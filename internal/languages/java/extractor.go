package java

import (
	"fmt"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// maxDepth bounds the recursion over the syntax tree.
const maxDepth = 400

type extractor struct {
	lang *ts.Language
	src  []byte
	file source.FileID

	pkg string // the file's package ("" for the unnamed package)

	// Imports: simple name → canonical name (single-type), the packages
	// and types of on-demand imports, the static imports' types by member
	// name (single) and in full (on-demand).
	singleTypes    map[string]string
	onDemand       []string
	staticSingle   map[string][]string
	staticOnDemand []string

	topTypes map[string]*typeInfo // the file's top-level types by name

	// broken: the file has a region the parser rejected outside a method
	// body — a declaration or an import may be lost, so no lookup of the
	// file is trusted: it states no references.
	broken bool

	symbols []language.SymbolDraft
	refs    []language.ReferenceDraft
	diags   []language.Diagnostic
	deep    bool

	localIDs map[string]int // namesakes of local and anonymous classes
}

func newExtractor(lang *ts.Language, src []byte, file source.FileID) *extractor {
	return &extractor{
		lang: lang, src: src, file: file,
		singleTypes:  map[string]string{},
		staticSingle: map[string][]string{},
		topTypes:     map[string]*typeInfo{},
		localIDs:     map[string]int{},
	}
}

// typeInfo is what the file states about one type declaration.
type typeInfo struct {
	q, name string
	kind    symbol.SymbolKind
	jkind   string // class, interface, enum, record, annotation, anonymous
	node    *ts.Node
	body    *ts.Node

	// header is the scope its header (extends, implements, bounds) is read
	// in: its type parameters and what encloses it, not its own members.
	header *frame

	methods     map[string]bool
	fields      map[string]*ts.Node // own field → its declared type node (nil: none written)
	memberTypes map[string]*typeInfo
	typeParams  map[string]*ts.Node // type parameter → its bound(s) node, nil without

	// supers: it has supertypes the file names (extends, implements; an
	// anonymous class's or enum constant body's), whose members only the
	// repository knows.
	supers bool
	// superclass is the lookup of its direct superclass as a receiver
	// type, and its text ("" without one: java.lang.Object).
	superLookup, superText string

	implicit     string          // ImplicitMembers
	implicitName map[string]bool // their names
	broken       bool            // its body has a region the parser rejected
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

func (x *extractor) typ(n *ts.Node) string { return n.Type(x.lang) }

// named returns n's named children in order (Child(i), not the quadratic
// NamedChild(i) loop), comments left out: a comment may stand between any
// two tokens, and is no argument, no type, no part of a name.
func (x *extractor) named(n *ts.Node) []*ts.Node {
	if n == nil {
		return nil
	}
	out := make([]*ts.Node, 0, n.ChildCount())
	for i := 0; i < n.ChildCount(); i++ {
		if ch := n.Child(i); ch.IsNamed() {
			if t := x.typ(ch); t == "line_comment" || t == "block_comment" {
				continue
			}
			out = append(out, ch)
		}
	}
	return out
}

// child returns n's first named child of type t, or nil.
func (x *extractor) child(n *ts.Node, t string) *ts.Node {
	for i := 0; n != nil && i < n.ChildCount(); i++ {
		if ch := n.Child(i); ch.IsNamed() && x.typ(ch) == t {
			return ch
		}
	}
	return nil
}

// hasToken reports whether n has an anonymous child token tok.
func (x *extractor) hasToken(n *ts.Node, tok string) bool {
	for i := 0; n != nil && i < n.ChildCount(); i++ {
		if ch := n.Child(i); !ch.IsNamed() && x.typ(ch) == tok {
			return true
		}
	}
	return false
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

func (x *extractor) tooDeep(n *ts.Node, depth int) bool {
	if depth < maxDepth {
		return false
	}
	if !x.deep {
		x.deep = true
		x.diags = append(x.diags, language.Diagnostic{
			Severity: language.SeverityWarning,
			Code:     diagNestingTooDeep,
			Message:  "java: syntax nested too deep; the rest of it is not analyzed",
			Location: x.loc(n),
		})
	}
	return true
}

// dotted returns the text of a package or import name (identifiers joined
// by "."), "" if it holds anything else.
func (x *extractor) dotted(n *ts.Node) string {
	switch x.typ(n) {
	case "identifier":
		return x.text(n)
	case "scoped_identifier":
		scope, name := x.dotted(x.field(n, "scope")), x.field(n, "name")
		if scope == "" || name == nil {
			return ""
		}
		return scope + "." + x.text(name)
	}
	return ""
}

// run extracts the file.
func (x *extractor) run(root *ts.Node) {
	// Pass 1: the package, the imports and every type declared outside a
	// body, with their members and symbols.
	var types []*typeInfo
	for _, n := range x.named(root) {
		switch x.typ(n) {
		case "package_declaration":
			for _, ch := range x.named(n) {
				if d := x.dotted(ch); d != "" {
					x.pkg = d
				}
			}
		case "import_declaration":
			x.addImport(n)
		case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration", "annotation_type_declaration":
			if t := x.collectTypeIn(n, nil, cuFrame, x.qualify(""), "", 0, false); t != nil {
				x.topTypes[t.name] = t
				types = append(types, t)
			}
		case "ERROR":
			x.broken = true
		}
	}
	if x.outsideBodiesBroken(root) {
		x.broken = true
	}
	if x.broken {
		// A declaration, an import, an extends clause may be lost: no type
		// of the file has members — or supertypes — Ark can list.
		for i := range x.symbols {
			switch x.symbols[i].Kind {
			case symbol.KindClass, symbol.KindInterface, symbol.KindEnum:
				x.symbols[i].ConfidenceCap = "candidate"
			}
		}
		return
	}
	// Pass 2: references, scope by scope.
	for _, t := range types {
		x.walkTypeRefs(t, 0)
	}
}

// qualify is the canonical-name prefix of a top-level type.
func (x *extractor) qualify(name string) string {
	if x.pkg == "" {
		return name
	}
	if name == "" {
		return x.pkg + "."
	}
	return x.pkg + "." + name
}

// outsideBodiesBroken reports whether a region the parser rejected lies
// outside every method, constructor and initializer body: a declaration or
// an import may be lost there.
func (x *extractor) outsideBodiesBroken(root *ts.Node) bool {
	var walk func(n *ts.Node, depth int) bool
	walk = func(n *ts.Node, depth int) bool {
		if !n.HasError() {
			return false
		}
		if n.IsError() || n.IsMissing() || depth > maxDepth {
			return true
		}
		switch x.typ(n) {
		case "block", "constructor_body":
			return false // inside a body: only that body is affected
		}
		for i := 0; i < n.ChildCount(); i++ {
			if walk(n.Child(i), depth+1) {
				return true
			}
		}
		return false
	}
	return walk(root, 0)
}

func (x *extractor) addImport(n *ts.Node) {
	static := x.hasToken(n, "static")
	onDemand := x.child(n, "asterisk") != nil
	name := ""
	for _, ch := range x.named(n) {
		if d := x.dotted(ch); d != "" {
			name = d
		}
	}
	if name == "" {
		return
	}
	switch {
	case static && onDemand:
		x.staticOnDemand = append(x.staticOnDemand, name)
	case static:
		if i := strings.LastIndexByte(name, '.'); i > 0 {
			member := name[i+1:]
			x.staticSingle[member] = append(x.staticSingle[member], name[:i])
		}
	case onDemand:
		x.onDemand = append(x.onDemand, name)
	default:
		simple := name
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			simple = name[i+1:]
		}
		x.singleTypes[simple] = name
	}
}

// modifiers returns the modifier keywords of a declaration.
func (x *extractor) modifiers(n *ts.Node) map[string]bool {
	out := map[string]bool{}
	if m := x.child(n, "modifiers"); m != nil {
		for i := 0; i < m.ChildCount(); i++ {
			if ch := m.Child(i); !ch.IsNamed() {
				out[x.text(ch)] = true
			}
		}
	}
	return out
}

// annotated reports whether declaration n has an annotation named name
// (simple, or java.lang qualified).
func (x *extractor) annotated(n *ts.Node, name string) bool {
	for _, a := range x.named(x.child(n, "modifiers")) {
		if t := x.typ(a); t == "marker_annotation" || t == "annotation" {
			if an := x.text(x.field(a, "name")); an == name || an == "java.lang."+name {
				return true
			}
		}
	}
	return false
}

func visibilityOf(mods map[string]bool, inInterface bool) string {
	switch {
	case mods["public"]:
		return "public"
	case mods["protected"]:
		return "protected"
	case mods["private"]:
		return "private"
	case inInterface:
		return "public"
	}
	return ""
}

// The members of the implicit supertypes outside the repository
// (JLS 4.3.2, 8.9.2, 8.10.3).
const (
	objectMembers = "getClass(),hashCode(),equals(?Object),clone(),toString(),notify(),notifyAll(),wait(),wait(long),wait(long,int),finalize()"
	enumMembers   = "name(),ordinal(),compareTo(?E),getDeclaringClass(),describeConstable(),valueOf(?Class,?String)"
)

// collectTypeIn states a type declaration — its symbol, its members' and
// its member types' — and returns what the file knows of it. outer is the
// type it is a member of (nil otherwise), enclosing the scope it is declared
// in, parentQ the qualified name of the enclosing declaration ("" at top
// level), prefix the text its identity starts with; local marks a type
// declared in a body, whose identity is made unique within the file.
func (x *extractor) collectTypeIn(n *ts.Node, outer *typeInfo, enclosing *frame, prefix, parentQ string, depth int, local bool) *typeInfo {
	if x.tooDeep(n, depth) {
		return nil
	}
	nameNode := x.field(n, "name")
	if nameNode == nil || nameNode.IsMissing() || x.text(nameNode) == "" {
		return nil
	}
	name := x.text(nameNode)
	t := &typeInfo{
		q: prefix + name, name: name, node: n, body: x.field(n, "body"),
		methods: map[string]bool{}, fields: map[string]*ts.Node{}, memberTypes: map[string]*typeInfo{},
		typeParams: map[string]*ts.Node{}, implicitName: map[string]bool{},
	}
	if local {
		t.q = x.localIdentity(prefix + name)
	}
	mods := x.modifiers(n)
	inInterface := outer != nil && (outer.jkind == "interface" || outer.jkind == "annotation")
	switch x.typ(n) {
	case "class_declaration":
		t.kind, t.jkind = symbol.KindClass, "class"
	case "interface_declaration":
		t.kind, t.jkind = symbol.KindInterface, "interface"
	case "enum_declaration":
		t.kind, t.jkind = symbol.KindEnum, "enum"
	case "record_declaration":
		t.kind, t.jkind = symbol.KindClass, "record"
	case "annotation_type_declaration":
		t.kind, t.jkind = symbol.KindInterface, "annotation"
	}
	x.typeParamsOf(n, t.typeParams)
	t.header = enclosing.push(&frame{typeParams: t.typeParams, typeParamPos: 0})
	sym := language.SymbolDraft{
		Name: name, Qualified: t.q, Kind: t.kind, Location: x.loc(n),
		StartByte: n.StartByte(), EndByte: n.EndByte(), Parent: parentQ,
		Visibility: visibilityOf(mods, inInterface),
	}
	sym.Exported = sym.Visibility == "public"
	if outer != nil {
		sym.Receiver = outer.name
		if mods["static"] || inInterface || t.jkind != "class" {
			sym.Placement = symbol.PlacementStatic
		}
	}
	if t.body != nil && x.outsideBodiesBroken(t.body) {
		// a member may be lost: what the type declares is not known
		t.broken = true
		sym.ConfidenceCap = "candidate"
	}
	idx := len(x.symbols)
	x.symbols = append(x.symbols, sym)

	// Supertypes and the header's lookups.
	x.collectSupers(n, t)
	x.collectMembers(t, depth)
	x.symbols[idx].ImplicitMembers = t.implicit
	return t
}

// localIdentity makes the identity of a local or anonymous class unique
// within the file: the second namesake is "#2", and so on.
func (x *extractor) localIdentity(base string) string {
	x.localIDs[base]++
	if n := x.localIDs[base]; n > 1 {
		return fmt.Sprintf("%s#%d", base, n)
	}
	return base
}

// typeParamsOf records the type parameters of declaration n.
func (x *extractor) typeParamsOf(n *ts.Node, into map[string]*ts.Node) {
	tps := x.field(n, "type_parameters")
	if tps == nil {
		tps = x.child(n, "type_parameters")
	}
	for _, tp := range x.named(tps) {
		if x.typ(tp) != "type_parameter" {
			continue
		}
		var name string
		var bound *ts.Node
		for _, ch := range x.named(tp) {
			switch x.typ(ch) {
			case "type_identifier", "identifier":
				if name == "" {
					name = x.text(ch)
				}
			case "type_bound":
				bound = ch
			}
		}
		if name != "" {
			into[name] = bound
		}
	}
}

// collectSupers reads whether t's header names supertypes. Their lookups
// are made once every declaration of the file is known (superclassLookup):
// a member type declared further down is in scope.
func (x *extractor) collectSupers(n *ts.Node, t *typeInfo) {
	if sc := x.field(n, "superclass"); sc != nil {
		t.supers = true
	}
	if x.field(n, "interfaces") != nil || x.child(n, "extends_interfaces") != nil {
		t.supers = true
	}
	if t.jkind == "enum" || t.jkind == "record" {
		// java.lang.Enum, java.lang.Record: outside the repository, their
		// members stated as implicit.
	}
}

// superclassLookup sets t's superclass lookup and text.
func (x *extractor) superclassLookup(t *typeInfo) {
	if sc := x.field(t.node, "superclass"); sc != nil {
		if ty := x.firstType(sc); ty != nil {
			t.superText = x.typeText(ty)
			t.superLookup = x.typeLookup(ty, t.header, ty.StartByte())
		}
	}
}

// firstType returns the first type node under n.
func (x *extractor) firstType(n *ts.Node) *ts.Node {
	for _, ch := range x.named(n) {
		if isTypeNode(x.typ(ch)) {
			return ch
		}
	}
	return nil
}

func isTypeNode(t string) bool {
	switch t {
	case "type_identifier", "scoped_type_identifier", "generic_type", "array_type", "integral_type",
		"floating_point_type", "boolean_type", "void_type", "annotated_type":
		return true
	}
	return false
}

// collectMembers states t's members: methods, constructors, fields, enum
// constants, record components, annotation elements, and member types.
func (x *extractor) collectMembers(t *typeInfo, depth int) {
	hasCtor := false
	var components []*ts.Node
	if t.jkind == "record" {
		for _, p := range x.named(x.field(t.node, "parameters")) {
			if x.typ(p) == "formal_parameter" {
				components = append(components, p)
			}
		}
	}
	body := t.body
	members := x.named(body)
	if t.jkind == "enum" {
		var flat []*ts.Node
		for _, m := range members {
			if x.typ(m) == "enum_body_declarations" {
				flat = append(flat, x.named(m)...)
			} else {
				flat = append(flat, m)
			}
		}
		members = flat
	}
	inInterface := t.jkind == "interface" || t.jkind == "annotation"
	declared := map[string]bool{} // methods with their Parameters, for records' implicit ones
	for _, m := range members {
		switch x.typ(m) {
		case "method_declaration", "annotation_type_element_declaration":
			name := x.field(m, "name")
			if name == nil || name.IsMissing() {
				continue
			}
			mods := x.modifiers(m)
			params := "()"
			if x.typ(m) == "method_declaration" {
				params = x.parameters(m, t)
			}
			sym := language.SymbolDraft{
				Name: x.text(name), Qualified: t.q + "." + x.text(name), Kind: symbol.KindMethod,
				Location: x.loc(m), StartByte: m.StartByte(), EndByte: m.EndByte(),
				Parent: t.q, Receiver: t.name, Signature: x.signature(m),
				Visibility: visibilityOf(mods, inInterface), Parameters: params,
				Placement: symbol.PlacementInstance, Overrides: x.annotated(m, "Override"),
			}
			if mods["static"] {
				sym.Placement = symbol.PlacementStatic
			}
			sym.Exported = sym.Visibility == "public"
			x.symbols = append(x.symbols, sym)
			t.methods[sym.Name] = true
			declared[sym.Name+params] = true
		case "constructor_declaration", "compact_constructor_declaration":
			mods := x.modifiers(m)
			params := ""
			if x.typ(m) == "constructor_declaration" {
				params = x.parameters(m, t)
			} else {
				params = x.componentParams(components, t)
			}
			x.symbols = append(x.symbols, language.SymbolDraft{
				Name: "<init>", Qualified: t.q + ".<init>", Kind: symbol.KindConstructor,
				Location: x.loc(m), StartByte: m.StartByte(), EndByte: m.EndByte(),
				Parent: t.q, Receiver: t.name, Signature: x.signature(m),
				Visibility: visibilityOf(mods, false), Parameters: params,
				Exported: mods["public"],
			})
			hasCtor = true
			declared["<init>"+params] = true
		case "field_declaration", "constant_declaration":
			mods := x.modifiers(m)
			ty := x.field(m, "type")
			for _, d := range x.named(m) {
				if x.typ(d) != "variable_declarator" {
					continue
				}
				name := x.field(d, "name")
				if name == nil || name.IsMissing() {
					continue
				}
				placement := symbol.PlacementInstance
				if mods["static"] || inInterface {
					placement = symbol.PlacementStatic
				}
				vis := visibilityOf(mods, inInterface)
				x.symbols = append(x.symbols, language.SymbolDraft{
					Name: x.text(name), Qualified: t.q + "." + x.text(name), Kind: symbol.KindField,
					Location: x.loc(d), StartByte: d.StartByte(), EndByte: d.EndByte(),
					Parent: t.q, Receiver: t.name, Visibility: vis, Placement: placement,
					Exported: vis == "public",
				})
				if x.field(d, "dimensions") != nil {
					t.fields[x.text(name)] = nil // an array: its members are the language's
				} else {
					t.fields[x.text(name)] = ty
				}
			}
		case "enum_constant":
			name := x.field(m, "name")
			if name == nil || name.IsMissing() {
				continue
			}
			x.symbols = append(x.symbols, language.SymbolDraft{
				Name: x.text(name), Qualified: t.q + "." + x.text(name), Kind: symbol.KindConstant,
				Location: x.loc(m), StartByte: m.StartByte(), EndByte: m.EndByte(),
				Parent: t.q, Receiver: t.name, Visibility: "public", Placement: symbol.PlacementStatic,
				Exported: true,
			})
			t.fields[x.text(name)] = nil // typed by the enum, followed through it below
		case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration", "annotation_type_declaration":
			body := &frame{typ: t}
			if mt := x.collectTypeIn(m, t, t.header.push(body), t.q+".", t.q, depth+1, false); mt != nil {
				t.memberTypes[mt.name] = mt
			}
		}
	}
	// Record components: private fields, and the implicit accessors and
	// canonical constructor no declaration states.
	var implicit []string
	for _, c := range components {
		name := x.field(c, "name")
		if name == nil || name.IsMissing() {
			continue
		}
		x.symbols = append(x.symbols, language.SymbolDraft{
			Name: x.text(name), Qualified: t.q + "." + x.text(name), Kind: symbol.KindField,
			Location: x.loc(c), StartByte: c.StartByte(), EndByte: c.EndByte(),
			Parent: t.q, Receiver: t.name, Visibility: "private", Placement: symbol.PlacementInstance,
		})
		t.fields[x.text(name)] = x.field(c, "type")
		if !declared[x.text(name)+"()"] {
			implicit = append(implicit, "+"+x.text(name)+"()")
		}
	}
	switch t.jkind {
	case "record":
		if p := x.componentParams(components, t); !declared["<init>"+p] {
			implicit = append(implicit, "+<init>"+p)
		}
		for _, m := range []string{"equals(?Object)", "hashCode()", "toString()"} {
			if !declared[m] {
				implicit = append(implicit, "+"+m)
			}
		}
		implicit = append(implicit, objectMembers)
	case "enum":
		implicit = append(implicit, "+values()", "+valueOf(?String)", enumMembers, objectMembers)
	case "interface", "annotation":
		implicit = append(implicit, objectMembers)
	case "class":
		if !hasCtor {
			implicit = append(implicit, "+<init>()")
		}
		if x.field(t.node, "superclass") == nil {
			implicit = append(implicit, objectMembers)
		}
	}
	t.implicit = strings.Join(implicit, ",")
	t.implicitName = implicitNames(t.implicit)
}

// implicitNames returns the names ImplicitMembers text lists (its entries
// are separated by the commas outside parentheses).
func implicitNames(s string) map[string]bool {
	out := map[string]bool{}
	depth, start := 0, 0
	add := func(e string) {
		e = strings.TrimPrefix(e, "+")
		if i := strings.IndexByte(e, '('); i > 0 {
			out[e[:i]] = true
		}
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				add(s[start:i])
				start = i + 1
			}
		}
	}
	add(s[start:])
	return out
}

// parameters is a callable's Parameters (language.SymbolDraft.Parameters).
func (x *extractor) parameters(m *ts.Node, t *typeInfo) string {
	var parts []string
	for _, p := range x.named(x.field(m, "parameters")) {
		switch x.typ(p) {
		case "formal_parameter":
			parts = append(parts, x.erasure(x.field(p, "type"), x.field(p, "dimensions") != nil, t, m))
		case "spread_parameter":
			parts = append(parts, x.erasure(x.firstType(p), false, t, m)+"...")
		}
	}
	return "(" + strings.Join(parts, ",") + ")"
}

// componentParams is a record's canonical constructor's Parameters.
func (x *extractor) componentParams(components []*ts.Node, t *typeInfo) string {
	var parts []string
	for _, c := range components {
		parts = append(parts, x.erasure(x.field(c, "type"), x.field(c, "dimensions") != nil, t, nil))
	}
	return "(" + strings.Join(parts, ",") + ")"
}

// erasure writes a parameter type as Parameters states it: a primitive as
// itself; a reference type as "?" and its simple name, without type
// arguments (a name the file's imports decide); arrays with "[]".
func (x *extractor) erasure(ty *ts.Node, extraDims bool, t *typeInfo, m *ts.Node) string {
	if ty == nil {
		return "?"
	}
	out := ""
	switch x.typ(ty) {
	case "integral_type", "floating_point_type", "boolean_type":
		out = x.text(ty)
	case "array_type":
		elem := x.erasure(x.field(ty, "element"), false, t, m)
		out = elem + strings.Repeat("[]", strings.Count(x.text(x.field(ty, "dimensions")), "["))
	case "generic_type":
		out = x.erasure(x.firstType(ty), false, t, m)
	case "annotated_type":
		out = x.erasure(x.firstType(ty), false, t, m)
	case "scoped_type_identifier", "type_identifier":
		txt := x.text(ty)
		if i := strings.LastIndexByte(txt, '.'); i >= 0 {
			txt = strings.TrimSpace(txt[i+1:])
		}
		out = "?" + txt
	default:
		out = "?"
	}
	if extraDims {
		out += "[]"
	}
	return out
}

// signature is a callable's display signature: its parameter list as
// written, whitespace folded.
func (x *extractor) signature(m *ts.Node) string {
	ps := x.field(m, "parameters")
	if ps == nil {
		return ""
	}
	return strings.Join(strings.Fields(x.text(ps)), " ")
}
