package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/languages"
)

// The language of a file that get_file_info and get_project_stats report is
// the language Ark's providers analyze it as (languages.Registry) — for every
// registered extension, in any case — and the highlighting tag of
// knownLanguageTags, unchanged, for any other file.
func TestDetectLanguage_FollowsTheLanguageRegistry(t *testing.T) {
	reg := languages.Registry()
	for _, ext := range reg.Extensions() {
		d, _ := reg.DetectByExtension(ext)
		for _, name := range []string{"file" + ext, "FILE" + strings.ToUpper(ext)} {
			if got := DetectLanguage(name); got != string(d.Language) {
				t.Errorf("DetectLanguage(%q) = %q, the registry says %q", name, got, d.Language)
			}
			if got := SupportedLanguage(name); got != string(d.Language) {
				t.Errorf("SupportedLanguage(%q) = %q, the registry says %q", name, got, d.Language)
			}
		}
	}
	for ext, tag := range knownLanguageTags {
		if _, registered := reg.DetectByExtension(ext); registered {
			continue
		}
		if got := DetectLanguage("file" + ext); got != tag {
			t.Errorf("DetectLanguage(%q) = %q, want the highlighting tag %q", "file"+ext, got, tag)
		}
		if got := SupportedLanguage("file" + ext); got != "" {
			t.Errorf("SupportedLanguage(%q) = %q for an unsupported language", "file"+ext, got)
		}
	}
	// Terraform's JSON syntax is not extracted, so it is JSON, not Terraform.
	for _, name := range []string{"main.tf.json", "prod.tfvars.json"} {
		if got := SupportedLanguage(name); got != "" {
			t.Errorf("SupportedLanguage(%q) = %q, want none", name, got)
		}
		if got := DetectLanguage(name); got != "json" {
			t.Errorf("DetectLanguage(%q) = %q, want json", name, got)
		}
	}
}

// get_file_info, get_project_stats and get_repository_map agree on the
// language of a file: the language the index has it in.
func TestLanguageClassification_ToolsAgree(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"infra/main.tf":           "resource \"aws_vpc\" \"main\" {}\nvariable \"region\" {}\n",
		"infra/prod.tfvars":       "region = \"ap-northeast-1\"\n",
		"infra/main.tf.json":      "{\"resource\": {}}\n",
		"app/main.go":             "package main\n\nfunc Main() {}\n",
		"web/view.jsx":            "export function View() { return null }\n",
		".github/workflows/x.yml": "on: push\n",
	})
	_, serveOpt, err := commandline.ServerOptParse("test", []string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandlerWithCache(root, serveOpt.GeneralOption, nil)
	call := func(tool string, args map[string]interface{}, v interface{}) {
		t.Helper()
		text, isErr := callText(t, h, tool, args)
		if isErr {
			t.Fatalf("%s: %s", tool, text)
		}
		if err := json.Unmarshal([]byte(text), v); err != nil {
			t.Fatalf("%s: %v\n%s", tool, err, text)
		}
	}

	want := map[string]string{"infra/main.tf": "terraform", "infra/prod.tfvars": "terraform", "app/main.go": "go", "web/view.jsx": "javascript", "infra/main.tf.json": "json", ".github/workflows/x.yml": "yaml"}
	for path, lang := range want {
		var info struct {
			Language string `json:"language"`
		}
		call("get_file_info", map[string]interface{}{"path": path}, &info)
		if info.Language != lang {
			t.Errorf("get_file_info %s: language %q, want %q", path, info.Language, lang)
		}
	}

	var stats struct {
		LanguageStats          map[string]int `json:"languageStats"`
		SupportedLanguageStats map[string]int `json:"supportedLanguageStats"`
	}
	call("get_project_stats", map[string]interface{}{"path": "."}, &stats)
	if stats.LanguageStats["terraform"] != 2 || stats.SupportedLanguageStats["terraform"] != 2 {
		t.Errorf("get_project_stats: terraform counted %d / %d, want 2 (.tf, .tfvars; not .tf.json)", stats.LanguageStats["terraform"], stats.SupportedLanguageStats["terraform"])
	}
	wantSupported := map[string]int{"terraform": 2, "go": 1, "javascript": 1, "yaml": 1}
	if len(stats.SupportedLanguageStats) != len(wantSupported) {
		t.Errorf("supportedLanguageStats %v, want %v", stats.SupportedLanguageStats, wantSupported)
	}
	for lang, n := range wantSupported {
		if stats.SupportedLanguageStats[lang] != n {
			t.Errorf("supportedLanguageStats[%s] = %d, want %d", lang, stats.SupportedLanguageStats[lang], n)
		}
	}
	if stats.LanguageStats["json"] != 1 || stats.LanguageStats["yaml"] != 1 {
		t.Errorf("languageStats %v: the highlighting tags of other files are kept", stats.LanguageStats)
	}

	var m struct {
		Packages []struct {
			Path     string `json:"Path"`
			Language string `json:"Language"`
		} `json:"Packages"`
	}
	call("get_repository_map", map[string]interface{}{"path": ".", "format": "json"}, &m)
	got := map[string]string{}
	for _, p := range m.Packages {
		got[p.Path] = p.Language
	}
	for dir, lang := range map[string]string{"infra": "terraform", "app": "go", "web": "javascript"} {
		if got[dir] != lang {
			t.Errorf("get_repository_map: %s is %q, want %q (packages %v)", dir, got[dir], lang, got)
		}
	}
}
