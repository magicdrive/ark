package completion_test

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A target is one completion implementation. complete returns the candidates
// for `ark <words...>` where the last word is the (possibly empty) word under
// the cursor. File and directory completion is reported as the markers
// "<files>" and "<dirs>" for zsh/fish, and as real names for bash.
type target struct {
	name       string
	shortFlags bool // offers -x as well as --long
	complete   func(t *testing.T, cwd string, words ...string) []string
}

// completionPath returns the absolute path of a file under misc/completions
// (shells are run in other working directories).
func completionPath(parts ...string) string {
	p, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "misc", "completions"}, parts...)...))
	if err != nil {
		panic(err)
	}
	return p
}

// trackInput makes the test process itself read a script. The shells run as
// subprocesses, and `go test` only invalidates its result cache for files the
// test process opened — without this, editing a completion file would keep
// returning a stale cached PASS.
func trackInput(t *testing.T, path string) { t.Helper(); _ = readFile(t, path) }

func requireTool(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not installed", name)
	}
	return p
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, errb.String())
	}
	return out.String()
}

// ---------------------------------------------------------------------------
// bash: source the script, find the function registered with `complete -F`,
// and invoke it exactly as readline would (COMP_WORDS / COMP_CWORD).
// ---------------------------------------------------------------------------

const bashRunner = `
file=$1; shift
source "$file"
fn=$(complete -p ark | sed -E 's/.*-F ([^ ]+) ark.*/\1/')
COMP_WORDS=("$@"); COMP_CWORD=$(( $# - 1 )); COMP_LINE="${COMP_WORDS[*]}"; COMP_POINT=${#COMP_LINE}
"$fn" "${COMP_WORDS[0]}" "${COMP_WORDS[COMP_CWORD]}" "${COMP_WORDS[COMP_CWORD-1]}"
for c in "${COMPREPLY[@]}"; do printf '%s\n' "$c"; done
`

func bashTarget(name, file string) target {
	return target{
		name:       name,
		shortFlags: true,
		complete: func(t *testing.T, cwd string, words ...string) []string {
			trackInput(t, file)
			bash := requireTool(t, "bash")
			args := append([]string{"-c", bashRunner, "bash", file, "ark"}, words...)
			return lines(run(t, cwd, bash, args...))
		},
	}
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// zsh: run the real script under a real zsh with compsys' `_arguments`,
// `_describe` and `_files` replaced by recorders. The script's own control flow
// (which spec set it hands to _arguments at a given cursor position) is real;
// the interpretation of the recorded specs below follows _arguments' documented
// semantics. This is NOT the compsys engine itself.
// ---------------------------------------------------------------------------

const zshRunner = `
kind=$1; file=$2; shift 2
US=$'\x1f'; RS=$'\x1e'
typeset -ga words; words=("$@"); CURRENT=${#words}
compdef() { :; }
_files() { print -r -- "FILES${US}$*"; }
_describe() { local -a items; items=("${(@P)2}"); print -r -- "DESCRIBE${US}${(pj:$US:)items}"; }
# takes_arg NAME: does one of the current specs declare NAME as an option with a value?
_takes_arg() {
  local s
  for s in "${specs[@]}"; do
    [[ $s == ${1}:* || $s == ${1}\[*\]:* ]] && return 0
  done
  return 1
}
_arguments() {
  local -a specs; local a i k=0
  for a in "$@"; do [[ $a == -C ]] || specs+=("$a"); done
  if [[ -n ${specs[(r)*->subcmd]} ]]; then
    for (( i=2; i<CURRENT; i++ )); do
      if [[ $words[i] == -* ]]; then
        _takes_arg $words[i] && (( i++ ))
      else
        k=$i; break
      fi
    done
    if (( k )); then
      state=args; words=("${(@)words[k,-1]}"); CURRENT=$(( CURRENT - k + 1 )); return 0
    fi
    if [[ $words[CURRENT] != -* ]] && ! { (( CURRENT > 2 )) && [[ $words[CURRENT-1] == -* ]] && _takes_arg $words[CURRENT-1]; }; then
      state=subcmd; return 0
    fi
    state=''
  fi
  print -r -- "CALL${US}${CURRENT}${US}${(pj:$RS:)words}${US}${(pj:$US:)specs}"
}
if [[ $kind == standalone ]]; then
  _ark() { . "$file"; }
  _ark
else
  . "$file"
  _ark_zsh
fi
`

func zshTarget(name, kind, file string) target {
	return target{
		name:       name,
		shortFlags: false, // zsh specs declare long options (plus -h); short aliases are not offered
		complete: func(t *testing.T, cwd string, words ...string) []string {
			trackInput(t, file)
			zsh := requireTool(t, "zsh")
			args := append([]string{"-f", "-c", zshRunner, "zsh", kind, file, "ark"}, words...)
			return zshCandidates(t, run(t, cwd, zsh, args...), words[len(words)-1])
		},
	}
}

const us, rs = "\x1f", "\x1e"

// zshCandidates interprets the recorder output.
func zshCandidates(t *testing.T, out, cur string) []string {
	t.Helper()
	var cands []string
	lastCall := ""
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "DESCRIBE"+us):
			for _, item := range strings.Split(strings.TrimPrefix(l, "DESCRIBE"+us), us) {
				name, _, _ := strings.Cut(item, ":")
				if strings.HasPrefix(name, cur) {
					cands = append(cands, name)
				}
			}
		case strings.HasPrefix(l, "FILES"+us):
			if strings.Contains(l, "-/") {
				cands = append(cands, "<dirs>")
			} else {
				cands = append(cands, "<files>")
			}
		case strings.HasPrefix(l, "CALL"+us):
			lastCall = l
		}
	}
	if lastCall != "" {
		cands = append(cands, zshCall(t, lastCall)...)
	}
	return cands
}

