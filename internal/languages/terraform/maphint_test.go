package terraform

import (
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func TestMapRole(t *testing.T) {
	p := NewProvider()
	for _, tc := range []struct {
		kind symbol.SymbolKind
		name string
		want language.MapRole
	}{
		{symbol.KindResource, "aws_vpc.main", language.MapRoleBuilt},
		{symbol.KindResource, "ephemeral.random_password.db", language.MapRoleBuilt},
		{symbol.KindModule, "module.network", language.MapRoleComposed},
		{symbol.KindDataSource, "data.aws_ami.ubuntu", language.MapRoleRead},
		{symbol.KindOutput, "output.vpc_id", language.MapRoleExposed},
		{symbol.KindConfiguration, "provider.aws", language.MapRoleConfigured},
		{symbol.KindConfiguration, "provider.aws.west", language.MapRoleConfigured},
		{symbol.KindConfiguration, "terraform", language.MapRoleDetail},
		{symbol.KindConfiguration, "check.health", language.MapRoleDetail},
		{symbol.KindVariable, "var.region", language.MapRoleParameter},
		{symbol.KindVariable, "local.name", language.MapRoleDetail},
		{symbol.KindFunction, "f", language.MapRoleUnspecified},
	} {
		if got := p.MapRole(tc.kind, tc.name); got != tc.want {
			t.Errorf("MapRole(%s, %s) = %v, want %v", tc.kind, tc.name, got, tc.want)
		}
	}
}

func TestDeclaredType(t *testing.T) {
	p := NewProvider()
	for _, tc := range []struct {
		kind symbol.SymbolKind
		name string
		want string
	}{
		{symbol.KindResource, "aws_vpc.main", "aws_vpc"},
		{symbol.KindResource, "resource.output.x", "output"},
		{symbol.KindResource, "ephemeral.random_password.db", "ephemeral.random_password"},
		{symbol.KindDataSource, "data.aws_vpc.shared", "data.aws_vpc"},
		{symbol.KindModule, "module.network", ""},
		{symbol.KindVariable, "var.region", ""},
	} {
		if got := p.DeclaredType(tc.kind, tc.name); got != tc.want {
			t.Errorf("DeclaredType(%s, %s) = %q, want %q", tc.kind, tc.name, got, tc.want)
		}
	}
}

// MemberScopeDir reads back the scope this provider states for a module call:
// the child directory of a local source, written with slashes or
// backslashes, nested or the root; nothing for any other source.
func TestMemberScopeDir_OfExtractedModuleCalls(t *testing.T) {
	src := `module "net" { source = "../../modules/network" }
module "win" { source = "..\\..\\modules\\cdn" }
module "sub" { source = "./sub" }
module "root" { source = "../.." }
module "registry" { source = "terraform-aws-modules/vpc/aws" }
module "git" { source = "git::https://example.com/vpc.git" }
module "outside" { source = "../../../outside" }
module "computed" { source = var.src }
`
	res, err := NewProvider().Extract(t.Context(), source.FileID("envs/prod/main.tf"), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"module.net": "modules/network", "module.win": "modules/cdn", "module.sub": "envs/prod/sub", "module.root": ""}
	p := NewProvider()
	seen := 0
	for _, s := range res.Symbols {
		if s.Kind != symbol.KindModule {
			continue
		}
		dir, ok := p.MemberScopeDir(s.MemberScope)
		w, local := want[s.Name]
		if ok != local || dir != w {
			t.Errorf("%s (scope %q): MemberScopeDir = %q %v, want %q %v", s.Name, s.MemberScope, dir, ok, w, local)
		}
		seen++
	}
	if seen != 8 {
		t.Errorf("%d module calls, want 8", seen)
	}
}

// A scope of any other form — not one this provider states — names no
// directory, rather than the root or a directory outside the repository.
func TestMemberScopeDir_OtherForms(t *testing.T) {
	p := NewProvider()
	for _, scope := range []string{"", "xoutput.", "a/xoutput.", "var.", "a/var.", "output", "/output.", "/a/output.", "../output.", "../a/output.", "a/../output.", "a//output.", "./output.", "a/./output.", "aws_s3_bucket.output.", `a\output.`} {
		if dir, ok := p.MemberScopeDir(scope); ok {
			t.Errorf("MemberScopeDir(%q) = %q, true; want none", scope, dir)
		}
	}
	for scope, want := range map[string]string{"output.": "", "a/output.": "a", "a/b/output.": "a/b", "a.b/output.": "a.b"} {
		if dir, ok := p.MemberScopeDir(scope); !ok || dir != want {
			t.Errorf("MemberScopeDir(%q) = %q, %v; want %q", scope, dir, ok, want)
		}
	}
}
