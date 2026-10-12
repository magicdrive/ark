package terraform

import (
	"testing"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func TestResourceAddress(t *testing.T) {
	for _, tc := range []struct {
		kind            symbol.SymbolKind
		address         string
		mode, typ, name string
		ok              bool
	}{
		{symbol.KindResource, "aws_vpc.main", ModeManaged, "aws_vpc", "main", true},
		{symbol.KindResource, "resource.output.x", ModeManaged, "output", "x", true},
		{symbol.KindResource, "resource.ephemeral.x", ModeManaged, "ephemeral", "x", true},
		{symbol.KindResource, "ephemeral.random_password.db", ModeEphemeral, "random_password", "db", true},
		{symbol.KindDataSource, "data.aws_ami.ubuntu", ModeData, "aws_ami", "ubuntu", true},
		{symbol.KindVariable, "var.region", "", "", "", false},
		{symbol.KindVariable, "local.region", "", "", "", false},
		{symbol.KindModule, "module.network", "", "", "", false},
		{symbol.KindOutput, "output.vpc_id", "", "", "", false},
		{symbol.KindResource, "aws_vpc", "", "", "", false},
		{symbol.KindResource, "a.b.c.d", "", "", "", false},
		{symbol.KindDataSource, "aws_ami.ubuntu", "", "", "", false},
		{symbol.KindResource, ".main", "", "", "", false},
	} {
		mode, typ, name, ok := ResourceAddress(tc.kind, tc.address)
		if mode != tc.mode || typ != tc.typ || name != tc.name || ok != tc.ok {
			t.Errorf("ResourceAddress(%s, %q) = %q %q %q %v, want %q %q %q %v", tc.kind, tc.address, mode, typ, name, ok, tc.mode, tc.typ, tc.name, tc.ok)
		}
	}
}

// Every resource-like symbol the provider extracts splits into the type and
// name of its block header, in every mode.
func TestResourceAddress_MatchesExtraction(t *testing.T) {
	src := `resource "aws_vpc" "main" {}
resource "output" "odd" {}
data "aws_ami" "ubuntu" {}
ephemeral "random_password" "db" {}
variable "region" {}
`
	res, err := NewProvider().Extract(t.Context(), source.FileID("m/main.tf"), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][3]string{
		"aws_vpc.main":                 {ModeManaged, "aws_vpc", "main"},
		"resource.output.odd":          {ModeManaged, "output", "odd"},
		"data.aws_ami.ubuntu":          {ModeData, "aws_ami", "ubuntu"},
		"ephemeral.random_password.db": {ModeEphemeral, "random_password", "db"},
	}
	seen := 0
	for _, s := range res.Symbols {
		mode, typ, name, ok := ResourceAddress(s.Kind, s.Name)
		w, isResource := want[s.Name]
		if ok != isResource || (ok && [3]string{mode, typ, name} != w) {
			t.Errorf("%s %s: ResourceAddress = %q %q %q %v, want %v %v", s.Kind, s.Name, mode, typ, name, ok, w, isResource)
		}
		if ok {
			seen++
		}
	}
	if seen != len(want) {
		t.Errorf("%d resource-like symbols, want %d: %+v", seen, len(want), res.Symbols)
	}
}
