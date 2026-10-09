package libgitignore

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func patternsOf(gi *GitIgnore) []string {
	var out []string
	for _, p := range gi.Patterns() {
		out = append(out, strings.Join([]string{p.Dir, p.Raw, p.Regexp.String(), map[bool]string{true: "!"}[p.Negate]}, "|"))
	}
	return out
}

// Compile builds exactly what GenerateIntegratedGitIgnore builds — the same
// patterns in the same order, with and without .gitignore handling — and
// fails where it fails.
func TestIgnoreFiles_CompileEqualsGenerate(t *testing.T) {
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
	write(".arkignore", "*.log\n!keep.log\n")
	write(".gitignore", "build/\n")
	write("a/.arkignore", "x.go\r\n# comment\n\n/y\n")
	write("a/b/.gitignore", "z\n")
	write("c/.arkignore", "\\!bang\n")
	write("d/x.go", "")
	write(".git/.arkignore", "never\n")
	// A subdirectory listed before the parent's rule files, a rule file
	// that is a symlink, and a dangling one (absent for os.Stat).
	write("-sub/.arkignore", "!keep.log\nsub.go\n")
	write("rules/shared", "linked.go\n")
	if err := os.Symlink("../rules/shared", filepath.Join(root, "d", ".arkignore")); err != nil {
		t.Fatal(err)
	}
	write("e/f.go", "")
	if err := os.Symlink("missing", filepath.Join(root, "e", ".arkignore")); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(t.TempDir(), "extra")
	if err := os.WriteFile(extra, []byte("e1\n!e2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, extras := range [][]string{nil, {extra}, {extra, filepath.Join(t.TempDir(), "missing")}} {
		files, err := ReadIgnoreFiles(root, extras)
		if err != nil {
			t.Fatal(err)
		}
		for _, allow := range []bool{false, true} {
			want, wantErr := GenerateIntegratedGitIgnore(allow, root, extras)
			got, gotErr := files.Compile(allow)
			if (wantErr == nil) != (gotErr == nil) {
				t.Fatalf("extras %v allow %v: error %v, Generate %v", extras, allow, gotErr, wantErr)
			}
			if wantErr == nil && !reflect.DeepEqual(patternsOf(got), patternsOf(want)) {
				t.Errorf("extras %v allow %v:\n got %q\nwant %q", extras, allow, patternsOf(got), patternsOf(want))
			}
		}
	}
}

// The fingerprint changes with any change to what Compile reads, and only
// then.
func TestIgnoreFiles_Fingerprint(t *testing.T) {
	root := t.TempDir()
	fp := func() string {
		f, err := ReadIgnoreFiles(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		return f.Fingerprint()
	}
	seen := map[string]string{}
	record := func(state string) {
		got := fp()
		if prev, ok := seen[got]; ok && prev != state {
			t.Errorf("%s and %s share a fingerprint", prev, state)
		}
		seen[got] = state
	}
	record("empty")
	if first, second := fp(), fp(); first != second {
		t.Fatal("fingerprint not stable")
	}
	_ = os.WriteFile(filepath.Join(root, ".arkignore"), []byte("a\n"), 0o644)
	record("root a")
	_ = os.WriteFile(filepath.Join(root, ".arkignore"), []byte("b\n"), 0o644)
	record("root b")
	_ = os.MkdirAll(filepath.Join(root, "d"), 0o755)
	_ = os.Rename(filepath.Join(root, ".arkignore"), filepath.Join(root, "d", ".arkignore"))
	record("moved to d") // same content, other directory
	_ = os.WriteFile(filepath.Join(root, "d", ".gitignore"), []byte("b\n"), 0o644)
	record("d with .gitignore")
}

// The rules For reads for one path decide it — and every directory above it
// — exactly as the whole repository's rules do.
func TestIgnoreReader_ForDecidesAsAll(t *testing.T) {
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
	write(".arkignore", "*.log\n!keep.log\nsecret/\n")
	write("a/.arkignore", "x.go\n!y.log\n")
	write("a/b/.arkignore", "*\n!*.go\n")
	write("a/b/c/.gitignore", "z.go\n")
	write("d/.arkignore", "/e\n")
	write(".git/sub/.arkignore", "never.go\n")
	write("real/.arkignore", "inner.go\n")
	write("real/inner.go", "")
	if err := os.Symlink("real", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"x.log", "keep.log", "secret", "secret/k", "a", "a/x.go", "a/y.log", "a/z.log", "a/b", "a/b/q.txt",
		"a/b/q.go", "a/b/c", "a/b/c/z.go", "a/b/c/w.txt", "d/e", "d/e/f", "d/f/e", ".git/sub/never.go",
		"link", "link/inner.go", "real/inner.go", "missing/dir/f.go", "a/x.go/deeper",
	}
	for _, allow := range []bool{false, true} {
		all, err := ReadIgnoreFiles(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		whole, err := all.Compile(allow)
		if err != nil {
			t.Fatal(err)
		}
		reader := NewIgnoreReader(root, nil)
		for _, p := range paths {
			files, err := reader.For(p)
			if err != nil {
				t.Fatalf("For(%q): %v", p, err)
			}
			part, err := files.Compile(allow)
			if err != nil {
				t.Fatal(err)
			}
			parts := strings.Split(p, "/")
			for i := range parts {
				q := strings.Join(parts[:i+1], "/")
				if got, want := part.MatchesRel(q), whole.MatchesRel(q); got != want {
					t.Errorf("allow %v: rules for %q decide %q = %v, whole repository %v", allow, p, q, got, want)
				}
			}
		}
	}
}

// One reader reads each file once: a change after the first read is not
// seen by that reader, in For or All, and is seen by the next reader.
func TestIgnoreReader_OneVersionPerReader(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	set := func(body string) {
		if err := os.WriteFile(filepath.Join(root, "a", ".arkignore"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	decides := func(f *IgnoreFiles, err error) bool {
		if err != nil {
			t.Fatal(err)
		}
		gi, err := f.Compile(false)
		if err != nil {
			t.Fatal(err)
		}
		return gi.MatchesRel("a/x.go")
	}
	set("x.go\n")
	r := NewIgnoreReader(root, nil)
	if !decides(r.For("a/x.go")) {
		t.Fatal("rule not applied")
	}
	set("other.go\n")
	if !decides(r.For("a/x.go")) || !decides(r.All()) {
		t.Error("a reader saw two versions of a file")
	}
	if decides(NewIgnoreReader(root, nil).All()) {
		t.Error("a new reader does not see the change")
	}
}
