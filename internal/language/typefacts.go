package language

import (
	"strconv"
	"strings"
)

// Declared-type facts and receiver chains.
//
// Member lookup needs a receiver's type; a provider proves one from local
// syntax (ReferenceDraft.ReceiverType) or not at all. Many receivers are
// typed by declarations of other files instead: a variable assigned a
// function's result (x := NewServer(); x.Run()), a field of a struct
// declared elsewhere (s.repo.Save()), a method promoted from an embedded
// type (o.M() with M declared on an embedded Inner). For these a provider
// states two kinds of evidence, neither of which is inference:
//
//   - TypeFact: what a declaration's own text says about types — a struct's
//     fields with their declared types (embedded ones marked), an
//     interface's embedded interfaces, a defined type's underlying type, an
//     alias's aliased type, a function's or method's declared result types,
//     a package variable's declared type (or how its initializer is derived).
//   - ReceiverChain: how a receiver expression is built, as written — "the
//     result of calling NewServer", "field repo of parameter s", "an element
//     of xs" — each step naming declarations or types only as the code
//     writes them.
//
// The resolver evaluates a chain step by step against the facts of the
// declarations each step names (resolver/typefacts.go): every step must name
// exactly one declaration, or the chain proves nothing and the reference is
// resolved as if it carried no chain. No step guesses: a name is looked up by
// the file's own scoping, a type is the declared one, and a value whose
// declaration states no type ends the chain.

// TypeExpr is a type as a declaration writes it, normalized:
//
//	T, q.T     a named type; q is an import name of the declaring file.
//	           Type arguments are dropped: T[X] is T (the generic type, whose
//	           members are the same declarations for every instantiation).
//	*X         a pointer to X
//	[]X        a slice or an array of X
//	map[K]V    a map
//
// "" means "not stated": any other type (a function, a channel, a struct or
// interface literal), or one naming a type parameter, whose type varies with
// each instantiation.
type TypeExpr = string

// TypeFact states the types a declaration's own text writes. It belongs to
// the symbol of the same file whose Qualified is Decl and whose location
// starts at Line:Column (namesakes — two init functions — are told apart by
// position).
type TypeFact struct {
	Decl   string `json:"decl"`
	Line   uint32 `json:"line"`
	Column uint32 `json:"column"`

	// A type declaration: a struct's fields in order, an interface's embedded
	// types (Embedded, no Name). Complete is false when the declaration's
	// fields or embedded types could not all be read (a parse error inside
	// it): then its members other than its methods are unknown.
	Fields   []FieldFact `json:"fields,omitempty"`
	Complete bool        `json:"complete,omitempty"`
	// Struct / Interface: the declaration is a struct type / an interface
	// type, whose members are its fields and methods / its methods and
	// those of the interfaces it embeds.
	Struct    bool `json:"struct,omitempty"`
	Interface bool `json:"interface,omitempty"`
	// Underlying: a defined type's underlying type when it is neither a
	// struct nor an interface (type Nodes []*Node); its members are its own
	// methods only — and, when the underlying type is a named one (type A
	// B), that type's fields and promoted members too, which member lookup
	// therefore treats as unknown. Alias: the type an alias declaration names (type A = B),
	// whose members are that type's.
	Underlying TypeExpr `json:"underlying,omitempty"`
	Alias      TypeExpr `json:"alias,omitempty"`

	// A function or a method: its declared result types, in order.
	Results []TypeExpr `json:"results,omitempty"`

	// A package variable: its declared type, or — without one — how its
	// initializer is built (a ReceiverChain evaluated in the declaring file).
	Type  TypeExpr `json:"type,omitempty"`
	Chain string   `json:"chain,omitempty"`
}

// FieldFact is one field of a struct, or one embedded type of an interface.
// An embedded field's name is its type's name (Embedded: its members are
// promoted — a selector x.f finds f on the shallowest embedding depth that
// declares one, and two at that depth make it ambiguous).
type FieldFact struct {
	Name     string   `json:"name,omitempty"`
	Type     TypeExpr `json:"type,omitempty"`
	Embedded bool     `json:"embedded,omitempty"`
}

// TypeDeref strips one pointer from t.
func TypeDeref(t TypeExpr) TypeExpr { return strings.TrimPrefix(t, "*") }

// TypeElem returns the element type of a slice, an array or a map (its
// values), or "" for any other type.
func TypeElem(t TypeExpr) TypeExpr {
	if rest, ok := strings.CutPrefix(t, "[]"); ok {
		return rest
	}
	if rest, ok := strings.CutPrefix(t, "map["); ok {
		depth := 1
		for i, r := range rest {
			switch r {
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					return rest[i+1:]
				}
			}
		}
	}
	return ""
}

// TypeNamed splits a named type T or q.T; ok is false for any other type.
func TypeNamed(t TypeExpr) (qualifier, name string, ok bool) {
	if t == "" || strings.ContainsAny(t, "*[]") {
		return "", "", false
	}
	if q, n, dotted := strings.Cut(t, "."); dotted {
		if q == "" || n == "" || strings.Contains(n, ".") {
			return "", "", false
		}
		return q, n, true
	}
	return "", t, true
}

// ReceiverChain is the derivation of a receiver's type from the
// declarations it names, as steps separated by "|" (ReferenceDraft and
// TypeFact.Chain):
//
//	type:<TypeExpr>   a value of the type (written in the file's scope)
//	decl:<N>, decl:<q>.<N>
//	                  the package-level declaration N (of the import q): a
//	                  function, a variable or a type, found by the file's own
//	                  scoping
//	call:<i>          result i (from 0) of calling the current function or
//	                  method; of a type, a conversion to it
//	member:<N>        field or method N of the current value, promoted ones
//	                  included; of a type, its method N (a method expression)
//	elem              an element of the current slice, array or map; of a
//	                  generic function or type, its instantiation (the same
//	                  declaration)
//
// A chain starts with type: or decl:.
type ChainStep struct {
	Op   string // "type", "decl", "call", "member" or "elem"
	Arg  string // the type, the name, or the member
	Call int    // call: the result index
}

// ChainSteps parses a chain; ok is false for a malformed one.
func ChainSteps(chain string) (steps []ChainStep, ok bool) {
	if chain == "" {
		return nil, false
	}
	for i, s := range strings.Split(chain, "|") {
		op, arg, _ := strings.Cut(s, ":")
		st := ChainStep{Op: op, Arg: arg}
		switch op {
		case "type", "decl":
			if i != 0 || arg == "" {
				return nil, false
			}
		case "member":
			if i == 0 || arg == "" {
				return nil, false
			}
		case "call":
			n, err := strconv.Atoi(arg)
			if i == 0 || err != nil || n < 0 {
				return nil, false
			}
			st.Call = n
		case "elem":
			if i == 0 || arg != "" {
				return nil, false
			}
		default:
			return nil, false
		}
		steps = append(steps, st)
	}
	return steps, true
}
