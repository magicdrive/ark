package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeClaude stands in for the `claude` CLI's `mcp add-json` / `mcp remove`
// with --scope user. Like Claude Code it edits the top-level "mcpServers" of
// the global state file (path) and keeps every other key and server. It never
// reads a malformed file: the tests assert that Ark does not start it then.
type fakeClaude struct {
	installed bool
	path      string
	calls     [][]string

	addFailRemaining int  // upcoming add-json calls that fail
	addMangle        int  // upcoming add-json calls that store a different entry (verify fails)
	echoInput        bool // a failing add-json echoes its JSON argument on stderr
	failRemove       bool
}

func newFakeClaude(path string) *fakeClaude { return &fakeClaude{installed: true, path: path} }

func (f *fakeClaude) look(name string) (string, error) {
	if name == "claude" && f.installed {
		return "/usr/local/bin/claude", nil
	}
	return "", fmt.Errorf("not found")
}

func (f *fakeClaude) run(name string, args ...string) ([]byte, []byte, int, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if name != "claude" || len(args) < 2 || args[0] != "mcp" {
		return nil, []byte("unknown command"), 1, fmt.Errorf("unknown")
	}
	doc := map[string]any{}
	if data, err := os.ReadFile(f.path); err == nil {
		if err := json.Unmarshal(data, &doc); err != nil {
			panic("fake claude started on a malformed " + f.path)
		}
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	switch {
	case args[1] == "add-json" && len(args) == 6 && args[2] == "--scope" && args[3] == "user":
		if f.addFailRemaining > 0 {
			f.addFailRemaining--
			if f.echoInput {
				return nil, []byte("Invalid configuration: " + args[5]), 1, fmt.Errorf("add failed")
			}
			return nil, []byte("add boom"), 1, fmt.Errorf("add failed")
		}
		if _, ok := servers[args[4]]; ok {
			return nil, []byte(`MCP server ` + args[4] + ` already exists in user config`), 1, fmt.Errorf("exit 1")
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(args[5]), &entry); err != nil {
			return nil, []byte("invalid JSON"), 1, fmt.Errorf("exit 1")
		}
		if f.addMangle > 0 {
			f.addMangle--
			entry["command"] = "/mangled"
		}
		servers[args[4]] = entry
	case args[1] == "remove" && len(args) == 5 && args[2] == "--scope" && args[3] == "user":
		if f.failRemove {
			return nil, []byte("remove boom"), 1, fmt.Errorf("remove failed")
		}
		if _, ok := servers[args[4]]; !ok {
			return nil, []byte("No user-scoped MCP server found"), 1, fmt.Errorf("exit 1")
		}
		delete(servers, args[4])
	default:
		return nil, []byte("unsupported"), 1, fmt.Errorf("unsupported")
	}
	doc["mcpServers"] = servers
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(f.path, data, 0o600); err != nil {
		return nil, []byte(err.Error()), 1, err
	}
	return nil, nil, 0, nil
}

func (f *fakeClaude) count(sub string) int {
	n := 0
	for _, c := range f.calls {
		if len(c) > 2 && c[2] == sub {
			n++
		}
	}
	return n
}

// claudeUserEnv isolates HOME (and CLAUDE_CONFIG_DIR) and returns the home
// directory, the root to serve and the ark command.
func claudeUserEnv(t *testing.T) (home, root, ark string) {
	t.Helper()
	home, root, ark = t.TempDir(), t.TempDir(), fakeArk(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv(claudeCustomOAuthEnv, "")
	t.Chdir(t.TempDir()) // the Claude adapter also writes a skill relative to the cwd
	return home, root, ark
}

func runClaudeUser(f *fakeClaude, opts Options) (*Result, error) {
	opts.Client, opts.Global = ClientClaude, true
	a := newClaudeAdapter().(claudeAdapter)
	a.runner = f
	return a.run(opts)
}

func wantEntry(ark, root string) map[string]any {
	return map[string]any{"type": "stdio", "command": ark, "args": []any{"mcp-server", "--root", root}, "env": map[string]any{}}
}

func userEntry(t *testing.T, path string) any {
	t.Helper()
	return mcpServers(t, readJSON(t, path))["ark"]
}

func TestClaudeUser_Absent_AddsThroughCLI(t *testing.T) {
	home, root, ark := claudeUserEnv(t)
	path := filepath.Join(home, ".claude.json")
	f := newFakeClaude(path)

	res, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.State != StateAbsent || !res.Changed || res.ConfigPath != path {
		t.Fatalf("got state=%s changed=%v path=%s", res.State, res.Changed, res.ConfigPath)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls: %v", f.calls)
	}
	c := f.calls[0]
	if strings.Join(c[:6], " ") != "claude mcp add-json --scope user ark" {
		t.Fatalf("unexpected argv %q", c)
	}
	var arg map[string]any
	if err := json.Unmarshal([]byte(c[6]), &arg); err != nil {
		t.Fatalf("add-json argument is not JSON: %v", err)
	}
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	if want := `{"args":["mcp-server","--root",` + q(root) + `],"command":` + q(ark) + `,"env":{},"type":"stdio"}`; c[6] != want {
		t.Errorf("add-json argument changed\n got %s\nwant %s", c[6], want)
	}
	// The user scope names the absolute root, never the project placeholder.
	if !reflect.DeepEqual(arg, wantEntry(ark, root)) {
		t.Errorf("add-json entry: got %v want %v", arg, wantEntry(ark, root))
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Errorf("setup created ~/.claude/settings.json: %v", err)
	}
	if !strings.Contains(res.Report(), "Config: "+path) {
		t.Errorf("report does not name %s:\n%s", path, res.Report())
	}
}

// Ark never writes ~/.claude.json: other keys and servers are claude's to
// keep. The fake keeps them; Ark's part is to ask for exactly one server.
func TestClaudeUser_Absent_OnlyAsksForItsEntry(t *testing.T) {
	home, root, ark := claudeUserEnv(t)
	path := filepath.Join(home, ".claude.json")
	writeJSON(t, path, map[string]any{
		"numStartups": 3,
		"projects":    map[string]any{"/x": map[string]any{"allowedTools": []any{}}},
		"mcpServers":  map[string]any{"other": map[string]any{"command": "keep"}},
	})
	f := newFakeClaude(path)
	if _, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root}); err != nil {
		t.Fatal(err)
	}
	if f.count("add-json") != 1 || f.count("remove") != 0 || len(f.calls) != 1 {
		t.Fatalf("calls: %v", f.calls)
	}
	doc := readJSON(t, path)
	if doc["numStartups"] != float64(3) || doc["projects"] == nil {
		t.Errorf("other keys lost: %v", doc)
	}
	if mcpServers(t, doc)["other"] == nil {
		t.Error("other server lost")
	}
}

