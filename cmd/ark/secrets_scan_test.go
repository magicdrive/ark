package ark

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ark secrets-scan exits 0 with no findings, 1 with findings, 2 on a
// configuration or argument error, 3 when a file could not be scanned —
// and never prints a value.
func TestSecretsScan_ExitStatus(t *testing.T) {
	write := func(root string, files map[string]string) string {
		for name, body := range files {
			p := filepath.Join(root, name)
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	clean := write(t.TempDir(), map[string]string{"a.go": "package a\n"})
	found := write(t.TempDir(), map[string]string{"a.env": "PASSWORD=hunter2value\n"})
	broken := write(t.TempDir(), map[string]string{".ark-secrets-rules.toml": "version = 9\n", "a.env": "PASSWORD=hunter2value\n"})
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"clean", []string{clean}, 0},
		{"findings", []string{found}, 1},
		{"findings json", []string{"--format", "json", found}, 1},
		{"suggestions", []string{"--suggest-ignore", found}, 1},
		{"include ignored", []string{"--include-ignored", found}, 1},
		{"list rules", []string{"--list-rules", found}, 0},
		{"list rules json", []string{"--list-rules", "-f", "json", found}, 0},
		{"help", []string{"--help"}, 0},
		{"broken configuration", []string{broken}, 2},
		{"broken configuration, list", []string{"--list-rules", broken}, 2},
		{"no directory", []string{filepath.Join(clean, "missing")}, 2},
		{"bad format", []string{"--format", "yaml", clean}, 2},
		{"bad flag", []string{"--no-such-flag", clean}, 2},
		{"two directories", []string{clean, found}, 2},
	} {
		var out, errOut bytes.Buffer
		if got := runSecretsScanCommand(tc.args, &out, &errOut); got != tc.want {
			t.Errorf("%s: exit %d, want %d\nstdout %s\nstderr %s", tc.name, got, tc.want, out.String(), errOut.String())
		}
		if strings.Contains(out.String()+errOut.String(), "hunter2value") {
			t.Errorf("%s: a value printed", tc.name)
		}
		if strings.Contains(strings.Join(tc.args, " "), "json") {
			var v any
			if err := json.Unmarshal(out.Bytes(), &v); err != nil {
				t.Errorf("%s: not JSON: %v\n%s", tc.name, err, out.String())
			}
		}
	}

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Logf("SKIP part: permission bits do not make a file unreadable here (%s)", runtime.GOOS)
		return
	}
	unreadable := write(t.TempDir(), map[string]string{"a.env": "PASSWORD=hunter2value\n", "b.txt": "x"})
	if err := os.Chmod(filepath.Join(unreadable, "b.txt"), 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(unreadable, "b.txt"), 0o644)
	var out, errOut bytes.Buffer
	if got := runSecretsScanCommand([]string{unreadable}, &out, &errOut); got != 3 || !strings.Contains(out.String(), "b.txt: not scanned") {
		t.Errorf("an unreadable file: exit %d\n%s", got, out.String())
	}
}
