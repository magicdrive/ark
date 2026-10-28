package zig

import (
	"path"
	"sort"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
)

// Named modules.
//
// `@import("name")` names the module the importing file's module imports
// under that name; a module's import table is what the build states for it
// (`m.addImport("name", other)`, `.imports = &.{ .{ .name = ..., .module =
// ... } }`), by code Ark never runs. Zig looks the string up in that table
// first — `@import("x.zig")` too: a relative file only when the table has
// no such name. Three identities keep what is known apart:
//
//	file                 src/vsr.zig          a file (a module's root is one)
//	module               {zigmod}src/vsr.zig  the modules a build creates with
//	                                          that root (their tables merged)
//	an importer's view   {zigenv}src/cli.zig  what a file's @import("name")
//	                                          may see: the tables of every
//	                                          module the file may belong to
//
// A file belongs only to a module whose root's directory holds it (Zig
// rejects an import out of the module's directory), so a file may see the
// tables of the modules rooted in its directory or above: each file states
// {zigenv}file → {zigdir}dir for its directories (its own and above), and a
// build.zig states {zigdir}dir → {zigmod}root for each module it creates,
// and {zigmod}root::name → the file an import of it names (capped at
// Strong: a build may be run otherwise, and a dependency's module of the
// name — one a fetched package provides — may stand for the one Ark finds).
//
// What a build.zig states is read from its syntax alone (buildEval): a
// module's root is a `b.path("literal")`; a module value is a
// createModule / addModule / Module.create call (its options a literal or
// a value naming one), an artifact's `.root_module`, a dependency's
// `.module("name")` (the module a build registers under that name with
// addModule: {zigreg}name; a name no build Ark reads registers stands for
// what Ark does not see), a constant or a tuple element naming one, a
// parameter of a function of the file (what its calls pass; unknown when
// the function is also used as a value or called as a member), a
// function's return. A table binds through addImport, addAnonymousImport,
// addOptions (a module the build generates: unknown), `.imports`, a put
// into or a copy of `import_table` — also through the type
// (`std.Build.Module.addImport(m, ...)`), a constant naming the function or
// @call. Soundness does not rest on listing operations: every occurrence
// of a module (or an artifact) Ark names is classified where it is
// written (occurrence) — a use Ark reads (a method above or one that keeps
// the table, a constant, a var — whose every value and use is classified
// too —, an argument, a literal's field, a return, a block's break, a
// test), or another use, which leaves the module's names unknown; an
// assignment of a root, an artifact's root module or a table, or a
// dereferenced write of a module, leaves every module's unknown. So do,
// whatever their receiver denotes, an assignment to such a field, those
// words written as @field names, the build's `modules` used directly and a
// binding call of another arity. A path is the repository's only through
// this build's *std.Build — the build function's parameter, or one every
// call passes it (`dep.path`, a dependency's builder, root in its package).
// What it cannot name stays unknown, never guessed:
//
//   - a binding of a name to a module Ark cannot name caps the name: any
//     import of it is no more than a Candidate ({zigunknown});
//   - a binding of a name Ark cannot read (computed at build time) caps the
//     module: any import from its files — `@import("x.zig")` included — is
//     no more than a Candidate;
//   - a binding on a module Ark cannot name may be any module's: every file
//     sees it ({zigany}).
//
// A relative import's file states, for each directory of each importer and
// what the importer wrote, file → {zigspec}dir::spec; a build states
// {zigspec}dir::name → the module it binds under a name ending in .zig, and
// {zigspec}dir → {zigunknown} (capped) for a module binding names Ark
// cannot read, dir the module's root's. The file is then one of the
// Candidates — or only the module, when no file has the path.
//
// The unknown aliases are open (AliasDraft.Open): a reference resolved
// through one is counted, besides its Candidates, as an unresolved one —
// any same-named declaration, any file for an import.
//
// Bindings only code Ark does not evaluate makes — a function of another
// file (a build helper, a dependency's build), a module a fetched package
// provides — are unknown to it: an import they alone bind is not resolved,
// and a name they would bind besides one a build.zig binds is the premise
// of Strong. Exact for `@import("x.zig")` rests on the same premise: a
// module name ending in .zig only such code binds is not seen.
// std and builtin are the compiler's: never mapped; root is the root
// module's file, which no syntax names.

// reservedModules are the modules the compiler provides under their names.
var reservedModules = map[string]bool{"std": true, "builtin": true, "root": true}

// reservedPath reports an identity through a module the compiler provides.
func reservedPath(ident string) bool {
	name, ok := strings.CutPrefix(ident, "{module}")
	if !ok {
		return false
	}
	name, _, _ = strings.Cut(name, "::")
	return reservedModules[name]
}

// unprovenModule reports an identity through a named module whose file the
// repository may hold under another identity: one a build maps (or does not
// map literally), and root, the root module's file. std and builtin are the
// compiler's.
func unprovenModule(ident string) bool {
	return strings.HasPrefix(ident, "{zigenv}") || ident == "{module}root" || strings.HasPrefix(ident, "{module}root::")
}

// envIdent is the identity an import of a named module has from file key.
func envIdent(key, name string) string { return "{zigenv}" + key + "::" + name }

// dirAncestors are the directories holding file key, innermost first: its
// own up to the repository's ("."), each a directory a module whose files
// it may be among is rooted in.
func dirAncestors(key string) []string {
	var out []string
	for d := path.Dir(key); ; d = path.Dir(d) {
		out = append(out, d)
		if d == "." || d == "/" {
			return out
		}
	}
}

// Identities of the build facts.
const (
	unknownModule = "{zigunknown}"
	anyModule     = "{zigany}"
)

func moduleIdentOf(root string) string { return "{zigmod}" + root }

// A directory's identities fold case: a case-insensitive file system makes
// `b.path("Src/main.zig")` the root of the files under src/ (two
// directories apart in case alone, on a case-sensitive one, then share
// their modules: more Candidates, nothing claimed).
func dirIdent(dir string) string    { return "{zigdir}" + strings.ToLower(dir) }
func specIdent(dir string) string   { return "{zigspec}" + strings.ToLower(dir) }
func registered(name string) string { return "{zigreg}" + name }

// keyDir is the directory of the module a binding key's module is rooted
// in: "." (every file's) for a module Ark cannot name.
func keyDir(k string) string {
	if root, ok := strings.CutPrefix(k, "{zigmod}"); ok {
		return path.Dir(root)
	}
	return "."
}

// importerAliases states what a file's imports may see (see above): its
// view of named modules, for its directories and the bindings of modules
// Ark cannot name; for each relative import, the module bound under its
// spec and the directories' unread bindings.
func (x *extractor) importerAliases() {
	dirs := dirAncestors(x.key)
	at := x.loc(x.root)
	// A build.zig is a module of the build runner's, whose table is its
	// package's dependencies: it sees no module a build makes.
	if x.namedImports && !x.build {
		env := "{zigenv}" + x.key
		for _, d := range dirs {
			x.aliases = append(x.aliases, language.AliasDraft{Qualified: env, Target: dirIdent(d), Location: at, Members: true})
		}
		x.aliases = append(x.aliases, language.AliasDraft{Qualified: env, Target: anyModule, Location: at, Members: true})
	}
	seen := map[[2]string]bool{}
	add := func(file, target string) {
		k := [2]string{file, target}
		if !seen[k] {
			seen[k] = true
			x.aliases = append(x.aliases, language.AliasDraft{Qualified: file, Target: target, Location: at})
		}
	}
	for _, imp := range x.relativeImports {
		if strings.Contains(imp[1], "::") {
			continue
		}
		for _, d := range dirs {
			add(imp[0], specIdent(d)+"::"+imp[1])
		}
	}
}

