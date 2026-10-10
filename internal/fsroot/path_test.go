package fsroot

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

// Paths are root-relative, cleaned lexically as Ark's gate cleans them, and
// never absolute or leaving the root.
func TestSplitRel(t *testing.T) {
	for _, tc := range []struct {
		in, clean string
		err       error
	}{
		{"a/b", "a/b", nil},
		{"./a//b/", "a/b", nil},
		{"a/../b", "b", nil},
		{".", ".", nil},
		{"a/..", ".", nil},
		{"", "", ErrInvalidPath},
		{"..", "", ErrOutsideRoot},
		{"../a", "", ErrOutsideRoot},
		{"a/../../b", "", ErrOutsideRoot},
		{"/etc/passwd", "", ErrInvalidPath},
		{"a\x00b", "", ErrInvalidPath},
	} {
		clean, comps, err := splitRel(tc.in)
		if !errors.Is(err, tc.err) || (err == nil && clean != tc.clean) {
			t.Errorf("splitRel(%q) = %q %v, %v; want %q %v", tc.in, clean, comps, err, tc.clean, tc.err)
		}
		if err == nil && clean != "." && strings.Join(comps, "/") != clean {
			t.Errorf("splitRel(%q): components %v", tc.in, comps)
		}
	}
	if runtime.GOOS != "windows" {
		// A backslash is an ordinary file name character on Unix.
		if clean, _, err := splitRel(`a\b`); err != nil || clean != `a\b` {
			t.Errorf(`splitRel("a\\b") = %q %v`, clean, err)
		}
	}
}
