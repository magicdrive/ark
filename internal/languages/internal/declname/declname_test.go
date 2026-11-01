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

// A token, or a value after "=" or ":", is a secret; a value after "-" is
// a word of a name; a format character hides nothing.
func TestHoldsSecret(t *testing.T) {
	for _, s := range []string{
		"password = hunter2secretvalue", "api_key=hunter3secretvalue", "aws AK" + "IAIOSFODNN7EXAMPLE",
		"Token: rotate", "img/password=hunter10secret.png", "g:token=hunterF3",
		"password​= hunterA2", "pass​word = hunterA3",
	} {
		if !HoldsSecret(s) {
			t.Errorf("HoldsSecret(%q) = false", s)
		}
	}
	for _, s := range []string{
		"Provide a password using STDIN (--password-stdin)", "password-stdin", "--secret-add", "Pass-Through",
		"Token-source behavior", "/b:book/@xml:id", "com.example:core", "/package/name", "Setup",
	} {
		if HoldsSecret(s) {
			t.Errorf("HoldsSecret(%q) = true", s)
		}
	}
}
