package fsroot

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Benchmarks against the current way (resolve by path, then read by path).
// The data: ARK_FSROOT_BENCH_ROOT if set (a real repository), else a
// generated tree of 40 directories x 50 files of 2 KiB, 2 levels deep.

func benchTree(b *testing.B) string {
	if r := os.Getenv("ARK_FSROOT_BENCH_ROOT"); r != "" {
		return r
	}
	root := b.TempDir()
	body := []byte(strings.Repeat("package p // benchmark data line\n", 64))
	for d := 0; d < 40; d++ {
		dir := filepath.Join(root, fmt.Sprintf("d%02d", d/8), fmt.Sprintf("s%02d", d))
		os.MkdirAll(dir, 0o755)
		for i := 0; i < 50; i++ {
			os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.go", i)), body, 0o644)
		}
	}
	os.Symlink("d00/s00/f00.go", filepath.Join(root, "l1"))
	os.Symlink("l1", filepath.Join(root, "l2"))
	os.Symlink("l2", filepath.Join(root, "l3"))
	return root
}

// deepest returns a root-relative path of a file at the greatest depth.
func deepest(root string) string {
	best, depth := "", -1
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if n := strings.Count(rel, string(filepath.Separator)); n > depth {
			best, depth = filepath.ToSlash(rel), n
		}
		return nil
	})
	return best
}

// pathRead is the current gate: resolve by path, check containment, read by
// path.
func pathRead(root, rel string) ([]byte, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return nil, err
	}
	realRoot, _ := filepath.EvalSymlinks(root)
	if r, err := filepath.Rel(realRoot, real); err != nil || strings.HasPrefix(r, "..") {
		return nil, ErrOutsideRoot
	}
	return os.ReadFile(full)
}

func BenchmarkPin(b *testing.B) {
	root := benchTree(b)
	for i := 0; i < b.N; i++ {
		tr, err := Pin(root, Options{})
		if err != nil {
			b.Fatal(err)
		}
		tr.Close()
	}
}

func BenchmarkDirectRead(b *testing.B) {
	root := benchTree(b)
	rel := deepest(root)
	b.Run("path", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := pathRead(root, rel); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("fsroot", func(b *testing.B) {
		tr, _ := Pin(root, Options{})
		defer tr.Close()
		for i := 0; i < b.N; i++ {
			if _, err := tr.ReadFile(rel, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("fsroot+pin", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tr, _ := Pin(root, Options{})
			if _, err := tr.ReadFile(rel, nil); err != nil {
				b.Fatal(err)
			}
			tr.Close()
		}
	})
}

func BenchmarkSymlinkResolution(b *testing.B) {
	root := benchTree(b)
	if _, err := os.Lstat(filepath.Join(root, "l3")); err != nil {
		b.Skip("no link chain in this tree")
	}
	b.Run("path", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := pathRead(root, "l3"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("fsroot", func(b *testing.B) {
		tr, _ := Pin(root, Options{})
		defer tr.Close()
		for i := 0; i < b.N; i++ {
			if _, err := tr.ReadFile("l3", nil); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkTraversal(b *testing.B) {
	root := benchTree(b)
	skip := func(name string) bool {
		return strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules"
	}
	b.Run("enumerate/path", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err == nil && d.IsDir() && p != root && skip(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			})
		}
	})
	b.Run("enumerate/fsroot", func(b *testing.B) {
		tr, _ := Pin(root, Options{})
		defer tr.Close()
		for i := 0; i < b.N; i++ {
			tr.Walk(".", func(e Entry) error {
				if e.IsDir() && e.Rel() != "." && skip(e.Name()) {
					return SkipDir
				}
				return nil
			})
		}
	})
	b.Run("read-all/path", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() {
					if p != root && skip(d.Name()) {
						return filepath.SkipDir
					}
					return nil
				}
				if strings.HasSuffix(p, ".go") && d.Type().IsRegular() {
					os.ReadFile(p)
				}
				return nil
			})
		}
	})
	b.Run("read-all/fsroot", func(b *testing.B) {
		tr, _ := Pin(root, Options{})
		defer tr.Close()
		for i := 0; i < b.N; i++ {
			tr.Walk(".", func(e Entry) error {
				if e.IsDir() {
					if e.Rel() != "." && skip(e.Name()) {
						return SkipDir
					}
					return nil
				}
				if strings.HasSuffix(e.Name(), ".go") && e.Type().IsRegular() {
					e.ReadAll()
				}
				return nil
			})
		}
	})
}

func BenchmarkDirIdentity(b *testing.B) {
	root := benchTree(b)
	rel := filepath.ToSlash(filepath.Dir(deepest(root)))
	tr, _ := Pin(root, Options{})
	defer tr.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tr.DirIdentity(rel); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConcurrentReads(b *testing.B) {
	root := benchTree(b)
	rel := deepest(root)
	b.Run("path", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				pathRead(root, rel)
			}
		})
	})
	b.Run("fsroot", func(b *testing.B) {
		tr, _ := Pin(root, Options{})
		defer tr.Close()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				tr.ReadFile(rel, nil)
			}
		})
	})
}
