package terraform

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/symbol"
)

// Terraform's local module sources start with ./ ../ .\ or ..\ ; a backslash
// is normalized to a slash before path cleaning (moduleaddrs.
// parseModuleSourceLocal). Such a source is in the repository, never outside.
func TestModuleSource_WindowsStyleLocalPaths(t *testing.T) {
	ex := extract(t, "envs/prod/main.tf", `
module "a" { source = ".\\modules\\net" }
module "b" { source = "..\\..\\shared\\net" }
module "c" { source = "./mods1" }
module "d" { source = "./x\\..\\y" }
`)
	equalLines(t, "symbols", symbolLines(ex), []string{
		"module module.a envs/prod/module.a scope=envs/prod/modules/net/output.",
		"module module.b envs/prod/module.b scope=shared/net/output.",
		"module module.c envs/prod/module.c scope=envs/prod/mods1/output.",
		"module module.d envs/prod/module.d scope=envs/prod/y/output.",
	})
}

func TestGraph_WindowsStyleLocalSourceResolves(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "root/main.tf", "module \"net\" { source = \"..\\\\modules\\\\net\" }\noutput \"id\" { value = module.net.vpc_id }\n")
	write(t, dir, "modules/net/outputs.tf", `output "vpc_id" { value = 1 }`)
	idx := buildIndex(t, dir)
	if !slices.Contains(edgeLines(idx), "root/output.id -references-> modules/net/output.vpc_id exact") {
		t.Errorf("edges %v", edgeLines(idx))
	}
	if un := idx.UnresolvedOutgoing(onlySymbol(t, idx, "root/output.id").ID); un.OutsideRepository != 0 {
		t.Errorf("a local source was reported outside the repository: %+v", un)
	}
}

// An override file may replace a module call's source (Terraform merges
// override blocks: configs.ModuleCall.merge). Which source is effective is
// decided by merge order, so the original source alone proves nothing: the
// module's outputs — and the call itself — are ambiguous, never Exact.
func TestGraph_OverrideReplacingModuleSourceIsNeverExact(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "root/main.tf", `
module "net" { source = "../modules/a" }
module "plain" { source = "../modules/a" }
output "id"    { value = module.net.vpc_id }
output "call"  { value = module.net }
output "plain" { value = module.plain.vpc_id }
`)
	write(t, dir, "root/net_override.tf", `
module "net" { source = "../modules/b" }
module "plain" { count = 2 }
`)
	write(t, dir, "modules/a/outputs.tf", `output "vpc_id" { value = 1 }`)
	write(t, dir, "modules/b/outputs.tf", `output "vpc_id" { value = 2 }`)
	idx := buildIndex(t, dir)
	edges := edgeLines(idx)
	for _, e := range edges {
		if e == "root/output.id -references-> modules/a/output.vpc_id exact" ||
			e == "root/output.id -references-> modules/b/output.vpc_id exact" ||
			e == "root/output.call -references-> root/module.net exact" {
			t.Errorf("false Exact through an overridden source: %s", e)
		}
	}
	for _, out := range []string{"root/output.id", "root/output.call"} {
		if _, o := idx.Unattributed(onlySymbol(t, idx, out).ID); o == 0 {
			t.Errorf("%s: the ambiguity is not reported", out)
		}
	}
	// An override that leaves the source alone changes no identity.
	if !slices.Contains(edges, "root/output.plain -references-> modules/a/output.vpc_id exact") {
		t.Errorf("an override without source broke resolution: %v", edges)
	}
}

// In a configuration file `output`, `check` and `provider` are no reference
// roots: Terraform reads `output.x` as a resource of type "output"
// (addrs.ParseRef). It must never resolve to an output, check or provider
// block — and a resource of such a type resolves to itself.
func TestGraph_ReservedBlockNamesAreResourceTypesInExpressions(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "main.tf", `
output "x" { value = 1 }
check "c" {
  assert {
    condition     = true
    error_message = "x"
  }
}
provider "aws" {}
resource "output" "real" {}
resource "var" "odd" {}
variable "odd" {}
locals {
  a = output.x.value
  b = check.c.status
  c = provider.aws.region
  d = output.real.id
  e = resource.output.real.id
  f = resource.var.odd.id
  g = var.odd
}
`)
	idx := buildIndex(t, dir)
	callees := func(local string) []string {
		var out []string
		for _, e := range idx.GetCallees(onlySymbol(t, idx, "local."+local).ID) {
			to, _ := idx.GetSymbol(e.To)
			out = append(out, to.Qualified+":"+string(to.Kind))
		}
		return out
	}
	for local, want := range map[string][]string{
		"a": nil, "b": nil, "c": nil,
		"d": {"resource.output.real:resource"},
		"e": {"resource.output.real:resource"},
		"f": {"resource.var.odd:resource"},
		"g": {"var.odd:variable"},
	} {
		if got := callees(local); !slices.Equal(got, want) {
			t.Errorf("local.%s -> %v, want %v", local, got, want)
		}
	}
	for _, q := range []string{"output.x", "check.c", "provider.aws"} {
		s := onlySymbol(t, idx, q)
		if len(idx.GetCallers(s.ID)) != 0 {
			t.Errorf("%s got a caller", q)
		}
		if in, _ := idx.Unattributed(s.ID); in != 0 {
			t.Errorf("%s was attributed a resource reference (%d)", q, in)
		}
	}
	if s := onlySymbol(t, idx, "resource.var.odd"); s.Kind != symbol.KindResource || s.Name != "resource.var.odd" {
		t.Errorf("resource of a reserved type: %+v", s)
	}
}

