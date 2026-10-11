package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The GORACE the tests give their processes, for the race report path of a
// Windows runner: a drive colon, backslashes and a short name, all inside the
// quoted log_path value; the caller's options in between.
func TestGoraceFor_WindowsPath(t *testing.T) {
	const report = `C:\Users\RUNNER~1\AppData\Local\Temp\ark-itest-race-123\report`
	for _, tc := range []struct{ caller, want string }{
		{"", `atexit_sleep_ms=0 log_path="` + report + `"`},
		{"  halt_on_error=1 exitcode=3 ", `atexit_sleep_ms=0 halt_on_error=1 exitcode=3 log_path="` + report + `"`},
	} {
		if got, err := goraceFor(tc.caller, report); err != nil || got != tc.want {
			t.Errorf("goraceFor(%q):\n got %s (%v)\nwant %s", tc.caller, got, err, tc.want)
		}
	}
	const spaced = `C:\Users\John Doe\AppData\Local\Temp\ark-itest-race-1\report`
	if got, err := goraceFor("", spaced); err != nil || !strings.HasSuffix(got, ` log_path="`+spaced+`"`) {
		t.Errorf("goraceFor with a space: %s (%v)", got, err)
	}
	if got, err := goraceFor("", `C:\a"b\report`); err == nil {
		t.Errorf("a double quote in the path must be refused, got %s", got)
	}
}

// The race detector itself takes that GORACE: a program with a data race,
// built with -race and run with it, reports the race to the log_path file —
// for directories whose names end an unquoted option value (a colon, as in
// "C:", a comma, a space) — and its option parser reports no error.
func TestGoraceFor_RaceDetectorTakesIt(t *testing.T) {
	src := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":  "module racy\n\ngo 1.22\n",
		"main.go": "package main\n\nfunc main() {\n\tvar x int\n\tdone := make(chan bool)\n\tgo func() { x++; done <- true }()\n\tx++\n\t<-done\n}\n",
	} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(t.TempDir(), "racy")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-race", "-o", bin, ".")
	build.Dir = src
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build -race: %v\n%s", err, out)
	}

	// t.TempDir is "C:\..." on Windows, which cannot hold a colon in a name.
	names := []string{"space dir", "comma,dir"}
	if runtime.GOOS != "windows" {
		names = append(names, "colon:dir", `C:\Users\RUNNER~1\AppData\Local\Temp\ark-itest-race-1`)
	}
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GORACE=") {
			env = append(env, kv)
		}
	}
	for _, name := range names {
		dir := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		gorace, err := goraceFor("", filepath.Join(dir, "report"))
		if err != nil {
			t.Fatal(err)
		}
		run := exec.Command(bin)
		run.Env = append(env, "GORACE="+gorace)
		var stderr strings.Builder
		run.Stderr = &stderr
		_ = run.Run() // exits 66: the race (or, had the parser failed, the error)
		if strings.Contains(stderr.String(), "ERROR") {
			t.Errorf("%q: the race detector rejects GORACE=%s:\n%s", name, gorace, stderr.String())
		}
		var reports []string // os.ReadDir: dir's name is not a pattern
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "report.") {
				reports = append(reports, filepath.Join(dir, e.Name()))
			}
		}
		if len(reports) != 1 {
			t.Errorf("%q: %d race reports in the log_path directory, want 1 (GORACE=%s)", name, len(reports), gorace)
			continue
		}
		if text, _ := os.ReadFile(reports[0]); !strings.Contains(string(text), "WARNING: DATA RACE") {
			t.Errorf("%q: the report holds no race:\n%s", name, text)
		}
	}
}
