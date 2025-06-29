package secrets_test

import (
	"os"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/secrets"
)

func TestScanReader_FullMask(t *testing.T) {
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
	if matches[0].Pattern != `AKIA[0-9A-Z]{16}` {
		t.Errorf("pattern mismatch: got %v", matches[0].Pattern)
	}
	if matches[1].Pattern != `ghp_[A-Za-z0-9_]{36,255}` {
		t.Errorf("pattern mismatch: got %v", matches[1].Pattern)
	}
	if matches[2].Pattern != "value-masking" {
		t.Errorf("expected value-masking for password, got %v", matches[2].Pattern)
	}
}

func TestScanReader_AllowList(t *testing.T) {
	rs := secrets.DefaultRuleSet()
	input := `AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
password=supersecret`
	rs.AddAllowed("password=supersecret")
	matches, err := rs.ScanReader(strings.NewReader(input), "test.env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("should detect 0 secrets, got %d", len(matches))
	}
}

func TestAddPattern(t *testing.T) {
	rs := secrets.DefaultRuleSet()
	rs.AddPattern(`mysecret\d+`)
	input := `mysecret123`
	matches, err := rs.ScanReader(strings.NewReader(input), "file.txt")
	if err != nil {
		t.Fatalf("ScanReader: %v", err)
	}
	if len(matches) == 0 || matches[0].Pattern != "mysecret\\d+" {
		t.Errorf("should detect pattern mysecret\\d+, got %+v", matches)
	}
}

func TestAddValueMaskKey(t *testing.T) {
	rs := secrets.DefaultRuleSet()
	rs.AddValueMaskKey("customkey")
	input := `
customkey = foo
mykey: "bar"
token: abcdefg
`
	matches, err := rs.ScanReader(strings.NewReader(input), "env.txt")
	if err != nil {
		t.Fatalf("ScanReader: %v", err)
	}
	var foundCustom, foundToken bool
	for _, m := range matches {
		if strings.Contains(m.Text, "customkey") && m.Pattern == "value-masking" {
			foundCustom = true
		}
		if strings.Contains(m.Text, "token") && m.Pattern == "value-masking" {
			foundToken = true
		}
	}
	if !foundCustom {
		t.Error("should detect customkey as value-masking")
	}
	if !foundToken {
		t.Error("should detect token as value-masking")
	}
}

