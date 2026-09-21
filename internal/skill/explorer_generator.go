package skill

import (
	"fmt"
	"os"
	"path/filepath"
)

// ExplorerOptions holds configuration for Explorer Skill generation
type ExplorerOptions struct {
	Name    string
	Output  string
	Archive bool
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

	// Generate SKILL.md with frontmatter
	skillContent := generateExplorerSkillMd(opts.Name)
	wrappedContent := WrapWithFrontmatter(skillContent, SkillTypeExplorer)
	skillMd := filepath.Join(opts.Output, "SKILL.md")
	if err := os.WriteFile(skillMd, []byte(wrappedContent), 0644); err != nil {
		return fmt.Errorf("failed to write SKILL.md: %w", err)
	}

	// Generate agents/openai.yaml
	openaiYaml := filepath.Join(agentsDir, "openai.yaml")
	if err := os.WriteFile(openaiYaml, []byte(generateExplorerOpenAIYaml(opts.Name)), 0644); err != nil {
		return fmt.Errorf("failed to write agents/openai.yaml: %w", err)
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

## Purpose

This skill provides code navigation capabilities to complement existing repository skills.
Use it to efficiently explore and understand code structure without reading entire files.

## Core Workflow

%s

## MCP Tools

### get_symbols
List all symbols (functions, types, classes) in a file.
**Use instead of reading entire files.**

### find_symbol
Search for symbol definitions by name across the repository.

### get_symbol
Get the exact source code of a specific symbol.

### get_directory_tree
View the directory structure.

## Usage Patterns

| Task | Tools |
|------|-------|
| Find a function | find_symbol → get_symbol |
| Understand a file | get_symbols → get_symbol (as needed) |
| Explore a package | get_directory_tree → get_symbols |

## Best Practices

1. **Never read entire files first** - Use get_symbols
2. **Search before browsing** - Use find_symbol
3. **Be specific** - Use get_symbol for single definitions
4. **Explore hierarchically** - Start with directory tree
`, name, codeBlock("get_directory_tree\n    ↓\nget_symbols / find_symbol\n    ↓\nget_symbol"))
}

func generateExplorerOpenAIYaml(name string) string {
	return fmt.Sprintf(`name: %s
description: Code exploration companion using Ark MCP

instructions: |
  You are a code exploration assistant. Use Ark MCP tools efficiently:
  
  1. NEVER read entire files immediately
  2. Use get_symbols to understand file structure
  3. Use find_symbol to search for definitions
  4. Use get_symbol to retrieve specific code
  5. Only use get_file_content when context is needed

tools:
  - get_directory_tree
  - get_symbols
  - find_symbol
  - get_symbol
  - get_file_content
  - search_in_files
`, name)
}
