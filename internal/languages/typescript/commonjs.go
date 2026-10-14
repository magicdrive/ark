package typescript

import (
	"slices"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/ecmascript"
)

// CommonJS module bindings and exports, in the forms whose meaning the
// syntax fixes. The value a CommonJS module exports is its module.exports;
// Ark names it with the export name "module.exports":
//
//	const X = require("./m")            binds X to m's module.exports
//	const { a, b: c } = require("./m")  binds a and c to m's exports a and b
//	const y = require("./m").y          binds y to m's export y
//
//	module.exports = X                  exports X as "module.exports"
//	module.exports = { a, b: c }        exports a as "a", c as "b"
//	exports.a = f / module.exports.a = f  exports f as "a"
//
// Only a top-level statement binds or exports: a require or an assignment
// inside a function or a branch may never run. A module that assigns one
// export name twice states two exports, and a binding of it is ambiguous —
// never a pick. Any other shape (a computed require, a nested pattern, an
// export of an expression) binds or exports nothing, and its names stay
// locals (ecmascript.ProgramLocals).
//
// An export states the value its module's statements fix, so none is stated
// where the module may write another (docs/development/web-semantic-fidelity/
// f6b5/semantics.md): a property of the replacement object a later property
// may redefine (a spread, a computed key, the same key: the object
// initializer defines its properties in order, the last one stays), a name
// any other write reaches (in a function or a branch, exports.a += …,
// exports.a++, delete exports.a, a destructuring target), every name when the
// module writes the exports object as a whole (Object.assign(exports, …),
// Object.defineProperty and the like, exports[k] = … with a computed key) and
// every export when it replaces module.exports anywhere but a top-level
// statement. Writes another module makes through its binding
// (require("./m").a = b) are not seen: the export states what the module
// itself fixes.

// requireBindings records the bindings of one top-level declarator whose
// value is a require (ecmascript.RequireBindings). It reports whether it did.
func (e *extractor) requireBindings(d *ts.Node) bool {
	bs, ok := ecmascript.RequireBindings(d, e.lang, e.src, e.reassigned)
	if !ok {
		return false
	}
	for _, b := range bs {
		e.imports = append(e.imports, language.ImportDraft{Path: b.Specifier, Location: nodeLocation(d, e.file)})
		e.bindings = append(e.bindings, language.BindingDraft{
			Local:    b.Local,
			Kind:     language.BindingNamed,
			Module:   e.moduleSpec(b.Specifier),
			Imported: b.Imported,
			Location: nodeLocation(b.At, e.file),
		})
	}
	return true
}

// finishCommonJS drops the exports whose value the module's statements do
// not fix:
//   - the property exports (exports.a = f, module.exports.a = f) of a module
//     that also replaces module.exports (module.exports = X): which of them
//     the replacement keeps depends on the order the statements run in and
//     on X;
//   - an export of the replacement object ({ a }) whose name a property
//     assignment also writes (module.exports.a = b after it);
//   - every export of a name a property assignment writes with a value no
//     export stands for (exports.a = function b() {} after exports.a = a):
//     the name has a value Ark does not state.
func (e *extractor) finishCommonJS() {
	drop := map[int]bool{}
	// Replaced more than once: the last replacement stays. An earlier one's
	// export of a name the last one also exports is kept, for the ambiguity
	// of a name exported twice; any other is not the module's.
	last := map[string]bool{}
	for i, n := range e.cjsReplacement {
		if n == e.cjsReplacements {
			last[e.exports[i].Exported] = true
		}
	}
	for _, i := range e.cjsExports {
		x := e.exports[i].Exported
		n := e.cjsReplacement[i] // 0: a property export
		replacement := n > 0
		switch {
		case e.cjsWrites.replaced, e.cjsWrites.all && x != "module.exports", e.cjsWrites.names[x]:
			drop[i] = true
		case replacement && n < e.cjsReplacements && !last[x]:
			drop[i] = true
		case !replacement && e.cjsWrites.rebound:
			drop[i] = true // exports.a = f: exports may be another object
		}
	}
	if e.cjsReplaced {
		for _, i := range e.cjsProperty {
			drop[i] = true
		}
	}
	stated := map[string]int{}
	for _, i := range e.cjsProperty {
		stated[e.exports[i].Exported]++
	}
	for i, x := range e.exports {
		n := e.cjsAssigned[x.Exported]
		if n == 0 || x.Exported == "module.exports" {
			continue
		}
		if !slices.Contains(e.cjsProperty, i) || n > stated[x.Exported] {
			drop[i] = true
		}
	}
	if len(drop) == 0 {
		return
	}
	kept := e.exports[:0]
	for i, x := range e.exports {
		if !drop[i] {
			kept = append(kept, x)
		}
	}
	e.exports = kept
}

