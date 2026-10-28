package source

import "testing"

// Paths a case- and normalization-insensitive file system opens as one
// fold alike; others do not.
func TestFoldPath(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		same bool
	}{
		{"Src/Main.zig", "src/main.zig", true},
		{"café/x.zig", "café/x.zig", true}, // NFC and NFD
		{"Σ/x.zig", "ς/x.zig", true},        // Σ and final ς
		{"K.zig", "k.zig", true},            // Kelvin sign
		{"a/x.zig", "b/x.zig", false},
	} {
		if got := FoldPath(tc.a) == FoldPath(tc.b); got != tc.same {
			t.Errorf("FoldPath(%q) == FoldPath(%q): %v, want %v", tc.a, tc.b, got, tc.same)
		}
	}
}
