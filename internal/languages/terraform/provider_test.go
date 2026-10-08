package terraform

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func extract(t *testing.T, file, src string) language.Extraction {
	t.Helper()
	ex, err := NewProvider().Extract(context.Background(), source.FileID(file), []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ex
}

// symbolLines renders symbols as "kind name qualified[ member]" lines.
func symbolLines(ex language.Extraction) []string {
	var out []string
	for _, s := range ex.Symbols {
		l := fmt.Sprintf("%s %s %s", s.Kind, s.Name, s.Qualified)
		if s.MemberScope != "" {
			l += " scope=" + s.MemberScope
		}
		if s.MembersOutside {
			l += " outside"
		}
		out = append(out, l)
	}
	return out
}

// refLines renders references as "kind name@container" (with the receiver
// for a member reference) in source order.
func refLines(ex language.Extraction) []string {
	var out []string
	for _, r := range ex.References {
		name := r.Name
		if r.ReceiverExpr != "" {
			name = r.ReceiverExpr + "->" + r.Name
		}
		out = append(out, fmt.Sprintf("%s %s@%s", r.Kind, name, r.Container))
	}
	return out
}

func refNames(ex language.Extraction) []string {
	var out []string
	for _, r := range ex.References {
		out = append(out, r.Name)
	}
	sort.Strings(out)
	return out
}

func equalLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got: %s\nwant: %s", what, strings.Join(got, "\n      "), strings.Join(want, "\n      "))
	}
}

func TestExtract_Declarations(t *testing.T) {
	ex := extract(t, "infra/main.tf", `
resource "aws_vpc" "main" {}
data "aws_ami" "ubuntu" {}
ephemeral "random_password" "db" {}
module "network" { source = "./modules/network" }
variable "region" {}
locals {
  a = 1
  b = 2
}
locals { c = 3 }
output "vpc_id" { value = 1 }
provider "aws" {}
provider "aws" { alias = "west" }
terraform { required_version = ">= 1.5" }
check "health" {
  data "http" "h" {}
}
`)
	equalLines(t, "symbols", symbolLines(ex), []string{
		"resource aws_vpc.main infra/aws_vpc.main",
		"data_source data.aws_ami.ubuntu infra/data.aws_ami.ubuntu",
		"resource ephemeral.random_password.db infra/ephemeral.random_password.db",
		"module module.network infra/module.network scope=infra/modules/network/output.",
		"variable var.region infra/var.region",
		"variable local.a infra/local.a",
		"variable local.b infra/local.b",
		"variable local.c infra/local.c",
		"output output.vpc_id infra/output.vpc_id",
		"configuration provider.aws infra/provider.aws",
		"configuration provider.aws.west infra/provider.aws.west",
		"configuration terraform infra/terraform",
		"configuration check.health infra/check.health",
		"data_source data.http.h infra/check.health/data.http.h",
	})
	if !ex.IdentityOnly {
		t.Error("a Terraform file must be IdentityOnly")
	}
	for _, s := range ex.Symbols {
		wantExported := s.Kind == symbol.KindOutput || strings.HasPrefix(s.Name, "var.")
		if s.Exported != wantExported {
			t.Errorf("%s: Exported=%t, want %t (a module's interface is its variables and outputs)", s.Name, s.Exported, wantExported)
		}
		if s.StartByte > s.EndByte || s.Location.Range.Start.Line == 0 {
			t.Errorf("%s: bad range %+v", s.Name, s)
		}
	}
	if len(ex.Diagnostics) != 0 {
		t.Errorf("valid source produced diagnostics: %+v", ex.Diagnostics)
	}
}

func TestExtract_RootModuleQualifiedIsTheAddress(t *testing.T) {
	ex := extract(t, "main.tf", `resource "aws_vpc" "main" {}`)
	equalLines(t, "symbols", symbolLines(ex), []string{"resource aws_vpc.main aws_vpc.main"})
}

func TestExtract_BareIdentifierLabels(t *testing.T) {
	ex := extract(t, "main.tf", `resource aws_vpc main {}`)
	equalLines(t, "symbols", symbolLines(ex), []string{"resource aws_vpc.main aws_vpc.main"})
}

