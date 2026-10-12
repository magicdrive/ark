package rust

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestOracle_Compiler compares the provider's resolutions with the Rust
// compiler's, reference by reference, on a fixed corpus. It is opt-in and not
// part of CI: docs/scripts/rust-oracle.sh prepares the oracle — rustc's
// save-analysis, an unstable interface (RUSTC_BOOTSTRAP=1, -Z save-analysis,
// rustc 1.69 or earlier; measured with 1.58.0) — from SHA-256-verified crate
// archives with no dependency, build script or procedural macro, compiled by
// rustc alone with an empty environment; it runs no crate code. Then
// ARK_RUST_ORACLE names the oracle directory and ARK_RUST_ORACLE_SRC its
// src/. The compiler resolves a reference as one configuration (the host,
// default features) compiles it, and records no reference in code that
// configuration leaves out, in a bound, a where clause or `Self::X`.
//
// Precision: of the references the provider resolves to one declaration (a
// graph edge), correct ones over correct plus wrong — correct when the
// compiler resolves the same span to a definition inside that declaration
// (or, for a braced enum variant, which save-analysis records as the enum,
// to the enum declaring it), wrong otherwise; unverified when the compiler
// recorded nothing at the span (not in either count). Recall: of the
// compiler's references to the crate's own items (locals and fields
// excluded) at spans the provider observes, method calls on values
// (unsupported by design) excluded, those it resolves to the compiler's
// definition; the rest are candidates (and whether they hold the compiler's
// definition), unresolved, or not observed (path prefixes, macro arguments,
// generic parameters: by kind). Precision holds for this corpus and
// configuration only.
func TestOracle_Compiler(t *testing.T) {
	dir, srcRoot := os.Getenv("ARK_RUST_ORACLE"), os.Getenv("ARK_RUST_ORACLE_SRC")
	if dir == "" || srcRoot == "" {
		t.Skip("ARK_RUST_ORACLE / ARK_RUST_ORACLE_SRC not set")
	}
	if v, err := os.ReadFile(filepath.Join(dir, "RUSTC_VERSION")); err == nil {
		t.Logf("oracle: %s", strings.TrimSpace(string(v)))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var total oracleCounts
	for _, e := range entries {
		jsons, _ := filepath.Glob(filepath.Join(dir, e.Name(), "save-analysis-temp", "*.json"))
		if !e.IsDir() || len(jsons) != 1 {
			continue
		}
		c := compareWithCompiler(t, filepath.Join(srcRoot, e.Name()), jsons[0])
		t.Logf("%-36s %s", e.Name(), c)
		total.add(c)
	}
	t.Logf("TOTAL %s", total)
	for _, w := range total.wrongSamples {
		t.Logf("WRONG %s", w)
	}
	if total.wrong > 0 {
		t.Errorf("%d resolutions disagree with the compiler", total.wrong)
	}
}

type oracleCounts struct {
	crates                            int
	edges, correct, wrong, unverified int
	unverifiedCompiled                int // on a line the compiler recorded something on
	// Recall, over the compiler's references to the crate's own items.
	compilerRefs, observed, resolved, candidate, unresolved, methodCalls, unobserved int
	candidateHit                                                                     int // a Candidate whose candidates hold the compiler's definition
	unobservedKinds                                                                  map[string]int
	wrongSamples                                                                     []string
}

func (c *oracleCounts) add(o oracleCounts) {
	c.crates++
	c.edges += o.edges
	c.correct += o.correct
	c.wrong += o.wrong
	c.unverified += o.unverified
	c.unverifiedCompiled += o.unverifiedCompiled
	c.compilerRefs += o.compilerRefs
	c.observed += o.observed
	c.resolved += o.resolved
	c.candidate += o.candidate
	c.candidateHit += o.candidateHit
	c.unresolved += o.unresolved
	c.methodCalls += o.methodCalls
	c.unobserved += o.unobserved
	if c.unobservedKinds == nil {
		c.unobservedKinds = map[string]int{}
	}
	for k, v := range o.unobservedKinds {
		c.unobservedKinds[k] += v
	}
	c.wrongSamples = append(c.wrongSamples, o.wrongSamples...)
}

func (c oracleCounts) String() string {
	precision, recall := 0.0, 0.0
	if c.correct+c.wrong > 0 {
		precision = 100 * float64(c.correct) / float64(c.correct+c.wrong)
	}
	if n := c.observed - c.methodCalls; n > 0 {
		recall = 100 * float64(c.resolved) / float64(n)
	}
	kinds := make([]string, 0, len(c.unobservedKinds))
	for k, v := range c.unobservedKinds {
		kinds = append(kinds, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(kinds)
	return fmt.Sprintf("edges=%d correct=%d wrong=%d unverified=%d (on compiled lines %d) precision=%.2f%% | compilerRefs=%d observed=%d resolvedCorrect=%d candidate=%d (holding the compiler's %d) unresolved=%d methodCalls=%d recall(observed, non-method)=%.1f%% unobserved=%d [%s]",
		c.edges, c.correct, c.wrong, c.unverified, c.unverifiedCompiled, precision, c.compilerRefs, c.observed, c.resolved, c.candidate, c.candidateHit, c.unresolved, c.methodCalls, recall, c.unobserved, strings.Join(kinds, " "))
}

type saSpan struct {
	File      string `json:"file_name"`
	ByteStart int    `json:"byte_start"`
	ByteEnd   int    `json:"byte_end"`
	LineEnd   uint32 `json:"line_end"`
	ColEnd    uint32 `json:"column_end"`
	LineStart uint32 `json:"line_start"`
	ColStart  uint32 `json:"column_start"`
}

type saID struct {
	Krate uint32 `json:"krate"`
	Index uint32 `json:"index"`
}

type saAnalysis struct {
	Defs []struct {
		Kind string `json:"kind"`
		ID   saID   `json:"id"`
		Name string `json:"name"`
		Qual string `json:"qualname"`
		Span saSpan `json:"span"`
	} `json:"defs"`
	Refs []struct {
		Kind  string `json:"kind"`
		Span  saSpan `json:"span"`
		RefID saID   `json:"ref_id"`
	} `json:"refs"`
	MacroRefs []struct {
		Span       saSpan `json:"span"`
		Qualname   string `json:"qualname"`
		CalleeSpan saSpan `json:"callee_span"`
	} `json:"macro_refs"`
}

type spanKey struct {
	file      string
	line, col uint32
}

func compareWithCompiler(t *testing.T, crateDir, analysis string) oracleCounts {
	t.Helper()
	data, err := os.ReadFile(analysis)
	if err != nil {
		t.Fatal(err)
	}
	var sa saAnalysis
	if err := json.Unmarshal(data, &sa); err != nil {
		t.Fatal(err)
	}
	rel := func(f string) (string, bool) {
		r, err := filepath.Rel(crateDir, f)
		if err != nil || strings.HasPrefix(r, "..") {
			return "", false
		}
		return filepath.ToSlash(r), true
	}

	// The crate as the provider sees it.
	p := NewProvider()
	var files []resolver.FileIndex
	refsAt := map[spanKey][]reference.Reference{}
	symbolsByID := map[symbol.SymbolID]symbol.Symbol{}
	filepath.Walk(filepath.Join(crateDir, "src"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".rs") {
			return nil
		}
		r, _ := rel(path)
		src, _ := os.ReadFile(path)
		ex, err := p.Extract(context.Background(), source.FileID(r), src)
		if err != nil {
			return nil
		}
		fi := index.NewFileIndex(p, source.FileID(r), ex)
		files = append(files, fi)
		for _, ref := range fi.References {
			k := spanKey{r, ref.Location.Range.End.Line, ref.Location.Range.End.Column}
			refsAt[k] = append(refsAt[k], ref)
		}
		for _, s := range fi.Symbols {
			symbolsByID[s.ID] = s
		}
		return nil
	})
	resolutions := map[reference.ReferenceID]resolver.Resolution{}
	for _, res := range resolver.New(files).Resolve() {
		resolutions[res.ReferenceID] = res
	}

	// The compiler's definitions of this crate, and its references.
	type def struct {
		kind, name, file string
		span             saSpan
	}
	defs := map[saID]def{}
	for _, d := range sa.Defs {
		if f, ok := rel(d.Span.File); ok {
			defs[d.ID] = def{d.Kind, d.Name, f, d.Span}
		}
	}
	type cref struct {
		kind  string
		local bool // to a definition of this crate
		def   def
	}
	compilerAt := map[spanKey][]cref{}
	// compiledLines are the lines the compiler recorded a reference or a
	// definition on: code the configuration compiled.
	compiledLines := map[spanKey]bool{}
	for _, d := range sa.Defs {
		if f, ok := rel(d.Span.File); ok {
			compiledLines[spanKey{f, d.Span.LineStart, 0}] = true
		}
	}
	for _, r := range sa.Refs {
		f, ok := rel(r.Span.File)
		if !ok {
			continue
		}
		d, local := defs[r.RefID]
		compiledLines[spanKey{f, r.Span.LineStart, 0}] = true
		k := spanKey{f, r.Span.LineEnd, r.Span.ColEnd}
		compilerAt[k] = append(compilerAt[k], cref{r.Kind, local, d})
	}
	for _, m := range sa.MacroRefs {
		f, ok := rel(m.Span.File)
		if !ok {
			continue
		}
		cf, local := rel(m.CalleeSpan.File)
		k := spanKey{f, m.Span.LineEnd, m.Span.ColEnd}
		compilerAt[k] = append(compilerAt[k], cref{"Macro", local, def{"Macro", lastSegment(m.Qualname), cf, m.CalleeSpan}})
	}

	inside := func(s symbol.Symbol, d def) bool {
		if string(s.Location.File) != d.file {
			return false
		}
		r := s.Location.Range
		start := source.Position{Line: d.span.LineStart, Column: d.span.ColStart}
		end := source.Position{Line: d.span.LineEnd, Column: d.span.ColEnd}
		return !before(start, r.Start) && !before(r.End, end)
	}

	// save-analysis records a braced enum variant (`E::V { .. }`, as an
	// expression or a pattern) as a reference to the enum E; the provider
	// resolves it to the variant, which the enum declares.
	enums := map[string]symbol.Symbol{}
	for _, s := range symbolsByID {
		if s.Kind == symbol.KindEnum {
			enums[s.Qualified] = s
		}
	}
	variantOf := func(target symbol.Symbol, d def) bool {
		e, ok := enums[target.ParentQualified]
		return ok && d.kind == "Enum" && target.Kind == symbol.KindConstant && inside(e, d)
	}

	var c oracleCounts
	c.unobservedKinds = map[string]int{}
	// Precision: every reference the provider resolves to one declaration.
	for k, refs := range refsAt {
		for _, ref := range refs {
			res, ok := resolutions[ref.ID]
			if !ok || !res.HasUniqueTarget() {
				continue
			}
			c.edges++
			target := symbolsByID[res.Candidates[0].SymbolID]
			crs := compilerAt[k]
			if len(crs) == 0 {
				c.unverified++
				if compiledLines[spanKey{k.file, k.line, 0}] {
					c.unverifiedCompiled++
				}
				dump(map[bool]string{true: "UNVERIFIED-COMPILED", false: "UNVERIFIED"}[compiledLines[spanKey{k.file, k.line, 0}]], fmt.Sprintf("%s %s:%d:%d %s -> %s", ref.Kind, k.file, k.line, k.col, ref.Name, target.Qualified))
				continue
			}
			match := false
			for _, cr := range crs {
				if cr.local && (inside(target, cr.def) || variantOf(target, cr.def)) {
					match = true
				}
			}
			if match {
				c.correct++
			} else {
				c.wrong++
				var got []string
				for _, cr := range crs {
					got = append(got, fmt.Sprintf("%s %s %s:%d (local %v)", cr.kind, cr.def.name, cr.def.file, cr.def.span.LineStart, cr.local))
				}
				c.wrongSamples = append(c.wrongSamples, fmt.Sprintf("%s:%d:%d %s -> %s (%s:%d); compiler: %s",
					k.file, k.line, k.col, ref.Name, target.Qualified, target.Location.File, target.Location.Range.Start.Line, strings.Join(got, "; ")))
			}
		}
	}
	// Recall: the compiler's references to this crate's items.
	for k, crs := range compilerAt {
		for _, cr := range crs {
			if !cr.local || cr.def.kind == "Local" || cr.def.kind == "Field" {
				continue
			}
			c.compilerRefs++
			refs := refsAt[k]
			if len(refs) == 0 {
				c.unobserved++
				c.unobservedKinds[cr.kind]++
				dump("UNOBSERVED", fmt.Sprintf("%s->%s %s:%d:%d %s", cr.kind, cr.def.kind, k.file, k.line, k.col, cr.def.name))
				continue
			}
			c.observed++
			ref := refs[0]
			if ref.ReceiverExpr != "" && ref.NameQualified == "" && !strings.Contains(ref.ReceiverExpr, "::") && cr.def.kind == "Method" {
				c.methodCalls++
				continue
			}
			res := resolutions[ref.ID]
			if !res.HasUniqueTarget() {
				dump("UNRESOLVED", fmt.Sprintf("%s->%s %s:%d:%d %s q=%q recv=%q conf=%s", ref.Kind, cr.def.kind, k.file, k.line, k.col, ref.Name, ref.NameQualified, ref.ReceiverExpr, res.Confidence))
			}
			switch {
			case res.HasUniqueTarget() && (inside(symbolsByID[res.Candidates[0].SymbolID], cr.def) || variantOf(symbolsByID[res.Candidates[0].SymbolID], cr.def)):
				c.resolved++
			case res.Confidence == resolver.ConfidenceCandidate:
				c.candidate++
				for _, cand := range res.Candidates {
					if t := symbolsByID[cand.SymbolID]; inside(t, cr.def) || variantOf(t, cr.def) {
						c.candidateHit++
						break
					}
				}
			default:
				c.unresolved++
			}
		}
	}
	return c
}

// dump writes one line of a category to $ARK_RUST_ORACLE_DUMP (manual
// review).
func dump(category, line string) {
	p := os.Getenv("ARK_RUST_ORACLE_DUMP")
	if p == "" {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\t%s\n", category, line)
}

func before(a, b source.Position) bool {
	return a.Line < b.Line || a.Line == b.Line && a.Column < b.Column
}

func lastSegment(q string) string {
	if i := strings.LastIndex(q, "::"); i >= 0 {
		return q[i+2:]
	}
	return q
}
