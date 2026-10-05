package instruction

// renderPlain returns the canonical guidance unchanged. It is shared by every
// target whose agent has no officially documented, model-visible MCP
// tool-naming convention to render to: inventing one (a server prefix, a
// qualified name, ...) would fabricate a relationship Ark cannot verify, so
// the guidance's bare tool names (exactly as the MCP server registers them)
// are the safest presentation.
func renderPlain(guidance string) string { return guidance }
