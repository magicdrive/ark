package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// fakeProvider emits a fixed Extraction regardless of source, so the test can
// inject references with arbitrary ReferenceKinds (including unknown ones) and
// verify the Graph Builder's kind→EdgeKind mapping.
type fakeProvider struct {
	symbols []language.SymbolDraft
	refs    []language.ReferenceDraft
}

func (fakeProvider) Language() language.Language { return "fake" }
func (fakeProvider) Extensions() []string        { return []string{".fake"} }
func (fakeProvider) CacheVersion() string        { return "t" }
func (p fakeProvider) Extract(_ context.Context, _ source.FileID, _ []byte) (language.Extraction, error) {
	return language.Extraction{Symbols: p.symbols, References: p.refs}, nil
}

func sym(name string) language.SymbolDraft {
	return language.SymbolDraft{
		Name: name, Qualified: name, Kind: symbol.KindClass,
		Location:  source.Location{File: "x.fake", Range: source.Range{Start: source.Position{Line: 1, Column: 1}, End: source.Position{Line: 1, Column: 2}}},
		StartByte: 0, EndByte: 1, Exported: true,
	}
}

func ref(name, kind string) language.ReferenceDraft {
	return language.ReferenceDraft{
		Name: name, Kind: kind, Container: "Caller",
		Location: source.Location{File: "x.fake", Range: source.Range{Start: source.Position{Line: 2, Column: 1}, End: source.Position{Line: 2, Column: 2}}},
	}
}

func buildFakeIndex(t *testing.T, p fakeProvider) *index.RepositoryIndex {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.fake"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := index.New(context.Background(), dir, []language.Provider{p})
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx
}

// edgeKindTo returns the EdgeKind of the edge from Caller to the named target,
// or "" if there is none.
func edgeKindTo(idx *index.RepositoryIndex, target string) index.EdgeKind {
	callers := idx.FindSymbolsByQualified("Caller")
	if len(callers) == 0 {
		return ""
	}
	for _, e := range idx.GetCallees(callers[0].ID) {
		if to, ok := idx.GetSymbol(e.To); ok && to.Qualified == target {
			return e.Kind
		}
	}
	return ""
}

func TestGraphBuilder_KindMapping(t *testing.T) {
	p := fakeProvider{
		symbols: []language.SymbolDraft{
			sym("Caller"),
			sym("TCall"), sym("TCtor"), sym("TType"),
			sym("TExtends"), sym("TImpl"), sym("TTrait"),
			sym("TRead"), sym("TUnknown"),
			sym("TValue"), sym("TDepends"),
		},
		refs: []language.ReferenceDraft{
			ref("TCall", "call"),
			ref("TCtor", "construction"),
			ref("TType", "type_use"),
			ref("TExtends", "inheritance"),
			ref("TImpl", "implementation"),
			ref("TTrait", "uses_trait"),
			ref("TValue", "value_reference"),
			ref("TDepends", "depends_on"),
			ref("TRead", "read"),              // not mapped → no edge
			ref("TUnknown", "future_unknown"), // unknown → no edge
		},
	}
	idx := buildFakeIndex(t, p)

	// Known mappings (existing + new typed relations).
	want := map[string]index.EdgeKind{
		"TCall":    index.EdgeCalls,
		"TCtor":    index.EdgeCalls, // construction is explicitly call-like
		"TType":    index.EdgeUsesType,
		"TExtends": index.EdgeExtends,
		"TImpl":    index.EdgeImplements,
		"TTrait":   index.EdgeUsesTrait,
		// Dependencies of configuration languages are never calls.
		"TValue":   index.EdgeReferences,
		"TDepends": index.EdgeDependsOn,
	}
	for target, wantKind := range want {
		if got := edgeKindTo(idx, target); got != wantKind {
			t.Errorf("edge Caller->%s = %q, want %q", target, got, wantKind)
		}
	}

	// Unmapped / unknown kinds MUST NOT fabricate an edge.
	for _, target := range []string{"TRead", "TUnknown"} {
		if got := edgeKindTo(idx, target); got != "" {
			t.Errorf("edge Caller->%s = %q, want NONE (unknown/unmapped kinds must not fabricate edges)", target, got)
		}
	}
}
