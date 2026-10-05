package completion_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/instruction"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/setup"
)

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func setOf(in []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range in {
		m[s] = true
	}
	return m
}

func emptyDir(t *testing.T) string { return t.TempDir() }

// nonOption drops flag-looking candidates and file/dir markers.
func nonOption(in []string) []string {
	var out []string
	for _, s := range in {
		if !strings.HasPrefix(s, "-") && !strings.HasPrefix(s, "<") {
			out = append(out, s)
		}
	}
	return out
}

// --- command tree -----------------------------------------------------------

// ark <TAB> offers exactly the subcommands the dispatcher routes.
func TestSubcommandsMatchDispatcher(t *testing.T) {
	want := sorted(cliSubcommands(t))
	for _, tg := range allTargets() {
		t.Run(tg.name, func(t *testing.T) {
			got := sorted(nonOption(tg.complete(t, emptyDir(t), "")))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ark <TAB> = %v, want the dispatcher's subcommands %v", got, want)
			}
		})
	}
}

// ark skill <TAB> offers exactly the skill sub-subcommands the dispatcher routes.
func TestSkillSubcommandsMatchDispatcher(t *testing.T) {
	want := sorted(cliSkillSubcommands(t))
	for _, tg := range allTargets() {
		t.Run(tg.name, func(t *testing.T) {
			got := sorted(nonOption(tg.complete(t, emptyDir(t), "skill", "")))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ark skill <TAB> = %v, want %v", got, want)
			}
		})
	}
}

// --- setup clients ------------------------------------------------------------

// ark setup <TAB> offers every registered client, in registry order.
func TestSetupClientsMatchRegistry(t *testing.T) {
	want := setup.SupportedClientStrings()
	if len(want) == 0 {
		t.Fatal("empty client registry")
	}
	for _, tg := range allTargets() {
		t.Run(tg.name, func(t *testing.T) {
			got := nonOption(tg.complete(t, emptyDir(t), "setup", ""))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ark setup <TAB> = %v, want registry order %v", got, want)
			}
			// A prefix narrows without inventing clients.
			for _, id := range want {
				got := nonOption(tg.complete(t, emptyDir(t), "setup", id[:1]))
				for _, g := range got {
					if !strings.HasPrefix(g, id[:1]) || !contains(want, g) {
						t.Errorf("ark setup %q <TAB> offered %q", id[:1], g)
					}
				}
				if !contains(got, id) {
					t.Errorf("ark setup %q <TAB> misses %q: %v", id[:1], id, got)
				}
			}
		})
	}
}

// The parser requires the client first, so once a client or any flag is
// present, clients are no longer offered.
func TestSetupClientOnlyAsFirstOperand(t *testing.T) {
	clients := setOf(setup.SupportedClientStrings())
	for _, tg := range allTargets() {
		t.Run(tg.name, func(t *testing.T) {
			for _, words := range [][]string{
				{"setup", "cursor", ""},
				{"setup", "--global", ""},
				{"setup", "-g", ""},
			} {
				for _, c := range tg.complete(t, emptyDir(t), words...) {
					if clients[c] {
						t.Errorf("ark %v offered client %q after the first operand", words, c)
					}
				}
			}
		})
	}
}

// --- instruction targets ----------------------------------------------------------

// ark instruction <TAB> offers exactly the instruction targets — a separate
// registry from the setup clients, which must never leak into it.
func TestInstructionTargetsMatchRegistry(t *testing.T) {
	want := instruction.Targets()
	setupOnly := map[string]bool{}
	for _, id := range setup.SupportedClientStrings() {
		if !contains(want, id) {
			setupOnly[id] = true
		}
	}
	if len(setupOnly) == 0 {
		t.Fatal("fixture assumption: some setup clients are not instruction targets")
	}
	for _, tg := range allTargets() {
		t.Run(tg.name, func(t *testing.T) {
			got := nonOption(tg.complete(t, emptyDir(t), "instruction", ""))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ark instruction <TAB> = %v, want %v", got, want)
			}
			for _, g := range got {
				if setupOnly[g] {
					t.Errorf("setup client %q offered as an instruction target", g)
				}
			}
			// Not offered once the target is given.
			for _, g := range tg.complete(t, emptyDir(t), "instruction", "claude", "") {
				if contains(want, g) {
					t.Errorf("target offered after the first operand: %q", g)
				}
			}
		})
	}
}