func TestClaudeUser_Equivalent_NoCLICall(t *testing.T) {
	for name, entry := range map[string]map[string]any{
		"as-written":     {"type": "stdio", "command": "ARK", "args": []any{"mcp-server", "--root", "ROOT"}, "env": map[string]any{}},
		"no-type-no-env": {"command": "ARK", "args": []any{"mcp-server", "--root", "ROOT"}},
		"null-env":       {"type": "stdio", "command": "ARK", "args": []any{"mcp-server", "--root", "ROOT"}, "env": nil},
	} {
		t.Run(name, func(t *testing.T) {
			home, root, ark := claudeUserEnv(t)
			path := filepath.Join(home, ".claude.json")
			e := map[string]any{}
			for k, v := range entry {
				e[k] = v
			}
			e["command"] = ark
			e["args"] = []any{"mcp-server", "--root", root}
			writeJSON(t, path, map[string]any{"mcpServers": map[string]any{"ark": e}})
			before, _ := os.ReadFile(path)
			f := newFakeClaude(path)
			f.installed = false // nothing to do, so claude is not needed
			res, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root})
			if err != nil {
				t.Fatal(err)
			}
			if res.State != StateEquivalent || res.Changed || len(f.calls) != 0 {
				t.Fatalf("state=%s changed=%v calls=%v", res.State, res.Changed, f.calls)
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
				t.Error("file changed")
			}
		})
	}
}

