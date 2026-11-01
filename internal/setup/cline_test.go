package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clineEnv isolates the variables Cline locates its settings with and returns
// the home directory.
func clineEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, v := range []string{"CLINE_MCP_SETTINGS_PATH", "CLINE_DATA_DIR", "CLINE_DIR", "HOMEDRIVE", "HOMEPATH"} {
		t.Setenv(v, "")
	}
	return home
}

// clineDefaultPath is the Cline CLI's MCP settings file under home.
func clineDefaultPath(home string) string {
	return filepath.Join(home, ".cline", "data", "settings", "cline_mcp_settings.json")
}

func TestCline_WritesUserLevelConfig(t *testing.T) {
	home := clineEnv(t)
	root := t.TempDir()
	ark := fakeArk(t)

	res, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Scope != "user" {
		t.Errorf("scope: got %s want user", res.Scope)
	}
	want := clineDefaultPath(home)
	if res.ConfigPath != want {
		t.Errorf("config path: got %s want %s", res.ConfigPath, want)
	}
	servers := mcpServers(t, readJSON(t, want))
	entry := servers["ark"].(map[string]any)
	if entry["command"] != ark {
		t.Errorf("command: got %v want %v", entry["command"], ark)
	}
	args := entry["args"].([]any)
	if args[2] != root {
		t.Errorf("root arg: got %v want %v", args[2], root)
	}
}

func TestCline_PreservesUnrelated_ConflictAndMalformed(t *testing.T) {
	home := clineEnv(t)
	root := t.TempDir()
	ark := fakeArk(t)
	path := clineDefaultPath(home)

	// Existing unrelated server + unknown field.
	writeJSON(t, path, map[string]any{
		"mcpServers": map[string]any{
			"other": map[string]any{"command": "keep", "customField": true},
		},
	})
	if _, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	servers := mcpServers(t, readJSON(t, path))
	if servers["other"].(map[string]any)["customField"] != true {
		t.Error("unknown field on unrelated server not preserved")
	}
	if _, ok := servers["ark"]; !ok {
		t.Error("ark not added")
	}

	// Conflict without --force → error, zero mutation.
	writeJSON(t, path, map[string]any{
		"mcpServers": map[string]any{"ark": map[string]any{"command": "/old", "args": []string{"x"}}},
	})
	before, _ := os.ReadFile(path)
	if _, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root}); err == nil {
		t.Fatal("expected conflict without --force")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("mutated on conflict without --force")
	}

	// Malformed → error even with --force, file untouched.
	bad := []byte("{broken")
	os.WriteFile(path, bad, 0o644)
	if _, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root, Force: true}); err == nil {
		t.Fatal("expected malformed error")
	}
	cur, _ := os.ReadFile(path)
	if string(cur) != string(bad) {
		t.Error("malformed file was modified")
	}
}

