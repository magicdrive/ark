package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RepositoryOptions holds configuration for Repository Skill generation
type RepositoryOptions struct {
	Name     string
	Output   string
	Archive  bool
	Analysis *RepoAnalysis
}

// GenerateRepository creates a Repository Skill (for repos without existing skills)
func GenerateRepository(opts RepositoryOptions) error {
	if opts.Name == "" {
		opts.Name = "repository-development"
	}
	if opts.Output == "" {
		opts.Output = opts.Name
	}

	if err := os.MkdirAll(opts.Output, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	agentsDir := filepath.Join(opts.Output, "agents")
	os.MkdirAll(agentsDir, 0755)
	refsDir := filepath.Join(opts.Output, "references")
	os.MkdirAll(refsDir, 0755)

	skillContent := generateRepositorySkillMd(opts.Name, opts.Analysis)
	wrappedContent := WrapWithFrontmatter(skillContent, SkillTypeRepository)
	os.WriteFile(filepath.Join(opts.Output, "SKILL.md"), []byte(wrappedContent), 0644)
	os.WriteFile(filepath.Join(agentsDir, "openai.yaml"), []byte(generateRepoYaml(opts.Name, opts.Analysis)), 0644)
	os.WriteFile(filepath.Join(refsDir, "conventions.md"), []byte(generateConventionsMd(opts.Analysis)), 0644)

	if opts.Archive {
		createArchive(opts.Output, opts.Output+".zip")
	}
	return nil
}

func generateRepositorySkillMd(name string, a *RepoAnalysis) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\nRepository-specific development skill with Ark MCP.\n\n", name))
	sb.WriteString("## Project Overview\n\n")
	if a != nil {
		sb.WriteString(fmt.Sprintf("- **Project**: %s\n", a.ProjectName))
		if len(a.Languages) > 0 {
			var langs []string
			for _, l := range a.Languages {
				langs = append(langs, l.Name)
			}
			sb.WriteString(fmt.Sprintf("- **Languages**: %s\n", strings.Join(langs, ", ")))
		}
	}
	sb.WriteString("\n## Build & Test\n\n")
	if a != nil && len(a.BuildCommands) > 0 {
		sb.WriteString("| Type | Command |\n|------|--------|\n")
		for _, cmd := range a.BuildCommands {
			sb.WriteString(fmt.Sprintf("| %s | `%s` |\n", cmd.Type, cmd.Command))
		}
	}
	if a != nil && len(a.Modules) > 0 {
		sb.WriteString("\n## Key Modules\n\n")
		for _, m := range a.Modules {
			sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", m.Path, m.Type))
		}
	}
	sb.WriteString("\n## Code Exploration\n\nUse Ark MCP tools:\n\n")
	sb.WriteString(codeBlock("get_directory_tree → get_symbols → get_symbol"))
	sb.WriteString("\n\n## Conventions\n\nSee `references/conventions.md`.\n")
	return sb.String()
}

func generateRepoYaml(name string, a *RepoAnalysis) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("name: %s\ndescription: Repository development with Ark MCP\n\n", name))
	sb.WriteString("instructions: |\n  You are a development assistant for this repository.\n")
	if a != nil && a.PrimaryLang != "" {
		sb.WriteString(fmt.Sprintf("  Primary language: %s\n", a.PrimaryLang))
	}
	sb.WriteString("  Use get_symbols instead of reading files.\n\n")
	sb.WriteString("tools:\n  - get_directory_tree\n  - get_symbols\n  - find_symbol\n  - get_symbol\n")
	return sb.String()
}

func generateConventionsMd(a *RepoAnalysis) string {
	var sb strings.Builder
	sb.WriteString("# Project Conventions\n\n")
	if a != nil && a.PrimaryLang != "" {
		sb.WriteString(fmt.Sprintf("## %s\n\n", a.PrimaryLang))
		switch a.PrimaryLang {
		case "Go":
			sb.WriteString("- Follow `gofmt` formatting\n- Document exported functions\n")
		case "TypeScript", "JavaScript":
			sb.WriteString("- Use consistent formatting\n")
		case "Python":
			sb.WriteString("- Follow PEP 8\n")
		}
	}
	sb.WriteString("\n## Git\n\n- Add conventions here\n")
	return sb.String()
}
