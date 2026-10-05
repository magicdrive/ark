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

This skill complements existing repository skills with Ark's repository, context and graph intelligence: it helps an agent find the code that matters for a question or a change, without needless reading or tool calls.

%s`, name, codeBlock("ark mcp-init"), markdownGuidance())
}

func generateExplorerOpenAIYaml(name string) string {
	return fmt.Sprintf(`name: %s
description: Code exploration companion using Ark MCP

instructions: |
  You are a code exploration assistant. Use Ark MCP tools efficiently.

%s
%s`, name, yamlBlockGuidance(), yamlTools())
}
