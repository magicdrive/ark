package terraform

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/graph"
	"github.com/magicdrive/ark/internal/impact"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// Module input arguments. `module "m" { cidr = ... }` binds the child
// module's `variable "cidr"`: the call refers to that declaration — the
// interface it binds — exactly as `module.m.out` refers to the child's output.
// The edge goes from the call to the child variable, so changing the variable
// reaches every call that passes it; the value flowing from the argument
// expression into the child is not an edge.

// moduleArgEdges lists the input-binding edges: module call -> child
// variable through the call's parameter scope.
func moduleArgEdges(idx *index.RepositoryIndex) []string {
	var out []string
	for _, s := range idx.FindSymbols("") {
		if s.Kind != symbol.KindModule {
			continue
		}
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			if to.Kind == symbol.KindVariable && e.Kind == index.EdgeReferences && edgeVia(e, resolver.EvidenceMemberScope) {
				out = append(out, s.Qualified+" -> "+to.Qualified+" "+e.Confidence.String())
			}
		}
	}
	slices.Sort(out)
	return out
}

func TestInputBinding_Basic(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "main.tf", `
variable "production_cidr" {}
module "network" {
  source = "./modules/network"
  cidr   = var.production_cidr
  name   = "prod"
}
output "vpc" { value = module.network.vpc_id }
`)
	write(t, dir, "modules/network/variables.tf", `
variable "cidr" { type = string }
variable "name" { default = "x" }
variable "unused" { default = "y" }
`)
	write(t, dir, "modules/network/main.tf", `resource "aws_vpc" "main" { cidr_block = var.cidr }`)
	write(t, dir, "modules/network/outputs.tf", `output "vpc_id" { value = aws_vpc.main.id }`)
	idx := buildIndex(t, dir)
	equalLines(t, "input bindings", moduleArgEdges(idx), []string{
		"module.network -> modules/network/var.cidr exact",
		"module.network -> modules/network/var.name exact",
	})
	// The argument expression's own reference stays a reference of the call.
	if !slices.Contains(edgeLines(idx), "module.network -references-> var.production_cidr exact") {
		t.Errorf("argument expression reference lost: %v", edgeLines(idx))
	}
	// Callers of the child variable: its own uses and the call binding it.
	cidr := onlySymbol(t, idx, "modules/network/var.cidr")
	var callers []string
	for _, e := range idx.GetCallers(cidr.ID) {
		c, _ := idx.GetSymbol(e.To)
		callers = append(callers, c.Qualified+":"+string(e.RefKind))
	}
	slices.Sort(callers)
	equalLines(t, "callers of var.cidr", callers, []string{"module.network:value_reference", "modules/network/aws_vpc.main:value_reference"})
	if in, _ := idx.Unattributed(cidr.ID); in != 0 {
		t.Errorf("unattributed %d", in)
	}
	// Impact of changing the child variable reaches the call and, through
	// it, the call's dependents; an unrelated variable has none of them.
	equalLines(t, "impact of var.cidr", impactLines(t, idx, "modules/network/var.cidr", 3), []string{
		"module.network direct_dependent 1",
		"modules/network/aws_vpc.main direct_dependent 1",
		"modules/network/output.vpc_id transitive_dependent 2",
		"output.vpc transitive_dependent 2",
	})
	equalLines(t, "impact of var.unused", impactLines(t, idx, "modules/network/var.unused", 3), nil)
	res, _ := impact.Analyze(context.Background(), idx, graph.New(idx), cidr.ID, 3)
	equalLines(t, "affected files", fileList(res.AffectedFiles), []string{"main.tf", "modules/network/main.tf", "modules/network/outputs.tf", "modules/network/variables.tf"})
	for _, e := range res.Entries {
		if e.Confidence != resolver.ConfidenceExact {
			t.Errorf("%s: %s", e.Symbol.Qualified, e.Confidence)
		}
	}
	// The argument occurrence is attributed: the call's partition is exact.
	call := onlySymbol(t, idx, "module.network")
	if _, out := idx.Unattributed(call.ID); out != 0 || idx.UnresolvedOutgoing(call.ID).Total != 0 {
		t.Errorf("module.network: unattributed %d, no-candidate %+v", out, idx.UnresolvedOutgoing(call.ID))
	}
}

