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

// One request that reads files it names: pin the root, take the policy,
// read n files (the deepest first), release. Three ways:
//
//	path:   the path gate's way — the rule files above each path read by
//	        path (IgnoreReader.For), then os.ReadFile
//	full:   Pin + Build (the whole tree's rules) + Snapshot.ReadFile
//	scoped: Pin + BuildScoped (the rules above each path) + Snapshot.ReadFile
func BenchmarkRequest(b *testing.B) {
	root := benchRoot(b)
	deep := deepestFile(root)
	for _, n := range []int{1, 50} {
		paths := append([]string{deep}, sample(root, n-1)...)
		b.Run(fmt.Sprintf("path/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				r := libgitignore.NewIgnoreReader(root, nil)
				for _, p := range paths {
					files, err := r.For(p)
					if err != nil {
						b.Fatal(err)
					}
					rule, _ := files.CompileSource(libgitignore.ArkSource)
					if !matchesWithParents(rule, p) {
						os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
					}
				}
			}
		})
		for _, mode := range []string{"full", "scoped"} {
			b.Run(fmt.Sprintf("%s/%d", mode, n), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					tr, err := fsroot.Pin(root, fsroot.Options{})
					if err != nil {
						b.Fatal(err)
					}
					var s *Snapshot
					if mode == "full" {
						s, err = Build(tr, Options{})
					} else {
						s, err = BuildScoped(tr, Options{})
					}
					if err != nil {
						b.Fatal(err)
					}
					for _, p := range paths {
						s.ReadFile(p)
					}
					s.Close()
					tr.Close()
				}
			})
		}
	}
}

func deepestFile(root string) string {
	best, depth := "", -1
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == ".git" {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if n := strings.Count(rel, "/"); n > depth && !strings.Contains(rel, ".git/") {
			best, depth = rel, n
		}
		return nil
	})
	return best
}
