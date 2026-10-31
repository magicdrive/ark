package declname

import (
	"strings"
	"testing"
)

func TestIsName(t *testing.T) {
	for _, s := range []string{"build", "日本語 🚀", "a/b~c", "x::y", strings.Repeat("a", MaxBytes)} {
		if !IsName(s) {
			t.Errorf("%q is a name", s)
		}
	}
	for _, s := range []string{"", "a\nb", "a\x00b", "a\u202eb", "a\u200bb", "a\u2060b", "a\u061cb", "a\u00adb", "a\u2028b", "\xff", strings.Repeat("a", MaxBytes+1)} {
		if IsName(s) {
			t.Errorf("%q is no name", s)
		}
	}
}
