package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/magicdrive/ark/internal/secrets"
)

// slackPathSecret is the secret section of a Slack token one fixture path
// holds.
const slackPathSecret = "SXpfKtHF4vUCsMehGAkWvj7F"

// pathsTree is the fixture of the path-masking tests: files whose paths look
// like assignments to the generic rules (token-…, password-…, api_key-…)
// and one whose path is a Slack token; the sources hold real secrets.
func pathsTree(t *testing.T, extra map[string]string, flags ...string) (*ToolsHandler, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"auth/token-store.go":    "package auth\n\n// Store keeps tokens.\nfunc Store() string {\n\tpassword := \"hunter2store\"\n\treturn Load() + password\n}\n",
		"auth/password-reset.go": "package auth\n\nfunc Load() string {\n\tapi_key := \"zebra42load\"\n\treturn api_key\n}\n",
		"auth/api_key-x.go":      "package auth\n\nfunc Use() string { return Store() }\n",
		"auth/xox" + "b-123456789012-1234567890123-" + slackPathSecret + ".go": "package auth\n\nfunc Leaky() string { return Store() + \"AK" + "IA1234567890ABCDEF\" }\n",
		"go.mod": "module example.com\n\ngo 1.22\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	writeTree(t, root, files)
	return maskServer(t, root, flags...), root
}

var pathLeaks = []string{"hunter2store", "zebra42load", slackPathSecret, "AK" + "IA1234567890ABCDEF"}

// Y2: the text of get_context and analyze_change_impact keeps a path that
// reads as an assignment ("auth/token-store.go"), its line numbers and what
// follows it, masks a token in a path, and masks the sources as before.
func TestMaskingPaths_TextKeepsPathsMasksSources(t *testing.T) {
	h, _ := pathsTree(t, nil)
	for _, c := range []struct {
		tool string
		keep []string
	}{
		{"get_context", []string{"### auth/token-store.go:4-7\nSymbol: Store\n", "### auth/password-reset.go:3-6\nSymbol: Load\n", "### auth/api_key-x.go:3-3\nSymbol: Use\n", "### auth/*****MASKED*****.go:3-3\nSymbol: Leaky\n", `password := "*****MASKED*****"`}},
		{"analyze_change_impact", []string{"Target:\n  Store  auth/token-store.go:4\n", "auth/api_key-x.go:3  [strong]\n", "auth/password-reset.go:3  [strong]\n", "Affected files (4):\n  auth/api_key-x.go\n  auth/password-reset.go\n  auth/token-store.go\n  auth/*****MASKED*****.go\n"}},
	} {
		text := mustText(t, h, c.tool, map[string]interface{}{"path": ".", "symbol": "Store"})
		for _, k := range c.keep {
			if !strings.Contains(text, k) {
				t.Errorf("%s: %q not in:\n%s", c.tool, k, text)
			}
		}
		for _, l := range pathLeaks {
			if strings.Contains(text, l) {
				t.Errorf("%s: leaks %q:\n%s", c.tool, l, text)
			}
		}
		if strings.IndexFunc(text, isMarkerRune) >= 0 {
			t.Errorf("%s: a marker left:\n%q", c.tool, text)
		}
	}
}

// The JSON format is unchanged: a path is an identifier a client passes
// back, never masked; a name is masked by the token rules only; the sources
// are masked.
func TestMaskingPaths_JSONUnchanged(t *testing.T) {
	h, _ := pathsTree(t, nil)
	for _, tool := range []string{"get_context", "analyze_change_impact"} {
		text := mustText(t, h, tool, map[string]interface{}{"path": ".", "symbol": "Store", "format": "json"})
		if !json.Valid([]byte(text)) || !strings.Contains(text, `"auth/token-store.go"`) {
			t.Errorf("%s json:\n%s", tool, text)
		}
		for _, l := range []string{"hunter2store", "zebra42load", "AK" + "IA1234567890ABCDEF"} {
			if strings.Contains(text, l) {
				t.Errorf("%s json: leaks %q", tool, l)
			}
		}
	}
}

