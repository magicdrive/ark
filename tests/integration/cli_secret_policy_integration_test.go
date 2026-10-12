package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// secretPolicyFixture is a repository with a secret rules configuration:
// it disables the pass keyword and adds a pattern rule; a .tf file with a
// reference, and a .tf link to a text file with one.
func secretPolicyFixture(t *testing.T, config string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"app.env":     "PASSWORD=correct horse battery\nbypass = true\nACME=acme_tok_abcdEFGH\nGH=ghp_abcdefghijklmnopqrstuvwxyz0123456789abcd\n",
		"infra/db.tf": "resource \"aws_db_instance\" \"x\" {\n  password = var.db_password\n}\n",
		"creds.txt":   "password = var.from_text\n",
	}
	if config != "" {
		files[".ark-secrets-rules.toml"] = config
	}
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("creds.txt", filepath.Join(root, "alias.tf")); err != nil && os.Getenv("ARK_REQUIRE_SYMLINKS") != "" {
		t.Fatal(err)
	}
	return root
}

const policyConfig = `version = 1
[[override]]
id = "generic-assignment-pass"
enabled = false
reason = "bypass flags are no secrets"

[[rule]]
id = "acme-token"
pattern = 'acme_tok_[A-Za-z0-9]{8}'
`

// The dump applies the repository's secret rules in every format: a rule it
// disables no longer masks, a rule it adds masks, every other rule masks as
// before, and a .tf file — not a .tf link — keeps its references.
func TestCLISecretPolicy_EveryDumpFormat(t *testing.T) {
	bin := buildArk(t)
	root := secretPolicyFixture(t, policyConfig)
	_, linkErr := os.Lstat(filepath.Join(root, "alias.tf"))
	for _, format := range []string{"plaintext", "markdown", "xml", "arklite"} {
		out := filepath.Join(t.TempDir(), "dump")
		cmd := exec.Command(bin, "-S", "-f", format, "-o", out, root)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", format, err, b)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		dump := string(b)
		for _, leak := range []string{"horse", "battery", "acme_tok_abcdEFGH", "ghp_abcdefghijklmnopqrstuvwxyz0123456789abcd", "var.from_text"} {
			if leak == "var.from_text" && linkErr != nil {
				continue
			}
			if strings.Contains(dump, leak) {
				t.Errorf("%s: %q not masked:\n%s", format, leak, dump)
			}
		}
		for _, keep := range []string{"bypass = true", "password = var.db_password"} {
			if !strings.Contains(dump, keep) {
				t.Errorf("%s: %q masked:\n%s", format, keep, dump)
			}
		}
	}

	// Without the configuration, the recommended rules: bypass is masked.
	out := filepath.Join(t.TempDir(), "dump")
	plain := secretPolicyFixture(t, "")
	if b, err := exec.Command(bin, "-S", "-o", out, plain).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	if b, _ := os.ReadFile(out); strings.Contains(string(b), "bypass = true") || !strings.Contains(string(b), "password = var.db_password") {
		t.Errorf("recommended dump:\n%s", b)
	}
}

// A configuration that cannot be compiled fails the dump — exit status not
// 0, no dump written, the file and the problem named, no configured value
// repeated — rather than dumping with the rules it could not read.
func TestCLISecretPolicy_BrokenConfigurationFailsTheDump(t *testing.T) {
	bin := buildArk(t)
	for _, silent := range []bool{true, false} {
		root := secretPolicyFixture(t, "version = 1\n[[rule]]\nid = \"acme\"\npattern = '(acme_tok_SECRETVALUE'\n")
		out := filepath.Join(t.TempDir(), "dump")
		args := []string{"-o", out, root}
		if silent {
			args = append([]string{"-S"}, args...)
		}
		b, err := exec.Command(bin, args...).CombinedOutput()
		if err == nil {
			t.Fatalf("silent=%v: the dump succeeded:\n%s", silent, b)
		}
		msg := string(b)
		if !strings.Contains(msg, ".ark-secrets-rules.toml") || !strings.Contains(msg, "pattern does not compile") {
			t.Errorf("silent=%v: message %q", silent, msg)
		}
		if strings.Contains(msg, "SECRETVALUE") {
			t.Errorf("silent=%v: the message repeats the pattern: %q", silent, msg)
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("silent=%v: a dump was written", silent)
		}
	}
	// With masking off the configuration is not read.
	root := secretPolicyFixture(t, "not toml [")
	out := filepath.Join(t.TempDir(), "dump")
	if b, err := exec.Command(bin, "-S", "--mask-secrets", "off", "-o", out, root).CombinedOutput(); err != nil {
		t.Errorf("masking off: %v\n%s", err, b)
	}
}

// toolText returns the text of a tools/call response.
func toolText(t *testing.T, raw string) (string, bool) {
	t.Helper()
	var resp struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
			IsError bool                    `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil || len(resp.Result.Content) == 0 {
		t.Fatalf("response %s: %v", raw, err)
	}
	return resp.Result.Content[0].Text, resp.Result.IsError
}

// dumpSections splits a plain-text dump into its files' contents.
func dumpSections(dump string) map[string]string {
	out := map[string]string{}
	var name string
	var body []string
	flush := func() {
		if name != "" {
			out[name] = strings.TrimRight(strings.Join(body, "\n"), "\n")
		}
	}
	for _, line := range strings.Split(dump, "\n") {
		if strings.HasPrefix(line, "=== ") && strings.HasSuffix(line, " ===") {
			flush()
			name, body = filepath.Base(strings.TrimSuffix(strings.TrimPrefix(line, "=== "), " ===")), nil
			continue
		}
		body = append(body, line)
	}
	flush()
	return out
}

// The dump and the MCP server read one configuration and mask alike: with
// the operator's permission the server masks every file exactly as the dump
// does; without it the server leaves the disabling out, says so once on
// stderr — naming the rule, no value — and still applies the added rule.
func TestSecretPolicy_DumpAndServerAgree(t *testing.T) {
	bin := buildArk(t)
	root := secretPolicyFixture(t, policyConfig)
	out := filepath.Join(t.TempDir(), "dump")
	if b, err := exec.Command(bin, "-S", "-f", "plaintext", "-o", out, root).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	b, _ := os.ReadFile(out)
	sections := dumpSections(string(b))

	c := startServer(t, root, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache", "--secret-rule-overrides", "on")
	c.handshake()
	for _, file := range []string{"app.env", "infra/db.tf"} {
		text, isErr := toolText(t, rawCall(t, c, "tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": file}}))
		if isErr {
			t.Fatalf("%s: %s", file, text)
		}
		want, ok := sections[filepath.Base(file)]
		if !ok || want == "" {
			t.Fatalf("%s: not in the dump: %q", file, sections)
		}
		if got := strings.TrimRight(text, "\n"); got != want {
			t.Errorf("%s: the server and the dump mask differently\nserver %q\ndump   %q", file, got, want)
		}
	}
	stopServer(c)

	c = startServer(t, root, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	var text string
	for range 2 {
		text, _ = toolText(t, rawCall(t, c, "tools/call", map[string]any{"name": "get_file_content", "arguments": map[string]any{"path": "app.env"}}))
	}
	stderr := stopServer(c)
	if strings.Contains(text, "bypass = true") || strings.Contains(text, "acme_tok_abcdEFGH") {
		t.Errorf("without the operator's permission:\n%s", text)
	}
	if strings.Count(stderr, "generic-assignment-pass") != 1 || strings.Contains(stderr, "acme_tok") || strings.Contains(stderr, "horse") {
		t.Errorf("stderr %q: want one notice naming the rule, no value", stderr)
	}
}
