package zig

import (
	"strconv"
	"strings"

	ts "github.com/odvcencio/gotreesitter"
)

// Closed and open builds.
//
// The module facts a build.zig states (buildEval) are what its code does
// to the build graph only if Ark follows every way that code may reach the
// graph: a closed build. Otherwise the build is open: what it binds, roots
// and registers is unknown — any module's, under any name (every module's
// names unknown, as when Ark cannot read a binding).
//
// Closedness rests on a fact of std.Build (Zig 0.17.0's source; neither
// std.Build nor the build runner keeps the graph in a global): code reaches
// the graph only through a value it is given — the build function's
// *std.Build, and what is derived from it (a module, an artifact, a step,
// a path, a dependency: each reaches the builder). Such values are
// tainted: a name holding one is tainted (flow-insensitively, every
// binding of the name in the file, through declarations, assignments,
// captures, parameters by every call passing one, results of functions
// returning one), and so is every expression a tainted one is part of,
// but for the few results std states hold no graph value (an option, a
// formatted string, a resolved target).
//
// Code may do with a graph value only what Ark models; any other use opens
// the build:
//
//   - a call taking one (receiver or argument) calls a function of the
//     file (its parameters tainted, its body read the same way), or a
//     std.Build method: one buildEval reads (createModule, addImport, ...),
//     one std states keeps every module's root, table and registration
//     (neutralMethods: Zig 0.17.0's source writes those only in Module.init
//     and create, addImport, addAnonymousImport, addOptions, and
//     Build.createModule / addModule, Options and TranslateC's
//     createModule / addModule), or a container's method (a list, a map
//     holding one: the container is tainted). Any other function — another
//     file's (a build helper), a dependency's build, a std function taking
//     it — may do anything with the graph;
//   - an assignment involving one writes a name, or a field std states
//     holds no module, root or table (neutralFields) of a graph value;
//     a field of another value, a dereference, an index, a container-level
//     var (another file reaches it through the build runner's root) may
//     hold what Ark does not follow;
//   - a builtin taking one is @as or @call (a call, as above);
//   - module options name only fields Zig 0.17.0's Module.CreateOptions or
//     an artifact's options have (another version's may bind or root).
//
// What Ark does not read opens the build too: syntax outside the forms
// below (allowedNodes; an error or missing node), nesting past maxDepth, a
// taint that does not settle.
//
// Uses of values that hold no graph value are free: an option, a string,
// a target, a computation on them cannot change the graph.

// openWhy is why the build's facts are not closed ("" when closed).
func (e *buildEval) openWhy(root *ts.Node) string {
	w := &world{e: e, x: e.x, tainted: map[string]bool{}, fnRet: map[string]bool{}, fns: map[string][]*ts.Node{}, resolved: map[nodeKey]string{}, fields: map[string]bool{}, keyNode: map[string]*ts.Node{}}
	return w.run(root)
}

// debugOpen, set by a test, sees why each build read is open.
var debugOpen func(file, why string)

// debugOpenAll, set by a test, sees every reason a build is open.
var debugOpenAll func(file, why string)

type world struct {
	e *buildEval
	x *extractor
	// tainted are the bindings that may hold a graph value: a declaration's
	// name, a parameter, a capture (bindingKey), or a name no binding of
	// the file declares (by its name).
	tainted map[string]bool
	// fnRet: a function of the name may return a graph value.
	fnRet map[string]bool
	// fns are the file's functions by name (nested ones too).
	fns      map[string][]*ts.Node
	resolved map[nodeKey]string
	// stdGiven: std code is given a graph value (givesStd).
	stdGiven bool
	// fields are the names of the fields the file's types declare.
	fields map[string]bool
	// keyNode is the declaration (or parameter) a binding key names.
	keyNode map[string]*ts.Node
	changed bool
	why     string
}

// givesStd reports a call of code not of the file (std's) taking a graph
// value as an argument.
func (w *world) givesStd(n *ts.Node) bool {
	x := w.x
	f := x.field(n, "function")
	name := ""
	switch {
	case f == nil:
		return false
	case x.typ(f) == "identifier":
		name = identifierName(x.text(f))
	case x.typ(f) == "field_expression":
		name = identifierName(x.text(x.field(f, "member")))
	}
	if _, fn := w.fns[name]; fn {
		return false
	}
	for _, a := range x.named(x.field(n, "arguments")) {
		if w.expr(a, 0) {
			return true
		}
	}
	return false
}

// taintMethods taints the parameters of the methods of the file's types.
func (w *world) taintMethods() {
	for _, fns := range w.fns {
		for _, fn := range fns {
			if p := fn.Parent(); p != nil && w.x.typ(p) != "source_file" {
				for i, name := range w.paramNames(fn) {
					w.bindAll(paramKey(fn, i), name)
				}
			}
		}
	}
}

const maxTaintRounds = 64

func (w *world) run(root *ts.Node) string {
	x := w.x
	ok := true
	var collect func(n *ts.Node, depth int)
	collect = func(n *ts.Node, depth int) {
		if n == nil || !ok {
			return
		}
		if depth > maxDepth {
			ok = false
			w.open("nesting past the depth Ark reads")
			return
		}
		if n.IsMissing() || n.IsError() || (n.IsNamed() && !allowedNodes[x.typ(n)]) {
			ok = false
			w.open("syntax Ark does not read: " + x.typ(n))
			return
		}
		if x.typ(n) == "function_declaration" {
			if nm := x.field(n, "name"); nm != nil {
				name := identifierName(x.text(nm))
				w.fns[name] = append(w.fns[name], n)
			}
		}
		if x.typ(n) == "container_field" {
			if nm := x.field(n, "name"); nm != nil {
				w.fields[identifierName(x.text(nm))] = true
			} else if id := x.firstNamed(n); id != nil && x.typ(id) == "identifier" {
				w.fields[identifierName(x.text(id))] = true
			}
		}
		for i := 0; i < n.ChildCount(); i++ {
			collect(n.Child(i), depth+1)
		}
	}
	collect(root, 0)
	if !ok {
		return w.why
	}
	// The build function's builder: the graph.
	for _, fn := range w.fns["build"] {
		if fn.Parent() == root {
			for i := range w.paramNames(fn) {
				w.taint(paramKey(fn, i))
			}
		}
	}
	settled := false
	for round := 0; round < maxTaintRounds; round++ {
		w.changed = false
		w.propagate(root, 0)
		if !w.changed {
			settled = true
			break
		}
	}
	if !settled {
		return w.open("a taint that does not settle")
	}
	w.check(root, 0)
	return w.why
}

