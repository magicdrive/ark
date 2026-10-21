package resolver

import (
	"strings"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// Argument types (language.ReferenceDraft.ArgumentTypes): the callables a
// call's arguments certainly cannot be passed to are left out, phase by
// phase, and among callables of primitive parameters only the most specific
// is kept. Every rule removes only what the stated types exclude: an
// argument, or a parameter, of a type the notation leaves unknown may be
// passed anywhere.

// valueType is a type in the Parameters / ArgumentTypes notation.
type valueType struct {
	base  string // a primitive, "null", "S", "F", "R", "?" (a reference type of unknown identity), "" unknown
	dims  int
	known bool
}

var primitiveRank = map[string]int{"byte": 1, "short": 2, "char": 2, "int": 3, "long": 4, "float": 5, "double": 6}

func isPrimitive(b string) bool { return primitiveRank[b] > 0 || b == "boolean" }

// widens reports whether primitive a converts to primitive b by identity or
// widening (JLS 5.1.2).
func widens(a, b string) bool {
	switch {
	case a == b:
		return true
	case a == "boolean" || b == "boolean":
		return false
	case a == "char":
		return primitiveRank[b] >= primitiveRank["int"]
	case b == "char":
		return false
	}
	return primitiveRank[a] < primitiveRank[b] && primitiveRank[a] > 0
}

func parseValueType(s string, param bool) valueType {
	s = strings.TrimSpace(s)
	if s == "" {
		return valueType{}
	}
	dims := 0
	for strings.HasSuffix(s, "[]") {
		dims++
		s = strings.TrimSuffix(s, "[]")
	}
	switch {
	case isPrimitive(s):
		return valueType{base: s, dims: dims, known: true}
	case param && strings.HasPrefix(s, "?"), param && s == "?":
		return valueType{base: "?", dims: dims, known: true}
	case !param && (s == "null" || s == "S" || s == "F" || s == "N" || s == "R"):
		return valueType{base: s, dims: dims, known: true}
	}
	return valueType{}
}

func (v valueType) primitive() bool { return v.dims == 0 && isPrimitive(v.base) }

// passable reports whether an argument of type a may be passed to a
// parameter of type p; loose admits boxing and unboxing.
func passable(a, p valueType, loose bool) bool {
	if !a.known || !p.known {
		return true
	}
	switch {
	case a.base == "null":
		return !p.primitive()
	case a.base == "N" && a.dims == 0 && p.dims == 0 && !isPrimitive(p.base):
		return true
	case p.primitive():
		switch {
		case a.primitive():
			return widens(a.base, p.base)
		case a.dims == 0 && a.base == "R":
			return loose // unboxing
		}
		return false
	case p.dims > 0 && isPrimitive(p.base):
		// a primitive array: only the same one
		return a.dims == p.dims && a.base == p.base
	case p.dims > 0:
		// an array of references: an array of as many or more dimensions
		// whose elements at that depth are references
		switch {
		case a.dims < p.dims:
			return false
		case a.dims == p.dims:
			return !isPrimitive(a.base) && a.base != "F" && a.base != "S" && a.base != "N"
		}
		return true
	}
	// A reference parameter of a type of unknown identity.
	switch {
	case a.primitive():
		return loose // boxing
	case a.dims > 0:
		return true // an array is an Object
	}
	return true
}

// callableTypes parses Parameters: the parameter types and whether the last
// is variadic; ok is false for "" (nothing stated).
func callableTypes(params string) (types []valueType, variadic, ok bool) {
	if params == "" {
		return nil, false, false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(params, "("), ")")
	if inner == "" {
		return nil, false, true
	}
	for _, p := range strings.Split(inner, ",") {
		if strings.HasSuffix(p, "...") {
			variadic = true
			p = strings.TrimSuffix(p, "...")
		}
		types = append(types, parseValueType(p, true))
	}
	return types, variadic, true
}

// applicableIn reports whether a callable may be applicable to args in phase
// 1 (strict), 2 (loose) or 3 (variadic); definite reports that it surely is.
func applicableIn(phase int, params string, args []valueType) (maybe, definite bool) {
	types, variadic, ok := callableTypes(params)
	if !ok {
		return true, false
	}
	loose := phase >= 2
	definite = true
	check := func(a, p valueType) bool {
		if !passable(a, p, loose) {
			return false
		}
		if !a.known || !p.known || !a.primitive() || !p.primitive() || !widens(a.base, p.base) {
			if !(a.known && p.known && a.dims > 0 && a.dims == p.dims && a.base == p.base && isPrimitive(a.base)) {
				definite = false
			}
		}
		return true
	}
	if phase < 3 {
		if len(args) != len(types) {
			return false, false
		}
		for i := range args {
			p := types[i]
			if variadic && i == len(types)-1 {
				p.dims++ // passed as the array itself
			}
			if !check(args[i], p) {
				return false, false
			}
		}
		return true, definite
	}
	if !variadic || len(args) < len(types)-1 {
		return false, false
	}
	for i := range args {
		p := types[len(types)-1]
		if i < len(types)-1 {
			p = types[i]
		}
		if !check(args[i], p) {
			return false, false
		}
	}
	return true, definite
}

// identical reports whether params are exactly the known primitive (or
// primitive array) types of args, the last a variadic one's array itself.
func identical(params string, args []valueType) bool {
	types, variadic, ok := callableTypes(params)
	if !ok || len(types) != len(args) {
		return false
	}
	for i, p := range types {
		if variadic && i == len(types)-1 {
			p.dims++
		}
		a := args[i]
		if !a.known || !isPrimitive(a.base) || !p.known || a.base != p.base || a.dims != p.dims {
			return false
		}
	}
	return true
}

// moreSpecific reports whether every parameter of a, all primitives, widens
// to b's at the same position (JLS 15.12.2.5).
func moreSpecific(a, b string) bool {
	ta, va, oka := callableTypes(a)
	tb, vb, okb := callableTypes(b)
	if !oka || !okb || va || vb || len(ta) != len(tb) {
		return false
	}
	for i := range ta {
		if !ta[i].primitive() || !tb[i].primitive() || !widens(ta[i].base, tb[i].base) {
			return false
		}
	}
	return true
}

// applyArgumentTypes narrows the callables by the call's argument types.
func (m *memberSet) applyArgumentTypes(ref reference.Reference) {
	var args []valueType
	for _, a := range strings.Split(ref.ArgumentTypes, ",") {
		args = append(args, parseValueType(a, false))
	}
	if len(args) != ref.ArgumentCount {
		return // not the notation's: nothing is trusted
	}
	type entry struct {
		params string
		decl   *symbol.Symbol
		im     *implicitMember
	}
	var all []entry
	for i := range m.decls {
		all = append(all, entry{params: m.decls[i].Parameters, decl: &m.decls[i]})
	}
	for i := range m.implicit {
		all = append(all, entry{params: m.implicit[i].params, im: &m.implicit[i]})
	}
	// The phases: the first that surely finds an applicable callable is the
	// language's; while one only may, the later ones may be too.
	var kept []entry
	allDefinite := false // every callable kept surely applies, in the phase that decides
	exact := -1          // the one callable of the arguments' own types, applying strictly
	for phase := 1; phase <= 3; phase++ {
		var maybe []entry
		definite, every := false, true
		for _, e := range all {
			if ok, def := applicableIn(phase, e.params, args); ok {
				maybe = append(maybe, e)
				// A callable the reference may not access does not end the
				// phases: an accessible one a later phase finds may be the
				// language's choice.
				def = def && !m.isUncertain(e)
				definite = definite || def
				every = every && def
			}
		}
		kept = appendEntries(kept, maybe)
		if definite {
			allDefinite = every && len(kept) == len(maybe)
			if phase == 1 {
				// Its parameters are subtypes of every other one's that
				// applies strictly: it is the most specific (JLS 4.10.1,
				// 15.12.2.5).
				for i, e := range kept {
					if identical(e.params, args) {
						if exact >= 0 {
							exact = -2 // two of the signature: no choice
						} else if exact == -1 {
							exact = i
						}
					}
				}
			}
			break
		}
	}
	if exact >= 0 && !m.isUncertain(kept[exact]) {
		kept = []entry{kept[exact]}
	}
	// Among callables of primitive parameters only, of primitive arguments,
	// all surely applicable: the most specific one.
	if len(kept) > 1 && allDefinite {
		for _, cand := range kept {
			best := true
			for _, other := range kept {
				if cand.params != other.params && !moreSpecific(cand.params, other.params) {
					best = false
					break
				}
				if cand.params == other.params && cand != other {
					best = false // two of one signature: no choice
					break
				}
			}
			if best {
				if !m.isUncertain(cand) {
					kept = []entry{cand}
				}
				break
			}
		}
	}
	var decls []symbol.Symbol
	var implicit []implicitMember
	for _, e := range kept {
		if e.decl != nil {
			decls = append(decls, *e.decl)
		} else {
			implicit = append(implicit, *e.im)
		}
	}
	m.decls, m.implicit = decls, implicit
}

// isUncertain reports whether e is a callable the reference may not access.
func (m *memberSet) isUncertain(e struct {
	params string
	decl   *symbol.Symbol
	im     *implicitMember
}) bool {
	return e.decl != nil && m.uncertain[e.decl.ID]
}

func appendEntries[T comparable](to, from []T) []T {
	for _, e := range from {
		dup := false
		for _, have := range to {
			if have == e {
				dup = true
				break
			}
		}
		if !dup {
			to = append(to, e)
		}
	}
	return to
}
