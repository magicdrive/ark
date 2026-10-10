package libgitignore

import (
	"os"
	"path/filepath"
	"testing"
)

// MatchesRel, and MatchesPath which uses it for paths inside the root, must
// decide exactly as the reference loop (MatchesPathHow): nested rule files,
// negations, anchored and directory patterns, and rules anchored outside the
// root (--additionally-ignorerule lines are anchored at the working
// directory).
func TestMatchesRel_EqualsMatchesPath(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".arkignore", "*.log\nbuild/\n/secret.txt\n!keep.log\ndocs/**/draft*\n")
	write("a/.arkignore", "*.go\n!main.go\n/local\n..dots\n")
	write("a/b/.arkignore", "main.go\n!*.log\n")
	write("c/.arkignore", "**/x/y\n?.md\n")
	paths := []string{
		"x.log", "keep.log", "a/keep.log", "a/b/keep.log", "a/b/c.log", "build", "build/o", "a/build/o",
		"secret.txt", "a/secret.txt", "docs/a/draft1", "docs/draft", "a/x.go", "a/main.go", "a/b/main.go",
		"a/b/z.go", "a/local", "a/local/f", "a/b/local", "a/..dots", "..dots", "a/..dots/f", "c/x/y", "c/q/x/y",
		"c/a.md", "c/ab.md", "a.md", "c", "a", "a/b", "z/a/b/main.go", "..x/a.log",
	}
	extraFile := filepath.Join(t.TempDir(), "extra.ignore")
	if err := os.WriteFile(extraFile, []byte("*.md\n!c/a.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, extra := range [][]string{nil, {extraFile}} {
		files, err := ReadIgnoreFiles(root, extra)
		if err != nil {
			t.Fatal(err)
		}
		gi, err := files.CompileSource(ArkSource)
		if err != nil {
			t.Fatal(err)
		}
		// Lines anchored at directories outside the root: its parent (an
		// ancestor), an unrelated directory, and a subdirectory.
		for _, dir := range []string{filepath.Dir(root), t.TempDir(), filepath.Join(root, "a")} {
			if _, err := AppendIgnoreLinesWithDir(gi, dir, "b/z.go", "!x.log"); err != nil {
				t.Fatal(err)
			}
		}
		check := func(when string) {
			for _, p := range paths {
				// MatchesPathHow is the reference: every pattern, in order.
				want, _ := gi.MatchesPathHow(p)
				if got := gi.MatchesRel(p); got != want {
					t.Errorf("extra %v, %s: MatchesRel(%q) = %v, reference %v", extra, when, p, got, want)
				}
				for _, spelling := range []string{p, filepath.Join(root, filepath.FromSlash(p)), "./" + p} {
					if got := gi.MatchesPath(spelling); got != want {
						t.Errorf("extra %v, %s: MatchesPath(%q) = %v, reference %v", extra, when, spelling, got, want)
					}
				}
			}
		}
		check("indexed")
		// Patterns appended after matching started are indexed too.
		if _, err := AppendIgnoreLinesWithDir(gi, root, "keep.log", "!a/x.go"); err != nil {
			t.Fatal(err)
		}
		check("after append")
	}
}