func (w *world) open(why string) string {
	if w.why == "" {
		w.why = why
	}
	if debugOpenAll != nil {
		debugOpenAll(w.x.key, why)
	}
	return w.why
}

func (w *world) taint(key string) {
	if key != "" && !w.tainted[key] {
		w.tainted[key] = true
		w.changed = true
	}
}

// Binding keys.
func declKey(st *ts.Node, name string) string { return "d:" + nodeID(st) + ":" + name }
func paramKey(fn *ts.Node, i int) string      { return "p:" + nodeID(fn) + ":" + strconv.Itoa(i) }
func captureKey(p *ts.Node, name string) string {
	return "c:" + nodeID(p) + ":" + name
}
func nameKey(name string) string { return "n:" + name }

func nodeID(n *ts.Node) string {
	k := keyOf(n)
	return strconv.Itoa(int(k.start)) + "-" + strconv.Itoa(int(k.end)) + "-" + strconv.Itoa(int(k.typ))
}

// paramNames are function fn's parameter names in order ("" for `_` or
// an unnamed one).
func (w *world) paramNames(fn *ts.Node) []string {
	x := w.x
	var out []string
	for _, p := range x.named(x.childOfType(fn, "parameters")) {
		name := ""
		if pn := x.field(p, "name"); pn != nil {
			name = identifierName(x.text(pn))
		}
		out = append(out, name)
	}
	return out
}

// resolve is the binding identifier n names (Zig's scopes: a block's
// declarations before n, a capture of what encloses n, a function's
// parameters, a container's declarations in any order — no name is
// shadowed); a name of no binding is keyed by itself.
func (w *world) resolve(n *ts.Node) string {
	k := keyOf(n)
	if r, ok := w.resolved[k]; ok {
		return r
	}
	r := w.resolveName(n, identifierName(w.x.text(n)))
	w.resolved[k] = r
	return r
}

func (w *world) resolveName(n *ts.Node, name string) string {
	x := w.x
	child := n
	for a := n.Parent(); a != nil; child, a = a, a.Parent() {
		// A capture of a, in scope after its payload.
		for _, ch := range x.named(a) {
			if keyOf(ch) == keyOf(child) {
				break
			}
			if x.typ(ch) == "payload" {
				for _, id := range x.named(ch) {
					if x.typ(id) == "identifier" && identifierName(x.text(id)) == name {
						return captureKey(ch, name)
					}
				}
			}
		}
		switch x.typ(a) {
		case "block":
			for _, st := range x.named(a) {
				if keyOf(st) == keyOf(child) {
					break
				}
				if x.typ(st) == "variable_declaration" && (x.hasToken(st, "const") || x.hasToken(st, "var")) {
					for _, d := range w.e.declNames(st) {
						if d == name {
							w.keyNode[declKey(st, name)] = st
							return declKey(st, name)
						}
					}
				}
			}
		case "function_declaration":
			for i, p := range w.paramNames(a) {
				if p == name {
					w.keyNode[paramKey(a, i)] = x.named(x.childOfType(a, "parameters"))[i]
					return paramKey(a, i)
				}
			}
		case "source_file", "struct_declaration", "union_declaration", "enum_declaration", "opaque_declaration":
			for _, st := range x.named(a) {
				switch x.typ(st) {
				case "variable_declaration":
					for _, d := range w.e.declNames(st) {
						if d == name {
							w.keyNode[declKey(st, name)] = st
							return declKey(st, name)
						}
					}
				case "function_declaration":
					if nm := x.field(st, "name"); nm != nil && identifierName(x.text(nm)) == name {
						return "f:" + name // a function: no graph value
					}
				}
			}
		}
	}
	return nameKey(name)
}

// bindAll taints, for a declaration, assignment or capture, the binding
// key and the name (what a name Ark cannot resolve reads).
func (w *world) bindAll(key, name string) {
	w.taint(key)
	if name != "" && name != "_" {
		w.taint(nameKey(name))
	}
}

