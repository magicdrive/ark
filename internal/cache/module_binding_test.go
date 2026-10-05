package cache

import (
	"reflect"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/language"
)

// TestFileStore_ModuleBindingRoundTrip: schema v2 persists module-binding
// evidence and ReceiverType losslessly.
func TestFileStore_ModuleBindingRoundTrip(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mod := language.ModuleSpec{Specifier: "./user", Candidates: []language.ModuleCandidate{
		{File: "src/user.ts"}, {File: "src/user.tsx", Priority: 1},
	}}
	key := NewCacheKey("src/a.ts", []byte("x"), "0.1.0", "pv")
	entry := &CachedExtraction{
		Key:        key,
		Language:   "typescript",
		References: []language.ReferenceDraft{{Name: "save", Kind: "call", ReceiverExpr: "repo", ReceiverType: "UserRepository"}},
		Bindings: []language.BindingDraft{
			{Local: "DomainUser", Kind: language.BindingNamed, Module: mod, Imported: "User", TypeOnly: true},
			{Local: "users", Kind: language.BindingNamespace, Module: language.ModuleSpec{Specifier: "zod"}},
		},
		Exports: []language.ExportDraft{
			{Kind: language.ExportAll, Module: mod, Except: []string{"default"}},
			{Kind: language.ExportLocal, Exported: "default", Local: "User"},
		},
		ModuleScoped: true,
		CachedAt:     time.Now(),
	}
	if err := store.Put(entry); err != nil {
		t.Fatal(err)
	}
	got, hit, err := store.Get(key)
	if err != nil || !hit {
		t.Fatalf("Get: hit=%v err=%v", hit, err)
	}
	if !reflect.DeepEqual(got.Bindings, entry.Bindings) {
		t.Errorf("bindings: got %+v want %+v", got.Bindings, entry.Bindings)
	}
	if !reflect.DeepEqual(got.Exports, entry.Exports) {
		t.Errorf("exports: got %+v want %+v", got.Exports, entry.Exports)
	}
	if !got.ModuleScoped || got.References[0].ReceiverType != "UserRepository" {
		t.Errorf("ModuleScoped/ReceiverType lost: %+v", got)
	}
}

// TestSchemaV1EntriesAreIncompatible: entries written before module-binding
// evidence existed must miss, never be read back with silently-empty bindings.
func TestSchemaV1EntriesAreIncompatible(t *testing.T) {
	if CurrentSchemaVersion == "1" {
		t.Fatal("schema must be bumped past v1 for module-binding evidence")
	}
	k := NewCacheKey("a.ts", []byte("x"), "0.1.0", "pv")
	k.SchemaVersion = "1"
	if IsCompatible(k) {
		t.Error("schema v1 entry must be incompatible")
	}
}
