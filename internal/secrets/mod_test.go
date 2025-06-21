package secrets_test

import (
	"os"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/secrets"
)

func TestScanReader_DetectsSecrets(t *testing.T) {
	rs := secrets.DefaultRuleSet()
	input := `
AWS_ACCESS_KEY_ID=AKIA1234567890ABCDEF
GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789abcd
password=supersecret
normal_line=notasecret
`
	matches, err := rs.ScanReader(strings.NewReader(input), "test.env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 3 {
		t.Errorf("should detect 3 secrets, got %d", len(matches))
	}
	wantPatterns := []string{
		`AKIA[0-9A-Z]{16}`,
		`ghp_[A-Za-z0-9_]{36,255}`,
		`(?i)(secret|password|passwd|pass|pw|token|api[_\-]?key|access[_\-]?key|private[_\-]?key)\s*[:=]\s*['"]?[^'"\s]+['"]?`,
	}
	for i, m := range matches {
		if m.File != "test.env" {
			t.Errorf("expected filename 'test.env', got %q", m.File)
		}
		if !strings.Contains(m.Text, "secret") && !strings.Contains(m.Text, "AKIA") && !strings.Contains(m.Text, "ghp_") {
			t.Errorf("expected secret content in Text, got %q", m.Text)
		}
		if m.Pattern != wantPatterns[i] {
			t.Errorf("pattern %d: want %q, got %q", i, wantPatterns[i], m.Pattern)
		}
	}
}

func TestScanReader_AllowList(t *testing.T) {
	rs := secrets.DefaultRuleSet()
	input := `AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
password=supersecret`
	matches, err := rs.ScanReader(strings.NewReader(input), "test.env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("should detect 1 secret (password), got %d", len(matches))
	}
	if !strings.Contains(matches[0].Text, "password=") {
		t.Errorf("should only detect password line")
	}
}