// propagate states each binding's taint from what it is bound to.
func (w *world) propagate(n *ts.Node, depth int) {
	x := w.x
	if n == nil || depth > maxDepth {
		return
	}
	switch x.typ(n) {
	case "variable_declaration":
		if r := x.initializer(n); r != nil && w.expr(r, 0) {
			if x.hasToken(n, "const") || x.hasToken(n, "var") {
				for _, name := range w.e.declNames(n) {
					w.bindAll(declKey(n, name), name)
				}
			} else {
				w.taintTarget(x.firstNamed(n)) // v = value, s.f = value, p.* = value
			}
		}
	case "assignment_expression":
		if p := n.Parent(); p == nil || x.typ(p) != "initializer_list" {
			if r := x.field(n, "right"); r != nil && w.expr(r, 0) {
				w.taintTarget(x.field(n, "left"))
			}
		}
	case "payload":
		if w.controlling(n) {
			for _, id := range x.named(n) {
				if x.typ(id) == "identifier" {
					name := identifierName(x.text(id))
					w.bindAll(captureKey(n, name), name)
				}
			}
		}
	case "return_expression":
		if v := x.firstNamed(n); v != nil && w.expr(v, 0) {
			if fn := enclosingFunction(x, n); fn != "" && !w.fnRet[fn] {
				w.fnRet[fn] = true
				w.changed = true
			}
		}
	case "call_expression":
		w.callTaint(x.field(n, "function"), x.named(x.field(n, "arguments")))
		if !w.stdGiven && w.givesStd(n) {
			// std given a graph value may call, by convention, the methods
			// of the file's types it is given (format, hash, eql,
			// lessThan, ...): anything may be their arguments.
			w.stdGiven = true
			w.changed = true
		}
		if w.stdGiven {
			w.taintMethods()
		}
	case "builtin_function":
		if x.builtinName(n) == "@call" {
			if args := x.builtinArgs(n); len(args) == 3 && x.typ(args[2]) == "anonymous_struct_initializer" {
				w.callTaint(args[1], x.named(x.firstNamed(args[2])))
			} else if len(args) == 3 && w.expr(args[2], 0) {
				w.allParams(args[1])
			}
		}
	case "identifier":
		// A function of the file used as a value: what calls it is what it
		// is given to — tainted when that call takes a graph value, or when
		// it is held (anything may call it then).
		name := identifierName(x.text(n))
		if _, fn := w.fns[name]; fn && !w.calleeOrDeclared(n) {
			if c := enclosingCall(x, n); c == nil || w.callInputs(c) {
				for _, f := range w.fns[name] {
					for i := range w.paramNames(f) {
						w.taint(paramKey(f, i))
					}
				}
			}
		}
	}
	for _, ch := range x.named(n) {
		w.propagate(ch, depth+1)
	}
}

// taintTarget taints what an assignment target writes: the binding it is
// part of (`s` of `s.f[i].*`).
func (w *world) taintTarget(l *ts.Node) {
	x := w.x
	for depth := 0; l != nil && depth < maxEvalDepth; depth++ {
		switch x.typ(l) {
		case "identifier":
			if identifierName(x.text(l)) != "_" {
				w.bindAll(w.resolve(l), identifierName(x.text(l)))
			}
			return
		case "field_expression":
			l = x.field(l, "object")
		default:
			l = x.firstNamed(l)
		}
	}
}

// callTaint taints the parameters of the file's function a call of callee
// f taking a graph value may call — by position (a method's receiver its
// first parameter, its arguments the next) —, and a container a method of
// it is given one.
func (w *world) callTaint(f *ts.Node, args []*ts.Node) {
	x := w.x
	if f == nil {
		return
	}
	var obj *ts.Node
	name := ""
	switch x.typ(f) {
	case "identifier":
		name = identifierName(x.text(f))
	case "field_expression":
		obj = x.field(f, "object")
		name = identifierName(x.text(x.field(f, "member")))
	default:
		return
	}
	objT := obj != nil && w.expr(obj, 0)
	argT := make([]bool, len(args))
	any := objT
	for i, a := range args {
		argT[i] = w.expr(a, 0)
		any = any || argT[i]
	}
	if !any {
		return
	}
	for _, fn := range w.fns[name] {
		ps := w.paramNames(fn)
		set := func(i int) {
			if i >= 0 && i < len(ps) {
				w.bindAll(paramKey(fn, i), ps[i])
			}
		}
		if objT {
			set(0)
		}
		for i, t := range argT {
			if t {
				set(i)
				if obj != nil {
					set(i + 1)
				}
			}
		}
		if len(args) > len(ps) {
			for i := range ps {
				set(i) // variadic in Ark's eyes: every parameter
			}
		}
	}
	if obj != nil && !objT && containerMethods[name] && w.runtimeValue(obj) {
		// list.append(gpa, m): the container holds it.
		w.taintTarget(obj)
	}
}

// allParams taints every parameter of the file's functions f may name.
func (w *world) allParams(f *ts.Node) {
	x := w.x
	name := ""
	switch {
	case f == nil:
		return
	case x.typ(f) == "identifier":
		name = identifierName(x.text(f))
	case x.typ(f) == "field_expression":
		name = identifierName(x.text(x.field(f, "member")))
	}
	for _, fn := range w.fns[name] {
		for i, p := range w.paramNames(fn) {
			w.bindAll(paramKey(fn, i), p)
		}
	}
}

// callInputs reports a call whose receiver or arguments may be a graph
// value.
func (w *world) callInputs(n *ts.Node) bool {
	x := w.x
	if f := x.field(n, "function"); f != nil && x.typ(f) == "field_expression" {
		if obj := x.field(f, "object"); obj != nil && w.expr(obj, 0) {
			return true
		}
	}
	for _, a := range x.named(x.field(n, "arguments")) {
		if w.expr(a, 0) {
			return true
		}
	}
	return false
}

// controlling reports a capture of a value that may be a graph value: what
// the if, while, for, switch or catch unwraps.
func (w *world) controlling(p *ts.Node) bool {
	x := w.x
	parent := p.Parent()
	if parent == nil {
		return true
	}
	switch x.typ(parent) {
	case "switch_case":
		if sw := parent.Parent(); sw != nil {
			for _, ch := range x.named(sw) {
				if x.typ(ch) != "switch_case" {
					return w.expr(ch, 0)
				}
			}
		}
		return true
	case "else_clause":
		if gp := parent.Parent(); gp != nil {
			return w.before(gp, parent)
		}
		return true
	}
	return w.before(parent, p)
}

// before reports a graph value among the named children of n before child.
func (w *world) before(n, child *ts.Node) bool {
	for _, ch := range w.x.named(n) {
		if keyOf(ch) == keyOf(child) {
			break
		}
		if w.x.typ(ch) != "payload" && w.expr(ch, 0) {
			return true
		}
	}
	return false
}

