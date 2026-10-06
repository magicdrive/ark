package cache

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/language"
)

// TestFileStore_QualifiedIdentityRoundTrip: provider-determined qualified
// identity evidence survives the persistent cache losslessly.
func TestFileStore_QualifiedIdentityRoundTrip(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := NewCacheKey("app/C.php", []byte("x"), "0.1.0", "php-7")
	refs := []language.ReferenceDraft{
		{Name: "make", Kind: "call", ReceiverExpr: "Bar", ReceiverType: "Bar", ReceiverTypeQualified: `App\Services\Foo`, IsCall: true},
		{Name: "Foo", Kind: "construction", NameQualified: `App\Services\Foo`},
	}
	if err := store.Put(&CachedExtraction{Key: key, Language: "php", References: refs, CachedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, hit, err := store.Get(key)
	if err != nil || !hit {
		t.Fatalf("Get: hit=%v err=%v", hit, err)
	}
	if !reflect.DeepEqual(got.References, refs) {
		t.Errorf("references changed across the cache:\n got  %+v\n want %+v", got.References, refs)
	}
}

// TestQualifiedIdentityFieldsOmittedWhenEmpty: providers that never set the
// qualified identity fields serialize byte-identically to before they existed,
// so existing cache entries stay valid and need no schema-version bump.
func TestQualifiedIdentityFieldsOmittedWhenEmpty(t *testing.T) {
	b, err := json.Marshal(language.ReferenceDraft{Name: "Println", Kind: "call", ReceiverExpr: "fmt", IsCall: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "Qualified") {
		t.Errorf("empty qualified identity fields must be omitted, got %s", b)
	}
	// An entry written before the fields existed decodes with them empty.
	var d language.ReferenceDraft
	if err := json.Unmarshal([]byte(`{"Name":"Save","Kind":"call","Container":"","Location":{},"ReceiverExpr":"repo","IsCall":true}`), &d); err != nil {
		t.Fatal(err)
	}
	if d.NameQualified != "" || d.ReceiverTypeQualified != "" {
		t.Errorf("pre-existing cache entry must decode with empty qualified fields, got %+v", d)
	}
}
