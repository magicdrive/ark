package terraform

import (
	"slices"
	"testing"
)

// Declaration names follow HCL's identifier syntax as Terraform validates it
// (hclsyntax.ValidIdentifier): (ID_Start | '_') (ID_Continue | '-')*.
func TestValidName(t *testing.T) {
	for _, ok := range []string{
		"main", "aws_vpc", "web-1", "_private", "a_b-c", "x9",
		"日本", "名前_1", "café", "ünï", "région", "Δx", "сервер",
		"e\u0301", // e + combining acute accent (Mn is ID_Continue)
		// ZWNJ / ZWJ are ID_Continue since Unicode 15.1; HCL's generated
		// tables and Go's unicode package are both Unicode 17.0.
		"ab\u200Cc", "a\u200Db",
	} {
		if !validName(ok) {
			t.Errorf("validName(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{
		"", "1abc", "-x", "a.b", "a b", "a/b", "a\"b", "${var.x}", "x$", "\U0001F680", "a\U0001F680",
		"\u0301e",                                   // a combining mark cannot start a name
		"\u200Da", "\xff", "a:b", "a*b", "a\u3000b", // ideographic space
		"\u2E2Fa", // VERTICAL TILDE: a letter (Lm), but Pattern_Syntax, so no ID_Start
	} {
		if validName(bad) {
			t.Errorf("validName(%q) = true, want false", bad)
		}
	}
}

func TestExtract_UnicodeDeclarationsAndReferences(t *testing.T) {
	ex := extract(t, "ネット/main.tf", `
resource "aws_vpc" "日本" {}
resource aws_vpc ünï {}
variable "région" {}
module "モジュール" { source = "./子" }
locals {
  名前 = aws_vpc.日本.id
  b    = aws_vpc.ünï.id
  c    = var.région
  d    = module.モジュール.出力
}
`)
	equalLines(t, "symbols", symbolLines(ex), []string{
		"resource aws_vpc.日本 ネット/aws_vpc.日本",
		"resource aws_vpc.ünï ネット/aws_vpc.ünï",
		"variable var.région ネット/var.région",
		"module module.モジュール ネット/module.モジュール scope=ネット/子/output.",
		"variable local.名前 ネット/local.名前",
		"variable local.b ネット/local.b",
		"variable local.c ネット/local.c",
		"variable local.d ネット/local.d",
	})
	equalLines(t, "references", refLines(ex), []string{
		"value_reference aws_vpc.日本@ネット/local.名前",
		"value_reference aws_vpc.ünï@ネット/local.b",
		"value_reference var.région@ネット/local.c",
		"value_reference module.モジュール@ネット/local.d",
		"value_reference module.モジュール->出力@ネット/local.d",
	})
	if len(ex.Diagnostics) != 0 {
		t.Errorf("diagnostics: %+v", ex.Diagnostics)
	}
}

func TestExtract_InvalidLabelsAreRejected(t *testing.T) {
	ex := extract(t, "main.tf", `
resource "aws_vpc" "1st" {}
resource "aws_vpc" "a.b" {}
resource "aws_vpc" "has space" {}
resource "aws_vpc" "`+"\U0001F680"+`" {}
variable "-x" {}
output "a/b" { value = 1 }
module "m:n" {}
provider "aws" { alias = "bad alias" }
`)
	// An invalid alias declares neither the alias nor the default
	// configuration provider.aws.
	if got := symbolLines(ex); len(got) != 0 {
		t.Errorf("symbols %v", got)
	}
	if len(ex.Diagnostics) != 8 {
		t.Errorf("want 8 diagnostics, got %+v", ex.Diagnostics)
	}
}

// Names are compared as written: Terraform (HCL) does not normalize
// identifiers, so NFC and NFD spellings, and look-alikes from another script,
// are different names — a reference to one never resolves to the other.
func TestGraph_UnicodeNamesResolveExactlyAndOnlyThemselves(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a/main.tf", "resource \"aws_vpc\" \"日本\" {}\nresource \"aws_vpc\" \"café\" {}\nresource \"aws_vpc\" \"main\" {}\n")
	write(t, dir, "a/out.tf", `
output "jp"    { value = aws_vpc.日本.id }
output "nfd"   { value = aws_vpc.cafe`+"\u0301"+`.id }
output "cyr"   { value = aws_vpc.m`+"\u0430"+`in.id }
`)
	write(t, dir, "b/main.tf", "resource \"aws_vpc\" \"日本\" {}\noutput \"jp\" { value = aws_vpc.日本.id }\n")
	idx := buildIndex(t, dir)
	edges := edgeLines(idx)
	for _, want := range []string{
		"a/output.jp -references-> a/aws_vpc.日本 exact",
		"b/output.jp -references-> b/aws_vpc.日本 exact",
	} {
		if !slices.Contains(edges, want) {
			t.Errorf("missing %q in %v", want, edges)
		}
	}
	for _, out := range []string{"a/output.nfd", "a/output.cyr"} {
		s := onlySymbol(t, idx, out)
		if len(idx.GetCallees(s.ID)) != 0 || idx.UnresolvedOutgoing(s.ID).Unresolved != 1 {
			t.Errorf("%s: a differently spelled name resolved: %v", out, idx.GetCallees(s.ID))
		}
	}
	if len(edges) != 2 {
		t.Errorf("edges %v", edges)
	}
}
