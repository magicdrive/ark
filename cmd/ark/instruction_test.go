package ark

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/instruction"
)

// buildArk compiles the real binary once per test.
func buildArk(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ark")
	cmd := exec.Command("go", "build", "-o", bin, filepath.Join("..", ".."))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// runArk runs the binary in dir with an isolated HOME and returns stdout,
// stderr and the exit code.
func runArk(t *testing.T, bin, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "USERPROFILE="+t.TempDir())
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

// tree snapshots every path (and file contents) under dir.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || p == dir {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if info.IsDir() {
			out[rel] = "<dir>"
		} else {
			b, _ := os.ReadFile(p)
			out[rel] = string(b)
		}
		return nil
	})
	return out
}

func sameTree(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// All six targets in canonical order.
var allInstructionTargets = []string{"claude", "codex", "cursor", "cline", "copilot-vscode", "copilot-cli"}

// `ark instruction claude`: exit 0, stdout is exactly the canonical Claude
// instruction, stderr is empty, repeated runs are identical, and the working
// directory (even one holding CLAUDE.md / .mcp.json) is untouched.
func TestInstructionClaude_RealBinary(t *testing.T) {
	bin := buildArk(t)
	want, err := instruction.Render("claude")
	if err != nil {
		t.Fatal(err)
	}

	repo := t.TempDir()
	for name, content := range map[string]string{
		"CLAUDE.md":   "# Mine\n",
		".mcp.json":   `{"mcpServers":{}}`,
		"src/main.go": "package main\n",
	} {
		p := filepath.Join(repo, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := tree(t, repo)

	stdout, stderr, code := runArk(t, bin, repo, "instruction", "claude")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if stdout != want {
		t.Errorf("stdout is not exactly the canonical instruction:\n%s", stdout)
	}
	if !strings.Contains(stdout, "mcp__ark__") {
		t.Error("claude output must carry the mcp__ark__ prefix")
	}
	if again, _, _ := runArk(t, bin, repo, "instruction", "claude"); again != stdout {
		t.Error("output is not deterministic across runs")
	}
	if !sameTree(before, tree(t, repo)) {
		t.Error("`ark instruction claude` changed the working directory")
	}

	// Redirecting to a file yields the canonical Markdown and nothing else.
	cmd := exec.Command(bin, "instruction", "claude")
	cmd.Dir = repo
	out := filepath.Join(t.TempDir(), "ark-instruction.md")
	f, _ := os.Create(out)
	cmd.Stdout = f
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if got, _ := os.ReadFile(out); string(got) != want {
		t.Error("redirected file differs from the canonical instruction")
	}
}

// Every supported target: exit 0, stdout matches instruction.Render exactly,
// stderr empty, filesystem unchanged. The five non-Claude targets additionally
// must be byte-identical to the canonical (bare-name) guidance.
func TestInstructionAllTargets_RealBinary(t *testing.T) {
	bin := buildArk(t)
	canonical := instruction.Guidance()
	for _, target := range allInstructionTargets {
		t.Run(target, func(t *testing.T) {
			want, err := instruction.Render(target)
			if err != nil {
				t.Fatal(err)
			}
			repo := t.TempDir()
			before := tree(t, repo)

			stdout, stderr, code := runArk(t, bin, repo, "instruction", target)
			if code != 0 {
				t.Fatalf("exit=%d stderr=%q", code, stderr)
			}
			if stderr != "" {
				t.Errorf("stderr not empty: %q", stderr)
			}
			if stdout != want {
				t.Errorf("stdout differs from instruction.Render(%q)", target)
			}
			if !sameTree(before, tree(t, repo)) {
				t.Errorf("ark instruction %s changed the filesystem", target)
			}
			if target != "claude" && stdout != canonical {
				t.Errorf("ark instruction %s is not byte-identical to the canonical guidance", target)
			}
		})
	}
	// The five plain targets are also byte-identical to one another.
	var plainOutputs = map[string]string{}
	for _, target := range allInstructionTargets {
		if target == "claude" {
			continue
		}
		out, _, _ := runArk(t, bin, t.TempDir(), "instruction", target)
		plainOutputs[target] = out
	}
	var first string
	for target, out := range plainOutputs {
		if first == "" {
			first = out
			continue
		}
		if out != first {
			t.Errorf("ark instruction %s differs from the other plain targets", target)
		}
	}
}

// Unsupported or missing targets fail non-zero with the supported targets on
// stderr, nothing on stdout and no filesystem mutation.
func TestInstruction_UnsupportedTargets_RealBinary(t *testing.T) {
	bin := buildArk(t)
	repo := t.TempDir()
	before := tree(t, repo)
	for _, args := range [][]string{
		{"instruction", "agents"}, {"instruction", "copilot"}, {"instruction", "cursor-rules"},
		{"instruction", "claude-code"}, {"instruction", "unknown"},
		{"instruction"}, {"instruction", "claude", "extra"},
	} {
		stdout, stderr, code := runArk(t, bin, repo, args...)
		if code == 0 {
			t.Errorf("ark %v: exit 0, want failure", args)
		}
		if stdout != "" {
			t.Errorf("ark %v: wrote to stdout: %q", args, stdout)
		}
		if !strings.Contains(stderr, "Error:") {
			t.Errorf("ark %v: no error on stderr: %q", args, stderr)
		}
		if len(args) <= 2 && !strings.Contains(stderr, "supported targets: "+strings.Join(allInstructionTargets, ", ")) {
			t.Errorf("ark %v: stderr does not list the supported targets: %q", args, stderr)
		}
	}
	if !sameTree(before, tree(t, repo)) {
		t.Error("a rejected instruction command changed the working directory")
	}
}

// `ark instruction --help` prints help and exits 0 without writing anything.
func TestInstruction_Help(t *testing.T) {
	bin := buildArk(t)
	repo := t.TempDir()
	stdout, _, code := runArk(t, bin, repo, "instruction", "--help")
	if code != 0 || !strings.Contains(stdout, "instruction <target>") {
		t.Errorf("help: exit=%d stdout=%q", code, stdout)
	}
	if len(tree(t, repo)) != 0 {
		t.Error("help wrote files")
	}
}