// buildEval evaluates the module expressions of a build.zig by its syntax.
type buildEval struct {
	x *extractor
	// calls are the file's calls of each function it declares, by name;
	// methodCalls the names called as a member (`Self.add(m)`), whose
	// arguments Ark does not bind to a declaration.
	calls       map[string][]*ts.Node
	methodCalls map[string]bool
	// fns are the file's top-level functions by name.
	fns map[string]*ts.Node
	// copyIters are the import_table uses a recognized copy iterates.
	copyIters map[nodeKey]bool
	// memo holds what an expression denotes outside any call (frame nil).
	memo map[nodeKey]bval
	// lookups are the registered names looked up (`.module("name")`).
	lookups map[string]bool
	// busy guards evaluation cycles; depth bounds it.
	busy map[evalKey]bool
}

type evalKey struct {
	n nodeKey
	f *frame
}

// frame binds a function's parameters to the arguments of one call, in the
// caller's frame.
type frame struct {
	fn     *ts.Node
	args   []*ts.Node
	caller *frame
}

// bsrc is a literal a value may be, in the frame it is written in.
type bsrc struct {
	n *ts.Node
	f *frame
	// artifact: n is an artifact's options (addExecutable(.{ ... })),
	// whose root_module is the artifact's.
	artifact bool
}

// bval is what a build expression denotes, as far as the syntax proves.
type bval struct {
	// mods are module identities: a root file, or {zigreg}name.
	mods []string
	// structs and tuples are the struct and tuple literals it may be.
	structs, tuples []bsrc
	// unknown: it may also be something Ark cannot name.
	unknown bool
	// unreached: a parameter no call of this file passes (another file's
	// calls give it: their effects are unknown to Ark, not guessed).
	unreached bool
}

func (v *bval) join(o bval) {
	v.mods = append(v.mods, o.mods...)
	v.structs = append(v.structs, o.structs...)
	v.tuples = append(v.tuples, o.tuples...)
	v.unknown = v.unknown || o.unknown
	v.unreached = v.unreached || o.unreached
}

// maxEvalDepth bounds the evaluation of a build expression.
const maxEvalDepth = 24

// buildFacts are the module facts of a build.zig.
type buildFacts struct {
	owners  map[string]bool     // module roots
	entries map[string][]string // binding key ({zigmod}root::name, {zigany}::name, {zigspec}name) → targets
	capped  map[string]bool     // binding keys whose module Ark cannot name
	dynamic map[string]bool     // module identities ({zigmod}root, {zigany}) binding names Ark cannot read
	regs    map[string][]string // registered name → roots
	// unknownRegs are the names registered for a module whose root Ark
	// cannot read: what the name stands for is unknown.
	unknownRegs map[string]bool
	// lookups are the registered names the build looks up
	// (`dep.module("name")`): a name registered nowhere Ark reads stands
	// for what Ark does not see.
	lookups map[string]bool
}

