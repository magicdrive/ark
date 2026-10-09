package index

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

type fileDrafts struct {
	file   source.FileID
	drafts []language.SymbolDraft
}

func draft(file source.FileID, name string, kind symbol.SymbolKind, line uint32) language.SymbolDraft {
	return language.SymbolDraft{Name: name, Qualified: name, Kind: kind, Location: source.Location{File: file,
		Range: source.Range{Start: source.Position{Line: line, Column: 1}, End: source.Position{Line: line, Column: 9}}}}
}

// ingestAll feeds files to a fresh builder in the given order and returns its
// identity error report ("" for none).
func ingestAll(ids IDFunc, files []fileDrafts) string {
	b := newBuilder()
	b.ids = ids
	for _, f := range files {
		b.ingestExtraction(f.file, "fake", language.Extraction{Symbols: f.drafts})
	}
	err := b.identityError()
	if err == nil {
		return ""
	}
	var ce *IdentityCollisionError
	if !errors.As(err, &ce) {
		return "unexpected: " + err.Error()
	}
	return fmt.Sprintf("%s\n%+v", ce.Error(), ce.Diagnostics())
}

// The report depends on the declarations, never on the order files (or
// their drafts) are ingested in.
func TestIdentityError_IngestOrderIndependent(t *testing.T) {
	smallIDs := func(lang, path string, kind symbol.SymbolKind, qualified string, ordinal int) symbol.SymbolID {
		real := symbol.NewDeclarationID(lang, path, kind, qualified, ordinal)
		return symbol.SymbolID(real[:1]) // 16 possible IDs: frequent collisions
	}
	var files []fileDrafts
	for i, f := range []source.FileID{"a.fake", "b.fake", "c.fake"} {
		var ds []language.SymbolDraft
		for j, n := range []string{"alpha", "beta", "gamma", "delta", "eps", "zeta"} {
			ds = append(ds, draft(f, n, []symbol.SymbolKind{symbol.KindFunction, symbol.KindClass}[(i+j)%2], uint32(j+1)))
		}
		files = append(files, fileDrafts{f, ds})
	}
	want := ingestAll(smallIDs, files)
	if want == "" {
		t.Fatal("fixture produced no collision")
	}
	rev := slices.Clone(files)
	slices.Reverse(rev)
	for i := range rev {
		rev[i].drafts = slices.Clone(rev[i].drafts)
		slices.Reverse(rev[i].drafts)
	}
	if got := ingestAll(smallIDs, rev); got != want {
		t.Errorf("ingest order changed the report:\n got %s\nwant %s", got, want)
	}
	if got := ingestAll(symbol.NewDeclarationID, files); got != "" {
		t.Errorf("production IDs collide: %s", got)
	}
}

// FuzzIdentityCollision: with IDs drawn from a tiny space, the build fails
// exactly when two distinct declarations share an ID, and the report does
// not depend on the ingest order.
func FuzzIdentityCollision(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 5, 6}, uint8(3))
	f.Add([]byte{0, 0, 9, 9}, uint8(1))
	f.Fuzz(func(t *testing.T, layout []byte, space uint8) {
		if len(layout) > 30 {
			layout = layout[:30]
		}
		k := int(space%8) + 1
		ids := func(lang, path string, kind symbol.SymbolKind, qualified string, ordinal int) symbol.SymbolID {
			real := symbol.NewDeclarationID(lang, path, kind, qualified, ordinal)
			return symbol.SymbolID(fmt.Sprint(int(real[0]) % k))
		}
		var files []fileDrafts
		for i := 0; i+1 < len(layout); i += 2 {
			file := source.FileID(fmt.Sprintf("f%d.fake", layout[i]%3))
			d := draft(file, fmt.Sprintf("n%d", layout[i+1]%5), symbol.KindFunction, uint32(layout[i]%7)+1)
			files = append(files, fileDrafts{file, []language.SymbolDraft{d}})
		}
		// Expected: some ID carried by two distinct declarations, after the
		// per-file conversion (exact duplicates are one declaration).
		b := newBuilder()
		b.ids = ids
		for _, fd := range files {
			b.ingestExtraction(fd.file, "fake", language.Extraction{Symbols: fd.drafts})
		}
		seen := map[symbol.SymbolID]symbol.Symbol{}
		collide := false
		for _, syms := range b.symbolsByFile {
			for _, s := range syms {
				if p, ok := seen[s.ID]; ok && p != s {
					collide = true
				}
				seen[s.ID] = s
			}
		}
		got := ingestAll(ids, files)
		if (got != "") != collide {
			t.Fatalf("collision %v, report %q", collide, got)
		}
		rev := slices.Clone(files)
		slices.Reverse(rev)
		if again := ingestAll(ids, rev); again != got {
			t.Fatalf("ingest order changed the report")
		}
	})
}