// commonJSExports records the exports a top-level expression statement
// assigns.
func (e *extractor) commonJSExports(stmt *ts.Node) {
	if stmt.NamedChildCount() != 1 {
		return
	}
	asg := stmt.NamedChild(0)
	if e.typ(asg) != "assignment_expression" {
		return
	}
	left, right := e.field(asg, "left"), e.field(asg, "right")
	replacing := false // the statement replaces module.exports
	export := func(exported, local string) {
		if exported == "" || local == "" {
			return
		}
		e.cjsExports = append(e.cjsExports, len(e.exports))
		if e.cjsReplacement == nil {
			e.cjsReplacement = map[int]int{}
		}
		if replacing {
			e.cjsReplacement[len(e.exports)] = e.cjsReplacements
		}
		// Mutable: the exports object is a plain object any code may change
		// (contract: docs/development/web-semantic-fidelity/commonjs-boundary).
		e.exports = append(e.exports, language.ExportDraft{
			Kind: language.ExportLocal, Exported: exported, Local: local, Mutable: true, Location: nodeLocation(stmt, e.file),
		})
	}
	target := e.commonJSTarget(left)
	replacing = target == "module.exports"
	switch {
	case target == "module.exports":
		e.cjsReplaced = true
		e.cjsReplacements++
		switch e.typ(right) {
		case "identifier":
			export("module.exports", e.text(right))
		case "object":
			// From the last property back: one is exported when no later
			// property may define its key.
			var props [][2]string
			later, anyKey := map[string]bool{}, false
			for i := right.NamedChildCount() - 1; i >= 0; i-- {
				p := right.NamedChild(i)
				var key string
				switch e.typ(p) {
				case "comment":
					continue
				case "shorthand_property_identifier":
					if key = e.text(p); strings.ContainsRune(key, '\\') {
						key = "" // an escape: the name is not the text
					}
					if key != "" && !anyKey && !later[key] {
						props = append(props, [2]string{key, key})
					}
				case "pair":
					k, v := e.field(p, "key"), e.field(p, "value")
					key = e.staticKey(k)
					if key == "__proto__" { // sets the prototype, defines no property
						continue
					}
					if key != "" && !anyKey && !later[key] && e.typ(k) == "property_identifier" && e.typ(v) == "identifier" {
						props = append(props, [2]string{key, e.text(v)})
					}
				case "method_definition":
					key = e.staticKey(e.field(p, "name"))
				}
				if key == "" { // a spread, a computed key: any key
					anyKey = true
				}
				later[key] = true
			}
			for i := len(props) - 1; i >= 0; i-- {
				export(props[i][0], props[i][1])
			}
		}
	case target != "" && !strings.ContainsRune(target, '\\'):
		if e.cjsAssigned == nil {
			e.cjsAssigned = map[string]int{}
		}
		if chain, ok := e.voidChain(asg); ok {
			// exports.b = exports.a = void 0 declares the names a later
			// statement assigns (TypeScript's CommonJS output); after an
			// export of the name it is a write of another value.
			for _, a := range chain {
				t := e.commonJSTarget(e.field(a, "left"))
				for _, i := range e.cjsExports {
					if e.exports[i].Exported == t {
						e.cjsAssigned[t]++
						break
					}
				}
			}
			return
		}
		if e.cjsAssigned == nil {
			e.cjsAssigned = map[string]int{}
		}
		e.cjsAssigned[target]++
		if e.typ(right) == "identifier" {
			e.cjsProperty = append(e.cjsProperty, len(e.exports))
			export(target, e.text(right))
		}
	}
}

// commonJSTarget names what an assignment target exports: "module.exports"
// for module.exports, the name a for exports.a or module.exports.a, "" for
// anything else.
func (e *extractor) commonJSTarget(n *ts.Node) string {
	if e.typ(n) != "member_expression" {
		return ""
	}
	obj, prop := e.field(n, "object"), e.field(n, "property")
	if e.typ(prop) != "property_identifier" {
		return ""
	}
	switch {
	case e.typ(obj) == "identifier" && e.text(obj) == "module" && e.text(prop) == "exports":
		return "module.exports"
	case e.typ(obj) == "identifier" && e.text(obj) == "exports":
		return e.text(prop)
	case e.commonJSTarget(obj) == "module.exports":
		return e.text(prop)
	}
	return ""
}