// readBuild reads a build.zig's module declarations and states them as
// aliases (see above).
func (x *extractor) readBuild(root *ts.Node) {
	e := &buildEval{x: x, calls: map[string][]*ts.Node{}, methodCalls: map[string]bool{}, fns: map[string]*ts.Node{}, lookups: map[string]bool{}, busy: map[evalKey]bool{}, copyIters: map[nodeKey]bool{}, memo: map[nodeKey]bval{}}
	var all, optionLits, importTableUses, idents, builtinCalls, assigns, derefs, memCalls []*ts.Node
	lexicalUnknown := false
	var walk func(n *ts.Node, depth int)
	walk = func(n *ts.Node, depth int) {
		if n == nil {
			return
		}
		if depth > maxDepth {
			lexicalUnknown = true // a region Ark does not read
			return
		}
		switch x.typ(n) {
		case "call_expression":
			all = append(all, n)
			// Functions that rewrite memory (a module's own bytes:
			// std.mem.swap(Module, a, c), std.mem.copyForwards, ...).
			if f := x.field(n, "function"); f != nil && strings.HasPrefix(collapse(x.text(f)), "std.mem.") {
				memCalls = append(memCalls, n)
			}
			if f := x.field(n, "function"); f != nil && x.typ(f) == "identifier" {
				name := identifierName(x.text(f))
				e.calls[name] = append(e.calls[name], n)
			} else if f != nil && x.typ(f) == "field_expression" {
				e.methodCalls[identifierName(x.text(x.field(f, "member")))] = true
			}
		case "function_declaration":
			if n.Parent() == root {
				if name := x.field(n, "name"); name != nil {
					e.fns[identifierName(x.text(name))] = n
				}
			}
		case "identifier":
			if t := x.text(n); strings.HasPrefix(t, "@\"") && strings.Contains(t, "\\") {
				// A name with an escape Ark does not decode: any word,
				// a table's or a binding's among them.
				lexicalUnknown = true
			}
			// A name used other than as a call's callee or a declaration's
			// name (`@call(.auto, f, args)`, a function value): if it names
			// a function, its parameters are what Ark does not see.
			p := n.Parent()
			callee := p != nil && x.typ(p) == "call_expression" && keyOf(x.field(p, "function")) == keyOf(n)
			declared := p != nil && keyOf(x.field(p, "name")) == keyOf(n)
			member := p != nil && x.typ(p) == "field_expression" && keyOf(x.field(p, "member")) == keyOf(n)
			if !callee && !declared && !member {
				e.methodCalls[identifierName(x.text(n))] = true
			}
			if !declared && !member {
				idents = append(idents, n)
			}
		case "builtin_function":
			switch x.builtinName(n) {
			case "@field":
				// @field(@This(), "f"): the function f, by its name.
				if args := x.builtinArgs(n); len(args) == 2 {
					if name, ok := x.stringLiteral(args[1]); ok {
						e.methodCalls[name] = true
					}
				}
			case "@call":
				builtinCalls = append(builtinCalls, n)
			case "@memcpy", "@memmove", "@memset":
				lexicalUnknown = true // bytes rewritten: a module's, perhaps
			case "@import":
				// Another build.zig's code run with this build's b: its paths
				// are this build's, not its own directory's.
				if args := x.builtinArgs(n); len(args) == 1 {
					if spec, ok := x.stringLiteral(args[0]); ok && path.Base(spec) == "build.zig" {
						lexicalUnknown = true
					}
				}
			}
			if x.builtinName(n) == "@field" {
				if args := x.builtinArgs(n); len(args) == 2 {
					if name, ok := x.stringLiteral(args[1]); !ok || tableWords[name] {
						lexicalUnknown = true // a table or root reached by a name Ark does not follow
					}
				}
			}
		case "variable_declaration":
			if !x.hasToken(n, "const") && !x.hasToken(n, "var") {
				l := x.firstNamed(n)
				switch {
				case l != nil && x.typ(l) == "identifier":
					assigns = append(assigns, n) // v = value
				case l != nil && x.typ(l) == "field_expression" && tableWords[identifierName(x.text(x.field(l, "member")))]:
					lexicalUnknown = true // m.root_source_file = ..., whatever m is
				case l != nil && x.typ(l) == "dereference_expression":
					derefs = append(derefs, n) // p.* = value
				}
			}
		case "anonymous_struct_initializer":
			if x.option(n, "imports") != nil || e.rootSource(n) != "" {
				optionLits = append(optionLits, n)
			}
		case "field_expression":
			switch identifierName(x.text(x.field(n, "member"))) {
			case "import_table":
				importTableUses = append(importTableUses, n)
			case "modules":
				lexicalUnknown = true // the build's registered modules, used directly
			}
		}
		for _, ch := range x.named(n) {
			walk(ch, depth+1)
		}
	}
	walk(root, 0)

	facts := buildFacts{
		owners: map[string]bool{}, entries: map[string][]string{}, capped: map[string]bool{},
		dynamic: map[string]bool{}, regs: map[string][]string{}, unknownRegs: map[string]bool{}, lookups: map[string]bool{},
	}
	bind := func(keys []string, name string, ok bool, v bval) {
		for _, k := range keys {
			if !ok {
				facts.dynamic[k] = true
				continue
			}
			unknown := v.unknown || v.unreached || len(v.mods) == 0
			keys := []string{k + "::" + name}
			if strings.HasSuffix(name, ".zig") {
				keys = append(keys, specIdent(keyDir(k))+"::"+name) // @import("x.zig") looks it up first
			}
			for _, bk := range keys {
				facts.entries[bk] = append(facts.entries[bk], v.mods...)
				facts.capped[bk] = facts.capped[bk] || unknown
			}
		}
	}
	// receivers are the binding keys of the modules value v may be; skip
	// when v is only a parameter no call of this file passes.
	receivers := func(v bval) (keys []string, skip bool) {
		for _, m := range v.mods {
			if strings.HasPrefix(m, "{zigreg}") {
				keys = append(keys, anyModule) // another build's module
			} else {
				keys = append(keys, moduleIdentOf(m))
			}
		}
		if v.unknown {
			keys = append(keys, anyModule)
		}
		return uniqueStrings(keys), len(keys) == 0 && v.unreached
	}
	type tableCopy struct {
		dst []string
		src bval
	}
	var copies []tableCopy
	consumed := map[nodeKey]bool{}
	// module reads the module options opts make: the module's binding
	// keys (anyModule for a root Ark cannot read), with its imports bound.
	module := func(opts *ts.Node, name *ts.Node) (keys []string) {
		srcs := e.literals(opts, nil)
		if len(srcs) == 0 {
			// Options Ark cannot read: a module of any root, any table.
			bind([]string{anyModule}, "", false, bval{})
			if name != nil {
				if n, ok := x.stringLiteral(name); ok && !reservedModules[n] {
					facts.unknownRegs[n] = true
				}
			}
			return []string{anyModule}
		}
		for _, src := range srcs {
			consumed[keyOf(src.n)] = true
			mod := e.rootSource(src.n)
			k := anyModule
			if mod != "" {
				facts.owners[mod] = true
				k = moduleIdentOf(mod)
			}
			keys = append(keys, k)
			if name != nil {
				if n, ok := x.stringLiteral(name); ok && !reservedModules[n] {
					if mod != "" {
						facts.regs[n] = append(facts.regs[n], mod)
					} else {
						facts.unknownRegs[n] = true
					}
				}
			}
			kk := []string{k}
			e.optionImports(src.n, src.f, func(name string, ok bool, v bval) { bind(kk, name, ok, v) })
		}
		return uniqueStrings(keys)
	}
	shapes := make([]callShape, 0, len(all)+len(builtinCalls))
	for _, call := range all {
		if sh, ok := e.shapeOf(call); ok {
			shapes = append(shapes, sh)
		}
	}
	for _, call := range builtinCalls {
		if sh, ok := e.shapeOfBuiltinCall(call); ok {
			shapes = append(shapes, sh)
		}
	}
	for _, sh := range shapes {
		obj, args, method := sh.obj, sh.args, sh.method
		switch method {
		case "createModule", "addModule", "create":
			if method == "create" && !strings.HasSuffix(x.text(obj), "Module") {
				break // another type's create: below
			}
			if len(args) == 0 || len(args) > 2 {
				continue
			}
			opts := args[len(args)-1:]
			var name *ts.Node
			if method == "addModule" && len(args) == 2 {
				name = args[0]
			}
			module(opts[0], name)
			continue
		case "addExecutable", "addTest", "addLibrary", "addObject", "addSharedLibrary", "addStaticLibrary":
			// The artifact's options: its root module is read where used
			// (eval); before 0.15 an artifact named its root itself.
			if len(args) == 1 {
				for _, src := range e.literals(args[0], nil) {
					consumed[keyOf(src.n)] = true
					if mod := e.rootSource(src.n); mod != "" {
						facts.owners[mod] = true
					}
				}
			}
			continue
		case "put", "putNoClobber", "putAssumeCapacity", "putAssumeCapacityNoClobber",
			"putContext", "putNoClobberContext", "putAssumeCapacityContext", "putAssumeCapacityNoClobberContext":
			// m.import_table.put(gpa, "name", module[, ctx])
			if x.typ(obj) == "field_expression" && identifierName(x.text(x.field(obj, "member"))) == "import_table" && (len(args) == 3 || len(args) == 4) {
				keys, skip := receivers(e.eval(x.field(obj, "object"), nil, 0))
				if !skip {
					name, ok := x.stringLiteral(args[1])
					bind(keys, name, ok && !reservedModules[name], e.eval(args[2], nil, 0))
				}
				continue
			}
		}
		recv := e.eval(obj, nil, 0)
		keys, skip := receivers(recv)
		if skip {
			continue
		}
		if len(recv.mods) == 0 && !e.isArtifact(recv) {
			// No module Ark names: a binding call may be any module's;
			// any other call is no module's operation Ark can tell.
			if !recv.unknown || (method != "addImport" && method != "addAnonymousImport" && method != "addOptions") || len(args) != 2 {
				continue
			}
		}
		switch method {
		case "addImport":
			if len(args) != 2 {
				bind(keys, "", false, bval{})
				continue
			}
			if src := e.tableCopy(args[0], args[1]); src != nil {
				// Every binding of another module's table, copied.
				copies = append(copies, tableCopy{keys, e.eval(src, nil, 0)})
				continue
			}
			name, ok := x.stringLiteral(args[0])
			if ok && reservedModules[name] {
				continue
			}
			bind(keys, name, ok, e.eval(args[1], nil, 0))
		case "addAnonymousImport":
			// m.addAnonymousImport("name", .{ .root_source_file = ... }):
			// a module of its own, bound under the name.
			if len(args) != 2 {
				bind(keys, "", false, bval{})
				continue
			}
			v := bval{unknown: true}
			if anon := module(args[1], nil); len(anon) == 1 && anon[0] != anyModule {
				v = bval{mods: []string{strings.TrimPrefix(anon[0], "{zigmod}")}}
			}
			name, ok := x.stringLiteral(args[0])
			bind(keys, name, ok && !reservedModules[name], v)
		case "addOptions":
			// A module the build generates, bound under the name.
			if len(args) == 2 {
				name, ok := x.stringLiteral(args[0])
				bind(keys, name, ok && !reservedModules[name], bval{unknown: true})
			} else {
				bind(keys, "", false, bval{})
			}
		default:
			if e.isArtifact(recv) && len(recv.mods) == 0 {
				// An artifact's own method: its root module's table only
				// through `.root_module` — or an older API's module calls.
				if strings.Contains(method, "Module") || strings.Contains(method, "Import") {
					bind(e.artifactKeys(recv), "", false, bval{})
				}
				continue
			}
			if !moduleMethodKeepsTable[method] {
				// What it does to the table is unknown.
				bind(keys, "", false, bval{})
			}
		}
	}
	// A use of a module's import_table other than a copy's iterator or a
	// put is unknown: the table's names are.
	for _, n := range importTableUses {
		if p := n.Parent(); p != nil && x.typ(p) == "field_expression" {
			m := identifierName(x.text(x.field(p, "member")))
			if (m == "iterator" && e.copyIters[keyOf(n)]) || putMethods[m] {
				continue
			}
		}
		keys, skip := receivers(e.eval(x.field(n, "object"), nil, 0))
		if !skip {
			bind(keys, "", false, bval{})
		}
	}
	// Operations on a root or a table Ark reads by their words, whatever the
	// receiver denotes; a dereferenced assignment of a value that may be a
	// module (or that Ark cannot tell from one).
	for _, st := range derefs {
		l, r := x.firstNamed(st), x.lastNamed(st)
		lv, rv := e.evalTop(x.firstNamed(l)), e.evalTop(r)
		if len(lv.mods) > 0 || len(rv.mods) > 0 || e.isArtifact(lv) || e.isArtifact(rv) || e.declaredModulePointer(x.firstNamed(l)) {
			lexicalUnknown = true // a module overwritten (`t.* = lib.*`)
		}
	}
	for _, sh := range shapes {
		switch {
		case (sh.method == "addImport" || sh.method == "addAnonymousImport") && len(sh.args) != 2,
			sh.method == "addOptions" && len(sh.args) != 0 && len(sh.args) != 2: // b.addOptions() is the build's
			lexicalUnknown = true // a binding in a form Ark does not read
		}
	}
	for _, call := range memCalls {
		for _, a := range x.named(x.field(call, "arguments")) {
			if v := e.evalTop(a); len(v.mods) > 0 || e.isArtifact(v) || strings.Contains(x.text(a), "Module") {
				lexicalUnknown = true // a module's bytes, swapped or copied
			}
		}
	}
	if lexicalUnknown {
		bind([]string{anyModule}, "", false, bval{})
	}
	// Every occurrence of a module (or an artifact holding one) must be one
	// Ark reads; elsewhere what is done to it is unknown: its names — and,
	// assigned or dereferenced, any module's root and table.
	for _, id := range append(idents, all...) { // names, and calls making one
		v := e.evalTop(id)
		if x.typ(id) == "identifier" {
			// A var holding a module: each use is classified as one of the
			// module it starts as (what it holds by then Ark does not
			// follow; a binding through it is any module's, above).
			if b, ok := e.binding(id, identifierName(x.text(id))); ok && b.kind == bindDecl && x.hasToken(b.node, "var") && e.declCount(b.node) == 1 {
				v = e.evalTop(x.initializer(b.node))
				for _, st := range assigns {
					// v = value: one of what the var holds.
					if l := x.firstNamed(st); l != nil && identifierName(x.text(l)) == identifierName(x.text(id)) {
						if lb, ok := e.binding(l, identifierName(x.text(l))); ok && keyOf(lb.node) == keyOf(b.node) {
							v.join(e.evalTop(x.lastNamed(st)))
						}
					}
				}
			}
		}
		modKeys := []string{}
		for _, m := range v.mods {
			if !strings.HasPrefix(m, "{zigreg}") {
				modKeys = append(modKeys, moduleIdentOf(m))
			}
		}
		if e.isArtifact(v) {
			modKeys = append(modKeys, e.artifactKeys(v)...)
		}
		if len(modKeys) == 0 {
			continue
		}
		if debugOccurrences != nil {
			debugOccurrences(x.text(id), x.typ(id.Parent()), int(id.StartPoint().Row)+1, e.occurrence(id))
		}
		switch e.occurrence(id) {
		case occursUnknown:
			bind(uniqueStrings(modKeys), "", false, bval{})
		case occursWrite:
			bind([]string{anyModule}, "", false, bval{})
		}
	}
	// Module options no API Ark reads consumed (a module made otherwise,
	// with a table and a root Ark cannot see bound): unknown.
	for _, lit := range optionLits {
		if consumed[keyOf(lit)] {
			continue
		}
		if mod := e.rootSource(lit); mod != "" {
			facts.owners[mod] = true
			bind([]string{moduleIdentOf(mod)}, "", false, bval{})
		} else {
			bind([]string{anyModule}, "", false, bval{})
		}
	}
	// A copy binds what its source binds, to a fixed point (a copy of a
	// copy); a source Ark cannot name binds names it cannot read.
	for changed, round := true, 0; changed && round < 16; round++ {
		changed = false
		for _, c := range copies {
			srcKeys, _ := receivers(c.src)
			if len(srcKeys) == 0 {
				srcKeys = []string{anyModule}
			}
			for _, src := range srcKeys {
				for _, dst := range c.dst {
					if src == dst {
						continue
					}
					if facts.dynamic[src] && !facts.dynamic[dst] {
						bind([]string{dst}, "", false, bval{})
						changed = true
					}
					if src == anyModule && c.src.unknown && !facts.dynamic[dst] {
						bind([]string{dst}, "", false, bval{})
						changed = true
					}
					for _, k := range sortedKeys(facts.entries) {
						name, ok := strings.CutPrefix(k, src+"::")
						if !ok {
							continue
						}
						dk := dst + "::" + name
						before := len(facts.entries[dk])
						capped := facts.capped[dk]
						bind([]string{dst}, name, true, bval{mods: facts.entries[k], unknown: facts.capped[k]})
						if len(uniqueStrings(facts.entries[dk])) != len(uniqueStrings(facts.entries[dk][:before])) || facts.capped[dk] != capped {
							changed = true
						}
					}
				}
			}
		}
	}
	for name := range e.lookups {
		facts.lookups[name] = true
	}
	x.buildAliases(facts)
}

