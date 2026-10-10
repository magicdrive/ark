//go:build windows

package fsroot

import (
	"errors"
	"testing"
)

// Windows path forms that are not plain root-relative paths.
func TestSplitRel_Windows(t *testing.T) {
	for _, in := range []string{`C:\x`, `C:x`, `\\server\share\x`, `\x`, `a:stream`, `dir\file:ads`, `\\?\C:\x`} {
		if _, _, err := splitRel(in); err == nil {
			t.Errorf("splitRel(%q) accepted", in)
		}
	}
	for _, in := range []string{`a\b`, `a/b`} {
		if clean, _, err := splitRel(in); err != nil || clean != "a/b" {
			t.Errorf("splitRel(%q) = %q %v", in, clean, err)
		}
	}
	if _, _, err := splitRel(`a\..\..\b`); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("escape accepted: %v", err)
	}
}
