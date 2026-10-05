package resolver_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// --- a tiny repository builder over resolver.FileIndex -----------------------

type repoB struct {
	files []*fileB
}

type fileB struct {
	fi   resolver.FileIndex
	line uint32
}

func newRepo() *repoB { return &repoB{} }

func (r *repoB) file(path string, moduleScoped bool) *fileB {
	f := &fileB{fi: resolver.FileIndex{FileID: source.FileID(path), Language: "lang", ModuleScoped: moduleScoped}}
	r.files = append(r.files, f)
	return f
}

func (f *fileB) at() source.Location {
	f.line++
	return source.Location{File: f.fi.FileID, Range: source.Range{
		Start: source.Position{Line: f.line, Column: 1}, End: source.Position{Line: f.line, Column: 2}}}
}

// sym declares a symbol. parent is the enclosing qualified name ("" for top
// level); receiver is the declaring type name for members.
func (f *fileB) sym(qualified string, kind symbol.SymbolKind, parent, receiver string) *fileB {
	name := qualified
	if i := strings.LastIndex(qualified, "."); i >= 0 {
		name = qualified[i+1:]
	}
	f.fi.Symbols = append(f.fi.Symbols, symbol.Symbol{
		ID:              symbol.NewSymbolID("lang", string(f.fi.FileID), kind, qualified),
		Name:            name,
		Qualified:       qualified,
		Kind:            kind,
		Language:        "lang",
		Location:        f.at(),
		ParentQualified: parent,
		Receiver:        receiver,
	})
	return f
}

func (f *fileB) class(name string) *fileB { return f.sym(name, symbol.KindClass, "", "") }
func (f *fileB) method(class, name string) *fileB {
	return f.sym(class+"."+name, symbol.KindMethod, class, class)
}
func (f *fileB) fn(name string) *fileB { return f.sym(name, symbol.KindFunction, "", "") }

func mod(spec string, files ...string) language.ModuleSpec {
	m := language.ModuleSpec{Specifier: spec}
	for _, p := range files {
		m.Candidates = append(m.Candidates, language.ModuleCandidate{File: source.FileID(p)})
	}
	return m
}

func (f *fileB) bind(local, imported string, m language.ModuleSpec, typeOnly bool) *fileB {
	kind := language.BindingNamed
	if imported == "" {
		kind = language.BindingNamespace
	}
	f.fi.Bindings = append(f.fi.Bindings, language.BindingDraft{
		Local: local, Kind: kind, Imported: imported, Module: m, TypeOnly: typeOnly, Location: f.at()})
	return f
}

func (f *fileB) exportLocal(exported, local string) *fileB {
	f.fi.Exports = append(f.fi.Exports, language.ExportDraft{
		Kind: language.ExportLocal, Exported: exported, Local: local, Location: f.at()})
	return f
}

func (f *fileB) exportFrom(exported, local string, m language.ModuleSpec) *fileB {
	f.fi.Exports = append(f.fi.Exports, language.ExportDraft{
		Kind: language.ExportFrom, Exported: exported, Local: local, Module: m, Location: f.at()})
	return f
}

func (f *fileB) exportAll(m language.ModuleSpec, except ...string) *fileB {
	f.fi.Exports = append(f.fi.Exports, language.ExportDraft{
		Kind: language.ExportAll, Module: m, Except: except, Location: f.at()})
	return f
}

func (f *fileB) exportNamespace(exported string, m language.ModuleSpec) *fileB {
	f.fi.Exports = append(f.fi.Exports, language.ExportDraft{
		Kind: language.ExportNamespace, Exported: exported, Module: m, Location: f.at()})
	return f
}

type refOpt func(*reference.Reference)

func recv(expr string) refOpt   { return func(r *reference.Reference) { r.ReceiverExpr = expr } }
func recvType(t string) refOpt  { return func(r *reference.Reference) { r.ReceiverType = t } }
func container(c string) refOpt { return func(r *reference.Reference) { r.Container = c } }
func kind(k reference.ReferenceKind) refOpt {
	return func(r *reference.Reference) { r.Kind = k; r.IsCall = k == reference.KindCall }
}

