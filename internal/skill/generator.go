// Package skill provides Cline/ChatGPT Skill generation for Ark MCP.
package skill

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Options holds configuration for skill generation
type Options struct {
	Name    string
	Output  string
	Archive bool
}

// DefaultName returns the default skill name
func DefaultName() string {
	return "ark-code-explorer"
}

// Generate creates the skill directory structure and files
func Generate(opts Options) error {
	if opts.Name == "" {
		opts.Name = DefaultName()
	}
	if opts.Output == "" {
		opts.Output = opts.Name
	}

	// Create output directory
	if err := os.MkdirAll(opts.Output, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create agents directory
	agentsDir := filepath.Join(opts.Output, "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}

	// Generate SKILL.md
	skillMd := filepath.Join(opts.Output, "SKILL.md")
	if err := os.WriteFile(skillMd, []byte(generateSkillMd(opts.Name)), 0644); err != nil {
		return fmt.Errorf("failed to write SKILL.md: %w", err)
	}

	// Generate agents/openai.yaml
	openaiYaml := filepath.Join(agentsDir, "openai.yaml")
	if err := os.WriteFile(openaiYaml, []byte(generateOpenAIYaml(opts.Name)), 0644); err != nil {
		return fmt.Errorf("failed to write agents/openai.yaml: %w", err)
	}

	// Generate archive if requested
	if opts.Archive {
		archivePath := opts.Output + ".zip"
		if err := createArchive(opts.Output, archivePath); err != nil {
			return fmt.Errorf("failed to create archive: %w", err)
		}
		fmt.Printf("Archive created: %s\n", archivePath)
	}

	return nil
}

func generateSkillMd(name string) string {
	return fmt.Sprintf(`# %s

A Cline/ChatGPT Skill for intelligent code exploration using Ark MCP.

## Overview

This skill enables LLMs to efficiently explore codebases using Ark's code intelligence features. Instead of reading entire files, the LLM can:

1. Get directory structure
2. Search for symbol definitions
3. Extract specific symbols with source code
4. Navigate code hierarchically

## Core Principles

### Do Not Read Entire Files Immediately

Prefer the following workflow:

%s

### Recommended Tool Usage

| Task | Preferred Tools |
|------|----------------|
| "What is Foo?" | find_symbol |
| "Explain Foo's implementation" | find_symbol → get_symbol |
| "Describe this file's structure" | get_symbols → get_symbol (as needed) |
| "What does this package do?" | get_directory_tree → get_symbols → get_symbol |
| "Explain the repo architecture" | get_directory_tree → identify modules → get_symbols selectively |

## Available MCP Tools

### get_symbols
List functions, methods, types, classes and other symbols defined in a source file.
**Prefer this over reading the entire file when exploring code structure.**

### find_symbol
Search source code for symbol definitions by name across a repository or directory.

### get_symbol
Return the exact source and location of a symbol definition.
**Prefer this over reading an entire file when only one function, method, type or class is needed.**

### get_directory_tree
Get the directory structure of a repository.

### get_file_content
Read an entire file. Use only when surrounding context is actually needed.

### search_in_files
Search for text patterns across files.

## Workflow Examples

### Finding a Function

%s

### Understanding a Package

%s

## Best Practices

1. **Start with structure** - Use get_directory_tree to understand layout
2. **Use symbols** - Prefer get_symbols over reading entire files
3. **Be specific** - Use get_symbol for single definitions
4. **Search smart** - Use find_symbol before browsing
5. **Context when needed** - Only use get_file_content when surrounding code matters
`, name, codeBlock("get_directory_tree\n    ↓\nfind_symbol\n    ↓\nget_symbols\n    ↓\nget_symbol"),
		codeBlock("1. find_symbol(name: \"FunctionName\")\n2. get_symbol(path: \"found/path.go\", name: \"FunctionName\")"),
		codeBlock("1. get_directory_tree(path: \"internal/package\")\n2. get_symbols(path: \"internal/package/main.go\")\n3. get_symbol for key definitions"))
}

func generateOpenAIYaml(name string) string {
	return fmt.Sprintf(`# OpenAI/Cline Agent Configuration for %s
name: %s
description: Intelligent code exploration using Ark MCP

instructions: |
  You are a code exploration assistant using Ark MCP tools.
  
  ## Core Rules
  
  1. NEVER read entire files immediately
  2. ALWAYS prefer symbol-based exploration
  3. Use get_directory_tree first to understand structure
  4. Use find_symbol to locate definitions
  5. Use get_symbol to retrieve specific code
  6. Only use get_file_content when context around a symbol is needed
  
  ## Preferred Workflow
  
  1. get_directory_tree - understand layout
  2. get_symbols - understand file structure  
  3. find_symbol - search for specific names
  4. get_symbol - retrieve source code
  5. get_file_content - only when absolutely necessary

tools:
  - get_directory_tree
  - get_symbols
  - find_symbol
  - get_symbol
  - get_file_content
  - search_in_files
  - list_files
  - get_file_info
`, name, name)
}

func codeBlock(content string) string {
	return "```\n" + content + "\n```"
}

func installClaudeCodeAgent(name, content string) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	commandsDir := filepath.Join(cwd, ".claude", "commands")
	if err := os.MkdirAll(commandsDir, 0755); err != nil {
		fmt.Printf("Warning: could not create .claude/commands/: %v\n", err)
		return
	}
	dest := filepath.Join(commandsDir, name+".md")
	if err := os.WriteFile(dest, []byte(content), 0644); err != nil {
		fmt.Printf("Warning: could not install Claude Code command: %v\n", err)
		return
	}
	fmt.Printf("Installed Claude Code command: %s\n", dest)
	fmt.Printf("  Use as: /%s\n", name)
}

func generateClaudeCodeAgentMd(name string, a *RepoAnalysis) string {
	description := "Efficiently explore and understand this codebase using Ark MCP tools"
	langNote := ""
	if a != nil && a.PrimaryLang != "" {
		langNote = fmt.Sprintf("\nPrimary language: %s\n", a.PrimaryLang)
	}

	return fmt.Sprintf(`---
description: %s
tools:
  - mcp__ark__get_directory_tree
  - mcp__ark__get_symbols
  - mcp__ark__find_symbol
  - mcp__ark__get_symbol
  - mcp__ark__search_in_files
  - mcp__ark__list_files
  - mcp__ark__get_file_content
  - mcp__ark__get_file_info
  - mcp__ark__get_project_stats
  - mcp__ark__get_files_arklite
---

# %s%s
You are a code exploration assistant. Use Ark MCP tools efficiently.

## Rules

1. NEVER read entire files immediately — use `+"`mcp__ark__get_symbols`"+` first
2. Use `+"`mcp__ark__find_symbol`"+` to locate definitions before browsing
3. Use `+"`mcp__ark__get_symbol`"+` to retrieve exact source of a specific function/type
4. Use `+"`mcp__ark__get_file_content`"+` only when surrounding context is needed
5. Start exploration with `+"`mcp__ark__get_directory_tree`"+` to understand structure

## Tool Selection

| Goal | Tool |
|------|------|
| Understand project structure | `+"`mcp__ark__get_directory_tree`"+` |
| List a file's functions/types | `+"`mcp__ark__get_symbols`"+` |
| Find "where is Foo?" | `+"`mcp__ark__find_symbol`"+` |
| Get Foo's source code | `+"`mcp__ark__get_symbol`"+` |
| Search for a string | `+"`mcp__ark__search_in_files`"+` |
| Read a whole file | `+"`mcp__ark__get_file_content`"+` *(last resort)* |
| File metadata | `+"`mcp__ark__get_file_info`"+` |
| Language/file breakdown | `+"`mcp__ark__get_project_stats`"+` |
| Multiple files at once | `+"`mcp__ark__get_files_arklite`"+` |
`, description, name, langNote)
}

func createArchive(sourceDir, destPath string) error {
	zipFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	w := zip.NewWriter(zipFile)
	defer w.Close()

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// Create proper path in zip (with skill name as root)
		zipPath := filepath.Join(filepath.Base(sourceDir), relPath)

		if info.IsDir() {
			_, err := w.Create(zipPath + "/")
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		f, err := w.Create(zipPath)
		if err != nil {
			return err
		}

		_, err = io.Copy(f, file)
		return err
	})
}