type zspec struct {
	name     string // option name, "" for positionals
	takesArg bool
	action   string
	pos      int // 1-based positional index, 0 for rest (*)
}

var (
	zOptRe = regexp.MustCompile(`^(--?[A-Za-z0-9][A-Za-z0-9-]*)(\[[^\]]*\])?(:.*)?$`)
	zPosRe = regexp.MustCompile(`^(\d+|\*)::?(.*)$`)
)

// splitUnescaped splits s at the first n unescaped colons.
func splitUnescaped(s string, n int) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s) && len(parts) < n; i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == ':' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

func parseZspec(spec string) (zspec, bool) {
	if m := zOptRe.FindStringSubmatch(spec); m != nil {
		z := zspec{name: m[1]}
		if m[3] != "" {
			z.takesArg = true
			if p := splitUnescaped(m[3][1:], 1); len(p) == 2 {
				z.action = p[1]
			}
		}
		return z, true
	}
	if m := zPosRe.FindStringSubmatch(spec); m != nil {
		z := zspec{}
		if m[1] != "*" {
			z.pos, _ = strconv.Atoi(m[1])
		}
		if p := splitUnescaped(m[2], 1); len(p) == 2 {
			z.action = p[1]
		}
		return z, true
	}
	return zspec{}, false
}

// zshActionValues turns a spec action into candidates.
func zshActionValues(action string) []string {
	switch {
	case strings.HasPrefix(action, "(("):
		body := strings.TrimSuffix(strings.TrimPrefix(action, "(("), "))")
		var out []string
		for _, item := range shellFields(body) {
			name, _, _ := strings.Cut(item, `\:`)
			out = append(out, name)
		}
		return out
	case strings.HasPrefix(action, "("):
		return strings.Fields(strings.TrimSuffix(strings.TrimPrefix(action, "("), ")"))
	case strings.HasPrefix(action, "_files -/"):
		return []string{"<dirs>"}
	case strings.HasPrefix(action, "_files"):
		return []string{"<files>"}
	}
	return nil
}