func TestClaudeUser_Conflict(t *testing.T) {
	home, root, ark := claudeUserEnv(t)
	path := filepath.Join(home, ".claude.json")
	old := map[string]any{"type": "stdio", "command": "/old/ark", "args": []any{"mcp-server", "--root", "/old"}, "env": map[string]any{"TOKEN": "s3cret"}}
	writeJSON(t, path, map[string]any{"mcpServers": map[string]any{"ark": old, "other": map[string]any{"command": "keep"}}})
	before, _ := os.ReadFile(path)

	f := newFakeClaude(path)
	_, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("want a conflict naming --force, got %v", err)
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Error("conflict message shows the existing entry's env")
	}
	if len(f.calls) != 0 {
		t.Errorf("conflict without --force ran claude: %v", f.calls)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Error("file changed")
	}

	res, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root, Force: true})
	if err != nil {
		t.Fatalf("--force: %v", err)
	}
	if res.State != StateConflict || !res.Changed {
		t.Fatalf("state=%s changed=%v", res.State, res.Changed)
	}
	if got := fmt.Sprint(f.calls); !strings.Contains(got, "[claude mcp remove --scope user ark] [claude mcp add-json --scope user ark") {
		t.Errorf("want remove then add-json, got %v", got)
	}
	if !reflect.DeepEqual(userEntry(t, path), wantEntry(ark, root)) {
		t.Errorf("entry not replaced: %v", userEntry(t, path))
	}
	if mcpServers(t, readJSON(t, path))["other"] == nil {
		t.Error("other server lost")
	}
}

func TestClaudeUser_Force_RestoresOnFailure(t *testing.T) {
	for _, c := range []struct {
		name   string
		inject func(*fakeClaude)
	}{
		{"add-fails", func(f *fakeClaude) { f.addFailRemaining = 1 }},
		{"verify-fails", func(f *fakeClaude) { f.addMangle = 1 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			home, root, ark := claudeUserEnv(t)
			path := filepath.Join(home, ".claude.json")
			old := map[string]any{"type": "stdio", "command": "/old/ark", "args": []any{"mcp-server"}, "env": map[string]any{"K": "v"}, "extra": true}
			writeJSON(t, path, map[string]any{"mcpServers": map[string]any{"ark": old}})
			f := newFakeClaude(path)
			c.inject(f)
			_, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root, Force: true})
			if err == nil || !strings.Contains(err.Error(), "previous Ark configuration was restored") {
				t.Fatalf("want a restored failure, got %v", err)
			}
			if got := userEntry(t, path); !reflect.DeepEqual(got, old) {
				t.Errorf("previous entry not restored exactly: %v", got)
			}
		})
	}
}

func TestClaudeUser_Force_RemoveFails_NothingAdded(t *testing.T) {
	home, root, ark := claudeUserEnv(t)
	path := filepath.Join(home, ".claude.json")
	writeJSON(t, path, map[string]any{"mcpServers": map[string]any{"ark": map[string]any{"command": "/old"}}})
	f := newFakeClaude(path)
	f.failRemove = true
	_, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root, Force: true})
	if err == nil || !strings.Contains(err.Error(), "left unchanged") {
		t.Fatalf("got %v", err)
	}
	if f.count("add-json") != 0 {
		t.Errorf("added after a failed remove: %v", f.calls)
	}
}

func TestClaudeUser_Force_NonObjectEntryIsNotRemoved(t *testing.T) {
	home, root, ark := claudeUserEnv(t)
	path := filepath.Join(home, ".claude.json")
	writeJSON(t, path, map[string]any{"mcpServers": map[string]any{"ark": "not an object"}})
	f := newFakeClaude(path)
	if _, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root, Force: true}); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("got %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("ran claude: %v", f.calls)
	}
}

func TestClaudeUser_Absent_VerifyFailureRemovesTheEntry(t *testing.T) {
	home, root, ark := claudeUserEnv(t)
	path := filepath.Join(home, ".claude.json")
	f := newFakeClaude(path)
	f.addMangle = 1
	if _, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root}); err == nil || !strings.Contains(err.Error(), "verify failed") {
		t.Fatalf("got %v", err)
	}
	if _, ok := mcpServers(t, readJSON(t, path))["ark"]; ok {
		t.Error("a half-registered entry was left behind")
	}
}

func TestClaudeUser_NoCLI(t *testing.T) {
	home, root, ark := claudeUserEnv(t)
	path := filepath.Join(home, ".claude.json")
	f := newFakeClaude(path)
	f.installed = false

	_, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root})
	if err == nil || !strings.Contains(err.Error(), "`claude`) was not found on PATH") ||
		!strings.Contains(err.Error(), "claude mcp add-json --scope user ark '{") {
		t.Fatalf("want guidance with the manual command, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Ark created %s: %v", path, err)
	}

	writeJSON(t, path, map[string]any{"mcpServers": map[string]any{"ark": map[string]any{"command": "/old"}}})
	before, _ := os.ReadFile(path)
	_, err = runClaudeUser(f, Options{ArkPath: ark, RootDir: root, Force: true})
	if err == nil || !strings.Contains(err.Error(), "claude mcp remove --scope user ark") {
		t.Fatalf("want the manual remove and add commands, got %v", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Error("file changed")
	}
	if len(f.calls) != 0 {
		t.Errorf("calls: %v", f.calls)
	}
}

