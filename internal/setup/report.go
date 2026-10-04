package setup

import (
	"fmt"
	"strings"
)

// DeprecatedNoClientWarning is shown when `ark setup` is run without a client.
// `ark setup` remains a Claude Code alias during v4.x for backward
// compatibility (plan §6).
const DeprecatedNoClientWarning = `Warning: "ark setup" without a client is deprecated.
Using "claude" for backward compatibility.

Use:
  ark setup claude`

// Report renders the human-facing setup output (plan §18).
func (r *Result) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Setting up Ark for %s...\n\n", r.Client.DisplayName())
	fmt.Fprintf(&b, "✓ MCP server: %s\n", r.ServerName)
	fmt.Fprintf(&b, "✓ Scope: %s\n", r.Scope)
	fmt.Fprintf(&b, "✓ Root: %s\n", r.Root)
	if r.ConfigPath != "" {
		fmt.Fprintf(&b, "✓ Config: %s\n", r.ConfigPath)
	} else if r.ManagedBy != "" {
		fmt.Fprintf(&b, "✓ Managed by: %s\n", r.ManagedBy)
	}

	b.WriteString("\n")
	switch {
	case r.State == StateEquivalent:
		fmt.Fprintf(&b, "Ark is already configured for %s.\nNo changes required.\n", r.Client.DisplayName())
	case r.State == StateConflict:
		fmt.Fprintf(&b, "Updated existing Ark configuration for %s.\n", r.Client.DisplayName())
	default:
		fmt.Fprintf(&b, "Ark is ready for %s.\n", r.Client.DisplayName())
	}

	for _, line := range r.ExtraLines {
		b.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}
