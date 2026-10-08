package terraform

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtractTerraform verifies the Terraform provider never panics, never
// emits out-of-bounds byte ranges or empty names, and keeps every reference
// identity-scoped, on arbitrary input.
func FuzzExtractTerraform(f *testing.F) {
	seeds := []string{
		"",
		"resource \"aws_vpc\" \"main\" { cidr_block = var.cidr }",
		"module \"m\" {\n  source = \"./child\"\n}\noutput \"o\" { value = module.m.id }\n",
		"locals { x = [for s in aws_subnet.a : s.id if s.ok] }",
		"resource \"a\" \"b\" {\n  dynamic \"d\" {\n    for_each = var.x\n    iterator = it\n    content { v = it.value }\n  }\n  depends_on = [a.c]\n}\n",
		"output \"o\" { value = \"${data.x.y.z}-%{ for v in var.l }${v}%{ endfor }\" }",
		"h = <<EOT\n${var.z}\nEOT\n",
		"check \"c\" {\n  data \"http\" \"h\" {}\n  assert { condition = data.http.h.ok }\n}\n",
		"resource \"a\" \"b\" {\n  x = var.y +\n}\nresource \"a\" \"c\" {}\n",
		"variable \"x\" {\n  type = string\n",
		"terraform { required_providers { aws = { source = \"hashicorp/aws\" } } }",
		"not terraform at all {{{ ]]] ${",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	p := NewProvider()
	f.Fuzz(func(t *testing.T, src []byte) {
		for _, file := range []string{"m/main.tf", "terraform.tfvars"} {
			ext, err := p.Extract(context.Background(), source.FileID(file), src)
			if err != nil {
				return // an honest error is fine; it must not panic
			}
			n := uint32(len(src))
			for _, s := range ext.Symbols {
				if s.Name == "" || s.Qualified == "" {
					t.Fatalf("empty name/qualified: %+v", s)
				}
				if s.StartByte > s.EndByte || s.EndByte > n {
					t.Fatalf("symbol out-of-bounds range [%d,%d] len=%d: %q", s.StartByte, s.EndByte, n, s.Name)
				}
				if s.Location.Range.Start.Line == 0 {
					t.Fatalf("symbol has 0 (non-1-based) line: %q", s.Name)
				}
				if s.MemberScope != "" && s.MembersOutside {
					t.Fatalf("both MemberScope and MembersOutside: %+v", s)
				}
			}
			for _, r := range ext.References {
				if r.Name == "" {
					t.Fatalf("empty reference name")
				}
				if r.Kind != "write" && r.NameQualified == "" && r.ReceiverTypeQualified == "" {
					t.Fatalf("reference without identity: %+v", r)
				}
			}
			if !ext.IdentityOnly {
				t.Fatal("IdentityOnly not set")
			}
		}
	})
}
