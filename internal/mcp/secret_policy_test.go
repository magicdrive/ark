package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

const masked = "*****MASKED*****"

// maskRepo is a repository whose files hold secrets the recommended rules
// mask, values only a configured rule masks, and a "pass" key the
// recommended rules mask and a configuration may unmask.
func maskRepo(t *testing.T, config string) (string, *ToolsHandler) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"app/conf.go": "package app\n\n// Conf holds settings.\nfunc Conf() map[string]string {\n\treturn map[string]string{\"password\": \"hunter2go\", \"acme\": \"acme_tok_abcdEFGH\"}\n}\n",
		"app.env":     "PASSWORD=correct horse battery\nbypass = true\nACME=acme_tok_abcdEFGH\n",
		"infra/db.tf": "resource \"aws_db_instance\" \"x\" {\n  password = var.db_password\n}\n",
	}
	if config != "" {
		files[".ark-secrets-rules.toml"] = config
	}
	writeTree(t, root, files)
	return root, maskServer(t, root)
}

func maskServer(t *testing.T, root string, flags ...string) *ToolsHandler {
	t.Helper()
	_, serveOpt, err := commandline.ServerOptParse("test", append([]string{"--root", root}, flags...))
	if err != nil {
		t.Fatal(err)
	}
	return NewToolsHandlerWithCache(root, serveOpt.GeneralOption, nil)
}

const acmeRule = "version = 1\n[[rule]]\nid = \"acme-token\"\npattern = 'acme_tok_[A-Za-z0-9]{8}'\n"

func mustText(t *testing.T, h *ToolsHandler, tool string, args map[string]interface{}) string {
	t.Helper()
	text, isErr := callText(t, h, tool, args)
	if isErr {
		t.Fatalf("%s %v: %s", tool, args, text)
	}
	return text
}

// A configured rule masks in every tool that returns repository text, and
// in resources; the recommended rules still mask; maskSecrets false changes
// nothing; arklite keeps its structure.
func TestSecretPolicy_ConfiguredRuleMasksEverywhere(t *testing.T) {
	root, h := maskRepo(t, acmeRule)
	for _, c := range []struct {
		tool string
		args map[string]interface{}
	}{
		{"get_file_content", map[string]interface{}{"path": "app.env"}},
		{"get_file_content", map[string]interface{}{"path": "app.env", "maskSecrets": false}},
		{"get_symbol", map[string]interface{}{"path": "app/conf.go", "name": "Conf"}},
		{"get_context", map[string]interface{}{"path": ".", "symbol": "Conf"}},
		{"search_in_files", map[string]interface{}{"path": ".", "query": "acme"}},
		{"get_files_arklite", map[string]interface{}{"paths": []interface{}{"app.env", "app/conf.go"}, "maskSecrets": false}},
	} {
		text := mustText(t, h, c.tool, c.args)
		for _, leak := range []string{"acme_tok_abcdEFGH", "horse", "hunter2go"} {
			if strings.Contains(text, leak) {
				t.Errorf("%s %v: %q not masked:\n%s", c.tool, c.args, leak, text)
			}
		}
		if c.tool == "get_files_arklite" && (!strings.Contains(text, "ACME="+masked+"␤") || strings.Count(text, "\n@") != 2) {
			t.Errorf("arklite structure:\n%s", text)
		}
	}
	res, err := NewResourcesHandler(root, h.opt).ReadResource("file://app.env")
	if err != nil || strings.Contains(res.Contents[0].Text, "acme_tok_abcdEFGH") {
		t.Errorf("resource: %v %+v", err, res)
	}
	// A .tf file keeps its references, as with the recommended rules.
	if tf := mustText(t, h, "get_file_content", map[string]interface{}{"path": "infra/db.tf"}); !strings.Contains(tf, "password = var.db_password") {
		t.Errorf("get_file_content .tf:\n%s", tf)
	}
}

