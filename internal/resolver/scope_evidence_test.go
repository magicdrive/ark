package resolver_test

import (
	"fmt"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// The language-neutral evidence the Python provider introduced, driven with
// synthetic evidence: a module candidate a file's presence voids
// (ModuleCandidate.Unless), a declaration's own cap (SymbolDraft.
// ConfidenceCap), an import binding local to a declaration's body
// (BindingDraft.Scope), nested declarations visible only within their
// parent (Extraction.LexicalNesting), a dotted receiver through namespace
// exports, and a supertype reference whose evidence is capped.

func modUnless(spec, file string, unless ...string) language.ModuleSpec {
	c := language.ModuleCandidate{File: source.FileID(file)}
	for _, u := range unless {
		c.Unless = append(c.Unless, source.FileID(u))
	}
	return language.ModuleSpec{Specifier: spec, Candidates: []language.ModuleCandidate{c}}
}

// A candidate whose Unless file is indexed does not exist: the binding names
// no repository file (Unresolved, never outside: the specifier had
// candidates).
func TestModuleCandidate_UnlessVoidsTheCandidate(t *testing.T) {
	r := newRepo()
	r.file("pkg/json.py", true).fn("loads").exportLocal("loads", "loads")
	r.file("pkg/__init__.py", true)
	r.file("pkg/a.py", true).fn("run").bind("loads", "loads", modUnless("json", "pkg/json.py", "pkg/__init__.py"), false)
	res := r.resolve(t, "pkg/a.py", "loads", container("run"))
	wantNoEdge(t, res, resolver.ConfidenceUnresolved)
	if res.OutsideRepository {
		t.Fatalf("voided candidate read as outside: %s", describe(res))
	}
	// The control: the same candidate where no Unless file exists.
	r2 := newRepo()
	r2.file("scripts/json.py", true).fn("loads").exportLocal("loads", "loads")
	r2.file("scripts/a.py", true).fn("run").bind("loads", "loads", modUnless("json", "scripts/json.py", "scripts/__init__.py"), false)
	wantTarget(t, r2.resolve(t, "scripts/a.py", "loads", container("run")), resolver.ConfidenceExact, "scripts/json.py", "loads")
}

func (f *fileB) capped(qualified, limit string) *fileB {
	for i := range f.fi.Symbols {
		if f.fi.Symbols[i].Qualified == qualified {
			f.fi.Symbols[i].ConfidenceCap = limit
		}
	}
	return f
}

// A declaration's cap lowers a resolution whose one candidate it is, never
// raises one, and leaves a resolution among several (a Candidate) alone.
func TestDeclarationCap_OnlyLowers(t *testing.T) {
	r := newRepo()
	r.file("m.py", true).fn("decorated").capped("decorated", "strong").fn("plain").
		fn("weak").capped("weak", "candidate").fn("run")
	wantTarget(t, r.resolve(t, "m.py", "decorated", container("run")), resolver.ConfidenceStrong, "m.py", "decorated")
	wantTarget(t, r.resolve(t, "m.py", "plain", container("run")), resolver.ConfidenceExact, "m.py", "plain")
	wantNoEdge(t, r.resolve(t, "m.py", "weak", container("run")), resolver.ConfidenceCandidate)

	// Through an import binding of another file, too.
	r.file("use.py", true).fn("go").bind("decorated", "decorated", mod("m", "m.py"), false)
	r.files[0].exportLocal("decorated", "decorated")
	wantTarget(t, r.resolve(t, "use.py", "decorated", container("go")), resolver.ConfidenceStrong, "m.py", "decorated")

	// Two declarations: a Candidate, which the cap does not raise or change.
	r3 := newRepo()
	r3.file("d.py", true).fn("dup").fn("dup").capped("dup", "strong").fn("run")
	wantNoEdge(t, r3.resolve(t, "d.py", "dup", container("run")), resolver.ConfidenceCandidate)
}

func (f *fileB) bindScoped(local, imported, scope string, m language.ModuleSpec) *fileB {
	f.bind(local, imported, m, false)
	f.fi.Bindings[len(f.fi.Bindings)-1].Scope = scope
	return f
}

// A binding local to a declaration's body binds the name there and in the
// declarations nested in it — shadowing the file's binding — and nowhere
// else; it never makes a module export.
func TestBindingScope(t *testing.T) {
	r := newRepo()
	r.file("util.py", true).fn("helper").exportLocal("helper", "helper")
	r.file("other.py", true).fn("helper").exportLocal("helper", "helper")
	r.file("main.py", true).fn("f").fn("g").fn("ff").
		bind("helper", "helper", mod("util", "util.py"), false).
		bindScoped("helper", "helper", "f", mod("other", "other.py")).
		bindScoped("only", "helper", "f", mod("other", "other.py")).
		exportLocal("only", "only")
	wantTarget(t, r.resolve(t, "main.py", "helper", container("f")), resolver.ConfidenceExact, "other.py", "helper")
	wantTarget(t, r.resolve(t, "main.py", "helper", container("f.inner")), resolver.ConfidenceExact, "other.py", "helper")
	wantTarget(t, r.resolve(t, "main.py", "helper", container("g")), resolver.ConfidenceExact, "util.py", "helper")
	// "ff" is not nested in "f": the prefix is a qualified name's.
	wantTarget(t, r.resolve(t, "main.py", "helper", container("ff")), resolver.ConfidenceExact, "util.py", "helper")
	wantNoEdge(t, r.resolve(t, "main.py", "only", container("g")), resolver.ConfidenceCandidate)
	// The export of only names no binding of the whole file.
	r.file("use.py", true).fn("go").bind("only", "only", mod("main", "main.py"), false)
	wantNoEdge(t, r.resolve(t, "use.py", "only", container("go")), resolver.ConfidenceCandidate)
}

func (f *fileB) nested(qualified, parent string, kind symbol.SymbolKind) *fileB {
	return f.sym(qualified, kind, parent, "")
}

// LexicalNesting: a nested declaration is a name only within its parent; a
// file that does not state it keeps the same-file stage as it was.
func TestLexicalNesting(t *testing.T) {
	r := newRepo()
	f := r.file("main.py", true).fn("helper").fn("outer").nested("outer.helper", "outer", symbol.KindFunction).fn("other").
		nested("outer.only", "outer", symbol.KindFunction)
	f.fi.LexicalNesting = true
	wantTarget(t, r.resolve(t, "main.py", "helper", container("other")), resolver.ConfidenceExact, "main.py", "helper")
	wantTarget(t, r.resolve(t, "main.py", "helper", container("outer")), resolver.ConfidenceExact, "main.py", "outer.helper")
	wantNoEdge(t, r.resolve(t, "main.py", "only", container("other")), resolver.ConfidenceCandidate)
	// From a function nested deeper in outer, both are in scope by name:
	// no pick.
	wantNoEdge(t, r.resolve(t, "main.py", "helper", container("outer.deep")), resolver.ConfidenceCandidate)
	wantTarget(t, r.resolve(t, "main.py", "only", container("outer.deep")), resolver.ConfidenceExact, "main.py", "outer.only")

	r2 := newRepo()
	r2.file("main.ts", true).fn("helper").fn("outer").nested("outer.helper", "outer", symbol.KindFunction).fn("other")
	wantNoEdge(t, r2.resolve(t, "main.ts", "helper", container("other")), resolver.ConfidenceCandidate)
}

// A dotted receiver whose leading part is a module binding resolves through
// the namespace exports of the modules it reaches; a step that reaches
// anything but modules leaves the receiver untyped.
func TestNamespaceChain(t *testing.T) {
	r := newRepo()
	r.file("pkg/__init__.py", true).fn("api").exportLocal("api", "api").
		exportNamespace("core", mod(".core", "pkg/core.py"))
	r.file("pkg/core.py", true).fn("run").exportLocal("run", "run")
	r.file("main.py", true).fn("go").bind("pkg", "", mod("pkg", "pkg/__init__.py"), false)
	wantTarget(t, r.resolve(t, "main.py", "run", recv("pkg.core"), container("go")), resolver.ConfidenceExact, "pkg/core.py", "run")
	// A missing name of a repository module: Unresolved, never outside.
	res := r.resolve(t, "main.py", "gone", recv("pkg.core"), container("go"))
	wantNoEdge(t, res, resolver.ConfidenceUnresolved)
	if res.OutsideRepository {
		t.Fatalf("outside: %s", describe(res))
	}
	// pkg.api is a function: the chain stops, the receiver is untyped.
	wantNoEdge(t, r.resolve(t, "main.py", "run", recv("pkg.api"), container("go")), resolver.ConfidenceCandidate)
	// pkg.nothing: no export, untyped as well.
	wantNoEdge(t, r.resolve(t, "main.py", "run", recv("pkg.nothing"), container("go")), resolver.ConfidenceCandidate)

	// Two exports of the step's name: no pick.
	r2 := newRepo()
	r2.file("pkg/__init__.py", true).exportNamespace("core", mod(".core", "pkg/core.py")).
		exportNamespace("core", mod(".core2", "pkg/core2.py"))
	r2.file("pkg/core.py", true).fn("run").exportLocal("run", "run")
	r2.file("pkg/core2.py", true).fn("run").exportLocal("run", "run")
	r2.file("main.py", true).fn("go").bind("pkg", "", mod("pkg", "pkg/__init__.py"), false)
	wantNoEdge(t, r2.resolve(t, "main.py", "run", recv("pkg.core"), container("go")), resolver.ConfidenceCandidate)

	// A step in the middle of a longer chain that offers two modules stays
	// no pick, even where the next step brings both to one module.
	r3 := newRepo()
	r3.file("pkg/__init__.py", true).exportNamespace("a", mod(".a1", "pkg/a1.py")).exportNamespace("a", mod(".a2", "pkg/a2.py"))
	r3.file("pkg/a1.py", true).exportNamespace("b", mod(".b", "pkg/b.py"))
	r3.file("pkg/a2.py", true).exportNamespace("b", mod(".b", "pkg/b.py"))
	r3.file("pkg/b.py", true).fn("run").exportLocal("run", "run")
	r3.file("main.py", true).fn("go").bind("pkg", "", mod("pkg", "pkg/__init__.py"), false)
	wantNoEdge(t, r3.resolve(t, "main.py", "run", recv("pkg.a.b"), container("go")), resolver.ConfidenceCandidate)
}

// A supertype reference the provider capped identifies no supertype: a
// member the class does not declare stays unresolved rather than found on
// a base the evidence does not prove.
func TestInheritance_CappedBaseIsUnidentified(t *testing.T) {
	build := func(cap string) *repoB {
		r := newRepo()
		f := r.file("m.py", true).class("Base").method("Base", "load").class("C").method("C", "run")
		f.fi.References = append(f.fi.References, reference.Reference{
			ID: reference.NewReferenceID("lang", "m.py", reference.KindInheritance, "Base", f.at()), Name: "Base",
			Kind: reference.KindInheritance, Language: "lang", Container: "C", Location: f.at(), ConfidenceCap: cap,
		})
		return r
	}
	wantTarget(t, build("").resolve(t, "m.py", "load", recvType("C"), recv("self"), container("C.run")), resolver.ConfidenceExact, "m.py", "Base.load")
	wantNoEdge(t, build("candidate").resolve(t, "m.py", "load", recvType("C"), recv("self"), container("C.run")), resolver.ConfidenceCandidate)
}

// An opaque export of a name beside a declaration of it found another way
// (an export-all) is no pick: the name may hold the unproven value.
func TestExportOpaque_BesideATargetIsCandidate(t *testing.T) {
	r := newRepo()
	r.file("m.py", true).fn("f").exportLocal("f", "f")
	f := r.file("pkg.py", true).exportAll(mod("m", "m.py"))
	f.fi.Exports = append(f.fi.Exports, language.ExportDraft{Kind: language.ExportOpaque, Location: f.at()})
	r.file("use.py", true).fn("go").bind("f", "f", mod("pkg", "pkg.py"), false)
	wantNoEdge(t, r.resolve(t, "use.py", "f", container("go")), resolver.ConfidenceCandidate)
	// An opaque name stops a Fallback binding; without it the fallback applies.
	r2 := newRepo()
	g := r2.file("pkg/__init__.py", true)
	g.fi.Exports = append(g.fi.Exports, language.ExportDraft{Kind: language.ExportOpaque, Exported: "n", Location: g.at()})
	r2.file("pkg/n.py", true).fn("run").exportLocal("run", "run")
	u := r2.file("use.py", true).fn("go").bind("n", "n", mod("pkg", "pkg/__init__.py"), false).bind("n", "", mod("pkg.n", "pkg/n.py"), false)
	u.fi.Bindings[1].Fallback = true
	wantNoEdge(t, r2.resolve(t, "use.py", "run", recv("n"), container("go")), resolver.ConfidenceUnresolved)
	g.fi.Exports = nil
	wantTarget(t, r2.resolve(t, "use.py", "run", recv("n"), container("go")), resolver.ConfidenceExact, "pkg/n.py", "run")
}

// A member found on a type whose declaration states a cap is capped too:
// the type's name may denote another type.
func TestDeclarationCap_ReachesMembersThroughTheType(t *testing.T) {
	r := newRepo()
	r.file("base.py", true).class("A").method("A", "m").class("B").capped("B", "candidate").method("B", "own").
		exportLocal("B", "B")
	r.file("use.py", true).fn("go").bind("B", "B", mod("base", "base.py"), false)
	wantNoEdge(t, r.resolve(t, "use.py", "own", recv("B"), container("go")), resolver.ConfidenceCandidate)
	wantNoEdge(t, r.resolve(t, "use.py", "own", recv("b"), recvType("B"), container("go")), resolver.ConfidenceCandidate)
}

// A lookup the depth bound cut off proves no name absent: a Fallback
// binding does not apply after it.
func TestFallback_NotAfterATruncatedLookup(t *testing.T) {
	r := newRepo()
	pkg := r.file("pkg/__init__.py", true).bind("n", "n", mod(".r1", "pkg/r1.py"), false).exportLocal("n", "n")
	_ = pkg
	for i := 1; i < 20; i++ {
		r.file(fmt.Sprintf("pkg/r%d.py", i), true).bind("n", "n", mod(fmt.Sprintf(".r%d", i+1), fmt.Sprintf("pkg/r%d.py", i+1)), false).exportLocal("n", "n")
	}
	r.file("pkg/r20.py", true).fn("n").exportLocal("n", "n")
	r.file("pkg/n.py", true).fn("run").exportLocal("run", "run")
	u := r.file("main.py", true).fn("go").bind("n", "n", mod("pkg", "pkg/__init__.py"), false).bind("n", "", mod("pkg.n", "pkg/n.py"), false)
	u.fi.Bindings[1].Fallback = true
	wantNoEdge(t, r.resolve(t, "main.py", "run", recv("n"), container("go")), resolver.ConfidenceCandidate)
}

// An opaque name beside a re-export of a module outside the repository is
// the repository's, unrepresented: never outside.
func TestExportOpaque_BesideAnExternalReExportIsNotOutside(t *testing.T) {
	r := newRepo()
	f := r.file("pkg.py", true).exportAll(language.ModuleSpec{Specifier: "external"})
	f.fi.Exports = append(f.fi.Exports, language.ExportDraft{Kind: language.ExportOpaque, Exported: "f", Location: f.at()})
	r.file("use.py", true).fn("go").bind("f", "f", mod("pkg", "pkg.py"), false)
	res := r.resolve(t, "use.py", "f", container("go"))
	wantNoEdge(t, res, resolver.ConfidenceUnresolved)
	if res.OutsideRepository {
		t.Fatalf("outside: %s", describe(res))
	}
}