// staticKey is the property name a property key n names, "" when it is no
// fixed name (a computed key, a string with an escape).
func (e *extractor) staticKey(n *ts.Node) string {
	switch e.typ(n) {
	case "property_identifier", "number", "private_property_identifier":
		if t := e.text(n); !strings.ContainsRune(t, '\\') {
			return t
		}
	case "string":
		for i := range n.NamedChildCount() {
			if e.typ(n.NamedChild(i)) != "string_fragment" {
				return ""
			}
		}
		if t := e.text(n); len(t) >= 2 {
			return t[1 : len(t)-1]
		}
	}
	return ""
}

// commonJSWrites are the writes to the exports object other than the
// top-level assignment statements commonJSExports reads.
type commonJSWrites struct {
	names    map[string]bool // exports.a written: a
	all      bool            // the object written as a whole, by a computed key, or let out
	replaced bool            // module.exports replaced, or module let out
	rebound  bool            // exports bound to another object
}

// exportsMutators write the properties of their first argument;
// exportsMethods are the methods that write a property of their object.
var (
	exportsMutators = map[string]bool{
		"Object.assign": true, "Object.defineProperty": true, "Object.defineProperties": true,
		"Reflect.set": true, "Reflect.defineProperty": true, "Reflect.deleteProperty": true,
	}
	exportsMethods = map[string]bool{"__defineGetter__": true, "__defineSetter__": true}
)

