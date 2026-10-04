package setup

import (
	"os"
	"path/filepath"
	"testing"
)

// TestVerify_DoesNotClobberConcurrentChange ensures a verify failure caused by a
// concurrent writer never rolls back over that writer's legitimate change
// (task §19).
func TestVerify_DoesNotClobberConcurrentChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	orig := []byte(`{"mcpServers":{}}` + "\n")
	if err := os.WriteFile(path, orig, 0o644); err != nil {
		t.Fatal(err)
	}

	c := &jsonConfig{
		path: path, serverName: "ark", existed: true,
		origBytes: orig, origMode: 0o644,
		doc: map[string]any{}, mcpServers: map[string]any{},
	}

	ourData := []byte(`{"mcpServers":{"ark":{"command":"ark"}}}` + "\n")
	external := []byte(`{"mcpServers":{"other":{"command":"x"}}}` + "\n")
	if err := os.WriteFile(path, external, 0o644); err != nil {
		t.Fatal(err)
	}

	err := c.verify(map[string]any{"command": "ark"}, ourData)
	if err == nil {
		t.Fatal("expected verify to fail on concurrent change")
	}
	cur, _ := os.ReadFile(path)
	if string(cur) != string(external) {
		t.Fatalf("concurrent change was clobbered; file=%q want=%q", cur, external)
	}
}
