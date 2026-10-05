package instruction

import (
	"fmt"
	"strings"
)

// A target renders the canonical guidance for one agent.
type targetInfo struct {
	name   string
	render func(guidance string) string
}

// targets is the ordered registry of instruction targets. It is independent of
// the setup client registry: supporting an agent's MCP setup says nothing about
// how (or whether) it takes standing instructions.
var targets = []targetInfo{
	{"claude", renderClaude},
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
