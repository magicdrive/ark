package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RepositoryOptions holds configuration for Repository Skill generation
type RepositoryOptions struct {
	Name      string
	Output    string
	Archive   bool
	Analysis  *RepoAnalysis
	NoInstall bool
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

	claudeAgentContent := generateClaudeCodeAgentMd(opts.Name, opts.Analysis)
	os.WriteFile(filepath.Join(agentsDir, "claude-code.md"), []byte(claudeAgentContent), 0644)
	if !opts.NoInstall {
		installClaudeCodeAgent(opts.Name, claudeAgentContent)
	}

	os.WriteFile(filepath.Join(refsDir, "conventions.md"), []byte(generateConventionsMd(opts.Analysis)), 0644)

	if opts.Archive {
		createArchive(opts.Output, opts.Output+".zip")
	}
	return nil
}

func generateRepositorySkillMd(name string, a *RepoAnalysis) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\nRepository-specific development skill with Ark MCP.\n\n", name))

	sb.WriteString("## Setup\n\n")
	sb.WriteString("Run once in your project root:\n\n")
	sb.WriteString(codeBlock("ark setup --name " + func() string {
		if a != nil && a.ProjectName != "" {
			return a.ProjectName
		}
		return "<name>"
	}()))
	sb.WriteString("\nThen restart Claude Code to approve the MCP server and activate the slash command.\n\n")

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

	sb.WriteString("\n## Code Exploration\n\n")
	sb.WriteString("**Core workflow** — start broad, drill down:\n\n")
	sb.WriteString(codeBlock("get_directory_tree   →   Understand project structure\n        ↓\n    find_symbol      →   Locate \"where is Foo?\"\n        ↓\n    get_symbols      →   List what's in a file\n        ↓\n    get_symbol       →   Extract exact source code"))
	sb.WriteString("\n\n### Available MCP Tools\n\n")
	sb.WriteString("| Tool | When to use |\n|------|-------------|\n")
	sb.WriteString("| `get_directory_tree` | First step — understand layout |\n")
	sb.WriteString("| `get_symbols` | List functions/types in a file **instead of reading it** |\n")
	sb.WriteString("| `find_symbol` | Search for a symbol by name across the repo |\n")
	sb.WriteString("| `get_symbol` | Get source code of one specific function/type |\n")
	sb.WriteString("| `search_in_files` | Full-text or regex search across files |\n")
	sb.WriteString("| `list_files` | Filter-aware file listing |\n")
	sb.WriteString("| `get_file_content` | Read whole file *(last resort)* |\n")
	sb.WriteString("| `get_file_info` | File metadata (size, lines, language) |\n")
	sb.WriteString("| `get_project_stats` | Language breakdown, file counts |\n")
	sb.WriteString("| `get_files_arklite` | Multiple files in compressed format |\n")
	sb.WriteString("\n### Usage Patterns\n\n")
	sb.WriteString("| Task | Tools |\n|------|-------|\n")
	sb.WriteString("| Find a function | `find_symbol` → `get_symbol` |\n")
	sb.WriteString("| Understand a file | `get_symbols` → `get_symbol` (as needed) |\n")
	sb.WriteString("| Explore a package | `get_directory_tree` → `get_symbols` |\n")
	sb.WriteString("| Search for string | `search_in_files` |\n")
	sb.WriteString("| Review architecture | `get_directory_tree` → `get_project_stats` |\n")

	sb.WriteString("\n## Conventions\n\nSee `references/conventions.md`.\n")
	return sb.String()
}

func generateRepoYaml(name string, a *RepoAnalysis) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("name: %s\ndescription: Repository development with Ark MCP\n\n", name))
	sb.WriteString("instructions: |\n  You are a development assistant for this repository.\n")
	if a != nil && a.PrimaryLang != "" {
		sb.WriteString(fmt.Sprintf("  Primary language: %s\n", a.PrimaryLang))
	}
	sb.WriteString("  \n")
	sb.WriteString("  Rules:\n")
	sb.WriteString("  1. NEVER read entire files immediately — use get_symbols first\n")
	sb.WriteString("  2. Use find_symbol to locate definitions before browsing\n")
	sb.WriteString("  3. Use get_symbol to retrieve exact source of a specific function/type\n")
	sb.WriteString("  4. Use get_file_content only when surrounding context is needed\n")
	sb.WriteString("  5. Start with get_directory_tree to understand structure\n\n")
	sb.WriteString("tools:\n")
	sb.WriteString("  - get_directory_tree\n")
	sb.WriteString("  - get_symbols\n")
	sb.WriteString("  - find_symbol\n")
	sb.WriteString("  - get_symbol\n")
	sb.WriteString("  - search_in_files\n")
	sb.WriteString("  - list_files\n")
	sb.WriteString("  - get_file_content\n")
	sb.WriteString("  - get_file_info\n")
	sb.WriteString("  - get_project_stats\n")
	sb.WriteString("  - get_files_arklite\n")
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
