package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UpdateResult holds the result of an update operation
type UpdateResult struct {
	SkillName    string
	SkillType    string
	Updated      []string
	Preserved    []string
	Skipped      []string
	HasConflicts bool
	Conflicts    []string
}

// Updater handles safe skill updates
type Updater struct {
	rootDir string
}

// NewUpdater creates a new skill updater
func NewUpdater(rootDir string) *Updater {
	return &Updater{rootDir: rootDir}
}

// Update updates all Ark-managed skills
func (u *Updater) Update(force bool) ([]UpdateResult, error) {
	arkSkills, err := NewDetector(u.rootDir).FindArkSkills()
	if err != nil {
		return nil, err
	}
	if len(arkSkills) == 0 {
		return nil, fmt.Errorf("no Ark-managed skills found")
	}

	var results []UpdateResult
	for _, s := range arkSkills {
		result := u.updateSkill(s, force)
		results = append(results, result)
	}
	return results, nil
}

func (u *Updater) updateSkill(s DetectedSkill, force bool) UpdateResult {
	result := UpdateResult{SkillName: s.Name, SkillType: s.SkillType}

	// Check for user modifications
	content, _ := os.ReadFile(filepath.Join(s.Path, "SKILL.md"))
	modified, _ := HasUserModifications(string(content))
	if modified && !force {
		result.HasConflicts = true
		result.Conflicts = append(result.Conflicts, "SKILL.md")
		result.Skipped = append(result.Skipped, "SKILL.md")
		return result
	}

	// Preserve user files in references/
	result.Preserved = u.listUserFiles(s.Path)

	// Regenerate
	switch s.SkillType {
	case SkillTypeExplorer:
		u.regenExplorer(s)
	case SkillTypeRepository:
		analysis, _ := NewAnalyzer(u.rootDir).Analyze()
		u.regenRepository(s, analysis)
	}
	result.Updated = []string{"SKILL.md", "agents/openai.yaml"}
	return result
}

func (u *Updater) listUserFiles(skillPath string) []string {
	var files []string
	refsDir := filepath.Join(skillPath, "references")
	if !dirExists(refsDir) {
		return files
	}
	filepath.Walk(refsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(skillPath, path)
		if rel != "references/conventions.md" {
			files = append(files, rel)
		}
		return nil
	})
	return files
}

func (u *Updater) regenExplorer(s DetectedSkill) {
	content := WrapWithFrontmatter(generateExplorerSkillMd(s.Name), SkillTypeExplorer)
	os.WriteFile(filepath.Join(s.Path, "SKILL.md"), []byte(content), 0644)
	os.MkdirAll(filepath.Join(s.Path, "agents"), 0755)
	os.WriteFile(filepath.Join(s.Path, "agents/openai.yaml"), []byte(generateExplorerOpenAIYaml(s.Name)), 0644)
}

func (u *Updater) regenRepository(s DetectedSkill, analysis *RepoAnalysis) {
	content := WrapWithFrontmatter(generateRepositorySkillMd(s.Name, analysis), SkillTypeRepository)
	os.WriteFile(filepath.Join(s.Path, "SKILL.md"), []byte(content), 0644)
	os.MkdirAll(filepath.Join(s.Path, "agents"), 0755)
	os.WriteFile(filepath.Join(s.Path, "agents/openai.yaml"), []byte(generateRepoYaml(s.Name, analysis)), 0644)
}

// CheckConflicts checks for user modifications
func (u *Updater) CheckConflicts() []string {
	arkSkills, _ := NewDetector(u.rootDir).FindArkSkills()
	var conflicts []string
	for _, s := range arkSkills {
		content, _ := os.ReadFile(filepath.Join(s.Path, "SKILL.md"))
		if modified, _ := HasUserModifications(string(content)); modified {
			conflicts = append(conflicts, s.Name)
		}
	}
	return conflicts
}

// FormatResults formats update results for display
func FormatResults(results []UpdateResult) string {
	var sb strings.Builder
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("Skill: %s (%s)\n", r.SkillName, r.SkillType))
		if len(r.Updated) > 0 {
			sb.WriteString("  Updated: " + strings.Join(r.Updated, ", ") + "\n")
		}
		if len(r.Preserved) > 0 {
			sb.WriteString("  Preserved: " + strings.Join(r.Preserved, ", ") + "\n")
		}
		if r.HasConflicts {
			sb.WriteString("  Conflicts: " + strings.Join(r.Conflicts, ", ") + " (use --force)\n")
		}
	}
	return sb.String()
}
