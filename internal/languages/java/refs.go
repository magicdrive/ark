package java

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// ref appends a reference.
func (x *extractor) ref(r language.ReferenceDraft, at *ts.Node) {
	r.Location = x.loc(at)
	x.refs = append(x.refs, r)
}

// bodyFrame is the chain inside t's body.
func bodyFrame(t *typeInfo) *frame {
	return t.header.push(&frame{typ: t})
}

// walkTypeRefs states the references of type declaration t: its header,
// its members' signatures and bodies, its member types'.
func (x *extractor) walkTypeRefs(t *typeInfo, depth int) {
	if x.tooDeep(t.node, depth) {
		return
	}
	n := t.node
	if t.jkind != "anonymous" {
		x.superclassLookup(t)
	}
	x.walkModifiers(n, t.header, t.q, depth)
	x.walkTypeParams(n, t.header, t.q, depth)
	if sc := x.field(n, "superclass"); sc != nil {
		if ty := x.firstType(sc); ty != nil {
			x.supertypeRef(ty, reference.KindInheritance, t, depth)
		}
	}
	if is := x.field(n, "interfaces"); is != nil {
		kind := reference.KindImplements
		for _, ty := range x.named(x.child(is, "type_list")) {
			x.supertypeRef(ty, kind, t, depth)
		}
	}
	if ei := x.child(n, "extends_interfaces"); ei != nil {
		for _, ty := range x.named(x.child(ei, "type_list")) {
			x.supertypeRef(ty, reference.KindInheritance, t, depth)
		}
	}
	if p := x.field(n, "permits"); p != nil {
		for _, ty := range x.named(x.child(p, "type_list")) {
			x.walkType(ty, t.header, t.q, reference.KindTypeUse, depth+1)
		}
	}
	body := bodyFrame(t)
	if t.jkind == "record" {
		// A record's members are in scope in its header (JLS 6.3).
		for _, c := range x.named(x.field(n, "parameters")) {
			if x.typ(c) == "formal_parameter" {
				x.walkModifiers(c, body, t.q, depth)
				x.walkType(x.field(c, "type"), body, t.q, reference.KindTypeUse, depth+1)
			}
		}
	}
	x.walkMembers(t, t.body, body, depth+1)
}

// supertypeRef states a supertype relation of t (and the uses of its type
// arguments).
func (x *extractor) supertypeRef(ty *ts.Node, kind reference.ReferenceKind, t *typeInfo, depth int) {
	base := ty
	if x.typ(ty) == "generic_type" {
		base = x.firstType(ty)
		for _, ta := range x.named(x.child(ty, "type_arguments")) {
			x.walkType(ta, t.header, t.q, reference.KindTypeUse, depth+1)
		}
	}
	steps, ok := x.typeNameSteps(base, t.header, base.StartByte())
	if !ok || len(steps) == 0 {
		// A supertype Ark cannot name is still one: what it declares is
		// not known.
		steps = []string{"?"}
	}
	x.ref(language.ReferenceDraft{Name: lastSegment(x.typeText(base)), Kind: string(kind), Container: t.q, Lookup: strings.Join(steps, ";")}, x.lastName(base))
}

// typeNameNodes returns the type_identifier nodes of a scoped type name in
// order (type arguments aside).
func (x *extractor) typeNameNodes(ty *ts.Node) []*ts.Node {
	var out []*ts.Node
	for _, ch := range x.named(ty) {
		switch x.typ(ch) {
		case "type_identifier":
			out = append(out, ch)
		case "scoped_type_identifier":
			out = append(out, x.typeNameNodes(ch)...)
		case "generic_type", "annotated_type":
			if base := x.firstType(ch); base != nil {
				if x.typ(base) == "type_identifier" {
					out = append(out, base)
				} else {
					out = append(out, x.typeNameNodes(base)...)
				}
			}
		}
	}
	return out
}

// lastName is the node of the last name a type node writes (Outer.Inner:
// Inner), where its reference is located.
func (x *extractor) lastName(ty *ts.Node) *ts.Node {
	if t := x.typ(ty); t == "generic_type" || t == "annotated_type" {
		if inner := x.firstType(ty); inner != nil {
			return x.lastName(inner)
		}
	}
	if x.typ(ty) == "scoped_type_identifier" {
		var last *ts.Node
		for _, ch := range x.named(ty) {
			if x.typ(ch) == "type_identifier" {
				last = ch
			}
		}
		if last != nil {
			return last
		}
	}
	return ty
}

