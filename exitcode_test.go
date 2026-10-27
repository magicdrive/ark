package main

// Exit status test of make fmt-check: make runs the recipe with sh -c (no
// -e, no pipefail), and gofmt prints nothing on stdout when it fails, so an
// unchecked substitution reads a failing gofmt or git as "formatted".

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestMakefile_FmtCheckFailsWhenGofmtFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Makefile targets macOS and Linux")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not installed")
	}
	for _, tc := range []struct {
		name, gofmt, git string
		ok               bool
	}{
		{"formatted", "exit 0", "", true},
		{"unformatted", "echo main.go", "", false},
		{"gofmt fails", `echo "broken.go:1:1: expected 'package', found 'EOF'" >&2; exit 2`, "", false},
		{"git ls-files fails", "exit 0", `echo "fatal: not a git repository" >&2; exit 128`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubs := t.TempDir()
			write := func(name, script string) {
				if err := os.WriteFile(filepath.Join(stubs, name), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			write("gofmt", tc.gofmt)
			if tc.git != "" {
				write("git", tc.git)
			}
			cmd := exec.Command("make", "--no-print-directory", "fmt-check")
			for _, kv := range os.Environ() {
				k, _, _ := strings.Cut(kv, "=")
				if !slices.Contains([]string{"MAKEFLAGS", "MFLAGS", "MAKELEVEL", "PATH"}, k) {
					cmd.Env = append(cmd.Env, kv)
				}
			}
			cmd.Env = append(cmd.Env, "PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.ok {
				t.Errorf("make fmt-check: err %v, want ok %t\n%s", err, tc.ok, out)
			}
		})
	}
}
