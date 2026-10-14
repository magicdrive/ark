package secretscan_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/secrets"
	"github.com/magicdrive/ark/internal/secretscan"
)

// leaks are the secret values of the fixture: no output may hold one.
var leaks = []string{"horse", "zebra42", "hunter2", "acme_tok_abcdEFGH", "ignored-value", "outside-value"}

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// fixture is a repository with secrets in several files, a configuration,
// an excluded file, a binary file, git metadata, and symlinks inside and out.
func fixture(t *testing.T) (root string, linked bool) {
	t.Helper()
	root = t.TempDir()
	outside := t.TempDir()
	write(t, outside, map[string]string{"x.txt": "password: outside-value\n"})
	write(t, root, map[string]string{
		".env":                    "PASSWORD=correct horse battery\nAPI_KEY=zebra42\n",
		"app/main.go":             "package app\n\nfunc f() {\n\ttoken := next()\n\tbypass := true\n}\n",
		"infra/db.tf":             "resource \"a_b\" \"x\" {\n  password = var.db\n  api_key  = \"hunter2-literal\"\n}\n",
		"notes.txt":               "acme = acme_tok_abcdEFGH\n",
		"private.txt":             "secret: ignored-value\n",
		".arkignore":              "private.txt\n",
		".git/config":             "token = hunter2-git\n",
		"bin.dat":                 "password=hunter2\x00binary",
		".ark-secrets-rules.toml": "version = 1\n[[override]]\nid = \"generic-assignment-pass\"\nenabled = false\nreason = \"flags\"\n[[rule]]\nid = \"acme-token\"\npattern = 'acme_tok_[A-Za-z0-9]{8}'\n",
	})
	linked = os.Symlink(filepath.Join(outside, "x.txt"), filepath.Join(root, "external.txt")) == nil &&
		os.Symlink("notes.txt", filepath.Join(root, "alias.txt")) == nil
	if !linked && os.Getenv("ARK_REQUIRE_SYMLINKS") != "" {
		t.Fatal("ARK_REQUIRE_SYMLINKS: cannot create symlinks")
	}
	return root, linked
}

func locations(r *secretscan.Report) []string {
	var out []string
	for _, f := range r.Findings {
		ex := ""
		if f.Excluded {
			ex = " excluded"
		}
		out = append(out, fmt.Sprintf("%s:%d:%d %s%s", f.Path, f.Line, f.Column, f.RuleID, ex))
	}
	return out
}

// The scan reports what the repository's policy masks, file by file, in
// path, line, column and rule order: the configured rule, not the disabled
// one; a .tf file's reference kept, its literal found; no excluded, binary or
// git file; a symlink inside followed, one outside listed and not read.
func TestScan(t *testing.T) {
	root, linked := fixture(t)
	r, err := secretscan.Scan(root, secretscan.Options{AllowGitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		".env:1:10 generic-assignment-password",
		".env:2:9 generic-assignment-api-key",
		"app/main.go:4:11 generic-assignment-token",
		"infra/db.tf:3:15 generic-assignment-api-key",
		"notes.txt:1:8 acme-token",
	}
	if linked {
		want = slices.Insert(want, 2, "alias.txt:1:8 acme-token")
	}
	if got := locations(r); !slices.Equal(got, want) {
		t.Errorf("findings\n got %q\nwant %q", got, want)
	}
	if r.Skipped != 1 || len(r.Errors) != 0 {
		t.Errorf("skipped %d, errors %v", r.Skipped, r.Errors)
	}
	if linked && !slices.Equal(r.Unfollowed, []string{"external.txt"}) {
		t.Errorf("unfollowed %q, want [external.txt]", r.Unfollowed)
	}

	inc, err := secretscan.Scan(root, secretscan.Options{AllowGitignore: true, IncludeIgnored: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := locations(inc); !slices.Contains(got, "private.txt:1:9 generic-assignment-secret excluded") || slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, ".git/") || strings.Contains(s, "external") }) {
		t.Errorf("with ignored files: %q", got)
	}
	if len(inc.Findings) != len(r.Findings)+1 {
		t.Errorf("with ignored files: %d findings, want %d", len(inc.Findings), len(r.Findings)+1)
	}
}

