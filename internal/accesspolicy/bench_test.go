package accesspolicy

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/fsroot"
	"github.com/magicdrive/ark/internal/libgitignore"
)

// Benchmarks against today's policy (IgnoreReader.All by path, compiled;
// a decision by path with EvalSymlinks). Data: ARK_POLICY_BENCH_ROOT if set,
// else a generated tree: 40 directories x 25 files, a rule file in every
// fifth directory, links into an excluded directory.

func benchRoot(b *testing.B) string {
	if r := os.Getenv("ARK_POLICY_BENCH_ROOT"); r != "" {
		return r
	}
	root := b.TempDir()
	for d := 0; d < 40; d++ {
		dir := filepath.Join(root, fmt.Sprintf("g%d", d/10), fmt.Sprintf("d%02d", d))
		os.MkdirAll(dir, 0o755)
		for i := 0; i < 25; i++ {
			os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.go", i)), []byte("package p\n"), 0o644)
		}
		if d%5 == 0 {
			os.WriteFile(filepath.Join(dir, ".arkignore"), []byte("f0*.go\n!f00.go\n"), 0o644)
		}
	}
	os.WriteFile(filepath.Join(root, ".arkignore"), []byte("*.tmp\ng3/\n"), 0o644)
	os.Symlink("g3/d30/f10.go", filepath.Join(root, "alias.go"))
	return root
}

// sample returns up to n root-relative regular file paths.
func sample(root string, n int) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || len(out) >= n {
			return filepath.SkipAll
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

func oldPolicy(b *testing.B, root string) *libgitignore.GitIgnore {
	files, err := libgitignore.ReadIgnoreFiles(root, nil)
	if err != nil {
		b.Fatal(err)
	}
	rule, err := files.CompileSource(libgitignore.ArkSource)
	if err != nil {
		b.Fatal(err)
	}
	return rule
}

func oldExcluded(rule *libgitignore.GitIgnore, root, realRoot, rel string) bool {
	match := func(rel string) bool {
		for i := 0; i < len(rel); i++ {
			if rel[i] == '/' && rule.MatchesRel(rel[:i]) {
				return true
			}
		}
		return rule.MatchesRel(rel)
	}
	if match(rel) {
		return true
	}
	if real, err := filepath.EvalSymlinks(filepath.Join(root, rel)); err == nil {
		if r, err := filepath.Rel(realRoot, real); err == nil && !strings.HasPrefix(r, "..") && r != "." {
			return match(filepath.ToSlash(r))
		}
	}
	return false
}

func BenchmarkBuild(b *testing.B) {
	root := benchRoot(b)
	b.Run("old", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			oldPolicy(b, root)
		}
	})
	b.Run("snapshot", func(b *testing.B) {
		b.ReportAllocs()
		tr, _ := fsroot.Pin(root, fsroot.Options{})
		defer tr.Close()
		for i := 0; i < b.N; i++ {
			if _, err := Build(tr, Options{}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkDecide(b *testing.B) {
	root := benchRoot(b)
	realRoot, _ := filepath.EvalSymlinks(root)
	for _, n := range []int{1, 1000} {
		paths := sample(root, n)
		b.Run(fmt.Sprintf("old/%d", len(paths)), func(b *testing.B) {
			rule := oldPolicy(b, root)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, p := range paths {
					oldExcluded(rule, root, realRoot, p)
				}
			}
		})
		b.Run(fmt.Sprintf("snapshot/%d", len(paths)), func(b *testing.B) {
			tr, _ := fsroot.Pin(root, fsroot.Options{})
			defer tr.Close()
			s, _ := Build(tr, Options{})
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, p := range paths {
					r, err := tr.Resolve(p)
					if err == nil {
						s.Check(r)
					}
				}
			}
		})
	}
}

func BenchmarkDecideSymlink(b *testing.B) {
	root := benchRoot(b)
	if _, err := os.Lstat(filepath.Join(root, "alias.go")); err != nil {
		b.Skip("no link")
	}
	realRoot, _ := filepath.EvalSymlinks(root)
	b.Run("old", func(b *testing.B) {
		rule := oldPolicy(b, root)
		for i := 0; i < b.N; i++ {
			oldExcluded(rule, root, realRoot, "alias.go")
		}
	})
	b.Run("snapshot", func(b *testing.B) {
		tr, _ := fsroot.Pin(root, fsroot.Options{})
		defer tr.Close()
		s, _ := Build(tr, Options{})
		for i := 0; i < b.N; i++ {
			r, _ := tr.Resolve("alias.go")
			s.Check(r)
		}
	})
}

func BenchmarkDecideConcurrent(b *testing.B) {
	root := benchRoot(b)
	paths := sample(root, 64)
	realRoot, _ := filepath.EvalSymlinks(root)
	b.Run("old", func(b *testing.B) {
		rule := oldPolicy(b, root)
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				oldExcluded(rule, root, realRoot, paths[i%len(paths)])
				i++
			}
		})
	})
	b.Run("snapshot", func(b *testing.B) {
		tr, _ := fsroot.Pin(root, fsroot.Options{})
		defer tr.Close()
		s, _ := Build(tr, Options{})
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if r, err := tr.Resolve(paths[i%len(paths)]); err == nil {
					s.Check(r)
				}
				i++
			}
		})
	})
}

// TestSnapshotSize reports what a snapshot holds (run with -v).
func TestSnapshotSize(t *testing.T) {
	root := os.Getenv("ARK_POLICY_BENCH_ROOT")
	if root == "" {
		t.Skip("set ARK_POLICY_BENCH_ROOT")
	}
	tr := pinTree(t, root, fsroot.Options{})
	s := snapshotOf(t, tr)
	bytes := 0
	for _, b := range s.rules {
		bytes += len(b)
	}
	t.Logf("directories %d, skipped %d, rule files %d (%d bytes)", len(s.dirs), len(s.skipped), len(s.rules), bytes)
}