// callShape is a call as a method of a value: obj.method(args), also when
// written through the type (`std.Build.Module.addImport(m, ...)`), a
// constant naming the function, or @call.
type callShape struct {
	obj    *ts.Node
	method string
	args   []*ts.Node
}

// putMethods are the import_table methods read as a binding.
var putMethods = map[string]bool{"put": true, "putNoClobber": true, "putAssumeCapacity": true, "putAssumeCapacityNoClobber": true,
	"putContext": true, "putNoClobberContext": true, "putAssumeCapacityContext": true, "putAssumeCapacityNoClobberContext": true}

// tableWords are the names through which a module's root or table is
// reached or bound: written where Ark does not read the operation (an
// assignment to one, `@field(x, "word")`), any module may be changed.
var tableWords = map[string]bool{"root_source_file": true, "root_module": true, "import_table": true,
	"addImport": true, "addAnonymousImport": true, "addOptions": true, "modules": true}

// bindingMethods are std.Build.Module's methods that bind a name.
var bindingMethods = map[string]bool{"addImport": true, "addAnonymousImport": true, "addOptions": true}

// shapeOf is call's shape; ok is false for a call of no method.
func (e *buildEval) shapeOf(call *ts.Node) (callShape, bool) {
	x := e.x
	f := x.field(call, "function")
	args := x.named(x.field(call, "arguments"))
	return e.shapeOfCallee(f, args)
}

// shapeOfBuiltinCall is the shape of `@call(modifier, f, .{ args })`.
func (e *buildEval) shapeOfBuiltinCall(n *ts.Node) (callShape, bool) {
	x := e.x
	args := x.builtinArgs(n)
	if len(args) != 3 || x.typ(args[2]) != "anonymous_struct_initializer" {
		return callShape{}, false
	}
	return e.shapeOfCallee(args[1], x.named(x.firstNamed(args[2])))
}

