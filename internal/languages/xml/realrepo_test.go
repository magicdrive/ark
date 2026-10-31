package xml

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/languages/internal/declname"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestRealFiles (opt-in: ARK_XML_REAL_DIRS, a list of directories
// separated by the OS's list separator) reads every .xml file under the
// directories, read-only, and checks what FuzzExtractXML checks; it logs
// the files, the POMs, the diagnostics by code and the slowest file.
func TestRealFiles(t *testing.T) {
	dirs := os.Getenv("ARK_XML_REAL_DIRS")
	if dirs == "" {
		t.Skip("ARK_XML_REAL_DIRS is not set")
	}
	p := NewProvider()
	var files, poms, bytes, syms, deps int
	codes := map[string]int{}
	var slowest time.Duration
	var slowFile string
	for _, dir := range filepath.SplitList(dirs) {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".xml") {
				return nil
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			start := time.Now()
			ex, err := p.Extract(context.Background(), source.FileID(filepath.ToSlash(rel)), src)
			if el := time.Since(start); el > slowest {
				slowest, slowFile = el, path
			}
			if err != nil {
				t.Errorf("%s: %v", path, err)
				return nil
			}
			files++
			bytes += len(src)
			qualified := map[string]bool{}
			for _, s := range ex.Symbols {
				syms++
				qualified[s.Qualified] = true
				if s.Kind == symbol.KindPackage {
					poms++
				}
				if s.Kind == symbol.KindDependency {
					deps++
				}
				if int(s.EndByte) > len(src) || s.StartByte > s.EndByte || !declname.IsName(s.Name) || !declname.IsName(s.Qualified) {
					t.Errorf("%s: symbol %q [%d,%d)", path, s.Qualified, s.StartByte, s.EndByte)
				}
			}
			for _, r := range ex.References {
				if !qualified[r.Container] {
					t.Errorf("%s: reference %s without its container %s", path, r.Name, r.Container)
				}
			}
			for _, d := range ex.Diagnostics {
				codes[d.Code]++
				if os.Getenv("ARK_XML_REAL_VERBOSE") != "" {
					t.Logf("%s:%d: %s %s", path, d.Location.Range.Start.Line, d.Code, d.Message)
				}
			}
			return nil
		})
	}
	var cs []string
	for c, n := range codes {
		cs = append(cs, c+"="+itoa(n))
	}
	sort.Strings(cs)
	t.Logf("%d files (%d bytes), %d POMs, %d symbols, %d Maven dependencies; diagnostics %v; slowest %v %s",
		files, bytes, poms, syms, deps, cs, slowest, slowFile)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
