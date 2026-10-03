package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/syntax"
)

// TestOneRegistrationInvariant_PHP verifies the architecture property that a
// language enters every MCP intelligence surface through the canonical Language
// Registry — not via per-tool registration. PHP was added by a single registry
// entry (internal/languages); this asserts it flows through syntax detection,
// repository indexing / relations (defaultProviders), find_references
// (refProviderRegistry), and get_language_support, with no PHP special-case.
func TestOneRegistrationInvariant_PHP(t *testing.T) {
	// 1. Canonical registry knows PHP.
	if _, ok := languages.Registry().Lookup("php"); !ok {
		t.Fatal("php missing from canonical registry")
	}

	// 2. Syntax detection (derived from registry).
	if syntax.DetectLanguage("index.php") != "php" {
		t.Error("syntax.DetectLanguage does not route .php to php")
	}

	// 3. Index / relations provider set (derived from registry).
	foundIdx := false
	for _, p := range defaultProviders() {
		if p.Language() == "php" {
			foundIdx = true
		}
	}
	if !foundIdx {
		t.Error("defaultProviders() (index/relations) missing php")
	}

	// 4. find_references extension registry (derived from registry).
	if _, ok := refProviderRegistry[".php"]; !ok {
		t.Error("find_references refProviderRegistry missing .php")
	}

	// 5. get_language_support reports php (derived from registry).
	h := &ToolsHandler{}
	res, err := h.getLanguageSupport(nil)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for _, c := range res.Content {
		text += c.Text
	}
	var entries []languageSupportEntry
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("parse get_language_support: %v\n%s", err, text)
	}
	phpFound := false
	for _, e := range entries {
		if e.Language == "php" {
			phpFound = true
			if e.Level != "graph" {
				t.Errorf("get_language_support php level = %q, want graph", e.Level)
			}
			if !containsExt(e.Extensions, ".php") {
				t.Errorf("get_language_support php extensions = %v, want to include .php", e.Extensions)
			}
		}
	}
	if !phpFound {
		t.Error("get_language_support does not report php")
	}
}

func containsExt(exts []string, want string) bool {
	for _, e := range exts {
		if strings.EqualFold(e, want) {
			return true
		}
	}
	return false
}
