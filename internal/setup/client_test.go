package setup

import (
	"strings"
	"testing"
)

func TestParseClientID(t *testing.T) {
	for _, id := range SupportedClientStrings() {
		got, err := ParseClientID(id)
		if err != nil {
			t.Errorf("%s: unexpected error %v", id, err)
		}
		if string(got) != id {
			t.Errorf("%s: got %s", id, got)
		}
	}

	_, err := ParseClientID("vscode")
	if err == nil {
		t.Fatal("expected error for unsupported client")
	}
	// Error must list the supported clients for actionability.
	for _, id := range SupportedClientStrings() {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("error should list %q; got: %v", id, err)
		}
	}
}

func TestSupportedClients_CanonicalOrder(t *testing.T) {
	want := []ClientID{ClientClaude, ClientCursor, ClientCodex, ClientCline, ClientCopilot, ClientCopilotCLI}
	got := SupportedClients()
	if len(got) != len(want) {
		t.Fatalf("expected %d clients, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("client %d: got %s want %s", i, got[i], want[i])
		}
	}
}

func TestDisplayName(t *testing.T) {
	cases := map[ClientID]string{
		ClientClaude: "Claude Code",
		ClientCursor: "Cursor",
		ClientCodex:  "Codex",
		ClientCline:  "Cline",
		// Names the surface: VS Code only, not the Copilot CLI or cloud agent.
		ClientCopilot:    "GitHub Copilot (VS Code)",
		ClientCopilotCLI: "GitHub Copilot CLI",
	}
	for id, want := range cases {
		if got := id.DisplayName(); got != want {
			t.Errorf("%s: got %q want %q", id, got, want)
		}
	}
}