// scanCommonJSWrites records in e.cjsWrites the writes to the exports object
// anywhere in the program but the assignments of a top-level expression
// statement to module.exports, exports.a and module.exports.a
// (commonJSExports). The exports object is exports, module.exports and the
// top-level this (an arrow function's too), through parentheses and
// TypeScript's as, satisfies, ! and <T>; module['exports'] is
// module.exports. A write is an assignment, an update, delete, a for-in or
// for-of target, a destructuring target, Object.assign and the like,
// __defineGetter__ and __defineSetter__. The object let out — an argument,
// an initializer, an assigned value, an element, a property's value, a
// spread, a returned, thrown or yielded value, an operand of && || ?? or a
// branch of ?: — may be written anywhere: every name is taken as written
// (module let out: module.exports may be replaced). Inside a function whose
// parameter (or a catch clause whose parameter) is named module or exports,
// that name is the parameter's; any other local of the name still counts its
// writes (a write the scan cannot place is taken as one).
func (e *extractor) scanCommonJSWrites(root *ts.Node) {
	w := &e.cjsWrites
	w.names = map[string]bool{}
	unwrap := func(n *ts.Node) *ts.Node {
		for n != nil {
			switch e.typ(n) {
			case "parenthesized_expression", "as_expression", "satisfies_expression", "non_null_expression", "type_assertion":
				var inner *ts.Node
				for i := range n.NamedChildCount() {
					if c := n.NamedChild(i); e.typ(c) != "type_arguments" && e.typ(c) != "comment" {
						inner = c
						break
					}
				}
				if e.typ(n) == "type_assertion" && n.NamedChildCount() > 1 {
					inner = n.NamedChild(n.NamedChildCount() - 1)
				}
				n = inner
				continue
			}
			return n
		}
		return nil
	}
	// object is "exports" for the exports object (exports, module.exports,
	// the top-level this), "module" for module, "" otherwise (sh: the names
	// a parameter shadows, "this" in a function or a class).
	var target func(n *ts.Node, sh map[string]bool) string
	object := func(n *ts.Node, sh map[string]bool) string {
		switch n = unwrap(n); e.typ(n) {
		case "identifier":
			if t := e.text(n); (t == "exports" || t == "module") && !sh[t] {
				return t
			}
		case "this":
			// The top-level this is the exports object; a function's or a
			// method's is whatever object it is called on, the exports object
			// among them. A constructor's, a field initializer's and a static
			// block's is a new instance or the class.
			if !sh["ctor"] {
				return "exports"
			}
		case "member_expression", "subscript_expression":
			if target(n, sh) == "module.exports" {
				return "exports"
			}
		}
		return ""
	}
	// key is the name a member or subscript expression names, "\x00" when it
	// names no fixed one.
	key := func(n *ts.Node) string {
		if e.typ(n) == "member_expression" {
			p := e.field(n, "property")
			if k := e.staticKey(p); k != "" && e.typ(p) == "property_identifier" {
				return k
			}
			return "\x00"
		}
		if k := e.staticKey(unwrap(e.field(n, "index"))); k != "" {
			return k
		}
		return "\x00"
	}
	// target names what the member or subscript expression n is:
	// "module.exports", a name of the exports object, "\x00" for one of no
	// fixed name, "" for anything else.
	target = func(n *ts.Node, sh map[string]bool) string {
		n = unwrap(n)
		if t := e.typ(n); t != "member_expression" && t != "subscript_expression" {
			return ""
		}
		k := key(n)
		switch object(e.field(n, "object"), sh) {
		case "module":
			if k == "exports" || k == "\x00" { // module[k] may be module.exports
				return "module.exports"
			}
		case "exports":
			return k
		}
		return ""
	}
	letOut := func(n *ts.Node, sh map[string]bool) {
		if u := unwrap(n); e.typ(u) == "identifier" && e.text(u) == "require" && !sh["require"] {
			w.replaced = true // const r = require; r.cache[…]
		}
		switch object(n, sh) {
		case "exports":
			w.all = true
		case "module":
			w.replaced = true
		}
	}
	// write records a write to n, and to every target in it when it is a
	// destructuring pattern. A member's object is read, not written
	// (exports.a.b = c writes a's value, not the export a).
	var write func(n *ts.Node, sh map[string]bool)
	write = func(n *ts.Node, sh map[string]bool) {
		n = unwrap(n)
		switch e.typ(n) {
		case "member_expression", "subscript_expression":
			switch t := target(n, sh); t {
			case "":
			case "module.exports":
				w.replaced = true
			case "\x00":
				w.all = true
			default:
				w.names[t] = true
			}
			return
		case "identifier":
			if t := e.text(n); !sh[t] && t == "exports" {
				w.rebound = true
			} else if !sh[t] && t == "module" {
				w.replaced = true
			}
			return
		case "call_expression":
			return
		}
		for i := range n.NamedChildCount() {
			write(n.NamedChild(i), sh)
		}
	}
	// keys records the names an object literal defines (Object.assign's
	// sources, Object.defineProperties' descriptors), or every name when
	// they are not all fixed.
	keys := func(n *ts.Node) {
		if n = unwrap(n); e.typ(n) != "object" {
			w.all = true
			return
		}
		for i := range n.NamedChildCount() {
			p := n.NamedChild(i)
			var k string
			switch e.typ(p) {
			case "comment":
				continue
			case "shorthand_property_identifier":
				if k = e.text(p); strings.ContainsRune(k, '\\') {
					k = ""
				}
			case "pair":
				k = e.staticKey(e.field(p, "key"))
			case "method_definition":
				k = e.staticKey(e.field(p, "name"))
			}
			if k == "" {
				w.all = true
				return
			}
			w.names[k] = true
		}
	}
	top := map[*ts.Node]bool{}
	for i := range root.ChildCount() {
		if n := root.Child(i); e.typ(n) == "expression_statement" && n.NamedChildCount() == 1 {
			top[n.NamedChild(0)] = true
			if chain, ok := e.voidChain(n.NamedChild(0)); ok {
				for _, a := range chain {
					top[a] = true
				}
			}
		}
	}
	shadow := func(sh map[string]bool, names ...string) map[string]bool {
		inner := map[string]bool{}
		for k := range sh {
			inner[k] = true
		}
		for _, k := range names {
			if strings.HasPrefix(k, "-") {
				delete(inner, k[1:])
			} else {
				inner[k] = true
			}
		}
		return inner
	}
	var visit func(n *ts.Node, sh map[string]bool)
	visit = func(n *ts.Node, sh map[string]bool) {
		switch e.typ(n) {
		case "function_declaration", "function_expression", "function", "generator_function_declaration",
			"generator_function", "method_definition", "class_declaration", "class", "abstract_class_declaration":
			if name := e.field(n, "name"); name != nil && !sh["fn"] && e.typ(n) != "method_definition" &&
				e.typ(name) == "identifier" && !sh[e.text(name)] {
				switch e.text(name) { // function module() {} at the top level
				case "exports":
					w.rebound = true
				case "module":
					w.replaced = true
				}
			}
			params := e.field(n, "parameters")
			// this (object): a class's constructor's is a new instance; any
			// other function's or method's, a class's method's among them, may
			// be the exports object (exports.m = A.prototype.m, by any route).
			this := []string{"fn", "-ctor", "-inclass"}
			switch t := e.typ(n); {
			case t == "class_declaration" || t == "class" || t == "abstract_class_declaration":
				this = []string{"fn", "inclass"}
			case t == "method_definition" && sh["inclass"] && e.text(e.field(n, "name")) == "constructor":
				this = []string{"fn", "ctor", "-inclass"}
			}
			sh = shadow(sh, append(e.cjsShadows(params), this...)...)
		case "arrow_function", "catch_clause":
			params := e.field(n, "parameters")
			if params == nil {
				params = e.field(n, "parameter")
			}
			sh = shadow(sh, append(e.cjsShadows(params), "fn")...)
			if e.typ(n) == "arrow_function" {
				if b := e.field(n, "body"); b != nil && e.typ(b) != "statement_block" && e.typ(unwrap(b)) != "this" {
					letOut(b, sh)
				}
			}
		case "assignment_expression":
			left := e.field(n, "left")
			if !top[n] || e.commonJSTarget(left) == "" || strings.ContainsRune(e.text(left), '\\') {
				write(left, sh)
			}
			letOut(e.field(n, "right"), sh)
		case "augmented_assignment_expression":
			write(e.field(n, "left"), sh)
			letOut(e.field(n, "right"), sh)
		case "update_expression":
			write(e.field(n, "argument"), sh)
		case "unary_expression":
			if op := e.field(n, "operator"); op != nil && e.text(op) == "delete" {
				write(e.field(n, "argument"), sh)
			}
		case "for_in_statement":
			if l := e.field(n, "left"); l != nil {
				write(l, sh)
			}
		case "variable_declarator":
			name, value := e.field(n, "name"), e.field(n, "value")
			if value != nil && !sh["fn"] && e.typ(name) == "identifier" {
				write(name, sh) // var exports = {} at the top level
			}
			if value != nil {
				letOut(value, sh)
			}
		case "public_field_definition", "field_definition", "class_static_block":
			sh = shadow(sh, "ctor", "-inclass") // this: the new instance, or the class
			letOut(e.field(n, "value"), sh)
		case "pair":
			letOut(e.field(n, "value"), sh)
		case "assignment_pattern", "object_assignment_pattern":
			letOut(e.field(n, "right"), sh)
		case "template_substitution":
			for i := range n.NamedChildCount() {
				letOut(n.NamedChild(i), sh)
			}
		case "with_statement": // names resolve against an object at run time
			w.replaced = true
		case "identifier":
			if e.text(n) == "arguments" && !sh["fn"] { // the module wrapper's arguments
				w.replaced = true
			}
		case "member_expression", "subscript_expression":
			// The module's other ways to its exports: require.cache[…],
			// require.main, module.constructor._cache, module.parent,
			// module.children, and any key no name fixes.
			// Only what is reached through them counts: require.main ===
			// module and !module.parent read nothing of the exports.
			if inner := unwrap(e.field(n, "object")); e.typ(inner) == "member_expression" || e.typ(inner) == "subscript_expression" {
				if o := unwrap(e.field(inner, "object")); e.typ(o) == "identifier" {
					switch k := key(inner); {
					case e.text(o) == "process" && k == "mainModule": // process.mainModule.exports
						w.replaced = true
					case e.text(o) == "require" && !sh["require"] && (k == "cache" || k == "main" || k == "\x00"):
						w.replaced = true
					case e.text(o) == "module" && !sh["module"] && (k == "constructor" || k == "parent" || k == "children" || k == "__proto__"):
						w.replaced = true
					}
				}
			}
		case "shorthand_property_identifier":
			if t := e.text(n); t == "exports" && !sh[t] {
				w.all = true
			} else if t == "module" && !sh[t] {
				w.replaced = true
			}
		case "return_statement":
			// return this hands the object back to the caller, as a call of
			// a method returns its receiver: writes through the returned
			// value are beyond the scan (semantics.md, known limits).
			for i := range n.NamedChildCount() {
				if c := unwrap(n.NamedChild(i)); e.typ(c) != "this" {
					letOut(c, sh)
				}
			}
		case "array", "spread_element", "throw_statement", "yield_expression",
			"await_expression", "sequence_expression", "jsx_expression":
			for i := range n.NamedChildCount() {
				letOut(n.NamedChild(i), sh)
			}
		case "ternary_expression":
			letOut(e.field(n, "consequence"), sh)
			letOut(e.field(n, "alternative"), sh)
		case "binary_expression":
			// a && b is a only when a is falsy, never an object.
			if op := e.field(n, "operator"); op != nil {
				if o := e.text(op); o == "&&" || o == "||" || o == "??" {
					if o != "&&" {
						letOut(e.field(n, "left"), sh)
					}
					letOut(e.field(n, "right"), sh)
				}
			}
		case "call_expression", "new_expression":
			f, args := e.field(n, "function"), e.field(n, "arguments")
			if e.typ(n) == "new_expression" {
				f = e.field(n, "constructor")
			}
			if args == nil || e.typ(args) != "arguments" {
				break
			}
			first := 0
			fn := strings.Join(strings.Fields(e.text(unwrap(f))), "")
			if fn == "eval" && !sh["eval"] { // a direct eval runs in the module's scope
				w.replaced = true
			}
			// require('module') reaches Module._cache; require(__filename) is
			// this module's exports.
			if fn == "require" && !sh["require"] && args.NamedChildCount() > 0 {
				switch a := unwrap(args.NamedChild(0)); {
				case e.typ(a) == "identifier" && e.text(a) == "__filename",
					e.typ(a) == "string" && (e.staticKey(a) == "module" || e.staticKey(a) == "node:module"):
					w.replaced = true
				}
			}
			method := ""
			if c := unwrap(f); e.typ(c) == "member_expression" || e.typ(c) == "subscript_expression" {
				if k := key(c); k != "\x00" && object(e.field(c, "object"), sh) == "exports" {
					method = k
				}
			}
			switch {
			case exportsMutators[fn] && args.NamedChildCount() > 0 && object(args.NamedChild(0), sh) == "exports":
				first = 1
				switch fn {
				case "Object.defineProperty", "Reflect.defineProperty", "Reflect.set", "Reflect.deleteProperty":
					if args.NamedChildCount() > 1 {
						if k := e.staticKey(unwrap(args.NamedChild(1))); k != "" {
							w.names[k] = true
							break
						}
					}
					w.all = true
				case "Object.defineProperties":
					if args.NamedChildCount() > 1 {
						keys(args.NamedChild(1))
					} else {
						w.all = true
					}
				default: // Object.assign
					for i := 1; i < args.NamedChildCount(); i++ {
						keys(args.NamedChild(i))
					}
				}
			case exportsMethods[method]:
				if args.NamedChildCount() > 0 {
					if k := e.staticKey(unwrap(args.NamedChild(0))); k != "" {
						w.names[k] = true
						break
					}
				}
				w.all = true
			}
			for i := first; i < args.NamedChildCount(); i++ {
				letOut(args.NamedChild(i), sh)
			}
		}
		for i := range n.NamedChildCount() {
			visit(n.NamedChild(i), sh)
		}
	}
	visit(root, nil)
}

