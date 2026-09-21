// Package skill provides Cline/ChatGPT Skill generation for Ark MCP.
package skill

import (
	"os"
	"path/filepath"
	"strings"
)

// SkillDirectories lists standard skill directory locations to search
var SkillDirectories = []string{
	"skills",
	".skills",
	".cline/skills",
	".cursor/skills",
	"prompts/skills",
}

// DetectedSkill represents a discovered skill in the repository
type DetectedSkill struct {
	Name       string // Skill name (directory name)
	Path       string // Absolute path to skill directory
	SkillFile  string // Path to SKILL.md
	IsArkOwned bool   // true if ark-managed: true in frontmatter
	SkillType  string // "explorer", "repository", or "" for user skills
	Hash       string // Content hash for change detection
}

// DetectionResult holds the results of skill detection
type DetectionResult struct {
	RootDir    string          // Repository root directory
	Skills     []DetectedSkill // All detected skills
	ArkSkills  []DetectedSkill // Ark-generated skills only
	UserSkills []DetectedSkill // User-created skills only
}

// HasSkills returns true if any skills were detected
func (r *DetectionResult) HasSkills() bool {
	return len(r.Skills) > 0
}

// HasArkSkills returns true if Ark-generated skills exist
func (r *DetectionResult) HasArkSkills() bool {
	return len(r.ArkSkills) > 0
}

// HasUserSkills returns true if user-created skills exist
func (r *DetectionResult) HasUserSkills() bool {
	return len(r.UserSkills) > 0
}

// Detector handles skill detection in a repository
type Detector struct {
	rootDir string
}

// NewDetector creates a new skill detector for the given root directory
func NewDetector(rootDir string) *Detector {
	return &Detector{rootDir: rootDir}
}

// Detect scans for existing skills in the repository
func (d *Detector) Detect() (*DetectionResult, error) {
	result := &DetectionResult{
		RootDir:    d.rootDir,
		Skills:     []DetectedSkill{},
		ArkSkills:  []DetectedSkill{},
		UserSkills: []DetectedSkill{},
	}

	for _, dir := range SkillDirectories {
		skillsRoot := filepath.Join(d.rootDir, dir)
		if !dirExists(skillsRoot) {
			continue
		}

		// Scan for skill directories (those containing SKILL.md)
		entries, err := os.ReadDir(skillsRoot)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			skillDir := filepath.Join(skillsRoot, entry.Name())
			skillFile := filepath.Join(skillDir, "SKILL.md")

			if !fileExists(skillFile) {
				continue
			}

			skill, err := d.parseSkill(entry.Name(), skillDir, skillFile)
			if err != nil {
				// Skip skills we can't parse, but continue scanning
				continue
			}

			result.Skills = append(result.Skills, skill)
			if skill.IsArkOwned {
				result.ArkSkills = append(result.ArkSkills, skill)
			} else {
				result.UserSkills = append(result.UserSkills, skill)
			}
		}
	}

	return result, nil
}

// parseSkill reads and parses a skill directory
func (d *Detector) parseSkill(name, skillDir, skillFile string) (DetectedSkill, error) {
	skill := DetectedSkill{
		Name:      name,
		Path:      skillDir,
		SkillFile: skillFile,
	}

	// Read SKILL.md content
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return skill, err
	}

	// Parse frontmatter
	meta, err := ParseFrontmatter(string(content))
	if err == nil && meta != nil {
		skill.IsArkOwned = meta.ArkManaged
		skill.SkillType = meta.SkillType
		skill.Hash = meta.ContentHash
	}

	return skill, nil
}

// FindSkillByName looks for a specific skill by name
func (d *Detector) FindSkillByName(name string) (*DetectedSkill, error) {
	result, err := d.Detect()
	if err != nil {
		return nil, err
	}

	for _, skill := range result.Skills {
		if skill.Name == name {
			return &skill, nil
		}
	}

	return nil, nil
}

// FindArkSkills returns only Ark-managed skills
func (d *Detector) FindArkSkills() ([]DetectedSkill, error) {
	result, err := d.Detect()
	if err != nil {
		return nil, err
	}
	return result.ArkSkills, nil
}

// GetDefaultSkillsDir returns the first existing skills directory or creates default
func (d *Detector) GetDefaultSkillsDir() string {
	for _, dir := range SkillDirectories {
		path := filepath.Join(d.rootDir, dir)
		if dirExists(path) {
			return path
		}
	}
	// Default to "skills/"
	return filepath.Join(d.rootDir, "skills")
}

// dirExists checks if a directory exists
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// isArkSkillName checks if a skill name matches Ark naming pattern
func isArkSkillName(name string) bool {
	arkPatterns := []string{
		"ark-",
		"ark_",
		"repository-development",
		"repo-development",
	}
	lower := strings.ToLower(name)
	for _, pattern := range arkPatterns {
		if strings.HasPrefix(lower, pattern) {
			return true
		}
	}
	return false
}