// Reserved roots Terraform rejects (plan, state, template, lazy, arg) and
// the contextual `caller` denote no declaration; action and list resources
// are addressed by type and name.
func TestExtract_OtherReferenceRoots(t *testing.T) {
	ex := extract(t, "main.tf", `
locals {
  a = plan.x.y
  b = state.x.y
  c = template.x.y
  d = lazy.x.y
  e = arg.x.y
  f = caller.x
  g = action.aws_lambda_invoke.notify
  h = list.aws_instance.all
}
`)
	equalLines(t, "references", refNames(ex), []string{"action.aws_lambda_invoke.notify", "list.aws_instance.all"})
}

// Two roots calling one shared module, which calls a nested module: every
// root keeps its own variables, locals and outputs; both reach the shared
// module's outputs; no edge connects the roots; changing the nested module
// reaches both roots transitively.
func TestGraph_MultipleRootsSharedModule(t *testing.T) {
	dir := t.TempDir()
	for _, env := range []string{"production", "staging"} {
		write(t, dir, "environments/"+env+"/main.tf", `
variable "cidr" {}
locals { name = "`+env+`" }
module "network" {
  source = "../../modules/network"
  cidr   = var.cidr
  name   = local.name
}
resource "aws_route53_record" "main" { records = [module.network.vpc_id] }
output "vpc_id" { value = module.network.vpc_id }
output "subnet" { value = module.network.subnet_id }
`)
	}
	write(t, dir, "modules/network/main.tf", `
variable "cidr" {}
variable "name" {}
resource "aws_vpc" "main" { cidr_block = var.cidr }
module "subnets" {
  source = "./subnets"
  vpc_id = aws_vpc.main.id
}
`)
	write(t, dir, "modules/network/outputs.tf", `
output "vpc_id"    { value = aws_vpc.main.id }
output "subnet_id" { value = module.subnets.id }
`)
	write(t, dir, "modules/network/subnets/main.tf", `
variable "vpc_id" {}
resource "aws_subnet" "main" { vpc_id = var.vpc_id }
output "id" { value = aws_subnet.main.id }
`)
	idx := buildIndex(t, dir)
	edges := edgeLines(idx)
	for _, env := range []string{"production", "staging"} {
		p := "environments/" + env + "/"
		for _, want := range []string{
			p + "module.network -references-> " + p + "var.cidr exact",
			p + "module.network -references-> " + p + "local.name exact",
			p + "output.vpc_id -references-> modules/network/output.vpc_id exact",
			p + "output.subnet -references-> modules/network/output.subnet_id exact",
			p + "aws_route53_record.main -references-> modules/network/output.vpc_id exact",
		} {
			if !slices.Contains(edges, want) {
				t.Errorf("missing %s", want)
			}
		}
	}
	for _, e := range edges {
		if strings.Contains(e, "production/") && strings.Contains(e, "staging/") {
			t.Errorf("edge between roots: %s", e)
		}
	}
	if !slices.Contains(edges, "modules/network/output.subnet_id -references-> modules/network/subnets/output.id exact") {
		t.Errorf("nested module output missing: %v", edges)
	}
	// Changing the nested module's resource reaches both roots.
	got := impactLines(t, idx, "modules/network/subnets/aws_subnet.main", 6)
	for _, want := range []string{
		"modules/network/subnets/output.id direct_dependent 1",
		"modules/network/output.subnet_id transitive_dependent 2",
		"environments/production/output.subnet transitive_dependent 3",
		"environments/staging/output.subnet transitive_dependent 3",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("impact misses %q: %v", want, got)
		}
	}
	for _, l := range got {
		if strings.Contains(l, "aws_route53_record") || strings.Contains(l, "output.vpc_id") {
			t.Errorf("impact reached an unrelated dependent: %s", l)
		}
	}
}