// calleeOrDeclared reports an identifier that is a call's callee, a
// declaration's name or a member name.
func (w *world) calleeOrDeclared(n *ts.Node) bool {
	x := w.x
	p := n.Parent()
	if p == nil {
		return false
	}
	switch {
	case x.typ(p) == "call_expression" && keyOf(x.field(p, "function")) == keyOf(n):
		return true
	case keyOf(x.field(p, "name")) == keyOf(n):
		return true
	case x.typ(p) == "field_expression" && keyOf(x.field(p, "member")) == keyOf(n):
		return true
	}
	return false
}

// expr reports an expression that may be (or hold) a graph value.
func (w *world) expr(n *ts.Node, depth int) bool {
	x := w.x
	if n == nil {
		return false
	}
	if depth > maxEvalDepth {
		return true
	}
	switch x.typ(n) {
	case "identifier":
		if x.primitive(n) {
			return false
		}
		if w.calleeOrDeclared(n) {
			return false
		}
		return w.tainted[w.resolve(n)]
	case "string", "multiline_string", "integer", "float", "character", "boolean", "builtin_type",
		"comment", "error_type", "struct_declaration", "union_declaration", "enum_declaration",
		"opaque_declaration", "error_set_declaration", "function_declaration", "pointer_type",
		"slice_type", "array_type", "nullable_type", "error_union_type", "anyframe_type":
		return false
	case "builtin_function":
		name := x.builtinName(n)
		if pureBuiltins[name] {
			return false
		}
		for _, a := range x.builtinArgs(n) {
			if w.expr(a, depth+1) {
				return true
			}
		}
		return false
	case "field_expression":
		obj := x.field(n, "object")
		if obj == nil {
			return false // .literal
		}
		if !w.expr(obj, depth+1) {
			return false
		}
		// A field std states holds no graph value — unless a type of the
		// file declares a field of the name (what it holds Ark does not
		// type).
		f := identifierName(x.text(x.field(n, "member")))
		return !graphFreeFields[f] || w.fields[f]
	case "binary_expression":
		switch binaryOperator(x, n) {
		case "orelse", "catch", "++", "**":
		default:
			return false // a comparison, arithmetic, logic: no graph value
		}
	case "call_expression":
		f := x.field(n, "function")
		if f != nil && x.typ(f) == "identifier" {
			name := identifierName(x.text(f))
			if _, fn := w.fns[name]; fn {
				return w.fnRet[name]
			}
		}
		if f != nil && x.typ(f) == "field_expression" {
			m := identifierName(x.text(x.field(f, "member")))
			_, fn := w.fns[m]
			if fn && w.fnRet[m] {
				return true
			}
			if path, std := w.stdPath(f); graphFreeResults[m] && !std || std && graphFreeStd[path] {
				return false
			}
		}
	}
	for _, ch := range x.named(n) {
		if w.expr(ch, depth+1) {
			return true
		}
	}
	return false
}

// check opens the build at the first use of a graph value Ark does not
// model.
func (w *world) check(n *ts.Node, depth int) {
	x := w.x
	if n == nil || (w.why != "" && debugOpenAll == nil) {
		return
	}
	at := func(n *ts.Node) string { return " (line " + strconv.Itoa(int(n.StartPoint().Row)+1) + ")" }
	switch x.typ(n) {
	case "call_expression":
		if w.callInputs(n) && !w.modeledCall(x.field(n, "function"), x.named(x.field(n, "arguments"))) {
			detail := ""
			if debugOpenAll != nil {
				for _, a := range x.named(x.field(n, "arguments")) {
					if w.expr(a, 0) {
						detail += " [" + collapse(x.text(a)) + "]"
					}
				}
			}
			w.open("a call Ark does not model taking a graph value: " + collapse(x.text(x.field(n, "function"))) + at(n) + detail)
			return
		}
	case "builtin_function":
		name := x.builtinName(n)
		tainted := false
		for _, a := range x.builtinArgs(n) {
			tainted = tainted || w.expr(a, 0)
		}
		switch {
		case !tainted || name == "@as" || pureBuiltins[name]:
		case name == "@call":
			if args := x.builtinArgs(n); len(args) != 3 || x.typ(args[2]) != "anonymous_struct_initializer" ||
				!w.modeledCall(args[1], x.named(x.firstNamed(args[2]))) {
				w.open("@call Ark does not model taking a graph value" + at(n))
				return
			}
		default:
			w.open("a builtin taking a graph value: " + name + at(n))
			return
		}
	case "variable_declaration":
		if !x.hasToken(n, "const") && !x.hasToken(n, "var") {
			if why := w.assignment(x.firstNamed(n), x.lastNamed(n)); why != "" {
				w.open(why + at(n))
				return
			}
		} else if x.hasToken(n, "var") && containerLevel(x, n) {
			for _, name := range w.e.declNames(n) {
				if w.tainted[declKey(n, name)] {
					w.open("a container-level var holding a graph value: " + name + at(n))
					return
				}
			}
		}
	case "assignment_expression":
		if p := n.Parent(); p == nil || x.typ(p) != "initializer_list" {
			if why := w.assignment(x.field(n, "left"), x.field(n, "right")); why != "" {
				w.open(why + at(n))
				return
			}
		}
	case "field_expression":
		// A table or the registered modules, reached other than as
		// buildEval reads (a put, a copy's iterator).
		if obj := x.field(n, "object"); obj != nil {
			switch identifierName(x.text(x.field(n, "member"))) {
			case "import_table", "modules":
				p := n.Parent()
				ok := p != nil && x.typ(p) == "field_expression" && keyOf(x.field(p, "object")) == keyOf(n)
				if ok {
					gp := p.Parent()
					ok = gp != nil && x.typ(gp) == "call_expression" && keyOf(x.field(gp, "function")) == keyOf(p) &&
						w.modeledCall(p, x.named(x.field(gp, "arguments")))
				}
				if !ok {
					w.open("a table or the registered modules used directly" + at(n))
					return
				}
			}
		}
	case "anonymous_struct_initializer":
		if x.option(n, "root_source_file") != nil || x.option(n, "imports") != nil || x.option(n, "root_module") != nil {
			for _, a := range x.named(x.firstNamed(n)) {
				l := x.field(a, "left")
				if x.typ(a) != "assignment_expression" || l == nil || x.typ(l) != "field_expression" || x.field(l, "object") != nil {
					continue
				}
				if f := identifierName(x.text(x.field(l, "member"))); !optionFields[f] {
					w.open("module or artifact options with a field Ark does not know: " + f + at(n))
					return
				}
			}
		}
		for _, a := range x.named(x.firstNamed(n)) {
			if l := x.field(a, "left"); x.typ(a) == "assignment_expression" && l != nil && x.typ(l) == "field_expression" && x.field(l, "object") == nil {
				if f := identifierName(x.text(x.field(l, "member"))); f == "dependencies" || f == "source_file" || f == "main_pkg_path" || f == "main_mod_path" {
					w.open("options of another Zig version's module API: " + f + at(n))
					return
				}
			}
		}
	}
	for _, ch := range x.named(n) {
		w.check(ch, depth+1)
	}
}