func TestExtract_ModuleSources(t *testing.T) {
	ex := extract(t, "envs/prod/main.tf", `
module "local" { source = "./child" }
module "parent" { source = "../../modules/vpc" }
module "self_root" { source = "../.." }
module "escapes" { source = "../../../outside" }
module "registry" { source = "terraform-aws-modules/vpc/aws" }
module "git" { source = "git::https://example.com/vpc.git?ref=v1" }
module "github" { source = "github.com/hashicorp/example" }
module "no_source" {}
module "interpolated" { source = "./${var.x}" }
module "absolute" { source = "/abs/path" }
`)
	equalLines(t, "symbols", symbolLines(ex), []string{
		"module module.local envs/prod/module.local scope=envs/prod/child/output.",
		"module module.parent envs/prod/module.parent scope=modules/vpc/output.",
		"module module.self_root envs/prod/module.self_root scope=output.",
		"module module.escapes envs/prod/module.escapes outside",
		"module module.registry envs/prod/module.registry outside",
		"module module.git envs/prod/module.git outside",
		"module module.github envs/prod/module.github outside",
		"module module.no_source envs/prod/module.no_source",
		"module module.interpolated envs/prod/module.interpolated",
		"module module.absolute envs/prod/module.absolute outside",
	})
}

func TestExtract_ReferenceClassification(t *testing.T) {
	ex := extract(t, "main.tf", `
resource "aws_instance" "web" {
  vpc_id     = aws_vpc.main.id
  ami        = data.aws_ami.ubuntu.id
  key        = ephemeral.random_password.db.result
  region     = var.region
  name       = local.name
  net        = module.network
  vpc        = module.network.vpc_id
  provider   = aws.west
  depends_on = [aws_security_group.web, module.network]
}
module "m" {
  source    = "./m"
  version   = "1.0.0"
  providers = { aws = aws, aws.east = aws.west }
}
`)
	equalLines(t, "references", refLines(ex), []string{
		"value_reference aws_vpc.main@aws_instance.web",
		"value_reference data.aws_ami.ubuntu@aws_instance.web",
		"value_reference ephemeral.random_password.db@aws_instance.web",
		"value_reference var.region@aws_instance.web",
		"value_reference local.name@aws_instance.web",
		"value_reference module.network@aws_instance.web",
		"value_reference module.network@aws_instance.web",
		"value_reference module.network->vpc_id@aws_instance.web",
		"value_reference provider.aws.west@aws_instance.web",
		"depends_on aws_security_group.web@aws_instance.web",
		"depends_on module.network@aws_instance.web",
		"value_reference provider.aws@module.m",
		"value_reference provider.aws.west@module.m",
	})
	for _, r := range ex.References {
		if !r.IdentityInRepository {
			t.Errorf("%s: IdentityInRepository not set", r.Name)
		}
		switch {
		case r.ReceiverExpr != "":
			if r.ReceiverTypeQualified != r.ReceiverExpr || r.NameQualified != "" {
				t.Errorf("%s: member reference evidence %+v", r.Name, r)
			}
		case r.NameQualified != r.Name:
			t.Errorf("%s: NameQualified=%q, want the root-module address", r.Name, r.NameQualified)
		}
	}
}

func TestExtract_ReferencesAreModuleQualified(t *testing.T) {
	ex := extract(t, "modules/network/outputs.tf", `
output "vpc_id" { value = aws_vpc.main.id }
output "sub" { value = module.sub.id }
`)
	var got []string
	for _, r := range ex.References {
		got = append(got, r.NameQualified+"|"+r.ReceiverTypeQualified)
	}
	equalLines(t, "identities", got, []string{
		"modules/network/aws_vpc.main|",
		"modules/network/module.sub|",
		"|modules/network/module.sub",
	})
}

