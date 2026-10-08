package terraform

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	arkcontext "github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/golden"
	"github.com/magicdrive/ark/internal/graph"
	"github.com/magicdrive/ark/internal/impact"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/python"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

const fixtureRepo = "testdata/repo"

func buildIndex(t testing.TB, dir string, providers ...language.Provider) *index.RepositoryIndex {
	t.Helper()
	if len(providers) == 0 {
		providers = []language.Provider{NewProvider()}
	}
	idx, err := index.New(context.Background(), dir, providers)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx
}

// edgeLines renders every forward edge as "from -kind-> to confidence".
func edgeLines(idx *index.RepositoryIndex) []string {
	var out []string
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			out = append(out, fmt.Sprintf("%s -%s-> %s %s", s.Qualified, e.Kind, to.Qualified, e.Confidence))
		}
	}
	sort.Strings(out)
	return out
}

func onlySymbol(t *testing.T, idx *index.RepositoryIndex, qualified string) symbol.Symbol {
	t.Helper()
	syms := idx.FindSymbolsByQualified(qualified)
	if len(syms) != 1 {
		t.Fatalf("%s: %d symbols", qualified, len(syms))
	}
	return syms[0]
}

// TestGraph_MultiModuleFixture compares the whole graph of the multi-module
// fixture with the expected one. Every edge is Exact: Terraform's own module
// scoping proves it; nothing weaker ever forms an edge.
func TestGraph_MultiModuleFixture(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "edges", edgeLines(idx), []string{
		"aws_instance.web -depends_on-> aws_security_group.web exact",
		"aws_instance.web -references-> data.aws_ami.ubuntu exact",
		"aws_instance.web -references-> local.volumes exact",
		"aws_instance.web -references-> module.network exact",
		"aws_instance.web -references-> modules/network/output.subnet_id exact",
		"aws_instance.web -references-> var.instance_count exact",
		"aws_instance.web -references-> var.region exact",
		"aws_security_group.web -references-> var.region exact",
		"local.volumes -references-> var.volume_names exact",
		"local.web_ids -references-> aws_instance.web exact",
		"module.database -references-> module.network exact",
		"module.database -references-> modules/network/output.subnet_id exact",
		"module.network -references-> var.cidr exact",
		"modules/database/aws_db_instance.db -references-> modules/database/aws_vpc.main exact",
		"modules/database/aws_db_instance.db -references-> modules/database/var.subnet_id exact",
		"modules/database/output.endpoint -references-> modules/database/aws_db_instance.db exact",
		"modules/network/aws_subnet.private -references-> modules/network/aws_vpc.main exact",
		"modules/network/aws_vpc.main -references-> modules/network/var.cidr exact",
		"modules/network/local.subnet_id -references-> modules/network/aws_subnet.private exact",
		"modules/network/output.subnet_id -references-> modules/network/local.subnet_id exact",
		"modules/network/output.vpc_id -references-> modules/network/aws_vpc.main exact",
		"output.endpoint -references-> module.database exact",
		"output.endpoint -references-> modules/database/output.endpoint exact",
		"output.missing -references-> module.network exact",
		"output.network_id -references-> module.network exact",
		"output.network_id -references-> modules/network/output.vpc_id exact",
		"output.remote_vpc -references-> module.vpc_remote exact",
	})
}

// TestGraph_NoEdgeCrossesAModuleBoundaryWithoutBinding is the soundness
// property behind module scoping: an edge stays within one module directory,
// except the edge to a child module's output, which goes exactly to the
// directory the module call's local source names.
func TestGraph_NoEdgeCrossesAModuleBoundaryWithoutBinding(t *testing.T) {
	for _, dir := range []string{fixtureRepo, "../../golden/testdata/terraform"} {
		idx := buildIndex(t, dir)
		for _, s := range idx.FindSymbols("") {
			for _, e := range idx.GetCallees(s.ID) {
				to, _ := idx.GetSymbol(e.To)
				if e.Confidence != resolver.ConfidenceExact {
					t.Errorf("%s -> %s: confidence %s; Terraform edges are Exact or absent", s.Qualified, to.Qualified, e.Confidence)
				}
				from, toDir := path.Dir(string(s.Location.File)), path.Dir(string(to.Location.File))
				if from == toDir {
					continue
				}
				viaScope := false
				for _, ev := range e.Evidence {
					viaScope = viaScope || ev.Kind == resolver.EvidenceMemberScope
				}
				if !viaScope || to.Kind != symbol.KindOutput || !strings.HasPrefix(to.Qualified, strings.TrimPrefix(toDir+"/", "./")) {
					t.Errorf("%s (%s) -> %s (%s) crosses a module boundary without a module binding", s.Qualified, from, to.Qualified, toDir)
				}
			}
		}
	}
}