// shapeOfCallee is the shape of a call of callee f with args.
func (e *buildEval) shapeOfCallee(f *ts.Node, args []*ts.Node) (callShape, bool) {
	x := e.x
	if f == nil {
		return callShape{}, false
	}
	if x.typ(f) == "identifier" {
		// A constant naming a function of the type: `const add =
		// std.Build.Module.addImport;`.
		b, ok := e.binding(f, identifierName(x.text(f)))
		if !ok || b.kind != bindDecl || e.declCount(b.node) != 1 {
			return callShape{}, false
		}
		f = x.initializer(b.node)
		if f == nil || x.typ(f) != "field_expression" {
			return callShape{}, false
		}
		return e.typeMethod(f, args)
	}
	if x.typ(f) != "field_expression" || x.field(f, "object") == nil {
		return callShape{}, false
	}
	if sh, ok := e.typeMethod(f, args); ok {
		return sh, true
	}
	return callShape{obj: x.field(f, "object"), method: identifierName(x.text(x.field(f, "member"))), args: args}, true
}

// typeMethod is the shape of a call of a binding method through its type
// (`std.Build.Module.addImport(m, name, module)`: m is the receiver).
func (e *buildEval) typeMethod(f *ts.Node, args []*ts.Node) (callShape, bool) {
	x := e.x
	method := identifierName(x.text(x.field(f, "member")))
	if !bindingMethods[method] || !strings.HasSuffix(x.text(x.field(f, "object")), "Module") || len(args) != 3 {
		return callShape{}, false
	}
	return callShape{obj: args[0], method: method, args: args[1:]}, true
}

// debugOccurrences, set by a test, sees each module occurrence classified.
var debugOccurrences func(text, parent string, line int, o occurs)

// evalTop is eval outside any call, memoized.
func (e *buildEval) evalTop(n *ts.Node) bval {
	k := keyOf(n)
	if v, ok := e.memo[k]; ok {
		return v
	}
	v := e.eval(n, nil, 0)
	e.memo[k] = v
	return v
}

type occurs int

const (
	occursRead    occurs = iota // a use Ark reads
	occursUnknown               // a use that may touch the module's table
	occursWrite                 // an assignment or dereference: anything
)

// occurrence classifies the use of a module (or artifact) the expression
// at n names, by where it is written.
func (e *buildEval) occurrence(n *ts.Node) occurs {
	x := e.x
	cur := n
	for depth := 0; depth < maxEvalDepth; depth++ {
		p := cur.Parent()
		if p == nil {
			return occursUnknown
		}
		switch x.typ(p) {
		case "field_expression":
			if keyOf(x.field(p, "object")) != keyOf(cur) {
				return occursUnknown
			}
			member := identifierName(x.text(x.field(p, "member")))
			if pp := p.Parent(); pp != nil && e.assignedTo(pp, p) {
				if moduleFields[member] {
					return occursWrite // m.root_source_file = ..., exe.root_module = ...
				}
				return occursRead // a setting no table or root is (exe.pie = true)
			}
			if member == "root_module" {
				cur = p // still a module
				continue
			}
			if member == "import_table" {
				return occursRead // its uses are read above
			}
			if pp := p.Parent(); pp != nil && x.typ(pp) == "call_expression" && keyOf(x.field(pp, "function")) == keyOf(p) {
				if member == "init" {
					return occursWrite // Module.init(.existing = other): the module is another's
				}
				if bindingMethods[member] || moduleMethodKeepsTable[member] || !(strings.Contains(member, "Module") || strings.Contains(member, "Import")) {
					return occursRead // read above, or no table's operation
				}
				return occursUnknown
			}
			return occursRead // a field read (m.owner, exe.step)
		case "dereference_expression":
			return occursWrite
		case "unary_expression", "parenthesized_expression", "try_expression", "comptime_expression", "null_coercion_expression":
			cur = p
			continue
		case "variable_declaration":
			if x.hasToken(p, "const") || x.hasToken(p, "var") || e.discard(p) {
				return occursRead // named (its uses classified), or discarded (`_ = m`)
			}
			if l := x.firstNamed(p); l != nil && x.typ(l) == "identifier" {
				return occursRead // a var made to hold another (`v = m`): its uses are classified
			}
			return occursWrite // an assignment (the grammar's statement form)
		case "arguments", "initializer_list", "return_expression", "anonymous_struct_initializer", "break_expression":
			return occursRead // passed, held, returned or a block's value: followed
		case "assignment_expression":
			if gp := p.Parent(); gp != nil && x.typ(gp) == "initializer_list" && keyOf(x.field(p, "right")) == keyOf(cur) {
				return occursRead // `.field = m` of a literal
			}
			return occursWrite
		case "binary_expression", "if_statement", "if_expression", "switch_expression", "while_statement":
			return occursRead // compared or tested
		default:
			return occursUnknown
		}
	}
	return occursUnknown
}

// moduleFields are the fields of a module or an artifact whose assignment
// changes which file a module is rooted at or what its table binds.
var moduleFields = map[string]bool{"root_source_file": true, "root_module": true, "import_table": true}

// declaredModulePointer reports an identifier declared with a type naming
// a module or an artifact (`p: *std.Build.Module`).
func (e *buildEval) declaredModulePointer(n *ts.Node) bool {
	x := e.x
	if n == nil || x.typ(n) != "identifier" {
		return false
	}
	b, ok := e.binding(n, identifierName(x.text(n)))
	if !ok {
		return false
	}
	var t string
	switch b.kind {
	case bindParam:
		if ps := x.named(x.childOfType(b.node, "parameters")); b.index < len(ps) {
			t = x.text(x.field(ps[b.index], "type"))
		}
	case bindDecl:
		t = x.text(x.field(b.node, "type"))
	}
	return strings.Contains(t, "Module") || strings.Contains(t, "Compile")
}

// assignedTo reports n as the target of assignment statement st (the
// grammar derives `a.b = c;` as a variable_declaration with no const/var).
func (e *buildEval) assignedTo(st, n *ts.Node) bool {
	x := e.x
	switch x.typ(st) {
	case "assignment_expression":
		return keyOf(x.field(st, "left")) == keyOf(n)
	case "variable_declaration":
		return !x.hasToken(st, "const") && !x.hasToken(st, "var") && keyOf(x.firstNamed(st)) == keyOf(n)
	}
	return false
}

// discard reports `_ = value;`.
func (e *buildEval) discard(st *ts.Node) bool {
	x := e.x
	l := x.firstNamed(st)
	return l != nil && x.typ(l) == "identifier" && x.text(l) == "_"
}

// moduleMethodKeepsTable are the methods of std.Build.Module that never
// touch its import table (any other may: what it binds is unknown).
var moduleMethodKeepsTable = map[string]bool{
	"linkLibrary": true, "linkSystemLibrary": true, "linkFramework": true,
	"addIncludePath": true, "addSystemIncludePath": true, "addAfterIncludePath": true,
	"addConfigHeader": true, "addCSourceFile": true, "addCSourceFiles": true, "addCMacro": true,
	"addLibraryPath": true, "addRPath": true, "addRPathSpecial": true, "addFrameworkPath": true,
	"addSystemFrameworkPath": true, "addObject": true, "addObjectFile": true, "addAssemblyFile": true,
	"addWin32ResourceFile": true, "addLinkerScript": true,
}

// literals are the struct literals options expression n may be (itself,
// or the literals a constant, a parameter or a field names); nil when one
// it may be is no literal Ark sees.
func (e *buildEval) literals(n *ts.Node, f *frame) []bsrc {
	if n != nil && e.x.typ(n) == "anonymous_struct_initializer" {
		return []bsrc{{n: n, f: f}}
	}
	v := e.eval(n, f, 0)
	if v.unknown || v.unreached || len(v.mods) > 0 || len(v.tuples) > 0 || len(v.structs) == 0 {
		return nil
	}
	for _, s := range v.structs {
		if s.artifact {
			return nil
		}
	}
	return v.structs
}

