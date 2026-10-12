package secrets_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/magicdrive/ark/internal/secrets"
	"github.com/magicdrive/ark/internal/secrets/internal/v61"
)

// modes are the ways a text is masked, with v6.1.0's function for each.
var modes = []struct {
	name string
	mode secrets.Mode
	v61  func(string) string
}{
	{"unknown", secrets.Mode{}, func(s string) string { return v61.MaskAllIn(s, v61.SyntaxUnknown) }},
	{"terraform", secrets.Mode{Syntax: secrets.SyntaxTerraform}, func(s string) string { return v61.MaskAllIn(s, v61.SyntaxTerraform) }},
	{"compact", secrets.Mode{Compact: true}, func(s string) string { return v61.MaskCompactIn(s, v61.SyntaxUnknown) }},
}

// fixtureTexts are texts full of secret-like syntax: this package's tests
// (every masking case written so far), the masking and security docs, and
// the MCP masking tests.
func fixtureTexts(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, glob := range []string{"*_test.go", "../mcp/*mask*_test.go", "../mcp/sanitize_test.go", "../../SECURITY.md", "../../docs/*.md", "../../tests/integration/mcp_secret_masking_integration_test.go"} {
		paths, _ := filepath.Glob(glob)
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			out[p] = string(b)
		}
	}
	if len(out) < 8 {
		t.Fatalf("only %d fixture texts", len(out))
	}
	return out
}

// rebuild masks s at the union of the findings' spans, as Mask masks:
// overlapping or nested spans are one mask; an empty span (a mask inserted at
// an empty value) is one mask where no other span covers it.
func rebuild(s string, fs []secrets.Finding) string {
	type span struct{ a, b int }
	var spans []span
	for _, f := range fs {
		spans = append(spans, span{f.Start, f.End})
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].a != spans[j].a {
			return spans[i].a < spans[j].a
		}
		return spans[i].b > spans[j].b
	})
	var merged []span
	for _, x := range spans {
		if n := len(merged); n > 0 {
			last := &merged[n-1]
			if x.a < last.b || (x.a == last.a && x.b <= last.b && last.b > last.a) || (x.a == x.b && x.a > last.a && x.a < last.b) {
				last.b = max(last.b, x.b)
				continue
			}
		}
		merged = append(merged, x)
	}
	var out strings.Builder
	prev := 0
	for _, x := range merged {
		out.WriteString(s[prev:x.a])
		out.WriteString("*****MASKED*****")
		prev = x.b
	}
	out.WriteString(s[prev:])
	return out.String()
}

// checkRecommended compares the recommended policy with v6.1.0 on s, and
// Find with Mask.
func checkRecommended(t *testing.T, what, s string) {
	t.Helper()
	p := secrets.Recommended()
	for _, m := range modes {
		got := p.Mask(s, m.mode)
		if want := m.v61(s); got != want {
			t.Errorf("%s (%s): Recommended().Mask differs from v6.1.0\n got %q\nwant %q", what, m.name, clip(got), clip(want))
			continue
		}
		if r := rebuild(s, p.Find(s, m.mode)); r != got {
			t.Errorf("%s (%s): Find's spans do not make Mask's result\nfind %q\nmask %q", what, m.name, clip(r), clip(got))
		}
	}
}

