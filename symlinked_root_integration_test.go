package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A repository given through a symlink — to the MCP server (--root) or to
// the dump — behaves as the directory itself: the same files are listed,
// searched, indexed and dumped, the same .arkignore rules apply, and symlinks
// below the root keep their own policy.

// rootSpellings returns the four ways of naming the fixture's root, each with
// the working directory it is named from: the directory and a symlink to it,
// absolutely and relatively.
func rootSpellings(t *testing.T, proj string) (spellings []struct{ name, cwd, root string }) {
	t.Helper()
	ws := t.TempDir()
	link := filepath.Join(ws, "repo-link")
	if err := os.Symlink(proj, link); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(ws, proj)
	if err != nil {
		t.Fatal(err)
	}
	return []struct{ name, cwd, root string }{
		{"directory, absolute", ws, proj},
		{"directory, relative", ws, rel},
		{"symlink, absolute", ws, link},
		{"symlink, relative", ws, "repo-link"},
	}
}

// normalizeRoot replaces every spelling of the root in a response by ROOT —
// longest first, so /private/var/x is not left as /privateROOT — so responses
// compare by meaning: Ark reports paths under the root as it was given.
func normalizeRoot(s string, roots ...string) string {
	sort.Slice(roots, func(i, j int) bool { return len(roots[i]) > len(roots[j]) })
	for _, r := range roots {
		if r != "" {
			s = strings.ReplaceAll(s, r, "ROOT")
		}
	}
	return s
}

// spellingsOf returns what the root may appear as in a response: the given
// spelling made absolute, the resolved directory and the base names.
func spellingsOf(t *testing.T, cwd, root, proj string) []string {
	abs := root
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, root)
	}
	out := []string{abs, realPath(t, proj), proj, filepath.Base(abs), filepath.Base(proj)}
	if !filepath.IsAbs(root) {
		// The process may see its working directory resolved (macOS
		// /var → /private/var).
		out = append(out, filepath.Join(realPath(t, cwd), root))
		out = append(out, root) // echoed as given
	}
	return out
}

func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSymlinkedRoot_MCPAnswersAsTheDirectory(t *testing.T) {
	bin := buildArk(t)
	for _, flags := range [][]string{nil, {"--mask-secrets", "off"}} {
		proj := arkignoreFixture(t, true, true)
		answers := map[string][]string{}
		for _, sp := range rootSpellings(t, proj) {
			c := startServer(t, sp.cwd, t.TempDir(), bin, append([]string{"mcp-server", "--root", sp.root, "--no-cache"}, flags...)...)
			c.handshake()
			var got []string
			tools := map[string]bool{}
			for _, call := range arkignoreCalls {
				line := rawCall(t, c, call.method, call.params)
				found := excludedIn(line)
				if call.direct {
					found = markersIn(line, contentMarkers)
				}
				if len(found) > 0 {
					t.Errorf("%s %v: %s %v reveals %q", flags, sp.name, call.method, call.params, found)
				}
				got = append(got, normalizeRoot(line, spellingsOf(t, sp.cwd, sp.root, proj)...))
				if name, ok := call.params["name"].(string); ok {
					tools[name] = true
				}
			}
			if len(tools) != 21 {
				t.Errorf("covered %d tools, want 21", len(tools))
			}
			for _, call := range []map[string]any{
				{"name": "list_files", "arguments": map[string]any{"path": "."}},
				{"name": "search_in_files", "arguments": map[string]any{"path": ".", "query": "VISIBLE_BODY_MARKER"}},
				{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "Visible"}},
			} {
				if line := rawCall(t, c, "tools/call", call); !strings.Contains(line, "visible.go") {
					t.Errorf("%s: %v misses the visible file:\n%s", sp.name, call, line)
				}
			}
			stopServer(c)
			answers[sp.name] = got
		}
		// A symlink answers as the directory named the same way (absolute
		// or relative: a relative root may be echoed as given).
		for _, pair := range [][2]string{{"symlink, absolute", "directory, absolute"}, {"symlink, relative", "directory, relative"}} {
			got, want := answers[pair[0]], answers[pair[1]]
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("%v %s differs from %s for %v:\n got %s\nwant %s", flags, pair[0], pair[1], arkignoreCalls[i].params, got[i], want[i])
				}
			}
		}
	}
}