// isArtifact reports a value that may be an artifact (addExecutable, ...).
func (e *buildEval) isArtifact(v bval) bool {
	for _, s := range v.structs {
		if s.artifact {
			return true
		}
	}
	return false
}

// artifactKeys are the binding keys of the root modules of the artifacts
// value v may be.
func (e *buildEval) artifactKeys(v bval) []string {
	var keys []string
	for _, s := range v.structs {
		if !s.artifact {
			continue
		}
		rm := e.member(bval{structs: []bsrc{s}}, "root_module", 0)
		for _, m := range rm.mods {
			if !strings.HasPrefix(m, "{zigreg}") {
				keys = append(keys, moduleIdentOf(m))
			}
		}
		if rm.unknown || len(rm.mods) == 0 {
			keys = append(keys, anyModule)
		}
	}
	return uniqueStrings(keys)
}

// tableCopy recognizes the binding of every name of another module's
// table — `var it = m.import_table.iterator(); while (it.next()) |e|
// x.addImport(e.key_ptr.*, e.value_ptr.*);` — and returns m, or nil.
func (e *buildEval) tableCopy(nameArg, modArg *ts.Node) *ts.Node {
	x := e.x
	ptr := func(n *ts.Node, member string) *ts.Node {
		if n == nil || x.typ(n) != "dereference_expression" {
			return nil
		}
		fe := x.firstNamed(n)
		if fe == nil || x.typ(fe) != "field_expression" || identifierName(x.text(x.field(fe, "member"))) != member {
			return nil
		}
		if obj := x.field(fe, "object"); obj != nil && x.typ(obj) == "identifier" {
			return obj
		}
		return nil
	}
	k, v := ptr(nameArg, "key_ptr"), ptr(modArg, "value_ptr")
	if k == nil || v == nil || identifierName(x.text(k)) != identifierName(x.text(v)) {
		return nil
	}
	b, ok := e.binding(k, identifierName(x.text(k)))
	if !ok || b.kind != bindCapture {
		return nil
	}
	cond := x.field(b.node, "condition") // it.next()
	cf := x.field(cond, "function")
	if cond == nil || x.typ(cond) != "call_expression" || cf == nil || x.typ(cf) != "field_expression" ||
		identifierName(x.text(x.field(cf, "member"))) != "next" || x.typ(x.field(cf, "object")) != "identifier" {
		return nil
	}
	it := x.field(cf, "object")
	ib, ok := e.binding(it, identifierName(x.text(it)))
	if !ok || ib.kind != bindDecl || e.declCount(ib.node) != 1 {
		return nil
	}
	init := x.initializer(ib.node) // m.import_table.iterator()
	inf := x.field(init, "function")
	if init == nil || x.typ(init) != "call_expression" || inf == nil || x.typ(inf) != "field_expression" ||
		identifierName(x.text(x.field(inf, "member"))) != "iterator" {
		return nil
	}
	tbl := x.field(inf, "object")
	if tbl == nil || x.typ(tbl) != "field_expression" || identifierName(x.text(x.field(tbl, "member"))) != "import_table" {
		return nil
	}
	// The loop does nothing else: its body is this call (alone in a block).
	body := x.field(b.node, "body")
	if body != nil && x.typ(body) == "block_expression" {
		body = x.firstNamed(body)
	}
	if body != nil && x.typ(body) == "block" {
		if sts := x.named(body); len(sts) == 1 && x.typ(sts[0]) == "expression_statement" {
			body = x.firstNamed(sts[0])
		} else {
			body = nil
		}
	}
	if body == nil || x.typ(body) != "call_expression" || keyOf(body) != keyOf(nameArg.Parent().Parent()) {
		return nil
	}
	e.copyIters[keyOf(tbl)] = true
	return x.field(tbl, "object")
}

// optionImports reads the `.imports` of module options opts: each binding,
// with ok false for a name (or a list) Ark cannot read.
func (e *buildEval) optionImports(opts *ts.Node, f *frame, bind func(name string, ok bool, v bval)) {
	x := e.x
	list := x.option(opts, "imports")
	if list == nil {
		return
	}
	e.importList(list, f, bind, 0)
}

// importList reads a list of bindings: `&.{ ... }`, or one a comptime
// condition chooses (every branch's: an import only one binds fails to
// compile in the other).
func (e *buildEval) importList(list *ts.Node, f *frame, bind func(name string, ok bool, v bval), depth int) {
	x := e.x
	if list == nil || depth > maxEvalDepth {
		bind("", false, bval{})
		return
	}
	switch x.typ(list) {
	case "unary_expression", "parenthesized_expression": // &.{ ... }
		e.importList(x.firstNamed(list), f, bind, depth+1)
		return
	case "if_expression":
		for i, ch := range x.named(list) {
			if i == 0 || x.typ(ch) == "payload" {
				continue // the condition
			}
			if x.typ(ch) == "else_clause" {
				ch = x.lastNamed(ch)
			}
			e.importList(ch, f, bind, depth+1)
		}
		return
	case "switch_expression":
		for _, ch := range x.named(list) {
			if x.typ(ch) == "switch_case" {
				e.importList(x.lastNamed(ch), f, bind, depth+1)
			}
		}
		return
	case "anonymous_struct_initializer":
	default:
		bind("", false, bval{}) // a list Ark cannot read
		return
	}
	for _, entry := range x.named(x.firstNamed(list)) {
		if x.typ(entry) != "anonymous_struct_initializer" {
			bind("", false, bval{})
			continue
		}
		name, ok := x.stringLiteral(x.option(entry, "name"))
		if ok && reservedModules[name] {
			continue
		}
		bind(name, ok, e.eval(x.option(entry, "module"), f, 0))
	}
}

// buildAliases states facts as aliases (see the package comment above).
func (x *extractor) buildAliases(facts buildFacts) {
	at := x.loc(x.root)
	add := func(q, t, limit string, members bool) {
		x.aliases = append(x.aliases, language.AliasDraft{Qualified: q, Target: t, Location: at, Members: members, ConfidenceCap: limit,
			Open: t == unknownModule}) // what it stands for is no declaration Ark has
	}
	for _, root := range sortedKeys(facts.owners) {
		add(dirIdent(path.Dir(root)), moduleIdentOf(root), "", true)
	}
	for _, k := range sortedKeys(facts.entries) {
		for _, m := range uniqueStrings(facts.entries[k]) {
			add(k, m, "strong", false)
		}
		if facts.capped[k] {
			add(k, unknownModule, "candidate", false)
		}
	}
	for _, k := range sortedKeys(facts.dynamic) {
		add(k, unknownModule, "candidate", true)
		add(specIdent(keyDir(k)), unknownModule, "candidate", true)
	}
	for _, name := range sortedKeys(facts.regs) {
		for _, root := range uniqueStrings(facts.regs[name]) {
			add(registered(name), root, "strong", false)
		}
	}
	for _, name := range sortedKeys(facts.unknownRegs) {
		add(registered(name), unknownModule, "candidate", false)
	}
	// A name looked up and registered by no build Ark reads (a fetched
	// package's): what it stands for is unknown. Read only where nothing
	// else carries the name.
	for _, name := range sortedKeys(facts.lookups) {
		x.aliases = append(x.aliases, language.AliasDraft{Qualified: registered(name), Target: "{zigunregistered}" + name, Location: at, Fallback: true})
		add("{zigunregistered}"+name, unknownModule, "candidate", false)
	}
}