// resolve resolves one reference written in file path.
func (r *repoB) resolve(t *testing.T, path, name string, opts ...refOpt) resolver.Resolution {
	t.Helper()
	var files []resolver.FileIndex
	var host *resolver.FileIndex
	for _, f := range r.files {
		files = append(files, f.fi)
	}
	for i := range files {
		if string(files[i].FileID) == path {
			host = &files[i]
		}
	}
	if host == nil {
		t.Fatalf("no file %s", path)
	}
	l := source.Location{File: host.FileID, Range: source.Range{Start: source.Position{Line: 900, Column: 1}}}
	ref := reference.Reference{Name: name, Kind: reference.KindCall, IsCall: true, Language: "lang", Location: l}
	for _, o := range opts {
		o(&ref)
	}
	ref.ID = reference.NewReferenceID("lang", host.FileID, ref.Kind, ref.Name, l)
	return resolver.New(files).ResolveReference(ref, *host)
}

func describe(res resolver.Resolution) string {
	var qs []string
	for _, c := range res.Candidates {
		qs = append(qs, fmt.Sprintf("%s:%s", c.File, c.Qualified))
	}
	return fmt.Sprintf("%s %v %v", res.Confidence, qs, res.Evidence)
}

func wantTarget(t *testing.T, res resolver.Resolution, conf resolver.Confidence, file, qualified string) {
	t.Helper()
	if res.Confidence != conf || len(res.Candidates) != 1 ||
		string(res.Candidates[0].File) != file || res.Candidates[0].Qualified != qualified {
		t.Fatalf("want %s %s:%s, got %s", conf, file, qualified, describe(res))
	}
}

// wantNoEdge asserts the resolution cannot produce a graph edge.
func wantNoEdge(t *testing.T, res resolver.Resolution, maxConf resolver.Confidence) {
	t.Helper()
	if res.HasUniqueTarget() || res.Confidence > maxConf {
		t.Fatalf("want at most %s and no unique target, got %s", maxConf, describe(res))
	}
}

// ============================================================================
// STOP-3: receiver classification
// ============================================================================

// R4: `repo.save()` with only ReceiverExpr evidence must never be Exact,
// even when the same file declares exactly one `save` (old Stage 2 behaviour).
func TestR4_UntypedReceiverSameFileIsNotExact(t *testing.T) {
	r := newRepo()
	r.file("src/orders.ts", false).class("OrderRepository").method("OrderRepository", "save").fn("persist")
	res := r.resolve(t, "src/orders.ts", "save", recv("repo"), container("persist"))
	wantNoEdge(t, res, resolver.ConfidenceCandidate)
	if len(res.Candidates) != 1 {
		t.Errorf("candidate evidence must be kept, got %s", describe(res))
	}
}

// R4: repository-wide uniqueness is not receiver evidence.
func TestR4_UntypedReceiverUniqueRepoIsNotStrong(t *testing.T) {
	r := newRepo()
	r.file("a/repo.go", false).sym("UserRepository", symbol.KindStruct, "", "").
		sym("UserRepository.Save", symbol.KindMethod, "", "UserRepository")
	r.file("b/svc.go", false).fn("Run")
	wantNoEdge(t, r.resolve(t, "b/svc.go", "Save", recv("repo"), container("Run")), resolver.ConfidenceCandidate)
}

// R4: the receiver-name suffix heuristic (`userRepository` ~ UserRepository)
// is not type evidence.
func TestR4_ReceiverSuffixHeuristicIsNotStrong(t *testing.T) {
	r := newRepo()
	r.file("a/repo.go", false).sym("UserRepository", symbol.KindStruct, "", "").
		sym("UserRepository.Save", symbol.KindMethod, "", "UserRepository").
		sym("OrderRepository", symbol.KindStruct, "", "").
		sym("OrderRepository.Save", symbol.KindMethod, "", "OrderRepository")
	r.file("b/svc.go", false).fn("Run")
	wantNoEdge(t, r.resolve(t, "b/svc.go", "Save", recv("userRepository"), container("Run")), resolver.ConfidenceCandidate)
}