func clip(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// Without configuration, Ark masks exactly what v6.1.0 masked, byte for
// byte, in every mode; and Find finds exactly what Mask masks.
func TestRecommended_MatchesV61(t *testing.T) {
	for path, s := range fixtureTexts(t) {
		checkRecommended(t, path, s)
	}
	for _, s := range []string{
		"", "\n", "\r\n", "password", "password:", "password: ", `password: ""`, `password: "`, "x␤password: a␤y",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----\npassword = <<EOT\na\nEOT\n",
		"token: |\n  a\n\tb\nnext: c\r\n",
		`"type": "service_account", "private_key_id": "abcdefabcdefabcdefabcdefabcdefab", "private_key": "-----BEGIN PRIVATE KEY-----foobarbarfoo"`,
	} {
		checkRecommended(t, "case", s)
	}
}

// FuzzRecommended_MatchesV61 compares the recommended policy with v6.1.0,
// and Find with Mask, on any text.
func FuzzRecommended_MatchesV61(f *testing.F) {
	for _, s := range []string{
		"password: correct horse battery", `"api_key": "x", 'pw' => 'y'`, "token: ${{ secrets.A }} x",
		"  password = var.x # c\r\n", "a␤password: |␤  b␤c", "AKIA1234567890ABCDEF password=x;token=y",
		"key: <<-EOT\n  s\n  EOT\n", `export TOKEN="a"b`, "-----BEGIN EC PRIVATE KEY-----\nx\n-----END EC PRIVATE KEY-----",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		checkRecommended(t, "fuzz", s)
	})
}

// The built-in rule IDs are a public contract (docs/secret-rules.md): a
// configuration disables a rule by its ID and a scan reports it. They change
// only on purpose, here and in the documentation together.
func TestBuiltinRules_StableIDs(t *testing.T) {
	want := []string{
		"aws-access-key-id", "gcp-service-account-key", "google-api-key", "gcp-private-key-id", "gcp-private-key",
		"azure-account-key", "azure-hex-key", "github-token", "gitlab-token", "slack-token", "stripe-live-key",
		"sendgrid-api-key", "google-oauth-token", "google-oauth-client-id", "private-key-header",
		"jwt-secret-assignment", "jwt",
		"pem-private-key-block",
		"generic-assignment-secret", "generic-assignment-password", "generic-assignment-passwd",
		"generic-assignment-pass", "generic-assignment-pw", "generic-assignment-token",
		"generic-assignment-api-key", "generic-assignment-access-key", "generic-assignment-private-key",
		"keep-actions-reference", "keep-terraform-reference",
	}
	if got := secrets.BuiltinRuleIDs(); !slices.Equal(got, want) {
		t.Errorf("built-in rule IDs changed:\n got %q\nwant %q", got, want)
	}
	for _, r := range secrets.Recommended().Rules() {
		if !r.Enabled || r.Source != secrets.SourceBuiltin || r.Description == "" || r.Kind == "" {
			t.Errorf("recommended rule %+v: want enabled, built-in, described", r)
		}
	}
}