// eval is what build expression n denotes in frame f.
func (e *buildEval) eval(n *ts.Node, f *frame, depth int) bval {
	x := e.x
	if n == nil || depth > maxEvalDepth {
		return bval{unknown: true}
	}
	k := evalKey{keyOf(n), f}
	if e.busy[k] {
		return bval{unknown: true}
	}
	e.busy[k] = true
	defer delete(e.busy, k)
	switch x.typ(n) {
	case "try_expression", "parenthesized_expression", "comptime_expression", "unary_expression", "null_coercion_expression", "dereference_expression":
		return e.eval(x.firstNamed(n), f, depth+1)
	case "if_expression":
		// Either branch's value (the condition is not evaluated).
		var out bval
		for i, ch := range x.named(n) {
			if i == 0 || x.typ(ch) == "payload" {
				continue
			}
			if x.typ(ch) == "else_clause" {
				ch = x.lastNamed(ch)
			}
			out.join(e.eval(ch, f, depth+1))
		}
		return out
	case "anonymous_struct_initializer":
		list := x.named(x.firstNamed(n))
		if len(list) > 0 && x.typ(list[0]) == "assignment_expression" {
			return bval{structs: []bsrc{{n: n, f: f}}}
		}
		return bval{tuples: []bsrc{{n: n, f: f}}}
	case "identifier":
		return e.evalName(n, f, depth)
	case "field_expression":
		obj, m := x.field(n, "object"), x.field(n, "member")
		if obj == nil || m == nil {
			return bval{unknown: true}
		}
		return e.member(e.eval(obj, f, depth+1), identifierName(x.text(m)), depth)
	case "call_expression":
		return e.evalCall(n, f, depth)
	case "labeled_type_expression":
		// blk: { ...; break :blk value; }: every value it breaks with.
		label := identifierName(x.text(x.firstNamed(x.childOfType(n, "block_label"))))
		var out bval
		breaks := e.breaks(x.childOfType(n, "block"), label, 0)
		if label == "" || len(breaks) == 0 {
			return bval{unknown: true}
		}
		for _, b := range breaks {
			out.join(e.eval(b, f, depth+1))
		}
		return out
	}
	return bval{unknown: true}
}

// breaks are the values block body breaks out of the block labeled label
// with (nested functions excluded; a break without a value is unknown).
func (e *buildEval) breaks(body *ts.Node, label string, depth int) []*ts.Node {
	x := e.x
	if body == nil || depth > maxDepth {
		return nil
	}
	var out []*ts.Node
	for _, ch := range x.named(body) {
		switch x.typ(ch) {
		case "function_declaration":
		case "break_expression":
			if bl := x.childOfType(ch, "break_label"); bl != nil && identifierName(x.text(x.firstNamed(bl))) == label {
				if v := x.lastNamed(ch); v != nil && keyOf(v) != keyOf(bl) {
					out = append(out, v)
				} else {
					out = append(out, ch) // no value: unknown
				}
			}
		default:
			out = append(out, e.breaks(ch, label, depth+1)...)
		}
	}
	return out
}

// member is field name of the literals value v may be.
func (e *buildEval) member(v bval, name string, depth int) bval {
	x := e.x
	out := bval{unknown: v.unknown || len(v.mods) > 0, unreached: v.unreached}
	for _, s := range v.structs {
		if s.artifact {
			if name != "root_module" {
				out.unknown = true
				continue
			}
			if rm := x.option(s.n, "root_module"); rm != nil {
				out.join(e.eval(rm, s.f, depth+1))
			} else if mod := e.rootSource(s.n); mod != "" {
				out.mods = append(out.mods, mod)
			} else {
				out.unknown = true
			}
			continue
		}
		if fv := x.option(s.n, name); fv != nil {
			out.join(e.eval(fv, s.f, depth+1))
		} else {
			out.unknown = true
		}
	}
	if len(v.tuples) > 0 {
		out.unknown = true
	}
	return out
}

// evalCall is what call n returns.
func (e *buildEval) evalCall(n *ts.Node, f *frame, depth int) bval {
	x := e.x
	fn := x.field(n, "function")
	args := x.named(x.field(n, "arguments"))
	if fn == nil {
		return bval{unknown: true}
	}
	if x.typ(fn) == "identifier" {
		decl := e.fns[identifierName(x.text(fn))]
		if decl == nil || e.shadowed(n, identifierName(x.text(fn))) {
			return bval{unknown: true}
		}
		callee := &frame{fn: decl, args: args, caller: f}
		var out bval
		returns := e.returns(x.field(decl, "body"), 0)
		if len(returns) == 0 {
			return bval{unknown: true}
		}
		for _, r := range returns {
			out.join(e.eval(r, callee, depth+1))
		}
		return out
	}
	if x.typ(fn) != "field_expression" || x.field(fn, "object") == nil {
		return bval{unknown: true}
	}
	switch identifierName(x.text(x.field(fn, "member"))) {
	case "createModule", "addModule", "create":
		if len(args) == 0 || len(args) > 2 || (identifierName(x.text(x.field(fn, "member"))) == "create" && !strings.HasSuffix(x.text(x.field(fn, "object")), "Module")) {
			break
		}
		var out bval
		srcs := e.literals(args[len(args)-1], f)
		for _, s := range srcs {
			if mod := e.rootSource(s.n); mod != "" {
				out.mods = append(out.mods, mod)
			} else {
				out.unknown = true
			}
		}
		if len(srcs) == 0 {
			out.unknown = true
		}
		return out
	case "module":
		if len(args) == 1 {
			if name, ok := x.stringLiteral(args[0]); ok {
				e.lookups[name] = true
				return bval{mods: []string{registered(name)}}
			}
		}
	case "addExecutable", "addTest", "addLibrary", "addObject", "addSharedLibrary", "addStaticLibrary":
		if len(args) == 1 && x.typ(args[0]) == "anonymous_struct_initializer" {
			return bval{structs: []bsrc{{n: args[0], f: f, artifact: true}}}
		}
	}
	return bval{unknown: true}
}

// returns are the expressions function body returns (nested functions
// excluded).
func (e *buildEval) returns(body *ts.Node, depth int) []*ts.Node {
	x := e.x
	if body == nil || depth > maxDepth {
		return nil
	}
	var out []*ts.Node
	for _, ch := range x.named(body) {
		switch x.typ(ch) {
		case "function_declaration":
		case "return_expression":
			if v := x.firstNamed(ch); v != nil {
				out = append(out, v)
			}
		default:
			out = append(out, e.returns(ch, depth+1)...)
		}
	}
	return out
}

// shadowed reports a name a local or parameter declares around n, which
// is then no function of the file.
func (e *buildEval) shadowed(n *ts.Node, name string) bool {
	b, ok := e.binding(n, name)
	return ok && b.kind != bindNone
}

type bindKind int

const (
	bindNone    bindKind = iota
	bindDecl             // a variable declaration's identifier index
	bindCapture          // an if's payload: the condition's value
	bindParam            // a function's parameter index
)

type binding struct {
	kind  bindKind
	node  *ts.Node // the declaration, the if, the function
	index int
}