// R2: declared receiver type evidence re-promotes. Contained member in the
// type's file → Exact; the unrelated same-name member is never chosen.
func TestR2_ReceiverTypeContainedMemberExact(t *testing.T) {
	r := newRepo()
	r.file("src/repos.ts", false).
		class("UserRepository").method("UserRepository", "save").
		class("OrderRepository").method("OrderRepository", "save").
		fn("persist")
	res := r.resolve(t, "src/repos.ts", "save", recv("repo"), recvType("UserRepository"), container("persist"))
	wantTarget(t, res, resolver.ConfidenceExact, "src/repos.ts", "UserRepository.save")
}

// R2: receiver-attached members (no lexical containment, e.g. Go methods in
// another file of the package) are capped at Strong.
func TestR2_ReceiverTypeAttachedMemberStrong(t *testing.T) {
	r := newRepo()
	r.file("app/types.go", false).sym("Repository", symbol.KindStruct, "", "")
	r.file("app/methods.go", false).sym("Repository.Find", symbol.KindMethod, "", "Repository")
	r.file("app/svc.go", false).fn("Run")
	res := r.resolve(t, "app/svc.go", "Find", recv("r"), recvType("Repository"), container("Run"))
	wantTarget(t, res, resolver.ConfidenceStrong, "app/methods.go", "Repository.Find")
}

// R2 is authoritative: unresolvable declared type → Unresolved, no fallback
// to the same-file `save`.
func TestR2_UnresolvedTypeDoesNotFallBack(t *testing.T) {
	r := newRepo()
	r.file("src/a.ts", false).class("Other").method("Other", "save").fn("f")
	res := r.resolve(t, "src/a.ts", "save", recv("x"), recvType("Missing"), container("f"))
	if res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 {
		t.Fatalf("want unresolved, got %s", describe(res))
	}
}

// R2: declared type found but no such member (e.g. inherited) → Unresolved.
func TestR2_MissingMemberUnresolved(t *testing.T) {
	r := newRepo()
	r.file("src/a.ts", false).class("Child").class("Other").method("Other", "save").fn("f")
	res := r.resolve(t, "src/a.ts", "save", recv("c"), recvType("Child"), container("f"))
	if res.Confidence != resolver.ConfidenceUnresolved {
		t.Fatalf("want unresolved, got %s", describe(res))
	}
}

// R2: an ambiguous declared type yields Candidate.
func TestR2_AmbiguousTypeIsCandidate(t *testing.T) {
	r := newRepo()
	r.file("x/a.ts", false).class("Repo").method("Repo", "save")
	r.file("y/b.ts", false).class("Repo").method("Repo", "save")
	r.file("z/c.ts", false).fn("f")
	wantNoEdge(t, r.resolve(t, "z/c.ts", "save", recv("r"), recvType("Repo"), container("f")), resolver.ConfidenceCandidate)
}

// Legacy module receiver (Go `fmt.Println`, `pkg.Func`) is not an untyped
// variable: existing import resolution is kept.
func TestModuleReceiverUnaffected(t *testing.T) {
	r := newRepo()
	r.file("store/store.go", false).fn("Open")
	f := r.file("app/main.go", false).fn("main")
	f.fi.Imports = []language.ImportDraft{{Path: "example.com/x/store"}}
	res := r.resolve(t, "app/main.go", "Open", recv("store"), container("main"))
	wantTarget(t, res, resolver.ConfidenceExact, "store/store.go", "Open")
}

// R3: an explicit type-name receiver keeps the existing constrained behaviour.
func TestR3_TypeNameReceiverUnaffected(t *testing.T) {
	r := newRepo()
	r.file("src/a.php", false).class("User").method("User", "create").
		class("SuperUser").method("SuperUser", "create").fn("f")
	res := r.resolve(t, "src/a.php", "create", recv("User"), container("f"))
	wantTarget(t, res, resolver.ConfidenceExact, "src/a.php", "User.create")
}