// cjsShadows returns module and exports when the parameters params bind
// them.
func (e *extractor) cjsShadows(params *ts.Node) []string {
	if params == nil {
		return nil
	}
	var out []string
	var bind func(n *ts.Node)
	bind = func(n *ts.Node) {
		switch e.typ(n) {
		case "identifier", "shorthand_property_identifier_pattern":
			if t := e.text(n); t == "module" || t == "exports" {
				out = append(out, t)
			}
			return
		case "assignment_pattern": // a default value is read, not bound
			bind(e.field(n, "left"))
			return
		case "pair_pattern":
			bind(e.field(n, "value"))
			return
		case "required_parameter", "optional_parameter": // TypeScript
			bind(e.field(n, "pattern"))
			return
		}
		for i := range n.NamedChildCount() {
			bind(n.NamedChild(i))
		}
	}
	bind(params)
	return out
}

// voidChain returns the assignments of asg = exports.b = exports.a = void 0
// (each to exports.x or module.exports.x, the last value void 0 or
// undefined), and whether asg is one.
func (e *extractor) voidChain(asg *ts.Node) ([]*ts.Node, bool) {
	var chain []*ts.Node
	n := asg
	for e.typ(n) == "assignment_expression" {
		if t := e.commonJSTarget(e.field(n, "left")); t == "" || t == "module.exports" || strings.ContainsRune(t, '\\') {
			return nil, false
		}
		chain = append(chain, n)
		n = e.field(n, "right")
	}
	switch {
	case e.typ(n) == "identifier" && e.text(n) == "undefined":
	case e.typ(n) == "unary_expression" && e.field(n, "operator") != nil && e.text(e.field(n, "operator")) == "void" &&
		e.typ(e.field(n, "argument")) == "number":
	default:
		return nil, false
	}
	return chain, len(chain) > 0
}
