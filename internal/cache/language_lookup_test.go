package cache

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/language"
)

// TestFileStore_LanguageLookupRoundTrip: the language-lookup evidence
// (ReferenceDraft.Lookup and its companions, SymbolDraft.Parameters /
// ImplicitMembers / Overrides, Extraction.SourceRoot) survives the
// persistent cache losslessly.
func TestFileStore_LanguageLookupRoundTrip(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := NewCacheKey("src/p/A.java", []byte("x"), "0.1.0", "java-1")
	syms := []language.SymbolDraft{
		{Name: "A", Qualified: "p.A", Kind: "class", ImplicitMembers: "toString(),+<init>()"},
		{Name: "m", Qualified: "p.A.m", Kind: "method", Parent: "p.A", Parameters: "(int,?String...)", Overrides: true},
	}
	refs := []language.ReferenceDraft{
		{Name: "m", Kind: "call", IsCall: true, Lookup: "@M:p.A.m;@M:q.B.m|?", CountsArguments: true, ArgumentCount: 2, ArgumentTypes: "int,S"},
		{Name: "go", Kind: "call", IsCall: true, ReceiverExpr: "x", ReceiverType: "X", ReceiverLookup: "=T:p.X;=T:java.lang.X", CountsArguments: true},
	}
	entry := &CachedExtraction{Key: key, Language: "java", Symbols: syms, References: refs, IdentityOnly: true, Package: "p", SourceRoot: "src/", CachedAt: time.Now()}
	if err := store.Put(entry); err != nil {
		t.Fatal(err)
	}
	got, hit, err := store.Get(key)
	if err != nil || !hit {
		t.Fatalf("Get: hit=%v err=%v", hit, err)
	}
	if !reflect.DeepEqual(got.Symbols, syms) || !reflect.DeepEqual(got.References, refs) || got.SourceRoot != "src/" || got.Package != "p" {
		t.Errorf("evidence changed across the cache:\n got  %+v %+v %q\n want %+v %+v", got.Symbols, got.References, got.SourceRoot, syms, refs)
	}
}

// TestLanguageLookupFieldsOmittedWhenEmpty: providers that never state the
// language-lookup evidence serialize byte-identically to before it existed,
// so their entries stay valid without a schema bump.
func TestLanguageLookupFieldsOmittedWhenEmpty(t *testing.T) {
	for _, v := range []any{
		language.ReferenceDraft{Name: "Println", Kind: "call", ReceiverExpr: "fmt", IsCall: true},
		language.SymbolDraft{Name: "f", Qualified: "f", Kind: "function"},
		CachedExtraction{Language: "go"},
	} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"Lookup", "ArgumentCount", "CountsArguments", "ArgumentTypes", "Parameters", "ImplicitMembers", "Overrides", "sourceRoot"} {
			if strings.Contains(string(b), field) {
				t.Errorf("%T: empty %s must be omitted, got %s", v, field, b)
			}
		}
	}
}
