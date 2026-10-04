package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildArk compiles the ark binary once for CLI integration tests.
func buildArk(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("integration test uses POSIX shell stub")
	}
	bin := filepath.Join(t.TempDir(), "ark-itest")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build ark: %v\n%s", err, out)
	}
	return bin
}

// runArk executes the built binary in dir with HOME overridden to home.
func runArk(t *testing.T, bin, dir, home string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run ark: %v", err)
		}
	}
	return stdout.String(), stderr.String(), code
}

func fakeArkBin(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ark")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCLI_SetupCursor_CreatesAndIsIdempotent(t *testing.T) {
	bin := buildArk(t)
	repo := t.TempDir()
	home := t.TempDir()
	ark := fakeArkBin(t)

	stdout, stderr, code := runArk(t, bin, repo, home, "setup", "cursor", "--ark-path", ark, "--root", repo)
	if code != 0 {
		t.Fatalf("exit %d\nstdout:%s\nstderr:%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Ark is ready for Cursor") {
		t.Errorf("missing success message:\n%s", stdout)
	}
	cfg := filepath.Join(repo, ".cursor", "mcp.json")
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("config not created: %v", err)
	}

	stdout, _, code = runArk(t, bin, repo, home, "setup", "cursor", "--ark-path", ark, "--root", repo)
	if code != 0 {
		t.Fatalf("second run exit %d", code)
	}
	if !strings.Contains(stdout, "No changes required") {
		t.Errorf("second run not a no-op:\n%s", stdout)
	}
}

func TestCLI_SetupUnknownClient_Fails(t *testing.T) {
	bin := buildArk(t)
	repo := t.TempDir()
	home := t.TempDir()

	_, stderr, code := runArk(t, bin, repo, home, "setup", "vscode")
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown client")
	}
	if !strings.Contains(stderr, "unsupported client") {
		t.Errorf("stderr should explain unsupported client:\n%s", stderr)
	}
}

func TestCLI_SetupNoClient_DeprecationWarning(t *testing.T) {
	bin := buildArk(t)
	repo := t.TempDir()
	home := t.TempDir()
	ark := fakeArkBin(t)

	_, stderr, code := runArk(t, bin, repo, home, "setup", "--ark-path", ark, "--root", repo)
	if code != 0 {
		t.Fatalf("exit %d\nstderr:%s", code, stderr)
	}
	if !strings.Contains(stderr, "deprecated") {
		t.Errorf("expected deprecation warning:\n%s", stderr)
	}
}

func TestCLI_SetupCodex_NotInstalled_Fails(t *testing.T) {
	bin := buildArk(t)
	repo := t.TempDir()
	home := t.TempDir()
	ark := fakeArkBin(t)

	// Run with an empty PATH so `codex` cannot be found.
	cmd := exec.Command(bin, "setup", "codex", "--ark-path", ark, "--root", repo)
	cmd.Dir = repo
	cmd.Env = []string{"HOME=" + home, "USERPROFILE=" + home, "PATH="}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected failure when codex is not installed")
	}
	if !strings.Contains(stderr.String(), "Codex CLI") {
		t.Errorf("stderr should mention Codex CLI:\n%s", stderr.String())
	}
}
