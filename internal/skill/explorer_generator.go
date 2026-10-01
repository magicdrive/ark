package skill

import (
	"fmt"
	"os"
	"path/filepath"
)

// ExplorerOptions holds configuration for Explorer Skill generation
type ExplorerOptions struct {
	Name      string
	Output    string
	Archive   bool
	NoInstall bool
}

// GenerateExplorer creates an Explorer Skill (for repos with existing skills)
func GenerateExplorer(opts ExplorerOptions) error {
	if opts.Name == "" {
		opts.Name = "ark-code-explorer"
	}
	if opts.Output == "" {
		opts.Output = opts.Name
	}

	if err := os.MkdirAll(opts.Output, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	agentsDir := filepath.Join(opts.Output, "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}

	skillContent := generateExplorerSkillMd(opts.Name)
	wrappedContent := WrapWithFrontmatter(skillContent, SkillTypeExplorer)
	skillMd := filepath.Join(opts.Output, "SKILL.md")
	if err := os.WriteFile(skillMd, []byte(wrappedContent), 0644); err != nil {
		return fmt.Errorf("failed to write SKILL.md: %w", err)
	}

	openaiYaml := filepath.Join(agentsDir, "openai.yaml")
	if err := os.WriteFile(openaiYaml, []byte(generateExplorerOpenAIYaml(opts.Name)), 0644); err != nil {
		return fmt.Errorf("failed to write agents/openai.yaml: %w", err)
	}

	claudeAgentContent := generateClaudeCodeAgentMd(opts.Name, nil)
	claudeCodeMd := filepath.Join(agentsDir, "claude-code.md")
	if err := os.WriteFile(claudeCodeMd, []byte(claudeAgentContent), 0644); err != nil {
		return fmt.Errorf("failed to write agents/claude-code.md: %w", err)
	}
	if !opts.NoInstall {
		installClaudeCodeAgent(opts.Name, claudeAgentContent)
	}

	if opts.Archive {
		if err := createArchive(opts.Output, opts.Output+".zip"); err != nil {
			return fmt.Errorf("failed to create archive: %w", err)
		}
	}
	return nil
}

func generateExplorerSkillMd(name string) string {
	return fmt.Sprintf(`# %s

A companion skill for efficient code exploration using Ark MCP.

## Setup

Run once in your project root to register the MCP server:

%s

Then restart Claude Code / your AI client to activate the tools.

## Purpose

This skill provides code navigation capabilities to complement existing repository skills.
Use it to efficiently explore and understand code structure without reading entire files.

## Core Workflow

%s

## MCP Tools

| Tool | When to use |
|------|-------------|
| `+"`get_directory_tree`"+` | First step — understand layout |
| `+"`get_symbols`"+` | List functions/types in a file **instead of reading it** |
| `+"`find_symbol`"+` | Search for a symbol by name across the repo |
| `+"`get_symbol`"+` | Get source code of one specific function/type |
| `+"`search_in_files`"+` | Full-text or regex search across files |
| `+"`list_files`"+` | Filter-aware file listing |
| `+"`get_file_content`"+` | Read whole file *(last resort)* |
| `+"`get_file_info`"+` | File metadata (size, lines, language) |
| `+"`get_project_stats`"+` | Language breakdown, file counts |
| `+"`get_files_arklite`"+` | Multiple files in compressed format |

## Usage Patterns

| Task | Tools |
|------|-------|
| Find a function | `+"`find_symbol`"+` → `+"`get_symbol`"+` |
| Understand a file | `+"`get_symbols`"+` → `+"`get_symbol`"+` (as needed) |
| Explore a package | `+"`get_directory_tree`"+` → `+"`get_symbols`"+` |
| Search for string | `+"`search_in_files`"+` |
| Review architecture | `+"`get_directory_tree`"+` → `+"`get_project_stats`"+` |

## Best Practices

1. **Never read entire files first** — use `+"`get_symbols`"+`
2. **Search before browsing** — use `+"`find_symbol`"+`
3. **Be specific** — use `+"`get_symbol`"+` for single definitions
4. **Explore hierarchically** — start with directory tree
`, name,
		codeBlock("ark mcp-init"),
		codeBlock("get_directory_tree\n    ↓\nget_symbols / find_symbol\n    ↓\nget_symbol"))
}

func generateExplorerOpenAIYaml(name string) string {
	return fmt.Sprintf(`name: %s
description: Code exploration companion using Ark MCP

instructions: |
  You are a code exploration assistant. Use Ark MCP tools efficiently:

  Rules:
  1. NEVER read entire files immediately
  2. Use get_symbols to understand file structure
  3. Use find_symbol to search for definitions
  4. Use get_symbol to retrieve specific code
  5. Only use get_file_content when context is needed
  6. Start with get_directory_tree to understand structure

tools:
  - get_directory_tree
  - get_symbols
  - find_symbol
  - get_symbol
  - search_in_files
  - list_files
  - get_file_content
  - get_file_info
  - get_project_stats
  - get_files_arklite
`, name)
}