// binding finds what name denotes at n: a declaration before n in an
// enclosing block, a capture of an enclosing if, a parameter of the
// enclosing function, a declaration of the file. ok is false for none.
func (e *buildEval) binding(n *ts.Node, name string) (binding, bool) {
	x := e.x
	child := n
	for a := n.Parent(); a != nil; child, a = a, a.Parent() {
		switch x.typ(a) {
		case "block", "source_file":
			for _, st := range x.named(a) {
				if x.typ(a) == "block" && keyOf(st) == keyOf(child) {
					break
				}
				if x.typ(st) == "variable_declaration" && (x.hasToken(st, "const") || x.hasToken(st, "var")) {
					if i := e.declIndex(st, name); i >= 0 {
						return binding{kind: bindDecl, node: st, index: i}, true
					}
				}
			}
			if x.typ(a) == "source_file" {
				return binding{}, false
			}
		case "if_statement", "if_expression", "while_statement", "while_expression":
			cond := x.field(a, "condition")
			if p := x.childOfType(a, "payload"); p != nil && cond != nil && keyOf(child) != keyOf(cond) {
				for _, id := range x.named(p) {
					if identifierName(x.text(id)) == name {
						return binding{kind: bindCapture, node: a}, true
					}
				}
			}
		case "function_declaration":
			if ps := x.childOfType(a, "parameters"); ps != nil {
				for i, p := range x.named(ps) {
					if pn := x.field(p, "name"); pn != nil && identifierName(x.text(pn)) == name {
						return binding{kind: bindParam, node: a, index: i}, true
					}
				}
			}
		case "struct_declaration", "union_declaration", "enum_declaration", "opaque_declaration":
			return binding{}, false
		}
	}
	return binding{}, false
}

// declIndex is the position of name among the names variable
// declaration st binds (`const a, const b: T = ...`), or -1.
func (e *buildEval) declIndex(st *ts.Node, name string) int {
	for i, n := range e.declNames(st) {
		if n == name {
			return i
		}
	}
	return -1
}

// declNames are the names variable declaration st binds, in order (`_`
// holds a position): its identifiers before "=", a type annotation's
// (field "type") excepted.
func (e *buildEval) declNames(st *ts.Node) []string {
	x := e.x
	var out []string
	for j := 0; j < st.ChildCount(); j++ {
		ch := st.Child(j)
		if x.typ(ch) == "=" {
			break
		}
		if ch.IsNamed() && x.typ(ch) == "identifier" && st.FieldNameForChild(j, x.lang) != "type" {
			out = append(out, identifierName(x.text(ch)))
		}
	}
	return out
}

// evalName is what identifier n denotes in frame f.
func (e *buildEval) evalName(n *ts.Node, f *frame, depth int) bval {
	x := e.x
	name := identifierName(x.text(n))
	b, ok := e.binding(n, name)
	if !ok {
		return bval{unknown: true}
	}
	switch b.kind {
	case bindDecl:
		if x.hasToken(b.node, "var") {
			return bval{unknown: true} // may be assigned another value
		}
		init := x.initializer(b.node)
		v := e.eval(init, f, depth+1)
		if e.declCount(b.node) == 1 {
			return v
		}
		out := bval{unknown: v.unknown || len(v.mods) > 0 || len(v.structs) > 0, unreached: v.unreached}
		for _, t := range v.tuples {
			elems := x.named(x.firstNamed(t.n))
			if b.index < len(elems) {
				out.join(e.eval(elems[b.index], t.f, depth+1))
			} else {
				out.unknown = true
			}
		}
		return out
	case bindCapture:
		return e.eval(x.field(b.node, "condition"), f, depth+1)
	case bindParam:
		for c := f; c != nil; c = c.caller {
			if c.fn == b.node {
				if b.index < len(c.args) {
					return e.eval(c.args[b.index], c.caller, depth+1)
				}
				return bval{unknown: true}
			}
		}
		// Not in a call being evaluated: what every call of the file passes.
		fname := ""
		if nm := x.field(b.node, "name"); nm != nil {
			fname = identifierName(x.text(nm))
		}
		calls := e.calls[fname]
		if e.fns[fname] != b.node || e.methodCalls[fname] {
			// A nested function, or one the file may call as a member:
			// what its parameters are is unknown.
			return bval{unknown: true}
		}
		if len(calls) == 0 {
			return bval{unreached: true}
		}
		var out bval
		for _, call := range calls {
			args := x.named(x.field(call, "arguments"))
			if b.index < len(args) {
				out.join(e.eval(args[b.index], nil, depth+1))
			} else {
				out.unknown = true
			}
		}
		return out
	}
	return bval{unknown: true}
}

// declCount is the number of names variable declaration st binds.
func (e *buildEval) declCount(st *ts.Node) int { return len(e.declNames(st)) }

// rootSource is the identity of the file a module's options name as its
// root source file: `.root_source_file = b.path("literal")`, b this
// build's *std.Build — relative to build.zig's directory. "" for any other
// value (a dependency's `dep.path(...)` names a file of that package).
func (e *buildEval) rootSource(opts *ts.Node) string {
	x := e.x
	v := x.option(opts, "root_source_file")
	if v == nil || x.typ(v) != "call_expression" {
		return ""
	}
	f := x.field(v, "function")
	if f == nil || x.typ(f) != "field_expression" || identifierName(x.text(x.field(f, "member"))) != "path" || !e.isBuild(x.field(f, "object"), 0) {
		return ""
	}
	args := x.named(x.field(v, "arguments"))
	if len(args) != 1 {
		return ""
	}
	p, ok := x.stringLiteral(args[0])
	if !ok || !strings.HasSuffix(p, ".zig") || path.IsAbs(p) || strings.ContainsAny(p, "\\\x00") {
		return ""
	}
	key := path.Join(path.Dir(x.key), p)
	if key == ".." || strings.HasPrefix(key, "../") {
		return ""
	}
	return key
}

// isBuild reports an expression naming this build's *std.Build: a
// parameter declared `*std.Build`, or a constant naming one.
func (e *buildEval) isBuild(n *ts.Node, depth int) bool {
	x := e.x
	if n == nil || x.typ(n) != "identifier" || depth > maxEvalDepth {
		return false
	}
	b, ok := e.binding(n, identifierName(x.text(n)))
	if !ok {
		return false
	}
	switch b.kind {
	case bindParam:
		ps := x.named(x.childOfType(b.node, "parameters"))
		if b.index >= len(ps) {
			return false
		}
		t := strings.Join(strings.Fields(x.text(x.field(ps[b.index], "type"))), "")
		if t != "*std.Build" && t != "*Build" {
			return false
		}
		// The build function's own, or one every call of the file passes
		// this build's (another builder — `dep.builder` — roots paths in
		// its package).
		fname := identifierName(x.text(x.field(b.node, "name")))
		if fname == "build" && b.node.Parent() == e.x.root {
			return true
		}
		calls := e.calls[fname]
		if len(calls) == 0 || e.methodCalls[fname] || e.fns[fname] != b.node {
			return false
		}
		for _, c := range calls {
			args := x.named(x.field(c, "arguments"))
			if b.index >= len(args) || !e.isBuild(args[b.index], depth+1) {
				return false
			}
		}
		return true
	case bindDecl:
		if x.hasToken(b.node, "var") || e.declCount(b.node) != 1 {
			return false
		}
		return e.isBuild(x.initializer(b.node), depth+1)
	}
	return false
}

// option is the value of field name in an anonymous struct literal
// (`.{ .name = value }`), or nil.
func (x *extractor) option(lit *ts.Node, name string) *ts.Node {
	if lit == nil || x.typ(lit) != "anonymous_struct_initializer" {
		return nil
	}
	for _, a := range x.named(x.firstNamed(lit)) {
		if x.typ(a) != "assignment_expression" {
			continue
		}
		l := x.field(a, "left")
		if l != nil && x.typ(l) == "field_expression" && x.field(l, "object") == nil && identifierName(x.text(x.field(l, "member"))) == name {
			return x.field(a, "right")
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func uniqueStrings(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	n := 0
	for i, v := range out {
		if i == 0 || v != out[n-1] {
			out[n] = v
			n++
		}
	}
	return out[:n]
}