// assignment is why an assignment of r to l opens the build ("" when it
// does not).
func (w *world) assignment(l, r *ts.Node) string {
	x := w.x
	if l == nil {
		return ""
	}
	lt, rt := w.expr(l, 0), w.expr(r, 0)
	switch x.typ(l) {
	case "identifier":
		if identifierName(x.text(l)) == "_" || !rt {
			return ""
		}
		return "" // a name: its every value is tainted, its uses read (buildEval: a var)
	case "field_expression":
		obj := x.field(l, "object")
		f := identifierName(x.text(x.field(l, "member")))
		if !lt && !rt {
			return ""
		}
		if obj != nil && w.expr(obj, 0) && neutralFields[f] {
			return ""
		}
		return "an assignment Ark does not model of a graph value: ." + f
	}
	if lt || rt {
		return "an assignment Ark does not model of a graph value: " + x.typ(l)
	}
	return ""
}

// modeledCall reports a call of callee f with args that takes a graph
// value Ark models: a function of the file, a std.Build method buildEval
// reads or one keeping every module's facts, a container's method, a std
// function that only reads.
func (w *world) modeledCall(f *ts.Node, args []*ts.Node) bool {
	x := w.x
	if f == nil {
		return false
	}
	if sh, ok := w.e.shapeOfCallee(f, args); ok && sh.obj != nil && keyOf(sh.obj) != keyOf(x.field(f, "object")) {
		// Through the type: std.Build.Module.addImport(m, ...), or a
		// constant naming it.
		path := f
		if x.typ(f) == "identifier" {
			if st := w.keyNode[w.resolve(f)]; st != nil {
				path = x.initializer(st)
			}
		}
		_, std := w.stdPath(x.field(path, "object"))
		return std && modeledMethods[sh.method]
	}
	switch x.typ(f) {
	case "identifier":
		// A function of the file the name resolves to (Zig's scopes).
		return strings.HasPrefix(w.resolve(f), "f:")
	case "field_expression":
		obj := x.field(f, "object")
		m := identifierName(x.text(x.field(f, "member")))
		if obj == nil {
			return false
		}
		if _, fn := w.fns[m]; fn && !oldModuleAPI[m] && !modeledMethods[m] && (w.expr(obj, 0) || w.fileType(obj)) {
			return true // a method of the file's (its body is read)
		}
		if tableReceiver(x, obj) {
			// A table's put buildEval reads, the iterator of a copy it
			// reads; any other operation on a table or on the registered
			// modules is not followed.
			return (putMethods[m] && (len(args) == 3 || len(args) == 4)) || (m == "iterator" && w.e.copyIters[keyOf(obj)])
		}
		if w.expr(obj, 0) {
			return modeledMethods[m] || neutralMethods[m] || graphFreeResults[m] || containerMethods[m]
		}
		// A graph value given to a function of std (by its path from
		// `@import("std")`), or to a method of a value holding none — a
		// container: a runtime value, never a namespace.
		if path, std := w.stdPath(f); std {
			kind := strings.TrimPrefix(strings.TrimSuffix(path, ".create"), "std.Build.Step.")
			switch {
			case path == "std.Build.Module.create":
				return true
			case strings.HasSuffix(path, ".create") && stepKinds[kind]:
				return true // a step
			case readOnlyStd[path]:
				return true
			}
			return false
		}
		return containerMethods[m] && w.runtimeValue(obj)
	}
	return false
}

// fileType reports a receiver that is a type of the file (a constant
// naming a container it declares, @This()) or a value declared of one: its
// methods are the file's, never another file's functions.
func (w *world) fileType(n *ts.Node) bool {
	x := w.x
	for depth := 0; n != nil && depth < maxEvalDepth; depth++ {
		switch x.typ(n) {
		case "builtin_function":
			return x.builtinName(n) == "@This"
		case "field_expression":
			n = x.field(n, "object")
			continue
		case "struct_declaration", "union_declaration", "enum_declaration", "opaque_declaration":
			return true
		case "pointer_type", "nullable_type", "parenthesized_expression":
			n = x.lastNamed(n)
			continue
		case "identifier":
			d := w.keyNode[w.resolve(n)]
			if d == nil {
				return false
			}
			switch x.typ(d) {
			case "variable_declaration":
				if t := x.field(d, "type"); t != nil {
					n = t // a value declared of a type
					continue
				}
				if !x.hasToken(d, "const") {
					return false
				}
				n = x.initializer(d)
				continue
			case "parameter":
				n = x.field(d, "type")
				continue
			}
			return false
		}
		return false
	}
	return false
}

