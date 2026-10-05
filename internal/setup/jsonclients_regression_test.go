package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Byte-for-byte regression guard for the JSON-based adapters. The shared JSON
// store was parameterised by its server-map key (for VS Code's "servers"); the
// serialised output and state semantics of the existing "mcpServers" clients
// must not change. The expected documents below were captured from the
// implementation before that change.

const regressionExistingDoc = `{
  "mcpServers": {
    "other": {
      "command": "x",
      "args": [
        "1"
      ]
    }
  },
  "someFutureField": {
    "keep": true
  }
}
`

type jsonRegressionCase struct {
	client ClientID
	global bool
	path   func(root, home string) string
}

func jsonRegressionCases() []jsonRegressionCase {
	return []jsonRegressionCase{
		{ClientCursor, false, func(root, _ string) string { return filepath.Join(root, ".cursor", "mcp.json") }},
		{ClientCursor, true, func(_, home string) string { return filepath.Join(home, ".cursor", "mcp.json") }},
		{ClientCline, false, func(_, home string) string { return filepath.Join(home, ".cline", "mcp.json") }},
		{ClientClaude, true, func(_, home string) string { return filepath.Join(home, ".claude", "settings.json") }},
	}
}

// expectedAbsent / expectedMerged render the golden documents for a client.
func goldenEntry(client ClientID, ark, root string) string {
	typeLine := ""
	if client == ClientClaude {
		typeLine = ",\n      \"type\": \"stdio\"" // keys are serialised alphabetically
	}
	return `    "ark": {
      "args": [
        "mcp-server",
        "--root",
        "` + root + `"
      ],
      "command": "` + ark + `",
      "env": {}` + typeLine + `
    }`
}

func TestJSONClients_SerializedOutputUnchanged(t *testing.T) {
	for _, c := range jsonRegressionCases() {
		name := string(c.client)
		if c.global {
			name += "/global"
		}
		t.Run(name+"/absent", func(t *testing.T) {
			root, home, ark := t.TempDir(), t.TempDir(), fakeArk(t)
			t.Chdir(t.TempDir()) // the Claude adapter also writes a skill relative to the cwd
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			if _, err := Run(Options{Client: c.client, ArkPath: ark, RootDir: root, Global: c.global}); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(c.path(root, home))
			if err != nil {
				t.Fatal(err)
			}
			want := "{\n  \"mcpServers\": {\n" + goldenEntry(c.client, ark, root) + "\n  }\n}\n"
			if string(got) != want {
				t.Errorf("serialized output changed\n--- got ---\n%s--- want ---\n%s", got, want)
			}
		})
		t.Run(name+"/merge", func(t *testing.T) {
			root, home, ark := t.TempDir(), t.TempDir(), fakeArk(t)
			t.Chdir(t.TempDir()) // the Claude adapter also writes a skill relative to the cwd
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			p := c.path(root, home)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(regressionExistingDoc), 0o644); err != nil {
				t.Fatal(err)
			}
			res, err := Run(Options{Client: c.client, ArkPath: ark, RootDir: root, Global: c.global})
			if err != nil || res.State != StateAbsent || !res.Changed {
				t.Fatalf("state=%v changed=%v err=%v", res, res != nil && res.Changed, err)
			}
			got, _ := os.ReadFile(p)
			want := "{\n  \"mcpServers\": {\n" + goldenEntry(c.client, ark, root) + ",\n" +
				"    \"other\": {\n      \"args\": [\n        \"1\"\n      ],\n      \"command\": \"x\"\n    }\n" +
				"  },\n  \"someFutureField\": {\n    \"keep\": true\n  }\n}\n"
			if string(got) != want {
				t.Errorf("merged output changed\n--- got ---\n%s--- want ---\n%s", got, want)
			}
			// Second run: Equivalent no-op, bytes untouched.
			res, err = Run(Options{Client: c.client, ArkPath: ark, RootDir: root, Global: c.global})
			if err != nil || res.State != StateEquivalent || res.Changed {
				t.Fatalf("second run: state=%v err=%v", res.State, err)
			}
			if again, _ := os.ReadFile(p); string(again) != want {
				t.Error("Equivalent run changed the file")
			}
		})
		t.Run(name+"/malformed", func(t *testing.T) {
			root, home, ark := t.TempDir(), t.TempDir(), fakeArk(t)
			t.Chdir(t.TempDir()) // the Claude adapter also writes a skill relative to the cwd
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			p := c.path(root, home)
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			bad := "{\"mcpServers\": [oops"
			_ = os.WriteFile(p, []byte(bad), 0o644)
			for _, force := range []bool{false, true} {
				_, err := Run(Options{Client: c.client, ArkPath: ark, RootDir: root, Global: c.global, Force: force})
				if err == nil || !strings.Contains(err.Error(), "cannot parse") {
					t.Fatalf("force=%v: want parse error, got %v", force, err)
				}
				if got, _ := os.ReadFile(p); string(got) != bad {
					t.Fatalf("force=%v: malformed file was modified", force)
				}
			}
		})
		t.Run(name+"/wrong-type", func(t *testing.T) {
			root, home, ark := t.TempDir(), t.TempDir(), fakeArk(t)
			t.Chdir(t.TempDir()) // the Claude adapter also writes a skill relative to the cwd
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			p := c.path(root, home)
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			bad := "{\"mcpServers\": []}"
			_ = os.WriteFile(p, []byte(bad), 0o644)
			if _, err := Run(Options{Client: c.client, ArkPath: ark, RootDir: root, Global: c.global, Force: true}); err == nil ||
				!strings.Contains(err.Error(), `unexpected "mcpServers" type`) {
				t.Fatalf("want wrong-type error naming mcpServers, got %v", err)
			}
			if got, _ := os.ReadFile(p); string(got) != bad {
				t.Fatal("file with wrong-typed mcpServers was modified")
			}
		})
	}
}