func TestCline_GlobalFlagStillUserLevel(t *testing.T) {
	home := clineEnv(t)
	root := t.TempDir()
	ark := fakeArk(t)

	res, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root, Global: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := clineDefaultPath(home)
	if res.ConfigPath != want {
		t.Errorf("config path: got %s want %s", res.ConfigPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("config not written: %v", err)
	}
}

// The settings file is located as the Cline CLI locates it.
func TestCline_SettingsPathResolution(t *testing.T) {
	abs := func(t *testing.T, elem ...string) string {
		return filepath.Join(append([]string{t.TempDir()}, elem...)...)
	}
	cases := []struct {
		name string
		env  func(t *testing.T, home string) (want string)
	}{
		{"default", func(t *testing.T, home string) string { return clineDefaultPath(home) }},
		{"CLINE_DIR", func(t *testing.T, home string) string {
			d := abs(t, "cfg")
			t.Setenv("CLINE_DIR", d)
			return filepath.Join(d, "data", "settings", "cline_mcp_settings.json")
		}},
		{"CLINE_DATA_DIR-over-CLINE_DIR", func(t *testing.T, home string) string {
			t.Setenv("CLINE_DIR", abs(t, "cfg"))
			d := abs(t, "data")
			t.Setenv("CLINE_DATA_DIR", "  "+d+" ") // trimmed, as Cline does
			return filepath.Join(d, "settings", "cline_mcp_settings.json")
		}},
		{"CLINE_MCP_SETTINGS_PATH-over-all", func(t *testing.T, home string) string {
			t.Setenv("CLINE_DIR", abs(t, "cfg"))
			t.Setenv("CLINE_DATA_DIR", abs(t, "data"))
			p := abs(t, "deep", "mcp.json")
			t.Setenv("CLINE_MCP_SETTINGS_PATH", p)
			return p
		}},
		{"blank-values-are-unset", func(t *testing.T, home string) string {
			t.Setenv("CLINE_MCP_SETTINGS_PATH", "  ")
			t.Setenv("CLINE_DATA_DIR", "")
			return clineDefaultPath(home)
		}},
		{"HOME-tilde-falls-back-to-USERPROFILE", func(t *testing.T, home string) string {
			up := t.TempDir()
			t.Setenv("HOME", "~")
			t.Setenv("USERPROFILE", up)
			return clineDefaultPath(up)
		}},
		{"empty-HOME-falls-back-to-USERPROFILE", func(t *testing.T, home string) string {
			up := t.TempDir()
			t.Setenv("HOME", "")
			t.Setenv("USERPROFILE", up)
			return clineDefaultPath(up)
		}},
		{"HOME-wins-over-USERPROFILE", func(t *testing.T, home string) string {
			t.Setenv("USERPROFILE", t.TempDir())
			return clineDefaultPath(home)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := clineEnv(t)
			want := c.env(t, home)
			got, err := clineSettingsPath()
			if err != nil || got != want {
				t.Fatalf("got %q, %v; want %q", got, err, want)
			}
			// setup writes there, creating the missing parent directories.
			res, err := Run(Options{Client: ClientCline, ArkPath: fakeArk(t), RootDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if res.ConfigPath != want {
				t.Errorf("config path %s, want %s", res.ConfigPath, want)
			}
			if _, err := os.Stat(want); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestCline_RelativeVariableRefused(t *testing.T) {
	for _, v := range []string{"CLINE_MCP_SETTINGS_PATH", "CLINE_DATA_DIR", "CLINE_DIR"} {
		t.Run(v, func(t *testing.T) {
			home := clineEnv(t)
			t.Chdir(t.TempDir())
			t.Setenv(v, filepath.Join("rel", "x"))
			_, err := Run(Options{Client: ClientCline, ArkPath: fakeArk(t), RootDir: t.TempDir()})
			if err == nil || !strings.Contains(err.Error(), v) || !strings.Contains(err.Error(), "relative") {
				t.Fatalf("got %v", err)
			}
			if _, err := os.Stat("rel"); !os.IsNotExist(err) {
				t.Error("created a relative directory")
			}
			if _, err := os.Stat(filepath.Join(home, ".cline")); !os.IsNotExist(err) {
				t.Error("fell back to the default path")
			}
		})
	}
}

// Cline accepts a stdio server flat or nested under "transport" (the form
// `cline mcp add` writes); an entry Ark would write in either form is
// Equivalent and the file is left as it is.
func TestCline_NestedAndFlatAreEquivalent(t *testing.T) {
	cases := map[string]func(ark, root string) map[string]any{
		"nested": func(ark, root string) map[string]any {
			return map[string]any{"transport": map[string]any{"type": "stdio", "command": ark, "args": []any{"mcp-server", "--root", root}}}
		},
		"nested-empty-env": func(ark, root string) map[string]any {
			return map[string]any{"transport": map[string]any{"type": "stdio", "command": ark, "args": []any{"mcp-server", "--root", root}, "env": map[string]any{}}}
		},
		"flat-no-env": func(ark, root string) map[string]any {
			return map[string]any{"command": ark, "args": []any{"mcp-server", "--root", root}}
		},
		"nested-disabled-false": func(ark, root string) map[string]any {
			// What `cline mcp add` writes (Cline 3.0.74).
			return map[string]any{"transport": map[string]any{"type": "stdio", "command": ark, "args": []any{"mcp-server", "--root", root}, "env": map[string]any{}}, "disabled": false}
		},
		"flat-typed": func(ark, root string) map[string]any {
			return map[string]any{"type": "stdio", "command": ark, "args": []any{"mcp-server", "--root", root}, "env": map[string]any{}}
		},
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			home := clineEnv(t)
			root, ark := t.TempDir(), fakeArk(t)
			path := clineDefaultPath(home)
			writeJSON(t, path, map[string]any{"mcpServers": map[string]any{"ark": entry(ark, root)}})
			before, _ := os.ReadFile(path)
			res, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root})
			if err != nil {
				t.Fatal(err)
			}
			if res.State != StateEquivalent || res.Changed {
				t.Fatalf("state=%s changed=%v", res.State, res.Changed)
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
				t.Error("file changed")
			}
		})
	}
	// Differences in either form are conflicts, never equivalence.
	conflicts := map[string]func(ark, root string) map[string]any{
		"nested-other-root": func(ark, _ string) map[string]any {
			return map[string]any{"transport": map[string]any{"type": "stdio", "command": ark, "args": []any{"mcp-server", "--root", "/elsewhere"}}}
		},
		"nested-env": func(ark, root string) map[string]any {
			return map[string]any{"transport": map[string]any{"type": "stdio", "command": ark, "args": []any{"mcp-server", "--root", root}, "env": map[string]any{"K": "v"}}}
		},
		"nested-not-stdio": func(ark, root string) map[string]any {
			return map[string]any{"transport": map[string]any{"type": "sse", "command": ark, "args": []any{"mcp-server", "--root", root}}}
		},
		"both-forms": func(ark, root string) map[string]any {
			return map[string]any{"command": ark, "transport": map[string]any{"type": "stdio", "command": ark, "args": []any{"mcp-server", "--root", root}}}
		},
		"flat-other-type": func(ark, root string) map[string]any {
			return map[string]any{"type": "http", "command": ark, "args": []any{"mcp-server", "--root", root}}
		},
		"extra-field": func(ark, root string) map[string]any {
			return map[string]any{"transport": map[string]any{"type": "stdio", "command": ark, "args": []any{"mcp-server", "--root", root}}, "disabled": true}
		},
	}
	for name, entry := range conflicts {
		t.Run("conflict/"+name, func(t *testing.T) {
			home := clineEnv(t)
			root, ark := t.TempDir(), fakeArk(t)
			path := clineDefaultPath(home)
			writeJSON(t, path, map[string]any{"mcpServers": map[string]any{"ark": entry(ark, root)}})
			before, _ := os.ReadFile(path)
			if _, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root}); err == nil || !strings.Contains(err.Error(), "--force") {
				t.Fatalf("want a conflict, got %v", err)
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
				t.Error("file changed")
			}
		})
	}
}

func TestCline_ForceReplacesOnlyArk_Idempotent(t *testing.T) {
	home := clineEnv(t)
	root, ark := t.TempDir(), fakeArk(t)
	path := clineDefaultPath(home)
	writeJSON(t, path, map[string]any{
		"futureTopLevel": map[string]any{"keep": true},
		"mcpServers": map[string]any{
			"ark":   map[string]any{"transport": map[string]any{"type": "stdio", "command": "/old", "args": []any{"x"}}},
			"other": map[string]any{"transport": map[string]any{"type": "stdio", "command": "keep"}, "disabled": true},
		},
	})
	res, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root, Force: true})
	if err != nil || res.State != StateConflict || !res.Changed {
		t.Fatalf("force: %v %v", res, err)
	}
	doc := readJSON(t, path)
	if doc["futureTopLevel"].(map[string]any)["keep"] != true {
		t.Error("unknown top-level field lost")
	}
	servers := mcpServers(t, doc)
	if o := servers["other"].(map[string]any); o["disabled"] != true || o["transport"] == nil {
		t.Errorf("other server changed: %v", o)
	}
	if servers["ark"].(map[string]any)["command"] != ark {
		t.Errorf("ark not replaced: %v", servers["ark"])
	}
	written, _ := os.ReadFile(path)
	res, err = Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root})
	if err != nil || res.State != StateEquivalent || res.Changed {
		t.Fatalf("second run: %v %v", res, err)
	}
	if again, _ := os.ReadFile(path); !bytes.Equal(written, again) {
		t.Error("equivalent run rewrote the file")
	}
}