func TestSymlinkedRoot_DumpEqualsTheDirectory(t *testing.T) {
	bin := buildArk(t)
	proj := cliIgnoreFixture(t)
	if err := os.Symlink("keep.txt", filepath.Join(proj, "filelink.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub", filepath.Join(proj, "dirlink")); err != nil {
		t.Fatal(err)
	}
	for _, format := range formatFlags {
		for _, gitignore := range []string{"on", "off"} {
			flags := append(append([]string{"-S"}, format...), "-a", gitignore)
			dumps := map[string]string{}
			for _, sp := range rootSpellings(t, proj) {
				out := filepath.Join(t.TempDir(), "dump")
				cmd := exec.Command(bin, append(append(append([]string{}, flags...), "-o", out), sp.root)...)
				cmd.Dir = sp.cwd
				if b, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%v %s: %v\n%s", flags, sp.name, err, b)
				}
				matches, _ := filepath.Glob(out + "*")
				if len(matches) != 1 {
					t.Fatalf("%v %s: outputs %v", flags, sp.name, matches)
				}
				b, _ := os.ReadFile(matches[0])
				got := normalizeRoot(string(b), spellingsOf(t, sp.cwd, sp.root, proj)...)
				if !strings.Contains(got, "plain.txt") {
					t.Errorf("%v %s: dump misses sub/plain.txt", flags, sp.name)
				}
				dumps[sp.name] = got
			}
			for _, pair := range [][2]string{{"symlink, absolute", "directory, absolute"}, {"symlink, relative", "directory, relative"}} {
				if dumps[pair[0]] != dumps[pair[1]] {
					t.Errorf("%v %s: dump differs from %s:\n%s\n---\n%s", flags, pair[0], pair[1], dumps[pair[0]], dumps[pair[1]])
				}
			}
		}
	}
}