func TestGraph_SameAddressInDifferentModules(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	net := onlySymbol(t, idx, "modules/network/aws_vpc.main")
	db := onlySymbol(t, idx, "modules/database/aws_vpc.main")
	callers := func(s symbol.Symbol) []string {
		var out []string
		for _, e := range idx.GetCallers(s.ID) {
			c, _ := idx.GetSymbol(e.To)
			out = append(out, c.Qualified)
		}
		sort.Strings(out)
		return out
	}
	equalLines(t, "network vpc callers", callers(net), []string{
		"modules/network/aws_subnet.private", "modules/network/output.vpc_id",
	})
	equalLines(t, "database vpc callers", callers(db), []string{"modules/database/aws_db_instance.db"})
	for _, s := range []symbol.Symbol{net, db} {
		if in, _ := idx.Unattributed(s.ID); in != 0 {
			t.Errorf("%s: unattributed %d; a reference of another module is never attributed to it", s.Qualified, in)
		}
	}
}

func TestCompleteness_Fixture(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	type want struct {
		in, out, unresolved, outside int
		reasons                      []string
	}
	cases := map[string]want{
		// aws_subnet.private exists only in modules/network.
		"modules/database/aws_db_instance.db": {unresolved: 1, reasons: []string{"aws_subnet.private:unresolved"}},
		"output.missing":                      {unresolved: 1, reasons: []string{"module.network->no_such_output:unresolved"}},
		"output.remote_vpc":                   {outside: 1, reasons: []string{"module.vpc_remote->vpc_id:outside_repository"}},
		// Two modules declare aws_vpc.main; the root does not.
		"output.undeclared": {unresolved: 1, reasons: []string{"aws_vpc.main:unresolved"}},
		// Declared twice in dup/: ambiguity, no edge.
		"dup/output.x": {out: 1},
		// override.tf references it from no declaration (sourceless).
		"var.region":         {in: 1},
		"aws_instance.web":   {},
		"module.network":     {},
		"output.network_id":  {},
		"local.volumes":      {},
		"output.endpoint":    {},
		"var.instance_count": {},
	}
	for q, w := range cases {
		s := onlySymbol(t, idx, q)
		in, out := idx.Unattributed(s.ID)
		un := idx.UnresolvedOutgoing(s.ID)
		var reasons []string
		for _, r := range un.References {
			name := r.Name
			if r.ReceiverExpr != "" {
				name = r.ReceiverExpr + "->" + name
			}
			reasons = append(reasons, name+":"+string(r.Reason))
		}
		if in != w.in || out != w.out || un.Unresolved != w.unresolved || un.OutsideRepository != w.outside || !slices.Equal(reasons, w.reasons) {
			t.Errorf("%s: in=%d out=%d unresolved=%d outside=%d %v; want %+v", q, in, out, un.Unresolved, un.OutsideRepository, reasons, w)
		}
	}
	for _, s := range idx.FindSymbolsByQualified("dup/null_resource.x") {
		if in, _ := idx.Unattributed(s.ID); in != 1 {
			t.Errorf("%s: unattributed in %d, want 1 (candidate of the ambiguous reference)", s.Location.File, in)
		}
		if len(idx.GetCallers(s.ID)) != 0 {
			t.Errorf("%s: an ambiguous reference formed an edge", s.Location.File)
		}
	}
}

// TestCompleteness_EveryReferenceIsAccounted checks the partition: every
// edge-kind reference inside a declaration is an edge, unattributed,
// unresolved or outside — none disappears.
func TestCompleteness_EveryReferenceIsAccounted(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	for _, s := range idx.FindSymbols("") {
		var refs int
		for _, r := range idx.ReferencesByContainer(s.ID) {
			if r.Kind == "value_reference" || r.Kind == "depends_on" {
				refs++
			}
		}
		_, out := idx.Unattributed(s.ID)
		un := idx.UnresolvedOutgoing(s.ID)
		resolved := resolvedReferenceCount(idx, s)
		if got := resolved + out + un.Total; got != refs {
			t.Errorf("%s: %d references, accounted %d (edges %d + unattributed %d + no-candidate %d)", s.Qualified, refs, got, resolved, out, un.Total)
		}
	}
}