// stdPath is the path of a field expression rooted at the file's
// `const std = @import("std")` ("std.mem.eql"); ok is false for another
// root.
func (w *world) stdPath(n *ts.Node) (string, bool) {
	x := w.x
	var members []string
	cur := n
	for cur != nil && x.typ(cur) == "field_expression" {
		members = append([]string{identifierName(x.text(x.field(cur, "member")))}, members...)
		cur = x.field(cur, "object")
	}
	if cur == nil || x.typ(cur) != "identifier" {
		return "", false
	}
	st := w.keyNode[w.resolve(cur)]
	if st == nil || x.typ(st) != "variable_declaration" || !x.hasToken(st, "const") {
		return "", false
	}
	init := x.initializer(st)
	if init == nil || x.typ(init) != "builtin_function" || x.builtinName(init) != "@import" {
		return "", false
	}
	if a := x.builtinArgs(init); len(a) != 1 {
		return "", false
	} else if spec, ok := x.stringLiteral(a[0]); !ok || spec != "std" {
		return "", false
	}
	return strings.Join(append([]string{"std"}, members...), "."), true
}

// runtimeValue reports a receiver that is a value at run time — a var
// (not comptime), a parameter of a declared type other than type and
// anytype (not comptime) — or a field of one: never a namespace.
func (w *world) runtimeValue(n *ts.Node) bool {
	x := w.x
	for depth := 0; n != nil && x.typ(n) == "field_expression" && depth < maxEvalDepth; depth++ {
		n = x.field(n, "object")
	}
	if n == nil || x.typ(n) != "identifier" {
		return false
	}
	key := w.resolve(n)
	d := w.keyNode[key]
	switch {
	case d == nil:
		return false
	case strings.HasPrefix(key, "d:"):
		return x.hasToken(d, "var") && !x.hasToken(d, "comptime")
	case strings.HasPrefix(key, "p:"):
		t := strings.TrimSpace(x.text(x.field(d, "type")))
		return t != "" && t != "type" && t != "anytype" && !x.hasToken(d, "comptime")
	}
	return false
}

// tableReceiver reports `x.import_table`, `x.modules`.
func tableReceiver(x *extractor, n *ts.Node) bool {
	if x.typ(n) != "field_expression" {
		return false
	}
	switch identifierName(x.text(x.field(n, "member"))) {
	case "import_table", "modules":
		return true
	}
	return false
}

// containerLevel reports a declaration of a container (the file's, a
// struct's), not of a block.
func containerLevel(x *extractor, n *ts.Node) bool {
	p := n.Parent()
	if p == nil {
		return false
	}
	switch x.typ(p) {
	case "source_file", "struct_declaration", "union_declaration", "enum_declaration", "opaque_declaration":
		return true
	}
	return false
}

// enclosingFunction is the name of the function n is in, "" for none.
func enclosingFunction(x *extractor, n *ts.Node) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if x.typ(p) == "function_declaration" {
			return identifierName(x.text(x.field(p, "name")))
		}
	}
	return ""
}

// enclosingCall is the call n is an argument of (through literals), or nil.
func enclosingCall(x *extractor, n *ts.Node) *ts.Node {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch x.typ(p) {
		case "arguments":
			return p.Parent()
		case "anonymous_struct_initializer", "initializer_list", "assignment_expression", "unary_expression", "parenthesized_expression":
			continue
		}
		return nil
	}
	return nil
}

// binaryOperator is the operator token of binary expression n.
func binaryOperator(x *extractor, n *ts.Node) string {
	for i := 0; i < n.ChildCount(); i++ {
		if ch := n.Child(i); !ch.IsNamed() {
			return x.typ(ch)
		}
	}
	return ""
}

// allowedNodes are the syntax forms whose effect on graph values the world
// reads (Zig grammar's named nodes, using_namespace, asm, async, await,
// suspend, resume and nosuspend excepted).
var allowedNodes = setOf(
	"source_file", "comment", "identifier", "builtin_identifier", "string", "string_content",
	"escape_sequence", "multiline_string", "character", "character_content", "integer", "float",
	"boolean", "builtin_type", "error_type",
	"test_declaration", "comptime_declaration", "container_field", "variable_declaration",
	"function_declaration", "function_signature", "parameters", "parameter",
	"block", "struct_declaration", "opaque_declaration", "enum_declaration", "union_declaration",
	"error_set_declaration", "comptime_statement", "defer_statement", "errdefer_statement",
	"block_expression", "labeled_statement", "expression_statement", "if_statement", "else_clause",
	"for_statement", "while_statement", "payload", "byte_alignment", "address_space",
	"link_section", "calling_convention", "if_expression", "for_expression", "while_expression",
	"assignment_expression", "unary_expression", "binary_expression", "comptime_expression",
	"continue_expression", "return_expression", "break_expression", "try_expression",
	"catch_expression", "switch_expression", "switch_case", "type_expression",
	"primary_type_expression", "nullable_type", "anyframe_type", "slice_type", "pointer_type",
	"array_type", "error_union_type", "field_expression", "index_expression",
	"dereference_expression", "null_coercion_expression", "range_expression", "call_expression",
	"anonymous_struct_initializer", "struct_initializer", "initializer_list", "field_initializer",
	"labeled_type_expression", "comptime_type_expression", "if_type_expression",
	"parenthesized_expression", "block_label", "break_label", "arguments", "builtin_function",
)

// pureBuiltins return no graph value, whatever they are given (a type,
// its information, a name, a size).
var pureBuiltins = setOf(
	"@import", "@This", "@TypeOf", "@typeInfo", "@typeName", "@sizeOf", "@alignOf", "@bitSizeOf",
	"@hasDecl", "@hasField", "@tagName", "@errorName", "@intFromEnum", "@src", "@compileError",
	"@compileLog", "@embedFile", "@panic", "@intFromBool", "@inComptime", "@intFromPtr",
)

