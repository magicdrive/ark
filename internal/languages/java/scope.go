package java

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"
)

// frame is one scope of the chain a name is looked up in, innermost first:
// a type's body (its members), a set of type parameters, or a block scope
// (local variables, parameters, pattern bindings, local classes). A block
// scope's declarations are visible from their position on.
type frame struct {
	parent *frame

	typ          *typeInfo
	typeParams   map[string]*ts.Node
	typeParamPos uint32

	locals map[string][]*localDecl
	// method: the frame is a method's, constructor's, lambda's or
	// initializer's outermost one (localNames below it are its body's).
	method     bool
	localNames map[string]bool // every local name of the body, wherever declared
}

// cuFrame is the compilation unit: the end of every chain.
var cuFrame *frame

// push returns f with parent p.
func (p *frame) push(f *frame) *frame {
	f.parent = p
	return f
}

// localDecl is one block-scope declaration.
type localDecl struct {
	pos   uint32
	scope *frame    // the scope its declared type is read in (nil: not recorded)
	class *typeInfo // a local class
	// A variable: its declared type's lookup as a receiver type and its
	// text ("" when not known: var, a lambda parameter, an array).
	typeLookup, typeText string
}

func (f *frame) declare(name string, d *localDecl) {
	if d.scope == nil {
		d.scope = f
	}
	if f.locals == nil {
		f.locals = map[string][]*localDecl{}
	}
	f.locals[name] = append(f.locals[name], d)
}

// visible returns the declaration of name in block frame f visible at pos.
func (f *frame) visible(name string, pos uint32, wantClass bool) *localDecl {
	var found *localDecl
	for _, d := range f.locals[name] {
		if d.pos <= pos && (d.class != nil) == wantClass {
			found = d
		}
	}
	return found
}

// lookupResult is a name's lookup: steps for the resolver, or what the file
// fixes alone.
type lookupResult struct {
	steps []string
	// local: the name is a local variable (nothing Ark represents), with
	// its declared type's lookup when known.
	local *localDecl
	// typeVar: the name is a type parameter (its bound, or nil).
	typeVar      bool
	typeVarBound *ts.Node
	typeVarScope *frame // where the type parameter is declared: its bound is read there
	// ownField: the innermost type with the name declares it (its type
	// node, nil when not followed).
	ownField     bool
	ownFieldType *ts.Node
	ownFieldIn   *typeInfo
}

func (l lookupResult) String() string { return strings.Join(l.steps, ";") }

// typeSteps is the lookup of a simple type name at pos in chain f (JLS
// 6.4.1, 6.5.5.1).
func (x *extractor) typeSteps(name string, f *frame, pos uint32) lookupResult {
	var steps []string
	for g := f; g != nil; g = g.parent {
		if d := g.visible(name, pos, true); d != nil {
			return lookupResult{steps: append(steps, "=T:"+d.class.q)}
		}
		if bound, ok := g.typeParams[name]; ok {
			return lookupResult{steps: steps, typeVar: true, typeVarBound: bound, typeVarScope: g}
		}
		if g.typ != nil {
			if mt := g.typ.memberTypes[name]; mt != nil {
				return lookupResult{steps: append(steps, "=T:"+mt.q)}
			}
			if s := inheritedStep('T', g.typ, name); s != "" {
				steps = append(steps, s)
			}
		}
	}
	if q, ok := x.singleTypes[name]; ok {
		return lookupResult{steps: append(steps, "=T:"+q)}
	}
	if t := x.topTypes[name]; t != nil {
		return lookupResult{steps: append(steps, "=T:"+t.q)}
	}
	// A single-static-import imports a member type of the name too (JLS
	// 7.5.3) — when the imported member is one.
	if owners := x.staticSingle[name]; len(owners) > 0 {
		var alts []string
		for _, o := range owners {
			alts = append(alts, "@T:"+o+"."+name)
		}
		steps = append(steps, strings.Join(alts, "|"))
	}
	steps = append(steps, "=T:"+x.qualify(name))
	alts := []string{}
	for _, p := range x.onDemand {
		alts = append(alts, "=T:"+p+"."+name)
	}
	for _, o := range x.staticOnDemand {
		alts = append(alts, "@T:"+o+"."+name) // static member types (JLS 7.5.4)
	}
	alts = append(alts, "=T:java.lang."+name)
	return lookupResult{steps: append(steps, strings.Join(alts, "|"))}
}

