package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolver_NoSkills(t *testing.T) {
	tmpDir := t.TempDir()
	result, err := NewResolver(tmpDir).Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if result.Mode != ModeRepository {
		t.Errorf("expected ModeRepository, got %v", result.Mode)
	}
	if result.SuggestedName != "repository-development" {
		t.Errorf("expected name 'repository-development', got %q", result.SuggestedName)
	}
}

func TestResolver_UserSkillsExist(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skills", "code-review")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Review"), 0644)

	result, _ := NewResolver(tmpDir).Resolve()
	if result.Mode != ModeExplorer {
		t.Errorf("expected ModeExplorer, got %v", result.Mode)
	}
	if result.SuggestedName != "ark-code-explorer" {
		t.Errorf("expected name 'ark-code-explorer', got %q", result.SuggestedName)
	}
}

func TestResolver_ArkSkillExists(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skills", "ark-code-explorer")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nark-managed: true\n---\n# Ark"), 0644)

	result, _ := NewResolver(tmpDir).Resolve()
	if result.Mode != ModeAlreadyExists {
		t.Errorf("expected ModeAlreadyExists, got %v", result.Mode)
	}
	if result.ExistingArk == nil {
		t.Error("expected ExistingArk to be set")
	}
}

func TestResolver_ResolveForCommand_Init(t *testing.T) {
	tmpDir := t.TempDir()
	// User skill should not affect init
	skillDir := filepath.Join(tmpDir, "skills", "code-review")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Review"), 0644)

	result, _ := NewResolver(tmpDir).ResolveForCommand("init")
	if result.Mode != ModeRepository {
		t.Errorf("expected ModeRepository, got %v", result.Mode)
	}
}

func TestResolver_ResolveForCommand_AddExplorer(t *testing.T) {
	tmpDir := t.TempDir()
	result, _ := NewResolver(tmpDir).ResolveForCommand("add-explorer")
	if result.Mode != ModeExplorer {
		t.Errorf("expected ModeExplorer, got %v", result.Mode)
	}
}

func TestResolver_ResolveForCommand_ExistingRepoSkill(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skills", "repository-development")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nark-managed: true\nark-skill-type: repository\n---\n# Repo"), 0644)

	result, _ := NewResolver(tmpDir).ResolveForCommand("init")
	if result.Mode != ModeAlreadyExists {
		t.Errorf("expected ModeAlreadyExists, got %v", result.Mode)
	}
}

func TestResolvedMode_String(t *testing.T) {
	tests := []struct {
		mode ResolvedMode
		want string
	}{
		{ModeRepository, "repository"},
		{ModeExplorer, "explorer"},
		{ModeAlreadyExists, "already-exists"},
		{ModeError, "error"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestPrintDetectionSummary(t *testing.T) {
	result := &DetectionResult{Skills: []DetectedSkill{}}
	if PrintDetectionSummary(result) != "No existing repository skills detected." {
		t.Error("unexpected summary for empty")
	}
	result.Skills = []DetectedSkill{{Name: "test"}}
	if PrintDetectionSummary(result) == "" {
		t.Error("expected non-empty summary")
	}
}
