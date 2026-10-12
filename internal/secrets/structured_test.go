package secrets_test

import (
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/secrets"
)

const masked = "*****MASKED*****"

// A value that only names another value — a GitHub Actions context
// reference, or (in a Terraform file) a Terraform reference — is kept as
// written; any other value of a secret-like key is masked, a GitHub Actions
// expression whole, never cut at one of its spaces.
func TestMaskAllIn_StructuredValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		syntax secrets.Syntax
		in     string
		want   string
	}{
		// GitHub Actions: references are kept.
		{"actions reference", "", `      DD_API_KEY: ${{ secrets.DD_API_KEY }}`, `      DD_API_KEY: ${{ secrets.DD_API_KEY }}`},
		{"actions reference, token key", "", `token: ${{ secrets.GITHUB_TOKEN }}`, `token: ${{ secrets.GITHUB_TOKEN }}`},
		{"actions github context", "", `token: ${{ github.token }}`, `token: ${{ github.token }}`},
		{"actions reference without spaces", "", `token: ${{secrets.A}}`, `token: ${{secrets.A}}`},
		{"actions reference, comment after", "", `token: ${{ secrets.A }} # rotated monthly`, `token: ${{ secrets.A }} # rotated monthly`},
		{"actions reference, comment after tab", "", "token: ${{ env.A }}\t# note", "token: ${{ env.A }}\t# note"},
		{"actions reference, quoted", "", `token: "${{ secrets.A }}"`, `token: "${{ secrets.A }}"`},
		{"actions step output", "", `api_key: ${{ steps.keys.outputs.api-key }}`, `api_key: ${{ steps.keys.outputs.api-key }}`},

		// GitHub Actions: anything but one plain reference is masked whole.
		{"actions fallback literal", "", `token: ${{ secrets.A || 'fallback-literal' }}`, `token: ` + masked},
		{"actions literal only", "", `token: ${{ 'literal-secret' }}`, `token: ` + masked},
		{"actions braces in a literal", "", `token: ${{ format('{0}}', secrets.A) }}`, `token: ` + masked},
		{"actions quote escape in a literal", "", `token: ${{ format('it''s }} x', secrets.A) }}`, `token: ` + masked},
		{"actions index", "", `token: ${{ secrets['A'] }}`, `token: ` + masked},
		{"actions reference then text", "", `token: ${{ secrets.A }}abc123`, `token: ` + masked},
		{"actions two references", "", `token: ${{ secrets.A }}${{ secrets.B }}`, `token: ` + masked},
		// A YAML value runs to the end of its line: ", other: x" is part of it.
		{"actions reference then a comma", "", `token: ${{ env.A }}, other: x`, `token: ` + masked},
		{"actions unterminated", "", `token: ${{ secrets.A`, `token: ` + masked},
		{"actions unterminated with text", "", `token: ${{ secrets.A hunter2 tail`, `token: ` + masked},
		{"actions unknown context", "", `token: ${{ hunter2.value }}`, `token: ` + masked},
		{"actions quoted with text", "", `token: "${{ secrets.A }} x"`, `token: "` + masked + `"`},

		// Plain values stay masked.
		{"plain value", "", `token: abcdefg`, `token: ` + masked},
		{"terraform reference outside terraform", "", `password = var.db_password`, `password = ` + masked},

		// Terraform: references are kept in a Terraform file only.
		{"terraform variable", secrets.SyntaxTerraform, `  password = var.db_password`, `  password = var.db_password`},
		{"terraform resource attribute", secrets.SyntaxTerraform, `  master_password = random_password.api_token.result`, `  master_password = random_password.api_token.result`},
		{"terraform data source", secrets.SyntaxTerraform, `  token = data.aws_secretsmanager_secret_version.db.secret_string`, `  token = data.aws_secretsmanager_secret_version.db.secret_string`},
		{"terraform module output", secrets.SyntaxTerraform, `  api_key = module.keys.api_key`, `  api_key = module.keys.api_key`},
		{"terraform local with index", secrets.SyntaxTerraform, `  password = local.passwords[0]`, `  password = local.passwords[0]`},
		{"terraform bare word", secrets.SyntaxTerraform, `  password = hunter2`, `  password = ` + masked},
		{"terraform two-part word", secrets.SyntaxTerraform, `  password = my.secret`, `  password = ` + masked},
		{"terraform resource without attribute", secrets.SyntaxTerraform, `  password = random_password.x`, `  password = ` + masked},
		{"terraform string key index", secrets.SyntaxTerraform, `  password = var.list["k"]`, `  password = ` + masked},
		{"terraform quoted literal", secrets.SyntaxTerraform, `  password = "hunter2"`, `  password = "` + masked + `"`},
		{"terraform interpolation", secrets.SyntaxTerraform, `  password = "${var.x}-suffix"`, `  password = "` + masked + `"`},
		{"terraform function", secrets.SyntaxTerraform, `  password = base64decode("aHVudGVyMg==")`, `  password = ` + masked},
		{"terraform plain value", secrets.SyntaxTerraform, `token: abcdefg`, `token: ` + masked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := secrets.MaskAllIn(tc.in, tc.syntax); got != tc.want {
				t.Errorf("MaskAllIn(%q, %q)\n got %q\nwant %q", tc.in, tc.syntax, got, tc.want)
			}
		})
	}
}

// A reference kept as written is still subject to every secret pattern, and
// an expression or reference never shields a secret written next to it.
func TestMaskAllIn_PatternsStillApply(t *testing.T) {
	for _, tc := range []struct {
		syntax secrets.Syntax
		in     string
	}{
		{"", `token: ${{ secrets.AKIA1234567890ABCDEF }}`},
		{"", `token: ${{ secrets.A }} AKIA1234567890ABCDEF`},
		{"", `token: ${{ secrets.A || 'AKIA1234567890ABCDEF' }}`},
		{"", `env: ${{ secrets.A }} ghp_abcdefghijklmnopqrstuvwxyz0123456789abcd`},
		{secrets.SyntaxTerraform, `  token = aws_db_instance.AKIA1234567890ABCDEF.x`},
		{secrets.SyntaxTerraform, `  password = var.x # AKIA1234567890ABCDEF`},
		{secrets.SyntaxTerraform, `  password = var.x eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NSJ9.c2lnbmF0dXJlc2ln`},
	} {
		got := secrets.MaskAllIn(tc.in, tc.syntax)
		for _, secret := range []string{"AKIA1234567890ABCDEF", "ghp_abcdefghijklmnopqrstuvwxyz0123456789abcd", "eyJhbGciOiJIUzI1NiJ9"} {
			if strings.Contains(got, secret) {
				t.Errorf("MaskAllIn(%q, %q) = %q: leaks %s", tc.in, tc.syntax, got, secret)
			}
		}
	}
	block := "key: |\n-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----\n"
	for _, syntax := range []secrets.Syntax{"", secrets.SyntaxTerraform} {
		if got := secrets.MaskAllIn(block, syntax); strings.Contains(got, "MIIEow") {
			t.Errorf("MaskAllIn(%q) leaks the private key:\n%s", syntax, got)
		}
	}
}

// MaskAll is MaskAllIn of text of no known syntax.
func TestMaskAll_IsMaskAllInUnknown(t *testing.T) {
	in := "password = var.db_password\ntoken: ${{ secrets.A }}\napi_key: \"x\"\n"
	if a, b := secrets.MaskAll(in), secrets.MaskAllIn(in, secrets.SyntaxUnknown); a != b {
		t.Errorf("MaskAll and MaskAllIn(unknown) differ:\n%s\n%s", a, b)
	}
}
