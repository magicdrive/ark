package terraform

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
)

// Module references with literal, dynamic, nested and splat indexes: every
// reference inside an index is observed on its own, the module call is
// observed once per occurrence, and the output resolves only through the
// module call's own source.
func TestExtract_ModuleIndexForms(t *testing.T) {
	ex := extract(t, "main.tf", `
locals {
  a = module.network.out
  b = module.network["key"].out
  c = module.network[key].out
  d = module.network[*].out
  e = module.network[count.index].out
  f = module.network[local.selected].out
  g = module.network[var.selected].out
  h = module.network[each.key].out
  i = module.network[var.x][var.y].out
  j = module.network[module.other.idx].out
  k = module.network.*.out
  l = module.network[local.m[var.k]].out
}
`)
	byLocal := map[string][]string{}
	for _, r := range ex.References {
		name := r.Name
		if r.ReceiverExpr != "" {
			name = r.ReceiverExpr + "->" + r.Name
		}
		byLocal[r.Container] = append(byLocal[r.Container], name)
	}
	want := map[string][]string{
		"local.a": {"module.network", "module.network->out"},
		"local.b": {"module.network", "module.network->out"},
		"local.c": {"module.network", "module.network->out"}, // `key` is a bare name: no address
		"local.d": {"module.network", "module.network->out"},
		"local.e": {"module.network", "module.network->out"},
		"local.f": {"module.network", "module.network->out", "local.selected"},
		"local.g": {"module.network", "module.network->out", "var.selected"},
		"local.h": {"module.network", "module.network->out"},
		// Two indexes are no module instance key: no output is claimed.
		"local.i": {"module.network", "var.x", "var.y"},
		"local.j": {"module.network", "module.network->out", "module.other", "module.other->idx"},
		"local.k": {"module.network", "module.network->out"},
		"local.l": {"module.network", "module.network->out", "local.m", "var.k"},
	}
	for local, w := range want {
		if got := byLocal[local]; !slices.Equal(got, w) {
			t.Errorf("%s: %v, want %v", local, got, w)
		}
	}
	// No source occurrence is observed twice.
	seen := map[string]bool{}
	for _, r := range ex.References {
		k := fmt.Sprintf("%s|%s|%s|%v", r.Name, r.ReceiverExpr, r.Kind, r.Location)
		if seen[k] {
			t.Errorf("duplicate reference %s", k)
		}
		seen[k] = true
	}
}

func TestGraph_DynamicModuleIndexing(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "root/main.tf", `
module "network" {
  source   = "../modules/network"
  for_each = toset(["a", "b"])
}
module "counted" {
  source = "../modules/network"
  count  = 2
}
module "remote" {
  source   = "registry.example.com/org/net/aws"
  for_each = toset(["a"])
}
module "escapes" {
  source = "../../outside"
}
locals {
  selected = "a"
}
output "lit"     { value = module.network["a"].vpc_id }
output "dyn"     { value = module.network[local.selected].vpc_id }
output "var"     { value = module.network[var.selected].vpc_id }
output "splat"   { value = module.network[*].vpc_id }
output "count"   { value = module.counted[0].vpc_id }
output "missing" { value = module.network[local.selected].nope }
output "remote"  { value = module.remote[local.selected].vpc_id }
output "escapes" { value = module.escapes.vpc_id }
variable "selected" {}
`)
	write(t, dir, "root/dup.tf", `
module "dup" { source = "../modules/network" }
`)
	write(t, dir, "root/dup2.tf", `
module "dup" { source = "../modules/other" }
output "dup" { value = module.dup["k"].vpc_id }
`)
	write(t, dir, "root/broken.tf", "output \"broken\" {\n  value = module.network[local.selected\n}\noutput \"after\" { value = module.network[var.selected].vpc_id }\n")
	write(t, dir, "modules/network/outputs.tf", `output "vpc_id" { value = 1 }`)
	write(t, dir, "modules/other/outputs.tf", `output "vpc_id" { value = 2 }`)
	write(t, dir, "elsewhere/outputs.tf", `output "vpc_id" { value = 3 }`)

	idx := buildIndex(t, dir)
	callees := func(out string) []string {
		s := onlySymbol(t, idx, "root/output."+out)
		var got []string
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			got = append(got, to.Qualified)
		}
		un := idx.UnresolvedOutgoing(s.ID)
		for _, r := range un.References {
			got = append(got, string(r.Reason)+":"+r.ReceiverExpr+"->"+r.Name)
		}
		if _, o := idx.Unattributed(s.ID); o > 0 {
			got = append(got, fmt.Sprintf("unattributed:%d", o))
		}
		sort.Strings(got)
		return got
	}
	const vpc = "modules/network/output.vpc_id"
	for out, want := range map[string][]string{
		"lit":     {vpc, "root/module.network"},
		"dyn":     {vpc, "root/local.selected", "root/module.network"},
		"var":     {vpc, "root/module.network", "root/var.selected"},
		"splat":   {vpc, "root/module.network"},
		"count":   {vpc, "root/module.counted"},
		"missing": {"root/local.selected", "root/module.network", "unresolved:module.network->nope"},
		"remote":  {"outside_repository:module.remote->vpc_id", "root/local.selected", "root/module.remote"},
		"escapes": {"outside_repository:module.escapes->vpc_id", "root/module.escapes"},
		// Two declarations of module.dup: both the call and its output are
		// ambiguous — candidates, no edge.
		"dup": {"unattributed:2"},
	} {
		if got := callees(out); !slices.Equal(got, want) {
			t.Errorf("output.%s: %v, want %v", out, got, want)
		}
	}
	// An unclosed index makes Tree-sitter's recovery consume the rest of the
	// file: nothing in it is observed (no guessed reference), and the file
	// reports the syntax error.
	if refs := idx.ReferencesByFile("root/broken.tf"); len(refs) != 0 || len(idx.SymbolsByFile("root/broken.tf")) != 0 {
		t.Errorf("broken.tf: refs %v", refs)
	}
	broken := false
	for _, d := range idx.Diagnostics() {
		broken = broken || d.Location.File == "root/broken.tf"
	}
	if !broken {
		t.Error("broken.tf: no diagnostic")
	}
	for _, q := range []string{"modules/other/output.vpc_id", "elsewhere/output.vpc_id"} {
		s := onlySymbol(t, idx, q)
		if len(idx.GetCallers(s.ID)) != 0 {
			t.Errorf("%s got an edge", q)
		}
	}
	// The module boundary holds and every reference is accounted for.
	for _, s := range idx.FindSymbols("") {
		var refs int
		for _, r := range idx.ReferencesByContainer(s.ID) {
			if r.Kind == "value_reference" || r.Kind == "depends_on" {
				refs++
			}
		}
		_, out := idx.Unattributed(s.ID)
		if got := resolvedReferenceCount(idx, s) + out + idx.UnresolvedOutgoing(s.ID).Total; got != refs {
			t.Errorf("%s: %d references, %d accounted", s.Qualified, refs, got)
		}
	}
	snap := func(i *index.RepositoryIndex) string { return strings.Join(edgeLines(i), "\n") }
	if a, b := snap(idx), snap(buildIndex(t, dir)); a != b {
		t.Error("non-deterministic")
	}
}