// TestExtract_NotReferences pins what is deliberately never observed as a
// reference (package doc): built-in and contextual values, locally bound
// names, attribute paths, object keys, type constraints, function names.
func TestExtract_NotReferences(t *testing.T) {
	ex := extract(t, "main.tf", `
variable "x" {
  type = object({ name = string, tags = map(string) })
  validation {
    condition     = length(var.x.name) > 0
    error_message = "x"
  }
}
resource "aws_instance" "web" {
  count     = 2
  name      = "web-${count.index}-${terraform.workspace}"
  path      = "${path.module}/f"
  for_each  = toset(["a"])
  key       = each.key
  ids       = [for aws_vpc in var.list : aws_vpc.main]
  objs      = { for k, v in var.map : k => v.id if v.ok }
  tmpl      = "%{ for s in var.list }${s.id}%{ endfor }"
  obj       = { name = 1, aws_vpc = 2 }
  fn        = provider::aws::arn_parse(var.arn)
  bare      = foo
  indexed   = foo[0]
  dynamic "ingress" {
    for_each = var.rules
    content {
      port = ingress.value.port
    }
  }
  dynamic "egress" {
    for_each = var.rules
    iterator = rule
    content {
      port = rule.value.port
      bad  = egress.value
    }
  }
  lifecycle {
    ignore_changes       = [tags.Name, ami]
    replace_triggered_by = [null_resource.trigger.id]
    precondition {
      condition     = self.id != ""
      error_message = "x"
    }
  }
  provisioner "local-exec" {
    command = "echo ${self.private_ip}"
  }
  connection {
    host = self.public_ip
  }
}
`)
	equalLines(t, "references", refNames(ex), []string{
		"egress.value", // the iterator is renamed: `egress` is no longer bound
		"null_resource.trigger",
		"var.arn",
		"var.list",
		"var.list",
		"var.map",
		"var.rules",
		"var.rules",
		"var.x",
	})
}

func TestExtract_ExpressionForms(t *testing.T) {
	ex := extract(t, "main.tf", `
locals {
  conditional = var.enabled ? aws_vpc.a.id : aws_vpc.b.id
  call        = coalesce(local.x, data.aws_ami.u.id)
  tuple       = [aws_subnet.a.id, aws_subnet.b.id]
  object      = { id = aws_vpc.c.id, (local.key) = 1 }
  splat       = aws_instance.web[*].id
  legacy      = aws_instance.web2.*.id
  legacy_idx  = aws_instance.web3.0.id
  index       = aws_subnet.list[var.i].id
  heredoc     = <<-EOT
    ${aws_vpc.d.arn}
  EOT
  directive   = "%{ if var.flag }${aws_vpc.e.id}%{ endif }"
  paren       = (aws_vpc.f).id
  math        = -local.n + local.m * 2
  for_coll    = [for s in aws_subnet.c : s.id]
  mod_index   = module.m["a"].out
  mod_splat   = module.n[*].out
  mod_legacy  = module.o.*.out
  explicit    = resource.aws_route53_zone.z.zone_id
  short       = resource.aws_vpc
}
`)
	equalLines(t, "references", refNames(ex), []string{
		"aws_instance.web", "aws_instance.web2", "aws_instance.web3",
		"aws_route53_zone.z", // resource.TYPE.NAME is TYPE.NAME
		"aws_subnet.a", "aws_subnet.b", "aws_subnet.c", "aws_subnet.list",
		"aws_vpc.a", "aws_vpc.b", "aws_vpc.c", "aws_vpc.d", "aws_vpc.e", "aws_vpc.f",
		"data.aws_ami.u",
		"local.key", "local.m", "local.n", "local.x",
		"module.m", "module.n", "module.o",
		"out", "out", "out",
		"var.enabled", "var.flag", "var.i",
	})
}

func TestExtract_LocationsCoverTheAddressOnly(t *testing.T) {
	ex := extract(t, "main.tf", "output \"o\" {\n  value = aws_vpc.main.id\n}\n")
	if len(ex.References) != 1 {
		t.Fatalf("references: %v", refLines(ex))
	}
	r := ex.References[0].Location.Range
	if r.Start != (source.Position{Line: 2, Column: 11}) || r.End != (source.Position{Line: 2, Column: 23}) {
		t.Errorf("range %+v, want 2:11-2:23 (aws_vpc.main without .id)", r)
	}
}

