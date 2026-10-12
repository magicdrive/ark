package rust

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestOracle_Workspace compares, on the workspace fixture (testdata/repo:
// a library, a binary, a single-file binary, an integration test, an
// example and a second package), the provider's resolutions with rustc's,
// each crate compiled on its own with save-analysis. It is opt-in like
// TestOracle_Compiler: ARK_RUST_WORKSPACE_ORACLE names a directory holding
// <crate>/save-analysis-temp/*.json, made from this fixture with rustc ≤ 1.69
// (RUSTC_BOOTSTRAP=1, --edition 2018 -Z save-analysis), e.g. for the binary:
//
//	rustc --crate-type bin --extern app=libapp.rlib -Z save-analysis src/main.rs
//
// A reference the compiler resolves into another crate of the workspace
// (`app::open`, `util::...`) must have no edge: crates are separate identity
// spaces and the crate's name is in Cargo.toml. A reference within its crate
// is checked as in TestOracle_Compiler.
func TestOracle_Workspace(t *testing.T) {
	dir := os.Getenv("ARK_RUST_WORKSPACE_ORACLE")
	if dir == "" {
		t.Skip("ARK_RUST_WORKSPACE_ORACLE not set")
	}
	root, _ := filepath.Abs(fixtureRepo)
	idx := buildIndex(t, fixtureRepo)
	// The index's references and their resolutions, by span end.
	type resolvedRef struct {
		ref    reference.Reference
		target symbol.Symbol
		unique bool
	}
	refsAt := map[spanKey][]resolvedRef{}
	for _, f := range idx.Files() {
		for _, r := range idx.ReferencesByFile(f) {
			k := spanKey{string(f), r.Location.Range.End.Line, r.Location.Range.End.Column}
			refsAt[k] = append(refsAt[k], resolvedRef{ref: r})
		}
	}
	for _, s := range idx.FindSymbols("") {
		for _, r := range idx.ReferencesByTarget(s.ID) {
			for _, f := range idx.Files() {
				for _, fr := range idx.ReferencesByFile(f) {
					if fr.ID == r.ID {
						k := spanKey{string(f), fr.Location.Range.End.Line, fr.Location.Range.End.Column}
						for i := range refsAt[k] {
							if refsAt[k][i].ref.ID == r.ID {
								refsAt[k][i].target, refsAt[k][i].unique = s, true
							}
						}
					}
				}
			}
		}
	}
	jsons, _ := filepath.Glob(filepath.Join(dir, "*", "save-analysis-temp", "*.json"))
	if len(jsons) == 0 {
		t.Fatal("no analysis")
	}
	var crossing, crossingEdges, local, localCorrect, localWrong int
	for _, j := range jsons {
		data, _ := os.ReadFile(j)
		var sa saAnalysis
		if err := json.Unmarshal(data, &sa); err != nil {
			t.Fatal(err)
		}
		defs := map[saID]saSpan{}
		for _, d := range sa.Defs {
			defs[d.ID] = d.Span
		}
		for _, r := range sa.Refs {
			f, err := filepath.Rel(root, r.Span.File)
			if err != nil || strings.HasPrefix(f, "..") {
				continue
			}
			k := spanKey{filepath.ToSlash(f), r.Span.LineEnd, r.Span.ColEnd}
			refs := refsAt[k]
			if r.RefID.Krate != 0 {
				// Another crate: the workspace's own (app, util) or std.
				crossing++
				for _, rr := range refs {
					if rr.unique {
						crossingEdges++
						t.Errorf("%s:%d %s resolved to %s, but the compiler resolves it into another crate", k.file, k.line, rr.ref.Name, rr.target.Qualified)
					}
				}
				continue
			}
			d, ok := defs[r.RefID]
			if !ok || len(refs) == 0 || !refs[0].unique {
				continue
			}
			local++
			df, _ := filepath.Rel(root, d.File)
			tg := refs[0].target
			if string(tg.Location.File) == filepath.ToSlash(df) && !before(source.Position{Line: d.LineStart, Column: d.ColStart}, tg.Location.Range.Start) && !before(tg.Location.Range.End, source.Position{Line: d.LineEnd, Column: d.ColEnd}) {
				localCorrect++
			} else {
				localWrong++
				t.Errorf("%s:%d %s -> %s; compiler %s:%d", k.file, k.line, refs[0].ref.Name, tg.Qualified, df, d.LineStart)
			}
		}
	}
	t.Logf("%s", fmt.Sprintf("crates=%d crossCrateRefs=%d crossCrateEdges=%d | inCrateResolved=%d correct=%d wrong=%d", len(jsons), crossing, crossingEdges, local, localCorrect, localWrong))
}
