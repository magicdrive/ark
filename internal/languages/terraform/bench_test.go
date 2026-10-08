package terraform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// syntheticModule is one module file with n resources chained by references,
// locals, a module call and outputs.
func syntheticModule(n int) string {
	var b strings.Builder
	b.WriteString("variable \"cidr\" {}\nmodule \"child\" { source = \"./child\" }\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "resource \"aws_subnet\" \"s%d\" {\n  cidr = cidrsubnet(var.cidr, 8, %d)\n", i, i)
		if i > 0 {
			fmt.Fprintf(&b, "  prev = aws_subnet.s%d.id\n  ids  = [for x in aws_subnet.s%d[*] : x.id]\n  depends_on = [module.child]\n", i-1, i-1)
		}
		b.WriteString("  tags = { Name = \"s${count.index}\", Child = module.child.id }\n}\n")
		fmt.Fprintf(&b, "locals { l%d = aws_subnet.s%d.arn }\noutput \"o%d\" { value = local.l%d }\n", i, i, i, i)
	}
	return b.String()
}

func BenchmarkExtract(b *testing.B) {
	src := []byte(syntheticModule(200))
	p := NewProvider()
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := p.Extract(context.Background(), source.FileID("m/main.tf"), src); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIndex builds an index over modules × files, so a regression to
// per-reference repository scans shows as superlinear growth between sizes.
func BenchmarkIndex(b *testing.B) {
	for _, modules := range []int{10, 40} {
		b.Run(fmt.Sprintf("modules=%d", modules), func(b *testing.B) {
			dir := b.TempDir()
			for m := 0; m < modules; m++ {
				for f := 0; f < 3; f++ {
					p := filepath.Join(dir, fmt.Sprintf("mod%d", m), fmt.Sprintf("f%d.tf", f))
					if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
						b.Fatal(err)
					}
					src := strings.ReplaceAll(syntheticModule(30), "\"s", fmt.Sprintf("\"f%ds", f))
					src = strings.ReplaceAll(src, "aws_subnet.s", fmt.Sprintf("aws_subnet.f%ds", f))
					src = strings.ReplaceAll(src, "local.l", fmt.Sprintf("local.f%dl", f))
					src = strings.ReplaceAll(src, "locals { l", fmt.Sprintf("locals { f%dl", f))
					src = strings.ReplaceAll(src, "variable \"cidr\" {}\nmodule \"child\" { source = \"./child\" }\n", "")
					if f == 0 {
						src = "variable \"cidr\" {}\nmodule \"child\" { source = \"./child\" }\n" + src
					}
					if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
						b.Fatal(err)
					}
				}
			}
			providers := []language.Provider{NewProvider()}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := index.New(context.Background(), dir, providers); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
