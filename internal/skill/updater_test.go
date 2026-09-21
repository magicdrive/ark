package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdater_Update(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an Ark-managed skill
	skillDir := filepath.Join(tmpDir, "skills", "ark-explorer")
	os.MkdirAll(filepath.Join(skillDir, "agents"), 0755)
	os.MkdirAll(filepath.Join(skillDir, "references"), 0755)

	// SKILL.md with matching hash
	content := generateExplorerSkillMd("ark-explorer")
	wrapped := WrapWithFrontmatter(content, SkillTypeExplorer)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(wrapped), 0644)
	os.WriteFile(filepath.Join(skillDir, "agents/openai.yaml"), []byte("test"), 0644)

	// User file that should be preserved
	os.WriteFile(filepath.Join(skillDir, "references/my-notes.md"), []byte("user notes"), 0644)

	updater := NewUpdater(tmpDir)
	results, err := updater.Update(false)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SkillName != "ark-explorer" {
		t.Errorf("expected ark-explorer, got %s", results[0].SkillName)
	}
	if len(results[0].Preserved) != 1 || results[0].Preserved[0] != "references/my-notes.md" {
		t.Errorf("expected preserved file, got %v", results[0].Preserved)
	}

	// Verify user file still exists
	if _, err := os.Stat(filepath.Join(skillDir, "references/my-notes.md")); err != nil {
		t.Error("user file should be preserved")
	}
}

func TestUpdater_ConflictDetection(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skills", "ark-explorer")
	os.MkdirAll(skillDir, 0755)

	// Create skill with hash
	content := generateExplorerSkillMd("ark-explorer")
	wrapped := WrapWithFrontmatter(content, SkillTypeExplorer)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(wrapped), 0644)

	// Modify content (but keep frontmatter)
	modifiedContent := strings.Replace(string(wrapped), "# ark-explorer", "# MODIFIED", 1)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(modifiedContent), 0644)

	updater := NewUpdater(tmpDir)
	conflicts := updater.CheckConflicts()
	if len(conflicts) != 1 {
		t.Errorf("expected 1 conflict, got %d", len(conflicts))
	}

	// Update without force should skip
	results, _ := updater.Update(false)
	if !results[0].HasConflicts {
		t.Error("expected conflict")
	}
	if len(results[0].Skipped) != 1 {
		t.Error("expected skipped file")
	}

	// Update with force should work
	results, _ = updater.Update(true)
	if results[0].HasConflicts {
		t.Error("force should override conflict")
	}
}

func TestUpdater_NoSkills(t *testing.T) {
	tmpDir := t.TempDir()
	updater := NewUpdater(tmpDir)
	_, err := updater.Update(false)
	if err == nil {
		t.Error("expected error for no skills")
	}
}

func TestFormatResults(t *testing.T) {
	results := []UpdateResult{{
		SkillName: "test", SkillType: "explorer",
		Updated: []string{"SKILL.md"}, Preserved: []string{"refs/notes.md"},
	}}
	output := FormatResults(results)
	if !strings.Contains(output, "test") {
		t.Error("expected skill name")
	}
	if !strings.Contains(output, "Updated") {
		t.Error("expected Updated")
	}
}