func lastSegment(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// walkMembers states the references of the members in body (a class body,
// an enum body, an interface body) of t.
func (x *extractor) walkMembers(t *typeInfo, body *ts.Node, f *frame, depth int) {
	if x.tooDeep(body, depth) {
		return
	}
	for _, m := range x.named(body) {
		switch x.typ(m) {
		case "enum_body_declarations":
			x.walkMembers(t, m, f, depth+1)
		case "field_declaration", "constant_declaration":
			first := ""
			for _, d := range x.named(m) {
				if x.typ(d) == "variable_declarator" {
					if name := x.field(d, "name"); name != nil && first == "" {
						first = t.q + "." + x.text(name)
					}
				}
			}
			if first == "" {
				continue
			}
			x.walkModifiers(m, f, first, depth)
			x.walkType(x.field(m, "type"), f, first, reference.KindTypeUse, depth+1)
			for _, d := range x.named(m) {
				if x.typ(d) != "variable_declarator" {
					continue
				}
				c := t.q + "." + x.text(x.field(d, "name"))
				if v := x.field(d, "value"); v != nil {
					x.walkBody(v, f, c, depth+1, false)
				}
			}
		case "method_declaration", "constructor_declaration", "compact_constructor_declaration", "annotation_type_element_declaration":
			x.walkCallable(t, m, f, depth+1)
		case "static_initializer", "block":
			x.walkBody(m, f, t.q, depth+1, true)
		case "enum_constant":
			name := x.field(m, "name")
			if name == nil {
				continue
			}
			c := t.q + "." + x.text(name)
			x.walkModifiers(m, f, c, depth)
			args := x.field(m, "arguments")
			x.walkExprs(args, f, c, depth+1)
			if x.field(m, "body") == nil {
				// The enum's constructor this constant invokes (a constant
				// with a body is an anonymous class's, the language's own).
				x.ref(language.ReferenceDraft{Name: "<init>", Kind: string(reference.KindCall), Container: c, IsCall: true,
					Lookup: "@C:" + t.q + ".<init>", CountsArguments: true, ArgumentCount: len(x.named(args)),
					ArgumentTypes: x.argumentTypes(args, f), Speculative: true}, name)
			}
			if b := x.field(m, "body"); b != nil {
				x.anonymous(b, "=T:"+t.q, t.name, f, c, name, depth+1)
			}
		case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration", "annotation_type_declaration":
			if nameNode := x.field(m, "name"); nameNode != nil {
				if mt := t.memberTypes[x.text(nameNode)]; mt != nil && mt.node == m {
					x.walkTypeRefs(mt, depth+1)
				}
			}
		}
	}
}

// walkCallable states a method's or constructor's references.
func (x *extractor) walkCallable(t *typeInfo, m *ts.Node, f *frame, depth int) {
	c := t.q + ".<init>"
	if x.typ(m) == "method_declaration" || x.typ(m) == "annotation_type_element_declaration" {
		name := x.field(m, "name")
		if name == nil {
			return
		}
		c = t.q + "." + x.text(name)
	}
	tps := map[string]*ts.Node{}
	x.typeParamsOf(m, tps)
	mf := f
	if len(tps) > 0 {
		mf = f.push(&frame{typeParams: tps})
	}
	x.walkModifiers(m, mf, c, depth)
	x.walkTypeParams(m, mf, c, depth)
	if ty := x.field(m, "type"); ty != nil {
		x.walkType(ty, mf, c, reference.KindTypeUse, depth+1)
	}
	body := x.field(m, "body")
	pf := mf.push(&frame{method: true, localNames: x.localNames(body)})
	for _, p := range x.named(x.field(m, "parameters")) {
		x.declareParam(p, pf, c, depth)
	}
	if t.jkind == "record" && x.typ(m) == "compact_constructor_declaration" {
		// The components are the compact constructor's parameters.
		for _, comp := range x.named(x.field(t.node, "parameters")) {
			if name := x.field(comp, "name"); name != nil && x.typ(comp) == "formal_parameter" {
				pf.declare(x.text(name), &localDecl{pos: 0, typeLookup: x.typeLookup(x.field(comp, "type"), bodyFrame(t), 0), typeText: x.typeText(x.field(comp, "type"))})
			}
		}
	}
	if th := x.child(m, "throws"); th != nil {
		for _, ty := range x.named(th) {
			x.walkType(ty, mf, c, reference.KindTypeUse, depth+1)
		}
	}
	if v := x.field(m, "value"); v != nil {
		x.walkExpr(v, mf, c, depth+1) // an annotation element's default
	}
	if body != nil {
		if body.HasError() {
			return // a local, a declaration may be lost: nothing in it is trusted
		}
		x.walkStmts(x.named(body), pf, c, t, depth+1)
	}
}

// declareParam declares a formal parameter in frame f, stating the uses of
// its type.
func (x *extractor) declareParam(p *ts.Node, f *frame, c string, depth int) {
	switch x.typ(p) {
	case "formal_parameter":
		ty := x.field(p, "type")
		x.walkModifiers(p, f, c, depth)
		x.walkType(ty, f, c, reference.KindTypeUse, depth+1)
		if name := x.field(p, "name"); name != nil {
			d := &localDecl{pos: 0}
			if x.field(p, "dimensions") == nil {
				d.typeLookup, d.typeText = x.typeLookup(ty, f, p.StartByte()), x.typeText(ty)
			}
			f.declare(x.text(name), d)
		}
	case "spread_parameter":
		x.walkModifiers(p, f, c, depth)
		if ty := x.firstType(p); ty != nil {
			x.walkType(ty, f, c, reference.KindTypeUse, depth+1)
		}
		if vd := x.child(p, "variable_declarator"); vd != nil {
			if name := x.field(vd, "name"); name != nil {
				f.declare(x.text(name), &localDecl{pos: 0}) // an array
			}
		}
	case "receiver_parameter":
		if ty := x.firstType(p); ty != nil {
			x.walkType(ty, f, c, reference.KindTypeUse, depth+1)
		}
	}
}

// localNames collects every name a body declares as a local, wherever.
func (x *extractor) localNames(body *ts.Node) map[string]bool {
	out := map[string]bool{}
	var walk func(n *ts.Node, depth int)
	walk = func(n *ts.Node, depth int) {
		if n == nil || depth > maxDepth {
			return
		}
		switch x.typ(n) {
		case "variable_declarator", "formal_parameter", "catch_formal_parameter", "resource", "enhanced_for_statement", "instanceof_expression":
			if name := x.field(n, "name"); name != nil && x.typ(name) == "identifier" {
				out[x.text(name)] = true
			}
		case "inferred_parameters":
			for _, ch := range x.named(n) {
				out[x.text(ch)] = true
			}
		case "lambda_expression":
			if p := x.field(n, "parameters"); p != nil && x.typ(p) == "identifier" {
				out[x.text(p)] = true
			}
		case "type_pattern", "record_pattern_component":
			for _, ch := range x.named(n) {
				if x.typ(ch) == "identifier" {
					out[x.text(ch)] = true
				}
			}
		}
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i), depth+1)
		}
	}
	walk(body, 0)
	return out
}

// walkBody states the references of a lambda body, an initializer, a field
// initializer: block lambda bodies and initializer blocks are statements.
func (x *extractor) walkBody(n *ts.Node, f *frame, c string, depth int, block bool) {
	if n.HasError() {
		return
	}
	pf := f.push(&frame{method: true, localNames: x.localNames(n)})
	if block || x.typ(n) == "block" {
		x.walkStmts(x.named(n), pf, c, innermostType(f), depth+1)
		return
	}
	x.walkExpr(n, pf, c, depth+1)
}

// walkStmts states the references of a block's statements, in one scope.
func (x *extractor) walkStmts(stmts []*ts.Node, f *frame, c string, t *typeInfo, depth int) {
	bf := f.push(&frame{})
	for _, s := range stmts {
		x.walkStmt(s, bf, c, depth)
	}
}

