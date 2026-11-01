package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// gitWorktrees makes a repository (main) with a committed file and a second
// Git worktree of it (feature) on its own branch, then leaves an uncommitted
// file in each: mainDraft.go in main, feature.go in feature. It returns both
// checkouts' paths, symlinks resolved, as Git and the server report them.
func gitWorktrees(t *testing.T) (main, feature string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main, feature = filepath.Join(base, "main"), filepath.Join(base, "feature")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	git(main, "init", "-q")
	git(main, "symbolic-ref", "HEAD", "refs/heads/main") // as git init -b main, before git 2.28
	write(filepath.Join(main, "go.mod"), "module example.com/wt\n\ngo 1.22\n")
	write(filepath.Join(main, "shared.go"), "package wt\n\nfunc Shared() int { return 1 }\n")
	write(filepath.Join(main, ".gitignore"), ".ark/\n")
	git(main, "add", ".")
	git(main, "commit", "-q", "-m", "init")
	git(main, "worktree", "add", "-q", "-b", "feature", feature)
	write(filepath.Join(main, "mainDraft.go"), "package wt\n\nfunc MainDraft() int { return Shared() }\n")
	write(filepath.Join(feature, "feature.go"), "package wt\n\nfunc FeatureOnly() int { return Shared() + 1 }\n")
	return main, feature
}