// Rule changes on a running server, a nested rule and a negation apply to the
// next request through a symlinked root as through the directory.
func TestSymlinkedRoot_RuleChangesApply(t *testing.T) {
	bin := buildArk(t)
	proj := arkignoreFixture(t, false, false)
	sp := rootSpellings(t, proj)[2]
	c := startServer(t, sp.cwd, t.TempDir(), bin, "mcp-server", "--root", sp.root, "--no-cache")
	c.handshake()
	defer stopServer(c)
	listed := func() string {
		return rawCall(t, c, "tools/call", map[string]any{"name": "list_files", "arguments": map[string]any{"path": "."}})
	}
	indexed := func(symbol string) bool {
		line := rawCall(t, c, "tools/call", map[string]any{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": symbol}})
		return strings.Contains(line, "BODY_MARKER")
	}
	if !strings.Contains(listed(), "ignored.go") || !indexed("IgnoredHelper") {
		t.Fatal("file not available before any rule")
	}
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(proj, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".arkignore", "src/ignored.go\n")
	if strings.Contains(listed(), "ignored.go") || indexed("IgnoredHelper") {
		t.Error("root rule not applied through the symlinked root")
	}
	write("config/.arkignore", "*.yaml\n!public.yaml\n")
	if l := listed(); strings.Contains(l, "private.yaml") || !strings.Contains(l, "public.yaml") {
		t.Errorf("nested rule with negation not applied:\n%s", l)
	}
	if err := os.Remove(filepath.Join(proj, ".arkignore")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed(), "ignored.go") || !indexed("IgnoredHelper") {
		t.Error("removed rule still applied")
	}
}

// Symlinks below a symlinked root keep their policy: outside targets refused
// by default and readable with --allow-external-symlinks on; .arkignore still
// applies; paths outside the root are refused either way.
func TestSymlinkedRoot_ExternalSymlinksKeepTheirPolicy(t *testing.T) {
	bin := buildArk(t)
	for _, allow := range []bool{false, true} {
		proj, ext := externalFixture(t)
		sp := rootSpellings(t, proj)[2]
		args := []string{"mcp-server", "--root", sp.root, "--no-cache"}
		if allow {
			args = append(args, "--allow-external-symlinks", "on")
		}
		c := startServer(t, sp.cwd, t.TempDir(), bin, args...)
		c.handshake()
		responses := externalSession(t, c, ext)
		var all strings.Builder
		for key, line := range responses {
			all.WriteString(line)
			if m := markersIn(line, neverMarkers); len(m) > 0 {
				t.Errorf("allow %v: %s reveals %q", allow, key, m)
			}
		}
		if got := strings.Contains(all.String(), "EXTERNAL_BODY_MARKER"); got != allow {
			t.Errorf("allow %v: external content returned = %v", allow, got)
		}
		if !strings.Contains(all.String(), "VISIBLE_BODY_MARKER") && !strings.Contains(all.String(), "visible.go") {
			t.Errorf("allow %v: repository content missing", allow)
		}
		stopServer(c)
	}
}

// Chains of links, a link retargeted while the server runs, and links that
// lead nowhere.
func TestSymlinkedRoot_Variants(t *testing.T) {
	bin := buildArk(t)
	a := arkignoreFixture(t, true, false)
	b := t.TempDir()
	if err := os.WriteFile(filepath.Join(b, "other.go"), []byte("package b\n\nfunc Other() string { return \"OTHER_MARKER\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, ".arkignore"), []byte("hidden.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "hidden.go"), []byte("package b\n\n// HIDDEN_B_MARKER\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	link1, link2 := filepath.Join(ws, "link1"), filepath.Join(ws, "link2")
	if err := os.Symlink(a, link1); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(link1, link2); err != nil {
		t.Fatal(err)
	}
	search := func(c *rpcClient) string {
		return rawCall(t, c, "tools/call", map[string]any{"name": "search_in_files", "arguments": map[string]any{"path": ".", "query": "MARKER"}})
	}

	// Two links deep.
	c := startServer(t, ws, t.TempDir(), bin, "mcp-server", "--root", link2, "--no-cache")
	c.handshake()
	if s := search(c); !strings.Contains(s, "VISIBLE_BODY_MARKER") || len(markersIn(s, contentMarkers)) > 0 {
		t.Errorf("two-link root:\n%s", s)
	}
	// Index the first target, so a stale index or root would be served after
	// the retarget.
	if ctx := rawCall(t, c, "tools/call", map[string]any{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "VisibleHelper"}}); !strings.Contains(ctx, "VISIBLE_BODY_MARKER") {
		t.Errorf("two-link root: index:\n%s", ctx)
	}
	// Retargeted while running: the next request serves the new directory
	// with its own rules.
	if err := os.Remove(link1); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(b, link1); err != nil {
		t.Fatal(err)
	}
	if s := search(c); !strings.Contains(s, "OTHER_MARKER") || strings.Contains(s, "VISIBLE_BODY_MARKER") || strings.Contains(s, "HIDDEN_B_MARKER") {
		t.Errorf("retargeted root:\n%s", s)
	}
	ctx := rawCall(t, c, "tools/call", map[string]any{"name": "get_context", "arguments": map[string]any{"path": ".", "symbol": "Other"}})
	if !strings.Contains(ctx, "OTHER_MARKER") {
		t.Errorf("retargeted root: index not rebuilt:\n%s", ctx)
	}
	stopServer(c)

	// Dangling and looping roots are errors, never an empty repository.
	dangling, loopA, loopB := filepath.Join(ws, "dangling"), filepath.Join(ws, "loopA"), filepath.Join(ws, "loopB")
	for _, l := range [][2]string{{filepath.Join(ws, "missing"), dangling}, {loopB, loopA}, {loopA, loopB}} {
		if err := os.Symlink(l[0], l[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, root := range []string{dangling, loopA} {
		cmd := exec.Command(bin, "mcp-server", "--root", root, "--no-cache")
		cmd.Stdin = strings.NewReader("")
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "root") {
			t.Errorf("mcp-server --root %s: err %v, output %q", filepath.Base(root), err, out)
		}
		dump := exec.Command(bin, "-S", "-o", filepath.Join(t.TempDir(), "out"), root)
		out, err = dump.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "not found") {
			t.Errorf("dump %s: err %v, output %q", filepath.Base(root), err, out)
		}
	}
}

// A directory symlink below the root, named as a tool's path, is not entered:
// below it, paths would not be the paths the rules name (sub/secret.go must
// not appear as dirlink/secret.go).
func TestSymlinkedRoot_DirectoryLinkBelowTheRootIsNotEntered(t *testing.T) {
	bin := buildArk(t)
	proj := t.TempDir()
	for p, body := range map[string]string{".arkignore": "sub/secret.go\n", "sub/secret.go": "// DIRLINK_SECRET\n", "sub/ok.go": "// DIRLINK_OK\n"} {
		full := filepath.Join(proj, filepath.FromSlash(p))
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("sub", filepath.Join(proj, "dirlink")); err != nil {
		t.Fatal(err)
	}
	sp := rootSpellings(t, proj)[2]
	c := startServer(t, sp.cwd, t.TempDir(), bin, "mcp-server", "--root", sp.root, "--no-cache")
	c.handshake()
	defer stopServer(c)
	for _, call := range []map[string]any{
		{"name": "list_files", "arguments": map[string]any{"path": "dirlink"}},
		{"name": "search_in_files", "arguments": map[string]any{"path": "dirlink", "query": "DIRLINK"}},
		{"name": "find_symbol", "arguments": map[string]any{"pattern": "x", "path": "dirlink"}},
		{"name": "get_project_stats", "arguments": map[string]any{"path": "dirlink"}},
	} {
		if line := rawCall(t, c, "tools/call", call); strings.Contains(line, "DIRLINK_SECRET") || strings.Contains(line, "secret.go") {
			t.Errorf("%v enters the directory link:\n%s", call, line)
		}
	}
}