// A ~/.claude.json Ark cannot read safely is never handed to claude, which
// would reset a malformed one and drop the user's servers.
func TestClaudeUser_UnsafeConfigDoesNotStartClaude(t *testing.T) {
	cases := map[string]func(t *testing.T, path string){
		"malformed": func(t *testing.T, path string) { os.WriteFile(path, []byte(`{"mcpServers": {`), 0o644) },
		"wrong-type": func(t *testing.T, path string) {
			os.WriteFile(path, []byte(`{"mcpServers": []}`), 0o644)
		},
		"directory": func(t *testing.T, path string) { os.Mkdir(path, 0o755) },
		"too-large": func(t *testing.T, path string) {
			fh, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			fh.Truncate(maxConfigSize + 1)
			fh.Close()
		},
		"symlink": func(t *testing.T, path string) {
			target := filepath.Join(t.TempDir(), "real.json")
			os.WriteFile(target, []byte(`{"mcpServers":{}}`), 0o644)
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("symlink: %v", err)
			}
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			home, root, ark := claudeUserEnv(t)
			path := filepath.Join(home, ".claude.json")
			prepare(t, path)
			for _, force := range []bool{false, true} {
				f := newFakeClaude(path)
				_, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root, Force: force})
				if err == nil || !strings.Contains(err.Error(), "Ark did not run `claude`") {
					t.Fatalf("force=%v: got %v", force, err)
				}
				if len(f.calls) != 0 {
					t.Fatalf("force=%v: ran claude: %v", force, f.calls)
				}
			}
		})
	}
}

func TestClaudeUser_ClaudeConfigDir(t *testing.T) {
	home, root, ark := claudeUserEnv(t)
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, ".claude.json")
	f := newFakeClaude(path)
	res, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if res.ConfigPath != path {
		t.Errorf("config path %s, want %s", res.ConfigPath, path)
	}
	if !reflect.DeepEqual(userEntry(t, path), wantEntry(ark, root)) {
		t.Errorf("entry: %v", userEntry(t, path))
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Errorf("~/.claude.json touched despite CLAUDE_CONFIG_DIR: %v", err)
	}
	// Equivalent is judged from the same file.
	f.calls = nil
	if res, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root}); err != nil || res.State != StateEquivalent || len(f.calls) != 0 {
		t.Fatalf("second run: %v %v %v", res, err, f.calls)
	}
}