// modeledMethods are the calls buildEval reads (std.Build and its
// Module's, ArrayHashMap.put on a table).
var modeledMethods = setOf(
	"createModule", "addModule", "create", "addImport", "addAnonymousImport", "addOptions",
	"addExecutable", "addTest", "addLibrary", "addObject", "addSharedLibrary", "addStaticLibrary",
	"module", "path",
)

// oldModuleAPI are module operations of other Zig versions (0.10–0.11's
// packages, anonymous modules, a module's root directory): never a
// function of the file's to Ark.
var oldModuleAPI = setOf(
	"addPackage", "addPackagePath", "addAnonymousModule", "addAnonymousDependency",
	"setMainPkgPath", "addModule", "init",
)

// neutralMethods are std.Build methods (of Build, Dependency, Step and
// its kinds, Module, LazyPath) Zig 0.17.0's source states never create,
// root, bind or register a module: none calls Module.init, create,
// addImport, addAnonymousImport, addOptions, a createModule or addModule.
var neutralMethods = setOf(
	// std.Build
	"step", "addRunArtifact", "addSystemCommand", "addInstallArtifact", "installArtifact",
	"addWriteFiles", "addInstallFile", "addInstallFileWithDir", "installFile", "addInstallDirectory",
	"installDirectory", "addInstallBinFile", "addInstallLibFile", "addInstallHeaderFile",
	"installBinFile", "installLibFile", "getInstallStep", "getUninstallStep", "dependency",
	"lazyDependency", "addFmt", "addCheckFile", "addUpdateSourceFiles",
	"addConfigHeader", "addObjCopy", "addFail", "addTranslateC",
	"dependencyFromBuildZig", "addNamedWriteFiles",
	// std.Build.Dependency
	"artifact", "namedWriteFiles", "namedLazyPath",
	// std.Build.Step
	"dependOn", "cast",
	// std.Build.Step.UpdateSourceFiles, Options
	"addCopyFileToSource", "addOptionPath",
	// std.Build.Step.Run
	"addArg", "addArgs", "addArtifactArg", "addFileArg", "addFileInput", "addPrefixedFileArg",
	"addDirectoryArg", "addPrefixedDirectoryArg", "addOutputFileArg", "addPrefixedOutputFileArg",
	"addOutputDirectoryArg", "addPrefixedOutputDirectoryArg", "addDepFileOutputArg",
	"addPrefixedDepFileOutputArg", "addDecoratedDirectoryArg", "captureStdOut", "captureStdErr",
	"setCwd", "setEnvironmentVariable", "removeEnvironmentVariable", "getEnvMap", "expectExitCode",
	"expectStdOutEqual", "expectStdErrEqual", "setStdIn", "addCheck", "enableTestRunnerMode", "clearEnvironment", "setName", "addPassthruArgs", "addFileArg2",
	// std.Build.Step.Compile
	"linkLibrary", "linkSystemLibrary",
	"linkFramework", "addIncludePath",
	"addSystemIncludePath", "addAfterIncludePath", "addConfigHeader", "addCSourceFile",
	"addCSourceFiles", "addCMacro", "addLibraryPath", "addRPath", "addRPathSpecial",
	"addFrameworkPath", "addSystemFrameworkPath", "addObjectFile", "addAssemblyFile",
	"addWin32ResourceFile", "addEmbedPath", "setLinkerScript", "setVersionScript",
	"getEmittedBin", "getEmittedImplib", "getEmittedH", "getEmittedPdb", "getEmittedDocs",
	"getEmittedAsm", "getEmittedLlvmIr", "getEmittedLlvmBc", "installHeader",
	"installHeadersDirectory", "installConfigHeader", "installLibraryHeaders", "getEmittedIncludeTree",
	"producesPdbFile", "producesImplib", "isDynamicLibrary", "isStaticLibrary", "isDll", "forceUndefinedSymbol", "addObject",
	// std.Build.Step.WriteFile, InstallDir, Options, TranslateC, ConfigHeader
	"add", "addCopyFile", "addCopyDirectory", "getDirectory", "addOption", "getOutput",
	"addValues", "addValue", "getOutputFile",
	// std.Build.LazyPath
	"getDisplayName", "dupe", "dirname", "basename", "join", "addStepDependencies", "getPath",
)

// graphFreeResults are std.Build methods whose result holds no graph value
// (Zig 0.17.0: an option, a string, a target, a mode, a flag).
var graphFreeResults = setOf(
	"option", "standardTargetOptions", "standardOptimizeOption", "resolveTargetQuery", "fmt",
	"dupe", "dupeStrings", "dupePath", "pathJoin", "pathResolve", "findProgram",
	"run", "runAllowFail", "systemIntegrationOption", "standardTargetOptionsQueryOnly",
	"getPath", "getDisplayName",
)

// graphFreeFields are fields of a graph value that hold none (Zig 0.17.0:
// the builder's allocator and directories, a resolved target, a name).
var graphFreeFields = setOf(
	"allocator", "build_root", "cache_root", "global_cache_root", "install_path", "install_prefix",
	"args", "host", "result", "name", "kind", "zig_exe", "sysroot", "release_mode", "io", "root",
)