func toplevel(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	p, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// rootPath is the root a server reports, as `ark instruction` tells an agent
// to check it: get_repository_map, format json, detail minimal.
func rootPath(t *testing.T, c *rpcClient) string {
	t.Helper()
	text, isErr := c.tool("get_repository_map", map[string]any{"path": ".", "format": "json", "detail": "minimal", "maxPackages": 1})
	if isErr {
		t.Fatalf("get_repository_map: %s", text)
	}
	var m struct{ RootPath string }
	if err := json.Unmarshal([]byte(text), &m); err != nil || m.RootPath == "" {
		t.Fatalf("no RootPath in %s: %v", text, err)
	}
	return m.RootPath
}

// arkliteDir is the directory of file as get_files_arklite shows it, as
// `ark instruction` tells an agent to check the root with one small file at
// its worktree's top.
func arkliteDir(t *testing.T, c *rpcClient, file string) string {
	t.Helper()
	text, isErr := c.tool("get_files_arklite", map[string]any{"paths": []string{file}})
	if isErr {
		t.Fatalf("get_files_arklite %s: %s", file, text)
	}
	for _, l := range strings.Split(text, "\n") {
		if p, ok := strings.CutPrefix(l, "@"); ok {
			return filepath.Dir(p)
		}
	}
	t.Fatalf("no file entry in %s", text)
	return ""
}

// samePath compares two paths as `ark instruction` says to: after resolving
// symlinks, not as strings.
func samePath(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

func hasSymbol(t *testing.T, c *rpcClient, name string) bool {
	t.Helper()
	text, isErr := c.tool("find_symbol", map[string]any{"path": ".", "pattern": "^" + name + "$"})
	if isErr {
		t.Fatalf("find_symbol %s: %s", name, text)
	}
	return strings.Contains(text, `"name": "`+name+`"`)
}

// Two Git worktrees of one repository, each served by its own Ark MCP server
// started on it: each server answers from its own worktree only — committed
// files, uncommitted changes and its extraction cache — reports its own root
// as RootPath (what `ark instruction` tells an agent to compare with `git
// rev-parse --show-toplevel`), refuses the other worktree's paths, and the
// servers start from a command line without any client configuration being
// changed.
func TestMCPWorktrees_SeparateServersStaySeparate(t *testing.T) {
	bin := buildArk(t)
	main, feature := gitWorktrees(t)
	home := t.TempDir()
	cfg := filepath.Join(main, ".mcp.json")
	seeded := []byte(`{"mcpServers":{"other":{"command":"other-server","args":["x"]}}}`)
	if err := os.WriteFile(cfg, seeded, 0o644); err != nil {
		t.Fatal(err)
	}

	// Twice: cold, then reading each worktree's own extraction cache back.
	for round := 1; round <= 2; round++ {
		m := startServer(t, main, home, bin, "mcp-server", "--root", "./")
		f := startServer(t, feature, home, bin, "mcp-server", "--root", "./")
		m.handshake()
		f.handshake()

		// 6. The root each reports is its own worktree, as Git names it —
		// RootPath, and the directory of a top-level file in arklite.
		if got := rootPath(t, m); !samePath(t, got, toplevel(t, main)) {
			t.Errorf("round %d: main server RootPath %q, want %q", round, got, toplevel(t, main))
		}
		if got := rootPath(t, f); !samePath(t, got, toplevel(t, feature)) {
			t.Errorf("round %d: feature server RootPath %q, want %q", round, got, toplevel(t, feature))
		}
		if got := arkliteDir(t, f, "go.mod"); !samePath(t, got, toplevel(t, feature)) {
			t.Errorf("round %d: feature server's arklite of go.mod is in %q, want %q", round, got, toplevel(t, feature))
		}
		// 7. An agent comparing the root with its worktree tells a server on
		// the other worktree apart.
		if samePath(t, rootPath(t, m), toplevel(t, feature)) {
			t.Errorf("round %d: the main server's root reads as the feature worktree", round)
		}

		// 1-4. Each answers for its own files, uncommitted ones included,
		// and never for the other's.
		for _, c := range []struct {
			server *rpcClient
			name   string
			want   map[string]bool
		}{
			{m, "main", map[string]bool{"Shared": true, "MainDraft": true, "FeatureOnly": false}},
			{f, "feature", map[string]bool{"Shared": true, "MainDraft": false, "FeatureOnly": true}},
		} {
			for sym, want := range c.want {
				if got := hasSymbol(t, c.server, sym); got != want {
					t.Errorf("round %d: %s server has %s = %v, want %v", round, c.name, sym, got, want)
				}
			}
		}
		text, _ := m.tool("get_callers", map[string]any{"path": ".", "symbol": "Shared"})
		if strings.Contains(text, "FeatureOnly") || !strings.Contains(text, "MainDraft") {
			t.Errorf("round %d: main server's callers of Shared:\n%s", round, text)
		}
		text, _ = f.tool("get_callers", map[string]any{"path": ".", "symbol": "Shared"})
		if strings.Contains(text, "MainDraft") || !strings.Contains(text, "FeatureOnly") {
			t.Errorf("round %d: feature server's callers of Shared:\n%s", round, text)
		}

		// 7. A path of the other worktree is refused, never answered.
		for _, tool := range []string{"find_symbol", "get_repository_map", "get_file_content"} {
			args := map[string]any{"path": feature, "pattern": "FeatureOnly"}
			if tool == "get_file_content" {
				args = map[string]any{"path": filepath.Join(feature, "feature.go")}
			}
			text, isErr := m.tool(tool, args)
			if !isErr || !strings.Contains(text, "outside the server root") {
				t.Errorf("round %d: main server's %s on the feature worktree: isError=%v %s", round, tool, isErr, text)
			}
		}
	}

	// 5. Each worktree keeps its own cache, under its own root, filled by
	// its own server.
	for _, dir := range []string{main, feature} {
		n := 0
		_ = filepath.WalkDir(filepath.Join(dir, ".ark", "index"), func(_ string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				n++
			}
			return nil
		})
		if n == 0 {
			t.Errorf("%s: no extraction cache entries of its own", dir)
		}
	}

	// 8. Starting the servers changed no client configuration.
	if got, err := os.ReadFile(cfg); err != nil || !bytes.Equal(got, seeded) {
		t.Errorf(".mcp.json changed: %s (%v)", got, err)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("the servers wrote into HOME: %v", entries)
	}

	// The CLI fallback the instruction names answers for its worktree.
	out, _, code := runArk(t, bin, feature, home, "symbol", "feature.go")
	if code != 0 || !strings.Contains(out, "FeatureOnly") {
		t.Errorf("ark symbol in the feature worktree: exit %d\n%s", code, out)
	}
}

// What `ark setup <client>` does in a second worktree, for every client, as
// docs/getting-started.md (Git worktrees) describes it: a project-scoped
// entry is the worktree's own (Claude's portable root serves whichever
// worktree the client starts in; an absolute root copied from another
// checkout is a conflict, not silently kept); a user-scoped entry names one
// root, so a second worktree's setup stops on the conflict and changes
// nothing — and an entry added by hand under another name for the second
// worktree survives setup.
func TestMCPWorktrees_SetupPerClient(t *testing.T) {
	bin := buildArk(t)
	type cfgFile struct{ path, key string }
	clients := []struct {
		id     string
		global bool
		config func(worktree, home string) cfgFile // nil: codex
	}{
		{"claude", false, func(w, _ string) cfgFile { return cfgFile{filepath.Join(w, ".mcp.json"), "mcpServers"} }},
		{"claude", true, func(_, h string) cfgFile { return cfgFile{filepath.Join(h, ".claude", "settings.json"), "mcpServers"} }},
		{"cursor", false, func(w, _ string) cfgFile { return cfgFile{filepath.Join(w, ".cursor", "mcp.json"), "mcpServers"} }},
		{"cursor", true, func(_, h string) cfgFile { return cfgFile{filepath.Join(h, ".cursor", "mcp.json"), "mcpServers"} }},
		{"cline", true, func(_, h string) cfgFile { return cfgFile{filepath.Join(h, ".cline", "mcp.json"), "mcpServers"} }},
		{"copilot-vscode", false, func(w, _ string) cfgFile { return cfgFile{filepath.Join(w, ".vscode", "mcp.json"), "servers"} }},
		{"copilot-cli", false, func(w, _ string) cfgFile { return cfgFile{filepath.Join(w, ".github", "mcp.json"), "mcpServers"} }},
		{"codex", true, nil},
	}
	for _, cl := range clients {
		name := cl.id
		if cl.global && (cl.id == "cursor" || cl.id == "claude") {
			name += "-global"
		}
		t.Run(name, func(t *testing.T) {
			if cl.id == "codex" && runtime.GOOS == "windows" {
				t.Skip("the fake codex CLI is a POSIX shell script")
			}
			main, feature := gitWorktrees(t)
			home := t.TempDir()
			fakeBin := t.TempDir()
			state := filepath.Join(t.TempDir(), "codex.json")
			if err := os.WriteFile(filepath.Join(fakeBin, "codex"), []byte(fakeCodexScript), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("FAKE_CODEX_STATE", state)
			setup := func(dir string, extra ...string) (string, int) {
				args := append([]string{"setup", cl.id, "--ark-path", bin}, extra...)
				if cl.global && (cl.id == "cursor" || cl.id == "claude") {
					args = append(args, "--global")
				}
				out, errOut, code := runArk(t, bin, dir, home, args...)
				return out + errOut, code
			}
			if out, code := setup(main); code != 0 {
				t.Fatalf("setup in main: exit %d\n%s", code, out)
			}

			if cl.config == nil || cl.global {
				// One user-level entry named "ark", rooted at main: setup in
				// the second worktree is a conflict and changes nothing.
				var before []byte
				var file string
				if cl.config == nil {
					file = state
				} else {
					file = cl.config(main, home).path
				}
				before, _ = os.ReadFile(file)
				out, code := setup(feature)
				if code == 0 || !strings.Contains(out, "--force") {
					t.Errorf("setup in a second worktree: exit %d, want a conflict naming --force\n%s", code, out)
				}
				if after, _ := os.ReadFile(file); !bytes.Equal(before, after) {
					t.Errorf("a refused setup changed %s", file)
				}
				if cl.config == nil {
					return
				}
				// A second entry added by hand, under its own name, for the
				// feature worktree: setup in main keeps it.
				var doc map[string]map[string]any
				if err := json.Unmarshal(before, &doc); err != nil {
					t.Fatal(err)
				}
				key := cl.config(main, home).key
				doc[key]["ark-feature"] = map[string]any{"command": bin, "args": []string{"mcp-server", "--root", feature}}
				withSecond, _ := json.Marshal(doc)
				if err := os.WriteFile(file, withSecond, 0o644); err != nil {
					t.Fatal(err)
				}
				if out, code := setup(main); code != 0 || !strings.Contains(out, "No changes required") {
					t.Errorf("setup in main with a second entry: exit %d\n%s", code, out)
				}
				if after, _ := os.ReadFile(file); !bytes.Equal(withSecond, after) {
					t.Errorf("setup changed the hand-added entry:\n%s", after)
				}
				return
			}

			// Project scope: the entry lives in each worktree.
			mainCfg, featureCfg := cl.config(main, home), cl.config(feature, home)
			mainBytes, _ := os.ReadFile(mainCfg.path)
			if cl.id == "claude" {
				// Committed or copied, Claude's portable entry serves
				// whichever worktree the client starts in: no change needed.
				if err := os.WriteFile(featureCfg.path, mainBytes, 0o644); err != nil {
					t.Fatal(err)
				}
				if out, code := setup(feature); code != 0 || !strings.Contains(out, "No changes required") {
					t.Errorf("setup in the feature worktree: exit %d\n%s", code, out)
				}
				c := startServer(t, feature, home, bin, "mcp-server", "--root", expandClaude("${CLAUDE_PROJECT_DIR:-.}/", nil), "--no-cache")
				c.handshake()
				if got := rootPath(t, c); !samePath(t, got, toplevel(t, feature)) {
					t.Errorf("Claude's entry started in the feature worktree serves %q", got)
				}
				return
			}
			// An absolute root copied from main is a conflict in the feature
			// worktree, and setup leaves it as it is.
			if err := os.MkdirAll(filepath.Dir(featureCfg.path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(featureCfg.path, mainBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			if out, code := setup(feature); code == 0 || !strings.Contains(out, "--force") {
				t.Errorf("setup over main's copied entry: exit %d, want a conflict naming --force\n%s", code, out)
			}
			if after, _ := os.ReadFile(featureCfg.path); !bytes.Equal(after, mainBytes) {
				t.Errorf("a refused setup changed %s", featureCfg.path)
			}
			// Without the copy, setup writes the worktree's own entry and
			// leaves main's file alone.
			if err := os.Remove(featureCfg.path); err != nil {
				t.Fatal(err)
			}
			if out, code := setup(feature); code != 0 {
				t.Fatalf("setup in the feature worktree: exit %d\n%s", code, out)
			}
			got, _ := os.ReadFile(featureCfg.path)
			if !strings.Contains(string(got), jsonQuote(feature)) || strings.Contains(string(got), jsonQuote(main)) {
				t.Errorf("the feature worktree's entry does not name it:\n%s", got)
			}
			if after, _ := os.ReadFile(mainCfg.path); !bytes.Equal(after, mainBytes) {
				t.Errorf("setup in the feature worktree changed main's %s", mainCfg.path)
			}
		})
	}
}

// jsonQuote is s as it appears inside a JSON string (Windows paths escape
// their backslashes).
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// A root given through a symlink is reported as it was given: compared with
// `git rev-parse --show-toplevel` as a string it differs, as a path (symlinks
// resolved, as `ark instruction` says) it is the same worktree.
func TestMCPWorktrees_SymlinkedRootComparesAsPath(t *testing.T) {
	bin := buildArk(t)
	_, feature := gitWorktrees(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Dir(feature), link); err != nil {
		if os.Getenv("ARK_REQUIRE_SYMLINKS") != "" {
			t.Fatalf("ARK_REQUIRE_SYMLINKS: cannot create symlink %s: %v", link, err)
		}
		t.Skipf("no symlinks here: %v", err)
	}
	viaLink := filepath.Join(link, filepath.Base(feature))
	c := startServer(t, t.TempDir(), t.TempDir(), bin, "mcp-server", "--root", viaLink, "--no-cache")
	c.handshake()
	for what, got := range map[string]string{"RootPath": rootPath(t, c), "arklite": arkliteDir(t, c, "go.mod")} {
		if got == toplevel(t, feature) {
			t.Errorf("%s %q: expected the symlinked spelling, not Git's", what, got)
		}
		if !samePath(t, got, toplevel(t, feature)) {
			t.Errorf("%s %q is not the feature worktree as a path", what, got)
		}
	}
}

// A worktree inside the root is part of the root's files: the root's server
// sees its declarations (the case `ark instruction` warns of), the check it
// prescribes tells it apart (from the nested worktree, the root's server
// reports another directory), and listing the directory in .arkignore takes
// it out of the root's server.
func TestMCPWorktrees_NestedWorktree(t *testing.T) {
	bin := buildArk(t)
	main, _ := gitWorktrees(t)
	nested := filepath.Join(main, ".worktrees", "nested")
	cmd := exec.Command("git", "worktree", "add", "-q", "-b", "nested", nested)
	cmd.Dir = main
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(nested, "nestedOnly.go"), []byte("package wt\n\nfunc NestedOnly() int { return 3 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()

	m := startServer(t, main, home, bin, "mcp-server", "--root", "./", "--no-cache")
	m.handshake()
	if !hasSymbol(t, m, "NestedOnly") {
		t.Error("the root's server does not see the nested worktree: the guidance's warning would be moot")
	}
	// The check from the nested worktree: the server's file is not its own.
	if samePath(t, arkliteDir(t, m, "shared.go"), toplevel(t, nested)) || samePath(t, rootPath(t, m), toplevel(t, nested)) {
		t.Error("the root's server passes the root check for the nested worktree")
	}

	if err := os.WriteFile(filepath.Join(main, ".arkignore"), []byte(".worktrees/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m2 := startServer(t, main, home, bin, "mcp-server", "--root", "./", "--no-cache")
	m2.handshake()
	if hasSymbol(t, m2, "NestedOnly") {
		t.Error(".arkignore does not take the nested worktree out of the root's server")
	}
	if !hasSymbol(t, m2, "MainDraft") {
		t.Error(".arkignore took the root's own files out")
	}
}