func TestUnescapeHCL(t *testing.T) {
	for in, want := range map[string]string{
		`plain`: "plain", `a\\b`: `a\b`, `a\"b`: `a"b`, `\n\t\r`: "\n\t\r",
		`A\U0001F600`: "A\U0001F600", `$${x}`: "${x}", `%%{y}`: "%{y}", `100%`: "100%", `$5`: "$5",
	} {
		if got, ok := unescapeHCL(in); !ok || got != want {
			t.Errorf("unescapeHCL(%q) = %q, %t; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{`\`, `\q`, `\u12`, `\uZZZZ`, `\UFFFFFFFF`, `\uD800`} {
		if got, ok := unescapeHCL(bad); ok {
			t.Errorf("unescapeHCL(%q) = %q, want failure", bad, got)
		}
	}
}

// Symlinks: the index walks real directories only (a symlinked directory is
// not descended), and Terraform resolves a local module through symlinks
// after cleaning the path lexically (initwd: filepath.Join, then
// EvalSymlinks). A module reached only through a symlinked directory is
// therefore Unresolved — never resolved to another directory, never outside —
// and loops cannot hang the walk.
func TestGraph_SymlinkedModules(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	write(t, dir, "root/main.tf", `
module "linked"  { source = "./modules/linked" }
module "real"    { source = "./real" }
module "outer"   { source = "./modules/outer" }
module "through" { source = "./modules/linked/../real" }
output "a" { value = module.linked.id }
output "b" { value = module.real.id }
output "c" { value = module.outer.id }
output "d" { value = module.through.id }
`)
	write(t, dir, "root/real/outputs.tf", `output "id" { value = 1 }`)
	write(t, outside, "ext/outputs.tf", `output "id" { value = 2 }`)
	write(t, dir, "root/modules/.keep", "")
	link := func(target, name string) {
		if err := os.Symlink(target, filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	link("../real", "root/modules/linked")
	link(filepath.Join(outside, "ext"), "root/modules/outer")
	link(".", "root/real/loop")
	write(t, dir, "shared/vars.tf", `variable "region" {}`)
	link("../../shared/vars.tf", "root/real/vars.tf") // a symlinked file is part of its directory's module

	idx := buildIndex(t, dir)
	edges := edgeLines(idx)
	// ./modules/linked/../real is ./modules/real lexically — not ./real.
	for out, want := range map[string]string{
		"a": "unresolved", "b": "root/real/output.id", "c": "unresolved", "d": "unresolved",
	} {
		s := onlySymbol(t, idx, "root/output."+out)
		var to []string
		for _, e := range idx.GetCallees(s.ID) {
			sym, _ := idx.GetSymbol(e.To)
			if sym.Kind == symbol.KindOutput {
				to = append(to, sym.Qualified)
			}
		}
		un := idx.UnresolvedOutgoing(s.ID)
		switch want {
		case "unresolved":
			if len(to) != 0 || un.Unresolved != 1 || un.OutsideRepository != 0 {
				t.Errorf("output.%s: edges %v unresolved %d outside %d", out, to, un.Unresolved, un.OutsideRepository)
			}
		default:
			if !slices.Equal(to, []string{want}) {
				t.Errorf("output.%s: %v, want %s", out, to, want)
			}
		}
	}
	if len(idx.FindSymbolsByQualified("root/real/var.region")) != 1 || len(idx.FindSymbolsByQualified("shared/var.region")) != 1 {
		t.Errorf("a symlinked file belongs to the directory it is linked into (and its own): %v", edges)
	}
}

// Malformed input: no panic, no symbol or reference invented from the broken
// part, an error diagnostic, and the other files of the module unaffected.
func TestGraph_OneBrokenFileInAModule(t *testing.T) {
	cases := map[string]string{
		"unclosed_brace":   "resource \"aws_vpc\" \"broken\" {\n  cidr = var.cidr\n",
		"unclosed_bracket": "locals {\n  l = [var.cidr,\n}\n",
		"unclosed_string":  "output \"o\" {\n  value = \"abc ${var.cidr}\n}\n",
		"partial_module":   "module \"half\" {\n  source = \"./modules/",
		"truncated":        "resource \"aws_subnet\" \"s\" {\n  vpc_id = aws_vpc.ma",
		"invalid_utf8":     "resource \"aws_vpc\" \"\xff\xfe\" {}\nlocals { x = var.\xff }\n",
	}
	for name, broken := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "main.tf", "variable \"cidr\" {}\nresource \"aws_vpc\" \"main\" { cidr_block = var.cidr }\n")
			write(t, dir, "outputs.tf", "output \"vpc\" { value = aws_vpc.main.id }\n")
			write(t, dir, "broken.tf", broken)
			idx := buildIndex(t, dir)
			edges := edgeLines(idx)
			for _, want := range []string{
				"aws_vpc.main -references-> var.cidr exact",
				"output.vpc -references-> aws_vpc.main exact",
			} {
				if !slices.Contains(edges, want) {
					t.Errorf("valid files lost %q: %v", want, edges)
				}
			}
			for _, s := range idx.SymbolsByFile("broken.tf") {
				if !validName(s.Name[strings.LastIndexByte(s.Name, '.')+1:]) {
					t.Errorf("symbol with an invalid name from broken input: %q", s.Name)
				}
			}
			diag := false
			for _, d := range idx.Diagnostics() {
				diag = diag || d.Location.File == "broken.tf"
			}
			if !diag {
				t.Error("no diagnostic for the broken file")
			}
		})
	}
}