// --- flags ----------------------------------------------------------------------

// ark <command> --<TAB> offers exactly the flags the command's real FlagSet
// declares (long flags everywhere; short flags where the shell offers them).
func TestFlagsMatchParsers(t *testing.T) {
	for _, cmd := range cliCommands(t) {
		long, short := flagNames(cmd.fs)
		for _, tg := range allTargets() {
			t.Run(cmd.name+"/"+tg.name, func(t *testing.T) {
				dir := emptyDir(t)
				words := append(append([]string{}, cmd.path...), "--")
				gotLong := sorted(unique(tg.complete(t, dir, words...)))
				if !reflect.DeepEqual(gotLong, long) {
					t.Errorf("long flags drift\n got  %v\n want %v", gotLong, long)
				}
				if tg.shortFlags {
					words := append(append([]string{}, cmd.path...), "-")
					got := sorted(unique(tg.complete(t, dir, words...)))
					want := sorted(append(append([]string{}, long...), short...))
					if !reflect.DeepEqual(got, want) {
						t.Errorf("flag drift (long+short)\n got  %v\n want %v", got, want)
					}
				}
			})
		}
	}
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// --- finite flag values ------------------------------------------------------------

type finiteValue struct {
	cmd      []string
	flag     string
	required []string             // must be offered
	accepts  func(v string) error // every offered value must be accepted by the real parser
	exact    bool                 // offered set must equal required
	probes   []string             // extra values the parser must reject if offered
}

func finiteValues(t *testing.T) []finiteValue {
	general := func(flag string) func(string) error {
		return func(v string) error { _, _, err := commandline.GeneralOptParse([]string{flag, v}); return err }
	}
	serve := func(flag string) func(string) error {
		return func(v string) error { _, _, err := commandline.ServerOptParse("test", []string{flag, v}); return err }
	}
	syntaxF := func(v string) error { _, _, err := commandline.SyntaxOptParse([]string{"--format", v}); return err }
	symbolF := func(v string) error { _, _, err := commandline.SymbolOptParse([]string{"--format", v}); return err }

	var out []finiteValue
	onOff := []string{"on", "off"}
	for _, f := range []string{"--mask-secrets", "--allow-gitignore", "--with-line-number", "--ignore-dotfile"} {
		out = append(out, finiteValue{cmd: nil, flag: f, required: onOff, accepts: general(f)})
	}
	for _, f := range []string{"--mask-secrets", "--allow-gitignore", "--ignore-dotfile"} {
		out = append(out, finiteValue{cmd: []string{"mcp-server"}, flag: f, required: onOff, accepts: serve(f)})
	}
	out = append(out,
		finiteValue{cmd: nil, flag: "--output-format", required: []string{"txt", "md", "xml", "arklite", "auto"}, exact: true, accepts: general("--output-format")},
		finiteValue{cmd: []string{"mcp-server"}, flag: "--type", required: []string{"stdio", "http"}, exact: true, accepts: serve("--type"), probes: []string{"sse", "tcp"}},
		finiteValue{cmd: []string{"syntax"}, flag: "--format", required: []string{"text", "json"}, exact: true, accepts: syntaxF, probes: []string{"xml", "md"}},
		finiteValue{cmd: []string{"symbol"}, flag: "--format", required: []string{"text", "json"}, exact: true, accepts: symbolF, probes: []string{"xml", "md"}},
	)
	// --lang is validated downstream against the language registry, which is
	// therefore the contract (order included).
	var langs []string
	for _, l := range languages.Registry().Languages() {
		langs = append(langs, string(l))
	}
	for _, c := range []string{"syntax", "symbol"} {
		out = append(out, finiteValue{cmd: []string{c}, flag: "--lang", required: langs, exact: true})
	}
	return out
}

// accepted runs the real parser on one value. The parser panics (instead of
// returning an error) for some invalid on/off values — a pre-existing CLI issue
// outside the completion scope — so a panic counts as "rejected".
func accepted(f func(string) error, v string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("parser panic: %v", r)
		}
	}()
	return f(v)
}

