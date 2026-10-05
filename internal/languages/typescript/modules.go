package typescript

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// Module bindings, exports and repository-local module candidates.
//
// The provider interprets TypeScript module specifiers lexically: it computes
// the repository-relative files a RELATIVE specifier may denote from the
// importing FileID alone (no filesystem access, no tsconfig, nothing
// executed). Bare specifiers ("zod", "react", "@/foo", "node:fs") and
// specifiers that would leave the repository root get no candidates and are
// therefore never matched against repository symbols.
//
// Ark assigns deterministic priority only within the repository-local,
// config-independent lexical subset it explicitly supports. This is NOT
// compiler-equivalent TypeScript module resolution: real resolution depends on
// moduleResolution, the runtime / host, moduleSuffixes, package.json and
// tsconfig, none of which Ark interprets or executes. A project whose
// resolution depends on those settings may resolve differently from this subset.
//
// The supported subset:
//
//	./user  → user.ts (0), user.tsx (1), user/index.ts (2), user/index.tsx (3)
//	./user.ts / ./user.tsx → exactly that file
//	./user.js / ./user.jsx → .ts and .tsx substitutes at EQUAL priority
//	                         (deliberately unranked: ambiguous when both exist)
//
// `.d.ts`, `.mts`/`.cts`, `.json`, moduleSuffixes and package.json resolution
// are not modelled.

// moduleSpec builds the ModuleSpec for specifier written in importer.
func moduleSpec(importer source.FileID, spec string) language.ModuleSpec {
	m := language.ModuleSpec{Specifier: spec}
	if spec != "." && spec != ".." && !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
		return m // bare / aliased / absolute: not repository-resolvable
	}
	// Module specifiers use `/`; a backslash or control character is not a
	// path this provider will interpret (it could name a different file on
	// another platform), so it is never matched against the repository.
	imp := filepath.ToSlash(string(importer))
	for _, r := range spec + imp {
		if r == '\\' || r < 0x20 || r == 0x7f {
			return m
		}
	}
	dir := path.Dir(imp)
	joined := path.Join(dir, spec)
	if joined == ".." || strings.HasPrefix(joined, "../") || path.IsAbs(joined) {
		return m // would escape the repository root
	}
	at := func(p string) source.FileID {
		if joined == "." {
			return source.FileID(p)
		}
		return source.FileID(path.Join(joined, p))
	}
	base := func(ext string) source.FileID { return source.FileID(joined + ext) }

	var c []language.ModuleCandidate
	switch ext := path.Ext(joined); {
	case spec == "." || spec == ".." || strings.HasSuffix(spec, "/") || strings.HasSuffix(spec, "/."):
		c = []language.ModuleCandidate{{File: at("index.ts"), Priority: 0}, {File: at("index.tsx"), Priority: 1}}
	case ext == ".ts" || ext == ".tsx":
		c = []language.ModuleCandidate{{File: source.FileID(joined)}}
	case ext == ".js" || ext == ".jsx":
		stem := strings.TrimSuffix(joined, ext)
		c = []language.ModuleCandidate{{File: source.FileID(stem + ".ts")}, {File: source.FileID(stem + ".tsx")}}
	default:
		c = []language.ModuleCandidate{
			{File: base(".ts"), Priority: 0},
			{File: base(".tsx"), Priority: 1},
			{File: at("index.ts"), Priority: 2},
			{File: at("index.tsx"), Priority: 3},
		}
	}
	sort.SliceStable(c, func(i, j int) bool {
		if c[i].Priority != c[j].Priority {
			return c[i].Priority < c[j].Priority
		}
		return c[i].File < c[j].File
	})
	m.Candidates = c
	return m
}