func fileList[T ~string](fs []T) []string {
	var out []string
	for _, f := range fs {
		out = append(out, string(f))
	}
	slices.Sort(out)
	return out
}

func TestInputBinding_SharedChildAndMultipleCalls(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "environments/production/main.tf", `
variable "prod_only" {}
module "network" {
  source = "../../modules/network"
  cidr   = "10.0.0.0/16"
  extra  = var.prod_only
}
module "network_dr" {
  source = "../../modules/network"
  cidr   = "10.1.0.0/16"
}
`)
	write(t, dir, "environments/staging/main.tf", `
module "network" {
  source = "../../modules/network"
  cidr   = "10.2.0.0/16"
}
`)
	write(t, dir, "modules/network/variables.tf", "variable \"cidr\" {}\nvariable \"extra\" { default = null }\n")
	write(t, dir, "modules/other/variables.tf", "variable \"cidr\" {}\nvariable \"extra\" {}\n")
	idx := buildIndex(t, dir)
	equalLines(t, "input bindings", moduleArgEdges(idx), []string{
		"environments/production/module.network -> modules/network/var.cidr exact",
		"environments/production/module.network -> modules/network/var.extra exact",
		"environments/production/module.network_dr -> modules/network/var.cidr exact",
		"environments/staging/module.network -> modules/network/var.cidr exact",
	})
	for _, l := range edgeLines(idx) {
		if strings.Contains(l, "modules/other/") {
			t.Errorf("a same-name variable of another module was bound: %s", l)
		}
		if strings.HasPrefix(l, "environments/staging/") && strings.Contains(l, "production") {
			t.Errorf("production reference attached to staging: %s", l)
		}
	}
	got := impactLines(t, idx, "modules/network/var.cidr", 3)
	equalLines(t, "impact of the shared variable", got, []string{
		"environments/production/module.network direct_dependent 1",
		"environments/production/module.network_dr direct_dependent 1",
		"environments/staging/module.network direct_dependent 1",
	})
}

// Root -> A -> B: each call binds its own child's variable; changing B's
// variable reaches A's call, the A output built on it, and the root.
func TestInputBinding_NestedModules(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "main.tf", `
module "a" {
  source = "./modules/a"
  x      = "1"
}
output "root" { value = module.a.out }
`)
	write(t, dir, "modules/a/main.tf", `
variable "x" {}
module "b" {
  source = "../b"
  y      = var.x
}
output "out" { value = module.b.out }
`)
	write(t, dir, "modules/b/main.tf", `
variable "y" {}
output "out" { value = var.y }
module "loop" {
  source = "../a"
  x      = var.y
}
`)
	idx := buildIndex(t, dir)
	equalLines(t, "input bindings", moduleArgEdges(idx), []string{
		"module.a -> modules/a/var.x exact",
		"modules/a/module.b -> modules/b/var.y exact",
		"modules/b/module.loop -> modules/a/var.x exact",
	})
	got := impactLines(t, idx, "modules/b/var.y", 6)
	for _, want := range []string{
		"modules/a/module.b direct_dependent 1",
		"modules/b/output.out direct_dependent 1",
		"modules/b/module.loop direct_dependent 1",
		"modules/a/output.out transitive_dependent 2",
		"output.root transitive_dependent 3",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("impact misses %q: %v", want, got)
		}
	}
}

