package python

import (
	"path"
	"sort"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// Imports, module bindings and exports.
//
// Which file a module name denotes follows from the importing file's path
// alone (nothing else is read): a relative import (from .m import f) names
// a module of the importing file's package directory, `..` its parent; an
// absolute one (import a.b) names a module of a root of sys.path. Ark models
// those roots as every ancestor directory of the importing file — the
// repository root included — that is no package (a directory with an
// __init__.py is a package, never a root: ModuleCandidate.Unless), and src/
// (the src layout, whose packages are installed from it). A module a.b of
// directory D is D/a/b.py or the package D/a/b/__init__.py, with one
// priority: two of them existing (in one directory, or under two roots) are
// ambiguous, never a choice. An import inside a function is a binding of
// that function's body (BindingDraft.Scope); one inside a class body binds
// a class attribute and is none.
//
// A package's __init__.py that imports from one of its submodules
// (from .sub import x) gives the package the attribute sub, the submodule:
// the import system sets it. It is stated as a namespace export of sub when
// the file binds the name sub in no other way.

// importStatement records one import statement of scope sc: the names it
// binds, its ImportDraft and, at module level, its bindings.
func (e *extractor) importStatement(n *ts.Node, sc *scope) {
	scope, bindable := "", sc == e.mod
	if sc.kind == scopeFunction && !sc.lambda {
		scope, bindable = sc.qual, true
	}
	switch e.typ(n) {
	case "future_import_statement":
		// from __future__ import annotations: annotations are not
		// evaluated where they are written (PEP 563).
		if strings.Contains(e.text(n), "annotations") && sc == e.mod {
			e.lazyAnnotations = true
		}
		return
	case "import_statement":
		for i := range n.ChildCount() {
			if n.FieldNameForChild(i, e.lang) != "name" {
				continue
			}
			c := n.Child(i)
			switch e.typ(c) {
			case "dotted_name":
				mod := e.text(c)
				top, _, _ := strings.Cut(mod, ".")
				e.bindName(sc, top, bindImport, c)
				if sc == e.mod {
					e.noteSubmodule(mod)
				}
				e.imports = append(e.imports, language.ImportDraft{Path: mod, Location: nodeLocation(c, e.file)})
				if bindable {
					// import a.b.c binds a; the submodules a.b and a.b.c are
					// reached through it (receivers a.b and a.b.c).
					parts := strings.Split(mod, ".")
					for j := range parts {
						m := strings.Join(parts[:j+1], ".")
						e.moduleImports = append(e.moduleImports, importInfo{local: m, module: m, scope: scope, at: c})
					}
				}
			case "aliased_import":
				mod := e.text(e.field(c, "name"))
				alias := e.text(e.field(c, "alias"))
				if mod == "" || alias == "" {
					continue
				}
				e.bindName(sc, alias, bindImport, c)
				if sc == e.mod {
					e.noteSubmodule(mod)
				}
				e.imports = append(e.imports, language.ImportDraft{Path: mod, Alias: alias, Location: nodeLocation(c, e.file)})
				if bindable {
					e.moduleImports = append(e.moduleImports, importInfo{local: alias, module: mod, scope: scope, at: c})
				}
			}
		}
	case "import_from_statement":
		mod := e.text(e.field(n, "module_name"))
		if mod == "" {
			return
		}
		e.imports = append(e.imports, language.ImportDraft{Path: mod, Location: nodeLocation(n, e.file)})
		if sc == e.mod {
			e.noteSubmodule(mod)
		}
		for i := range n.ChildCount() {
			c := n.Child(i)
			if e.typ(c) == "wildcard_import" {
				if sc == e.mod {
					e.hasStar = true
					if n.StartByte() >= e.lastStar {
						e.lastStar = n.StartByte()
					}
				}
				continue
			}
			if n.FieldNameForChild(i, e.lang) != "name" {
				continue
			}
			imported, local := "", ""
			switch e.typ(c) {
			case "dotted_name":
				imported = e.text(c)
				local = imported
			case "aliased_import":
				imported = e.text(e.field(c, "name"))
				local = e.text(e.field(c, "alias"))
			}
			if imported == "" || local == "" || strings.Contains(imported, ".") {
				continue
			}
			e.bindName(sc, local, bindImport, c)
			if bindable {
				e.moduleImports = append(e.moduleImports, importInfo{local: local, module: mod, imported: imported, scope: scope, at: c})
			}
		}
	}
}

// boundToSubmodule reports whether the package's every binding of name is
// an import of its submodule name (from . import name): the attribute the
// import system sets, the same value.
func (e *extractor) boundToSubmodule(name string) bool {
	b := e.mod.binds[name]
	if b == nil || b.decl+b.vars+b.params+b.other > 0 {
		return false
	}
	n := 0
	for _, im := range e.moduleImports {
		if im.local != name || im.scope != "" {
			continue
		}
		// from . import name, or from pkg import name in pkg's own __init__.py
		// Only a proven one: an absolute import that may name the package
		// may name another one as well (from utils import helpers).
		sub, proven := e.ownSubmodule(im.module + "." + name)
		same := im.imported == name && (im.module == "." || proven && sub == name)
		if !same {
			return false
		}
		n++
	}
	return n == b.imports
}

// ownSubmodule is the submodule of this package (the file being a
// package's __init__.py) an import of module names, and whether the import
// proves it: X of `.X…` does (a relative import is read from this
// package's directory); X of an absolute `p.X…` whose leading names are the
// trailing names of this package's directory (pkg.X, a.b.X in a/b/) may be:
// whether those names are this package depends on which ancestors are
// packages, files the provider does not read.
func (e *extractor) ownSubmodule(mod string) (string, bool) {
	if path.Base(string(e.file)) != "__init__.py" {
		return "", false
	}
	if strings.HasPrefix(mod, ".") {
		if strings.HasPrefix(mod, "..") || len(mod) == 1 {
			return "", false
		}
		sub, _, _ := strings.Cut(mod[1:], ".")
		return sub, true
	}
	dir := path.Dir(string(e.file))
	if dir == "." || dir == "/" {
		return "", false
	}
	comps := strings.Split(dir, "/")
	parts := strings.Split(mod, ".")
	for k := 0; k+1 < len(parts) && k < len(comps); k++ {
		if strings.Join(parts[:k+1], "/") == strings.Join(comps[len(comps)-k-1:], "/") {
			return parts[k+1], false
		}
	}
	return "", false
}

// noteSubmodule records a submodule of this package a module-level import
// sets as an attribute: one a relative import proves, or (unproven) one an
// absolute import may name.
func (e *extractor) noteSubmodule(mod string) {
	switch sub, proven := e.ownSubmodule(mod); {
	case sub == "":
	case proven:
		e.submodules = append(e.submodules, sub)
	default:
		e.maybeSubmodules = append(e.maybeSubmodules, sub)
	}
}

// moduleBindings states the module-level imports as bindings and the
// module's attributes as its export table.
func (e *extractor) moduleBindings() {
	for _, im := range e.moduleImports {
		loc := nodeLocation(im.at, e.file)
		if im.imported == "" {
			if spec, ok := e.moduleSpec(im.module); ok {
				e.bindings = append(e.bindings, language.BindingDraft{Local: im.local, Kind: language.BindingNamespace, Module: spec, Location: loc, Scope: im.scope})
			}
			continue
		}
		// from M import n: the attribute n of M, or (when M binds no n) its
		// submodule M.n — a Fallback binding: an n M binds to a value
		// nothing proves (an opaque export) leaves the submodule out; a
		// module that declares n beside a submodule n is ambiguous.
		if spec, ok := e.moduleSpec(im.module); ok {
			e.bindings = append(e.bindings, language.BindingDraft{Local: im.local, Kind: language.BindingNamed, Imported: im.imported, Module: spec, Location: loc, Scope: im.scope})
		}
		sub := im.module + "." + im.imported
		if strings.HasSuffix(im.module, ".") {
			sub = im.module + im.imported
		}
		if spec, ok := e.moduleSpec(sub); ok {
			e.bindings = append(e.bindings, language.BindingDraft{Local: im.local, Kind: language.BindingNamespace, Module: spec, Location: loc, Scope: im.scope, Fallback: true})
		}
	}

	// A submodule attribute the package also binds in another way: which
	// value it holds depends on the order the import system and the
	// package's code run in. A submodule an absolute import may set is such
	// a doubt too, never an attribute stated (the import may name another
	// package).
	submodule := map[string]bool{}
	for _, sub := range e.maybeSubmodules {
		if e.mod.binds[sub] != nil && !e.boundToSubmodule(sub) {
			submodule[sub] = true
		}
	}
	if path.Base(string(e.file)) == "__init__.py" {
		sort.Strings(e.submodules)
		for i, sub := range e.submodules {
			if i > 0 && sub == e.submodules[i-1] {
				continue
			}
			if e.mod.binds[sub] != nil {
				if !e.boundToSubmodule(sub) {
					submodule[sub] = true
				}
				continue
			}
			if spec, ok := e.moduleSpec("." + sub); ok {
				e.exports = append(e.exports, language.ExportDraft{Kind: language.ExportNamespace, Exported: sub, Module: spec, Location: source.Location{File: e.file}})
			}
		}
	}

	var exported []string
	for name := range e.mod.binds {
		exported = append(exported, name)
	}
	sort.Strings(exported)
	for _, name := range exported {
		b := e.mod.binds[name]
		if b.other > 0 || b.params > 0 || (b.symbols() > 0 && b.imports > 0) || (e.hasStar && b.last < e.lastStar) || submodule[name] {
			// The module binds the name, to a value no declaration or
			// import is proven to be.
			e.exports = append(e.exports, language.ExportDraft{Kind: language.ExportOpaque, Exported: name, Location: source.Location{File: e.file}})
			continue
		}
		e.exports = append(e.exports, language.ExportDraft{
			Kind:     language.ExportLocal,
			Exported: name,
			Local:    name,
			Location: source.Location{File: e.file},
		})
	}
	// A star import may bind any other name, and a module __getattr__
	// answers for any name (PEP 562).
	if e.hasStar || e.mod.binds["__getattr__"] != nil {
		e.exports = append(e.exports, language.ExportDraft{Kind: language.ExportOpaque, Location: source.Location{File: e.file}})
	}
}

// moduleSpec is the ModuleSpec of a module name as an import of this file
// writes it. It reports false when the name can denote no repository file
// (a relative import above the repository root).
func (e *extractor) moduleSpec(spec string) (language.ModuleSpec, bool) {
	dots := len(spec) - len(strings.TrimLeft(spec, "."))
	var parts []string
	if rest := spec[dots:]; rest != "" {
		parts = strings.Split(rest, ".")
	}
	dir := path.Dir(string(e.file))
	var cands []language.ModuleCandidate
	if dots > 0 {
		for i := 1; i < dots; i++ {
			if dir == "." || path.Dir(dir) == dir {
				return language.ModuleSpec{}, false
			}
			dir = path.Dir(dir)
		}
		cands = moduleFiles(dir, parts, nil)
	} else {
		if len(parts) == 0 {
			return language.ModuleSpec{}, false
		}
		roots := []string{"src"}
		for d := dir; ; d = path.Dir(d) {
			roots = append(roots, d)
			if d == "." || path.Dir(d) == d {
				break // the repository root (or, for a FileID that is no root-relative path, the file system's)
			}
		}
		for _, root := range roots {
			cands = append(cands, moduleFiles(root, parts, []source.FileID{source.FileID(path.Join(root, "__init__.py"))})...)
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].File < cands[j].File })
	out := cands[:0]
	for i, c := range cands {
		if i > 0 && c.File == out[len(out)-1].File {
			continue
		}
		out = append(out, c)
	}
	return language.ModuleSpec{Specifier: spec, Candidates: out}, len(out) > 0
}

// moduleFiles are the files module parts of directory dir may be: the
// module file and the package's __init__.py (the package itself for no
// parts).
func moduleFiles(dir string, parts []string, unless []source.FileID) []language.ModuleCandidate {
	base := path.Join(append([]string{dir}, parts...)...)
	if len(parts) == 0 {
		return []language.ModuleCandidate{{File: source.FileID(path.Join(base, "__init__.py")), Unless: unless}}
	}
	return []language.ModuleCandidate{
		{File: source.FileID(base + ".py"), Unless: unless},
		{File: source.FileID(path.Join(base, "__init__.py")), Unless: unless},
	}
}