func TestCline_SymlinkRefused(t *testing.T) {
	home := clineEnv(t)
	root, ark := t.TempDir(), fakeArk(t)
	path := clineDefaultPath(home)
	target := filepath.Join(t.TempDir(), "real.json")
	writeJSON(t, target, map[string]any{"mcpServers": map[string]any{}})
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink: %v", err)
	}
	before, _ := os.ReadFile(target)
	for _, force := range []bool{false, true} {
		if _, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root, Force: force}); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("force=%v: got %v", force, err)
		}
	}
	if after, _ := os.ReadFile(target); !bytes.Equal(before, after) {
		t.Error("symlink target changed")
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink replaced")
	}
}

// ~/.cline/mcp.json, where earlier Ark versions wrote, is reported and never
// changed: Cline never read it.
func TestCline_LegacyFileNotice(t *testing.T) {
	cases := []struct {
		name   string
		body   string // "" means no file
		want   []string
		absent bool // no notice expected
	}{
		{"none", "", nil, true},
		{"empty-servers", `{"mcpServers":{}}`, nil, true},
		{"ark-only", `{"mcpServers":{"ark":{"command":"ark","args":["mcp-server"]}}}`, []string{`"ark" entry`, "never took effect", "delete"}, false},
		{"ark-and-others", `{"mcpServers":{"ark":{"command":"ark"},"github":{"command":"gh","env":{"TOKEN":"s3cret"}}}}`, []string{`"ark" entry`, "other servers (github)", "Move any other servers"}, false},
		{"others-only", `{"mcpServers":{"github":{"command":"gh"}}}`, []string{"other servers (github)"}, false},
		{"malformed", `{oops`, []string{"could not check", "not valid JSON"}, false},
	}
	for _, c := range cases {
		for _, conflict := range []bool{false, true} {
			name := c.name
			if conflict {
				name += "/with-conflict"
			}
			t.Run(name, func(t *testing.T) {
				home := clineEnv(t)
				root, ark := t.TempDir(), fakeArk(t)
				legacy := filepath.Join(home, ".cline", "mcp.json")
				if c.body != "" {
					os.MkdirAll(filepath.Dir(legacy), 0o755)
					os.WriteFile(legacy, []byte(c.body), 0o644)
				}
				if conflict {
					writeJSON(t, clineDefaultPath(home), map[string]any{"mcpServers": map[string]any{"ark": map[string]any{"command": "/x"}}})
				}
				res, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root})
				var text string
				if conflict {
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
				if c.absent {
					if strings.Contains(text, legacy) {
						t.Errorf("unexpected notice: %s", text)
					}
				} else {
					for _, w := range append(c.want, legacy) {
						if !strings.Contains(text, w) {
							t.Errorf("notice lacks %q: %s", w, text)
						}
					}
				}
				if strings.Contains(text, "s3cret") {
					t.Error("notice shows a server's env")
				}
				if c.body != "" {
					if got, _ := os.ReadFile(legacy); string(got) != c.body {
						t.Errorf("legacy file changed: %s", got)
					}
				}
			})
		}
	}
}