func TestInputBinding_NegativeCases(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	write(t, dir, "root/main.tf", `
variable "src" {}
module "missing_var" {
  source  = "../modules/net"
  nope    = 1
}
module "remote" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.0.0"
  cidr    = "10.0.0.0/16"
}
module "dynamic" {
  source = "../modules/${var.src}"
  cidr   = 1
}
module "overridden" {
  source = "../modules/net"
  cidr   = 1
}
module "linked" {
  source = "./linked"
  cidr   = 1
}
module "meta" {
  source     = "../modules/net"
  count      = 2
  for_each   = {}
  providers  = { aws = aws }
  depends_on = []
}
module "escapes" {
  source = "../../elsewhere"
  cidr   = 1
}
`)
	write(t, dir, "root/dup.tf", "module \"dup\" { source = \"../modules/net\" }\n")
	write(t, dir, "root/dup2.tf", "module \"dup\" {\n  source = \"../modules/net\"\n  cidr = 1\n}\n")
	write(t, dir, "root/overridden_override.tf", "module \"overridden\" { source = \"../modules/other\" }\n")
	write(t, dir, "root/broken.tf", "module \"broken\" {\n  source = \"../modules/net\"\n  cidr = [\n")
	write(t, dir, "modules/net/variables.tf", "variable \"cidr\" {}\nvariable \"count\" {}\nvariable \"for_each\" {}\nvariable \"providers\" {}\nvariable \"depends_on\" {}\nvariable \"source\" {}\nvariable \"version\" {}\n")
	write(t, dir, "modules/other/variables.tf", `variable "cidr" {}`)
	write(t, outside, "net/variables.tf", `variable "cidr" {}`)
	if err := os.Symlink(filepath.Join(dir, "modules", "net"), filepath.Join(dir, "root", "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	idx := buildIndex(t, dir)
	if got := moduleArgEdges(idx); len(got) != 0 {
		t.Errorf("no input binding is provable here, got %v", got)
	}
	reason := func(call, arg string) string {
		for _, s := range idx.FindSymbolsByQualified("root/module." + call) {
			for _, r := range idx.UnresolvedOutgoing(s.ID).References {
				if r.Name == arg && r.ReceiverExpr == "module."+call {
					return string(r.Reason)
				}
			}
			if _, o := idx.Unattributed(s.ID); o > 0 {
				return "unattributed"
			}
		}
		return "none"
	}
	for _, c := range []struct{ call, arg, want string }{
		{"missing_var", "nope", "unresolved"},
		{"remote", "cidr", "outside_repository"},
		{"dynamic", "cidr", "unresolved"},
		{"linked", "cidr", "unresolved"},
		{"escapes", "cidr", "outside_repository"},
		{"overridden", "cidr", "unattributed"}, // two sources: candidates
		{"meta", "count", "none"},              // meta-arguments bind no variable
	} {
		if got := reason(c.call, c.arg); got != c.want {
			t.Errorf("module.%s %s: %s, want %s", c.call, c.arg, got, c.want)
		}
	}
	for _, s := range idx.FindSymbolsByQualified("root/module.dup") {
		if len(idx.GetCallees(s.ID)) != 0 {
			t.Errorf("duplicate module call bound an input: %s", s.Location.File)
		}
	}
	for _, q := range []string{"modules/net/var.count", "modules/net/var.for_each", "modules/net/var.providers", "modules/net/var.depends_on", "modules/net/var.source", "modules/net/var.version"} {
		s := onlySymbol(t, idx, q)
		if len(idx.GetCallers(s.ID)) != 0 {
			t.Errorf("%s bound by a meta-argument", q)
		}
		if in, _ := idx.Unattributed(s.ID); in != 0 {
			t.Errorf("%s attributed a meta-argument (%d)", q, in)
		}
	}
	if refs := idx.ReferencesByFile("root/broken.tf"); len(refs) != 0 {
		t.Errorf("broken.tf: %v", refs)
	}
}

func TestInputBinding_WindowsSourceAndUnicode(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "環境/main.tf", "module \"ネット\" {\n  source = \"..\\\\モジュール\\\\ネット\"\n  名前   = \"x\"\n}\n")
	write(t, dir, "モジュール/ネット/variables.tf", "variable \"名前\" {}\nvariable \"名前2\" {}\n")
	idx := buildIndex(t, dir)
	equalLines(t, "input bindings", moduleArgEdges(idx), []string{"環境/module.ネット -> モジュール/ネット/var.名前 exact"})
}

// The argument reference is extracted once per argument, with the call as
// receiver, and no argument reference is emitted outside a module block.
func TestExtract_ModuleArguments(t *testing.T) {
	ex := extract(t, "main.tf", `
module "m" {
  source   = "./c"
  version  = "1"
  count    = 1
  a        = 1
  b        = { x = var.y }
}
resource "r" "s" { a = 1 }
`)
	var got []string
	for _, r := range ex.References {
		if r.NamedArgument {
			got = append(got, r.ReceiverExpr+"->"+r.Name+"@"+r.ReceiverTypeQualified)
		}
	}
	equalLines(t, "arguments", got, []string{"module.m->a@module.m", "module.m->b@module.m"})
	if ex.Symbols[0].ParameterScope != "c/var." || ex.Symbols[0].MemberScope != "c/output." {
		t.Errorf("scopes %+v", ex.Symbols[0])
	}
}
