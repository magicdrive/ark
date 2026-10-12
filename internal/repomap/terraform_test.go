package repomap_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/languages/terraform"
	"github.com/magicdrive/ark/internal/repomap"
)

// terraformFixture is a Terraform repository: environments calling local
// modules (one of them nested, one of them twice through an override, two
// calling each other), external and missing module sources, a module of many
// rules of one type and many locals, and the same resource address in two
// modules.
func terraformFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"providers.tf": "terraform {\n  required_version = \">= 1.6\"\n}\nprovider \"aws\" {\n  region = \"ap-northeast-1\"\n}\n",
		"modules/network/main.tf": `variable "prefix" {}
variable "cidr" {}
resource "aws_vpc" "main" {
  cidr_block = var.cidr
  tags       = { Name = "${var.prefix}-vpc" }
}
resource "aws_subnet" "private" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${var.prefix}-private" }
}
output "vpc_id" { value = aws_vpc.main.id }
output "subnet_ids" { value = [aws_subnet.private.id] }
`,
		"modules/db/main.tf": `variable "prefix" {}
resource "aws_rds_cluster" "aurora" { cluster_identifier = "${var.prefix}-aurora" }
resource "aws_rds_cluster_instance" "aurora" { cluster_identifier = aws_rds_cluster.aurora.id }
resource "aws_vpc" "main" {}
`,
		"modules/cdn/main.tf": `variable "prefix" {}
locals {
  behavior_a = "/a/*"
  behavior_b = "/b/*"
  behavior_c = "/c/*"
}
resource "aws_cloudfront_distribution" "main" {
  comment = "${var.prefix}-cdn"
  paths   = [local.behavior_a, local.behavior_b, local.behavior_c]
}
`,
		"modules/app/sub/main.tf": `resource "aws_cloudwatch_log_group" "app" {}
`,
		"modules/cycle_a/main.tf": "module \"b\" {\n  source = \"../cycle_b\"\n}\n",
		"modules/cycle_b/main.tf": "module \"a\" {\n  source = \"../cycle_a\"\n}\n",
		"envs/prod/main.tf": `variable "prefix" {}
module "network" {
  source = "../../modules/network"
  prefix = var.prefix
  cidr   = "10.0.0.0/16"
}
module "app" {
  source  = "../../modules/app"
  prefix  = var.prefix
  vpc_id  = module.network.vpc_id
}
module "db" {
  source = "../../modules/db"
  prefix = var.prefix
}
module "cdn" {
  source = "..\\..\\modules\\cdn"
  prefix = var.prefix
}
module "registry_vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.0.0"
}
module "git_vpc" {
  source = "git::https://example.com/vpc.git"
}
module "missing" {
  source = "../../modules/missing"
}
module "outside" {
  source = "../../../outside"
}
module "dynamic" {
  source = var.prefix
}
`,
		"envs/ovr/main.tf":     "module \"app\" {\n  source = \"../../modules/app\"\n}\nmodule \"db\" {\n  source = \"../../modules/db\"\n}\n",
		"envs/ovr/override.tf": "module \"app\" {\n  source = \"../../modules/cdn\"\n}\n",
	}
	var app strings.Builder
	app.WriteString("variable \"prefix\" {}\nvariable \"vpc_id\" {}\nvariable \"subnet_ids\" { default = [] }\nvariable \"is_new\" { default = false }\nvariable \"tags\" { default = {} }\nvariable \"region\" { default = \"x\" }\n")
	app.WriteString("module \"logs\" {\n  source = \"./sub\"\n}\n")
	app.WriteString("resource \"aws_ecs_cluster\" \"this\" { name = \"${var.prefix}-${var.region}\" }\n")
	app.WriteString("resource \"aws_ecs_service\" \"web\" {\n  cluster = aws_ecs_cluster.this.id\n  tags    = var.tags\n}\n")
	app.WriteString("resource \"aws_lb\" \"web\" { subnets = var.subnet_ids }\n")
	app.WriteString("resource \"aws_lb_listener\" \"https\" { load_balancer_arn = aws_lb.web.arn }\n")
	app.WriteString("resource \"aws_security_group\" \"web\" { vpc_id = var.vpc_id }\n")
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&app, "resource \"aws_security_group_rule\" \"r%02d\" {\n  security_group_id = aws_security_group.web.id\n  description       = \"${var.prefix}-${var.is_new}-${local.l%02d}\"\n}\n", i, i)
	}
	app.WriteString("locals {\n")
	for i := 0; i < 15; i++ {
		fmt.Fprintf(&app, "  l%02d = \"${var.prefix}-%d\"\n", i, i)
	}
	app.WriteString("}\n")
	files["modules/app/main.tf"] = app.String()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func terraformMap(t *testing.T, root string, detail repomap.DetailLevel) *repomap.RepositoryMap {
	t.Helper()
	idx, err := index.New(context.Background(), root, []language.Provider{terraform.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	return repomap.Build(idx, root, repomap.Options{DetailLevel: detail})
}

func listing(m *repomap.RepositoryMap, pkg string) []string {
	for _, p := range m.Packages {
		if p.Path == filepath.FromSlash(pkg) {
			var names []string
			for _, s := range p.Symbols {
				names = append(names, s.Name)
			}
			return names
		}
	}
	return nil
}

func dependencies(m *repomap.RepositoryMap) map[string]bool {
	out := map[string]bool{}
	for _, d := range m.Dependencies {
		out[filepath.ToSlash(d.From)+" -> "+filepath.ToSlash(d.To)] = true
	}
	return out
}

// The default map of a Terraform repository lists, per module, what it
// builds: its resources (one of each type before a second of any) and module
// calls ahead of its variables, and no locals.
func TestTerraformMap_ListsTheInfrastructure(t *testing.T) {
	m := terraformMap(t, terraformFixture(t), repomap.DetailNormal)

	app := listing(m, "modules/app")
	if len(app) != 10 {
		t.Fatalf("modules/app lists %d symbols, want 10: %v", len(app), app)
	}
	for _, want := range []string{"aws_ecs_cluster.this", "aws_ecs_service.web", "aws_lb.web", "aws_lb_listener.https", "aws_security_group.web", "module.logs"} {
		if !contains(app, want) {
			t.Errorf("modules/app misses %s: %v", want, app)
		}
	}
	rules, firstVariable, lastResource := 0, -1, -1
	for i, name := range app {
		switch {
		case strings.HasPrefix(name, "aws_security_group_rule."):
			rules++
		case strings.HasPrefix(name, "local."):
			t.Errorf("modules/app lists the local %s in the default map", name)
		}
		if strings.HasPrefix(name, "var.") && firstVariable < 0 {
			firstVariable = i
		}
		if strings.HasPrefix(name, "aws_") || strings.HasPrefix(name, "module.") {
			lastResource = i
		}
	}
	if rules != 1 {
		t.Errorf("modules/app lists %d aws_security_group_rule, want 1 (one per type first): %v", rules, app)
	}
	if firstVariable >= 0 && firstVariable < lastResource {
		t.Errorf("modules/app lists a variable before a resource or module call: %v", app)
	}

	// No module's default listing has a local, however few its resources.
	for _, p := range m.Packages {
		for _, s := range p.Symbols {
			if strings.HasPrefix(s.Name, "local.") {
				t.Errorf("%s lists the local %s in the default map", p.Path, s.Name)
			}
		}
	}

	for pkg, want := range map[string][]string{
		"modules/network": {"aws_vpc.main", "aws_subnet.private"},
		"modules/db":      {"aws_rds_cluster.aurora", "aws_rds_cluster_instance.aurora", "aws_vpc.main"},
		"modules/cdn":     {"aws_cloudfront_distribution.main"},
		"envs/prod":       {"module.network", "module.app", "module.db", "module.cdn"},
		"":                {"provider.aws"},
	} {
		got := listing(m, pkg)
		for _, w := range want {
			if !contains(got, w) {
				t.Errorf("%q misses %s: %v", pkg, w, got)
			}
		}
	}
	// The same address in two modules is two symbols, each in its module.
	for _, p := range m.Packages {
		for _, s := range p.Symbols {
			if s.Name == "aws_vpc.main" && filepath.ToSlash(s.Qualified) != filepath.ToSlash(p.Path)+"/aws_vpc.main" {
				t.Errorf("%s lists %s", p.Path, s.Qualified)
			}
		}
	}
}

// A verbose map lists locals too, after what the module builds.
func TestTerraformMap_VerboseLocalsComeLast(t *testing.T) {
	m := terraformMap(t, terraformFixture(t), repomap.DetailVerbose)
	app := listing(m, "modules/app")
	for i, name := range app {
		if strings.HasPrefix(name, "local.") {
			for _, later := range app[i:] {
				if strings.HasPrefix(later, "aws_") || strings.HasPrefix(later, "module.") {
					t.Fatalf("a local ranks above %s: %v", later, app)
				}
			}
			return
		}
	}
}

// Dependencies are the module calls of a local directory inside the
// repository — nested, written with backslashes, or cyclic — and never an
// external source, a missing or outside directory, a source that is no
// literal, or a call an override file may replace.
func TestTerraformMap_ModuleDependencies(t *testing.T) {
	m := terraformMap(t, terraformFixture(t), repomap.DetailNormal)
	deps := dependencies(m)
	for _, want := range []string{
		"envs/prod -> modules/network", "envs/prod -> modules/app", "envs/prod -> modules/db", "envs/prod -> modules/cdn",
		"modules/app -> modules/app/sub",
		"modules/cycle_a -> modules/cycle_b", "modules/cycle_b -> modules/cycle_a",
		"envs/ovr -> modules/db",
	} {
		if !deps[want] {
			t.Errorf("missing dependency %s: %v", want, deps)
		}
	}
	for d := range deps {
		from, to, _ := strings.Cut(d, " -> ")
		if strings.Contains(to, "missing") || strings.Contains(to, "outside") || strings.Contains(to, "terraform-aws-modules") || strings.Contains(to, "example.com") {
			t.Errorf("dependency on no local module: %s", d)
		}
		if from == "envs/ovr" && (to == "modules/app" || to == "modules/cdn") {
			t.Errorf("dependency through a module call an override replaces: %s", d)
		}
	}
	if len(deps) != 8 {
		t.Errorf("%d dependencies, want 8: %v", len(deps), deps)
	}
}

// The text names every package the map holds — the JSON's packages, one
// line each, in the same order, a package that lists nothing too — so every
// end of a dependency line is a package of the listing.
func TestFormat_NamesEveryPackage(t *testing.T) {
	root := terraformFixture(t)
	if err := os.WriteFile(filepath.Join(root, "modules", "locals.tf"), []byte("locals {\n  a = 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, detail := range []repomap.DetailLevel{repomap.DetailMinimal, repomap.DetailNormal, repomap.DetailVerbose} {
		m := terraformMap(t, root, detail)
		var named []string
		lines := strings.Split(m.Format(), "\n")
		for i, line := range lines {
			if line == "Dependencies:" {
				break
			}
			if i == 0 || line == "" || strings.HasPrefix(line, "  ") {
				continue
			}
			named = append(named, strings.SplitN(line, " [", 2)[0])
		}
		var want []string
		listed := map[string]bool{}
		for _, p := range m.Packages {
			label := p.Path
			if label == "" {
				label = "."
			}
			want = append(want, label)
			listed[p.Path] = true
		}
		if strings.Join(named, "|") != strings.Join(want, "|") {
			t.Errorf("%s: text names %q, map holds %q", detail, named, want)
		}
		if !listed[filepath.FromSlash("modules")] {
			t.Errorf("%s: the package of locals.tf is not in the map", detail)
		}
		for _, d := range m.Dependencies {
			if !listed[d.From] || !listed[d.To] {
				t.Errorf("%s: dependency %q -> %q leaves the listing", detail, d.From, d.To)
			}
		}
	}
}

// The same index maps to the same text, every time.
func TestTerraformMap_Deterministic(t *testing.T) {
	root := terraformFixture(t)
	first := terraformMap(t, root, repomap.DetailNormal).Format()
	for i := 0; i < 5; i++ {
		if got := terraformMap(t, root, repomap.DetailNormal).Format(); got != first {
			t.Fatalf("run %d differs:\n%s\n---\n%s", i, first, got)
		}
	}
}

// A repository of another language is mapped as it always was: a Go
// package's unexported declarations stay out of the default map.
func TestDefaultPolicy_GoUnchanged(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nfunc Exported() {}\nfunc unexported() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := index.New(context.Background(), root, []language.Provider{golang.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	got := listing(repomap.Build(idx, root, repomap.Options{}), "")
	if len(got) != 1 || got[0] != "Exported" {
		t.Errorf("default map lists %v, want [Exported]", got)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// A module call depends on exactly the directory its source names: the root
// too, a directory with a dot in its name, the same directory once however
// many calls name it, never its own directory.
func TestTerraformMap_ModuleDependencyTargets(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"main.tf":           "resource \"aws_vpc\" \"root\" {}\n",
		"envs/a/main.tf":    "module \"up\" {\n  source = \"../..\"\n}\nmodule \"one\" {\n  source = \"../../mods/v1.2\"\n}\nmodule \"two\" {\n  source = \"../../mods/v1.2/\"\n}\nmodule \"self\" {\n  source = \"./\"\n}\nmodule \"dots\" {\n  source = \"./../a/../../mods/./v1.2\"\n}\n",
		"mods/v1.2/main.tf": "resource \"aws_s3_bucket\" \"output\" {}\noutput \"x\" { value = 1 }\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := terraformMap(t, root, repomap.DetailNormal)
	var got []string
	for _, d := range m.Dependencies {
		got = append(got, filepath.ToSlash(d.From)+" -> "+filepath.ToSlash(d.To))
	}
	want := []string{"envs/a -> ", "envs/a -> mods/v1.2"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("dependencies %q, want %q", got, want)
	}
}
