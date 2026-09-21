package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetector_NoSkills(t *testing.T) {
	tmpDir := t.TempDir()
	detector := NewDetector(tmpDir)
	result, err := detector.Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.HasSkills() {
		t.Errorf("expected no skills, got %d", len(result.Skills))
	}
}

func TestDetector_UserSkill(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skills", "code-review")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Code Review"), 0644); err != nil {
		t.Fatal(err)
	}

	detector := NewDetector(tmpDir)
	result, err := detector.Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(result.Skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(result.Skills))
	}
	if result.Skills[0].IsArkOwned {
		t.Error("expected skill to not be Ark-owned")
	}
}

func TestDetector_ArkSkill(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skills", "ark-code-explorer")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	arkContent := "---\nark-managed: true\nark-skill-type: explorer\n---\n# Ark"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(arkContent), 0644); err != nil {
		t.Fatal(err)
	}

	detector := NewDetector(tmpDir)
	result, err := detector.Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if !result.HasArkSkills() {
		t.Error("expected ark skills")
	}
	if result.ArkSkills[0].SkillType != "explorer" {
		t.Errorf("expected skill type 'explorer', got %q", result.ArkSkills[0].SkillType)
	}
}

func TestDetector_MixedSkills(t *testing.T) {
	tmpDir := t.TempDir()
	// User skill
	userDir := filepath.Join(tmpDir, "skills", "code-review")
	os.MkdirAll(userDir, 0755)
	os.WriteFile(filepath.Join(userDir, "SKILL.md"), []byte("# Review"), 0644)
	// Ark skill
	arkDir := filepath.Join(tmpDir, "skills", "ark-explorer")
	os.MkdirAll(arkDir, 0755)
	os.WriteFile(filepath.Join(arkDir, "SKILL.md"), []byte("---\nark-managed: true\n---\n# Ark"), 0644)

	detector := NewDetector(tmpDir)
	result, _ := detector.Detect()
	if len(result.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(result.Skills))
	}
	if len(result.ArkSkills) != 1 || len(result.UserSkills) != 1 {
		t.Error("expected 1 ark and 1 user skill")
	}
}

func TestDetector_AlternativeDirectories(t *testing.T) {
	for _, dir := range []string{".skills", ".cline/skills", ".cursor/skills"} {
		t.Run(dir, func(t *testing.T) {
			tmpDir := t.TempDir()
			skillDir := filepath.Join(tmpDir, dir, "my-skill")
			os.MkdirAll(skillDir, 0755)
			os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Skill"), 0644)

			result, _ := NewDetector(tmpDir).Detect()
			if !result.HasSkills() {
				t.Errorf("expected skill in %s to be detected", dir)
			}
		})
	}
}
