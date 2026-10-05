package index_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// tableProvider returns a fixed Extraction per FileID, so the real
// index → resolver → graph pipeline can be driven with module-binding
// evidence before any production provider emits it.
type tableProvider struct {
	byFile map[source.FileID]language.Extraction
}

func (tableProvider) Language() language.Language { return "mb" }
func (tableProvider) Extensions() []string        { return []string{".mb"} }
func (tableProvider) CacheVersion() string        { return "mb-1" }
func (p tableProvider) Extract(_ context.Context, f source.FileID, _ []byte) (language.Extraction, error) {
	return p.byFile[f], nil
}

func mbLoc(f string, line uint32) source.Location {
	return source.Location{File: source.FileID(f), Range: source.Range{
		Start: source.Position{Line: line, Column: 1}, End: source.Position{Line: line, Column: 9}}}
}

func mbSym(f, qualified string, kind symbol.SymbolKind, parent, recv string, line uint32) language.SymbolDraft {
	name := qualified
	if i := strings.LastIndex(qualified, "."); i >= 0 {
		name = qualified[i+1:]
	}
	return language.SymbolDraft{Name: name, Qualified: qualified, Kind: kind, Parent: parent, Receiver: recv,
		Location: mbLoc(f, line), StartByte: 0, EndByte: 1}
}

func mbMod(spec string, files ...string) language.ModuleSpec {
	m := language.ModuleSpec{Specifier: spec}
	for _, f := range files {
		m.Candidates = append(m.Candidates, language.ModuleCandidate{File: source.FileID(f)})
	}
	return m
}

func moduleBindingRepo() tableProvider {
	user, dIdx, idx, app := "domain/user.mb", "domain/index.mb", "index.mb", "app.mb"
	return tableProvider{byFile: map[source.FileID]language.Extraction{
		source.FileID(user): {
			ModuleScoped: true,
			Symbols: []language.SymbolDraft{
				mbSym(user, "Account", symbol.KindClass, "", "", 1),
				mbSym(user, "Account.open", symbol.KindMethod, "Account", "Account", 2),
			},
			Exports: []language.ExportDraft{{Kind: language.ExportLocal, Exported: "Account", Local: "Account", Location: mbLoc(user, 1)}},
		},
		source.FileID(dIdx): {
			ModuleScoped: true,
			Exports: []language.ExportDraft{{Kind: language.ExportFrom, Exported: "Account", Local: "Account",
				Module: mbMod("./user", user), Location: mbLoc(dIdx, 1)}},
		},
		source.FileID(idx): {
			ModuleScoped: true,
			Exports: []language.ExportDraft{{Kind: language.ExportAll, Module: mbMod("./domain", "domain.mb", dIdx),
				Except: []string{"default"}, Location: mbLoc(idx, 1)}},
		},
		source.FileID(app): {
			ModuleScoped: true,
			Symbols:      []language.SymbolDraft{mbSym(app, "run", symbol.KindFunction, "", "", 1)},
			Bindings: []language.BindingDraft{
				{Local: "Account", Kind: language.BindingNamed, Imported: "Account", Module: mbMod("./index", idx), Location: mbLoc(app, 1)},
				{Local: "z", Kind: language.BindingNamed, Imported: "z", Module: language.ModuleSpec{Specifier: "zod"}, Location: mbLoc(app, 1)},
			},
			References: []language.ReferenceDraft{
				{Name: "Account", Kind: "construction", Container: "run", Location: mbLoc(app, 3)},
				{Name: "open", Kind: "call", Container: "run", ReceiverExpr: "a", ReceiverType: "Account", IsCall: true, Location: mbLoc(app, 4)},
				{Name: "open", Kind: "call", Container: "run", ReceiverExpr: "x", IsCall: true, Location: mbLoc(app, 5)},
				{Name: "z", Kind: "call", Container: "run", IsCall: true, Location: mbLoc(app, 6)},
			},
		},
	}}
}

func edgeLines(idx *index.RepositoryIndex) []string {
	var out []string
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			for _, e := range idx.GetCallees(s.ID) {
				to, _ := idx.GetSymbol(e.To)
				out = append(out, fmt.Sprintf("%s:%s -%s-> %s:%s %s", f, s.Qualified, e.Kind, to.Location.File, to.Qualified, e.Confidence))
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestModuleBinding_EndToEnd drives bindings, a barrel chain, declared
// receiver types and untyped receivers through the real index pipeline, with
// and without the cache (cold and warm), and requires identical edges.
func TestModuleBinding_EndToEnd(t *testing.T) {
	p := moduleBindingRepo()
	files := map[string]string{}
	for f := range p.byFile {
		files[string(f)] = "x"
	}
	root := writeFiles(t, files)

	want := []string{
		"app.mb:run -calls-> domain/user.mb:Account exact",
		"app.mb:run -calls-> domain/user.mb:Account.open exact",
	}
	check := func(label string, idx *index.RepositoryIndex) {
		t.Helper()
		got := edgeLines(idx)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s edges:\n%s\nwant:\n%s", label, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}

	idx, err := index.New(context.Background(), root, []language.Provider{p})
	if err != nil {
		t.Fatal(err)
	}
	check("index.New", idx)

	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cold, err := index.NewWithCache(context.Background(), root, []language.Provider{p}, store)
	if err != nil {
		t.Fatal(err)
	}
	check("cache cold", cold)
	// Warm run with a provider that extracts nothing (same CacheVersion): the
	// edges can only come from the persisted bindings/exports/ReceiverType.
	warm, err := index.NewWithCache(context.Background(), root, []language.Provider{tableProvider{}}, store)
	if err != nil {
		t.Fatal(err)
	}
	check("cache warm", warm)
}
