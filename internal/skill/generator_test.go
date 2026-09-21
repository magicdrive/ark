package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateExplorer(t *testing.T) {
	tmpDir := t.TempDir()
	output := filepath.Join(tmpDir, "ark-explorer")

	err := GenerateExplorer(ExplorerOptions{Name: "ark-explorer", Output: output})
	if err != nil {
		t.Fatal(err)
	}

	// Check SKILL.md exists and has frontmatter
	content, err := os.ReadFile(filepath.Join(output, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "ark-managed: true") {
		t.Error("expected ark-managed frontmatter")
	}
	if !strings.Contains(string(content), "ark-skill-type: explorer") {
		t.Error("expected explorer skill type")
	}

	// Check agents/openai.yaml exists
	if !fileExists(filepath.Join(output, "agents", "openai.yaml")) {
		t.Error("expected agents/openai.yaml")
	}
}

func TestGenerateRepository(t *testing.T) {
	tmpDir := t.TempDir()
	output := filepath.Join(tmpDir, "repo-dev")

	analysis := &RepoAnalysis{
		ProjectName: "test-project",
		PrimaryLang: "Go",
		Languages:   []LanguageInfo{{Name: "Go", FileCount: 10}},
		BuildCommands: []BuildCommand{
			{Type: "build", Command: "go build ./...", Source: "go.mod"},
			{Type: "test", Command: "go test ./...", Source: "go.mod"},
		},
		Modules: []ModuleInfo{
			{Name: "core", Path: "internal/core", Type: "package"},
		},
	}

	err := GenerateRepository(RepositoryOptions{
		Name: "repo-dev", Output: output, Analysis: analysis,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Check SKILL.md
	content, err := os.ReadFile(filepath.Join(output, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "ark-skill-type: repository") {
		t.Error("expected repository skill type")
	}
	if !strings.Contains(string(content), "Go") {
		t.Error("expected Go language")
	}
	if !strings.Contains(string(content), "go build") {
		t.Error("expected build command")
	}

	// Check references/conventions.md
	if !fileExists(filepath.Join(output, "references", "conventions.md")) {
		t.Error("expected references/conventions.md")
	}
}

func TestGenerateRepository_WithArchive(t *testing.T) {
	tmpDir := t.TempDir()
	output := filepath.Join(tmpDir, "repo-dev")

	err := GenerateRepository(RepositoryOptions{
		Name: "repo-dev", Output: output, Archive: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !fileExists(output + ".zip") {
		t.Error("expected zip archive")
	}
}