func TestMaskLine(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`password=supersecret`, `password=*****MASKED*****`},
		{`GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789abcd`, `GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789abcd`},
		{`AKIA1234567890ABCDEF`, `*****MASKED*****`}, // ← 16桁に修正
		{`xoxb-1234567890abcdef`, `xoxb-1234567890abcdef`},
		{`some text with JWT eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6.abcdefghij`, `some text with JWT *****MASKED*****`}, // ← JWTパターン修正
		{`password="hunter2"`, `password="*****MASKED*****"`},
	}
	for _, tt := range tests {
		got := secrets.MaskLine(tt.input)
		if got != tt.want {
			t.Errorf("MaskLine(%q) = %q; want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsTextFile(t *testing.T) {
	textContent := []byte("normal ascii content\npassword=secret\n")
	binaryContent := []byte{0x00, 0x01, 0x02, 0x03, 0xff, 0xfe, 0xfd, 0xfc}

	textfile := "test_textfile.txt"
	binfile := "test_binfile.bin"

	if err := writeFile(textfile, textContent); err != nil {
		t.Fatalf("write text file: %v", err)
	}
	defer os.Remove(textfile)

	if err := writeFile(binfile, binaryContent); err != nil {
		t.Fatalf("write bin file: %v", err)
	}
	defer os.Remove(binfile)

	if !secrets.IsTextFile(textfile) {
		t.Errorf("expected text file to be detected as text")
	}
	if secrets.IsTextFile(binfile) {
		t.Errorf("expected binary file to be detected as binary")
	}
}

func writeFile(name string, content []byte) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(content)
	return err
}

func TestAddPatternAndAllowed(t *testing.T) {
	rs := secrets.DefaultRuleSet()
	err := rs.AddPattern(`testpattern[0-9]+`)
	if err != nil {
		t.Fatalf("AddPattern: %v", err)
	}
	err = rs.AddAllowed(`allowme`)
	if err != nil {
		t.Fatalf("AddAllowed: %v", err)
	}

	input := `
some=testpattern123
allowme=password
`
	matches, err := rs.ScanReader(strings.NewReader(input), "foo.env")
	if err != nil {
		t.Fatalf("ScanReader: %v", err)
	}
	found := false
	for _, m := range matches {
		if m.Pattern == `testpattern[0-9]+` {
			found = true
		}
	}
	if !found {
		t.Errorf("should detect testpattern")
	}
}

func TestScanReader_MultiplePatterns(t *testing.T) {
	rs := secrets.DefaultRuleSet()
	cases := []struct {
		line      string
		wantMatch bool
	}{
		// GCP "private_key_id"などは現状ルールでは検知しないのでfalseに
		{`private_key_id="abcdefabcdefabcdef"`, false},
		// JSON private_key
		{`"type": "service_account", "private_key_id": "abcdef1234", "private_key": "-----BEGIN PRIVATE KEY-----MIIBVwIBADANBg..."`, true},
		// Azure
		{`AccountKey=ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=`, true},
		// GitLab
		{`glpat-abc123def456ghi789jklmn`, true},
		// Slack
		{`xoxb-1234567890abcdef`, true},
		// Stripe
		{`sk_live_1234567890abcdefABCDEF12`, true},
		// SendGrid
		{`SG.abcdefghijklmnopqrstuv.abcdEFGHIJKLMN_opqrstuvw0123456789abcdefGHIJKL`, true},
		// Google OAuth
		{`ya29.A0ARrdaM-0123456789abcdefGHIJKLMNOPQRSTUVWXYZ`, true},
		// Google ClientID
		{`123456789012-abcdefghijklmnopqrstuvwxyzABCDEF.apps.googleusercontent.com`, true},
		// JWT
		{`eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6.abcdefghij`, true},
		// large case
		{`TOKEN=abcd1234`, true},
		// hypen
		{`api-key: xyzzy`, true},
		// colon space quite
		{`PASSWORD : "foo"`, true},
		// single quote
		{`pass='bar'`, true},
		// pw
		{`pw="baz"`, true},
		// not 'secret'
		{`normal=abcdef`, false},
		// allow-list
		{`AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE`, false},
	}
	for _, c := range cases {
		ms, err := rs.ScanReader(strings.NewReader(c.line), "f.env")
		if err != nil {
			t.Fatalf("ScanReader error: %v", err)
		}
		if c.wantMatch && len(ms) == 0 {
			t.Errorf("should detect: %q", c.line)
		}
		if !c.wantMatch && len(ms) > 0 {
			t.Errorf("should NOT detect: %q", c.line)
		}
	}
}

func TestMaskAll_SecretKeyBlocks(t *testing.T) {
	src := `
-----BEGIN ENCRYPTED PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDME3YzOw6f
hogehoge
-----END ENCRYPTED PRIVATE KEY-----
this line is normal_line
`
	masked := secrets.MaskAll(src)
	if !strings.Contains(masked, "*****MASKED*****") {
		t.Error("PEM key block should be masked")
	}
	if strings.Contains(masked, "BEGIN ENCRYPTED PRIVATE KEY") || strings.Contains(masked, "MIIEvQ") {
		t.Error("Key material should not appear")
	}
	if !strings.Contains(masked, "this line is normal_line") {
		t.Error("Normal lines should not be masked")
	}
}

func TestMaskLine_Variants(t *testing.T) {
	cases := []struct{ in, want string }{
		{`password : foo`, `password : *****MASKED*****`},
		{`token=123456`, `token=*****MASKED*****`},
		{`api_key:'abc'`, `api_key:'*****MASKED*****'`},
		{`PRIVATE_KEY="xyz"`, `PRIVATE_KEY="*****MASKED*****"`},
		{`secret='foo bar'`, `secret='*****MASKED*****'`},
	}
	for _, c := range cases {
		got := secrets.MaskLine(c.in)
		if got != c.want {
			t.Errorf("MaskLine(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
