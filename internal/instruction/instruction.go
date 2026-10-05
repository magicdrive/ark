// Package instruction holds Ark's agent-facing usage guidance and the targets
// it can be rendered for.
//
// Two separate concerns live here, deliberately small:
//
//   - Guidance: the vendor-neutral knowledge of how a coding agent should use
//     the Ark MCP tools. It names tools by their Ark MCP names and says nothing
//     about any particular agent.
//   - Target: how that guidance is presented to one agent. Targets are NOT the
//     setup clients of internal/setup (a different concern with a different
//     registry); only `claude` exists today.
//
// Dependencies point one way: targets import the guidance, never the reverse,
// so another target can be added later without touching the guidance.
package instruction

import (
	_ "embed"
	"regexp"
)

//go:embed guidance.md
var guidance string

// Guidance returns the canonical, vendor-neutral Ark MCP usage guidance as a
// Markdown section. Tool names appear in backticks exactly as the MCP server
// registers them.
func Guidance() string { return guidance }

// toolName matches a backticked Ark MCP tool name (snake_case with at least one
// underscore). The guidance mentions no other backticked snake_case words.
var toolName = regexp.MustCompile("`([a-z]+(?:_[a-z]+)+)`")

// GuidanceTools returns the MCP tool names the guidance mentions, in order of
// first appearance and without duplicates.
func GuidanceTools() []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range toolName.FindAllStringSubmatch(guidance, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}