// resolvedReferenceCount counts the references inside s that resolve to a
// unique target (an edge each; several references may share one edge).
func resolvedReferenceCount(idx *index.RepositoryIndex, s symbol.Symbol) int {
	var n int
	for _, r := range idx.ReferencesByContainer(s.ID) {
		if r.Kind != "value_reference" && r.Kind != "depends_on" {
			continue
		}
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			if (r.NameQualified != "" && to.Qualified == r.NameQualified) ||
				(r.ReceiverTypeQualified != "" && to.Kind == symbol.KindOutput && strings.HasSuffix(to.Qualified, "output."+r.Name) && edgeVia(e, resolver.EvidenceMemberScope)) {
				n++
				break
			}
		}
	}
	return n
}

func edgeVia(e index.GraphEdge, k resolver.EvidenceKind) bool {
	for _, ev := range e.Evidence {
		if ev.Kind == k {
			return true
		}
	}
	return false
}

// TestIsolation_OtherLanguagesAreUnaffected indexes a Python file next to
// Terraform declarations whose addresses end in the Python names: the Python
// results must be byte-identical to an index without the Terraform files, and
// no Terraform reference may reach a Python symbol.
func TestIsolation_OtherLanguagesAreUnaffected(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.py", "def run():\n    main()\n    region()\n    ubuntu()\n\ndef web():\n    pass\n")
	write(t, dir, "main.tf", `
resource "aws_vpc" "main" {}
data "aws_ami" "ubuntu" {}
variable "region" {}
output "o" { value = aws_instance.web.id }
`)
	py := python.NewProvider()
	without := buildIndex(t, dir, py)
	with := buildIndex(t, dir, py, NewProvider())

	pyOnly := func(idx *index.RepositoryIndex) string {
		var b strings.Builder
		for _, s := range idx.FindSymbols("") {
			if s.Language != "python" {
				continue
			}
			in, out := idx.Unattributed(s.ID)
			un := idx.UnresolvedOutgoing(s.ID)
			fmt.Fprintf(&b, "%s in=%d out=%d unresolved=%d edges=%v callers=%d\n", s.Qualified, in, out, un.Unresolved, idx.GetCallees(s.ID), len(idx.GetCallers(s.ID)))
		}
		return b.String()
	}
	if a, b := pyOnly(without), pyOnly(with); a != b {
		t.Errorf("Python results changed by Terraform files:\nwithout:\n%s\nwith:\n%s", a, b)
	}
	for _, s := range with.FindSymbols("") {
		for _, e := range with.GetCallees(s.ID) {
			to, _ := with.GetSymbol(e.To)
			if to.Language != s.Language {
				t.Errorf("cross-language edge %s (%s) -> %s (%s)", s.Qualified, s.Language, to.Qualified, to.Language)
			}
		}
	}
	o := onlySymbol(t, with, "output.o")
	if un := with.UnresolvedOutgoing(o.ID); un.Unresolved != 1 {
		t.Errorf("aws_instance.web must stay unresolved next to Python's web(): %+v", un)
	}
}

// TestJSONSyntaxSiblingIsNotIndexed: a declaration in a .tf.json file of the
// same module is not extracted, so a reference to it is Unresolved — never
// outside the repository, never another module's declaration.
func TestJSONSyntaxSiblingIsNotIndexed(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "main.tf", `output "o" { value = aws_vpc.json_declared.id }`)
	write(t, dir, "vpc.tf.json", `{"resource": {"aws_vpc": {"json_declared": {}}}}`)
	write(t, dir, "other/main.tf", `resource "aws_vpc" "json_declared" {}`)
	idx := buildIndex(t, dir)
	o := onlySymbol(t, idx, "output.o")
	un := idx.UnresolvedOutgoing(o.ID)
	if un.Unresolved != 1 || un.OutsideRepository != 0 || len(idx.GetCallees(o.ID)) != 0 {
		t.Errorf("unresolved=%d outside=%d edges=%v", un.Unresolved, un.OutsideRepository, idx.GetCallees(o.ID))
	}
	if in, _ := idx.Unattributed(onlySymbol(t, idx, "other/aws_vpc.json_declared").ID); in != 0 {
		t.Errorf("another module's declaration was attributed the reference (%d)", in)
	}
}