func mustPolicy(t *testing.T, spec secrets.Spec) *secrets.Policy {
	t.Helper()
	p, err := secrets.NewPolicy(spec)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// ruleIDs returns the distinct rules of findings.
func ruleIDs(fs []secrets.Finding) []string {
	var out []string
	for _, f := range fs {
		if !slices.Contains(out, f.RuleID) {
			out = append(out, f.RuleID)
		}
	}
	sort.Strings(out)
	return out
}

// Disabling a rule removes exactly its masks: what other rules mask stays
// masked.
func TestPolicy_DisableRemovesOnlyThatRule(t *testing.T) {
	const m = "*****MASKED*****"
	aws := "AKIA" + "1234567890ABCDEF"
	for _, tc := range []struct {
		disable []string
		syntax  secrets.Syntax
		in      string
		want    string
	}{
		// The pass keyword no longer masks; password still does.
		{[]string{"generic-assignment-pass"}, "", "bypass = true\npassword = hunter2", "bypass = true\npassword = " + m},
		// The AWS pattern no longer masks a bare key; an assignment still masks its value.
		{[]string{"aws-access-key-id"}, "", "id: " + aws + "\npassword: " + aws, "id: " + aws + "\npassword: " + m},
		// All generic assignments off, a heredoc's body with them; token patterns stay.
		{[]string{"generic-assignment"}, "", "password = <<EOT\nhunter2\nEOT\nx = ghp_abcdefghijklmnopqrstuvwxyz0123456789abcd", "password = <<EOT\nhunter2\nEOT\nx = " + m},
		// The PEM block off: the header pattern still masks the BEGIN line.
		{[]string{"pem-private-key-block"}, "", "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----", m + "\nMIIE\n-----END RSA PRIVATE KEY-----"},
		// A reference exception off: a Terraform reference is masked too.
		{[]string{"keep-terraform-reference"}, secrets.SyntaxTerraform, "  password = var.db", "  password = " + m},
		{[]string{"keep-actions-reference"}, "", "token: ${{ secrets.A }}", "token: " + m},
	} {
		var spec secrets.Spec
		for _, id := range tc.disable {
			spec.Disable = append(spec.Disable, secrets.Disable{ID: id, Reason: "test"})
		}
		p := mustPolicy(t, spec)
		mode := secrets.Mode{Syntax: tc.syntax}
		if got := p.Mask(tc.in, mode); got != tc.want {
			t.Errorf("disable %v: Mask(%q)\n got %q\nwant %q", tc.disable, tc.in, got, tc.want)
		}
		for _, f := range p.Find(tc.in, mode) {
			if slices.Contains(tc.disable, f.RuleID) || (slices.Contains(tc.disable, "generic-assignment") && strings.HasPrefix(f.RuleID, "generic-assignment-")) {
				t.Errorf("disable %v: a disabled rule still finds %+v", tc.disable, f)
			}
		}
		if r := rebuild(tc.in, p.Find(tc.in, mode)); r != p.Mask(tc.in, mode) {
			t.Errorf("disable %v: Find and Mask differ on %q", tc.disable, tc.in)
		}
	}
}

// Disabling a rule takes away only its own masks: every span another rule
// finds with the full policy is still masked without it. (The converse does
// not hold, and need not: a keyword alone may mask more than with the
// others, as password=a;token=b masks a;token=b when token is no key.)
func TestPolicy_DisablingKeepsOtherRulesMasks(t *testing.T) {
	texts := map[string]string{}
	for path, s := range fixtureTexts(t) {
		if strings.HasSuffix(path, "_test.go") && !strings.Contains(path, "/") || strings.HasSuffix(path, "SECURITY.md") {
			texts[path] = s // the densest in secret-like syntax
		}
	}
	type key struct {
		path string
		mode int
	}
	fullFound := map[key][]secrets.Finding{}
	for path, s := range texts {
		for i, m := range modes {
			fullFound[key{path, i}] = secrets.Recommended().Find(s, m.mode)
		}
	}
	for _, id := range append(secrets.BuiltinRuleIDs(), secrets.RuleGenericAssignment) {
		if strings.HasPrefix(id, "keep-") {
			continue
		}
		without := mustPolicy(t, secrets.Spec{Disable: []secrets.Disable{{ID: id}}})
		for path, s := range texts {
			for i, m := range modes {
				covered := allFound(without.Find(s, m.mode))
				for _, f := range fullFound[key{path, i}] {
					if f.RuleID == id || (id == secrets.RuleGenericAssignment && strings.HasPrefix(f.RuleID, "generic-assignment-")) {
						continue
					}
					if f.End > f.Start && !covered(f.Start, f.End) {
						t.Errorf("without %s (%s): %s's mask at %s:%d:%d is gone", id, m.name, f.RuleID, path, f.Line, f.Column)
					}
				}
			}
		}
	}
}

// allFound returns whether a span is within the union of findings' spans.
func allFound(fs []secrets.Finding) func(a, b int) bool {
	return func(a, b int) bool {
		pos := a
		for _, f := range fs { // sorted by start
			if f.Start > pos {
				break
			}
			pos = max(pos, f.End)
			if pos >= b {
				return true
			}
		}
		return pos >= b
	}
}

// A configuration's rules mask as the built-in ones do: a pattern its
// matches (or a group of them), an assignment rule its keys' values with the
// built-in value boundaries — every word, quotes, heredoc bodies.
func TestPolicy_OwnRules(t *testing.T) {
	const m = "*****MASKED*****"
	p := mustPolicy(t, secrets.Spec{
		Patterns: []secrets.PatternSpec{
			{ID: "acme-token", Description: "ACME token", Pattern: `acme_tok_[A-Za-z0-9]{8}`},
			{ID: "acme-session", Pattern: `session=([0-9a-f]{6})`, Group: 1},
		},
		Assignments: []secrets.AssignmentSpec{{ID: "acme-assignment", Keys: []string{"license_key", "dsn"}}},
	})
	for _, tc := range []struct{ in, want string }{
		{"x acme_tok_abcdEFGH y", "x " + m + " y"},
		{"cookie: session=a1b2c3; path=/", "cookie: session=" + m + "; path=/"},
		{"license_key: correct horse battery", "license_key: " + m},
		{`"database_dsn": "postgres://u:p@h/db", "user": "u"`, `"database_dsn": "` + m + `", "user": "u"`},
		{"license_key = <<EOT\nabc\nEOT\n", "license_key = " + m + "\n" + m + "\nEOT\n"},
		{"password: still-masked", "password: " + m},
	} {
		if got := p.Mask(tc.in, secrets.Mode{}); got != tc.want {
			t.Errorf("Mask(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
	got := ruleIDs(p.Find("acme_tok_abcdEFGH session=a1b2c3\ndsn=x\npassword=y", secrets.Mode{}))
	if want := []string{"acme-assignment", "acme-session", "acme-token", "generic-assignment-password"}; !slices.Equal(got, want) {
		t.Errorf("rules found %q, want %q", got, want)
	}
	var own []string
	for _, r := range p.Rules() {
		if r.Source == secrets.SourceConfig {
			own = append(own, r.ID)
		}
	}
	if want := []string{"acme-token", "acme-session", "acme-assignment"}; !slices.Equal(own, want) {
		t.Errorf("configured rules %q, want %q", own, want)
	}
}

// A specification with a problem is refused, every problem named — never a
// text a pattern would match.
func TestNewPolicy_Errors(t *testing.T) {
	secret := "hunter2-secret-value"
	for _, tc := range []struct {
		name string
		spec secrets.Spec
		want string
	}{
		{"unknown rule", secrets.Spec{Disable: []secrets.Disable{{ID: "no-such-rule"}}}, `disable "no-such-rule": no built-in rule`},
		{"disabled twice", secrets.Spec{Disable: []secrets.Disable{{ID: "jwt"}, {ID: "jwt"}}}, `disabled twice`},
		{"bad id", secrets.Spec{Patterns: []secrets.PatternSpec{{ID: "Bad ID", Pattern: "x"}}}, `rule "Bad ID": an ID is`},
		{"built-in id", secrets.Spec{Patterns: []secrets.PatternSpec{{ID: "jwt", Pattern: "x"}}}, `a built-in rule has this ID`},
		{"reserved id", secrets.Spec{Assignments: []secrets.AssignmentSpec{{ID: "generic-assignment", Keys: []string{"k"}}}}, `reserved`},
		{"defined twice", secrets.Spec{Patterns: []secrets.PatternSpec{{ID: "a", Pattern: "x"}}, Assignments: []secrets.AssignmentSpec{{ID: "a", Keys: []string{"k"}}}}, `defined twice`},
		{"no compile", secrets.Spec{Patterns: []secrets.PatternSpec{{ID: "a", Pattern: "(" + secret}}}, `pattern does not compile: missing closing )`},
		{"empty match", secrets.Spec{Patterns: []secrets.PatternSpec{{ID: "a", Pattern: secret + "|x*"}}}, `matches the empty string`},
		{"bad group", secrets.Spec{Patterns: []secrets.PatternSpec{{ID: "a", Pattern: "(" + secret + ")", Group: 2}}}, `capture group 2: the pattern has 1`},
		{"negative group", secrets.Spec{Patterns: []secrets.PatternSpec{{ID: "a", Pattern: secret, Group: -1}}}, `capture group -1`},
		{"no keys", secrets.Spec{Assignments: []secrets.AssignmentSpec{{ID: "a"}}}, `between 1 and 100 keys`},
		{"bad key", secrets.Spec{Assignments: []secrets.AssignmentSpec{{ID: "a", Keys: []string{"a b"}}}}, `key "a b"`},
		{"long pattern", secrets.Spec{Patterns: []secrets.PatternSpec{{ID: "a", Pattern: strings.Repeat("x", secrets.MaxPatternLen+1)}}}, `pattern longer than`},
		{"too many", secrets.Spec{Patterns: slices.Repeat([]secrets.PatternSpec{{ID: "a", Pattern: "x"}}, secrets.MaxRules+1)}, `at most 1000`},
	} {
		_, err := secrets.NewPolicy(tc.spec)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want one containing %q", tc.name, err, tc.want)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the error repeats a pattern's text: %v", tc.name, err)
		}
	}
}

// A finding locates a span and names its rule; it holds nothing of the text.
func TestFinding_HoldsNoText(t *testing.T) {
	ft := reflect.TypeOf(secrets.Finding{})
	for i := range ft.NumField() {
		if f := ft.Field(i); f.Type.Kind() == reflect.String && f.Name != "RuleID" {
			t.Errorf("Finding.%s is a string: a finding must not hold the text it covers", f.Name)
		}
	}
	s := "a: 1\r\nx␤token: abc\nsecret = <<EOT\nbody\nEOT\n-----BEGIN EC PRIVATE KEY-----\nz\n-----END EC PRIVATE KEY-----\n"
	lines := strings.Split(s, "\n")
	var got []string
	for _, f := range secrets.Recommended().Find(s, secrets.Mode{Compact: true}) {
		line := lines[f.Line-1]
		got = append(got, fmt.Sprintf("%s %d:%d %q", f.RuleID, f.Line, f.Column, line[f.Column-1:min(len(line), f.Column-1+f.End-f.Start)]))
	}
	want := []string{
		`generic-assignment-token 2:12 "abc"`,
		`generic-assignment-secret 3:10 "<<EOT"`,
		`generic-assignment-secret 4:1 "body"`,
		`pem-private-key-block 6:1 "-----BEGIN EC PRIVATE KEY-----"`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("findings\n got %q\nwant %q", got, want)
	}
}

// One policy serves concurrent masks.
func TestPolicy_Concurrent(t *testing.T) {
	p := mustPolicy(t, secrets.Spec{Patterns: []secrets.PatternSpec{{ID: "acme", Pattern: "acme_[0-9]+"}}})
	in := "password: x\nacme_123 token=y\n"
	want := p.Mask(in, secrets.Mode{})
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				if got := p.Mask(in, secrets.Mode{}); got != want {
					t.Errorf("concurrent Mask = %q", got)
					return
				}
				p.Find(in, secrets.Mode{Compact: true})
			}
		}()
	}
	wg.Wait()
}

// docs/secret-rules.md lists every built-in rule, by ID and kind, in order:
// the public contract of the IDs.
func TestBuiltinRules_Documented(t *testing.T) {
	b, err := os.ReadFile("../../docs/secret-rules.md")
	if err != nil {
		t.Fatal(err)
	}
	var documented []string
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			continue
		}
		id, kind := strings.Trim(strings.TrimSpace(cells[1]), "`"), strings.TrimSpace(cells[2])
		if kind == "pattern" || kind == "block" || kind == "assignment" || kind == "reference" {
			documented = append(documented, id+" "+kind)
		}
	}
	var want []string
	for _, r := range secrets.Recommended().Rules() {
		want = append(want, r.ID+" "+string(r.Kind))
	}
	if !slices.Equal(documented, want) {
		t.Errorf("docs/secret-rules.md documents\n%q\nthe built-in rules are\n%q", documented, want)
	}
}
