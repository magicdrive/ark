package skill

import (
	"strings"
	"testing"
	"time"
)

func TestNewMetadata(t *testing.T) {
	meta := NewMetadata(SkillTypeExplorer)
	if !meta.ArkManaged {
		t.Error("expected ArkManaged true")
	}
	if meta.SkillType != SkillTypeExplorer {
		t.Errorf("expected SkillType %q", SkillTypeExplorer)
	}
}

func TestArkMetadata_GenerateFrontmatter(t *testing.T) {
	meta := &ArkMetadata{
		ArkManaged:  true,
		ArkVersion:  "1.0.0",
		SkillType:   SkillTypeRepository,
		GeneratedAt: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC),
		ContentHash: "sha256:abc123",
	}
	fm := meta.GenerateFrontmatter()
	for _, s := range []string{"ark-managed: true", "ark-version: 1.0.0", "ark-skill-type: repository"} {
		if !strings.Contains(fm, s) {
			t.Errorf("frontmatter missing %q", s)
		}
	}
}

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantNil bool
		wantType string
	}{
		{"no frontmatter", "# Just content", true, ""},
		{"ark-managed true", "---\nark-managed: true\nark-skill-type: explorer\n---\n# Skill", false, "explorer"},
		{"ark-managed false", "---\nark-managed: false\n---\n# Skill", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, _ := ParseFrontmatter(tt.content)
			if tt.wantNil && meta != nil {
				t.Error("expected nil")
			}
			if !tt.wantNil && (meta == nil || meta.SkillType != tt.wantType) {
				t.Errorf("expected type %q", tt.wantType)
			}
		})
	}
}

func TestComputeHash(t *testing.T) {
	h1 := ComputeHash("# Test")
	h2 := ComputeHash("# Test")
	h3 := ComputeHash("# Different")
	if h1 != h2 {
		t.Error("hash should be deterministic")
	}
	if h1 == h3 {
		t.Error("different content should have different hash")
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Error("hash should start with sha256:")
	}
}

func TestComputeHash_IgnoresFrontmatter(t *testing.T) {
	h1 := ComputeHash("# Test")
	h2 := ComputeHash("---\nark-managed: true\n---\n\n# Test")
	if h1 != h2 {
		t.Error("hash should ignore frontmatter")
	}
}

func TestVerifyHash(t *testing.T) {
	content := "# Test"
	hash := ComputeHash(content)
	if !VerifyHash(content, hash) {
		t.Error("hash should verify")
	}
	if VerifyHash("# Modified", hash) {
		t.Error("modified content should not verify")
	}
}

func TestWrapWithFrontmatter(t *testing.T) {
	wrapped := WrapWithFrontmatter("# My Skill", SkillTypeExplorer)
	if !strings.HasPrefix(wrapped, "---\n") {
		t.Error("should start with frontmatter")
	}
	if !strings.Contains(wrapped, "ark-managed: true") {
		t.Error("should contain ark-managed")
	}
	if !strings.Contains(wrapped, "content-hash: sha256:") {
		t.Error("should contain content hash")
	}
}
