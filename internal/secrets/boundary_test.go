package secrets_test

import (
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/secrets"
)

// What follows a kept reference in the same value is the value too: a
// reference is kept only when nothing but a comment (GitHub Actions), or a
// comment, a separator or a closing bracket (Terraform) follows it.
func TestMaskAllIn_NoLiteralAfterAKeptReference(t *testing.T) {
	for _, tc := range []struct {
		syntax secrets.Syntax
		in     string
		leak   string
	}{
		{"", `token: ${{ secrets.A }} ${{ 'hunter2' }}`, "hunter2"},
		{"", `token: ${{ secrets.A }}, x: hunter2`, "hunter2"},
		{"", `token: ${{ secrets.A }} hunter2`, "hunter2"},
		{"", "token: ${{ secrets.A }}\thunter2", "hunter2"},
		{secrets.SyntaxTerraform, `  password = var.x != "" ? var.x : "default-secret"`, "default-secret"},
		{secrets.SyntaxTerraform, `  password = var.x == "" ? "default-secret" : var.x`, "default-secret"},
		{secrets.SyntaxTerraform, `  password = var.x hunter2`, "hunter2"},
		{secrets.SyntaxTerraform, `  password = var.x+"hunter2"`, "hunter2"},
	} {
		if got := secrets.MaskAllIn(tc.in, tc.syntax); strings.Contains(got, tc.leak) {
			t.Errorf("MaskAllIn(%q, %q) = %q: leaks %q", tc.in, tc.syntax, got, tc.leak)
		}
	}
}

// Value boundaries: a CRLF line keeps its "\r" and its reference; a
// Terraform reference is kept before a comment, a separator or a closing
// bracket, and masked to the end of the line before anything else.
func TestMaskAllIn_ValueBoundaries(t *testing.T) {
	const m = "*****MASKED*****"
	for _, tc := range []struct {
		syntax   secrets.Syntax
		in, want string
	}{
		{"", "token: ${{ secrets.A }}\r", "token: ${{ secrets.A }}\r"},
		{"", "token: abcdefg\r", "token: " + m + "\r"},
		{"", "a: 1\r\ntoken: ${{ secrets.A }}\r\napi_key: 'x'\r\n", "a: 1\r\ntoken: ${{ secrets.A }}\r\napi_key: '" + m + "'\r\n"},
		{secrets.SyntaxTerraform, "  password = var.x\r", "  password = var.x\r"},
		{"", "token: ${{ env.A }}\t# note", "token: ${{ env.A }}\t# note"},
		{"", "token: ${{ env.A }} #note", "token: ${{ env.A }} #note"},
		{"", "token: ${{ env.A }}#note", "token: " + m},
		{secrets.SyntaxTerraform, "  password = var.x # from tfvars", "  password = var.x # from tfvars"},
		{secrets.SyntaxTerraform, "  password = var.x // from tfvars", "  password = var.x // from tfvars"},
		{secrets.SyntaxTerraform, "  creds = { password = var.x, user = \"u\" }", "  creds = { password = var.x, user = \"u\" }"},
		{secrets.SyntaxTerraform, "  creds = { password = var.x }", "  creds = { password = var.x }"},
		{secrets.SyntaxTerraform, "  creds = { password = var.x}", "  creds = { password = " + m},
		{secrets.SyntaxTerraform, "  password = coalesce(var.x, \"fallback\")", "  password = " + m},
		{secrets.SyntaxTerraform, "  password = var.x != \"\" ? var.x : \"default\"", "  password = " + m},
		{secrets.SyntaxTerraform, "  password = local.passwords[0] # first", "  password = local.passwords[0] # first"},
	} {
		if got := secrets.MaskAllIn(tc.in, tc.syntax); got != tc.want {
			t.Errorf("MaskAllIn(%q, %q)\n got %q\nwant %q", tc.in, tc.syntax, got, tc.want)
		}
	}
}

