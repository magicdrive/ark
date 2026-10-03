package php_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/php"
	"github.com/magicdrive/ark/internal/symbol"
)

func testdataDir(t *testing.T, name string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

// TestPHP_ContainmentThroughIndex is the PHP-3 acceptance criterion: PHP nested
// members flow through SymbolDraft.Parent into the repository index and produce
// the correct symbol.ParentQualified — with NO PHP-specific logic in the
// builder/resolver.
func TestPHP_ContainmentThroughIndex(t *testing.T) {
	dir := testdataDir(t, "intsvc")
	idx, err := index.New(context.Background(), dir, []language.Provider{php.NewProvider()})
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}

	const cls = "App\\Service\\UserService"
	checks := []struct {
		qualified   string
		kind        symbol.SymbolKind
		wantParentQ string
	}{
		{cls, symbol.KindClass, ""},
		{cls + ".find", symbol.KindMethod, cls},
		{cls + ".repository", symbol.KindProperty, cls},
		{cls + ".DEFAULT_LIMIT", symbol.KindConstant, cls},
		{cls + ".__construct", symbol.KindConstructor, cls},
		{cls + ".logger", symbol.KindProperty, cls}, // promoted
	}
	for _, c := range checks {
		syms := idx.FindSymbolsByQualified(c.qualified)
		if len(syms) == 0 {
			t.Errorf("indexed symbol %q not found", c.qualified)
			continue
		}
		s := syms[0]
		if s.Kind != c.kind {
			t.Errorf("%q: kind=%q want %q", c.qualified, s.Kind, c.kind)
		}
		if s.ParentQualified != c.wantParentQ {
			t.Errorf("%q: ParentQualified=%q want %q", c.qualified, s.ParentQualified, c.wantParentQ)
		}
	}

	// The method symbol must also carry Receiver=UserService (bare type name),
	// so PHP-5 receiver matching can resolve `UserService::...` / `$obj->...`.
	if syms := idx.FindSymbolsByQualified(cls + ".find"); len(syms) > 0 {
		if syms[0].Receiver != "UserService" {
			t.Errorf("find Receiver=%q want UserService", syms[0].Receiver)
		}
	}
}