// walkStmt states the references of statement n in block frame f.
func (x *extractor) walkStmt(n *ts.Node, f *frame, c string, depth int) {
	if x.tooDeep(n, depth) {
		return
	}
	switch x.typ(n) {
	case "block":
		x.walkStmts(x.named(n), f, c, nil, depth+1)
	case "local_variable_declaration":
		x.localVariables(n, f, c, depth)
	case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration":
		lt := x.collectLocal(n, f, c, depth)
		if lt != nil {
			f.declare(lt.name, &localDecl{pos: n.StartByte(), class: lt})
			x.walkTypeRefs(lt, depth+1)
		}
	case "expression_statement":
		for _, e := range x.named(n) {
			x.walkExpr(e, f, c, depth+1)
		}
	case "if_statement":
		cond := x.field(n, "condition")
		x.walkExpr(cond, f, c, depth+1)
		if cons := x.field(n, "consequence"); cons != nil {
			x.walkStmt(cons, x.withBindings(cond, f, c), c, depth+1)
		}
		if alt := x.field(n, "alternative"); alt != nil {
			x.walkStmt(alt, f.push(&frame{}), c, depth+1)
		}
	case "while_statement":
		cond := x.field(n, "condition")
		x.walkExpr(cond, f, c, depth+1)
		if body := x.field(n, "body"); body != nil {
			x.walkStmt(body, x.withBindings(cond, f, c), c, depth+1)
		}
	case "do_statement":
		if body := x.field(n, "body"); body != nil {
			x.walkStmt(body, f.push(&frame{}), c, depth+1)
		}
		x.walkExpr(x.field(n, "condition"), f, c, depth+1)
	case "for_statement":
		ff := f.push(&frame{})
		for i := 0; i < n.ChildCount(); i++ {
			ch := n.Child(i)
			if !ch.IsNamed() {
				continue
			}
			switch {
			case x.typ(ch) == "local_variable_declaration":
				x.localVariables(ch, ff, c, depth+1)
			case ch == x.field(n, "body"):
				x.walkStmt(ch, ff.push(&frame{}), c, depth+1)
			default:
				x.walkExpr(ch, ff, c, depth+1)
			}
		}
	case "enhanced_for_statement":
		x.walkModifiers(n, f, c, depth)
		ty := x.field(n, "type")
		x.walkType(ty, f, c, reference.KindTypeUse, depth+1)
		x.walkExpr(x.field(n, "value"), f, c, depth+1)
		ff := f.push(&frame{})
		if name := x.field(n, "name"); name != nil {
			d := &localDecl{pos: name.StartByte()}
			if x.field(n, "dimensions") == nil {
				d.typeLookup, d.typeText = x.typeLookup(ty, f, ty.StartByte()), x.typeText(ty)
			}
			ff.declare(x.text(name), d)
		}
		if body := x.field(n, "body"); body != nil {
			x.walkStmt(body, ff, c, depth+1)
		}
	case "labeled_statement":
		for _, ch := range x.named(n) {
			if x.typ(ch) != "identifier" {
				x.walkStmt(ch, f, c, depth+1)
			}
		}
	case "return_statement", "throw_statement", "yield_statement", "assert_statement":
		for _, e := range x.named(n) {
			x.walkExpr(e, f, c, depth+1)
		}
	case "synchronized_statement":
		for _, ch := range x.named(n) {
			if x.typ(ch) == "block" {
				x.walkStmt(ch, f, c, depth+1)
			} else {
				x.walkExpr(ch, f, c, depth+1)
			}
		}
	case "try_statement", "try_with_resources_statement":
		tf := f.push(&frame{})
		if rs := x.field(n, "resources"); rs != nil {
			for _, r := range x.named(rs) {
				x.resource(r, tf, c, depth+1)
			}
		}
		for _, ch := range x.named(n) {
			switch x.typ(ch) {
			case "block":
				x.walkStmt(ch, tf, c, depth+1)
			case "catch_clause":
				x.catchClause(ch, f, c, depth+1)
			case "finally_clause":
				for _, b := range x.named(ch) {
					x.walkStmt(b, f, c, depth+1)
				}
			}
		}
	case "switch_expression":
		x.walkExpr(n, f, c, depth+1)
	case "explicit_constructor_invocation":
		x.explicitConstructor(n, f, c, depth+1)
	}
}

// localVariables declares the variables of a local variable declaration,
// each from its declarator on (its own initializer included), stating the
// uses of the type and the references of the initializers.
func (x *extractor) localVariables(n *ts.Node, f *frame, c string, depth int) {
	x.walkModifiers(n, f, c, depth)
	ty := x.field(n, "type")
	x.walkType(ty, f, c, reference.KindTypeUse, depth+1)
	for _, d := range x.named(n) {
		if x.typ(d) != "variable_declarator" {
			continue
		}
		name := x.field(d, "name")
		if name == nil {
			continue
		}
		decl := &localDecl{pos: d.StartByte()}
		value := x.field(d, "value")
		if x.field(d, "dimensions") == nil {
			if x.typ(ty) == "type_identifier" && x.text(ty) == "var" {
				// var: the type of a class instance creation without a body.
				if value != nil && x.typ(value) == "object_creation_expression" && x.child(value, "class_body") == nil && !x.qualifiedCreation(value) {
					if vt := x.field(value, "type"); vt != nil {
						decl.typeLookup, decl.typeText = x.typeLookup(vt, f, vt.StartByte()), x.typeText(vt)
					}
				}
			} else {
				decl.typeLookup, decl.typeText = x.typeLookup(ty, f, ty.StartByte()), x.typeText(ty)
			}
		}
		f.declare(x.text(name), decl)
		if value != nil {
			x.walkExpr(value, f, c, depth+1)
		}
	}
}

func (x *extractor) resource(r *ts.Node, f *frame, c string, depth int) {
	if x.typ(r) != "resource" {
		x.walkExpr(r, f, c, depth)
		return
	}
	ty := x.field(r, "type")
	name := x.field(r, "name")
	value := x.field(r, "value")
	if ty == nil || name == nil {
		for _, e := range x.named(r) {
			x.walkExpr(e, f, c, depth)
		}
		return
	}
	x.walkModifiers(r, f, c, depth)
	x.walkType(ty, f, c, reference.KindTypeUse, depth+1)
	d := &localDecl{pos: r.StartByte()}
	if !(x.typ(ty) == "type_identifier" && x.text(ty) == "var") {
		d.typeLookup, d.typeText = x.typeLookup(ty, f, ty.StartByte()), x.typeText(ty)
	}
	f.declare(x.text(name), d)
	if value != nil {
		x.walkExpr(value, f, c, depth+1)
	}
}

func (x *extractor) catchClause(n *ts.Node, f *frame, c string, depth int) {
	cf := f.push(&frame{})
	for _, ch := range x.named(n) {
		switch x.typ(ch) {
		case "catch_formal_parameter":
			x.walkModifiers(ch, cf, c, depth)
			var types []*ts.Node
			if ct := x.child(ch, "catch_type"); ct != nil {
				types = x.named(ct)
			}
			for _, ty := range types {
				x.walkType(ty, cf, c, reference.KindTypeUse, depth+1)
			}
			if name := x.field(ch, "name"); name != nil {
				d := &localDecl{pos: 0}
				if len(types) == 1 {
					d.typeLookup, d.typeText = x.typeLookup(types[0], f, types[0].StartByte()), x.typeText(types[0])
				}
				cf.declare(x.text(name), d)
			}
		case "block":
			x.walkStmt(ch, cf, c, depth+1)
		}
	}
}