func TestLocalModuleOutput_Variants(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "envs/prod/main.tf", `
module "vpc" {
  source = "../../modules/vpc"
}
module "vpcs" {
  source   = "../../modules/vpc"
  for_each = toset(["a", "b"])
}
module "gone" {
  source = "../../modules/missing"
}
module "outside" {
  source = "../../../elsewhere"
}
locals {
  a = module.vpc.id
  b = module.vpcs["a"].id
  c = [for m in module.vpcs : m.id]
  d = module.gone.id
  e = module.outside.id
  f = module.vpc.undeclared
  g = module.nope.id
}
`)
	write(t, dir, "modules/vpc/outputs.tf", `output "id" { value = 1 }`)
	write(t, dir, "modules/other/outputs.tf", `output "id" { value = 2 }`)
	idx := buildIndex(t, dir)
	target := "modules/vpc/output.id"
	for local, want := range map[string]string{
		"a": target, "b": target, "c": "", "d": "unresolved", "e": "outside_repository", "f": "unresolved", "g": "unresolved",
	} {
		s := onlySymbol(t, idx, "envs/prod/local."+local)
		var to []string
		for _, e := range idx.GetCallees(s.ID) {
			sym, _ := idx.GetSymbol(e.To)
			if sym.Kind == symbol.KindOutput {
				to = append(to, sym.Qualified)
			}
		}
		un := idx.UnresolvedOutgoing(s.ID)
		var reasons []string
		for _, r := range un.References {
			if r.ReceiverExpr != "" {
				reasons = append(reasons, string(r.Reason))
			}
		}
		switch want {
		case target:
			if !slices.Equal(to, []string{target}) {
				t.Errorf("local.%s: output edges %v, want %s", local, to, target)
			}
		case "":
			if len(to) != 0 || len(reasons) != 0 {
				t.Errorf("local.%s: %v %v", local, to, reasons)
			}
		default:
			if len(to) != 0 || !slices.Equal(reasons, []string{want}) {
				t.Errorf("local.%s: edges %v reasons %v, want %s", local, to, reasons, want)
			}
		}
	}
	if in, _ := idx.Unattributed(onlySymbol(t, idx, "modules/other/output.id").ID); in != 0 {
		t.Error("an output of an unrelated module was attributed a module-output reference")
	}
}

func TestDeterminism_RepeatedBuilds(t *testing.T) {
	snapshot := func() string {
		idx := buildIndex(t, fixtureRepo)
		return golden.IndexSnapshot(idx) + golden.CompletenessSnapshot(idx)
	}
	want := snapshot()
	runs := 20
	if testing.Short() {
		runs = 5
	}
	for i := 0; i < runs; i++ {
		if got := snapshot(); got != want {
			t.Fatalf("run %d: non-deterministic index", i)
		}
	}
}

func TestCache_RoundTripAndInvalidation(t *testing.T) {
	dir := t.TempDir()
	copyDir(t, fixtureRepo, dir)
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	providers := []language.Provider{NewProvider()}
	snap := func(idx *index.RepositoryIndex) string {
		return golden.IndexSnapshot(idx) + golden.CompletenessSnapshot(idx)
	}
	fresh := snap(buildIndex(t, dir))
	for i := 0; i < 2; i++ { // cold, then warm
		idx, err := index.NewWithCache(context.Background(), dir, providers, store)
		if err != nil {
			t.Fatal(err)
		}
		if got := snap(idx); got != fresh {
			t.Fatalf("run %d: cached index differs from a fresh one:\n%s\nvs\n%s", i, got, fresh)
		}
	}
	// A content change is a cache miss for that file.
	write(t, dir, "outputs.tf", `output "network_id" { value = module.database.endpoint }`)
	idx, err := index.NewWithCache(context.Background(), dir, providers, store)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := snap(idx), snap(buildIndex(t, dir)); got != want {
		t.Fatalf("stale cache entry used after a content change")
	}
	if !slices.Contains(edgeLines(idx), "output.network_id -references-> modules/database/output.endpoint exact") {
		t.Errorf("edited reference not indexed: %v", edgeLines(idx))
	}
}

