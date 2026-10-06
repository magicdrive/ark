package completion_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// This file drives the REAL zsh completion engine (compinit, the actual
// _arguments/_describe/_files builtins, zle) through a pty, using zsh's own
// zsh/zpty module. This is deliberately separate from the zshTarget in
// harness_test.go, whose `_arguments`/`_describe`/`_files` are hand-written
// recorders (see its doc comment: "This is NOT the compsys engine itself").
// That recorder-based harness can only ever be as correct as its own
// reimplementation of _arguments' semantics; it cannot catch a divergence
// between that reimplementation and real zsh. These tests close that gap by
// checking that the candidates are reachable from the real engine, not just
// that the completion scripts contain the right spec strings.
//
// zsh/zpty is a standard module shipped with zsh itself (not an external
// dependency), so this only needs zsh to be installed, same as the rest of
// this package's zsh tests. It is skipped when zsh is unavailable.

// realZshDriver spawns an interactive zsh under a pty with an isolated
// ZDOTDIR/fpath, loads the completion file under test (either autoloaded
// from fpath as "_ark", for the dedicated script, or sourced directly, for
// the combined script which self-registers via `compdef` when sourced),
// types the given line, presses Tab twice (once to insert any common
// prefix, once to force a listing even when ambiguous), and prints the raw
// pty transcript.
//
// Positional args ($1 kind, $2 completion file, $3 scratch dir, $4 line).
const realZshDriver = `
zmodload zsh/zpty

kind=$1
file=$2
tmproot=$3
line=$4

fpathdir="$tmproot/fpath"
workdir="$tmproot/work"
mkdir -p "$fpathdir" "$workdir"

extra=""
if [[ $kind == standalone ]]; then
  cp "$file" "$fpathdir/_ark"
else
  extra=". '$file'"
fi

cat > "$tmproot/.zshrc" <<RC
autoload -Uz compinit
fpath=("$fpathdir" \$fpath)
compinit -u -d "$tmproot/zcompdump"
$extra
ark() { :; }
PS1='RDY> '
unsetopt BEEP LIST_BEEP
RC

zpty sess "ZDOTDIR='$tmproot' HOME='$tmproot' zsh -i"
zpty -w sess "cd '$workdir'"

typeset -F t0=$EPOCHREALTIME
local buf="" c
while (( EPOCHREALTIME - t0 < 15 )); do
  zpty -r -t sess c 2>/dev/null && buf+=$c
  [[ $buf == *'RDY>'* ]] && break
  sleep 0.05
done

zpty -w sess "$line"$'\t\t'

buf=""; t0=$EPOCHREALTIME
local quiet=0
while (( EPOCHREALTIME - t0 < 10 )); do
  if zpty -r -t sess c 2>/dev/null; then
    buf+=$c; quiet=0
  else
    quiet=$((quiet+1))
    (( quiet > 15 )) && break
  fi
  sleep 0.1
done

print -r -- "$buf"
zpty -d sess 2>/dev/null
# Best-effort: the just-killed pty child can still be flushing a history
# file for a moment, which can make a single rm -rf pass race it and report
# "Directory not empty". Cleanup here is advisory (the Go side also removes
# this directory); never fail the whole script -- and therefore the
# completion assertions above, which already ran successfully -- over it.
rm -rf "$tmproot" 2>/dev/null
exit 0
`

var csiSeqRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)
var strayEscRe = regexp.MustCompile(`\x1b.`)
var wsRe = regexp.MustCompile(`\s+`)

// renderTerminal approximates what a human would see on screen: it strips
// ANSI control sequences and applies backspace/carriage-return the way a
// terminal does, instead of just discarding the bytes and hoping the
// candidate text survives verbatim.
func renderTerminal(raw string) string {
	s := csiSeqRe.ReplaceAllString(raw, "")
	s = strayEscRe.ReplaceAllString(s, "")
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\b':
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		case r == '\r':
			out = append(out, '\n')
		case r == '\n' || r == '\t' || r >= 0x20:
			out = append(out, r)
		}
	}
	return string(out)
}

// tokens splits rendered terminal text on whitespace, so that e.g. "claude"
// in "claude -- Configure Ark for Claude Code" is matched as a whole
// candidate, not as a substring of some unrelated word.
func tokens(rendered string) map[string]bool {
	set := map[string]bool{}
	for _, f := range wsRe.Split(rendered, -1) {
		if f != "" {
			set[f] = true
		}
	}
	return set
}

func realZshTargets() []struct{ name, file string } {
	return []struct{ name, file string }{
		{"standalone(_ark)", completionPath("zsh", "_ark")},
		{"combined(ark-completion.sh)", completionPath("ark-completion.sh")},
	}
}

