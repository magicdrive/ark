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
		"password\u200b= hunterA2", "pass\u200bword = hunterA3",
		`password => "hunterXsecret1"`, "'password' => 'hunterXsecret2'", "password -> hunterXsecretB",
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

// In an XML path, a ":" glued between words is a QName's.
func TestHoldsSecretQName(t *testing.T) {
	if HoldsSecretQName("/root/secret:key") || HoldsSecretQName("/a/@xlink:href") {
		t.Error("a QName taken for a secret")
	}
	if !HoldsSecretQName("::dependencies::g:token=hunterF3") || !HoldsSecretQName("/a/@x") == false && false {
		t.Error("an assignment in an XML path not taken for a secret")
	}
	if !HoldsSecret("/root/secret:key") {
		t.Error("HoldsSecret: a glued \":\" outside XML is an assignment")
	}
}
