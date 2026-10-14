package repomap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
)

// A stylesheet of hundreds of custom properties (design tokens) and a page of
// hundreds of ids are values and anchors: they rank a directory like
// constants do, not above the packages that define the repository's
// structure, and the listing shows the stylesheet before its tokens.
func TestWebDetailDoesNotDominate(t *testing.T) {
	root := t.TempDir()
	write := func(name, src string) {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/m\n\ngo 1.22\n")
	for i := range 40 {
		write(fmt.Sprintf("p%02d/p.go", i), fmt.Sprintf("package p%02d\n\ntype T struct{}\n\nfunc A() {}\n\nfunc B() {}\n", i))
	}
	var tokens, ids strings.Builder
	tokens.WriteString(":root {\n")
	for i := range 800 {
		fmt.Fprintf(&tokens, "  --color-%d: #%03x;\n", i, i)
		fmt.Fprintf(&ids, "<p id=\"s%d\">x</p>\n", i)
	}
	tokens.WriteString("}\n@keyframes spin { to { opacity: 0 } }\n")
	write("zstyles/tokens.css", tokens.String())
	write("zpages/index.html", ids.String())
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	m := Build(idx, root, Options{})
	if len(m.Packages) == 0 || m.Packages[0].Path == "zstyles" || m.Packages[0].Path == "zpages" {
		t.Fatalf("first package %q", m.Packages[0].Path)
	}
	for _, p := range m.Packages {
		if p.Path != "zstyles" {
			continue
		}
		if len(p.Symbols) == 0 || !strings.Contains(p.Symbols[0].label(), "tokens.css") {
			t.Errorf("zstyles lists %v first, want the stylesheet", p.Symbols)
		}
		for _, s := range p.Symbols {
			if strings.Contains(s.label(), "--color-") {
				t.Errorf("zstyles lists a custom property %s", s.label())
			}
		}
	}
}