// realZshComplete drives the actual compsys engine (not a recorder) for
// `ark <line>` and returns the rendered transcript covering everything from
// the echoed input through the resulting candidate listing or insertion.
func realZshComplete(t *testing.T, file, line string) string {
	t.Helper()
	zsh := requireTool(t, "zsh")
	trackInput(t, file)
	kind := "combined"
	if strings.HasSuffix(file, "/zsh/_ark") {
		kind = "standalone"
	}
	// A plain os.MkdirTemp, not t.TempDir(): t.TempDir() embeds the (possibly
	// truncated) subtest name in the path, e.g.
	// ".../TestX/combined(ark-com.../001" with an unbalanced "(" -- and that
	// path is handed to zpty, which concatenates and `eval`s it as a new
	// command line. An unbalanced paren there is a zsh parse error, which
	// hangs this test until the outer timeout kills it: a real failure mode,
	// but one in the test harness, not in the completion script under test.
	tmproot, err := os.MkdirTemp("", "ark-zsh-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmproot) })

	out := runWithTimeout(t, 40*time.Second, zsh, "-f", "-c", realZshDriver, "zsh", kind, file, tmproot, line)
	return renderTerminal(out)
}

// runWithTimeout is like run (harness_test.go) but with a caller-chosen
// timeout: spinning up a real interactive zsh under a pty (compinit, zle)
// is inherently slower and more timing-sensitive than the in-process fake
// harness, especially under load, so this gets a longer budget than the
// package's shared 20s default.
func runWithTimeout(t *testing.T, timeout time.Duration, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, errb.String())
	}
	return out.String()
}

// TestZshRealEngine_PositionalCandidatesReachable checks that positional
// candidates are reachable through the real engine, not merely present as
// strings in the completion file (TestSetupClientsMatchRegistry and friends
// cover that drift check). A broken `_arguments -C` / `->state` subcommand
// dispatch makes this test fail, while the recorder-based tests still pass
// because they never run `_arguments` itself.
func TestZshRealEngine_PositionalCandidatesReachable(t *testing.T) {
	requireTool(t, "zsh")

	cases := []struct {
		name    string
		line    string
		want    []string
		notWant []string
	}{
		{
			name: "setup clients",
			line: "ark setup ",
			want: []string{"claude", "cursor", "codex", "cline", "copilot-vscode", "copilot-cli"},
		},
		{
			name: "instruction targets",
			line: "ark instruction ",
			want: []string{"claude", "codex", "cursor", "cline", "copilot-vscode", "copilot-cli"},
		},
		{
			name: "skill subcommands",
			line: "ark skill ",
			want: []string{"init", "add-explorer", "update", "inspect"},
		},
		{
			name:    "setup prefix narrows to copilot-*",
			line:    "ark setup cop",
			want:    []string{"copilot-vscode", "copilot-cli"},
			notWant: []string{"claude", "cursor", "codex", "cline"},
		},
		{
			name:    "instruction prefix narrows to co*",
			line:    "ark instruction co",
			want:    []string{"codex", "copilot-vscode", "copilot-cli"},
			notWant: []string{"claude", "cursor", "cline"},
		},
		{
			name:    "skill prefix narrows to the unique match",
			line:    "ark skill up",
			want:    []string{"update"},
			notWant: []string{"init", "add-explorer", "inspect"},
		},
		{
			// Flags-before-client is rejected by SetupOptParse (the client
			// must be the first token), so the completion offering only
			// options here -- and no client names -- is the CORRECT
			// behavior, not a bug. This case pins that contract down.
			name:    "setup flag before client offers options, not clients",
			line:    "ark setup --force ",
			want:    []string{"--ark-path", "--global", "--root"},
			notWant: []string{"claude", "cursor", "codex", "cline", "copilot-vscode", "copilot-cli"},
		},
	}

	for _, tgt := range realZshTargets() {
		t.Run(tgt.name, func(t *testing.T) {
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					rendered := realZshComplete(t, tgt.file, c.line)
					got := tokens(rendered)
					for _, w := range c.want {
						if !got[w] {
							t.Errorf("ark %s<TAB>: expected candidate %q was not reachable from the real zsh completion engine\n--- transcript ---\n%s", c.line, w, rendered)
						}
					}
					for _, nw := range c.notWant {
						if got[nw] {
							t.Errorf("ark %s<TAB>: candidate %q should not have been offered\n--- transcript ---\n%s", c.line, nw, rendered)
						}
					}
				})
			}
		})
	}
}
