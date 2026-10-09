package skill

import (
	_ "embed"
	"regexp"
	"strings"
)

// usageGuidance is the operational guidance every Ark-generated skill artifact
// teaches: a goal-driven model of the Ark MCP tools. It is deliberately longer
// and more task-oriented than the short standing instruction printed by
// `ark instruction`; the two share one Ark usage model but not their text, and
// neither imports the other.
//
//go:embed usage_guidance.md
var usageGuidance string

// toolName matches a backticked Ark MCP tool name (snake_case with at least one
// underscore); the guidance mentions no other backticked snake_case words.
var toolName = regexp.MustCompile("`([a-z]+(?:_[a-z]+)+)`")

// mcpToolPrefix is how Claude Code names the tools of the MCP server "ark".
const mcpToolPrefix = "mcp__ark__"

// skillTools are the MCP tools every Ark skill grants (bare names), in the
// order they are listed in generated tool lists. The guidance mentions the
// first group; the rest are supporting tools skills have always granted.
var skillTools = []string{
	"get_repository_map", "get_directory_tree", "find_symbol", "get_symbols",
	"get_context", "search_context", "get_symbol", "get_relations", "get_callers", "get_callees",
	"analyze_change_impact", "search_code", "search_in_files", "get_file_content",
	"list_files", "get_file_info", "get_project_stats", "get_files_arklite",
	"get_diagnostics",
}

// markdownGuidance returns the guidance with bare tool names (SKILL.md).
func markdownGuidance() string { return strings.TrimRight(usageGuidance, "\n") + "\n" }

// claudeGuidance returns the guidance with tool names as Claude Code exposes
// them (the slash-command / agents/claude-code.md presentation).
func claudeGuidance() string {
	return toolName.ReplaceAllString(markdownGuidance(), "`"+mcpToolPrefix+"$1`")
}

// yamlBlockGuidance indents the guidance as the body of a YAML literal block.
func yamlBlockGuidance() string {
	lines := strings.Split(strings.TrimRight(usageGuidance, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = "  " + l
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// yamlTools renders the `tools:` list of agents/openai.yaml.
func yamlTools() string {
	var sb strings.Builder
	sb.WriteString("tools:\n")
	for _, t := range skillTools {
		sb.WriteString("  - " + t + "\n")
	}
	return sb.String()
}

// claudeToolsFrontmatter renders the `tools:` list of the Claude Code command.
func claudeToolsFrontmatter() string {
	var sb strings.Builder
	sb.WriteString("tools:\n")
	for _, t := range skillTools {
		sb.WriteString("  - " + mcpToolPrefix + t + "\n")
	}
	return sb.String()
}