// A name in the text is masked as the JSON masks it: by the token rules
// alone. A YAML key "token-ttl" is kept; a key that is a token is masked.
func TestMaskingPaths_NamesByTokenRulesOnly(t *testing.T) {
	h, _ := pathsTree(t, map[string]string{
		"conf/app.yaml": "token-ttl: 30\nxox" + "b-123456789012-1234567890123-" + slackPathSecret + ": 1\npassword: hunter2yaml\n",
	})
	for _, tool := range []string{"get_context", "analyze_change_impact"} {
		text := mustText(t, h, tool, map[string]interface{}{"path": ".", "symbol": "token-ttl"})
		if !strings.Contains(text, "token-ttl") || !strings.Contains(text, "conf/app.yaml") {
			t.Errorf("%s: name or path not kept:\n%s", tool, text)
		}
		for _, l := range []string{slackPathSecret, "hunter2yaml"} {
			if strings.Contains(text, l) {
				t.Errorf("%s: leaks %q:\n%s", tool, l, text)
			}
		}
	}
}

// With a configuration the server cannot apply, nothing with repository
// text goes out, paths included (fail closed); with masking off the text is
// the formatted text, no marker in it.
func TestMaskingPaths_FailClosedAndMaskingOff(t *testing.T) {
	h, _ := pathsTree(t, map[string]string{".ark-secrets-rules.toml": "version = 1\n[[rule]]\nid = \"x\"\npattern = '(broken'\n"})
	for _, tool := range []string{"get_context", "analyze_change_impact"} {
		text, isErr := callText(t, h, tool, map[string]interface{}{"path": ".", "symbol": "Store"})
		if !isErr || !strings.Contains(text, "configuration error") || strings.Contains(text, "token-store") || strings.Contains(text, "hunter2store") {
			t.Errorf("%s: want the configuration error alone, got isErr=%v:\n%s", tool, isErr, text)
		}
	}
	h, _ = pathsTree(t, nil, "--mask-secrets", "off")
	text := mustText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": "Store"})
	if !strings.Contains(text, "hunter2store") || !strings.Contains(text, slackPathSecret) || strings.IndexFunc(text, isMarkerRune) >= 0 {
		t.Errorf("masking off:\n%q", text)
	}
}

// A source holding a marker rune leaves the text with no identifiers: it is
// masked whole, as before, and the rune is kept as written.
func TestMaskingPaths_SourceWithMarkerRuneMaskedWhole(t *testing.T) {
	h, _ := pathsTree(t, map[string]string{"auth/odd.go": "package auth\n\nfunc Odd() string { return Store() + \"\U000F0000 \U00100000\" }\n"})
	text := mustText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": "Odd"})
	if !strings.Contains(text, "\U000F0000 \U00100000") || !strings.Contains(text, "auth/token-*****MASKED*****") {
		t.Errorf("want the whole text masked as before:\n%s", text)
	}
	for _, l := range pathLeaks {
		if strings.Contains(text, l) {
			t.Errorf("leaks %q", l)
		}
	}
}

// identifierMarks keeps fmt's padding (a marker has the identifier's rune
// count), finds every use of an identifier, and gives up on any text it did
// not make.
func TestIdentifierMarks_Spans(t *testing.T) {
	var m identifierMarks
	a, b := m.mark("auth/token-store.go"), m.mark("Störe")
	text := fmt.Sprintf("%-30s|%s|%s", "Störe", "auth/token-store.go", "Störe")
	marked := fmt.Sprintf("%-30s|%s|%s", b, a, b)
	spans := m.spans(marked, text)
	if len(spans) != 3 {
		t.Fatalf("spans %v", spans)
	}
	for i, want := range []string{"Störe", "auth/token-store.go", "Störe"} {
		if got := text[spans[i].start:spans[i].end]; got != want {
			t.Errorf("span %d = %q, want %q", i, got, want)
		}
	}
	if m.spans(marked, text+"x") != nil || m.spans(marked+"\U000F0005", text) != nil || m.spans(fmt.Sprintf("%q", a), fmt.Sprintf("%q", "auth/token-store.go")) != nil {
		t.Errorf("spans of a text it did not make")
	}
	if got := m.mark(""); got != "" {
		t.Errorf("empty identifier marked %q", got)
	}
}