// Every finding is one Policy.Find finds in that file — what masking masks —
// and Find finds nothing the scan leaves out (but masks of empty values).
func TestScan_IsPolicyFind(t *testing.T) {
	root, _ := fixture(t)
	r, err := secretscan.Scan(root, secretscan.Options{AllowGitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{".env", "app/main.go", "infra/db.tf", "notes.txt"} {
		b, _ := os.ReadFile(filepath.Join(root, file))
		var want []string
		for _, f := range r.Config.Policy.Find(string(b), secrets.Mode{Syntax: secrets.SyntaxForFile(file)}) {
			if f.End > f.Start {
				want = append(want, fmt.Sprintf("%s:%d:%d %s", file, f.Line, f.Column, f.RuleID))
			}
		}
		var got []string
		for _, l := range locations(r) {
			if strings.HasPrefix(l, file+":") {
				got = append(got, l)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: scan %q, Policy.Find %q", file, got, want)
		}
	}
}

// No output holds a value: text, JSON, suggestions, the rule list.
func TestScan_OutputsHoldNoValue(t *testing.T) {
	root, _ := fixture(t)
	r, err := secretscan.Scan(root, secretscan.Options{AllowGitignore: true, IncludeIgnored: true})
	if err != nil {
		t.Fatal(err)
	}
	var text, js, rules bytes.Buffer
	r.WriteText(&text, true)
	if err := r.WriteJSON(&js, true); err != nil {
		t.Fatal(err)
	}
	loaded, err := secretscan.LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	secretscan.WriteRules(&rules, loaded, false)
	for name, out := range map[string]string{"text": text.String(), "json": js.String(), "rules": rules.String()} {
		for _, leak := range leaks {
			if strings.Contains(out, leak) {
				t.Errorf("%s output holds %q:\n%s", name, leak, out)
			}
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(js.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "root", "config", "includeIgnored", "summary", "findings", "errors", "unfollowedSymlinks", "suggestions"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("JSON has no %q: %s", key, js.String())
		}
	}
	again, _ := secretscan.Scan(root, secretscan.Options{AllowGitignore: true, IncludeIgnored: true})
	var js2 bytes.Buffer
	again.WriteJSON(&js2, true)
	if js.String() != js2.String() {
		t.Error("two scans of one repository differ")
	}
}

// Suggestions sort files and rules: a credentials file or one mostly of
// secrets to exclude, a file with a few values to leave to masking, an
// assignment rule finding mostly code to review.
func TestScan_Suggestions(t *testing.T) {
	root := t.TempDir()
	var code strings.Builder
	code.WriteString("package app\n\nfunc f() {\n")
	for i := range secretscan.CodeFindings {
		fmt.Fprintf(&code, "\tv%d_token := next()\n", i)
	}
	code.WriteString("}\n")
	write(t, root, map[string]string{
		".env":        "PASSWORD=x\n",
		"conf.yml":    "password: a\ntoken: b\nsecret: c\nname: d\n",
		"readme.md":   "Set password: a in the file.\n\nIt is read at startup, and many other lines.\nMore.\nMore.\n",
		"app/main.go": code.String(),
	})
	r, err := secretscan.Scan(root, secretscan.Options{AllowGitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	s := r.Suggest()
	var exclude, maskOnly, review []string
	for _, f := range s.Exclude {
		exclude = append(exclude, f.Path)
	}
	for _, f := range s.MaskOnly {
		maskOnly = append(maskOnly, f.Path)
	}
	for _, rs := range s.ReviewRules {
		review = append(review, rs.RuleID)
	}
	if !slices.Equal(exclude, []string{".env", "conf.yml"}) || !slices.Equal(maskOnly, []string{"app/main.go", "readme.md"}) || !slices.Equal(review, []string{"generic-assignment-token"}) {
		t.Errorf("exclude %q, mask only %q, review %q", exclude, maskOnly, review)
	}
}

// HTML and CSS files are no source code to the suggestions: one dense with
// findings is suggested for exclusion like a configuration file.
func TestScan_SuggestionsHTMLIsNotCode(t *testing.T) {
	for _, name := range []string{"page.html", "theme.css"} {
		root := t.TempDir()
		write(t, root, map[string]string{
			name: "password: a\ntoken: b\nsecret: c\nname: d\n",
		})
		r, err := secretscan.Scan(root, secretscan.Options{AllowGitignore: true})
		if err != nil {
			t.Fatal(err)
		}
		s := r.Suggest()
		if len(s.Exclude) != 1 || s.Exclude[0].Path != name {
			t.Errorf("exclude %+v, want %s", s.Exclude, name)
		}
	}
}

// A configuration that cannot be compiled stops the scan: no report with
// the rules it could not read.
func TestScan_BrokenConfiguration(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"ark-secrets-rules.toml": "version = 1\n[[rule]]\nid = \"a\"\npattern = '(hunter2'\n", "a.txt": "x"})
	if _, err := secretscan.Scan(root, secretscan.Options{}); err == nil || !strings.Contains(err.Error(), "pattern does not compile") || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("Scan: %v", err)
	}
	if _, err := secretscan.LoadConfig(root); err == nil {
		t.Error("LoadConfig accepted it")
	}
}