// neutralFields are fields of a graph value Zig 0.17.0 states hold no
// module, root, table or registration: settings a build may assign.
var neutralFields = setOf(
	"has_side_effects", "stdio", "stdio_limit", "cwd", "linkage",
	"link_gc_sections", "link_function_sections", "link_data_sections", "pie", "rdynamic",
	"bundle_compiler_rt", "bundle_ubsan_rt", "entry", "dll_export_fns",
	"linker_allow_shlib_undefined", "headerpad_max_install_names", "dead_strip_dylibs",
	"each_lib_rpath", "install_name", "entitlements", "pagezero_size", "stack_size",
	"compress_debug_sections", "lto", "use_llvm", "use_lld", "skip_foreign_checks",
	"failing_to_execute_foreign_is_an_error", "producer", "version", "emit_object", "filters", "image_base", "link_eh_frame_hdr",
	"link_emit_relocs", "link_z_notext", "link_z_relro", "link_z_lazy", "link_z_common_page_size",
	"link_z_max_page_size", "force_load_objc", "discard_local_symbols", "zig_lib_dir", "export_memory", "import_memory", "import_symbols",
	"initial_memory", "max_memory", "shared_memory", "global_base",
	// std.Build.Module's settings (CreateOptions' own), a target's parts.
	"resolved_target", "optimize", "dwarf_format", "strip", "unwind_tables", "single_threaded",
	"stack_protector", "stack_check", "sanitize_c", "sanitize_thread", "fuzz", "code_model",
	"valgrind", "pic", "red_zone", "omit_frame_pointer", "error_tracing", "link_libc", "link_libcpp",
	"no_builtin", "patchable_function_entry", "max_rss", "abi", "os_tag", "cpu_arch",
)

// optionFields are the fields Zig 0.17.0's Module.CreateOptions and an
// artifact's options have, and an artifact's earlier root forms
// (root_source_file and its module settings, before 0.15).
var optionFields = setOf(
	"root_source_file", "imports", "target", "optimize", "link_libc", "link_libcpp",
	"single_threaded", "strip", "unwind_tables", "dwarf_format", "code_model", "stack_protector",
	"stack_check", "sanitize_c", "sanitize_thread", "fuzz", "valgrind", "pic", "red_zone",
	"omit_frame_pointer", "error_tracing", "no_builtin", "patchable_function_entry",
	"name", "root_module", "version", "linkage", "max_rss", "use_llvm", "use_lld", "zig_lib_dir",
	"filters", "test_runner", "emit_object",
)

// containerMethods are methods of std's containers (lists, maps, sets,
// iterators) a graph value may be given to or read from: they hold it,
// never do anything with it.
var containerMethods = setOf(
	"append", "appendSlice", "appendAssumeCapacity", "appendSliceAssumeCapacity", "appendNTimes",
	"insert", "insertSlice", "put", "putNoClobber", "putAssumeCapacity", "putAssumeCapacityNoClobber",
	"getOrPut", "getOrPutValue", "getOrPutAssumeCapacity", "get", "getPtr", "getEntry", "contains",
	"iterator", "next", "toOwnedSlice", "deinit", "clearRetainingCapacity", "clearAndFree",
	"ensureTotalCapacity", "ensureUnusedCapacity", "pop", "popOrNull", "orderedRemove",
	"swapRemove", "fetchPut", "fetchRemove", "remove", "keys", "values", "count", "items",
	"addOne", "addOneAssumeCapacity",
)

// stepKinds are the steps of Zig 0.17.0 whose create (Step.X.create(b,
// ...)) makes a step: none creates, roots, binds or registers a module.
var stepKinds = setOf(
	"CheckFile", "Compile", "ConfigHeader", "Fail", "FindProgram", "Fmt", "InstallArtifact",
	"InstallDir", "InstallFile", "ObjCopy", "Options", "Run", "UpdateSourceFiles", "WriteFile",
)

// readOnlyStd are std functions that read the values they are given
// (slices, format arguments) and keep none: a graph value given to one is
// not changed.
var readOnlyStd = setOf(
	"std.mem.eql", "std.mem.startsWith", "std.mem.endsWith", "std.mem.indexOf",
	"std.mem.indexOfScalar", "std.mem.indexOfAny", "std.mem.lastIndexOf",
	"std.mem.lastIndexOfScalar", "std.mem.count", "std.mem.containsAtLeast", "std.mem.order",
	"std.mem.lessThan", "std.mem.splitScalar", "std.mem.splitSequence", "std.mem.splitAny",
	"std.mem.tokenizeScalar", "std.mem.tokenizeAny", "std.mem.tokenizeSequence", "std.mem.trim",
	"std.mem.trimStart", "std.mem.trimEnd", "std.mem.join", "std.mem.concat",
	"std.fmt.allocPrint", "std.fmt.bufPrint", "std.fmt.comptimePrint", "std.fmt.parseInt",
	"std.fmt.parseUnsigned", "std.fs.path.join", "std.fs.path.joinZ", "std.fs.path.basename",
	"std.fs.path.dirname", "std.fs.path.extension", "std.fs.path.stem", "std.fs.path.relative",
	"std.fs.path.isAbsolute", "std.log.err", "std.log.warn", "std.log.info", "std.log.debug",
	"std.debug.print", "std.meta.stringToEnum",
)

// graphFreeStd are those of readOnlyStd whose result holds no graph value
// (a flag, a position, a count, a string): the generic ones over the
// slices they are given (trim, split, concat) may.
var graphFreeStd = setOf(
	"std.mem.eql", "std.mem.startsWith", "std.mem.endsWith", "std.mem.indexOf",
	"std.mem.indexOfScalar", "std.mem.indexOfAny", "std.mem.lastIndexOf",
	"std.mem.lastIndexOfScalar", "std.mem.count", "std.mem.containsAtLeast", "std.mem.order",
	"std.mem.lessThan", "std.mem.join", "std.fmt.allocPrint", "std.fmt.bufPrint",
	"std.fmt.comptimePrint", "std.fmt.parseInt", "std.fmt.parseUnsigned", "std.fs.path.join",
	"std.fs.path.joinZ", "std.fs.path.basename", "std.fs.path.dirname", "std.fs.path.extension",
	"std.fs.path.stem", "std.fs.path.relative", "std.fs.path.isAbsolute", "std.meta.stringToEnum",
)

func setOf(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}
