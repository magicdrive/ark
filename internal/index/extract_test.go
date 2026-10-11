package index_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// panicProvider panics extracting files named boom*.
type panicProvider struct{ fakeProvider }

func (panicProvider) Extract(_ context.Context, f source.FileID, _ []byte) (language.Extraction, error) {
	if strings.HasPrefix(filepath.Base(string(f)), "boom") {
		panic("provider panic on " + string(f))
	}
	return language.Extraction{Diagnostics: []language.Diagnostic{{Severity: language.SeverityWarning, Message: "seen " + string(f), Location: source.Location{File: f}}}}, nil
}

// A provider's panic during a build with parallel extraction reaches the
// building goroutine, with its value — as in a sequential build, where the
// index cache recovers it — and leaves no worker running; files are
// ingested in walk order whatever order their extractions finish in.
func TestParallelExtraction_PanicAndOrder(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 64; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.fake", i)), []byte(strings.Repeat("x", 64-i)), 0o644)
	}
	idx, err := index.NewWithCacheExcluding(context.Background(), dir, []language.Provider{panicProvider{}}, cache.NopStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ds := idx.Diagnostics()
	if len(ds) != 64 {
		t.Fatalf("%d diagnostics, want 64", len(ds))
	}
	for i, d := range ds {
		if want := fmt.Sprintf("seen f%02d.fake", i); d.Message != want {
			t.Fatalf("diagnostic %d is %q, want %q (walk order)", i, d.Message, want)
		}
	}

	os.WriteFile(filepath.Join(dir, "boom.fake"), []byte("x"), 0o644)
	before := runtime.NumGoroutine()
	func() {
		defer func() {
			r := recover()
			if r == nil || !strings.Contains(fmt.Sprint(r), "provider panic on boom.fake") {
				t.Errorf("recovered %v, want the provider's panic", r)
			}
		}()
		index.NewWithCacheExcluding(context.Background(), dir, []language.Provider{panicProvider{}}, cache.NopStore{}, nil)
		t.Error("the build returned instead of panicking")
	}()
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("goroutines: %d before the build, %d after its panic", before, after)
	}
}

// Files larger than the bytes a build extracts at once run one at a time —
// the build neither stalls nor reorders them.
func TestParallelExtraction_LargeFiles(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 6; i++ {
		size := 1 << 10
		if i%2 == 0 {
			size = 1 << 20 // over the in-flight budget
		}
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.fake", i)), []byte(strings.Repeat("x", size)), 0o644)
	}
	idx, err := index.NewWithCacheExcluding(context.Background(), dir, []language.Provider{panicProvider{}}, cache.NopStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ds := idx.Diagnostics()
	for i, d := range ds {
		if want := fmt.Sprintf("seen f%02d.fake", i); d.Message != want {
			t.Fatalf("diagnostic %d is %q, want %q", i, d.Message, want)
		}
	}
	if len(ds) != 6 {
		t.Fatalf("%d diagnostics, want 6", len(ds))
	}
}