// A receiverless name never denotes a receiver-attached member.
func TestFreeNameNeverResolvesToMember(t *testing.T) {
	r := newRepo()
	r.file("src/a.ts", false).class("Repo").method("Repo", "save").fn("f")
	res := r.resolve(t, "src/a.ts", "save", container("f"))
	if res.Confidence != resolver.ConfidenceUnresolved {
		t.Fatalf("free `save()` must not resolve to Repo.save, got %s", describe(res))
	}
}

// ============================================================================
// ModuleScoped
// ============================================================================

func TestModuleScoped_ProximityAndUniquenessCapped(t *testing.T) {
	r := newRepo()
	r.file("src/util.ts", true).fn("format")
	r.file("src/lib/only.ts", true).fn("unique")
	r.file("src/app.ts", true).fn("main")
	wantNoEdge(t, r.resolve(t, "src/app.ts", "format", container("main")), resolver.ConfidenceCandidate)
	wantNoEdge(t, r.resolve(t, "src/app.ts", "unique", container("main")), resolver.ConfidenceCandidate)
	// Local declarations are still Exact.
	wantTarget(t, r.resolve(t, "src/app.ts", "main", container("main")), resolver.ConfidenceExact, "src/app.ts", "main")
}

// Same inputs without ModuleScoped keep the legacy (Go-style) behaviour.
func TestNotModuleScoped_LegacyUnchanged(t *testing.T) {
	r := newRepo()
	r.file("src/util.go", false).fn("format")
	r.file("src/app.go", false).fn("main")
	wantTarget(t, r.resolve(t, "src/app.go", "format", container("main")), resolver.ConfidenceStrong, "src/util.go", "format")
}

// Legacy implicit ImportDraft alias matching is not applied to module-scoped
// files (a bare package name could substring-match an unrelated path).
func TestModuleScoped_NoLegacyImportMatch(t *testing.T) {
	r := newRepo()
	r.file("user/model.ts", true).fn("make").fn("other")
	f := r.file("app/a.ts", true).fn("main")
	f.fi.Imports = []language.ImportDraft{{Path: "user"}}
	wantNoEdge(t, r.resolve(t, "app/a.ts", "make", recv("user"), container("main")), resolver.ConfidenceCandidate)
}

// The legacy importMatch also matches dotted receiverless names
// ("user.make"); module-scoped files must not use it either.
func TestModuleScoped_NoLegacyDottedImportMatch(t *testing.T) {
	r := newRepo()
	r.file("user/model.ts", true).fn("make").fn("other")
	f := r.file("app/a.ts", true).fn("main")
	f.fi.Imports = []language.ImportDraft{{Path: "user"}}
	wantNoEdge(t, r.resolve(t, "app/a.ts", "user.make", container("main")), resolver.ConfidenceCandidate)
}

// ============================================================================
// Module bindings / exports / re-exports
// ============================================================================

func TestBinding_NamedAndAlias(t *testing.T) {
	r := newRepo()
	r.file("src/user.ts", true).class("User").exportLocal("User", "User")
	r.file("src/other/user.ts", true).class("User").exportLocal("User", "User")
	r.file("src/svc.ts", true).fn("f").
		bind("User", "User", mod("./user", "src/user.ts"), false).
		bind("DomainUser", "User", mod("./user", "src/user.ts"), false)
	wantTarget(t, r.resolve(t, "src/svc.ts", "User", container("f"), kind(reference.KindConstruction)),
		resolver.ConfidenceExact, "src/user.ts", "User")
	wantTarget(t, r.resolve(t, "src/svc.ts", "DomainUser", container("f"), kind(reference.KindConstruction)),
		resolver.ConfidenceExact, "src/user.ts", "User")
}

