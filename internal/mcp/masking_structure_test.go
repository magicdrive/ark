package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

// Masking keeps a value that only names another value — a GitHub Actions
// context reference anywhere, a Terraform reference in a file the request
// read as a .tf file — and masks every value, a GitHub Actions expression
// whole. Whether a file is Terraform is decided by the file read, never by
// the name asked for: a .tf link to another file masks as that file.
func TestMasking_KeepsReferencesNotValues(t *testing.T) {
	root := t.TempDir()
	aws := "AKIA" + "IOSFODNN7EXAMPLE"
	writeTree(t, root, map[string]string{
		".github/workflows/ci.yml": "jobs:\n  deploy:\n    env:\n      DD_API_KEY: ${{ secrets.DD_API_KEY }}\n      DATADOG_API_KEY: 9f3c1a7e5b2d4f6a8c0e1b3d5f7a9c2e\n      FALLBACK_TOKEN: ${{ secrets.A || 'fallback-literal-value' }}\n    steps:\n      - with:\n          token: ${{ secrets.GITHUB_TOKEN }}\n",
		"db/main.tf":               "resource \"aws_db_instance\" \"x\" {\n  password        = var.db_password\n  master_password = random_password.api_token.result\n  api_key         = \"hunter2-literal\"\n  token           = " + aws + "\n}\n",
		"db/creds.txt":             "password = var.db_password\n",
	})
	link := filepath.Join(root, "db", "alias.tf")
	linked := os.Symlink("creds.txt", link) == nil
	if !linked && os.Getenv("ARK_REQUIRE_SYMLINKS") != "" {
		t.Fatal("ARK_REQUIRE_SYMLINKS: cannot create a symlink")
	}
	_, serveOpt, err := commandline.ServerOptParse("test", []string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandlerWithCache(root, serveOpt.GeneralOption, nil)
	call := func(tool string, args map[string]interface{}) string {
		t.Helper()
		text, isErr := callText(t, h, tool, args)
		if isErr {
			t.Fatalf("%s %v: %s", tool, args, text)
		}
		return text
	}
	check := func(what, text string, keep, hide []string) {
		t.Helper()
		for _, k := range keep {
			if !strings.Contains(text, k) {
				t.Errorf("%s: %q not kept:\n%s", what, k, text)
			}
		}
		for _, s := range hide {
			if strings.Contains(text, s) {
				t.Errorf("%s: %q not masked:\n%s", what, s, text)
			}
		}
	}

	workflow := call("get_file_content", map[string]interface{}{"path": ".github/workflows/ci.yml"})
	check("workflow", workflow,
		[]string{"DD_API_KEY: ${{ secrets.DD_API_KEY }}", "token: ${{ secrets.GITHUB_TOKEN }}", "FALLBACK_TOKEN: *****MASKED*****\n"},
		[]string{"9f3c1a7e5b2d4f6a8c0e1b3d5f7a9c2e", "fallback-literal-value", "MASKED***** secrets."})

	tf := call("get_file_content", map[string]interface{}{"path": "db/main.tf"})
	check("get_file_content .tf", tf,
		[]string{"password        = var.db_password", "master_password = random_password.api_token.result"},
		[]string{"hunter2-literal", aws})

	sym := call("get_symbol", map[string]interface{}{"path": "db/main.tf", "name": "aws_db_instance.x"})
	check("get_symbol .tf", sym,
		[]string{"password        = var.db_password", "master_password = random_password.api_token.result"},
		[]string{"hunter2-literal", aws})

	check("a .txt file", call("get_file_content", map[string]interface{}{"path": "db/creds.txt"}), nil, []string{"var.db_password"})
	if linked {
		check("a .tf link to a .txt file", call("get_file_content", map[string]interface{}{"path": "db/alias.tf"}), nil, []string{"var.db_password"})
	}
}

// A value's boundary at the output: a CRLF line keeps its reference, a
// literal written after a kept reference is masked, and in the arklite
// format — one physical line per file, lines joined by "␤" — a value ends at
// its "␤", so masking neither swallows the next line nor lets a secret
// through, whatever maskSecrets asks.
func TestMasking_ValueBoundariesAtTheOutput(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"crlf/main.tf":            "resource \"aws_db_instance\" \"x\" {\r\n  password = var.db_password\r\n  api_key  = \"hunter2-crlf\"\r\n}\r\n",
		"leak/main.tf":            "resource \"aws_db_instance\" \"y\" {\n  password = var.x != \"\" ? var.x : \"default-secret-1\"\n  token    = var.x+\"hunter2-after-ref\"\n  api_key  = var.y # rotated\n}\n",
		"leak/bad.tf":             "  token = var.x hunter2-invalid-hcl\n",
		".github/workflows/a.yml": "env:\r\n  TOKEN: ${{ secrets.A }}\r\n  API_KEY: ${{ secrets.B }}, other: literal-after-comma\r\n  PASSWORD: ${{ secrets.C }}\t# note\r\n",
		"x.yml":                   "token: ${{ secrets.A }}\nnext: visible-line\npassword: hunter2-arklite\nlast: also-visible\n",
	})
	_, serveOpt, err := commandline.ServerOptParse("test", []string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandlerWithCache(root, serveOpt.GeneralOption, nil)
	call := func(tool string, args map[string]interface{}) string {
		t.Helper()
		text, isErr := callText(t, h, tool, args)
		if isErr {
			t.Fatalf("%s %v: %s", tool, args, text)
		}
		return text
	}
	check := func(what, text string, keep, hide []string) {
		t.Helper()
		for _, k := range keep {
			if !strings.Contains(text, k) {
				t.Errorf("%s: %q not kept:\n%s", what, k, text)
			}
		}
		for _, s := range hide {
			if strings.Contains(text, s) {
				t.Errorf("%s: %q not masked:\n%s", what, s, text)
			}
		}
	}

	check("CRLF .tf", call("get_file_content", map[string]interface{}{"path": "crlf/main.tf"}),
		[]string{"password = var.db_password\r\n"}, []string{"hunter2-crlf"})
	check("CRLF .tf get_symbol", call("get_symbol", map[string]interface{}{"path": "crlf/main.tf", "name": "aws_db_instance.x"}),
		[]string{"password = var.db_password"}, []string{"hunter2-crlf"})
	for _, tool := range []string{"get_file_content", "get_symbol"} {
		check("literal after a reference ("+tool+")",
			call(tool, map[string]interface{}{"path": "leak/main.tf", "name": "aws_db_instance.y"}),
			[]string{"api_key  = var.y # rotated"}, []string{"default-secret-1", "hunter2-after-ref"})
	}
	check("text after a reference", call("get_file_content", map[string]interface{}{"path": "leak/bad.tf"}), nil, []string{"hunter2-invalid-hcl"})
	check("CRLF workflow", call("get_file_content", map[string]interface{}{"path": ".github/workflows/a.yml"}),
		[]string{"TOKEN: ${{ secrets.A }}\r\n", "PASSWORD: ${{ secrets.C }}\t# note\r\n"}, []string{"literal-after-comma"})

	for _, maskSecrets := range []bool{true, false} {
		lite := call("get_files_arklite", map[string]interface{}{"paths": []interface{}{"x.yml", "leak/main.tf"}, "maskSecrets": maskSecrets})
		check("arklite", lite,
			[]string{"token: ${{ secrets.A }}␤next: visible-line␤password: *****MASKED*****␤last: also-visible"},
			[]string{"hunter2-arklite", "default-secret-1", "hunter2-after-ref"})
	}
}

