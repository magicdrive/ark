package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegration_FullWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "Makefile"), []byte("build:\n\tgo build"), 0644)

	// Step 1: Resolve empty repo → Repository
	resolver := NewResolver(tmpDir)
	result, _ := resolver.Resolve()
	if result.Mode != ModeRepository {
		t.Errorf("expected ModeRepository, got %s", result.Mode)
	}

	// Step 2: Generate Repository Skill
	analysis, _ := NewAnalyzer(tmpDir).Analyze()
	skillPath := filepath.Join(tmpDir, "skills", "repo-dev")
	GenerateRepository(RepositoryOptions{Name: "repo-dev", Output: skillPath, Analysis: analysis, NoInstall: true})

	skillMd, _ := os.ReadFile(filepath.Join(skillPath, "SKILL.md"))
	if !strings.Contains(string(skillMd), "ark-managed: true") {
		t.Error("expected ark-managed")
	}

	// Step 3: Resolve again → AlreadyExists
	result, _ = resolver.Resolve()
	if result.Mode != ModeAlreadyExists {
		t.Errorf("expected ModeAlreadyExists, got %s", result.Mode)
	}

	// Step 4: add-explorer → Explorer
	result, _ = resolver.ResolveForCommand("add-explorer")
	if result.Mode != ModeExplorer {
		t.Errorf("expected ModeExplorer, got %s", result.Mode)
	}

	// Step 5: Generate Explorer, Update, Conflict
	explorerPath := filepath.Join(tmpDir, "skills", "ark-explorer")
	GenerateExplorer(ExplorerOptions{Name: "ark-explorer", Output: explorerPath, NoInstall: true})

	updater := NewUpdater(tmpDir)
	results, _ := updater.Update(false)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Modify and detect conflict
	modifiedContent := strings.Replace(string(skillMd), "# repo-dev", "# MODIFIED", 1)
	os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(modifiedContent), 0644)
	conflicts := updater.CheckConflicts()
	if len(conflicts) != 1 {
		t.Errorf("expected 1 conflict, got %d", len(conflicts))
	}

	// Force update
	results, _ = updater.Update(true)
	for _, r := range results {
		if r.SkillName == "repo-dev" && r.HasConflicts {
			t.Error("force should resolve conflict")
		}
	}
}

func TestIntegration_UserSkillsPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	userSkillDir := filepath.Join(tmpDir, "skills", "my-project")
	os.MkdirAll(userSkillDir, 0755)
	original := "# My Project\nCustom content"
	os.WriteFile(filepath.Join(userSkillDir, "SKILL.md"), []byte(original), 0644)

	resolver := NewResolver(tmpDir)
	result, _ := resolver.Resolve()
	if result.Mode != ModeExplorer {
		t.Errorf("expected ModeExplorer, got %s", result.Mode)
	}

	GenerateExplorer(ExplorerOptions{Name: "ark-explorer", Output: filepath.Join(tmpDir, "skills", "ark-explorer"), NoInstall: true})
	content, _ := os.ReadFile(filepath.Join(userSkillDir, "SKILL.md"))
	if string(content) != original {
		t.Error("user skill modified")
	}
}

func TestIntegration_LanguageDetection(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		wantLang string
	}{
		{"Go", map[string]string{"go.mod": "module test", "main.go": "package main"}, "Go"},
		{"Node", map[string]string{"package.json": "{}", "index.js": ""}, "JavaScript"},
		{"Rust", map[string]string{"Cargo.toml": "[package]", "src/main.rs": ""}, "Rust"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			for name, content := range tt.files {
				path := filepath.Join(tmpDir, name)
				os.MkdirAll(filepath.Dir(path), 0755)
				os.WriteFile(path, []byte(content), 0644)
			}
			analysis, _ := NewAnalyzer(tmpDir).Analyze()
			if analysis.PrimaryLang != tt.wantLang {
				t.Errorf("expected %s, got %s", tt.wantLang, analysis.PrimaryLang)
			}
		})
	}
}

func TestIntegration_MultipleSkillDirs(t *testing.T) {
	tmpDir := t.TempDir()
	for i, dir := range []string{"skills", ".skills", ".cline/skills"} {
		skillDir := filepath.Join(tmpDir, dir, "skill"+string(rune('A'+i)))
		os.MkdirAll(skillDir, 0755)
		os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Skill"), 0644)
	}
	result, _ := NewDetector(tmpDir).Detect()
	if len(result.Skills) != 3 {
		t.Errorf("expected 3 skills, got %d", len(result.Skills))
	}
}
