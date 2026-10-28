package zig

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
)

func extract(t testing.TB, file, src string) language.Extraction {
	t.Helper()
	ex, err := NewProvider().Extract(context.Background(), source.FileID(file), []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !ex.IdentityOnly {
		t.Fatal("a Zig file must be IdentityOnly")
	}
	return ex
}

// symbolLines renders each declaration as "kind qualified".
func symbolLines(ex language.Extraction) []string {
	var out []string
	for _, s := range ex.Symbols {
		out = append(out, fmt.Sprintf("%s %s", s.Kind, s.Qualified))
	}
	return out
}

// refLine renders a reference as "line kind name -> identity" ("?" without
// identity; "~" marks a speculative reference, "?" a tentative identity,
// "!" a dynamic one).
func refLine(r language.ReferenceDraft) string {
	q := r.NameQualified
	if q == "" {
		q = "?"
	}
	s := fmt.Sprintf("%d %s %s -> %s", r.Location.Range.Start.Line, r.Kind, r.Name, q)
	if r.Speculative {
		s += " ~"
	}
	if r.TentativeIdentity {
		s += " ?"
	}
	if r.Dynamic {
		s += " !"
	}
	return s
}

func refLines(ex language.Extraction) []string {
	var out []string
	for _, r := range ex.References {
		out = append(out, refLine(r))
	}
	return out
}

func equalLines(t testing.TB, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got:\n  %s\n want:\n  %s", what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// resolveRepo extracts and resolves a repository of files: for each
// reference "file:line name kind", its "confidence target|target"
// ("confidence outside" when placed outside the repository). A speculative
// reference the index would drop (no candidate) is left out.
func resolveRepo(t testing.TB, files map[string]string) map[string]string {
	t.Helper()
	p := NewProvider()
	var paths []string
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var fis []resolver.FileIndex
	for _, path := range paths {
		ex, err := p.Extract(context.Background(), source.FileID(path), []byte(files[path]))
		if err != nil {
			t.Fatal(err)
		}
		fis = append(fis, index.NewFileIndex(p, source.FileID(path), ex))
	}
	refs := map[reference.ReferenceID]reference.Reference{}
	for _, fi := range fis {
		for _, ref := range fi.References {
			refs[ref.ID] = ref
		}
	}
	out := map[string]string{}
	for _, res := range resolver.New(fis).Resolve() {
		ref := refs[res.ReferenceID]
		if ref.Speculative && len(res.Candidates) == 0 {
			continue
		}
		key := fmt.Sprintf("%s:%d %s %s", ref.Location.File, ref.Location.Range.Start.Line, ref.Name, ref.Kind)
		var targets []string
		for _, c := range res.Candidates {
			targets = append(targets, c.Qualified)
		}
		sort.Strings(targets)
		v := res.Confidence.String() + " " + strings.Join(targets, "|")
		if res.OutsideRepository {
			v = res.Confidence.String() + " outside"
		}
		if prev, dup := out[key]; dup {
			v = prev + " ; " + v
		}
		out[key] = strings.TrimSpace(v)
	}
	return out
}

// expectResolutions asserts each listed reference's resolution.
func expectResolutions(t testing.TB, got map[string]string, want map[string]string) {
	t.Helper()
	var keys []string
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if got[k] != want[k] {
			t.Errorf("%s: got %q, want %q", k, got[k], want[k])
		}
	}
	if t.Failed() {
		var all []string
		for k, v := range got {
			all = append(all, k+" => "+v)
		}
		sort.Strings(all)
		t.Logf("all resolutions:\n  %s", strings.Join(all, "\n  "))
	}
}

// noStrongTo asserts that no reference named name resolves to a target at
// Strong or Exact.
func noStrongTo(t testing.TB, got map[string]string, name string) {
	t.Helper()
	for k, v := range got {
		if strings.Contains(k, " "+name+" ") && (strings.HasPrefix(v, "exact") || strings.HasPrefix(v, "strong")) {
			t.Errorf("%s resolved %s; no target is proven", k, v)
		}
	}
}
