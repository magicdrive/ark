package yaml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestRealRepositories is an opt-in check of real repositories, read only:
// ARK_YAML_REAL_REPOS lists their roots (separated by the OS's path list
// separator). For each, the index (built without a cache, so nothing is
// written) must read every workflow file without a syntax error, state a
// workflow per file, a contains edge to every job, resolve every needs of a
// job the workflow declares Exactly and every local uses whose target the
// repository holds. With ARK_YAML_ORACLE_DIR, the structure of each
// workflow — triggers, jobs, needs, each step's uses in order — is written
// to <dir>/<repo>.ark.txt in the form docs/scripts/oracle/actions-psych.rb
// writes from Ruby's Psych (libyaml), to be compared byte for byte.
func TestRealRepositories(t *testing.T) {
	roots := os.Getenv("ARK_YAML_REAL_REPOS")
	if roots == "" {
		t.Skip("ARK_YAML_REAL_REPOS is not set")
	}
	for _, root := range filepath.SplitList(roots) {
		t.Run(filepath.Base(root), func(t *testing.T) {
			checkRealRepository(t, root)
		})
	}
}

func checkRealRepository(t *testing.T, root string) {
	idx, err := index.New(context.Background(), root, []language.Provider{NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	workflows, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	var lines []string
	for _, path := range workflows {
		rel, _ := filepath.Rel(root, path)
		file := source.FileID(filepath.ToSlash(rel))
		var wf *symbol.Symbol
		jobs := map[string]symbol.Symbol{}
		for _, s := range idx.SymbolsByFile(file) {
			switch s.Kind {
			case symbol.KindWorkflow:
				wf = &s
			case symbol.KindJob:
				jobs[s.Name] = s
			}
		}
		if wf == nil {
			t.Errorf("%s: no workflow declared", file)
			continue
		}
		for _, d := range idx.Diagnostics() {
			if d.Location.File == file && d.Severity == language.SeverityError {
				t.Errorf("%s: %s %s", file, d.Code, d.Message)
			}
		}
		contained := map[string]bool{}
		for _, e := range idx.GetCallees(wf.ID) {
			if e.Kind == index.EdgeContains {
				to, _ := idx.GetSymbol(e.To)
				contained[to.Qualified] = true
			}
		}
		for _, j := range jobs {
			if !contained[j.Qualified] {
				t.Errorf("%s: no contains edge to job %s", file, j.Name)
			}
			needs := map[string]string{}
			for _, e := range idx.GetCallees(j.ID) {
				to, _ := idx.GetSymbol(e.To)
				if e.Kind == index.EdgeDependsOn {
					needs[to.Name] = e.Confidence.String()
				}
			}
			for _, r := range idx.ReferencesByContainer(j.ID) {
				if r.Kind == "depends_on" && !r.Dynamic {
					if _, declared := jobs[r.Name]; declared && needs[r.Name] != "exact" {
						t.Errorf("%s: %s needs %s: %q, want an exact edge", file, j.Name, r.Name, needs[r.Name])
					}
				}
			}
		}
		// Every local uses whose target the repository holds is an edge.
		for _, s := range idx.SymbolsByFile(file) {
			for _, r := range idx.ReferencesByContainer(s.ID) {
				if r.Kind != "call" || !strings.HasPrefix(r.Name, "./") {
					continue
				}
				if len(idx.FindSymbolsByQualified(r.NameQualified)) == 1 && len(idx.GetCallees(s.ID)) == 0 {
					t.Errorf("%s: %s uses %s, a declaration of the repository, without an edge", file, s.Qualified, r.Name)
				}
			}
		}
		lines = append(lines, structureLines(t, root, string(file))...)
	}
	t.Logf("%s: %d workflow(s), %d structure line(s), %d file(s) indexed", root, len(workflows), len(lines), len(idx.Files()))
	if dir := os.Getenv("ARK_YAML_ORACLE_DIR"); dir != "" {
		out := filepath.Join(dir, filepath.Base(root)+".ark.txt")
		var text string
		for _, l := range lines {
			text += l + "\n"
		}
		if err := os.WriteFile(out, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// structureLines renders a workflow's structure as the oracle does:
// "trigger F E", "job F J", "needs F J N" (sorted within the job, as a set),
// "step F J I uses=U" with I the step's position from 0 and U "-" for none.
func structureLines(t *testing.T, root, file string) []string {
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
	if err != nil {
		t.Fatal(err)
	}
	ex, err := NewProvider().Extract(context.Background(), source.FileID(file), src)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	uses := map[string]string{}
	for _, r := range ex.References {
		if r.Kind == "call" {
			name := r.Name
			if r.NameQualified != "" && strings.HasPrefix(r.NameQualified, "remote:") {
				name = strings.TrimPrefix(r.NameQualified, "remote:")
			}
			uses[r.Container] = name
		}
	}
	needs := map[string][]string{}
	for _, r := range ex.References {
		if r.Kind == "depends_on" {
			needs[r.Container] = append(needs[r.Container], r.Name)
		}
	}
	steps := map[string]int{}
	for _, s := range ex.Symbols {
		switch s.Kind {
		case symbol.KindTrigger:
			out = append(out, fmt.Sprintf("trigger %s %s", file, s.Name))
		case symbol.KindJob:
			out = append(out, fmt.Sprintf("job %s %s", file, s.Name))
			n := slices.Clone(needs[s.Qualified])
			slices.Sort(n)
			for _, x := range slices.Compact(n) {
				out = append(out, fmt.Sprintf("needs %s %s %s", file, s.Name, x))
			}
		case symbol.KindStep:
			job := strings.TrimPrefix(s.Parent, file+"::")
			u := uses[s.Qualified]
			if u == "" {
				u = "-"
			}
			out = append(out, fmt.Sprintf("step %s %s %d uses=%s", file, job, steps[job], u))
			steps[job]++
		}
	}
	return out
}

// TestCorpus is an opt-in measurement over every .yaml and .yml file below
// the roots ARK_YAML_CORPUS lists, read only: extraction never fails, its
// ranges lie within the file, and the time and the files' problems are
// reported.
func TestCorpus(t *testing.T) {
	roots := os.Getenv("ARK_YAML_CORPUS")
	if roots == "" {
		t.Skip("ARK_YAML_CORPUS is not set")
	}
	p := NewProvider()
	var files, bytes, symbols, refs int
	var slowest time.Duration
	slowFile := ""
	codes := map[string]int{}
	start := time.Now()
	for _, root := range filepath.SplitList(roots) {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			if ext := strings.ToLower(filepath.Ext(path)); ext != ".yaml" && ext != ".yml" {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			t0 := time.Now()
			ex, err := p.Extract(context.Background(), source.FileID(filepath.ToSlash(rel)), src)
			if el := time.Since(t0); el > slowest {
				slowest, slowFile = el, path
			}
			if err != nil {
				t.Errorf("%s: %v", path, err)
			}
			for _, s := range ex.Symbols {
				if int(s.EndByte) > len(src) || s.StartByte > s.EndByte {
					t.Errorf("%s: %s out of range", path, s.Qualified)
				}
			}
			seen := map[string]bool{}
			if v := os.Getenv("ARK_YAML_CORPUS_SHOW"); v != "" {
				for _, d := range ex.Diagnostics {
					if slices.ContainsFunc(strings.Split(v, ","), func(p string) bool { return p != "" && strings.HasPrefix(d.Code, p) }) {
						t.Logf("%s %s:%d: %s", d.Code, path, d.Location.Range.Start.Line, d.Message)
					}
				}
			}
			for _, d := range ex.Diagnostics {
				if !seen[d.Code] {
					codes[d.Code]++
					seen[d.Code] = true
				}
			}
			files++
			bytes += len(src)
			symbols += len(ex.Symbols)
			refs += len(ex.References)
			return nil
		})
	}
	t.Logf("%d files, %d bytes, %d symbols, %d references in %v; slowest %v (%s); files with each diagnostic: %v",
		files, bytes, symbols, refs, time.Since(start).Round(time.Millisecond), slowest.Round(time.Microsecond), slowFile, codes)
}
