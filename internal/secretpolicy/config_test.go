package secretpolicy_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/fsroot"
	"github.com/magicdrive/ark/internal/secretpolicy"
	"github.com/magicdrive/ark/internal/secrets"
)

// example is the configuration docs/secret-rules.md shows.
const example = `version = 1
preset = "recommended"

[[override]]
id = "generic-assignment-pass"
enabled = false
reason = "bypass = true is no secret"

[[override]]
id = "keep-terraform-reference"
enabled = false
reason = "mask Terraform references too"

[[rule]]
id = "acme-internal-token"
description = "ACME internal token"
pattern = 'acme_tok_[A-Za-z0-9]{8}'

[[rule]]
id = "acme-session"
pattern = 'session=([0-9a-f]{6})'
mask = "group"
capture_group = 1

[[assignment]]
id = "acme-assignment"
description = "ACME license keys"
keys = ["license_key", "dsn"]
`

func TestBuild_Example(t *testing.T) {
	const m = "*****MASKED*****"
	l, err := secretpolicy.Build("ark-secrets-rules.toml", []byte(example), secretpolicy.Options{AllowWeakening: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Ignored) != 0 {
		t.Errorf("ignored %v with weakening allowed", l.Ignored)
	}
	for _, tc := range []struct {
		syntax   secrets.Syntax
		in, want string
	}{
		{"", "bypass = true", "bypass = true"},
		{"", "password = x", "password = " + m},
		{"", "acme_tok_abcdEFGH", m},
		{"", "session=a1b2c3", "session=" + m},
		{"", "license_key: a b c", "license_key: " + m},
		{secrets.SyntaxTerraform, "  password = var.x", "  password = " + m},
	} {
		if got := l.Policy.Mask(tc.in, secrets.Mode{Syntax: tc.syntax}); got != tc.want {
			t.Errorf("Mask(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	var disabled []string
	for _, r := range l.Policy.Rules() {
		if !r.Enabled {
			disabled = append(disabled, r.ID+": "+r.Reason)
		}
	}
	if want := []string{"generic-assignment-pass: bypass = true is no secret", "keep-terraform-reference: mask Terraform references too"}; !slices.Equal(disabled, want) {
		t.Errorf("disabled %q, want %q", disabled, want)
	}
}

// Without the weakening allowed, the entries that disable masking rules
// are left out and reported; the rest applies — a reference exception off
// masks more, so it applies too.
func TestBuild_WeakeningNotAllowed(t *testing.T) {
	const m = "*****MASKED*****"
	l, err := secretpolicy.Build(".ark-secrets-rules.toml", []byte(example), secretpolicy.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := l.IgnoredIDs(); !slices.Equal(got, []string{"generic-assignment-pass"}) {
		t.Errorf("ignored %q, want [generic-assignment-pass]", got)
	}
	for _, tc := range []struct {
		syntax   secrets.Syntax
		in, want string
	}{
		{"", "bypass = true", "bypass = " + m},
		{"", "acme_tok_abcdEFGH", m},
		{secrets.SyntaxTerraform, "  password = var.x", "  password = " + m},
	} {
		if got := l.Policy.Mask(tc.in, secrets.Mode{Syntax: tc.syntax}); got != tc.want {
			t.Errorf("Mask(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	allowed, _ := secretpolicy.Build(".ark-secrets-rules.toml", []byte(example), secretpolicy.Options{AllowWeakening: true})
	if allowed.Fingerprint == l.Fingerprint {
		t.Error("the options do not change the fingerprint")
	}
	again, _ := secretpolicy.Build(".ark-secrets-rules.toml", []byte(example), secretpolicy.Options{})
	if again.Fingerprint != l.Fingerprint {
		t.Error("the fingerprint of the same bytes changed")
	}
}

// Every mistake is an error naming the file — and, where the parser knows
// it, the line — never the policy without the mistaken rule; and no error
// repeats a value the configuration holds.
func TestBuild_Errors(t *testing.T) {
	secret := "hunter2secretvalue"
	for _, tc := range []struct{ name, toml, want string }{
		{"no version", `preset = "recommended"`, "version = 1 is required"},
		{"future version", "version = 2", "version 2: this Ark reads version 1"},
		{"preset", "version = 1\npreset = \"strict\"", `preset "strict"`},
		{"unknown top-level field", "version = 1\ncolour = \"red\"", "unknown field colour"},
		{"unknown nested field", "version = 1\n[[rule]]\nid = \"a\"\npattern = \"x\"\nflags = \"i\"", "unknown field rule.flags"},
		{"misspelled table", "version = 1\n[[overide]]\nid = \"jwt\"", "unknown field overide"},
		{"type", "version = 1\n[[override]]\nid = \"jwt\"\nenabled = \"no\"", "line 4 (last key \"override.enabled\"): incompatible types"},
		{"syntax", "version = 1\n[[rule]]\nid = \"a\"\npattern = '" + secret, ":4: invalid TOML"},
		{"override no id", "version = 1\n[[override]]\nenabled = false\nreason = \"r\"", "[[override]] #1: id is required"},
		{"override no enabled", "version = 1\n[[override]]\nid = \"jwt\"\nreason = \"r\"", "enabled = false is required"},
		{"override enabled", "version = 1\n[[override]]\nid = \"jwt\"\nenabled = true\nreason = \"r\"", "enabled = true changes nothing"},
		{"override no reason", "version = 1\n[[override]]\nid = \"jwt\"\nenabled = false", "reason is required"},
		{"override unknown rule", "version = 1\n[[override]]\nid = \"no-such\"\nenabled = false\nreason = \"r\"", `no built-in rule has this ID`},
		{"rule no pattern", "version = 1\n[[rule]]\nid = \"a\"", "pattern is required"},
		{"rule bad mask", "version = 1\n[[rule]]\nid = \"a\"\npattern = \"x\"\nmask = \"line\"", `mask "line"`},
		{"rule group without mask", "version = 1\n[[rule]]\nid = \"a\"\npattern = \"(x)\"\ncapture_group = 1", `capture_group needs mask = "group"`},
		{"rule mask without group", "version = 1\n[[rule]]\nid = \"a\"\npattern = \"(x)\"\nmask = \"group\"", "needs capture_group = 1 or more"},
		{"rule group too big", "version = 1\n[[rule]]\nid = \"a\"\npattern = \"(" + secret + ")\"\nmask = \"group\"\ncapture_group = 2", "capture group 2: the pattern has 1"},
		{"rule bad regexp", "version = 1\n[[rule]]\nid = \"a\"\npattern = \"(" + secret + "\"", "pattern does not compile"},
		{"rule empty match", "version = 1\n[[rule]]\nid = \"a\"\npattern = \"" + secret + "|\"", "matches the empty string"},
		{"rule builtin id", "version = 1\n[[rule]]\nid = \"jwt\"\npattern = \"x\"", "a built-in rule has this ID"},
		{"rule duplicate", "version = 1\n[[rule]]\nid = \"a\"\npattern = \"x\"\n[[rule]]\nid = \"a\"\npattern = \"y\"", "defined twice"},
		{"assignment no keys", "version = 1\n[[assignment]]\nid = \"a\"", "between 1 and 100 keys"},
		{"assignment bad key", "version = 1\n[[assignment]]\nid = \"a\"\nkeys = [\"a.b\"]", `key "a.b"`},
		{"too large", "version = 1\n#" + strings.Repeat("x", secretpolicy.MaxFileSize), "larger than"},
	} {
		_, err := secretpolicy.Build("cfg.toml", []byte(tc.toml), secretpolicy.Options{AllowWeakening: true})
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "cfg.toml") {
			t.Errorf("%s: error %v, want one naming cfg.toml and containing %q", tc.name, err, tc.want)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the error repeats a configured value: %v", tc.name, err)
		}
	}
}

func pin(t *testing.T, dir string) *fsroot.Tree {
	t.Helper()
	tree, err := fsroot.Pin(dir, fsroot.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tree.Close() })
	return tree
}

// The configuration is a regular file at the root under either name; none
// is the recommended policy; both, a symlink or a directory are errors.
func TestLoad(t *testing.T) {
	cfg := "version = 1\n[[rule]]\nid = \"acme\"\npattern = \"acme_[0-9]+\"\n"
	write := func(dir, name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	none := t.TempDir()
	l, err := secretpolicy.Load(pin(t, none), secretpolicy.Options{})
	if err != nil || l.File != "" || l.Policy != secrets.Recommended() {
		t.Errorf("no file: %+v %v, want the recommended policy", l, err)
	}

	for _, name := range []string{secretpolicy.FileName, secretpolicy.AltFileName} {
		dir := t.TempDir()
		write(dir, name, cfg)
		l, err := secretpolicy.Load(pin(t, dir), secretpolicy.Options{})
		if err != nil || l.File != name || l.Policy.Mask("acme_42", secrets.Mode{}) != "*****MASKED*****" {
			t.Errorf("%s: %+v %v", name, l, err)
		}
	}

	both := t.TempDir()
	write(both, secretpolicy.FileName, cfg)
	write(both, secretpolicy.AltFileName, cfg)
	if _, err := secretpolicy.Load(pin(t, both), secretpolicy.Options{}); err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("both names: %v", err)
	}

	dirName := t.TempDir()
	if err := os.Mkdir(filepath.Join(dirName, secretpolicy.FileName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := secretpolicy.Load(pin(t, dirName), secretpolicy.Options{}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("a directory: %v", err)
	}

	// A configuration in a subdirectory is not read.
	sub := t.TempDir()
	os.Mkdir(filepath.Join(sub, "d"), 0o755)
	write(filepath.Join(sub, "d"), secretpolicy.FileName, "not toml at all [")
	if l, err := secretpolicy.Load(pin(t, sub), secretpolicy.Options{}); err != nil || l.File != "" {
		t.Errorf("a subdirectory's file: %+v %v", l, err)
	}

	link := t.TempDir()
	outside := filepath.Join(t.TempDir(), "rules.toml")
	write(filepath.Dir(outside), "rules.toml", cfg)
	if err := os.Symlink(outside, filepath.Join(link, secretpolicy.FileName)); err != nil {
		if os.Getenv("ARK_REQUIRE_SYMLINKS") != "" {
			t.Fatal(err)
		}
		t.Logf("SKIP part: no symlinks here (%s): %v", runtime.GOOS, err)
		return
	}
	if _, err := secretpolicy.Load(pin(t, link), secretpolicy.Options{}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("a symlink: %v", err)
	}
	inside := t.TempDir()
	write(inside, "real.toml", cfg)
	if err := os.Symlink("real.toml", filepath.Join(inside, secretpolicy.AltFileName)); err != nil {
		t.Fatal(err)
	}
	if _, err := secretpolicy.Load(pin(t, inside), secretpolicy.Options{}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("a symlink inside the root: %v", err)
	}
}

// FuzzBuild: any bytes are a configuration or an error, never a panic, and a
// configuration that compiles masks.
func FuzzBuild(f *testing.F) {
	f.Add([]byte(example))
	f.Add([]byte("version = 1\n[[rule]]\nid = \"a\"\npattern = \"(x)\"\nmask = \"group\"\ncapture_group = 1\n"))
	f.Add([]byte("version = 1\n[[assignment]]\nid = \"a\"\nkeys = [\"k\"]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, allow := range []bool{false, true} {
			l, err := secretpolicy.Build("f.toml", data, secretpolicy.Options{AllowWeakening: allow})
			if err != nil {
				continue
			}
			if l.Policy == nil {
				t.Fatal("no policy and no error")
			}
			l.Policy.Mask("password = x\nacme a␤b", secrets.Mode{Compact: true})
		}
	})
}
