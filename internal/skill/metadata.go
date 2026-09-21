package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ArkMetadata represents the YAML frontmatter for Ark-generated skills
type ArkMetadata struct {
	ArkManaged  bool      `yaml:"ark-managed"`
	ArkVersion  string    `yaml:"ark-version"`
	SkillType   string    `yaml:"ark-skill-type"` // "explorer" or "repository"
	GeneratedAt time.Time `yaml:"generated-at"`
	ContentHash string    `yaml:"content-hash"`
}

// SkillTypeExplorer is the skill type for Explorer Skills
const SkillTypeExplorer = "explorer"

// SkillTypeRepository is the skill type for Repository Skills
const SkillTypeRepository = "repository"

// CurrentArkVersion returns the current Ark skill schema version
func CurrentArkVersion() string {
	return "1.0.0"
}

// NewMetadata creates a new ArkMetadata with default values
func NewMetadata(skillType string) *ArkMetadata {
	return &ArkMetadata{
		ArkManaged:  true,
		ArkVersion:  CurrentArkVersion(),
		SkillType:   skillType,
		GeneratedAt: time.Now().UTC(),
	}
}

// GenerateFrontmatter creates YAML frontmatter string from metadata
func (m *ArkMetadata) GenerateFrontmatter() string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("ark-managed: %t\n", m.ArkManaged))
	sb.WriteString(fmt.Sprintf("ark-version: %s\n", m.ArkVersion))
	sb.WriteString(fmt.Sprintf("ark-skill-type: %s\n", m.SkillType))
	sb.WriteString(fmt.Sprintf("generated-at: %s\n", m.GeneratedAt.Format(time.RFC3339)))
	if m.ContentHash != "" {
		sb.WriteString(fmt.Sprintf("content-hash: %s\n", m.ContentHash))
	}
	sb.WriteString("---\n")
	return sb.String()
}

// SetContentHash computes and sets the content hash from the given content
func (m *ArkMetadata) SetContentHash(content string) {
	m.ContentHash = ComputeHash(content)
}

// ParseFrontmatter extracts ArkMetadata from SKILL.md content
// Returns nil if no frontmatter or not Ark-managed
func ParseFrontmatter(content string) (*ArkMetadata, error) {
	// Match YAML frontmatter block
	re := regexp.MustCompile(`(?s)^---\n(.+?)\n---`)
	matches := re.FindStringSubmatch(content)
	if len(matches) < 2 {
		return nil, nil // No frontmatter
	}

	frontmatter := matches[1]
	meta := &ArkMetadata{}

	// Parse each line
	lines := strings.Split(frontmatter, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "ark-managed":
			meta.ArkManaged = value == "true"
		case "ark-version":
			meta.ArkVersion = value
		case "ark-skill-type":
			meta.SkillType = value
		case "generated-at":
			if t, err := time.Parse(time.RFC3339, value); err == nil {
				meta.GeneratedAt = t
			}
		case "content-hash":
			meta.ContentHash = value
		}
	}

	// Only return metadata if it's Ark-managed
	if !meta.ArkManaged {
		return nil, nil
	}

	return meta, nil
}

// ExtractContentWithoutFrontmatter removes frontmatter and returns the body
func ExtractContentWithoutFrontmatter(content string) string {
	re := regexp.MustCompile(`(?s)^---\n.+?\n---\n*`)
	return re.ReplaceAllString(content, "")
}

// ComputeHash calculates SHA256 hash of content (without frontmatter)
func ComputeHash(content string) string {
	// Remove frontmatter before hashing
	body := ExtractContentWithoutFrontmatter(content)
	hash := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(hash[:])
}

// VerifyHash checks if the stored hash matches the current content
func VerifyHash(content string, storedHash string) bool {
	if storedHash == "" {
		return false
	}
	currentHash := ComputeHash(content)
	return currentHash == storedHash
}

// HasUserModifications checks if the content has been modified by user
// by comparing stored hash with current content hash
func HasUserModifications(content string) (bool, error) {
	meta, err := ParseFrontmatter(content)
	if err != nil {
		return false, err
	}
	if meta == nil {
		return false, errors.New("not an Ark-managed skill")
	}
	if meta.ContentHash == "" {
		// No hash stored, assume no modifications
		return false, nil
	}
	return !VerifyHash(content, meta.ContentHash), nil
}

// WrapWithFrontmatter adds frontmatter to content and computes hash
func WrapWithFrontmatter(content string, skillType string) string {
	meta := NewMetadata(skillType)
	meta.SetContentHash(content)
	return meta.GenerateFrontmatter() + "\n" + content
}