// External package / unsupported path alias: the bound name is Unresolved and
// never falls back to a same-name repository symbol.
func TestBinding_ExternalAndPathAliasNeverFallBack(t *testing.T) {
	r := newRepo()
	r.file("src/z.ts", true).fn("z").exportLocal("z", "z")
	r.file("src/shared/helper.ts", true).fn("helper").exportLocal("helper", "helper")
	r.file("src/app.ts", true).fn("main").
		bind("z", "z", mod("zod"), false).
		bind("helper", "helper", mod("@/shared/helper"), false)
	for _, name := range []string{"z", "helper"} {
		res := r.resolve(t, "src/app.ts", name, container("main"))
		if res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 {
			t.Errorf("%s: want unresolved without candidates, got %s", name, describe(res))
		}
	}
}

func TestBinding_DefaultExport(t *testing.T) {
	r := newRepo()
	r.file("src/user.ts", true).fn("makeUser").exportLocal("default", "makeUser")
	r.file("src/app.ts", true).fn("main").bind("mk", "default", mod("./user", "src/user.ts"), false)
	wantTarget(t, r.resolve(t, "src/app.ts", "mk", container("main")), resolver.ConfidenceExact, "src/user.ts", "makeUser")
}

// A declaration without an export entry is not importable (module export
// table = ExportDrafts only).
func TestBinding_NotExportedIsUnresolved(t *testing.T) {
	r := newRepo()
	r.file("src/user.ts", true).class("User")
	r.file("src/app.ts", true).fn("main").bind("User", "User", mod("./user", "src/user.ts"), false)
	if res := r.resolve(t, "src/app.ts", "User", container("main")); res.Confidence != resolver.ConfidenceUnresolved {
		t.Fatalf("want unresolved, got %s", describe(res))
	}
}

func TestBinding_TypeOnly(t *testing.T) {
	r := newRepo()
	r.file("src/user.ts", true).class("User").exportLocal("User", "User")
	r.file("src/app.ts", true).fn("main").bind("User", "User", mod("./user", "src/user.ts"), true)
	if res := r.resolve(t, "src/app.ts", "User", container("main"), kind(reference.KindConstruction)); res.Confidence != resolver.ConfidenceUnresolved {
		t.Errorf("type-only binding used as value: want unresolved, got %s", describe(res))
	}
	wantTarget(t, r.resolve(t, "src/app.ts", "User", container("main"), kind(reference.KindTypeUse)),
		resolver.ConfidenceExact, "src/user.ts", "User")
}

// Barrel chain: app → index (export *) → domain/index (export {X} from) →
// domain/user. The edge goes to the defining symbol.
func TestReExport_BarrelChain(t *testing.T) {
	r := newRepo()
	r.file("src/domain/user.ts", true).class("Account").exportLocal("Account", "Account")
	r.file("src/domain/index.ts", true).
		exportFrom("Account", "Account", mod("./user", "src/domain/user.ts")).
		exportFrom("DomainAccount", "Account", mod("./user", "src/domain/user.ts"))
	r.file("src/index.ts", true).exportAll(mod("./domain", "src/domain.ts", "src/domain/index.ts"), "default")
	r.file("src/app.ts", true).fn("run").
		bind("Account", "Account", mod("./index", "src/index.ts"), false).
		bind("Alias", "DomainAccount", mod("./index", "src/index.ts"), false)
	wantTarget(t, r.resolve(t, "src/app.ts", "Account", container("run"), kind(reference.KindConstruction)),
		resolver.ConfidenceExact, "src/domain/user.ts", "Account")
	wantTarget(t, r.resolve(t, "src/app.ts", "Alias", container("run"), kind(reference.KindConstruction)),
		resolver.ConfidenceExact, "src/domain/user.ts", "Account")
}

// Cyclic barrels terminate and never fabricate; a name really declared in
// the cycle is still found.
func TestReExport_CycleIsSafe(t *testing.T) {
	r := newRepo()
	r.file("c/a.ts", true).class("Alpha").exportLocal("Alpha", "Alpha").exportAll(mod("./b", "c/b.ts"))
	r.file("c/b.ts", true).class("Beta").exportLocal("Beta", "Beta").exportAll(mod("./a", "c/a.ts"))
	r.file("c/app.ts", true).fn("f").
		bind("Beta", "Beta", mod("./a", "c/a.ts"), false).
		bind("Ghost", "Ghost", mod("./a", "c/a.ts"), false)
	wantTarget(t, r.resolve(t, "c/app.ts", "Beta", container("f"), kind(reference.KindConstruction)),
		resolver.ConfidenceExact, "c/b.ts", "Beta")
	if res := r.resolve(t, "c/app.ts", "Ghost", container("f")); res.Confidence != resolver.ConfidenceUnresolved {
		t.Fatalf("cycle must yield unresolved, got %s", describe(res))
	}
}