// No part of a secret leaks through any tool that returns repository text:
// a value of several words, a value of a quoted key (JSON, PHP, Python,
// TypeScript), a value a quote does not end, a value with a "␤" in it — in
// get_file_content, get_symbol, get_context, search_in_files and
// get_files_arklite, with maskSecrets false too. A JSON result stays JSON.
func TestMasking_NoPartOfAValueLeaksThroughAnyTool(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"cfg/app.json": "{\n  \"db\": {\n    \"password\": \"hunter2json\",\n    \"api_key\": \"zebra42json\",\n    \"user\": \"visible-user\"\n  }\n}\n",
		"cfg/x.env":    "PASSWORD=correct horsebattery staple\nexport TOKEN=\"abc\"hunter2shell\nAPI_KEY=one␤tailsym\nUSER=visible-user\n",
		"cfg/ci.yml":   "env:\n  TOKEN: ${{ secrets.A }}\n  PASSWORD: correct horsebattery\n  NEXT: visible-next\n",
		"app/db.php":   "<?php\nfunction dbConfig() {\n    return ['password' => 'hunter2php', 'host' => 'visible-host'];\n}\n",
		"app/conf.py":  "def conf(x):\n    token = \"a\" if x else \"fallback2py\"\n    creds = {'api_key': 'zebra42py'}\n    return token, creds\n",
		"app/conf.ts":  "export function confTs() {\n  const c = { \"password\": \"hunter2ts\", user: \"visible-user\" };\n  return c;\n}\n",
		"infra/db.tf":  "resource \"aws_db_instance\" \"x\" {\n  password = var.db_password\n  api_key  = \"correct horsebattery\"\n}\n",
	})
	_, serveOpt, err := commandline.ServerOptParse("test", []string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandlerWithCache(root, serveOpt.GeneralOption, nil)
	leaks := []string{"hunter2json", "zebra42json", "horsebattery", "staple", "hunter2shell", "tailsym", "hunter2php", "fallback2py", "zebra42py", "hunter2ts"}
	check := func(what string, args map[string]interface{}, tool string, keep ...string) string {
		t.Helper()
		text, isErr := callText(t, h, tool, args)
		if isErr {
			t.Fatalf("%s: %s", what, text)
		}
		for _, l := range leaks {
			if strings.Contains(text, l) {
				t.Errorf("%s: leaks %q:\n%s", what, l, text)
			}
		}
		for _, k := range keep {
			if !strings.Contains(text, k) {
				t.Errorf("%s: %q not kept:\n%s", what, k, text)
			}
		}
		return text
	}
	for _, f := range []string{"cfg/app.json", "cfg/x.env", "cfg/ci.yml", "app/db.php", "app/conf.py", "app/conf.ts", "infra/db.tf"} {
		for _, ms := range []bool{true, false} {
			check("get_file_content "+f, map[string]interface{}{"path": f, "maskSecrets": ms}, "get_file_content")
		}
	}
	check("get_file_content json keeps", map[string]interface{}{"path": "cfg/app.json"}, "get_file_content", `"user": "visible-user"`)
	check("get_file_content yml keeps", map[string]interface{}{"path": "cfg/ci.yml"}, "get_file_content", "TOKEN: ${{ secrets.A }}", "NEXT: visible-next")
	check("get_file_content tf keeps", map[string]interface{}{"path": "infra/db.tf"}, "get_file_content", "password = var.db_password")
	check("get_symbol php", map[string]interface{}{"path": "app/db.php", "name": "dbConfig"}, "get_symbol", "'visible-host'")
	check("get_symbol py", map[string]interface{}{"path": "app/conf.py", "name": "conf"}, "get_symbol")
	check("get_symbol ts", map[string]interface{}{"path": "app/conf.ts", "name": "confTs"}, "get_symbol")
	for _, sym := range []string{"dbConfig", "conf", "confTs"} {
		check("get_context "+sym, map[string]interface{}{"path": ".", "symbol": sym}, "get_context")
	}
	for _, q := range []string{"a", "PASSWORD", "password", "token", "api_key"} {
		check("search_in_files "+q, map[string]interface{}{"path": ".", "query": q}, "search_in_files")
	}
	paths := []interface{}{"cfg/app.json", "cfg/x.env", "cfg/ci.yml", "app/db.php", "app/conf.py", "app/conf.ts", "infra/db.tf"}
	for _, ms := range []bool{true, false} {
		lite := check("get_files_arklite", map[string]interface{}{"paths": paths, "maskSecrets": ms}, "get_files_arklite", "USER=visible-user", "NEXT: visible-next")
		if !strings.Contains(lite, "␤") || strings.Count(lite, "\n@") != len(paths) {
			t.Errorf("get_files_arklite: structure changed:\n%s", lite)
		}
	}
	// A JSON result is still JSON, its masked values included.
	text, _ := callText(t, h, "get_symbol", map[string]interface{}{"path": "app/db.php", "name": "dbConfig"})
	if !json.Valid([]byte(text)) {
		t.Errorf("get_symbol result is not JSON:\n%s", text)
	}
}
