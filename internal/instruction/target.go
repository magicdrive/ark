package instruction

import (
	"fmt"
	"strings"
)

// A target renders the canonical guidance for one agent. destination is the
// suggested paste location for docs/help only; Render never writes it.
type targetInfo struct {
	name        string
	destination string
	render      func(guidance string) string
}

// targets is the ordered registry of instruction targets. It is independent
// of the setup client registry (internal/setup): supporting an agent's MCP
// setup says nothing about how (or whether) it takes standing instructions.
// The two registries currently name the same six agents, but that is
// coincidence, not architecture — this file never reads internal/setup, and
// nothing here is derived from it.
//
// Only `claude` has an officially documented, model-visible MCP tool-naming
// convention (mcp__<server>__<tool>), so only it gets a dedicated renderer.
// Every other target uses renderPlain: the canonical guidance's bare tool
// names, unchanged: no other agent documents a naming convention, so any other
// synthetic name would be a guess.
var targets = []targetInfo{
	{"claude", "CLAUDE.md", renderClaude},
	{"codex", "AGENTS.md", renderPlain},
	{"cursor", "AGENTS.md", renderPlain},
	{"cline", ".clinerules/ark.md", renderPlain},
	{"copilot-vscode", ".github/copilot-instructions.md", renderPlain},
	{"copilot-cli", ".github/copilot-instructions.md", renderPlain},
}

// Targets returns the supported instruction target names in canonical order.
func Targets() []string {
	out := make([]string, len(targets))
	for i, t := range targets {
		out[i] = t.name
	}
	return out
}

// Render returns the instruction for the named target. An unknown target is an
// error that lists the supported ones.
func Render(target string) (string, error) {
	for _, t := range targets {
		if t.name == target {
			return t.render(Guidance()), nil
		}
	}
	return "", fmt.Errorf("unsupported instruction target %q\n\nsupported targets: %s", target, strings.Join(Targets(), ", "))
}

// Destination returns the suggested paste destination for the named target
// (documentation only; Ark never writes to it). An unknown target is an error
// that lists the supported ones.
func Destination(target string) (string, error) {
	for _, t := range targets {
		if t.name == target {
			return t.destination, nil
		}
	}
	return "", fmt.Errorf("unsupported instruction target %q\n\nsupported targets: %s", target, strings.Join(Targets(), ", "))
}
