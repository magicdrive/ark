package setup

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/magicdrive/ark/internal/mcp"
	"github.com/magicdrive/ark/internal/skill"
)

// claudeAdapter configures Ark for Claude Code (plan §14 Claude).
//
//	project:  <cwd>/.mcp.json, written by Ark (jsonClient)
//	global:   Claude Code's user scope, the top-level "mcpServers" of
//	          ~/.claude.json, changed only through the claude CLI
//	          (claudeuser.go)
//	entry:    {type:"stdio", command, args:["mcp-server","--root",<root>], env:{}}
//	          where <root> is "${CLAUDE_PROJECT_DIR:-.}/" when the project
//	          scope serves cwd, and the absolute root otherwise (always for
//	          the user scope, which is shared by every project).
//
// Only this adapter produces Claude-specific artifacts (the repository skill
// and the CLAUDE.md advisory). No other adapter writes .claude/* (plan §14).
type claudeAdapter struct {
	base   jsonClient
	runner commandRunner // the claude CLI, for the user scope
}

func newClaudeAdapter() adapter {
	return claudeAdapter{
		base: jsonClient{
			id:          ClientClaude,
			serverName:  claudeServerName,
			managedBy:   "config file",
			resolvePath: claudeConfigPath,
			buildEntry:  claudeEntry,
		},
		runner: execRunner{},
	}
}

// claudeConfigPath is the project-scope file. The user scope is not a file
// Ark writes; see claudeUserConfigPath.
func claudeConfigPath(opts Options) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine current directory: %w", err)
	}
	return filepath.Join(cwd, ".mcp.json"), nil
}

// claudeEntry mirrors the historical mcp-init output so existing Claude
// installations see no artifact drift (plan §40). For project scope that serves
// the current directory, the portable ${CLAUDE_PROJECT_DIR:-.}/ placeholder is
// used instead of an absolute path. The user scope always names the absolute
// root: one user-scoped entry serves every project, so the placeholder would
// serve whatever directory Claude Code was started in.
func claudeEntry(opts Options) map[string]any {
	rootArg := opts.RootDir
	if !opts.Global {
		if cwd, err := os.Getwd(); err == nil {
			realRoot, err1 := filepath.EvalSymlinks(opts.RootDir)
			realCwd, err2 := filepath.EvalSymlinks(cwd)
			if err1 == nil && err2 == nil && realRoot == realCwd {
				rootArg = "${CLAUDE_PROJECT_DIR:-.}/"
			}
		}
	}
	return map[string]any{
		"type":    "stdio",
		"command": opts.ArkPath,
		"args":    []string{"mcp-server", "--root", rootArg},
		"env":     map[string]any{},
	}
}

func (a claudeAdapter) run(opts Options) (*Result, error) {
	var res *Result
	var err error
	if opts.Global {
		res, err = a.runUserScope(opts)
	} else {
		res, err = a.base.applyJSON(opts)
	}
	if err != nil {
		return nil, err
	}

	// Claude-specific enhancement: generate the repository skill / slash command.
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot determine current directory: %w", err)
	}
	name := opts.Name
	if name == "" {
		name = filepath.Base(opts.RootDir)
	}
	analysis, _ := skill.NewAnalyzer(cwd).Analyze()
	output := filepath.Join("skills", name)
	if err := skill.GenerateRepository(skill.RepositoryOptions{
		Name:     name,
		Output:   output,
		Analysis: analysis,
		Archive:  false,
	}); err != nil {
		return nil, fmt.Errorf("MCP server configured, but skill generation failed: %w", err)
	}
	res.ExtraLines = append(res.ExtraLines, fmt.Sprintf("✓ Claude skill: %s", output))
	res.ExtraLines = append(res.ExtraLines, fmt.Sprintf("Next: restart Claude Code to approve the MCP server, then use /%s", name))

	if suggestion := mcp.CLAUDEMdSuggestion(cwd); suggestion != "" {
		res.ExtraLines = append(res.ExtraLines, suggestion)
	}
	return res, nil
}