// An edit of the configuration is seen by the next request; an unchanged
// configuration is not compiled again.
func TestSecretPolicy_ConfigurationChangesApplyToTheNextRequest(t *testing.T) {
	root, h := maskRepo(t, "")
	read := func() string { return mustText(t, h, "get_file_content", map[string]interface{}{"path": "app.env"}) }
	if !strings.Contains(read(), "acme_tok_abcdEFGH") {
		t.Fatal("masked without a rule")
	}
	cfg := filepath.Join(root, ".ark-secrets-rules.toml")
	if err := os.WriteFile(cfg, []byte(acmeRule), 0o644); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(), "acme_tok_abcdEFGH") {
		t.Error("the added rule did not apply to the next request")
	}
	builds := h.policyCache().builds
	for range 3 {
		read()
	}
	if h.policyCache().builds != builds {
		t.Errorf("an unchanged configuration was compiled again (%d → %d)", builds, h.policyCache().builds)
	}
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(), "acme_tok_abcdEFGH") {
		t.Error("the removed rule still applies")
	}
}

// Concurrent requests while the configuration changes: each request masks
// with one configuration, never two — a result masks exactly one of the
// two values each configuration's rule masks.
func TestSecretPolicy_RequestsNeverMixConfigurations(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.txt": "first acmeA_1234 second acmeB_5678\n"})
	h := maskServer(t, root)
	cfg := filepath.Join(root, ".ark-secrets-rules.toml")
	configs := []string{
		"version = 1\n[[rule]]\nid = \"a\"\npattern = 'acmeA_[0-9]+'\n",
		"version = 1\n[[rule]]\nid = \"b\"\npattern = 'acmeB_[0-9]+'\n",
	}
	if err := os.WriteFile(cfg, []byte(configs[0]), 0o644); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			// Replace the file whole (rename), as an editor saves it.
			tmp := cfg + ".tmp"
			os.WriteFile(tmp, []byte(configs[i%2]), 0o644)
			os.Rename(tmp, cfg)
		}
	}()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 40 {
				text, isErr := callText(t, h, "get_file_content", map[string]interface{}{"path": "a.txt"})
				if isErr {
					continue // the tree changed under the read: refused, as designed
				}
				a, b := strings.Contains(text, "acmeA_1234"), strings.Contains(text, "acmeB_5678")
				if a == b {
					t.Errorf("a result masked by two configurations or none: %q", text)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	writer.Wait()
}

// captureLog collects what the server logs while f runs.
func captureLog(f func()) string {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	f()
	return buf.String()
}

const weakening = "version = 1\n[[override]]\nid = \"generic-assignment-pass\"\nenabled = false\nreason = \"bypass flags\"\n" + "[[rule]]\nid = \"acme-token\"\npattern = 'acme_tok_[A-Za-z0-9]{8}'\n"

// A configuration that disables a built-in rule applies whole only with the
// operator's --secret-rule-overrides on. Without it the disabling is left
// out — and logged, once — while the rest applies.
func TestSecretPolicy_WeakeningNeedsTheOperator(t *testing.T) {
	root, h := maskRepo(t, weakening)
	var text string
	logged := captureLog(func() {
		text = mustText(t, h, "get_file_content", map[string]interface{}{"path": "app.env"})
		mustText(t, h, "get_file_content", map[string]interface{}{"path": "app.env"})
	})
	if strings.Contains(text, "bypass = true") || strings.Contains(text, "acme_tok_abcdEFGH") {
		t.Errorf("without the operator's permission:\n%s", text)
	}
	if strings.Count(logged, "generic-assignment-pass") != 1 || !strings.Contains(logged, "--secret-rule-overrides on") {
		t.Errorf("log %q: want one notice naming the rule and the flag", logged)
	}

	allowed := maskServer(t, root, "--secret-rule-overrides", "on")
	text = mustText(t, allowed, "get_file_content", map[string]interface{}{"path": "app.env"})
	if !strings.Contains(text, "bypass = true") || strings.Contains(text, "acme_tok_abcdEFGH") || strings.Contains(text, "horse") {
		t.Errorf("with the operator's permission:\n%s", text)
	}
	if _, _, err := commandline.ServerOptParse("test", []string{"--root", root, "--secret-rule-overrides", "yes"}); err == nil {
		t.Error("--secret-rule-overrides yes accepted")
	}
}

// A configuration that cannot be compiled fails closed: a result with
// repository text in it is refused, naming the file and the problem but no
// value, and logged once; identifier-only results go out; the access policy
// and the analysis are unchanged.
func TestSecretPolicy_BrokenConfigurationFailsClosed(t *testing.T) {
	root, h := maskRepo(t, "version = 1\n[[rule]]\nid = \"acme\"\npattern = '(acme_tok_SECRETVALUE'\n")
	logged := captureLog(func() {
		for _, c := range []struct {
			tool string
			args map[string]interface{}
		}{
			{"get_file_content", map[string]interface{}{"path": "app.env", "maskSecrets": false}},
			{"get_symbol", map[string]interface{}{"path": "app/conf.go", "name": "Conf"}},
			{"search_in_files", map[string]interface{}{"path": ".", "query": "acme"}},
			{"get_files_arklite", map[string]interface{}{"paths": []interface{}{"app.env"}}},
			{"search_code", map[string]interface{}{"path": ".", "namePattern": "Conf", "format": "json"}},
			// Declaration names can be data (a YAML key): the text tools
			// that show them are refused too.
			{"get_repository_map", map[string]interface{}{"path": "."}},
			{"search_code", map[string]interface{}{"path": ".", "namePattern": "Conf"}},
		} {
			text, isErr := callText(t, h, c.tool, c.args)
			if !isErr || !strings.Contains(text, ".ark-secrets-rules.toml") || !strings.Contains(text, "pattern does not compile") {
				t.Errorf("%s: isErr=%v %q", c.tool, isErr, text)
			}
			for _, leak := range []string{"acme_tok", "horse", "hunter2go", "SECRETVALUE"} {
				if strings.Contains(text, leak) {
					t.Errorf("%s: %q in %q", c.tool, leak, text)
				}
			}
		}
		for _, c := range []struct {
			tool string
			args map[string]interface{}
			want string
		}{
			{"list_files", map[string]interface{}{"path": "."}, "app.env"},
		} {
			if text := mustText(t, h, c.tool, c.args); !strings.Contains(text, c.want) {
				t.Errorf("%s: %q", c.tool, text)
			}
		}
	})
	if strings.Count(logged, "pattern does not compile") != 1 || strings.Contains(logged, "SECRETVALUE") {
		t.Errorf("log %q: want the problem once, no value", logged)
	}
	if _, err := NewResourcesHandler(root, h.opt).ReadResource("file://app.env"); err == nil || !strings.Contains(err.Error(), "configuration error") {
		t.Errorf("resource: %v", err)
	}
}

// The configuration is a regular file at the root: a symlink is an error,
// whatever it points at; and no configuration makes a file the access
// policy excludes readable, nor changes what the index holds.
func TestSecretPolicy_BoundaryAndAnalysisUnchanged(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"app/conf.go": "package app\n\nfunc Conf() string { return \"x\" }\n",
		"secret.env":  "TOKEN=abc\n",
		".arkignore":  "secret.env\n",
		"rules.toml":  acmeRule,
	})
	plain := maskServer(t, root)
	mapBefore := mustText(t, plain, "get_repository_map", map[string]interface{}{"path": ".", "format": "json"})
	searchBefore := mustText(t, plain, "search_code", map[string]interface{}{"path": ".", "namePattern": "Conf", "format": "json"})

	if err := os.Symlink("rules.toml", filepath.Join(root, ".ark-secrets-rules.toml")); err != nil {
		if os.Getenv("ARK_REQUIRE_SYMLINKS") != "" {
			t.Fatal(err)
		}
		t.Skipf("no symlinks here: %v", err)
	}
	h := maskServer(t, root)
	text, isErr := callText(t, h, "get_file_content", map[string]interface{}{"path": "app/conf.go"})
	if !isErr || !strings.Contains(text, "symbolic link") {
		t.Errorf("a symlinked configuration: isErr=%v %q", isErr, text)
	}
	os.Remove(filepath.Join(root, ".ark-secrets-rules.toml"))
	os.WriteFile(filepath.Join(root, ".ark-secrets-rules.toml"), []byte(weakening), 0o644)
	h = maskServer(t, root, "--secret-rule-overrides", "on")
	if text, isErr := callText(t, h, "get_file_content", map[string]interface{}{"path": "secret.env"}); !isErr || strings.Contains(text, "abc") {
		t.Errorf("an excluded file read with a configuration: %v %q", isErr, text)
	}
	if got := mustText(t, h, "get_repository_map", map[string]interface{}{"path": ".", "format": "json"}); !sameJSON(got, mapBefore) {
		t.Errorf("the repository map changed with a configuration:\n%s\n%s", mapBefore, got)
	}
	if got := mustText(t, h, "search_code", map[string]interface{}{"path": ".", "namePattern": "Conf", "format": "json"}); !sameJSON(got, searchBefore) {
		t.Errorf("search_code changed with a configuration:\n%s\n%s", searchBefore, got)
	}
}

