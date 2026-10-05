package instruction

// toolMCPPrefix is how Claude Code names the tools of an MCP server called
// "ark" (the server name `ark setup claude` registers): mcp__<server>__<tool>.
const toolMCPPrefix = "mcp__ark__"

// renderClaude presents the canonical guidance as Markdown to paste into a
// CLAUDE.md. The only target-specific change is naming each tool the way Claude
// Code exposes it; the guidance itself is unchanged.
func renderClaude(guidance string) string {
	return toolName.ReplaceAllString(guidance, "`"+toolMCPPrefix+"$1`")
}