func TestExtract_CheckScopedData(t *testing.T) {
	ex := extract(t, "main.tf", `
check "health" {
  data "http" "h" { url = var.url }
  assert {
    condition     = data.http.h.status_code == 200 && data.http.other.ok
    error_message = "down"
  }
}
output "o" { value = data.http.h.body }
`)
	var got []string
	for _, r := range ex.References {
		got = append(got, r.Name+"="+r.NameQualified+"@"+r.Container)
	}
	equalLines(t, "references", got, []string{
		"var.url=var.url@check.health/data.http.h",
		"data.http.h=check.health/data.http.h@check.health",
		"data.http.other=data.http.other@check.health",
		"data.http.h=data.http.h@output.o", // outside the check it is not in scope
	})
}

func TestExtract_DependsOnOnlyAtTopLevel(t *testing.T) {
	ex := extract(t, "main.tf", `
resource "x" "y" {
  nested {
    depends_on = aws_vpc.main.id
  }
  depends_on = [aws_vpc.other]
}
`)
	equalLines(t, "references", refLines(ex), []string{
		"value_reference aws_vpc.main@x.y",
		"depends_on aws_vpc.other@x.y",
	})
}

func TestExtract_TerraformBlockIsNotScanned(t *testing.T) {
	ex := extract(t, "main.tf", `
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
  backend "s3" { bucket = "b" }
}
terraform { required_version = ">= 1.6" }
`)
	equalLines(t, "symbols", symbolLines(ex), []string{"configuration terraform terraform"})
	if len(ex.References) != 0 || len(ex.Diagnostics) != 0 {
		t.Errorf("refs=%v diags=%+v", refLines(ex), ex.Diagnostics)
	}
}

func TestExtract_AddressMetadataBlocksAreNotObserved(t *testing.T) {
	ex := extract(t, "main.tf", `
moved {
  from = aws_instance.old
  to   = aws_instance.new
}
import {
  to = aws_instance.new
  id = var.id
}
removed {
  from = aws_instance.gone
}
unknown_block "x" { a = var.y }
`)
	if len(ex.Symbols) != 0 || len(ex.References) != 0 {
		t.Errorf("symbols=%v refs=%v", symbolLines(ex), refLines(ex))
	}
}

func TestExtract_DuplicateDeclarationInOneFile(t *testing.T) {
	ex := extract(t, "main.tf", `
resource "aws_vpc" "main" { a = var.first }
resource "aws_vpc" "main" { a = var.second }
`)
	equalLines(t, "symbols", symbolLines(ex), []string{"resource aws_vpc.main aws_vpc.main"})
	equalLines(t, "references", refLines(ex), []string{
		"value_reference var.first@aws_vpc.main",
		"value_reference var.second@", // the duplicate is no container
	})
	if len(ex.Diagnostics) != 1 || ex.Diagnostics[0].Severity != language.SeverityWarning {
		t.Errorf("diagnostics: %+v", ex.Diagnostics)
	}
}

func TestExtract_OverrideFileDeclaresNothing(t *testing.T) {
	for _, file := range []string{"override.tf", "net/web_override.tf"} {
		ex := extract(t, file, `
resource "aws_instance" "web" { ami = var.ami }
locals { x = var.y }
`)
		if len(ex.Symbols) != 0 {
			t.Errorf("%s: symbols %v", file, symbolLines(ex))
		}
		equalLines(t, file+" references", refLines(ex), []string{
			"value_reference var.ami@",
			"value_reference var.y@",
		})
	}
}

func TestExtract_TfvarsAreWritesWithoutIdentity(t *testing.T) {
	for _, file := range []string{"terraform.tfvars", "prod.auto.tfvars", "envs/prod.tfvars"} {
		ex := extract(t, file, "region = \"eu\"\ncount_x = 2\nresource \"x\" \"y\" {}\n")
		if len(ex.Symbols) != 0 {
			t.Errorf("%s: symbols %v", file, symbolLines(ex))
		}
		equalLines(t, file+" references", refLines(ex), []string{"write var.region@", "write var.count_x@"})
		for _, r := range ex.References {
			if r.NameQualified != "" || r.IdentityInRepository {
				t.Errorf("%s: a tfvars write must carry no identity: %+v", file, r)
			}
		}
	}
}