// inheritedStep is the step that looks name up among what type t inherits
// (and its own members, which the step finds as well), "" when t has no
// supertype the file cannot see into. A type whose body the parser
// rejected may hold anything.
func inheritedStep(class byte, t *typeInfo, name string) string {
	alt := "@" + string(class) + ":" + t.q + "." + name
	switch {
	case t.broken:
		return alt + "|?"
	case t.supers:
		return alt
	}
	return ""
}

// valueSteps is the lookup of a simple variable name at pos in chain f
// (JLS 6.5.6.1): block scopes, enclosing types' fields, static imports.
func (x *extractor) valueSteps(name string, f *frame, pos uint32) lookupResult {
	var steps []string
	for g := f; g != nil; g = g.parent {
		if d := g.visible(name, pos, false); d != nil {
			return lookupResult{steps: append(steps, "!"), local: d}
		}
		if g.method && g.localNames[name] {
			// Declared as a local in this body, though not visibly here by
			// the scopes as read: never taken for a field.
			return lookupResult{steps: append(steps, "!"), local: &localDecl{}}
		}
		if g.typ != nil {
			if ty, ok := g.typ.fields[name]; ok {
				return lookupResult{steps: append(steps, "@V:"+g.typ.q+"."+name), ownField: true, ownFieldType: ty, ownFieldIn: g.typ}
			}
			if s := inheritedStep('V', g.typ, name); s != "" {
				steps = append(steps, s)
			}
		}
	}
	var alts []string
	for _, t := range x.staticSingle[name] {
		alts = append(alts, "@V:"+t+"."+name)
	}
	for _, t := range x.staticOnDemand {
		alts = append(alts, "@V:"+t+"."+name)
	}
	if len(alts) > 0 {
		steps = append(steps, strings.Join(alts, "|"))
	}
	return lookupResult{steps: steps}
}

// methodSteps is the lookup of a simple method name in chain f (JLS
// 15.12.1): the innermost enclosing type that has a member method of the
// name, then the static imports.
func (x *extractor) methodSteps(name string, f *frame) lookupResult {
	var steps []string
	for g := f; g != nil; g = g.parent {
		if g.typ == nil {
			continue
		}
		t := g.typ
		if t.methods[name] || t.implicitName[name] {
			alt := "@M:" + t.q + "." + name
			if t.broken {
				alt += "|?"
			}
			return lookupResult{steps: append(steps, alt)}
		}
		if s := inheritedStep('M', t, name); s != "" {
			steps = append(steps, s)
		}
	}
	var alts []string
	for _, t := range x.staticSingle[name] {
		alts = append(alts, "@M:"+t+"."+name)
	}
	for _, t := range x.staticOnDemand {
		alts = append(alts, "@M:"+t+"."+name)
	}
	if len(alts) > 0 {
		steps = append(steps, strings.Join(alts, "|"))
	}
	return lookupResult{steps: steps}
}

// innermostType is the type whose body chain f is in (this), nil outside.
func innermostType(f *frame) *typeInfo {
	for g := f; g != nil; g = g.parent {
		if g.typ != nil {
			return g.typ
		}
	}
	return nil
}

// enclosingNamed is the innermost enclosing type named name (Outer.this).
func enclosingNamed(f *frame, name string) *typeInfo {
	for g := f; g != nil; g = g.parent {
		if g.typ != nil && g.typ.name == name {
			return g.typ
		}
	}
	return nil
}

// withPath appends a member path to every "=" and "@" alternative of steps.
func withPath(steps []string, path string) []string {
	if path == "" {
		return steps
	}
	out := make([]string, len(steps))
	for i, st := range steps {
		alts := strings.Split(st, "|")
		for j, a := range alts {
			if a != "?" && a != "!" {
				alts[j] = a + path
			}
		}
		out[i] = strings.Join(alts, "|")
	}
	return out
}

// typeLookup is the lookup of the type a type node names, as a receiver's
// type, at pos in chain f: "" for what has no members Ark follows (a
// primitive, an array, a type parameter without a single class bound).
func (x *extractor) typeLookup(ty *ts.Node, f *frame, pos uint32) string {
	steps, ok := x.typeNameSteps(ty, f, pos)
	if !ok {
		return ""
	}
	return strings.Join(steps, ";")
}

