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

A skill for code exploration and understanding with Ark MCP.

%s`, name, markdownGuidance())
}

func generateOpenAIYaml(name string) string {
	return fmt.Sprintf(`# OpenAI/Cline Agent Configuration for %s
name: %s
description: Code exploration and understanding with Ark MCP

instructions: |
  You are a code exploration assistant using Ark MCP tools.

%s
%s`, name, name, yamlBlockGuidance(), yamlTools())
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
%s---

# %s%s
You are a code exploration assistant. Use Ark MCP tools efficiently.

%s`, description, claudeToolsFrontmatter(), name, langNote, claudeGuidance())
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