func TestFiniteFlagValuesMatchCLI(t *testing.T) {
	for _, fv := range finiteValues(t) {
		for _, tg := range allTargets() {
			name := strings.Join(append(append([]string{}, fv.cmd...), fv.flag), " ") + "/" + tg.name
			t.Run(name, func(t *testing.T) {
				words := append(append([]string{}, fv.cmd...), fv.flag, "")
				got := tg.complete(t, emptyDir(t), words...)
				if fv.exact {
					if !reflect.DeepEqual(sorted(got), sorted(fv.required)) {
						t.Errorf("values = %v, want %v", got, fv.required)
					}
				} else {
					for _, r := range fv.required {
						if !contains(got, r) {
							t.Errorf("values %v miss %q", got, r)
						}
					}
				}
				if fv.accepts != nil {
					for _, v := range got {
						if err := accepted(fv.accepts, v); err != nil {
							t.Errorf("offered value %q is rejected by the CLI: %v", v, err)
						}
					}
					for _, p := range fv.probes {
						if accepted(fv.accepts, p) == nil {
							t.Errorf("probe %q unexpectedly accepted: test is not discriminating", p)
						}
					}
				}
			})
		}
	}
}

// --lang follows the language registry, including its order.
func TestLangValuesFollowRegistryOrder(t *testing.T) {
	var want []string
	for _, l := range languages.Registry().Languages() {
		want = append(want, string(l))
	}
	for _, tg := range []target{allTargets()[0], allTargets()[1], allTargets()[2], allTargets()[3]} {
		t.Run(tg.name, func(t *testing.T) {
			got := tg.complete(t, emptyDir(t), "syntax", "--lang", "")
			if !reflect.DeepEqual(got, want) {
				t.Errorf("--lang = %v, want registry order %v", got, want)
			}
		})
	}
}

// --- CLI help text ------------------------------------------------------------------

// The CLI's own flag help and help.txt must name every supported language and
// client, so help, completion and registries cannot disagree.
func TestHelpTextMatchesRegistries(t *testing.T) {
	help := string(readFile(t, filepath.Join("..", "commandline", "help.txt")))
	for _, id := range setup.SupportedClientStrings() {
		if !strings.Contains(help, id) {
			t.Errorf("help.txt does not mention client %q", id)
		}
	}
	for _, sub := range cliSubcommands(t) {
		if !strings.Contains(help, sub) {
			t.Errorf("help.txt does not mention subcommand %q", sub)
		}
	}
	for _, l := range languages.Registry().Languages() {
		if !strings.Contains(help, string(l)) {
			t.Errorf("help.txt does not mention language %q", l)
		}
	}
	for _, cmd := range cliCommands(t) {
		if f := cmd.fs.Lookup("lang"); f != nil {
			for _, l := range languages.Registry().Languages() {
				if !strings.Contains(f.Usage, string(l)) {
					t.Errorf("%s --lang usage %q does not mention %q", cmd.name, f.Usage, l)
				}
			}
		}
	}
}

// --- operands (files / directories) ---------------------------------------------------

func TestOperandCompletion(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"srcdir"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}
	subs := setOf(cliSubcommands(t))

	for _, tg := range allTargets()[:4] { // fish completes files through its default path rules
		t.Run(tg.name, func(t *testing.T) {
			// First word: subcommands and the default command's directory operand.
			got := tg.complete(t, dir, "")
			if !contains(got, "srcdir") && !contains(got, "<dirs>") {
				t.Errorf("ark <TAB> offers no directory operand: %v", got)
			}
			if contains(got, "a.go") || contains(got, "<files>") {
				t.Errorf("ark <TAB> must offer directories, not files: %v", got)
			}
			// After a general flag a subcommand is no longer valid (argv[1] only).
			after := tg.complete(t, dir, "-c", "")
			for _, c := range after {
				if subs[c] {
					t.Errorf("ark -c <TAB> offered subcommand %q", c)
				}
			}
			if !contains(after, "srcdir") && !contains(after, "<dirs>") {
				t.Errorf("ark -c <TAB> offers no directory operand: %v", after)
			}
			// syntax / symbol take a file operand.
			for _, c := range []string{"syntax", "symbol"} {
				got := tg.complete(t, dir, c, "")
				if !contains(got, "a.go") && !contains(got, "<files>") {
					t.Errorf("ark %s <TAB> offers no file operand: %v", c, got)
				}
			}
			// Commands without an operand offer no files or directories.
			for _, c := range []string{"mcp-init"} {
				for _, g := range tg.complete(t, dir, c, "") {
					if g == "a.go" || g == "srcdir" || strings.HasPrefix(g, "<") {
						t.Errorf("ark %s <TAB> must not offer files: %v", c, g)
					}
				}
			}
		})
	}
}