func TestIndex_ConcurrentBuildsAndReads(t *testing.T) {
	want := edgeLines(buildIndex(t, fixtureRepo))
	idx := buildIndex(t, fixtureRepo)
	var wg sync.WaitGroup
	errs := make(chan string, 16)
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if got := edgeLines(buildIndex(t, fixtureRepo)); !slices.Equal(got, want) {
				errs <- "concurrent build differs"
			}
		}()
		go func() {
			defer wg.Done()
			if got := edgeLines(idx); !slices.Equal(got, want) {
				errs <- "concurrent read differs"
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

func TestIndex_AccessorsReturnCopies(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	web := onlySymbol(t, idx, "aws_instance.web")
	before := edgeLines(idx)
	edges := idx.GetCallees(web.ID)
	for i := range edges {
		edges[i].To = ""
	}
	_ = append(edges[:0], index.GraphEdge{})
	if after := edgeLines(idx); !slices.Equal(before, after) {
		t.Error("mutating a returned slice changed the index")
	}
}

func TestContext_FollowsDependencyChain(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	out := onlySymbol(t, idx, "modules/network/output.subnet_id")
	res, err := arkcontext.New(idx, fixtureRepo).Build(context.Background(), arkcontext.Request{Target: out.ID, MaxTokens: 8000, MaxDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range res.Items {
		got = append(got, it.Symbol.Qualified+" "+it.Reason)
	}
	sort.Strings(got)
	equalLines(t, "context", got, []string{
		// output.subnet_id → local.subnet_id → aws_subnet.private; and its
		// consumers through module.network.
		"aws_instance.web caller",
		"module.database caller",
		"modules/network/aws_subnet.private transitive callee",
		"modules/network/local.subnet_id references",
		"modules/network/output.subnet_id target",
	})
	if res.Stats.UnattributedCallers != 0 || res.Stats.UnattributedCallees != 0 || res.Stats.UnresolvedCallees != 0 {
		t.Errorf("stats %+v", res.Stats)
	}
}

func impactLines(t *testing.T, idx *index.RepositoryIndex, qualified string, depth int) []string {
	t.Helper()
	res, err := impact.Analyze(context.Background(), idx, graph.New(idx), onlySymbol(t, idx, qualified).ID, depth)
	if err != nil || res == nil {
		t.Fatalf("Analyze: %v", err)
	}
	var got []string
	for _, e := range res.Entries {
		got = append(got, fmt.Sprintf("%s %s %d", e.Symbol.Qualified, e.Category, e.Distance))
	}
	sort.Strings(got)
	return got
}

// TestImpact_ThroughModuleOutputs follows dependents upstream through
// references and module outputs, more than one hop, and never into another
// module's same-name resource.
func TestImpact_ThroughModuleOutputs(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "impact of modules/network/aws_subnet.private (depth 4)", impactLines(t, idx, "modules/network/aws_subnet.private", 4), []string{
		"aws_instance.web transitive_dependent 3", // via module.network.subnet_id
		"local.web_ids transitive_dependent 4",    // aws_instance.web[*].id
		"module.database transitive_dependent 3",  // via module.network.subnet_id
		"modules/network/aws_vpc.main direct_dependency 1",
		"modules/network/local.subnet_id direct_dependent 1",
		"modules/network/output.subnet_id transitive_dependent 2",
		"output.endpoint transitive_dependent 4", // references module.database
	})
	equalLines(t, "impact of modules/network/aws_vpc.main (default depth)", impactLines(t, idx, "modules/network/aws_vpc.main", 0), []string{
		"modules/network/aws_subnet.private direct_dependent 1",
		"modules/network/local.subnet_id transitive_dependent 2",
		"modules/network/output.subnet_id transitive_dependent 3",
		"modules/network/output.vpc_id direct_dependent 1",
		"modules/network/var.cidr direct_dependency 1",
		"output.network_id transitive_dependent 2",
	})
	// depends_on is followed upstream as well.
	equalLines(t, "impact of aws_security_group.web", impactLines(t, idx, "aws_security_group.web", 3), []string{
		"aws_instance.web direct_dependent 1",
		"local.web_ids transitive_dependent 2",
		"var.region direct_dependency 1",
	})
	// Every Terraform edge is Exact, so every path is.
	res, _ := impact.Analyze(context.Background(), idx, graph.New(idx), onlySymbol(t, idx, "modules/network/aws_subnet.private").ID, 4)
	for _, e := range res.Entries {
		if e.Confidence != resolver.ConfidenceExact {
			t.Errorf("%s: path confidence %s", e.Symbol.Qualified, e.Confidence)
		}
	}
	for _, l := range impactLines(t, idx, "modules/network/aws_vpc.main", 10) {
		if strings.HasPrefix(l, "modules/database/") {
			t.Errorf("impact crossed into another module's same-name resource: %s", l)
		}
	}
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		write(t, to, filepath.ToSlash(rel), string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