// withBindings returns the scope of what follows when cond is true: its
// pattern bindings, typed (JLS 6.3.1); f itself when it has none.
func (x *extractor) withBindings(cond *ts.Node, f *frame, c string) *frame {
	bf := f.push(&frame{})
	x.trueBindings(cond, bf, 0)
	return bf
}

// trueBindings declares in f the bindings expression e introduces when true.
func (x *extractor) trueBindings(e *ts.Node, f *frame, depth int) {
	if e == nil || depth > maxDepth {
		return
	}
	switch x.typ(e) {
	case "parenthesized_expression":
		for _, ch := range x.named(e) {
			x.trueBindings(ch, f, depth+1)
		}
	case "binary_expression":
		if op := x.operator(e); op == "&&" {
			x.trueBindings(x.field(e, "left"), f, depth+1)
			x.trueBindings(x.field(e, "right"), f, depth+1)
		}
	case "instanceof_expression":
		name := x.field(e, "name")
		ty := x.field(e, "right")
		if name != nil && ty != nil {
			f.declare(x.text(name), &localDecl{pos: 0, typeLookup: x.typeLookup(ty, f, ty.StartByte()), typeText: x.typeText(ty)})
		}
	}
}

// operator is a binary expression's operator token.
func (x *extractor) operator(e *ts.Node) string {
	for i := 0; i < e.ChildCount(); i++ {
		if ch := e.Child(i); !ch.IsNamed() {
			return x.typ(ch)
		}
	}
	return ""
}

// collectLocal states a local class, declared in a body at chain f.
func (x *extractor) collectLocal(n *ts.Node, f *frame, c string, depth int) *typeInfo {
	return x.collectTypeIn(n, nil, f, c+".", c, depth, true)
}

// anonymous states an anonymous class (body) whose supertype the lookup
// super names (an enum constant's body: the enum), declared at chain f in
// container c, and its references.
func (x *extractor) anonymous(body *ts.Node, super, superText string, f *frame, c string, at *ts.Node, depth int) *typeInfo {
	outer := innermostType(f)
	base := c
	if outer != nil {
		base = outer.q
	}
	t := &typeInfo{
		q: x.localIdentity(base + ".<anonymous>"), name: "<anonymous>", kind: symbol.KindClass, jkind: "anonymous",
		node: body, body: body, methods: map[string]bool{}, fields: map[string]*ts.Node{}, memberTypes: map[string]*typeInfo{},
		typeParams: map[string]*ts.Node{}, implicitName: map[string]bool{}, supers: super != "",
	}
	t.header = f.push(&frame{typeParams: t.typeParams})
	sym := language.SymbolDraft{Name: "<anonymous>", Qualified: t.q, Kind: symbol.KindClass, Location: x.loc(body),
		StartByte: body.StartByte(), EndByte: body.EndByte(), Parent: c}
	if x.outsideBodiesBroken(body) {
		t.broken = true
		sym.ConfidenceCap = "candidate"
	}
	idx := len(x.symbols)
	x.symbols = append(x.symbols, sym)
	x.collectMembers(t, depth)
	t.implicit = objectMembers // its superclass may be java.lang.Object (a supertype that is an interface)
	t.implicitName = implicitNames(t.implicit)
	x.symbols[idx].ImplicitMembers = t.implicit
	if super != "" {
		x.ref(language.ReferenceDraft{Name: lastSegment(superText), Kind: string(reference.KindInheritance), Container: t.q, Lookup: super}, at)
	}
	x.walkMembers(t, body, bodyFrame(t), depth+1)
	return t
}

// explicitConstructor states this(...) or super(...).
func (x *extractor) explicitConstructor(n *ts.Node, f *frame, c string, depth int) {
	args := x.field(n, "arguments")
	x.walkExprs(args, f, c, depth+1)
	t := innermostType(f)
	ctor := x.field(n, "constructor")
	if t == nil || ctor == nil || x.field(n, "object") != nil {
		return
	}
	r := language.ReferenceDraft{Name: "<init>", Kind: string(reference.KindCall), Container: c, IsCall: true,
		CountsArguments: true, ArgumentCount: len(x.named(args)), ArgumentTypes: x.argumentTypes(args, f), Speculative: true}
	switch x.typ(ctor) {
	case "this":
		r.Lookup = "@C:" + t.q + ".<init>"
	case "super":
		if t.superLookup == "" {
			return
		}
		r.ReceiverExpr, r.ReceiverLookup = "super", t.superLookup
	default:
		return
	}
	x.ref(r, ctor)
}

// walkExprs walks the named children of n as expressions.
func (x *extractor) walkExprs(n *ts.Node, f *frame, c string, depth int) {
	for _, e := range x.named(n) {
		x.walkExpr(e, f, c, depth)
	}
}

// walkExpr states the references of expression e.
func (x *extractor) walkExpr(e *ts.Node, f *frame, c string, depth int) {
	if e == nil || x.tooDeep(e, depth) {
		return
	}
	switch x.typ(e) {
	case "identifier":
		x.valueRef(e, f, c, reference.KindValueReference)
	case "field_access":
		x.fieldAccess(e, f, c, reference.KindValueReference, depth)
	case "method_invocation":
		x.call(e, f, c, depth)
	case "object_creation_expression":
		x.creation(e, f, c, depth)
	case "method_reference":
		x.methodReference(e, f, c, depth)
	case "lambda_expression":
		x.lambda(e, f, c, depth)
	case "assignment_expression":
		left := x.field(e, "left")
		switch x.typ(left) {
		case "identifier":
			x.valueRef(left, f, c, reference.KindWrite)
		case "field_access":
			x.fieldAccess(left, f, c, reference.KindWrite, depth)
		default:
			x.walkExpr(left, f, c, depth+1)
		}
		x.walkExpr(x.field(e, "right"), f, c, depth+1)
	case "cast_expression":
		for _, ch := range x.named(e) {
			if ch == x.field(e, "value") {
				x.walkExpr(ch, f, c, depth+1)
			} else {
				x.walkType(ch, f, c, reference.KindTypeUse, depth+1)
			}
		}
	case "instanceof_expression":
		x.walkExpr(x.field(e, "left"), f, c, depth+1)
		if ty := x.field(e, "right"); ty != nil {
			x.walkType(ty, f, c, reference.KindTypeUse, depth+1)
		}
		if name := x.field(e, "name"); name != nil {
			f.declare(x.text(name), &localDecl{pos: name.StartByte()}) // in scope where true: here untyped
		}
		if p := x.field(e, "pattern"); p != nil {
			x.pattern(p, f, c, depth+1)
		}
	case "binary_expression":
		left, right := x.field(e, "left"), x.field(e, "right")
		x.walkExpr(left, f, c, depth+1)
		if x.operator(e) == "&&" {
			x.walkExpr(right, x.withBindings(left, f, c), c, depth+1)
		} else {
			x.walkExpr(right, f, c, depth+1)
		}
	case "class_literal":
		for _, ch := range x.named(e) {
			x.walkType(ch, f, c, reference.KindTypeUse, depth+1)
		}
	case "array_creation_expression":
		for _, ch := range x.named(e) {
			if ch == x.field(e, "type") {
				x.walkType(ch, f, c, reference.KindTypeUse, depth+1)
			} else {
				x.walkExpr(ch, f, c, depth+1)
			}
		}
	case "switch_expression":
		x.walkExpr(x.field(e, "condition"), f, c, depth+1)
		x.switchBlock(x.field(e, "body"), f, c, depth+1)
	case "unary_expression", "parenthesized_expression", "ternary_expression", "array_access", "argument_list",
		"array_initializer", "element_value_array_initializer", "template_expression", "string_interpolation",
		"update_expression", "dimensions_expr", "string_literal":
		x.walkExprs(e, f, c, depth+1)
	case "annotation", "marker_annotation":
		x.annotation(e, f, c, depth+1)
	}
}

