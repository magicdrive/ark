package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The repository dump and the MCP server give one .arkignore / .gitignore the
// same meaning: the dump selects the same files wherever it is run from, and
// the same files the MCP file tools list; symlinks never stop it.

func cliIgnoreFixture(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	for p, body := range map[string]string{
		".arkignore":      "ark_root.txt\n/sub/ark_anchored.txt\nsecret.txt\n",
		".gitignore":      "git_root.txt\n",
		"sub/.arkignore":  "*.log\n!keep.log\n",
		"both/.gitignore": "g.txt\n",
		"both/.arkignore": "a.txt\n",
		"neg/.gitignore":  "!secret.txt\n",
		"ark_root.txt":    "x", "git_root.txt": "x", "keep.txt": "x", "secret.txt": "x",
		"sub/a.log": "x", "sub/keep.log": "x", "sub/ark_anchored.txt": "x", "sub/plain.txt": "x",
		"both/g.txt": "x", "both/a.txt": "x", "both/n.txt": "x", "neg/secret.txt": "x",
	} {
		full := filepath.Join(repo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

// dumpedFiles runs the dump and returns the files it dumped, relative to the
// target.
func dumpedFiles(t *testing.T, bin, cwd, target string, flags ...string) []string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "dump.txt")
	cmd := exec.Command(bin, append(append([]string{"-S", "-o", out}, flags...), target)...)
	cmd.Dir = cwd
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ark %v %s (in %s): %v\n%s", flags, target, cwd, err, b)
	}
	dump, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, line := range strings.Split(string(dump), "\n") {
		if !strings.HasPrefix(line, "=== ") {
			continue
		}
		p := strings.TrimSuffix(strings.TrimPrefix(line, "=== "), " ===")
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		base := target
		if !filepath.IsAbs(base) {
			base = filepath.Join(cwd, base)
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, filepath.ToSlash(rel))
	}
	sort.Strings(files)
	return files
}

func TestCLIIgnore_SameFilesFromAnyDirectory(t *testing.T) {
	bin := buildArk(t)
	repo := cliIgnoreFixture(t)
	elsewhere := t.TempDir()
	for _, flags := range [][]string{nil, {"-a", "off"}} {
		want := dumpedFiles(t, bin, repo, ".", flags...)
		for _, run := range []struct{ cwd, target string }{
			{elsewhere, repo},
			{filepath.Join(repo, "sub"), ".."},
			{filepath.Dir(repo), "repo"},
		} {
			if got := dumpedFiles(t, bin, run.cwd, run.target, flags...); strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("%v, cwd %s, target %s:\n got %v\nwant %v", flags, run.cwd, run.target, got, want)
			}
		}
		for _, excluded := range []string{"ark_root.txt", "secret.txt", "neg/secret.txt", "sub/a.log", "sub/ark_anchored.txt", "both/a.txt"} {
			for _, f := range want {
				if f == excluded {
					t.Errorf("%v: %s dumped", flags, f)
				}
			}
		}
	}
	// A subdirectory target reads its own rules only, from anywhere.
	sub := dumpedFiles(t, bin, repo, "sub")
	if got := dumpedFiles(t, bin, elsewhere, filepath.Join(repo, "sub")); strings.Join(got, " ") != strings.Join(sub, " ") {
		t.Errorf("subdirectory target depends on the working directory: %v vs %v", got, sub)
	}
}

// With the same root and rules, the dump selects exactly the files the MCP
// file tools list.
func TestCLIIgnore_DumpMatchesMCPListing(t *testing.T) {
	bin := buildArk(t)
	repo := cliIgnoreFixture(t)
	c := startServer(t, repo, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	defer stopServer(c)
	for _, gitignore := range []bool{true, false} {
		text, isErr := c.tool("list_files", map[string]any{"path": ".", "allowGitignore": gitignore})
		if isErr {
			t.Fatal(text)
		}
		listed := strings.Split(strings.TrimSpace(text), "\n")
		sort.Strings(listed)
		var flags []string
		if !gitignore {
			flags = []string{"-a", "off"}
		}
		if dumped := dumpedFiles(t, bin, repo, ".", flags...); strings.Join(dumped, " ") != strings.Join(listed, " ") {
			t.Errorf(".gitignore %v:\n dump %v\n  MCP %v", gitignore, dumped, listed)
		}
	}
}

// Symlinks — to a file, to a directory, outside the repository, dangling,
// looping — never stop the dump; directory links are not followed.
func TestCLIIgnore_SymlinksDoNotStopTheDump(t *testing.T) {
	bin := buildArk(t)
	repo := cliIgnoreFixture(t)
	ext := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ext, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "inner", "deep.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		"filelink.txt": "keep.txt", "dirlink": "sub", "extdir": ext, "broken.txt": "missing",
		"loopa": "loopb", "loopb": "loopa", "selfdir": ".",
	} {
		if err := os.Symlink(target, filepath.Join(repo, link)); err != nil {
			t.Fatal(err)
		}
	}
	files := dumpedFiles(t, bin, repo, ".")
	joined := " " + strings.Join(files, " ") + " "
	for _, want := range []string{" filelink.txt ", " keep.txt ", " sub/plain.txt "} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s not dumped: %v", want, files)
		}
	}
	for _, never := range []string{"dirlink/", "extdir", "deep.txt", "broken.txt", "loopa", "selfdir"} {
		if strings.Contains(joined, never) {
			t.Errorf("%s dumped: %v", never, files)
		}
	}
}
