package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseCodexList_CurrentTransportFormat is the authoritative contract test:
// the real `codex mcp list --json` output nests stdio details under "transport".
// The ark entry must be decoded with its real command/args, the HTTP entry must
// NOT be treated as a stdio server with an empty command.
func TestParseCodexList_CurrentTransportFormat(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "codex", "list_current_format.json"))
	if err != nil {
		t.Fatal(err)
	}
	servers, err := parseCodexList(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	ark, ok := servers["ark"]
	if !ok {
		t.Fatal("ark entry not found")
	}
	if ark.transportType != "stdio" {
		t.Errorf("ark transportType: got %q want stdio", ark.transportType)
	}
	if ark.command != "/usr/local/bin/ark" {
		t.Errorf("ark command: got %q want /usr/local/bin/ark", ark.command)
	}
	wantArgs := []string{"mcp-server", "--root", "/repo"}
	if len(ark.args) != len(wantArgs) {
		t.Fatalf("ark args: got %v want %v", ark.args, wantArgs)
	}
	for i := range wantArgs {
		if ark.args[i] != wantArgs[i] {
			t.Errorf("ark args[%d]: got %q want %q", i, ark.args[i], wantArgs[i])
		}
	}

	// Non-stdio transport must not be mistaken for an empty-command stdio server.
	remote, ok := servers["remote"]
	if !ok {
		t.Fatal("remote entry not found")
	}
	if remote.transportType == "stdio" {
		t.Errorf("remote should not be classified as stdio, got transportType=%q", remote.transportType)
	}
	if remote.command != "" {
		t.Errorf("remote stdio command should be empty for non-stdio transport, got %q", remote.command)
	}
}

// TestCodex_CurrentFormatEquivalent_NoMutation fixes the key regression: a real
// current-format Ark entry that matches the desired config must classify as
// Equivalent and trigger neither remove nor add.
func TestCodex_CurrentFormatEquivalent_NoMutation(t *testing.T) {
	f := newFakeCodex(true)
	f.setStdio("ark", "/usr/local/bin/ark", []string{"mcp-server", "--root", "/repo"})

	a := codexAdapter{runner: f}
	res, err := a.run(Options{Client: ClientCodex, ArkPath: "/usr/local/bin/ark", RootDir: "/repo"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.State != StateEquivalent || res.Changed {
		t.Fatalf("expected equivalent no-op, got %s changed=%v", res.State, res.Changed)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "mcp add") || strings.Contains(c, "mcp remove") {
			t.Errorf("equivalent must not mutate; called: %s", c)
		}
	}
}