// pattern states a pattern's type uses and declares its bindings, untyped,
// in f.
func (x *extractor) pattern(p *ts.Node, f *frame, c string, depth int) {
	if p == nil || depth > maxDepth {
		return
	}
	for _, ch := range x.named(p) {
		switch x.typ(ch) {
		case "identifier":
			f.declare(x.text(ch), &localDecl{pos: ch.StartByte()})
		case "type_pattern", "record_pattern", "record_pattern_body", "record_pattern_component", "pattern":
			x.pattern(ch, f, c, depth+1)
		default:
			if isTypeNode(x.typ(ch)) {
				x.walkType(ch, f, c, reference.KindTypeUse, depth+1)
			}
		}
	}
	if x.typ(p) == "record_pattern" {
		// The record type is written as an identifier.
		if id := x.child(p, "identifier"); id != nil {
			x.typeRef(id, x.text(id), f, c, reference.KindTypeUse)
		}
	}
}

// switchBlock states a switch body's references: a label's bare
// identifiers are not resolved (the selector's type decides them).
func (x *extractor) switchBlock(body *ts.Node, f *frame, c string, depth int) {
	group := f.push(&frame{})
	for _, ch := range x.named(body) {
		switch x.typ(ch) {
		case "switch_rule":
			rf := f.push(&frame{})
			for _, part := range x.named(ch) {
				switch x.typ(part) {
				case "switch_label":
					x.switchLabel(part, rf, c, depth+1)
				case "expression_statement", "throw_statement":
					x.walkStmt(part, rf, c, depth+1)
				case "block":
					x.walkStmt(part, rf, c, depth+1)
				}
			}
		case "switch_block_statement_group":
			for _, part := range x.named(ch) {
				if x.typ(part) == "switch_label" {
					x.switchLabel(part, group, c, depth+1)
				} else {
					x.walkStmt(part, group, c, depth+1)
				}
			}
		}
	}
}

func (x *extractor) switchLabel(l *ts.Node, f *frame, c string, depth int) {
	for _, ch := range x.named(l) {
		switch x.typ(ch) {
		case "identifier":
			// an enum constant, or a constant the selector's type decides
		case "pattern":
			x.patternTyped(ch, f, c, depth)
		case "guard":
			x.walkExprs(ch, f, c, depth+1)
		default:
			x.walkExpr(ch, f, c, depth+1)
		}
	}
}

// patternTyped declares a switch label's type pattern binding typed, its
// record pattern components untyped.
func (x *extractor) patternTyped(p *ts.Node, f *frame, c string, depth int) {
	for _, ch := range x.named(p) {
		if x.typ(ch) == "type_pattern" {
			var ty, name *ts.Node
			for _, part := range x.named(ch) {
				if isTypeNode(x.typ(part)) {
					ty = part
				} else if x.typ(part) == "identifier" {
					name = part
				}
			}
			if ty != nil {
				x.walkType(ty, f, c, reference.KindTypeUse, depth+1)
			}
			if name != nil {
				d := &localDecl{pos: 0}
				if ty != nil {
					d.typeLookup, d.typeText = x.typeLookup(ty, f, ty.StartByte()), x.typeText(ty)
				}
				f.declare(x.text(name), d)
			}
			continue
		}
		x.pattern(ch, f, c, depth)
	}
}

// lambda states a lambda's references in a scope of its parameters.
func (x *extractor) lambda(e *ts.Node, f *frame, c string, depth int) {
	body := x.field(e, "body")
	if body == nil || body.HasError() {
		return
	}
	lf := f.push(&frame{method: true, localNames: x.localNames(e)})
	switch p := x.field(e, "parameters"); x.typ(p) {
	case "identifier":
		lf.declare(x.text(p), &localDecl{pos: 0})
	case "inferred_parameters":
		for _, id := range x.named(p) {
			lf.declare(x.text(id), &localDecl{pos: 0})
		}
	case "formal_parameters":
		for _, fp := range x.named(p) {
			x.declareParam(fp, lf, c, depth)
		}
	}
	if x.typ(body) == "block" {
		x.walkStmts(x.named(body), lf, c, nil, depth+1)
	} else {
		x.walkExpr(body, lf, c, depth+1)
	}
}

// annotation states an annotation's type use and its values' references
// (not its element names, which its type declares).
func (x *extractor) annotation(a *ts.Node, f *frame, c string, depth int) {
	if name := x.field(a, "name"); name != nil {
		switch x.typ(name) {
		case "identifier":
			x.typeRef(name, x.text(name), f, c, reference.KindTypeUse)
		case "scoped_identifier":
			if segs := strings.Split(x.dotted(name), "."); len(segs) > 1 && segs[0] != "" {
				at := name
				if last := x.field(name, "name"); last != nil {
					at = last
				}
				x.ref(language.ReferenceDraft{Name: segs[len(segs)-1], Kind: string(reference.KindTypeUse), Container: c,
					Lookup: strings.Join(x.qualifiedTypeSteps(segs, f, name.StartByte()), ";")}, at)
			}
		}
	}
	for _, arg := range x.named(x.field(a, "arguments")) {
		if x.typ(arg) == "element_value_pair" {
			x.walkExpr(x.field(arg, "value"), f, c, depth+1)
		} else {
			x.walkExpr(arg, f, c, depth+1)
		}
	}
}

