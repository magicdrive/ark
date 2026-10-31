package markdown

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/css"
	"github.com/magicdrive/ark/internal/languages/html"
	"github.com/magicdrive/ark/internal/languages/yaml"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestCorpus_Measure is an opt-in measurement over a directory tree of
// Markdown files, read only: ARK_MD_CORPUS names its root. Every .md and
// .markdown file is extracted (no index, nothing written): the number of
// files, bytes, sections, links and code blocks, the total and the slowest
// file's time, and the guard's costs — the largest block cost and file
// cost (guard.go), and the files the guard rewrote.
func TestCorpus_Measure(t *testing.T) {
	root := os.Getenv("ARK_MD_CORPUS")
	if root == "" {
		t.Skip("ARK_MD_CORPUS is not set")
	}
	var files []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == "cache" && filepath.Dir(p) == root {
			return filepath.SkipDir
		}
		if ext := strings.ToLower(filepath.Ext(p)); d.Type().IsRegular() && (ext == ".md" || ext == ".markdown") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	p := NewProvider()
	var bytes, secs, links, codes, degenerate, large int
	var total, slowest time.Duration
	var slowFile string
	var slowSize int
	var maxBlock, maxFile int
	var maxBlockFile, maxFileFile string
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		bytes += len(src)
		start := time.Now()
		x, err := p.Extract(context.Background(), source.FileID("a.md"), src)
		d := time.Since(start)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		total += d
		if d > slowest {
			slowest, slowFile, slowSize = d, f, len(src)
		}
		for _, s := range x.Symbols {
			switch s.Kind {
			case symbol.KindSection:
				secs++
			case symbol.KindCodeBlock:
				codes++
			}
		}
		for _, r := range x.References {
			if r.Kind != string(reference.KindContains) {
				links++
			}
		}
		for _, dg := range x.Diagnostics {
			switch dg.Code {
			case CodeDegenerate:
				degenerate++
				t.Logf("degenerate: %s: %s", f, dg.Message)
			case CodeTooLarge:
				large++
			}
		}
		if len(src) <= maxBytes {
			b, fc := guardCosts(src)
			if b > maxBlock {
				maxBlock, maxBlockFile = b, f
			}
			if fc > maxFile {
				maxFile, maxFileFile = fc, f
			}
		}
	}
	t.Logf("files %d, %d bytes; sections %d, links %d, code blocks %d; degenerate %d, too large %d", len(files), bytes, secs, links, codes, degenerate, large)
	t.Logf("time %v (%.1f MB/s); slowest %v: %s (%d bytes)", total, float64(bytes)/total.Seconds()/1e6, slowest, slowFile, slowSize)
	t.Logf("largest block cost %d (%.4f of the budget): %s", maxBlock, float64(maxBlock)/inlineBudget, maxBlockFile)
	t.Logf("largest file cost %d (%.4f of the budget): %s", maxFile, float64(maxFile)/inlineBudget, maxFileFile)
}

// guardCosts returns the largest block cost and the file's cost, as the
// guard computes them.
func guardCosts(src []byte) (block, file int) {
	g := guard(src)
	return g.maxBlock, g.total
}

// TestSelf_ArkDocuments is the opt-in self-check over Ark's own documents:
// ARK_MD_SELF names an Ark checkout. The index (Markdown, HTML, CSS and
// YAML providers; no cache) reads README.md, ARCHITECTURE.md and docs/**;
// it reports the headings, the local links and how many resolve, and lists
// every broken link: a file that does not exist, an anchor no heading or
// id of the file has, a file no provider declares (a directory, a Go file).
func TestSelf_ArkDocuments(t *testing.T) {
	root := os.Getenv("ARK_MD_SELF")
	if root == "" {
		t.Skip("ARK_MD_SELF is not set")
	}
	idx, err := index.New(context.Background(), root, []language.Provider{NewProvider(), html.NewProvider(), css.NewProvider(), yaml.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	inScope := func(f source.FileID) bool {
		s := filepath.ToSlash(string(f))
		return s == "README.md" || s == "ARCHITECTURE.md" || strings.HasPrefix(s, "docs/") && strings.HasSuffix(s, ".md")
	}
	// A reference resolves when a declaration carries its identity: a
	// file's own declaration (or its asset node) for a resource link, a
	// section or an anchor for an id reference. The count is checked
	// against the index's own count of unresolved references.
	declares := func(q string) bool {
		for _, s := range idx.FindSymbolsByQualified(q) {
			if symbol.DeclaresFile(s.Kind) || s.Kind == symbol.KindSection || s.Kind == symbol.KindAnchor {
				return true
			}
		}
		return false
	}
	var docs, headings, local, external, anchors, anchorsBroken, indexUnresolved int
	var broken []string
	for _, f := range idx.Files() {
		if !inScope(f) {
			continue
		}
		docs++
		for _, s := range idx.SymbolsByFile(f) {
			if s.Kind == symbol.KindSection {
				headings++
			}
			indexUnresolved += idx.UnresolvedOutgoing(s.ID).Unresolved
		}
		for _, r := range idx.ReferencesByFile(f) {
			switch r.Kind {
			case reference.KindResourceLink, reference.KindIDReference:
			default:
				continue
			}
			if strings.HasPrefix(r.NameQualified, "url:") {
				external++
				continue
			}
			local++
			if r.Kind == reference.KindIDReference {
				anchors++
			}
			if r.NameQualified != "" && declares(r.NameQualified) {
				continue
			}
			why := "missing"
			target := r.NameQualified
			switch {
			case target == "":
				why = "outside the repository"
				target = r.Name
			case r.Kind == reference.KindResourceLink:
				if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(target))); err == nil {
					why = "exists, no file declaration"
					if st.IsDir() {
						why = "a directory"
					}
				}
			default:
				anchorsBroken++
				file, _, _ := strings.Cut(target, "#")
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(file))); err == nil {
					why = "no such anchor"
				}
			}
			broken = append(broken, fmt.Sprintf("%s:%d %s %s (%s)", f, r.Location.Range.Start.Line, r.Kind, target, why))
		}
	}
	sort.Strings(broken)
	resolved := local - len(broken)
	t.Logf("documents %d, headings %d", docs, headings)
	t.Logf("local links %d, resolved %d (%.1f%%); anchor links %d, resolved %d; external %d", local, resolved, 100*float64(resolved)/float64(max(local, 1)), anchors, anchors-anchorsBroken, external)
	for _, b := range broken {
		t.Logf("broken: %s", b)
	}
	if indexUnresolved != len(broken) {
		t.Errorf("the index counts %d unresolved references, the listing %d", indexUnresolved, len(broken))
	}
}