// maskAroundIdentifiers masks the text around the spans as the whole text,
// the spans by the token rules; it masks whole on spans that do not fit, a
// marker rune in the text, or a swallowed marker.
func TestMaskAroundIdentifiers(t *testing.T) {
	p := secrets.Recommended()
	text := "### auth/token-store.go:4-7\npassword: hunter2\n"
	spans := []identifierSpan{{4, 23}}
	if got, want := maskAroundIdentifiers(text, spans, secrets.Mode{}, p), "### auth/token-store.go:4-7\npassword: *****MASKED*****\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	whole := p.Mask(text, secrets.Mode{})
	for _, bad := range [][]identifierSpan{nil, {{4, 100}}, {{10, 5}}, {{4, 23}, {5, 6}}} {
		if got := maskAroundIdentifiers(text, bad, secrets.Mode{}, p); got != whole {
			t.Errorf("spans %v: got %q, want the whole mask", bad, got)
		}
	}
	odd := "\U000F0000" + text
	if got := maskAroundIdentifiers(odd, []identifierSpan{{8, 27}}, secrets.Mode{}, p); got != p.Mask(odd, secrets.Mode{}) {
		t.Errorf("marker rune in text: %q", got)
	}
	// A marker rune the text holds is never read as a marker, even where a
	// mask swallowed the real one.
	late := "password: auth/x.go\n\U000F0000\n"
	if got := maskAroundIdentifiers(late, []identifierSpan{{10, 19}}, secrets.Mode{}, p); got != p.Mask(late, secrets.Mode{}) {
		t.Errorf("marker rune after a swallowed marker: %q", got)
	}
	// A swallowed marker masks the text whole, which can mask more: an
	// identifier that opens a heredoc masks its body.
	heredoc := "token: <<EOT\nhunter2body\nEOT\n"
	if got := maskAroundIdentifiers(heredoc, []identifierSpan{{7, 12}}, secrets.Mode{}, p); got != p.Mask(heredoc, secrets.Mode{}) || strings.Contains(got, "hunter2body") {
		t.Errorf("swallowed marker opening a heredoc: %q", got)
	}
	// A marker inside a value is swallowed by its mask: the whole is masked.
	sw := "password: auth/token-store.go\n"
	if got := maskAroundIdentifiers(sw, []identifierSpan{{10, 29}}, secrets.Mode{}, p); got != p.Mask(sw, secrets.Mode{}) {
		t.Errorf("swallowed marker: %q", got)
	}
	// An identifier that is a token is masked; one that is an assignment is not.
	tok := "Symbol: xox" + "b-123456789012-1234567890123-" + slackPathSecret + "\nSymbol: token-ttl\n"
	got := maskAroundIdentifiers(tok, []identifierSpan{{8, 8 + 56}, {8 + 56 + 9, 8 + 56 + 18}}, secrets.Mode{}, p)
	if got != "Symbol: *****MASKED*****\nSymbol: token-ttl\n" {
		t.Errorf("tokens: %q", got)
	}
}

// FuzzMaskAroundIdentifiers: any text and identifier, never a panic, never a
// marker out, and a secret outside the identifier never survives.
func FuzzMaskAroundIdentifiers(f *testing.F) {
	for _, s := range [][3]string{
		{"### ", "auth/token-store.go", ":4-7\n"}, {"password: ", "x", "\n"}, {"token: |\n  ", "a", "\n  b\n"},
		{"-----BEGIN RSA PRIVATE KEY-----\n", "k", "\n-----END RSA PRIVATE KEY-----\n"}, {"a␤", "xox" + "b-1-2", "␤b"}, {"", "\U000F0001", ""},
	} {
		f.Add(s[0], s[1], s[2])
	}
	const secret = "AK" + "IA1234567890ABCDEF"
	p := secrets.Recommended()
	f.Fuzz(func(t *testing.T, pre, id, post string) {
		if !utf8.ValidString(pre + id + post) {
			return
		}
		text := pre + id + " " + secret + " " + post
		spans := []identifierSpan{{len(pre), len(pre) + len(id)}}
		got := maskAroundIdentifiers(text, spans, secrets.Mode{}, p)
		if strings.IndexFunc(text, isMarkerRune) < 0 && strings.IndexFunc(got, isMarkerRune) >= 0 {
			t.Fatalf("marker out: %q", got)
		}
		if strings.Contains(got, secret) && !strings.Contains(id, secret) {
			t.Fatalf("secret survives: %q -> %q", text, got)
		}
	})
}
