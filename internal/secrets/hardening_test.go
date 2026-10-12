package secrets_test

import (
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/secrets"
)

// No part of a secret-like key's value leaks, whatever follows its first
// word: a value of several words, a value written as a quoted key's (JSON,
// PHP, Python), a value a quote does not end (an escaped or doubled quote,
// shell concatenation, an operator), a value cut at a comma that starts no
// assignment, or a value with a line-break symbol in it.
func TestMaskAllIn_NoPartOfAValueLeaks(t *testing.T) {
	for _, tc := range []struct {
		syntax secrets.Syntax
		in     string
		leaks  []string
	}{
		// Several words.
		{"", "password: correct horse battery", []string{"horse", "battery"}},
		{"", "api_key: some long token value", []string{"long", "value"}},
		{"", "secret: word1 word2 word3", []string{"word2", "word3"}},
		{"", "password: correct horse battery # comment", []string{"horse", "battery"}},
		{"", "PASSWORD=correct horse battery", []string{"horse", "battery"}},
		{"", "  password:\tcorrect\thorse", []string{"horse"}},
		{"", "- password: correct horse", []string{"horse"}},
		{secrets.SyntaxTerraform, "  password = correct horse", []string{"horse"}},
		// A comma that starts no assignment is part of the value.
		{"", "password: alpha, bravo-leak", []string{"bravo-leak"}},
		{"", "password: alpha; bravo-leak", []string{"bravo-leak"}},
		{"", `password = "alpha", "bravo-leak"`, []string{"bravo-leak"}},
		// Quoted keys: JSON, PHP, Python, JavaScript.
		{"", `"password": "hunter2json"`, []string{"hunter2json"}},
		{"", `  "db_password" : "hunter2json",`, []string{"hunter2json"}},
		{"", `{"password":"hunter2json","user":"u"}`, []string{"hunter2json"}},
		{"", `'password' => 'hunter2php',`, []string{"hunter2php"}},
		{"", `$cfg['api_key'] = 'hunter2php';`, []string{"hunter2php"}},
		{"", `creds = {'token': 'hunter2py'}`, []string{"hunter2py"}},
		{"", `{ "password": hunter2bare }`, []string{"hunter2bare"}},
		// A quote that does not end the value.
		{"", `password: 'ab''cd-leak'`, []string{"cd-leak"}},
		{"", `export PASSWORD="correct horse"battery-leak`, []string{"battery-leak"}},
		{"", `export PASSWORD="correct horse" "battery-leak"`, []string{"battery-leak"}},
		{"", `export PASSWORD='a'"b-leak"`, []string{"b-leak"}},
		{"", `password = "abc" + "def-leak"`, []string{"def-leak"}},
		{"", `token = "a" if x else "fallback-leak"`, []string{"fallback-leak"}},
		{"", `token: "${{ secrets.A }}" fallback-leak`, []string{"fallback-leak"}},
		{secrets.SyntaxTerraform, `  password = "a" == "" ? "b" : "fallback-leak"`, []string{"fallback-leak"}},
		// Unterminated and escaped quotes.
		{"", `password: "unterminated leak`, []string{"unterminated", "leak"}},
		{"", `password: "ab\"cd leak"`, []string{"cd leak"}},
		// A line-break symbol in a file is no line break.
		{"", "password: hunter2␤tail-leak", []string{"tail-leak"}},
		{"", "password: \"a␤tail-leak\"", []string{"tail-leak"}},
		// Several secrets on one line.
		{"", "password: one-leak, token: two-leak", []string{"one-leak", "two-leak"}},
		{"", "password=one-leak;token=two-leak", []string{"one-leak", "two-leak"}},
		{"", `{"password":"one-leak","api_key":"two-leak"}`, []string{"one-leak", "two-leak"}},
	} {
		got := secrets.MaskAllIn(tc.in, tc.syntax)
		for _, leak := range tc.leaks {
			if strings.Contains(got, leak) {
				t.Errorf("MaskAllIn(%q, %q) = %q: leaks %q", tc.in, tc.syntax, got, leak)
			}
		}
	}
}

// What a value's mask leaves of a line: the separator that ends a member or
// a statement, and nothing of an empty value; a comment after a plain value
// is masked with it (in YAML or prose " #" may be part of the value).
func TestMaskAllIn_PlainValueStructure(t *testing.T) {
	const m = "*****MASKED*****"
	for _, tc := range []struct{ in, want string }{
		{"\tAccessKey:   accessKey,", "\tAccessKey:   " + m + ","},
		{"  password: params.Password;  ", "  password: " + m + ";  "},
		{"password: alpha, beta,", "password: " + m + ","},
		{"password: correct horse # comment", "password: " + m},
		{"password: correct horse #1 battery", "password: " + m},
		{"elif tt == 'password':", "elif tt == 'password':"},
		{"password:", "password:"},
		{"password:   ", "password:   "},
		{`{"password": "a", "user": "u"}`, `{"password": "` + m + `", "user": "u"}`},
		{`f(password="a", user="u")`, `f(password="` + m + `", user="u")`},
		{`token = "a" # note`, `token = "` + m + `" # note`},
		{`token = "a"#note`, `token = "` + m},
	} {
		if got := secrets.MaskAllIn(tc.in, ""); got != tc.want {
			t.Errorf("MaskAllIn(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
}

// A secret pattern applies to the line as written, before any value is
// masked: a service account's key, a pattern spanning several values, is
// masked whole.
func TestMaskAllIn_PatternsBeforeValues(t *testing.T) {
	line := `"type": "service_account", "private_key_id": "abcdefabcdefabcdefabcdefabcdefab", "private_key": "-----BEGIN PRIVATE KEY-----foobarbarfoo"`
	if got := secrets.MaskAllIn(line, ""); got != "*****MASKED*****" {
		t.Errorf("MaskAllIn(service account) = %q, want the line masked whole", got)
	}
}

// Masking stays linear in a long line (a minified script): a plain value
// running through many separators costs one anchored match attempt at each,
// never a search of the rest of the line, and many quoted values cost no copy
// of it each. The bound is loose — a quadratic masking of these lines takes
// minutes.
func TestMaskAllIn_LongLineIsLinear(t *testing.T) {
	separators := "password:x," + strings.Repeat("a,b;c,", 70_000)
	quoted := strings.Repeat(`{"token":"x","k":"v"},`, 20_000)
	for name, line := range map[string]string{"separators": separators, "quoted values": quoted} {
		done := make(chan string, 1)
		go func() { done <- secrets.MaskAllIn(line, "") }()
		select {
		case got := <-done:
			if strings.Contains(got, `"token":"x"`) || strings.Contains(got, "a,b;c") {
				t.Errorf("%s: a value in the long line is not masked", name)
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("%s: masking a %d KB line took over 20s", name, len(line)/1000)
		}
	}
}
