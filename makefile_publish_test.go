package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestMakefile_Publish drives the publish targets with `hephaestus publish`
// replaced by a stub that records how it was called (PUBLISH=stub): the
// version reaches it once, through the environment, never through a shell
// command line; anything ambiguous fails before it runs; make -n runs
// nothing. No git or GitHub access. The wiring is the one of Hephaestus's
// own Makefile (docs/publish.md of Hephaestus).
func TestMakefile_Publish(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Makefile targets macOS and Linux")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not installed")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	stub := filepath.Join(dir, "stub")
	script := "#!/bin/sh\nprintf '%s|%s|%s|%s\\n' \"$*\" \"$HEPHAESTUS_PUBLISH_ARGS\" \"$HEPHAESTUS_PUBLISH_VERSION\" \"$HEPHAESTUS_PUBLISH_TAG\" >> " + log + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	mk := func(env []string, args ...string) (string, []string, error) {
		os.Remove(log)
		cmd := exec.Command("make", args...)
		// A clean make environment: no MAKEFLAGS or jobserver from a parent
		// make, and no PUBLISH, HEPHAESTUS, VERSION, YES or tag unless the
		// case sets them.
		for _, kv := range os.Environ() {
			k, _, _ := strings.Cut(kv, "=")
			if !slices.Contains([]string{"MAKEFLAGS", "MFLAGS", "MAKELEVEL", "PUBLISH", "HEPHAESTUS", "HEPHAESTUS_VERSION", "VERSION", "YES", "tag"}, k) &&
				!strings.HasPrefix(k, "HEPHAESTUS_PUBLISH_") {
				cmd.Env = append(cmd.Env, kv)
			}
		}
		cmd.Env = append(cmd.Env, env...)
		out, err := cmd.CombinedOutput()
		calls, _ := os.ReadFile(log)
		var lines []string
		for _, l := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
			if l != "" {
				lines = append(lines, l)
			}
		}
		return string(out), lines, err
	}
	withStub := func(args ...string) []string {
		return append([]string{"--no-print-directory", "PUBLISH=" + stub}, args...)
	}

	for _, tc := range []struct {
		name string
		env  []string
		args []string
		want string // the one stub call: "args|ARGS|VERSION|TAG"
	}{
		{"positional", nil, []string{"publish", "v1.0.0"}, "publish -from-make|v1.0.0||"},
		{"prerelease", nil, []string{"publish", "v1.0.0-rc.1"}, "publish -from-make|v1.0.0-rc.1||"},
		{"VERSION=", nil, []string{"publish", "VERSION=v1.0.0"}, "publish -from-make||v1.0.0|"},
		{"tag= (as before)", nil, []string{"publish", "tag=v1.0.0"}, "publish -from-make|||v1.0.0"},
		{"both reach the tool, which rejects them", nil, []string{"publish", "v1.0.0", "VERSION=v1.0.0"}, "publish -from-make|v1.0.0|v1.0.0|"},
		{"no version reaches the tool, which rejects it", nil, []string{"publish"}, "publish -from-make|||"},
		{"YES=1 on the command line", nil, []string{"publish", "v1.0.0", "YES=1"}, "publish -from-make -yes|v1.0.0||"},
		{"YES other than 1 asks", nil, []string{"publish", "v1.0.0", "YES=yes"}, "publish -from-make|v1.0.0||"},
		{"YES, VERSION and tag from the environment are ignored", []string{"YES=1", "VERSION=v9.9.9", "tag=v8.8.8"}, []string{"publish", "v1.0.0"}, "publish -from-make|v1.0.0||"},
		{"check", nil, []string{"publish-check", "v1.0.0"}, "check -from-make|v1.0.0||"},
		{"check tag=", nil, []string{"publish-check", "tag=v1.0.0"}, "check -from-make|||v1.0.0"},
		{"status", nil, []string{"publish-status", "v1.0.0"}, "status -from-make|v1.0.0||"},
		{"watch", nil, []string{"publish-watch", "v1.0.0"}, "watch -from-make|v1.0.0||"},
		{"help", nil, []string{"publish-help"}, "help|||"},
		{"parallel make runs the tool once", nil, []string{"-j4", "publish", "v1.0.0"}, "publish -from-make|v1.0.0||"},
		// Were the goal pasted into the recipe, the shell would run false.
		{"shell metacharacters stay data", nil, []string{"publish", "v1;false"}, "publish -from-make|v1;false||"},
		{"a quote stays data", nil, []string{"publish", "v1'$(false)"}, "publish -from-make|v1'$(false)||"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, calls, err := mk(tc.env, withStub(tc.args...)...)
			if err != nil {
				t.Fatalf("make %v: %v\n%s", tc.args, err, out)
			}
			if len(calls) != 1 || calls[0] != tc.want {
				t.Fatalf("make %v called the tool %q, want once %q\n%s", tc.args, calls, tc.want, out)
			}
		})
	}

	// Refused by make before the tool runs: two versions, a version that is
	// not one, another target with a publish target (before or after it),
	// two publish targets, publish-help with a version.
	for _, args := range [][]string{
		{"publish", "v1.0.0", "v2.0.0"},
		{"publish", "latest"},
		{"publish", "x;false"},
		{"build", "publish", "v1.0.0"},
		{"test", "publish", "v1.0.0"},
		{"publish", "publish-check", "v1.0.0"},
		{"publish-check", "publish", "v1.0.0"},
		{"publish-help", "v1.0.0"},
		{"publish", "clean"},
		{"publish", "v1.0.0", "vet"},
		{"publish", "preflight", "tag=v1.0.0"},
	} {
		out, calls, err := mk(nil, withStub(args...)...)
		if err == nil || len(calls) != 0 {
			t.Errorf("make %v: err %v, calls %q\n%s", args, err, calls, out)
		}
	}

	out, calls, err := mk(nil, withStub("-n", "publish", "v1.0.0")...)
	if err != nil || len(calls) != 0 || !strings.Contains(out, "publish -from-make") {
		t.Errorf("make -n ran the tool (calls %q, err %v)\n%s", calls, err, out)
	}

	// Without PUBLISH, the targets run `hephaestus publish` of
	// HEPHAESTUS_VERSION (make -n: nothing runs).
	mkfile, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mkfile), "\nPUBLISH ?= $(HEPHAESTUS) publish\n") {
		t.Error("Makefile: PUBLISH must default to $(HEPHAESTUS) publish")
	}
	for target, want := range map[string]string{
		"publish":        "go run github.com/magicdrive/hephaestus@$V publish publish -from-make\n",
		"publish-check":  "go run github.com/magicdrive/hephaestus@$V publish check -from-make\n",
		"publish-status": "go run github.com/magicdrive/hephaestus@$V publish status -from-make\n",
		"publish-watch":  "go run github.com/magicdrive/hephaestus@$V publish watch -from-make\n",
	} {
		// The recipe's line, then the version goal's no-op.
		out, _, err := mk(nil, "--no-print-directory", "-n", "HEPHAESTUS_VERSION=v9.8.7", target, "v1.0.0")
		lines := strings.Split(out, "\n")
		want = strings.ReplaceAll(want, "$V", "v9.8.7")
		if err != nil || len(lines) != 3 || strings.TrimRight(lines[0], " ")+"\n" != want || lines[1] != ":" {
			t.Errorf("make -n %s v1.0.0: %v\n%q\nwant %q", target, err, out, want)
		}
	}

	// The release checks that stay as they were: make preflight tag=.
	out, calls, err = mk(nil, "--no-print-directory", "HEPHAESTUS="+stub, "preflight", "tag=v1.0.0")
	if err != nil || len(calls) != 1 || calls[0] != "preflight --tag v1.0.0 --strict|||" {
		t.Errorf("make preflight tag=v1.0.0: err %v, calls %q\n%s", err, calls, out)
	}
}
