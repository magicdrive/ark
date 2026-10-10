package accesspolicy

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/fsroot"
)

// Walk + decide (+ read) every regular file, three ways:
//   path:     today — filepath.WalkDir, the path-based rule (EvalSymlinks), os.ReadFile
//   resolve:  Phase 2 — a walk, then Snapshot.Check(Resolve(rel)) / Snapshot.ReadFile(rel)
//   entry:    Phase 3-A — Tree.Walk with CheckEntry / ReadEntry

func skipMeta(name string) bool { return name == ".git" || name == ".ark" }

func BenchmarkWalkDecide(b *testing.B) { benchWalk(b, false) }
func BenchmarkWalkRead(b *testing.B)   { benchWalk(b, true) }

func benchWalk(b *testing.B, read bool) {
	root := benchRoot(b)
	realRoot, _ := filepath.EvalSymlinks(root)
	b.Run("path", func(b *testing.B) {
		b.ReportAllocs()
		rule := oldPolicy(b, root)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() {
					if p != root && skipMeta(d.Name()) {
						return filepath.SkipDir
					}
					return nil
				}
				if !d.Type().IsRegular() {
					return nil
				}
				rel, _ := filepath.Rel(root, p)
				if !oldExcluded(rule, root, realRoot, filepath.ToSlash(rel)) && read {
					os.ReadFile(p)
				}
				return nil
			})
		}
	})
	// Today's walks (mcp excludesEntry): the rule on the entry's own path,
	// no resolution for regular files.
	b.Run("path-walk", func(b *testing.B) {
		b.ReportAllocs()
		rule := oldPolicy(b, root)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				rel, _ := filepath.Rel(root, p)
				rel = filepath.ToSlash(rel)
				if d.IsDir() {
					if p != root && (skipMeta(d.Name()) || rule.MatchesRel(rel)) {
						return filepath.SkipDir
					}
					return nil
				}
				if d.Type().IsRegular() && !rule.MatchesRel(rel) && read {
					os.ReadFile(p)
				}
				return nil
			})
		}
	})
	b.Run("resolve", func(b *testing.B) {
		b.ReportAllocs()
		tr, _ := fsroot.Pin(root, fsroot.Options{})
		defer tr.Close()
		s, _ := Build(tr, Options{})
		var rels []string
		tr.Walk(".", func(e fsroot.Entry) error {
			if e.IsDir() && skipMeta(e.Name()) {
				return fsroot.SkipDir
			}
			if e.Type().IsRegular() {
				rels = append(rels, e.Rel())
			}
			return nil
		})
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, rel := range rels {
				if read {
					s.ReadFile(rel)
				} else if r, err := tr.Resolve(rel); err == nil {
					s.Check(r)
				}
			}
		}
	})
	b.Run("entry", func(b *testing.B) {
		b.ReportAllocs()
		tr, _ := fsroot.Pin(root, fsroot.Options{})
		defer tr.Close()
		s, _ := Build(tr, Options{})
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tr.Walk(".", func(e fsroot.Entry) error {
				if e.IsDir() {
					if e.Rel() != "." && skipMeta(e.Name()) {
						return fsroot.SkipDir
					}
					return nil
				}
				if !e.Type().IsRegular() {
					return nil
				}
				if read {
					s.ReadEntry(e)
				} else {
					s.CheckEntry(e)
				}
				return nil
			})
		}
	})
}

// Concurrent full scans: each goroutine walks, decides and reads.
func BenchmarkWalkReadConcurrent(b *testing.B) {
	root := benchRoot(b)
	tr, _ := fsroot.Pin(root, fsroot.Options{})
	defer tr.Close()
	s, _ := Build(tr, Options{})
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tr.Walk(".", func(e fsroot.Entry) error {
				if e.IsDir() {
					if e.Rel() != "." && skipMeta(e.Name()) {
						return fsroot.SkipDir
					}
					return nil
				}
				if e.Type().IsRegular() {
					s.ReadEntry(e)
				}
				return nil
			})
		}
	})
}

// Direct reads by path through the snapshot (decide, then read the object
// decided): one file, and a thousand.
func BenchmarkDirectReads(b *testing.B) {
	root := benchRoot(b)
	tr, _ := fsroot.Pin(root, fsroot.Options{})
	defer tr.Close()
	s, _ := Build(tr, Options{})
	for _, n := range []int{1, 1000} {
		paths := sample(root, n)
		b.Run(fmt.Sprintf("snapshot/%d", len(paths)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for _, p := range paths {
					s.ReadFile(p)
				}
			}
		})
	}
	b.Run("resolve/1", func(b *testing.B) {
		p := sample(root, 1)[0]
		for i := 0; i < b.N; i++ {
			tr.Resolve(p)
		}
	})
}
