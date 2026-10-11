package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/perffixture"
)

// Performance benchmarks of the CLI dump on the synthetic repositories of
// internal/perffixture: the whole command (pinned root, policy snapshot,
// tree, file contents, output file), as `ark -S -f <format> <repo>` runs it.
// ARK_BENCH_FIXTURES (comma-separated spec names) limits the repositories.

func BenchmarkDump(b *testing.B) {
	names := []string{"small", "medium-shallow", "medium-deep", "medium-nested-rules", "large", "bigfiles"}
	if env := os.Getenv("ARK_BENCH_FIXTURES"); env != "" {
		names = strings.Split(env, ",")
	}
	for _, name := range names {
		spec, ok := perffixture.ByName(name)
		if !ok {
			b.Fatalf("unknown fixture %q", name)
		}
		b.Run(name, func(b *testing.B) {
			dir := b.TempDir()
			repo := filepath.Join(dir, "repo")
			st, err := perffixture.Write(repo, spec)
			if err != nil {
				b.Fatal(err)
			}
			b.Logf("%s: %s", name, st)
			for _, format := range []string{"txt", "xml", "arklite"} {
				b.Run(format, func(b *testing.B) {
					out := filepath.Join(dir, "out."+format)
					var size int64
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						_, opt, err := commandline.GeneralOptParse([]string{"-S", "-o", out, "-f", format, repo})
						if err != nil {
							b.Fatal(err)
						}
						if err := createDumpFile(opt); err != nil {
							b.Fatal(err)
						}
						if fi, err := os.Stat(out); err == nil {
							size = fi.Size()
						}
					}
					if size == 0 {
						b.Fatal("empty dump")
					}
					b.ReportMetric(float64(size), "outB")
				})
			}
		})
	}
}