// walkModifiers states the annotations of declaration n's modifiers.
func (x *extractor) walkModifiers(n *ts.Node, f *frame, c string, depth int) {
	for _, a := range x.named(x.child(n, "modifiers")) {
		x.annotation(a, f, c, depth+1)
	}
}

// walkTypeParams states the uses of declaration n's type parameter bounds.
func (x *extractor) walkTypeParams(n *ts.Node, f *frame, c string, depth int) {
	tps := x.field(n, "type_parameters")
	if tps == nil {
		tps = x.child(n, "type_parameters")
	}
	for _, tp := range x.named(tps) {
		for _, ch := range x.named(tp) {
			switch x.typ(ch) {
			case "type_bound":
				for _, b := range x.named(ch) {
					x.walkType(b, f, c, reference.KindTypeUse, depth+1)
				}
			case "annotation", "marker_annotation":
				x.annotation(ch, f, c, depth+1)
			}
		}
	}
}

// walkType states the uses of the types type node ty names.
func (x *extractor) walkType(ty *ts.Node, f *frame, c string, kind reference.ReferenceKind, depth int) {
	if ty == nil || x.tooDeep(ty, depth) {
		return
	}
	switch x.typ(ty) {
	case "type_identifier":
		x.typeRef(ty, x.text(ty), f, c, kind)
	case "scoped_type_identifier":
		segs := x.scopedSegments(ty)
		if len(segs) > 1 {
			x.ref(language.ReferenceDraft{Name: segs[len(segs)-1], Kind: string(kind), Container: c,
				Lookup: strings.Join(x.qualifiedTypeSteps(segs, f, ty.StartByte()), ";")}, x.lastName(ty))
			// Its outer names are types too — or packages (Speculative).
			if nodes := x.typeNameNodes(ty); len(nodes) == len(segs) {
				for k := 1; k < len(segs); k++ {
					var steps []string
					if k == 1 {
						if l := x.typeSteps(segs[0], f, ty.StartByte()); !l.typeVar {
							steps = l.steps
						}
					} else {
						steps = x.qualifiedTypeSteps(segs[:k], f, ty.StartByte())
					}
					if len(steps) > 0 {
						x.ref(language.ReferenceDraft{Name: segs[k-1], Kind: string(reference.KindTypeUse), Container: c, Speculative: true,
							Lookup: strings.Join(steps, ";"), TargetKinds: typeKinds}, nodes[k-1])
					}
				}
			}
		}
		for _, ch := range x.named(ty) {
			if x.typ(ch) == "annotation" || x.typ(ch) == "marker_annotation" {
				x.annotation(ch, f, c, depth+1)
			}
		}
	case "generic_type", "array_type", "annotated_type", "type_arguments", "wildcard", "type_list", "type_bound", "catch_type", "dimensions":
		for _, ch := range x.named(ty) {
			switch x.typ(ch) {
			case "annotation", "marker_annotation":
				x.annotation(ch, f, c, depth+1)
			default:
				x.walkType(ch, f, c, kind, depth+1)
			}
		}
	}
}

// typeRef states a use of simple type name name at node n.
func (x *extractor) typeRef(n *ts.Node, name string, f *frame, c string, kind reference.ReferenceKind) {
	if name == "var" {
		return
	}
	l := x.typeSteps(name, f, n.StartByte())
	if l.typeVar || len(l.steps) == 0 {
		return
	}
	x.ref(language.ReferenceDraft{Name: name, Kind: string(kind), Container: c, Lookup: l.String()}, n)
}

// valueRef states a use of simple variable name e: nothing for a local.
func (x *extractor) valueRef(e *ts.Node, f *frame, c string, kind reference.ReferenceKind) {
	l := x.valueSteps(x.text(e), f, e.StartByte())
	if len(l.steps) == 0 || (l.local != nil && len(l.steps) == 1) {
		return // a local, which no scope the repository holds can hide
	}
	x.ref(language.ReferenceDraft{Name: x.text(e), Kind: string(kind), Container: c, Lookup: l.String()}, e)
}

// receiver is what the provider knows of a member access's receiver.
type receiver struct {
	lookup   string // Lookup of the member itself (this.m(), Outer.this.m())
	recv     string // ReceiverLookup
	recvType string // the receiver is a value of this declared type
	broken   bool   // lookup's type has a body the parser rejected
	ok       bool
}

// receiverOf classifies a member access's receiver expression obj.
func (x *extractor) receiverOf(obj *ts.Node, f *frame, c string, depth int) receiver {
	switch x.typ(obj) {
	case "this":
		if t := innermostType(f); t != nil {
			return receiver{lookup: "@%s:" + t.q, broken: t.broken, ok: true}
		}
	case "super":
		if t := innermostType(f); t != nil && t.superLookup != "" {
			return receiver{recv: t.superLookup, recvType: "super", ok: true}
		}
	case "parenthesized_expression":
		if inner := x.named(obj); len(inner) == 1 {
			return x.receiverOf(inner[0], f, c, depth+1)
		}
	case "cast_expression":
		ty := x.field(obj, "type")
		x.walkExpr(obj, f, c, depth+1)
		if lk := x.typeLookup(ty, f, ty.StartByte()); lk != "" {
			return receiver{recv: lk, recvType: x.typeText(ty), ok: true}
		}
		return receiver{}
	case "object_creation_expression":
		x.walkExpr(obj, f, c, depth+1)
		if x.child(obj, "class_body") == nil && !x.qualifiedCreation(obj) {
			if ty := x.field(obj, "type"); ty != nil {
				if lk := x.typeLookup(ty, f, ty.StartByte()); lk != "" {
					return receiver{recv: lk, recvType: x.typeText(ty), ok: true}
				}
			}
		}
		return receiver{}
	case "string_literal":
		return receiver{recv: "=T:java.lang.String", recvType: "String", ok: true}
	case "class_literal":
		x.walkExpr(obj, f, c, depth+1)
		return receiver{recv: "=T:java.lang.Class", recvType: "Class", ok: true}
	case "identifier":
		return x.nameReceiver([]string{x.text(obj)}, []*ts.Node{obj}, f, c)
	case "field_access":
		if field := x.field(obj, "field"); field != nil && x.typ(field) == "this" {
			// Outer.this
			if t := enclosingNamed(f, x.text(x.field(obj, "object"))); t != nil {
				return receiver{lookup: "@%s:" + t.q, broken: t.broken, ok: true}
			}
			return receiver{}
		}
		if inner := x.field(obj, "object"); inner != nil && x.typ(inner) == "this" {
			// this.f: the innermost type's field f
			t := innermostType(f)
			field := x.field(obj, "field")
			if t == nil || field == nil {
				return receiver{}
			}
			x.fieldAccess(obj, f, c, reference.KindValueReference, depth)
			if ty, own := t.fields[x.text(field)]; own && ty != nil {
				if lk := x.typeLookup(ty, bodyFrame(t), ty.StartByte()); lk != "" {
					return receiver{recv: lk, recvType: x.typeText(ty), ok: true}
				}
			}
			return receiver{}
		}
		if segs := x.identifierChain(obj); segs != nil {
			return x.nameReceiver(segs, x.identifierNodes(obj), f, c)
		}
		x.walkExpr(obj, f, c, depth+1)
		return receiver{}
	}
	x.walkExpr(obj, f, c, depth+1)
	return receiver{}
}