// An Ark entry in ~/.claude/settings.json (where earlier versions wrote) is
// reported and never changed, whatever the state of ~/.claude.json.
func TestClaudeUser_LegacySettingsNotice(t *testing.T) {
	type legacy struct {
		name    string
		write   func(t *testing.T, path, ark, root string)
		want    string // "" means no notice
		wantNot string
	}
	cases := []legacy{
		{"none", func(*testing.T, string, string, string) {}, "", ""},
		{"only-other-servers", func(t *testing.T, p, _, _ string) {
			writeJSON(t, p, map[string]any{"mcpServers": map[string]any{"other": map[string]any{"command": "x"}}})
		}, "", ""},
		{"same", func(t *testing.T, p, ark, root string) {
			writeJSON(t, p, map[string]any{"theme": "dark", "mcpServers": map[string]any{"ark": wantEntry(ark, root)}})
		}, "it is the same as the requested entry", "differs"},
		{"different", func(t *testing.T, p, _, _ string) {
			writeJSON(t, p, map[string]any{"mcpServers": map[string]any{
				"ark":   map[string]any{"command": "/old/ark", "args": []any{"mcp-server"}, "env": map[string]any{"TOKEN": "s3cret"}},
				"other": map[string]any{"command": "x"},
			}})
		}, "it differs from the requested entry", "s3cret"},
		{"malformed", func(t *testing.T, p, _, _ string) {
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte("{oops"), 0o644)
		}, "could not check", ""},
		{"symlink", func(t *testing.T, p, ark, root string) {
			target := filepath.Join(t.TempDir(), "settings.json")
			writeJSON(t, target, map[string]any{"mcpServers": map[string]any{"ark": wantEntry(ark, root)}})
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.Symlink(target, p); err != nil {
				t.Skipf("symlink: %v", err)
			}
		}, "could not check", ""},
	}
	for _, c := range cases {
		for _, target := range []string{"absent", "equivalent", "conflict"} {
			t.Run(c.name+"/"+target, func(t *testing.T) {
				home, root, ark := claudeUserEnv(t)
				legacyPath := filepath.Join(home, ".claude", "settings.json")
				c.write(t, legacyPath, ark, root)
				before, _ := os.ReadFile(legacyPath)
				path := filepath.Join(home, ".claude.json")
				switch target {
				case "equivalent":
					writeJSON(t, path, map[string]any{"mcpServers": map[string]any{"ark": wantEntry(ark, root)}})
				case "conflict":
					writeJSON(t, path, map[string]any{"mcpServers": map[string]any{"ark": map[string]any{"command": "/x"}}})
				}
				res, err := runClaudeUser(newFakeClaude(path), Options{ArkPath: ark, RootDir: root})
				var text string
				if target == "conflict" {
					if err == nil {
						t.Fatal("want a conflict")
					}
					text = err.Error()
				} else {
					if err != nil {
						t.Fatal(err)
					}
					text = strings.Join(res.Warnings, "\n")
				}
				switch {
				case c.want == "" && strings.Contains(text, "settings.json"):
					t.Errorf("unexpected notice: %s", text)
				case c.want != "" && (!strings.Contains(text, c.want) || !strings.Contains(text, legacyPath)):
					t.Errorf("want a notice with %q naming %s, got %s", c.want, legacyPath, text)
				case c.wantNot != "" && strings.Contains(text, c.wantNot):
					t.Errorf("notice contains %q: %s", c.wantNot, text)
				}
				if c.name == "same" || c.name == "different" {
					if !strings.Contains(text, "can delete") {
						t.Errorf("notice does not say the entry can be deleted: %s", text)
					}
				}
				if c.name == "different" && !strings.Contains(text, "other") {
					t.Errorf("notice does not mention the other server: %s", text)
				}
				if c.name != "symlink" {
					if after, _ := os.ReadFile(legacyPath); !bytes.Equal(before, after) {
						t.Errorf("legacy settings changed:\n%s", after)
					}
				} else if fi, err := os.Lstat(legacyPath); err != nil || fi.Mode()&os.ModeSymlink == 0 {
					t.Error("legacy symlink replaced")
				}
			})
		}
	}
}

// Through Run (the real claude CLI lookup), --global with no claude on PATH
// fails with guidance and writes neither the old nor the new file.
func TestClaude_Global_WithoutClaudeCLI_WritesNothing(t *testing.T) {
	home, root, ark := claudeUserEnv(t)
	t.Setenv("PATH", t.TempDir())
	_, err := Run(Options{Client: ClientClaude, ArkPath: ark, RootDir: root, Global: true})
	if err == nil || !strings.Contains(err.Error(), "claude mcp add-json --scope user ark") {
		t.Fatalf("got %v", err)
	}
	for _, p := range []string{filepath.Join(home, ".claude.json"), filepath.Join(home, ".claude", "settings.json")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s was written: %v", p, err)
		}
	}
}

// Claude Code reads <config home>/.config.json instead of .claude.json when
// that is a non-empty file; Ark classifies and verifies against the same file.
func TestClaudeUser_LegacyConfigJSONFollowed(t *testing.T) {
	for _, viaEnv := range []bool{false, true} {
		t.Run(fmt.Sprintf("CLAUDE_CONFIG_DIR=%v", viaEnv), func(t *testing.T) {
			home, root, ark := claudeUserEnv(t)
			configHome, base := filepath.Join(home, ".claude"), home
			if viaEnv {
				configHome = t.TempDir()
				base = configHome
				t.Setenv("CLAUDE_CONFIG_DIR", configHome)
			}
			legacy := filepath.Join(configHome, ".config.json")
			writeJSON(t, legacy, map[string]any{"mcpServers": map[string]any{"other": map[string]any{"command": "x"}}})
			// A stale file claude does not read holds the very entry.
			stale := filepath.Join(base, ".claude.json")
			writeJSON(t, stale, map[string]any{"mcpServers": map[string]any{"ark": wantEntry(ark, root)}})
			staleBefore, _ := os.ReadFile(stale)

			f := newFakeClaude(legacy)
			res, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root})
			if err != nil {
				t.Fatal(err)
			}
			if res.State != StateAbsent || res.ConfigPath != legacy || f.count("add-json") != 1 {
				t.Fatalf("state=%s path=%s calls=%v", res.State, res.ConfigPath, f.calls)
			}
			if got := userEntry(t, legacy); !reflect.DeepEqual(got, wantEntry(ark, root)) {
				t.Errorf("entry: %v", got)
			}
			if after, _ := os.ReadFile(stale); !bytes.Equal(staleBefore, after) {
				t.Error(".claude.json changed")
			}
		})
	}
}