// importStatement records the legacy module dependency (ImportDraft, one per
// statement as before) and one BindingDraft per bound local name.
func (e *extractor) importStatement(n *ts.Node) {
	spec := e.stringValue(e.field(n, "source"))
	if spec == "" {
		return
	}
	e.imports = append(e.imports, language.ImportDraft{Path: spec, Location: nodeLocation(n, e.file)})

	mod := moduleSpec(e.file, spec)
	typeOnly := e.hasToken(n, "type")
	bind := func(at *ts.Node, local, imported string, kind language.BindingKind, to bool) {
		if local == "" {
			return
		}
		e.bindings = append(e.bindings, language.BindingDraft{
			Local: local, Kind: kind, Module: mod, Imported: imported,
			TypeOnly: to, Location: nodeLocation(at, e.file),
		})
	}

	for i := range n.ChildCount() {
		clause := n.Child(i)
		if e.typ(clause) != "import_clause" {
			continue
		}
		for j := range clause.ChildCount() {
			c := clause.Child(j)
			switch e.typ(c) {
			case "identifier": // default import
				bind(c, e.text(c), "default", language.BindingNamed, typeOnly)
			case "namespace_import":
				for k := range c.ChildCount() {
					if id := c.Child(k); e.typ(id) == "identifier" {
						bind(c, e.text(id), "", language.BindingNamespace, typeOnly)
					}
				}
			case "named_imports":
				for k := range c.ChildCount() {
					spc := c.Child(k)
					if e.typ(spc) != "import_specifier" {
						continue
					}
					name := e.text(e.field(spc, "name"))
					local := name
					if a := e.field(spc, "alias"); a != nil {
						local = e.text(a)
					}
					bind(spc, local, name, language.BindingNamed, typeOnly || e.hasToken(spc, "type"))
				}
			}
		}
	}
}

// exportStatement records the module's export table and extracts any
// exported declaration. ExportDrafts are the complete export table of the
// file: a declaration without an `export` is not importable.
func (e *extractor) exportStatement(n *ts.Node) {
	isDefault := e.hasToken(n, "default")
	typeOnly := e.hasToken(n, "type")
	loc := nodeLocation(n, e.file)
	export := func(d language.ExportDraft) {
		d.Location = loc
		e.exports = append(e.exports, d)
	}

	// export <declaration> / export default <declaration>
	if decl := e.field(n, "declaration"); decl != nil {
		info, ok := e.declaration(decl, true)
		if !ok {
			e.walk(decl, scope{})
			return
		}
		if isDefault {
			if len(info.names) > 0 {
				export(language.ExportDraft{Kind: language.ExportLocal, Exported: "default", Local: info.names[0], TypeOnly: info.typeOnly})
			}
			return
		}
		for _, name := range info.names {
			export(language.ExportDraft{Kind: language.ExportLocal, Exported: name, Local: name, TypeOnly: info.typeOnly})
		}
		return
	}

	// export default <expression>
	if val := e.field(n, "value"); val != nil {
		if e.typ(val) == "identifier" {
			export(language.ExportDraft{Kind: language.ExportLocal, Exported: "default", Local: e.text(val)})
		} else {
			e.walk(val, scope{}) // anonymous default export: no symbol identity
		}
		return
	}

	// export { ... } [from "..."], export * [as ns] from "..."
	srcNode := e.field(n, "source")
	var mod language.ModuleSpec
	if srcNode != nil {
		spec := e.stringValue(srcNode)
		if spec == "" {
			return
		}
		e.imports = append(e.imports, language.ImportDraft{Path: spec, Location: loc})
		mod = moduleSpec(e.file, spec)
	}

	for i := range n.ChildCount() {
		c := n.Child(i)
		switch e.typ(c) {
		case "export_clause":
			for j := range c.ChildCount() {
				spc := c.Child(j)
				if e.typ(spc) != "export_specifier" {
					continue
				}
				name := e.text(e.field(spc, "name"))
				exported := name
				if a := e.field(spc, "alias"); a != nil {
					exported = e.text(a)
				}
				if name == "" || exported == "" {
					continue
				}
				d := language.ExportDraft{Exported: exported, Local: name, TypeOnly: typeOnly || e.hasToken(spc, "type")}
				if srcNode != nil {
					d.Kind, d.Module = language.ExportFrom, mod
				} else {
					d.Kind = language.ExportLocal
				}
				export(d)
			}
		case "namespace_export":
			if srcNode == nil {
				continue
			}
			for j := range c.ChildCount() {
				if id := c.Child(j); e.typ(id) == "identifier" {
					export(language.ExportDraft{Kind: language.ExportNamespace, Exported: e.text(id), Module: mod, TypeOnly: typeOnly})
				}
			}
		case "*":
			// `export * from` (no namespace_export sibling): forwards every
			// export except `default`, per ES semantics.
			if srcNode != nil && !e.hasToken(n, "namespace_export") {
				export(language.ExportDraft{Kind: language.ExportAll, Module: mod, Except: []string{"default"}, TypeOnly: typeOnly})
			}
		}
	}
}
