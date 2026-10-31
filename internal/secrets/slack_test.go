package secrets_test

import (
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/secrets"
)

// slackTokens are Slack tokens in every documented shape (docs.slack.dev,
// "Token types", "Using token rotation"); the values are made up.
var slackTokens = []string{
	"xox" + "b-123456789012-1234567890123-SXpfKtHF4vUCsMehGAkWvj7F",                                          // bot
	"xox" + "p-111-222-333-d6bc768406e5c2e6958cfc399b438004",                                                 // user (the docs' example)
	"xox" + "p-1234567890-1234567890-1234567890123-0123456789abcdef0123456789abcdef",                         // user
	"xox" + "a-2-123456789012-1234567890123-abcdefABCDEF0123456789",                                          // legacy workspace
	"xox" + "r-1234567890-abcdefABCDEF0123456789",                                                            // legacy refresh
	"xox" + "s-1234567890-1234567890-1234567890-abcdef0123",                                                  // legacy
	"xoxb-" + strings.Repeat("a1B2", 20),                                                                     // one long section (past 48)
	"xox" + "e.xox" + "b-1-MS0yLTEyMzQ1Njc4OTAxMi0xMjM0NTY3ODkwMTIz" + strings.Repeat("Ab9", 30),             // rotated bot
	"xox" + "e.xox" + "p-1-1234-" + strings.Repeat("Zx0", 50),                                                // rotated user
	"xap" + "p-1-A0123ABCDEF-1234567890123-0a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f9", // app-level
	"xox" + "e.xap" + "p-1-A0123ABCDEF-1234567890123-" + strings.Repeat("f0", 32),                            // rotated app-level
	"xox" + "e-1-" + strings.Repeat("My0xLTEy", 18),                                                          // refresh
	"xox" + "o-1234567890-1234567890-1234567890-0123abcdef",                                                  // legacy xoxo
	"xwfp-" + strings.Repeat("Ab0", 12),                                                                      // workflow
}

// The whole token is masked, in every mode and wherever it stands; Find
// reports it as slack-token over all of it.
func TestSlackToken_MaskedWhole(t *testing.T) {
	p := secrets.Recommended()
	for _, tok := range slackTokens {
		for _, in := range []string{tok, "SLACK=" + tok, `"` + tok + `"`, "a " + tok + " b", "url?t=" + tok + "&x=1"} {
			for _, mode := range []secrets.Mode{{}, {Tokens: true}, {Compact: true}, {Syntax: secrets.SyntaxTerraform}} {
				got := p.Mask(in, mode)
				for _, part := range strings.Split(tok, "-")[1:] {
					if len(part) >= 4 && strings.Contains(got, part) {
						t.Errorf("%+v: %q masks to %q: section %q left", mode, in, got, part)
					}
				}
				if !strings.Contains(got, "*****MASKED*****") {
					t.Errorf("%+v: %q not masked: %q", mode, in, got)
				}
			}
			fs := p.Find(in, secrets.Mode{Tokens: true})
			ok := false
			for _, f := range fs {
				if f.RuleID == "slack-token" && in[f.Start:f.End] == tok {
					ok = true
				}
			}
			if !ok {
				t.Errorf("Find(%q) = %+v: want slack-token over %q", in, fs, tok)
			}
		}
	}
}

// The reported case: v6.1.0 masked the first section only.
func TestSlackToken_ReportedCase(t *testing.T) {
	in := "token xox" + "b-123456789012-1234567890123-SXpfKtHF4vUCsMehGAkWvj7F end"
	if got, want := secrets.Recommended().Mask(in, secrets.Mode{Tokens: true}), "token *****MASKED***** end"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Prose, identifiers and lookalikes are left as written; a bare prefix is
// masked as v6.1.0 masked it.
func TestSlackToken_NotOvermasked(t *testing.T) {
	p := secrets.Recommended()
	for in, want := range map[string]string{
		"xap" + "p-style config":            "xap" + "p-style config",
		"xox" + "o-gossip":                  "xox" + "o-gossip",
		"xox" + "o-1-2":                     "xox" + "o-1-2",
		"xox" + "e-style":                   "xox" + "e-style",
		"xox" + "e.notes":                   "xox" + "e.notes",
		"xwf" + "p-short":                   "xwf" + "p-short",
		"xoxbox and xoxp":                   "xoxbox and xoxp",
		"xap" + "p-1":                       "xap" + "p-1",
		"mixap" + "p-config":                "mixap" + "p-config",
		"tests/xoxb_test.go":                "tests/xoxb_test.go",
		"xoxb-":                             "*****MASKED*****",
		"xox" + "b-token in prose":          "*****MASKED*****token in prose",
		"xox" + "b-a.b":                     "*****MASKED*****a.b",
		"see xox" + "p-111-222 for details": "see *****MASKED***** for details",
	} {
		if got := p.Mask(in, secrets.Mode{Tokens: true}); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}

// The other formats are masked as before, next to a Slack token.
func TestSlackToken_OtherFormatsUnchanged(t *testing.T) {
	p := secrets.Recommended()
	others := []string{
		"AK" + "IA1234567890ABCDEF", "gh" + "p_abcdefghijklmnopqrstuvwxyz0123456789abcd", "gl" + "pat-abcdefghijklmnopqrst",
		"sk_" + "live_abcdefghijklmnopqrstuvwx", "SG" + ".abcdefghijklmnopqrstuv.ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi12345678",
		"ya" + "29.A0ARrdaM-0123456789", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6.abcdefghij",
	}
	for _, o := range others {
		for _, tok := range slackTokens {
			for _, in := range []string{tok + " " + o, o + " " + tok, tok + "," + o} {
				got := p.Mask(in, secrets.Mode{Tokens: true})
				if strings.Contains(got, o) || strings.Count(got, "*****MASKED*****") != 2 {
					t.Errorf("Mask(%q) = %q: want two masks", in, got)
				}
			}
		}
	}
}
