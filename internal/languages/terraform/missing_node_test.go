package terraform

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// The parser recovers from a missing identifier — the name after a trailing
// dot, the root of a traversal, an attribute name — by inserting a
// zero-width MISSING node, and from a broken line by joining it to the next.
// Neither is in the source: nothing is named after it (no empty name, no
// empty part of a name), and what the source does spell is still extracted.
// (A missing block label or for variable is guarded the same way, though
// the grammar recovers from those otherwise today.) Well-formed input has
// neither, so its extraction is the same as before.
func TestMissingIdentifiersNameNothing(t *testing.T) {
	for _, c := range []struct {
		name, src string
		refs      []string // every reference name, in order
		symbols   []string
		file      string // default m/main.tf
	}{
		// The fuzz input of PR #74's CI (FuzzExtractTerraform/bf9fa62123e93813).
		{"fuzz input", `output "A"0{A0= module.A. }0`, []string{"module.A"}, []string{"output.A"}, ""},
		{"module output after a trailing dot", `output "o" { value = module.m. }`, []string{"module.m"}, []string{"output.o"}, ""},
		{"module output after an index", `output "o" { value = module.m[0]. }`, []string{"module.m"}, []string{"output.o"}, ""},
		{"module output after a splat", `output "o" { value = module.m[*]. }`, []string{"module.m"}, []string{"output.o"}, ""},
		{"resource attribute after a trailing dot", `output "o" { value = aws_vpc.main. }`, []string{"aws_vpc.main"}, []string{"output.o"}, ""},
		{"resource name missing", `output "o" { value = aws_vpc. }`, nil, []string{"output.o"}, ""},
		{"variable name missing", `output "o" { value = var. }`, nil, []string{"output.o"}, ""},
		{"local name missing", `output "o" { value = local. }`, nil, []string{"output.o"}, ""},
		{"data source name missing", `output "o" { value = data.http. }`, nil, []string{"output.o"}, ""},
		{"for variable missing", `output "o" { value = [for in var.l : 1] }`, nil, nil, ""},
		{"template for variable missing", `output "o" { value = "%{ for in var.l }x%{ endfor }" }`, nil, nil, ""},
		{"block label missing", `resource "a" { }`, nil, nil, ""},
		{"attribute value missing", `resource "a" "b" { x = . }`, nil, []string{"a.b"}, ""},
		{"argument name missing", "module \"m\" { source = \"./c\"\n  = 1 }", nil, []string{"module.m"}, ""},
		{"traversal root missing", `output "o" { value = .m. }`, nil, []string{"output.o"}, ""},
		{"provider reference after a trailing dot", `resource "x" "y" { provider = aws. }`, nil, []string{"x.y"}, ""},
		{"tfvars name missing", "= 1\nregion = \"x\"\n", []string{"var.region"}, nil, "m/terraform.tfvars"},
		{"variable joined to the next line", "resource \"x\" \"z\" {\n  dynamic \"d\" {\n    for_each = var.\n    iterator = it\n    content { v = 1 }\n  }\n}\nvariable \"iterator\" {}\n", nil, []string{"x.z", "var.iterator"}, ""},
		{"resource joined to the next line", "output \"o\" { value = aws_vpc.\n  main = 1 }", nil, []string{"output.o"}, ""},
		{"check data without a type", `check "c" { data "" "latest" {} }`, nil, []string{"check.c"}, ""},
		{"provider joined to the next line", "resource \"a\" \"b\" { provider = aws.\n  west = 1 }", nil, []string{"a.b"}, ""},
		{"traversal broken before a dot", "output \"o\" { value = module\n  .m.x }", nil, []string{"output.o"}, ""},
		{"splat output on the next line", "output \"o\" { value = module.m[*]\n  .id }", []string{"module.m"}, []string{"output.o"}, ""},
		{"bracket left open joins nothing", "output \"o\" { value = foo(var.\n  y = bar() }", nil, nil, ""},
		// Well-formed counterparts: unchanged.
		{"module output", `output "o" { value = module.m.id }`, []string{"module.m", "id"}, []string{"output.o"}, ""},
		{"module output through an index", `output "o" { value = module.m[0].id }`, []string{"module.m", "id"}, []string{"output.o"}, ""},
		{"resource attribute", `output "o" { value = aws_vpc.main.id }`, []string{"aws_vpc.main"}, []string{"output.o"}, ""},
		{"Unicode identifiers", `output "ö" { value = module.ün.ä }`, []string{"module.ün", "ä"}, []string{"output.ö"}, ""},
		// HCL allows a line break inside brackets: these are references.
		{"line break in parentheses", "output \"o\" { value = (var.\n  a) }", []string{"var.a"}, []string{"output.o"}, ""},
		{"line break in a tuple", "output \"o\" { value = [aws_vpc\n  .main.id] }", []string{"aws_vpc.main"}, []string{"output.o"}, ""},
		{"line break in function arguments", "output \"o\" { value = foo(var.\n  b) }", []string{"var.b"}, []string{"output.o"}, ""},
		{"line break in an interpolation", "output \"o\" { value = \"${module.m.\n  out}\" }", []string{"module.m", "out"}, []string{"output.o"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := []byte(c.src)
			file := c.file
			if file == "" {
				file = "m/main.tf"
			}
			ext, err := NewProvider().Extract(context.Background(), source.FileID(file), src)
			if err != nil {
				t.Fatal(err)
			}
			var refs, syms []string
			for _, r := range ext.References {
				if slices.Contains(strings.Split(r.Name, "."), "") {
					t.Errorf("empty reference name or part: %+v", r)
				}
				refs = append(refs, r.Name)
			}
			for _, s := range ext.Symbols {
				if s.Name == "" || s.StartByte > s.EndByte || s.EndByte > uint32(len(src)) {
					t.Errorf("bad symbol: %+v", s)
				}
				syms = append(syms, s.Name)
			}
			if !slices.Equal(refs, c.refs) {
				t.Errorf("references %q, want %q", refs, c.refs)
			}
			if !slices.Equal(syms, c.symbols) {
				t.Errorf("symbols %q, want %q", syms, c.symbols)
			}
		})
	}
}