// typeNameSteps is the lookup of a type node naming a class or interface
// type; ok is false for any other type.
func (x *extractor) typeNameSteps(ty *ts.Node, f *frame, pos uint32) ([]string, bool) {
	return x.typeNameStepsDepth(ty, f, pos, 0)
}

// typeNameStepsDepth is typeNameSteps, following at most a few bounds of
// type variables (bounds naming one another end there).
func (x *extractor) typeNameStepsDepth(ty *ts.Node, f *frame, pos uint32, depth int) ([]string, bool) {
	if ty == nil || depth > 8 {
		return nil, false
	}
	switch x.typ(ty) {
	case "generic_type", "annotated_type":
		return x.typeNameStepsDepth(x.firstType(ty), f, pos, depth+1)
	case "type_identifier":
		name := x.text(ty)
		if name == "var" {
			return nil, false
		}
		l := x.typeSteps(name, f, pos)
		if l.typeVar {
			// A type variable's members are those of its bound, when it is
			// one class or interface type.
			if l.typeVarBound == nil {
				return nil, false
			}
			bounds := x.named(l.typeVarBound)
			if len(bounds) != 1 {
				return nil, false
			}
			// Read where the parameter is declared: a type parameter
			// section sees no member of its own class (JLS 6.3).
			return x.typeNameStepsDepth(bounds[0], l.typeVarScope, bounds[0].StartByte(), depth+1)
		}
		return l.steps, true
	case "scoped_type_identifier":
		segs := x.scopedSegments(ty)
		if len(segs) < 2 {
			return nil, false
		}
		return x.qualifiedTypeSteps(segs, f, pos), true
	}
	return nil, false
}

// scopedSegments returns the identifiers of a scoped type name — type
// arguments and annotations aside (Outer<T>.Inner names Outer.Inner) —, nil
// if it holds anything else.
func (x *extractor) scopedSegments(ty *ts.Node) []string {
	var out []string
	for _, ch := range x.named(ty) {
		switch x.typ(ch) {
		case "type_identifier":
			out = append(out, x.text(ch))
		case "scoped_type_identifier", "generic_type", "annotated_type":
			var sub []string
			if x.typ(ch) == "scoped_type_identifier" {
				sub = x.scopedSegments(ch)
			} else if base := x.firstType(ch); base != nil {
				if x.typ(base) == "type_identifier" {
					sub = []string{x.text(base)}
				} else {
					sub = x.scopedSegments(base)
				}
			}
			if sub == nil {
				return nil
			}
			out = append(out, sub...)
		case "annotation", "marker_annotation":
		default:
			return nil
		}
	}
	return out
}

// qualifiedTypeSteps is the lookup of a qualified type name a.b.C (JLS
// 6.5.5.2): its head as a type, the rest its member types; else a package
// and its types, shortest package first.
func (x *extractor) qualifiedTypeSteps(segs []string, f *frame, pos uint32) []string {
	head := x.typeSteps(segs[0], f, pos)
	path := "/" + strings.Join(segs[1:], "/")
	var steps []string
	if !head.typeVar {
		steps = withPath(head.steps, path)
	}
	for k := 2; k <= len(segs); k++ {
		alt := "=T:" + strings.Join(segs[:k], ".")
		if k < len(segs) {
			alt += "/" + strings.Join(segs[k:], "/")
		}
		steps = append(steps, alt)
	}
	return steps
}

// typeText is the type a type node names, written without annotations,
// type arguments or whitespace ("java.util.Map.Entry", "int[][]"); "" for a
// node that is no type name.
func (x *extractor) typeText(ty *ts.Node) string {
	switch x.typ(ty) {
	case "generic_type", "annotated_type":
		return x.typeText(x.firstType(ty))
	case "array_type":
		elem := x.typeText(x.field(ty, "element"))
		if elem == "" {
			return ""
		}
		return elem + strings.Repeat("[]", strings.Count(x.text(x.field(ty, "dimensions")), "["))
	case "scoped_type_identifier":
		if segs := x.scopedSegments(ty); segs != nil {
			return strings.Join(segs, ".")
		}
		return ""
	case "type_identifier", "integral_type", "floating_point_type", "boolean_type", "void_type":
		return x.text(ty)
	}
	return ""
}