// Two export-all sources providing the same name: ambiguity, not a choice.
func TestReExport_StarConflictIsCandidate(t *testing.T) {
	r := newRepo()
	r.file("m/a.ts", true).class("User").exportLocal("User", "User")
	r.file("m/b.ts", true).class("User").exportLocal("User", "User")
	r.file("m/index.ts", true).exportAll(mod("./a", "m/a.ts")).exportAll(mod("./b", "m/b.ts"))
	r.file("m/app.ts", true).fn("f").bind("User", "User", mod("./index", "m/index.ts"), false)
	wantNoEdge(t, r.resolve(t, "m/app.ts", "User", container("f")), resolver.ConfidenceCandidate)
}

// An explicit export shadows export-all; Except (e.g. "default") is honoured.
func TestReExport_ExplicitShadowsStarAndExcept(t *testing.T) {
	r := newRepo()
	r.file("m/a.ts", true).class("User").exportLocal("User", "User").fn("dflt").exportLocal("default", "dflt")
	r.file("m/b.ts", true).class("User").exportLocal("User", "User")
	r.file("m/index.ts", true).exportAll(mod("./a", "m/a.ts"), "default").
		exportFrom("User", "User", mod("./b", "m/b.ts"))
	r.file("m/app.ts", true).fn("f").
		bind("User", "User", mod("./index", "m/index.ts"), false).
		bind("D", "default", mod("./index", "m/index.ts"), false)
	wantTarget(t, r.resolve(t, "m/app.ts", "User", container("f"), kind(reference.KindConstruction)),
		resolver.ConfidenceExact, "m/b.ts", "User")
	if res := r.resolve(t, "m/app.ts", "D", container("f")); res.Confidence != resolver.ConfidenceUnresolved {
		t.Fatalf("Except must not forward default, got %s", describe(res))
	}
}

// Chains longer than the depth bound yield Unresolved (bounded, no guess).
func TestReExport_DepthBounded(t *testing.T) {
	r := newRepo()
	const n = 40
	r.file("d/f0.ts", true).class("Deep").exportLocal("Deep", "Deep")
	for i := 1; i <= n; i++ {
		p := fmt.Sprintf("d/f%d.ts", i)
		r.file(p, true).exportAll(mod(fmt.Sprintf("./f%d", i-1), fmt.Sprintf("d/f%d.ts", i-1)))
	}
	r.file("d/app.ts", true).fn("f").
		bind("Deep", "Deep", mod("./f40", fmt.Sprintf("d/f%d.ts", n)), false).
		bind("Near", "Deep", mod("./f3", "d/f3.ts"), false)
	if res := r.resolve(t, "d/app.ts", "Deep", container("f")); res.Confidence != resolver.ConfidenceUnresolved {
		t.Fatalf("over-deep chain must be unresolved, got %s", describe(res))
	}
	wantTarget(t, r.resolve(t, "d/app.ts", "Near", container("f"), kind(reference.KindConstruction)),
		resolver.ConfidenceExact, "d/f0.ts", "Deep")
}

