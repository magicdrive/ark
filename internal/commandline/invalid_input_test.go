package commandline

import (
	"fmt"
	"strings"
	"testing"
)

// parseNoPanic runs a parser and turns a panic into a test failure: invalid CLI
// input must be an ordinary error, never a crash.
func parseNoPanic(t *testing.T, name string, parse func() error) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s: panicked instead of returning an error: %v", name, r)
		}
	}()
	return parse()
}

// The on/off values the CLI accepts today (internal/model onOffUnitMap). The
// contract is pinned here: no value is added or removed by the invalid-input fix.
var validOnOff = []string{"on", "ON", "YES", "yes", "y", "off", "OFF", "NO", "no", "n"}

// Not accepted today; must stay errors (and must not panic).
var invalidOnOff = []string{"bogus", "", "On", "true", "false", "1", "0", "Yes", "maybe", " on"}

type onOffFlag struct {
	cmd   string
	flag  string
	parse func(args []string) error
}

func onOffFlags() []onOffFlag {
	general := func(args []string) error { _, _, err := GeneralOptParse(args); return err }
	serve := func(args []string) error { _, _, err := ServerOptParse("test", args); return err }
	var out []onOffFlag
	for _, f := range []string{"--mask-secrets", "--allow-gitignore", "--with-line-number", "--ignore-dotfile"} {
		out = append(out, onOffFlag{"ark", f, general})
	}
	for _, f := range []string{"--mask-secrets", "--allow-gitignore", "--ignore-dotfile"} {
		out = append(out, onOffFlag{"ark mcp-server", f, serve})
	}
	return out
}

// Known regression: `ark --allow-gitignore bogus` panicked in Normalize
// (AllowGitignoreFlag.Bool() on a switch whose Set had just failed).
func TestOnOffFlags_InvalidValueIsErrorNotPanic(t *testing.T) {
	for _, f := range onOffFlags() {
		for _, v := range invalidOnOff {
			name := fmt.Sprintf("%s %s %q", f.cmd, f.flag, v)
			t.Run(name, func(t *testing.T) {
				err := parseNoPanic(t, name, func() error { return f.parse([]string{f.flag, v}) })
				if err == nil {
					t.Fatalf("invalid value accepted")
				}
				msg := err.Error()
				if !strings.Contains(msg, f.flag) {
					t.Errorf("error %q does not identify the option %s", msg, f.flag)
				}
				if !strings.Contains(msg, fmt.Sprintf("%q", v)) {
					t.Errorf("error %q does not identify the invalid value %q", msg, v)
				}
			})
		}
	}
}

// Valid values keep parsing exactly as before.
func TestOnOffFlags_ValidValuesStillParse(t *testing.T) {
	for _, f := range onOffFlags() {
		for _, v := range validOnOff {
			name := fmt.Sprintf("%s %s %q", f.cmd, f.flag, v)
			t.Run(name, func(t *testing.T) {
				if err := parseNoPanic(t, name, func() error { return f.parse([]string{f.flag, v}) }); err != nil {
					t.Errorf("valid value rejected: %v", err)
				}
			})
		}
	}
}

// Normalized values are unchanged: the accepted aliases map to on/off as before.
func TestAllowGitignore_NormalizedValue(t *testing.T) {
	for v, want := range map[string]string{"on": "on", "YES": "on", "y": "on", "off": "off", "no": "off", "n": "off"} {
		_, opt, err := GeneralOptParse([]string{"--allow-gitignore", v})
		if err != nil {
			t.Fatalf("%s: %v", v, err)
		}
		if got := opt.AllowGitignoreFlag.String(); got != want {
			t.Errorf("--allow-gitignore %s normalized to %q, want %q", v, got, want)
		}
	}
}

// Every other finite-valued option must also reject bad input with an error.
func TestFiniteValuedOptions_InvalidInputNeverPanics(t *testing.T) {
	cases := []struct {
		name  string
		parse func(args []string) error
		flag  string
	}{
		{"ark --output-format", func(a []string) error { _, _, e := GeneralOptParse(a); return e }, "--output-format"},
		{"ark --scan-buffer", func(a []string) error { _, _, e := GeneralOptParse(a); return e }, "--scan-buffer"},
		{"mcp-server --type", func(a []string) error { _, _, e := ServerOptParse("test", a); return e }, "--type"},
		{"mcp-server --scan-buffer", func(a []string) error { _, _, e := ServerOptParse("test", a); return e }, "--scan-buffer"},
		{"syntax --format", func(a []string) error { _, _, e := SyntaxOptParse(a); return e }, "--format"},
		{"symbol --format", func(a []string) error { _, _, e := SymbolOptParse(a); return e }, "--format"},
	}
	for _, c := range cases {
		for _, v := range []string{"bogus", "", "💥", "--", "on off", "\x00"} {
			name := fmt.Sprintf("%s %q", c.name, v)
			t.Run(name, func(t *testing.T) {
				err := parseNoPanic(t, name, func() error { return c.parse([]string{c.flag, v}) })
				if err == nil {
					t.Errorf("invalid value accepted")
				}
			})
		}
	}
}

// Several invalid options at once: all are reported and nothing panics.
func TestInvalidOptions_Combined(t *testing.T) {
	err := parseNoPanic(t, "combined", func() error {
		_, _, err := GeneralOptParse([]string{"--allow-gitignore", "bogus", "--mask-secrets", "nope", "--ignore-dotfile", "x"})
		return err
	})
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{"--allow-gitignore", "--mask-secrets", "--ignore-dotfile", `"bogus"`, `"nope"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("combined error %q misses %s", err, want)
		}
	}
}
