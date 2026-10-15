package typescript

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
)

// Value reads.
//
// Besides calls, constructions and JSX components, a program names its
// declarations as values: a function passed as an argument, a constant in
// an expression, an object whose member it reads or writes, `export default
// x`, `export { x }`. Each such name becomes one value_reference (edge:
// references), as in Go (golang/refs_values.go), when the program itself
// shows that the name denotes a declaration:
//
//   - it is a top-level declaration of the program, or a name one of its
//     imports or requires binds (BindingDraft);
//   - it stands where a value is read (valueUse: an operand, an argument, an
//     element, an initializer, a property's value, a condition, the object
//     of a member access, ...), never at a declaring position, a key, a
//     callee (call records it) or a constructor (construction does);
//   - no local declaration hides it at that position (locals): a local's use
//     is no reference to a declaration;
//   - it is a value's name: an interface's or a type alias's is a type's
//     (valueKinds), and the reference denotes a value only (TargetKinds);
//   - it stands outside an enum's members (where a name is first the enum's
//     own member) and a with statement's body (where it may be a property
//     of the object).
//
// Whether a name is a top-level declaration or a binding is known only once
// the whole program is read (a function may use a name declared below it),
// so the reads are kept aside and stated at the end (finishReads).

// valueKinds are the kinds of the declarations a value read can denote
// (symbol.SymbolKind values, as ReferenceDraft.TargetKinds): an interface or
// a type alias names a type, never a value — a read of `Response` beside an
// `interface Response` is the global value.
const valueKinds = "function,class,variable,constant,enum,namespace,module"

type pendingRead struct {
	draft language.ReferenceDraft
}

// valueRead keeps aside the read of the identifier n where it stands.
func (e *extractor) valueRead(n *ts.Node, sc scope) {
	name := e.text(n)
	if name == "" || sc.locals.Values[name] || sc.enumBody || sc.dynamic || !e.valueUse(n) {
		return
	}
	e.readAt(n, name, sc.container)
}

// readAt keeps aside a read of name at n, in container.
func (e *extractor) readAt(n *ts.Node, name, container string) {
	if _, ok := e.cssClasses[name]; ok {
		return // a CSS Modules class (cssmodules.go)
	}
	if _, ok := e.cssModules[name]; ok {
		return // a CSS Modules file's class map: no declaration of the program
	}
	e.reads = append(e.reads, pendingRead{draft: language.ReferenceDraft{
		Name:        name,
		Kind:        string(reference.KindValueReference),
		Container:   container,
		Location:    nodeLocation(n, e.file),
		TargetKinds: valueKinds,
	}})
}

// finishReads states the reads of names the program declares at its top
// level or binds by an import or a require.
func (e *extractor) finishReads() {
	known := map[string]bool{}
	for _, s := range e.symbols {
		if s.Parent == "" && strings.Contains(","+valueKinds+",", ","+string(s.Kind)+",") {
			known[s.Name] = true
		}
	}
	for _, b := range e.bindings {
		known[b.Local] = true
	}
	for _, r := range e.reads {
		if known[r.draft.Name] {
			e.refs = append(e.refs, r.draft)
		}
	}
	e.reads = nil
}