// A value that continues past its key's line — a heredoc, a YAML block
// scalar — is masked on every line of its body, and only there: the body
// ends at the heredoc's terminator, or at the first line indented no deeper
// than the key, wherever lines break ("\n" or "\r\n"; "␤" in compact text,
// TestMaskCompactIn_LineBreakSymbol).
func TestMaskAllIn_MultiLineValues(t *testing.T) {
	const m = "*****MASKED*****"
	for _, tc := range []struct {
		syntax   secrets.Syntax
		in, want string
	}{
		{secrets.SyntaxTerraform, "  password = <<EOT\nhunter2\n  more\n\nEOT\nuser = 1\n", "  password = " + m + "\n" + m + "\n  " + m + "\n\nEOT\nuser = 1\n"},
		{"", "  password = <<-EOT\n    hunter2\n    EOT\n  user = 1\n", "  password = " + m + "\n    " + m + "\n    EOT\n  user = 1\n"},
		{"", "$password = <<<'EOT'\n  hunter2\n  EOT;\n$x = 1;", "$password = " + m + "\n  " + m + "\n  EOT;\n$x = 1;"},
		{"", "password = <<~EOT\n  hunter2\nEOT\n", "password = " + m + "\n  " + m + "\nEOT\n"},
		{"", "token: <<\"END\"\nhunter2\nEND\nx: 1", "token: " + m + "\n" + m + "\nEND\nx: 1"},
		{"", "password: |\n  hunter2\n\n  line2\nnext: visible\n", "password: " + m + "\n  " + m + "\n\n  " + m + "\nnext: visible\n"},
		{"", "env:\n  API_KEY: >-\n    hunter2\n  OTHER: visible\n", "env:\n  API_KEY: " + m + "\n    " + m + "\n  OTHER: visible\n"},
		// The block indicator line is masked to its end, comment included:
		// a plain value runs to the end of its line (plainValueEnd).
		{"", "- token: |2 # c\n    hunter2\n  same: visible\n", "- token: " + m + "\n    " + m + "\n  same: visible\n"},
		{"", "password: |\r\n  hunter2\r\nnext: visible\r\n", "password: " + m + "\r\n  " + m + "\r\nnext: visible\r\n"},
		// Not a block: a quoted value, a non-secret key, a block after text.
		{"", "password: \"|\"\n  visible\n", "password: \"" + m + "\"\n  visible\n"},
		{"", "run: |\n  echo visible\n", "run: |\n  echo visible\n"},
		{"", "password: x |\n  visible\n", "password: " + m + "\n  visible\n"},
	} {
		if got := secrets.MaskAllIn(tc.in, tc.syntax); got != tc.want {
			t.Errorf("MaskAllIn(%q, %q)\n got %q\nwant %q", tc.in, tc.syntax, got, tc.want)
		}
	}
}

// In compact text (MaskCompactIn) "␤" is a line break: a value ends at it,
// a reference before it is kept, a block's body runs across it. Elsewhere
// (MaskAllIn) it is a character of the line, and a value runs through it.
func TestMaskCompactIn_LineBreakSymbol(t *testing.T) {
	const m = "*****MASKED*****"
	for _, tc := range []struct {
		syntax   secrets.Syntax
		in, want string
	}{
		{"", "token: ${{ secrets.A }}␤next: value␤password = hunter2␤tail", "token: ${{ secrets.A }}␤next: value␤password = " + m + "␤tail"},
		{"", "password = var.x␤other = 1", "password = " + m + "␤other = 1"},
		{secrets.SyntaxTerraform, "  password = var.x␤  user = \"u\"", "  password = var.x␤  user = \"u\""},
		{"", "password: |␤  hunter2␤next: visible", "password: " + m + "␤  " + m + "␤next: visible"},
		{"", "password: correct horse␤next: visible\nb: 1", "password: " + m + "␤next: visible\nb: 1"},
	} {
		if got := secrets.MaskCompactIn(tc.in, tc.syntax); got != tc.want {
			t.Errorf("MaskCompactIn(%q, %q)\n got %q\nwant %q", tc.in, tc.syntax, got, tc.want)
		}
	}
	for _, in := range []string{"password: hunter2␤tail-leak", "token: ${{ secrets.A }}␤tail-leak", "password: \"a␤tail-leak\""} {
		if got := secrets.MaskAllIn(in, ""); strings.Contains(got, "tail-leak") {
			t.Errorf("MaskAllIn(%q) = %q: a value ends at a \"␤\" of the file", in, got)
		}
	}
}