// identifierChain returns the identifiers of a.b.c, nil for anything else.
func (x *extractor) identifierChain(n *ts.Node) []string {
	switch x.typ(n) {
	case "identifier":
		return []string{x.text(n)}
	case "field_access":
		head := x.identifierChain(x.field(n, "object"))
		field := x.field(n, "field")
		if head == nil || field == nil || x.typ(field) != "identifier" {
			return nil
		}
		return append(head, x.text(field))
	}
	return nil
}

// identifierNodes returns the identifier nodes of a.b.c in order.
func (x *extractor) identifierNodes(n *ts.Node) []*ts.Node {
	switch x.typ(n) {
	case "identifier":
		return []*ts.Node{n}
	case "field_access":
		return append(x.identifierNodes(x.field(n, "object")), x.field(n, "field"))
	}
	return nil
}

// typeKinds are the symbol kinds a Java type name denotes.
const typeKinds = "class,interface,enum"

// nameReceiver classifies a receiver written as names a.b.c (JLS 6.5.2):
// its head a variable — a local or a field of known declared type, followed
// only when it is the whole receiver —, a type (its members then followed),
// or a package (its types, shortest package first). Each name of a receiver
// whose head is no variable the file fixes is stated twice, as the field
// and as the type it may be (Speculative: the one it is stays).
func (x *extractor) nameReceiver(segs []string, nodes []*ts.Node, f *frame, c string) receiver {
	at := nodes[0]
	head := segs[0]
	vs := x.valueSteps(head, f, at.StartByte())
	before := vs.steps
	if len(before) > 0 {
		before = before[:len(before)-1]
	}
	switch {
	case vs.local != nil:
		if len(segs) == 1 && vs.local.typeLookup != "" {
			return receiver{recv: join(before, vs.local.typeLookup), recvType: vs.local.typeText, ok: true}
		}
		return receiver{recv: join(before, "!"), recvType: "?", ok: true}
	case vs.ownField:
		if len(segs) == 1 && vs.ownFieldType != nil {
			x.valueRef(at, f, c, reference.KindValueReference)
			if lk := x.typeLookup(vs.ownFieldType, bodyFrame(vs.ownFieldIn), vs.ownFieldType.StartByte()); lk != "" {
				return receiver{recv: join(before, lk), recvType: x.typeText(vs.ownFieldType), ok: true}
			}
		}
		return receiver{recv: vs.String(), recvType: "?", ok: true}
	}
	for k := 1; k <= len(segs) && k <= len(nodes); k++ {
		r := language.ReferenceDraft{Name: segs[k-1], Kind: string(reference.KindValueReference), Container: c, Speculative: true}
		if k == 1 {
			r.Lookup = vs.String()
		} else {
			r.ReceiverExpr, r.ReceiverLookup = strings.Join(segs[:k-1], "."), x.nameLookup(segs[:k-1], vs, at.StartByte(), f)
		}
		if r.Lookup != "" || r.ReceiverLookup != "" {
			x.ref(r, nodes[k-1])
		}
		if lk := x.nameLookup(segs[:k], vs, at.StartByte(), f); lk != "" {
			x.ref(language.ReferenceDraft{Name: segs[k-1], Kind: string(reference.KindTypeUse), Container: c, Speculative: true,
				Lookup: lk, TargetKinds: typeKinds}, nodes[k-1])
		}
	}
	lk := x.nameLookup(segs, vs, at.StartByte(), f)
	if lk == "" {
		return receiver{}
	}
	return receiver{recv: lk, ok: true}
}

// nameLookup is the lookup of names a.b.c whose head no scope of the file
// fixes as a variable: its head as a variable vs found, then as a type with
// its members followed, then as a package.
func (x *extractor) nameLookup(segs []string, vs lookupResult, pos uint32, f *frame) string {
	steps := append([]string(nil), vs.steps...)
	ts := x.typeSteps(segs[0], f, pos)
	if !ts.typeVar {
		path := ""
		for _, s := range segs[1:] {
			path += "~" + s
		}
		steps = append(steps, withPath(ts.steps, path)...)
	}
	for k := 2; k <= len(segs); k++ {
		alt := "=T:" + strings.Join(segs[:k], ".")
		for _, s := range segs[k:] {
			alt += "~" + s
		}
		steps = append(steps, alt)
	}
	return strings.Join(steps, ";")
}

func join(steps []string, last string) string {
	if len(steps) == 0 {
		return last
	}
	return strings.Join(steps, ";") + ";" + last
}

// call states a method invocation.
func (x *extractor) call(e *ts.Node, f *frame, c string, depth int) {
	name := x.field(e, "name")
	args := x.field(e, "arguments")
	if ta := x.field(e, "type_arguments"); ta != nil {
		x.walkType(ta, f, c, reference.KindTypeUse, depth+1)
	}
	obj := x.field(e, "object")
	if name == nil || x.typ(name) != "identifier" {
		x.walkExpr(obj, f, c, depth+1)
		x.walkExprs(args, f, c, depth+1)
		return
	}
	r := language.ReferenceDraft{Name: x.text(name), Kind: string(reference.KindCall), Container: c, IsCall: true,
		CountsArguments: true, ArgumentCount: len(x.named(args)), ArgumentTypes: x.argumentTypes(args, f)}
	switch {
	case obj == nil:
		r.Lookup = x.methodSteps(r.Name, f).String()
	case x.hasToken(e, "super") || (x.child(e, "super") != nil && x.child(e, "super") != obj):
		// X.super.m(): an enclosing class's superclass, or an interface's.
		r.ReceiverExpr = x.text(obj) + ".super"
		if t := enclosingNamed(f, x.text(obj)); t != nil {
			r.ReceiverLookup = t.superLookup
		} else if x.typ(obj) == "identifier" {
			r.ReceiverLookup = x.typeSteps(x.text(obj), f, obj.StartByte()).String()
		}
		r.ReceiverType = "super"
	default:
		r.ReceiverExpr = strings.Join(strings.Fields(x.text(obj)), "")
		rc := x.receiverOf(obj, f, c, depth)
		x.applyReceiver(&r, rc, 'M')
	}
	x.walkExprs(args, f, c, depth+1)
	if r.Lookup == "" && r.ReceiverLookup == "" && r.ReceiverExpr == "" {
		return // no scope has the name: nothing it could denote
	}
	x.ref(r, name)
}