// shellFields splits on whitespace outside double quotes.
func shellFields(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for _, r := range s {
		switch {
		case r == '"':
			inQ = !inQ
			cur.WriteRune(r)
		case (r == ' ' || r == '\t') && !inQ:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func zshCall(t *testing.T, line string) []string {
	f := strings.Split(line, us)
	cur, _ := strconv.Atoi(f[1])
	words := strings.Split(f[2], rs)
	var specs []zspec
	for _, s := range f[3:] {
		if z, ok := parseZspec(s); ok {
			specs = append(specs, z)
		} else {
			t.Fatalf("unparsable zsh spec %q", s)
		}
	}
	curWord := words[cur-1]
	var out []string
	switch {
	case strings.HasPrefix(curWord, "-"):
		for _, z := range specs {
			if z.name != "" && strings.HasPrefix(z.name, curWord) {
				out = append(out, z.name)
			}
		}
		return out
	}
	opt := func(name string) (zspec, bool) {
		for _, z := range specs {
			if z.name == name {
				return z, true
			}
		}
		return zspec{}, false
	}
	// Value of the preceding option.
	if cur >= 2 {
		if z, ok := opt(words[cur-2]); ok && z.takesArg {
			return filterPrefix(zshActionValues(z.action), curWord)
		}
	}
	// Positional operand: count operands before the cursor (words[0] is the
	// command word itself).
	n := 0
	for i := 1; i < cur-1; i++ {
		w := words[i]
		if strings.HasPrefix(w, "-") {
			if z, ok := opt(w); ok && z.takesArg {
				i++
			}
			continue
		}
		n++
	}
	for _, z := range specs {
		if z.name == "" && z.pos == n+1 {
			return filterPrefix(zshActionValues(z.action), curWord)
		}
	}
	for _, z := range specs {
		if z.name == "" && z.pos == 0 {
			return filterPrefix(zshActionValues(z.action), curWord)
		}
	}
	return nil
}

func filterPrefix(in []string, prefix string) []string {
	var out []string
	for _, s := range in {
		if strings.HasPrefix(s, prefix) {
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// fish: fish is not required to run the tests, so the completion file is
// analysed statically. Conditions are evaluated with the semantics of the
// helper functions defined in the file itself. Runtime fish behaviour is NOT
// executed (see the report's limitations).
// ---------------------------------------------------------------------------

type fishEntry struct {
	cond  string
	long  []string
	short []string
	args  []string // -a values
}

var (
	fishForRe = regexp.MustCompile(`^for (\w+) in (.+)$`)
	fishOptRe = regexp.MustCompile(`\s-(l|s|a|n|d)\s+('[^']*'|"[^"]*"|\S+)`)
)

func parseFish(t *testing.T, path string) []fishEntry {
	t.Helper()
	data := readFile(t, path)
	// Join backslash continuations.
	text := strings.ReplaceAll(string(data), "\\\n", " ")
	var raw []string
	var loopVar string
	var loopVals []string
	var body []string
	inLoop := false
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if m := fishForRe.FindStringSubmatch(l); m != nil {
			inLoop, loopVar, loopVals, body = true, m[1], strings.Fields(m[2]), nil
			continue
		}
		if inLoop {
			if l == "end" {
				for _, v := range loopVals {
					for _, b := range body {
						raw = append(raw, strings.ReplaceAll(b, "$"+loopVar, v))
					}
				}
				inLoop = false
				continue
			}
			body = append(body, l)
			continue
		}
		raw = append(raw, l)
	}
	var out []fishEntry
	for _, l := range raw {
		if !strings.HasPrefix(l, "complete -c ark") {
			continue
		}
		e := fishEntry{}
		for _, m := range fishOptRe.FindAllStringSubmatch(" "+strings.TrimPrefix(l, "complete -c ark"), -1) {
			v := strings.Trim(m[2], `'"`)
			switch m[1] {
			case "l":
				e.long = append(e.long, v)
			case "s":
				e.short = append(e.short, v)
			case "n":
				e.cond = v
			case "a":
				e.args = strings.Fields(v)
			}
		}
		out = append(out, e)
	}
	return out
}

// fishCond evaluates a condition given the tokens before the cursor
// (tokens[0] == "ark"), following the helper functions in ark.fish.
func fishCond(cond string, tokens []string) bool {
	if cond == "" {
		return true
	}
	for _, c := range strings.Split(cond, "; and ") {
		c = strings.TrimSpace(c)
		var ok bool
		switch {
		case c == "__fish_ark_is_first_arg":
			ok = len(tokens) == 1
		case c == "__fish_ark_no_subcommand":
			ok = len(tokens) < 2 || !contains([]string{"mcp-server", "mcp-init", "setup", "syntax", "symbol", "skill"}, tokens[1])
		case c == "__fish_ark_setup_client_position":
			ok = len(tokens) == 2 && tokens[1] == "setup"
		case c == "__fish_ark_skill_no_subcmd":
			ok = true
			for _, s := range []string{"init", "add-explorer", "update", "inspect"} {
				if contains(tokens, s) {
					ok = false
				}
			}
		case strings.HasPrefix(c, "__fish_seen_subcommand_from "):
			ok = false
			for _, w := range strings.Fields(strings.TrimPrefix(c, "__fish_seen_subcommand_from ")) {
				if contains(tokens[1:], w) {
					ok = true
				}
			}
		default:
			return false // unknown condition: treat as never satisfied so tests flag it
		}
		if !ok {
			return false
		}
	}
	return true
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func fishTarget(name, path string) target {
	return target{
		name:       name,
		shortFlags: true,
		complete: func(t *testing.T, cwd string, words ...string) []string {
			entries := parseFish(t, path)
			tokens := append([]string{"ark"}, words[:len(words)-1]...)
			cur := words[len(words)-1]
			var out []string
			for _, e := range entries {
				if !fishCond(e.cond, tokens) {
					continue
				}
				switch {
				case strings.HasPrefix(cur, "-"):
					for _, l := range e.long {
						if n := "--" + l; strings.HasPrefix(n, cur) {
							out = append(out, n)
						}
					}
					if !strings.HasPrefix(cur, "--") {
						for _, s := range e.short {
							if n := "-" + s; strings.HasPrefix(n, cur) {
								out = append(out, n)
							}
						}
					}
				case len(e.long)+len(e.short) == 0:
					out = append(out, filterPrefix(e.args, cur)...)
				default:
					// Value of the preceding option.
					prev := ""
					if len(tokens) > 1 {
						prev = tokens[len(tokens)-1]
					}
					for _, l := range e.long {
						if prev == "--"+l {
							out = append(out, filterPrefix(e.args, cur)...)
						}
					}
					for _, s := range e.short {
						if prev == "-"+s {
							out = append(out, filterPrefix(e.args, cur)...)
						}
					}
				}
			}
			return out
		},
	}
}

func allTargets() []target {
	return []target{
		bashTarget("bash", completionPath("bash", "ark-completion.bash")),
		bashTarget("sh(bash)", completionPath("ark-completion.sh")),
		zshTarget("zsh", "standalone", completionPath("zsh", "_ark")),
		zshTarget("sh(zsh)", "combined", completionPath("ark-completion.sh")),
		fishTarget("fish", completionPath("fish", "ark.fish")),
	}
}