func TestExtract_MalformedHeaders(t *testing.T) {
	ex := extract(t, "main.tf", `
resource "only_one_label" { a = var.x }
variable { }
resource "aws_vpc" "${var.n}" {}
`)
	if len(ex.Symbols) != 0 || len(ex.References) != 0 {
		t.Errorf("symbols=%v refs=%v", symbolLines(ex), refLines(ex))
	}
	if len(ex.Diagnostics) != 3 {
		t.Errorf("want one diagnostic per malformed header: %+v", ex.Diagnostics)
	}
}

func TestExtract_CommentsAreIgnored(t *testing.T) {
	ex := extract(t, "main.tf", `
# resource "aws_vpc" "commented" {}
// resource "aws_vpc" "commented2" {}
/* resource "aws_vpc" "commented3" { a = var.x } */
resource "aws_vpc" "main" {
  # cidr = var.hidden
  cidr = var.cidr // trailing var.no
}
`)
	equalLines(t, "symbols", symbolLines(ex), []string{"resource aws_vpc.main aws_vpc.main"})
	equalLines(t, "references", refNames(ex), []string{"var.cidr"})
}

func TestExtract_IncompleteSourceRecoversLaterDeclarations(t *testing.T) {
	ex := extract(t, "main.tf", `
resource "aws_vpc" "broken" {
  cidr = var.x +
}

resource "aws_vpc" "good" {
  cidr = var.y
}
`)
	if !slices.Contains(symbolLines(ex), "resource aws_vpc.good aws_vpc.good") {
		t.Errorf("trailing valid declaration not recovered: %v", symbolLines(ex))
	}
	if !slices.Contains(refNames(ex), "var.y") {
		t.Errorf("references of the recovered declaration missing: %v", refNames(ex))
	}
	if len(ex.Diagnostics) == 0 || ex.Diagnostics[0].Severity != language.SeverityError {
		t.Errorf("broken source must yield an error diagnostic: %+v", ex.Diagnostics)
	}
}

func TestExtract_UnclosedBlock(t *testing.T) {
	ex := extract(t, "main.tf", "variable \"x\" {\n  type = string\n\noutput \"y\" {\n  value = var.x\n}\n")
	if len(ex.Diagnostics) == 0 {
		t.Error("no diagnostic for an unclosed block")
	}
	for _, s := range ex.Symbols {
		if s.EndByte > uint32(len("variable \"x\" {\n  type = string\n\noutput \"y\" {\n  value = var.x\n}\n")) {
			t.Errorf("symbol range beyond source: %+v", s)
		}
	}
}

func TestExtract_DeepNestingIsBounded(t *testing.T) {
	depth := maxExprDepth * 2
	src := "locals {\n  x = " + strings.Repeat("[", depth) + "var.deep" + strings.Repeat("]", depth) + "\n  y = var.shallow\n}\n"
	ex := extract(t, "main.tf", src)
	names := refNames(ex)
	if slices.Contains(names, "var.deep") {
		t.Error("a reference below the depth bound was observed")
	}
	if !slices.Contains(names, "var.shallow") {
		t.Error("references after the deep expression were lost")
	}
	var warned int
	for _, d := range ex.Diagnostics {
		if strings.Contains(d.Message, "nesting too deep") {
			warned++
		}
	}
	if warned != 1 {
		t.Errorf("want exactly one depth diagnostic, got %d: %+v", warned, ex.Diagnostics)
	}
}

func TestExtract_EveryReferenceKindFormsAnEdgeOrIsAWrite(t *testing.T) {
	ex := extract(t, "main.tf", `
resource "a" "b" {
  x = var.y
  depends_on = [a.c]
}
`)
	for _, r := range ex.References {
		switch reference.ReferenceKind(r.Kind) {
		case reference.KindValueReference, reference.KindExplicitDependency:
		default:
			t.Errorf("unexpected reference kind %q", r.Kind)
		}
	}
}

func TestProvider_Identity(t *testing.T) {
	p := NewProvider()
	if p.Language() != "terraform" {
		t.Errorf("Language() = %q", p.Language())
	}
	if !slices.Equal(p.Extensions(), []string{".tf", ".tfvars"}) {
		t.Errorf("Extensions() = %v (.hcl must not be claimed: it is not Terraform-specific)", p.Extensions())
	}
	if p.CacheVersion() == "" {
		t.Error("empty CacheVersion")
	}
}