// An empty .config.json is passed over, as claude does; one that is not a
// regular file is refused, not passed over.
func TestClaudeUser_LegacyConfigJSONEdgeCases(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		home, root, ark := claudeUserEnv(t)
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".claude", ".config.json"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(home, ".claude.json")
		res, err := runClaudeUser(newFakeClaude(path), Options{ArkPath: ark, RootDir: root})
		if err != nil || res.ConfigPath != path {
			t.Fatalf("res=%v err=%v", res, err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		home, root, ark := claudeUserEnv(t)
		target := filepath.Join(t.TempDir(), "real.json")
		writeJSON(t, target, map[string]any{})
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(home, ".claude", ".config.json")); err != nil {
			t.Skipf("symlink: %v", err)
		}
		f := newFakeClaude(target)
		if _, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root}); err == nil || !strings.Contains(err.Error(), "Ark did not run `claude`") {
			t.Fatalf("got %v", err)
		}
		if len(f.calls) != 0 {
			t.Errorf("claude started: %v", f.calls)
		}
	})
}

// With CLAUDE_CODE_CUSTOM_OAUTH_URL set, Claude Code keeps its state in
// another file; Ark refuses, printing the command, and does not start claude.
func TestClaudeUser_CustomOAuthRefused(t *testing.T) {
	home, root, ark := claudeUserEnv(t)
	t.Setenv(claudeCustomOAuthEnv, "https://example.invalid/oauth")
	f := newFakeClaude(filepath.Join(home, ".claude.json"))
	_, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root})
	if err == nil || !strings.Contains(err.Error(), claudeCustomOAuthEnv) || !strings.Contains(err.Error(), "claude mcp add-json --scope user ark") {
		t.Fatalf("got %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("claude started: %v", f.calls)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Error("~/.claude.json created")
	}
}

// claude reports an empty or blank ~/.claude.json corrupted and resets it, so
// Ark does not start claude on one.
func TestClaudeUser_EmptyConfigRefused(t *testing.T) {
	for name, content := range map[string]string{"empty": "", "blank": "  \n"} {
		t.Run(name, func(t *testing.T) {
			home, root, ark := claudeUserEnv(t)
			path := filepath.Join(home, ".claude.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			f := newFakeClaude(path)
			if _, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root}); err == nil || !strings.Contains(err.Error(), "empty") {
				t.Fatalf("got %v", err)
			}
			if len(f.calls) != 0 {
				t.Errorf("claude started: %v", f.calls)
			}
			if got, _ := os.ReadFile(path); string(got) != content {
				t.Error("file changed")
			}
		})
	}
}

// When restoring the previous entry fails too, claude's stderr of that call,
// which may echo the entry and its env, is not in the error.
func TestClaudeUser_RestoreFailure_NoSecretInError(t *testing.T) {
	home, root, ark := claudeUserEnv(t)
	path := filepath.Join(home, ".claude.json")
	old := map[string]any{"type": "stdio", "command": "/old/ark", "args": []any{"mcp-server"}, "env": map[string]any{"TOKEN": "SECRET-VALUE-42"}}
	writeJSON(t, path, map[string]any{"mcpServers": map[string]any{"ark": old}})
	f := newFakeClaude(path)
	f.addFailRemaining, f.echoInput = 2, true
	_, err := runClaudeUser(f, Options{ArkPath: ark, RootDir: root, Force: true})
	if err == nil || !strings.Contains(err.Error(), "restoration of the previous Ark configuration") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "SECRET-VALUE-42") {
		t.Errorf("secret in error: %v", err)
	}
}