// --- determinism -----------------------------------------------------------------------

func TestCandidateOrderIsDeterministic(t *testing.T) {
	queries := [][]string{
		{""}, {"setup", ""}, {"skill", ""}, {"--"}, {"setup", "--"}, {"syntax", "--lang", ""},
		{"mcp-server", "--type", ""}, {"skill", "update", "--"}, {"instruction", ""}, {"instruction", "--"},
	}
	for _, tg := range allTargets() {
		t.Run(tg.name, func(t *testing.T) {
			dir := emptyDir(t)
			for _, q := range queries {
				first := tg.complete(t, dir, q...)
				for i := 0; i < 3; i++ {
					if again := tg.complete(t, dir, q...); !reflect.DeepEqual(first, again) {
						t.Fatalf("ark %v: order changed between runs:\n%v\n%v", q, first, again)
					}
				}
			}
		})
	}
}

// --- escaping / syntax --------------------------------------------------------------------

var safeWord = regexp.MustCompile(`^(--?)?[A-Za-z0-9][A-Za-z0-9._-]*$|^<(files|dirs)>$`)

// Every candidate is a plain token: no whitespace, quotes or shell
// metacharacters can leak out of the scripts' lists.
func TestCandidatesAreSafeTokens(t *testing.T) {
	queries := [][]string{
		{""}, {"-"}, {"--"}, {"setup", ""}, {"setup", "-"}, {"skill", ""}, {"skill", "init", "-"},
		{"mcp-server", "-"}, {"mcp-init", "-"}, {"syntax", "-"}, {"symbol", "--lang", ""},
		{"-f", ""}, {"--output-format", ""}, {"instruction", ""}, {"instruction", "-"},
	}
	for _, tg := range allTargets() {
		t.Run(tg.name, func(t *testing.T) {
			dir := emptyDir(t)
			for _, q := range queries {
				for _, c := range tg.complete(t, dir, q...) {
					if !safeWord.MatchString(c) {
						t.Errorf("ark %v offered unsafe candidate %q", q, c)
					}
				}
			}
		})
	}
}

// Scripts must at least parse in the shells they claim to support.
func TestScriptsParse(t *testing.T) {
	for _, tc := range []struct {
		shell string
		args  []string
	}{
		{"bash", []string{"-n", completionPath("bash", "ark-completion.bash")}},
		{"bash", []string{"-n", completionPath("ark-completion.sh")}},
		{"zsh", []string{"-n", completionPath("zsh", "_ark")}},
		{"zsh", []string{"-n", completionPath("ark-completion.sh")}},
	} {
		t.Run(tc.shell+" "+filepath.Base(tc.args[1]), func(t *testing.T) {
			trackInput(t, tc.args[1])
			run(t, ".", requireTool(t, tc.shell), tc.args...)
		})
	}
}

// fish cannot be required in CI; check what can be checked statically: quotes
// balance on every line, block keywords balance, and every `complete` line
// parses to an entry.
func TestFishStaticWellFormed(t *testing.T) {
	path := completionPath("fish", "ark.fish")
	text := strings.ReplaceAll(string(readFile(t, path)), "\\\n", " ")
	depth := 0
	for i, l := range strings.Split(text, "\n") {
		code := strings.TrimSpace(l)
		if strings.HasPrefix(code, "#") || code == "" {
			continue
		}
		if strings.Count(code, "'")%2 != 0 && !strings.Contains(code, `"`) {
			t.Errorf("line %d: unbalanced single quotes: %s", i+1, code)
		}
		if strings.Count(code, `"`)%2 != 0 {
			t.Errorf("line %d: unbalanced double quotes: %s", i+1, code)
		}
		first := strings.Fields(code)[0]
		switch first {
		case "function", "for", "if", "while", "switch", "begin":
			depth++
		case "end":
			depth--
		}
	}
	if depth != 0 {
		t.Errorf("unbalanced block keywords (function/for/if/end): depth %d", depth)
	}
	entries := parseFish(t, path)
	n := strings.Count(text, "\ncomplete -c ark") + strings.Count(text, "\n    complete -c ark")
	if len(entries) < n {
		t.Errorf("parsed %d complete entries, file has at least %d", len(entries), n)
	}
}