// Module candidate priority: provider-ranked priority is evidence; equally
// ranked existing candidates are ambiguity.
func TestModuleCandidatePriority(t *testing.T) {
	r := newRepo()
	r.file("p/user.ts", true).class("User").exportLocal("User", "User")
	r.file("p/user.tsx", true).class("User").exportLocal("User", "User")
	ranked := language.ModuleSpec{Specifier: "./user", Candidates: []language.ModuleCandidate{
		{File: "p/user.ts"}, {File: "p/user.tsx", Priority: 1}, {File: "p/user/index.ts", Priority: 2}}}
	unranked := mod("./user", "p/user.ts", "p/user.tsx")
	missingFirst := language.ModuleSpec{Specifier: "./user", Candidates: []language.ModuleCandidate{
		{File: "p/nope.ts"}, {File: "p/user.tsx", Priority: 1}}}
	r.file("p/app.ts", true).fn("f").
		bind("Ranked", "User", ranked, false).
		bind("Unranked", "User", unranked, false).
		bind("Fallback", "User", missingFirst, false)
	wantTarget(t, r.resolve(t, "p/app.ts", "Ranked", container("f"), kind(reference.KindConstruction)),
		resolver.ConfidenceExact, "p/user.ts", "User")
	wantNoEdge(t, r.resolve(t, "p/app.ts", "Unranked", container("f"), kind(reference.KindConstruction)), resolver.ConfidenceCandidate)
	wantTarget(t, r.resolve(t, "p/app.ts", "Fallback", container("f"), kind(reference.KindConstruction)),
		resolver.ConfidenceExact, "p/user.tsx", "User")
}

// R1: namespace binding receiver, named binding of a type as receiver, and a
// namespace re-export.
func TestR1_BindingReceivers(t *testing.T) {
	r := newRepo()
	r.file("s/user.ts", true).class("User").method("User", "create").exportLocal("User", "User").
		fn("helper").exportLocal("helper", "helper")
	r.file("s/order.ts", true).class("Order").method("Order", "create")
	r.file("s/index.ts", true).exportNamespace("users", mod("./user", "s/user.ts"))
	r.file("s/app.ts", true).fn("f").
		bind("ns", "", mod("./user", "s/user.ts"), false).
		bind("U", "User", mod("./user", "s/user.ts"), false).
		bind("users", "users", mod("./index", "s/index.ts"), false)
	wantTarget(t, r.resolve(t, "s/app.ts", "helper", recv("ns"), container("f")), resolver.ConfidenceExact, "s/user.ts", "helper")
	wantTarget(t, r.resolve(t, "s/app.ts", "create", recv("U"), container("f")), resolver.ConfidenceExact, "s/user.ts", "User.create")
	wantTarget(t, r.resolve(t, "s/app.ts", "helper", recv("users"), container("f")), resolver.ConfidenceExact, "s/user.ts", "helper")
	if res := r.resolve(t, "s/app.ts", "missing", recv("ns"), container("f")); res.Confidence != resolver.ConfidenceUnresolved {
		t.Fatalf("missing namespace member must be unresolved, got %s", describe(res))
	}
}

// A re-exported import binding (`import {A} from; export {A}`) is followed.
func TestReExport_LocalExportOfImportBinding(t *testing.T) {
	r := newRepo()
	r.file("q/a.ts", true).class("A").exportLocal("A", "A")
	r.file("q/barrel.ts", true).bind("A", "A", mod("./a", "q/a.ts"), false).exportLocal("A", "A")
	r.file("q/app.ts", true).fn("f").bind("A", "A", mod("./barrel", "q/barrel.ts"), false)
	wantTarget(t, r.resolve(t, "q/app.ts", "A", container("f"), kind(reference.KindConstruction)), resolver.ConfidenceExact, "q/a.ts", "A")
}

// Same inputs, many runs, identical output.
func TestModuleResolutionDeterministic(t *testing.T) {
	build := func() string {
		r := newRepo()
		r.file("m/a.ts", true).class("User").exportLocal("User", "User")
		r.file("m/b.ts", true).class("User").exportLocal("User", "User")
		r.file("m/index.ts", true).exportAll(mod("./a", "m/a.ts")).exportAll(mod("./b", "m/b.ts"))
		r.file("m/app.ts", true).fn("f").bind("User", "User", mod("./index", "m/index.ts"), false)
		return describe(r.resolve(t, "m/app.ts", "User", container("f")))
	}
	want := build()
	for i := 0; i < 100; i++ {
		if got := build(); got != want {
			t.Fatalf("run %d: %s != %s", i, got, want)
		}
	}
}