func sameJSON(a, b string) bool {
	var x, y interface{}
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return a == b
	}
	return fmt.Sprint(x) == fmt.Sprint(y)
}

// An error message is masked by the configuration the failing request read:
// a rule added just before it masks the message; while the configuration
// does not compile, no message is returned at all — not one masked by fewer
// rules than the configuration holds.
func TestSecretPolicy_ErrorMessages(t *testing.T) {
	root, _ := maskRepo(t, "")
	_, serveOpt, err := commandline.ServerOptParse("test", []string{"--root", root, "--no-cache"})
	if err != nil {
		t.Fatal(err)
	}
	s := NewMCPServer(root, serveOpt)
	msg := fmt.Errorf("failed near acme_tok_abcdEFGH and password=hunter2x")
	fail := func() {
		t.Helper()
		// A call that fails as a tool error: it reads the configuration as
		// every masked call does.
		if _, err := s.tools.CallTool("get_file_content", map[string]interface{}{}); err == nil {
			t.Fatal("get_file_content without a path succeeded")
		}
	}
	fail()
	if got := s.errorData(msg); !strings.Contains(got, "acme_tok_abcdEFGH") || strings.Contains(got, "hunter2x") {
		t.Errorf("no configuration: %q", got)
	}

	cfg := filepath.Join(root, ".ark-secrets-rules.toml")
	if err := os.WriteFile(cfg, []byte(acmeRule), 0o644); err != nil {
		t.Fatal(err)
	}
	fail()
	if got := s.errorData(msg); strings.Contains(got, "acme_tok_abcdEFGH") || strings.Contains(got, "hunter2x") {
		t.Errorf("a rule added before the failing request: %q", got)
	}

	if err := os.WriteFile(cfg, []byte(acmeRule+"[[rule]]\nid = \"b\"\npattern = '('\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fail()
	got := s.errorData(msg)
	if strings.Contains(got, "acme_tok") || strings.Contains(got, "hunter2x") || strings.Contains(got, "failed near") || !strings.Contains(got, "withheld") {
		t.Errorf("a configuration that does not compile: %q", got)
	}

	// A configuration that cannot be read (both names) is no older one.
	os.WriteFile(cfg, []byte(acmeRule), 0o644)
	fail()
	os.WriteFile(filepath.Join(root, "ark-secrets-rules.toml"), []byte(acmeRule), 0o644)
	fail()
	if got := s.errorData(msg); strings.Contains(got, "failed near") || !strings.Contains(got, "withheld") {
		t.Errorf("a configuration that cannot be read: %q", got)
	}
}
