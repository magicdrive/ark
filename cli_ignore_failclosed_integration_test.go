package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// An ignore file the dump cannot read stops it before anything is written:
// non-zero exit, the reason on stderr, nothing on stdout, no output file
// created or changed — in every output format — so a caller that ignores the
// exit status never receives a partial dump or an excluded file.

var failClosedMarkers = []string{"MAIN_MARKER", "README_MARKER", "CRED_MARKER"}

// failClosedFixture: files that dump fine (src/main.go, README.md) next to a
// nested .arkignore that excludes credentials.txt.
func failClosedFixture(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repository")
	for p, body := range map[string]string{
		"src/main.go":                      "package main\n\n// MAIN_MARKER\nfunc main() {}\n",
		"README.md":                        "README_MARKER\n",
		"packages/service/.arkignore":      "credentials.txt\n",
		"packages/service/.gitignore":      "*.tmp\n",
		"packages/service/credentials.txt": "CRED_MARKER=1\n",
	} {
		full := filepath.Join(repo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

// makeUnreadable removes read permission from path and proves the read now
// fails, so the test cannot pass on a read that succeeded (root, Windows).
func makeUnreadable(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("the file is still readable; cannot test a read failure here")
	}
}

type dumpRun struct {
	code           int
	stdout, stderr string
	cwdEntries     []string // files the run left in its working directory
}

// runDump runs `ark <flags> <target>` from an empty working directory.
func runDump(t *testing.T, bin, target string, flags ...string) dumpRun {
	t.Helper()
	cwd := t.TempDir()
	cmd := exec.Command(bin, append(flags, target)...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	r := dumpRun{stdout: stdout.String(), stderr: stderr.String()}
	if exit, ok := err.(*exec.ExitError); ok {
		r.code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(cwd)
	for _, e := range entries {
		r.cwdEntries = append(r.cwdEntries, e.Name())
	}
	return r
}

// formatFlags are every output format, with and without compression where
// the format allows it.
var formatFlags = [][]string{
	{"-f", "plaintext"}, {"-f", "markdown"}, {"-f", "xml"}, {"-f", "arklite"}, {"-f", "auto"},
	{"-f", "plaintext", "-c"}, {"-f", "markdown", "-c"},
}

func assertFailedClosed(t *testing.T, label string, r dumpRun) {
	t.Helper()
	if r.code == 0 {
		t.Errorf("%s: exit 0", label)
	}
	if !strings.Contains(r.stderr, "ignore rules:") {
		t.Errorf("%s: stderr lacks the reason: %q", label, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("%s: stdout not empty: %q", label, r.stdout)
	}
	if len(r.cwdEntries) > 0 {
		t.Errorf("%s: output written: %v", label, r.cwdEntries)
	}
	if m := markersIn(r.stdout+r.stderr, failClosedMarkers); len(m) > 0 {
		t.Errorf("%s: file content leaked: %v", label, m)
	}
}

func TestCLIIgnoreFailClosed_NestedArkignoreUnreadable(t *testing.T) {
	bin := buildArk(t)
	repo := failClosedFixture(t)
	makeUnreadable(t, filepath.Join(repo, "packages", "service", ".arkignore"))
	for _, format := range formatFlags {
		for _, gitignore := range []string{"on", "off"} {
			flags := append(append([]string{}, format...), "-a", gitignore)
			// Default output file (in the working directory) and an explicit
			// one that already exists: neither is created or touched.
			assertFailedClosed(t, strings.Join(flags, " "), runDump(t, bin, repo, flags...))
			existing := filepath.Join(t.TempDir(), "previous.txt")
			if err := os.WriteFile(existing, []byte("PREVIOUS"), 0o644); err != nil {
				t.Fatal(err)
			}
			r := runDump(t, bin, repo, append(flags, "-o", existing)...)
			assertFailedClosed(t, strings.Join(flags, " ")+" -o", r)
			if b, _ := os.ReadFile(existing); string(b) != "PREVIOUS" {
				t.Errorf("%v -o: existing output changed: %q", flags, b)
			}
			if _, err := os.Stat(existing + ".arklite.txt"); err == nil {
				t.Errorf("%v -o: compressed output created", flags)
			}
		}
	}
}

// An unreadable .gitignore stops the dump as an ignore-rule failure only when
// .gitignore handling is on. With -a off its rules are not used and .arkignore
// still applies; the file itself is then ordinary content, so the dump
// completes once it is not dumped (-d on skips dotfiles' content) and is not
// refused for its rules otherwise.
func TestCLIIgnoreFailClosed_GitignoreUnreadable(t *testing.T) {
	bin := buildArk(t)
	repo := failClosedFixture(t)
	makeUnreadable(t, filepath.Join(repo, "packages", "service", ".gitignore"))
	for _, format := range formatFlags {
		assertFailedClosed(t, strings.Join(format, " ")+" -a on", runDump(t, bin, repo, append(format, "-a", "on")...))

		if r := runDump(t, bin, repo, append(format, "-a", "off", "-S")...); strings.Contains(r.stderr, "ignore rules:") {
			t.Errorf("%v -a off: refused for .gitignore rules: %s", format, r.stderr)
		}

		out := filepath.Join(t.TempDir(), "dump")
		r := runDump(t, bin, repo, append(format, "-a", "off", "-d", "on", "-S", "-o", out)...)
		if r.code != 0 {
			t.Fatalf("%v -a off -d on: exit %d: %s", format, r.code, r.stderr)
		}
		matches, _ := filepath.Glob(out + "*")
		if len(matches) != 1 {
			t.Fatalf("%v -a off -d on: outputs %v", format, matches)
		}
		dump, _ := os.ReadFile(matches[0])
		if !bytes.Contains(dump, []byte("MAIN_MARKER")) && !bytes.Contains(dump, []byte("func main")) {
			t.Errorf("%v -a off -d on: main.go missing", format)
		}
		if bytes.Contains(dump, []byte("CRED_MARKER")) {
			t.Errorf("%v -a off -d on: .arkignore not applied", format)
		}
	}
}

// The same repository dumps normally once the file is readable again.
func TestCLIIgnoreFailClosed_RecoversWhenReadable(t *testing.T) {
	bin := buildArk(t)
	repo := failClosedFixture(t)
	rules := filepath.Join(repo, "packages", "service", ".arkignore")
	makeUnreadable(t, rules)
	assertFailedClosed(t, "unreadable", runDump(t, bin, repo, "-S"))
	if err := os.Chmod(rules, 0o644); err != nil {
		t.Fatal(err)
	}
	r := runDump(t, bin, repo, "-S")
	if r.code != 0 || len(r.cwdEntries) != 1 {
		t.Fatalf("readable again: exit %d, outputs %v, stderr %s", r.code, r.cwdEntries, r.stderr)
	}
}