// applyReceiver puts what receiverOf found on r, whose member is of class
// class.
func (x *extractor) applyReceiver(r *language.ReferenceDraft, rc receiver, class byte) {
	switch {
	case !rc.ok:
	case rc.lookup != "":
		r.Lookup = strings.Replace(rc.lookup, "%s", string(class), 1) + "." + r.Name
		if rc.broken {
			r.Lookup += "|?"
		}
	default:
		r.ReceiverLookup, r.ReceiverType = rc.recv, rc.recvType
		if r.ReceiverType == "?" {
			r.ReceiverType = r.ReceiverExpr
		}
	}
}

// fieldAccess states a field access obj.name.
func (x *extractor) fieldAccess(e *ts.Node, f *frame, c string, kind reference.ReferenceKind, depth int) {
	obj, field := x.field(e, "object"), x.field(e, "field")
	if field == nil || x.typ(field) != "identifier" {
		// Outer.this, a.super: no member
		return
	}
	r := language.ReferenceDraft{Name: x.text(field), Kind: string(kind), Container: c}
	r.ReceiverExpr = strings.Join(strings.Fields(x.text(obj)), "")
	if x.hasToken(e, "super") {
		return // Outer.super.f: rare, not followed
	}
	rc := x.receiverOf(obj, f, c, depth)
	x.applyReceiver(&r, rc, 'V')
	x.ref(r, field)
}

// qualifiedCreation reports whether a class instance creation is qualified
// (outer.new Inner()).
func (x *extractor) qualifiedCreation(e *ts.Node) bool {
	for _, ch := range x.named(e) {
		if ch == x.field(e, "type") {
			return false
		}
		if !isTypeNode(x.typ(ch)) && x.typ(ch) != "type_arguments" && x.typ(ch) != "marker_annotation" && x.typ(ch) != "annotation" {
			return true
		}
	}
	return false
}

// creation states a class instance creation: the type constructed, its
// constructor, and an anonymous class body.
func (x *extractor) creation(e *ts.Node, f *frame, c string, depth int) {
	args := x.field(e, "arguments")
	ty := x.field(e, "type")
	if x.qualifiedCreation(e) {
		for _, ch := range x.named(e) {
			if ch != ty && x.typ(ch) != "class_body" {
				x.walkExpr(ch, f, c, depth+1)
			}
		}
		return
	}
	if ta := x.child(e, "type_arguments"); ta != nil {
		x.walkType(ta, f, c, reference.KindTypeUse, depth+1)
	}
	base := ty
	if ty != nil && x.typ(ty) == "generic_type" {
		base = x.firstType(ty)
		for _, ta := range x.named(x.child(ty, "type_arguments")) {
			x.walkType(ta, f, c, reference.KindTypeUse, depth+1)
		}
	}
	x.walkExprs(args, f, c, depth+1)
	if base == nil {
		return
	}
	steps, ok := x.typeNameSteps(base, f, base.StartByte())
	if !ok || len(steps) == 0 {
		return
	}
	lookup := strings.Join(steps, ";")
	name := lastSegment(x.typeText(base))
	at := x.lastName(base)
	x.ref(language.ReferenceDraft{Name: name, Kind: string(reference.KindConstruction), Container: c, Lookup: lookup}, at)
	body := x.child(e, "class_body")
	if body == nil {
		// An anonymous class's own constructor is the language's: the
		// supertype's it invokes is not followed.
		x.ref(language.ReferenceDraft{Name: "<init>", Kind: string(reference.KindCall), Container: c, IsCall: true,
			ReceiverExpr: name, ReceiverLookup: lookup, CountsArguments: true, ArgumentCount: len(x.named(args)),
			ArgumentTypes: x.argumentTypes(args, f), Speculative: true}, at)
		return
	}
	x.anonymous(body, lookup, x.typeText(base), f, c, at, depth+1)
}

// methodReference states X::m or X::new.
func (x *extractor) methodReference(e *ts.Node, f *frame, c string, depth int) {
	parts := x.named(e)
	if len(parts) == 0 {
		return
	}
	recv := parts[0]
	if x.hasToken(e, "new") {
		if isTypeNode(x.typ(recv)) || x.typ(recv) == "identifier" || x.typ(recv) == "scoped_identifier" || x.typ(recv) == "field_access" {
			var steps []string
			ok := false
			switch x.typ(recv) {
			case "identifier":
				l := x.typeSteps(x.text(recv), f, recv.StartByte())
				steps, ok = l.steps, !l.typeVar
			case "field_access":
				if segs := x.identifierChain(recv); segs != nil {
					steps, ok = x.qualifiedTypeSteps(segs, f, recv.StartByte()), true
				}
			default:
				steps, ok = x.typeNameSteps(recv, f, recv.StartByte())
			}
			if ok && len(steps) > 0 {
				at := x.lastName(recv)
				if x.typ(recv) == "field_access" {
					if last := x.field(recv, "field"); last != nil {
						at = last
					}
				}
				x.ref(language.ReferenceDraft{Name: lastSegment(x.typeText(recv)), Kind: string(reference.KindConstruction), Container: c, Lookup: strings.Join(steps, ";")}, at)
			}
		}
		return
	}
	if len(parts) < 2 || x.typ(parts[len(parts)-1]) != "identifier" {
		return
	}
	name := parts[len(parts)-1]
	r := language.ReferenceDraft{Name: x.text(name), Kind: string(reference.KindCall), Container: c, IsCall: true,
		ReceiverExpr: strings.Join(strings.Fields(x.text(recv)), "") + "::"}
	var rc receiver
	switch x.typ(recv) {
	case "generic_type", "scoped_type_identifier", "type_identifier":
		if steps, ok := x.typeNameSteps(recv, f, recv.StartByte()); ok {
			rc = receiver{recv: strings.Join(steps, ";"), ok: true}
		}
	default:
		rc = x.receiverOf(recv, f, c, depth)
	}
	x.applyReceiver(&r, rc, 'M')
	if r.Lookup == "" && r.ReceiverLookup == "" {
		return
	}
	x.ref(r, name)
}