func TestMaskAll(t *testing.T) {
	src := `
-----BEGIN ENCRYPTED PRIVATE KEY-----
hogehoge
-----END ENCRYPTED PRIVATE KEY-----
secret = "hogehoge"
token: abcdefg
AWS_ACCESS_KEY_ID=AKIA1234567890ABCDEF
normal = value
normal_hoge = "ghp_abcdefghijklmnopqrstuvwxyz0123456789abcd"
`
	want := `
*****MASKED*****
secret = "*****MASKED*****"
token: *****MASKED*****
AWS_ACCESS_KEY_ID=*****MASKED*****
normal = value
normal_hoge = "*****MASKED*****"
`
	got := secrets.MaskAll(src)
	linesWant := strings.Split(strings.TrimSpace(want), "\n")
	linesGot := strings.Split(strings.TrimSpace(got), "\n")
	if len(linesWant) != len(linesGot) {
		t.Fatalf("MaskAll line count mismatch:\nwant: %q\ngot:  %q", want, got)
	}
	for i := range linesWant {
		if strings.TrimSpace(linesWant[i]) != strings.TrimSpace(linesGot[i]) {
			t.Errorf("MaskAll line %d mismatch:\nwant: %q\ngot:  %q", i, linesWant[i], linesGot[i])
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

func TestDefaultPatterns_CompleteCoverage(t *testing.T) {
	rs := secrets.DefaultRuleSet()
	tests := []struct {
		desc    string
		input   string
		pattern string
	}{
		// AWS
		{"AKIA", "AWS_ACCESS_KEY_ID=AKIA1234567890ABCDEF", `AKIA[0-9A-Z]{16}`},
		{"ASIA", "AWS_ACCESS_KEY_ID=ASIAABCDEFGHIJKLMN12", `ASIA[0-9A-Z]{16}`},
		{"A3T", "AWS_ID=A3T1234567890ABCDEF", `A3T[A-Z0-9]{16}`},
		{"AGPA", "AWS_ID=AGPA1234567890ABCDEF", `AGPA[A-Z0-9]{16}`},
		{"AIDA", "AWS_ID=AIDA1234567890ABCDEF", `AIDA[A-Z0-9]{16}`},
		{"AROA", "AWS_ID=AROA1234567890ABCDEF", `AROA[A-Z0-9]{16}`},
		{"AIPA", "AWS_ID=AIPA1234567890ABCDEF", `AIPA[A-Z0-9]{16}`},
		{"ANPA", "AWS_ID=ANPA1234567890ABCDEF", `ANPA[A-Z0-9]{16}`},
		{"ANVA", "AWS_ID=ANVA1234567890ABCDEF", `ANVA[A-Z0-9]{16}`},

		// GCP
		{
			"service_account_block",
			`"type": "service_account", "private_key_id": "abcdefabcdefabcdefabcdefabcdefab", "private_key": "-----BEGIN PRIVATE KEY-----foobarbarfoo"`,
			`"type":\s*"service_account".*"private_key_id":\s*"[a-f0-9]+".*"private_key":\s*"-----BEGIN PRIVATE KEY-----[^"]+"`,
		},
		{"AIza", `AIzaAbCdEfGhIjKlMnOpQrStUvWxYz123456789012345`, `AIza[0-9A-Za-z\-_]{35}`},

		// GCP service account keys
		{"private_key_id =", `private_key_id = "abcdefabcdefabcdefabcdefabcdefab"`, `private_key_id\s*=\s*"[a-f0-9]{32}"`},
		{"private_key_id :", `"private_key_id": "abcdefabcdefabcdefabcdefabcdefab"`, `"private_key_id"\s*:\s*"[a-f0-9]{32}"`},
		{"private_key :", `"private_key": "-----BEGIN PRIVATE KEY-----fooooobar"`, `"private_key"\s*:\s*"(-----BEGIN PRIVATE KEY-----[^"]+)"`},

		// Azure
		{"AccountKey", `AccountKey=ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=`, `AccountKey=([A-Za-z0-9+/=]{88})`},
		{"Azure triple-32", `abcdefabcdefabcdefabcdefabcdefab-abcdefabcdefabcdefabcdefabcdefab-abcdefabcdefabcdefabcdefabcdefab`, `[0-9a-f]{32}-[0-9a-f]{32}-[0-9a-f]{32}`},

		// GitHub/GitLab
		{"ghp", `ghp_abcdefghijklmnopqrstuvwxyz0123456789abcd1234abcd`, `ghp_[A-Za-z0-9_]{36,255}`},
		{"ghu", `ghu_abcdefghijklmnopqrstuvwxyz0123456789abcd1234abcd`, `ghu_[A-Za-z0-9_]{36,255}`},
		{"ghs", `ghs_abcdefghijklmnopqrstuvwxyz0123456789abcd1234abcd`, `ghs_[A-Za-z0-9_]{36,255}`},
		{"glpat", `glpat-abcdefghijklmnopqrstuvwx`, `glpat-[0-9a-zA-Z\-\_]{20}`},

		// Slack
		{"xoxb", `xoxb-1234567890abcdefg`, `xox[baprs]-([0-9a-zA-Z]{10,48})?`},

		// Stripe
		{"sk_live", `sk_live_abcdefghijklmnopqrstuvwxyzABCDEF12`, `sk_live_[0-9a-zA-Z]{24}`},
		{"pk_live", `pk_live_abcdefghijklmnopqrstuvwxyzABCDEF12`, `pk_live_[0-9a-zA-Z]{24}`},

		// SendGrid
		{"sendgrid", `SG.abcdefghijklmnopqrstuv.ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi1234567890123456789abcdefghijklmno`, `SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}`},

		// Google OAuth/Client
		{"ya29", `ya29.A0ARrdaM-0123456789abcdefGHIJKLMNOPQRSTUVWXYZ`, `ya29\.[0-9A-Za-z\-_]+`},
		{"google_clientid", "123456789012-abcdefghijklmnopqrstuvwxyzABCDEF.apps.googleusercontent.com", `[0-9]+-([0-9A-Za-z_]{32})\.apps\.googleusercontent\.com`},

		// Private Keys
		{"private_key_block", `-----BEGIN RSA PRIVATE KEY----- anything -----END RSA PRIVATE KEY-----`, `-----BEGIN (RSA|DSA|EC|OPENSSH|PRIVATE|ENCRYPTED) PRIVATE KEY-----`},

		// JWT
		{"jwt_secret", `JWT_SECRET=supersecretkey`, `JWT_SECRET(.{0,10})?([=:])(.{0,100})`},
		{"jwt_token", `eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6.abcdefghij`, `eyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}`},
	}

	// ensure all defaultPatterns are covered
	if len(tests) != len(secrets.DefaultRuleSet().FullMaskPatterns) {
		t.Fatalf("number of tests (%d) != number of patterns (%d): possible drift, please update testcases", len(tests), len(secrets.DefaultRuleSet().FullMaskPatterns))
	}

	for i, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ms, err := rs.ScanReader(strings.NewReader(tt.input), "test.txt")
			if err != nil {
				t.Fatalf("ScanReader error: %v", err)
			}
			found := false
			for _, m := range ms {
				if m.Pattern == tt.pattern {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("pattern %d (%s) not detected. Input: %q, got: %+v", i, tt.pattern, tt.input, ms)
			}
		})
	}
}
