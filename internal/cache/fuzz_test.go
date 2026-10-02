package cache

import (
	"os"
	"testing"
)

// FuzzCacheDecoding verifies that decoding arbitrary bytes from a cache file
// never panics — it must either return a valid entry or a graceful miss.
func FuzzCacheDecoding(f *testing.F) {
	// Seed: valid JSON, empty, corrupt, partial.
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"key":{"schemaVersion":"1"}}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte(``))
	f.Add([]byte(`{"key":{"filePath":"a.go","contentHash":"abc","arkVersion":"0.1.0","schemaVersion":"1","providerVersion":"1"},"language":"go","symbols":[]}`))
	f.Add([]byte{0x00, 0x01, 0x02, 0xff})

	dir := f.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		f.Fatalf("NewFileStore: %v", err)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Compute the path where the store would look for this key.
		key := NewCacheKey("fuzz_file.go", []byte("content"), "0.1.0", "pv1")
		p, pathErr := store.path(key)
		if pathErr != nil {
			return
		}

		// Write arbitrary bytes directly, bypassing store.Put marshaling.
		if writeErr := os.WriteFile(p, data, 0o600); writeErr != nil {
			return
		}
		defer os.Remove(p)

		// Must never panic regardless of content.
		_, _, _ = store.Get(key)
	})
}