func TestCline_LegacySymlinkNotFollowed(t *testing.T) {
	home := clineEnv(t)
	legacy := filepath.Join(home, ".cline", "mcp.json")
	target := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(target, []byte(`{"mcpServers":{"ark":{}}}`), 0o644)
	os.MkdirAll(filepath.Dir(legacy), 0o755)
	if err := os.Symlink(target, legacy); err != nil {
		t.Skipf("symlink: %v", err)
	}
	res, err := Run(Options{Client: ClientCline, ArkPath: fakeArk(t), RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if w := strings.Join(res.Warnings, "\n"); !strings.Contains(w, "could not check") || !strings.Contains(w, "symlink") {
		t.Errorf("got %q", w)
	}
}

// A conflict with an entry in the nested form names its command and args.
func TestCline_ConflictDescribesNestedEntry(t *testing.T) {
	home := clineEnv(t)
	root, ark := t.TempDir(), fakeArk(t)
	writeJSON(t, clineDefaultPath(home), map[string]any{"mcpServers": map[string]any{"ark": map[string]any{
		"transport": map[string]any{"type": "stdio", "command": "/nested/ark", "args": []any{"mcp-server", "--root", "/elsewhere"}}}}})
	_, err := Run(Options{Client: ClientCline, ArkPath: ark, RootDir: root})
	if err == nil || !strings.Contains(err.Error(), "/nested/ark") || !strings.Contains(err.Error(), "/elsewhere") {
		t.Fatalf("got %v", err)
	}
}

// A home directory that is not absolute ("~" falls through to the OS home
// directory, which is $HOME again) is refused rather than resolved against
// Ark's working directory.
func TestCline_RelativeHomeRefused(t *testing.T) {
	for _, h := range []string{"~", "relhome"} {
		t.Run(h, func(t *testing.T) {
			clineEnv(t)
			cwd := t.TempDir()
			t.Chdir(cwd)
			t.Setenv("HOME", h)
			t.Setenv("USERPROFILE", "")
			t.Setenv("HOMEDRIVE", "")
			t.Setenv("HOMEPATH", "")
			_, err := Run(Options{Client: ClientCline, ArkPath: fakeArk(t), RootDir: t.TempDir()})
			// On Windows, os.UserHomeDir reads USERPROFILE, unset here.
			if err == nil || !strings.Contains(err.Error(), "not an absolute path") && !strings.Contains(err.Error(), "cannot determine home directory") {
				t.Fatalf("got %v", err)
			}
			if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
				t.Errorf("wrote under the working directory: %v", entries)
			}
		})
	}
}
