package setup

import (
	"fmt"
	"strings"
)

// ClientID identifies a supported coding-agent client.
//
// The registry defined in this file is the single source of truth for which
// clients Ark can configure. Parser, help text, completion and dispatcher must
// all derive from it (completion is a static file kept in sync by tests).
type ClientID string

const (
	ClientClaude ClientID = "claude"
	ClientCursor ClientID = "cursor"
	ClientCodex  ClientID = "codex"
	ClientCline  ClientID = "cline"
)

// clientInfo holds registry metadata for a supported client.
type clientInfo struct {
	id          ClientID
	displayName string
	description string
}

// registry is the ordered, authoritative list of supported clients.
var registry = []clientInfo{
	{ClientClaude, "Claude Code", "Configure Ark for Claude Code"},
	{ClientCursor, "Cursor", "Configure Ark for Cursor"},
	{ClientCodex, "Codex", "Configure Ark for Codex"},
	{ClientCline, "Cline", "Configure Ark for Cline"},
}

// SupportedClients returns the ordered list of supported client IDs.
func SupportedClients() []ClientID {
	ids := make([]ClientID, 0, len(registry))
	for _, c := range registry {
		ids = append(ids, c.id)
	}
	return ids
}

// SupportedClientStrings returns the supported client IDs as strings.
func SupportedClientStrings() []string {
	ids := SupportedClients()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
}

// ClientDescriptions returns id/description pairs for help and completion.
func ClientDescriptions() [][2]string {
	out := make([][2]string, 0, len(registry))
	for _, c := range registry {
		out = append(out, [2]string{string(c.id), c.description})
	}
	return out
}

// DisplayName returns the human-facing name for a client (e.g. "Cursor").
func (c ClientID) DisplayName() string {
	for _, info := range registry {
		if info.id == c {
			return info.displayName
		}
	}
	return string(c)
}

// ParseClientID validates a raw client string against the registry.
// An unknown client yields an error listing the supported clients, so the
// caller can exit non-zero with an actionable message (plan §5).
func ParseClientID(raw string) (ClientID, error) {
	candidate := ClientID(strings.TrimSpace(raw))
	for _, info := range registry {
		if info.id == candidate {
			return info.id, nil
		}
	}
	supported := SupportedClientStrings()
	return "", fmt.Errorf("unsupported client %q\n\nSupported clients:\n  %s",
		raw, strings.Join(supported, "\n  "))
}
