package instruction_test

import (
	"flag"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/instruction"
	"github.com/magicdrive/ark/internal/mcp"
	"github.com/magicdrive/ark/internal/setup"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func render(t *testing.T, target string) string {
	t.Helper()
	out, err := instruction.Render(target)
	if err != nil {
		t.Fatalf("Render(%q): %v", target, err)
	}
	return out
}

// --- target registry --------------------------------------------------------

var wantTargets = []string{"claude", "codex", "cursor", "cline", "copilot-vscode", "copilot-cli"}

func TestTargets_SixSupported(t *testing.T) {
	if got := instruction.Targets(); !reflect.DeepEqual(got, wantTargets) {
		t.Fatalf("Targets() = %v, want %v (in this order)", got, wantTargets)
	}
	for _, name := range wantTargets {
		if _, err := instruction.Render(name); err != nil {
			t.Errorf("%s must be valid: %v", name, err)
		}
	}
}

// Every other name — including plausible but unsupported spellings — is
// unsupported, with the supported targets listed in the error.
func TestRender_UnknownTargets(t *testing.T) {
	bad := []string{"copilot", "agents", "cursor-rules", "claude-code", "openai", "github-copilot",
		"vscode", "unknown", "", "Claude", "claude "}
	for _, name := range bad {
		out, err := instruction.Render(name)
		if err == nil || out != "" {
			t.Errorf("Render(%q) = %q, %v; want an error and no output", name, out, err)
			continue
		}
		if !strings.Contains(err.Error(), "supported targets: "+strings.Join(wantTargets, ", ")) {
			t.Errorf("Render(%q) error does not list the supported targets: %v", name, err)
		}
	}
}

// `agents` and `copilot` are deliberately not targets (nor aliases of one):
// AGENTS.md is a destination some targets share, not an agent identity, and
// `copilot` is ambiguous between copilot-vscode and copilot-cli.
func TestRender_NoAgentsOrCopilotAlias(t *testing.T) {
	for _, name := range []string{"agents", "copilot"} {
		if _, err := instruction.Render(name); err == nil {
			t.Errorf("%q must remain unsupported", name)
		}
	}
}

// Instruction targets and setup clients are independent registries: this
// package never imports internal/setup, so Targets() cannot be derived from
// (or drift automatically with) the setup client registry. That today's two
// registries happen to name the same six agents is coincidence, not an
// architectural equivalence — each can grow or shrink on its own.
func TestInstructionPackageDoesNotImportSetup(t *testing.T) {
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if path == "github.com/magicdrive/ark/internal/setup" {
				t.Errorf("%s imports internal/setup; instruction targets must not be derived from setup clients", name)
			}
		}
	}
}

// Confirms the fixture assumption behind TestInstructionPackageDoesNotImportSetup:
// the two registries' name sets are equal today (by coincidence, per the
// package doc), so a naive "disjoint sets" test would be the wrong contract.
func TestTargetsCurrentlyMatchSetupClientsByCoincidence(t *testing.T) {
	if got := sortedStrings(instruction.Targets()); !reflect.DeepEqual(got, sortedStrings(setup.SupportedClientStrings())) {
		t.Fatalf("instruction.Targets() = %v, setup.SupportedClientStrings() = %v; "+
			"update this fixture (and reread the package doc) if they are meant to diverge",
			got, setup.SupportedClientStrings())
	}
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// --- output -------------------------------------------------------------------

func goldenPath() string { return filepath.Join("testdata", "claude.golden.md") }

// The Claude output is pinned byte for byte. Changing the guidance means
// reviewing and updating the golden (`go test ./internal/instruction -update`).
func TestClaudeOutput_Golden(t *testing.T) {
	got := render(t, "claude")
	if *update {
		if err := os.WriteFile(goldenPath(), []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath())
	if err != nil {
		t.Fatalf("%v (run with -update to create)", err)
	}
	if got != string(want) {
		t.Errorf("claude instruction differs from the golden\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestClaudeOutput_PureDeterministicMarkdown(t *testing.T) {
	first := render(t, "claude")
	for i := 0; i < 50; i++ {
		if render(t, "claude") != first {
			t.Fatal("output is not deterministic")
		}
	}
	if !strings.HasPrefix(first, "## Ark Code Intelligence\n") {
		t.Errorf("must start with the section heading:\n%s", first)
	}
	if !strings.HasSuffix(first, "\n") || strings.HasSuffix(first, "\n\n") {
		t.Error("must end with exactly one newline")
	}
	// No success messages, ANSI, emoji or status glyphs: instruction text only.
	if regexp.MustCompile("\x1b|[ℹ✓✔⚠\U0001F300-\U0001FAFF]").MatchString(first) {
		t.Error("output contains control sequences or status glyphs")
	}
	for _, noise := range []string{"Setting up", "restart", "Restart", "Error", "Warning"} {
		if strings.Contains(first, noise) {
			t.Errorf("output contains non-instruction text %q", noise)
		}
	}
}

// The current model: repository map first, context for symbols, no mandatory
// tree-first ritual, no absolute whole-file ban, no unconditional tool calls.
func TestGuidance_CurrentModelNotObsoleteWorkflow(t *testing.T) {
	g := instruction.Guidance()
	for _, tool := range []string{"get_repository_map", "get_context", "find_symbol", "get_symbol", "get_symbols",
		"get_relations", "get_callers", "get_callees", "analyze_change_impact", "search_code", "search_in_files",
		"get_file_content", "get_directory_tree"} {
		if !strings.Contains(g, "`"+tool+"`") {
			t.Errorf("guidance does not mention %s", tool)
		}
	}
	for _, obsolete := range []string{"NEVER", "ALWAYS", "Always", "First step", "last resort", "get_directory_tree   →", "must call", "every change", "Every change"} {
		if strings.Contains(g, obsolete) {
			t.Errorf("guidance contains the absolute/obsolete phrasing %q", obsolete)
		}
	}
	if strings.Index(g, "`get_repository_map`") > strings.Index(g, "`get_directory_tree`") {
		t.Error("get_repository_map must be recommended before get_directory_tree")
	}
	if !strings.Contains(g, "uncertainty") || !strings.Contains(g, "instead of guessing") {
		t.Error("the uncertainty rule is missing")
	}
	// Internal confidence levels stay internal ("exact" as plain English is fine).
	for _, internal := range []string{"Candidate", "Strong", "Unresolved", "ConfidenceExact", "confidence=exact"} {
		if strings.Contains(g, internal) {
			t.Errorf("internal confidence term %q leaks into the guidance", internal)
		}
	}
	if n := len(strings.Split(strings.TrimSpace(g), "\n")); n > 20 {
		t.Errorf("guidance has %d lines; keep it short", n)
	}
}

// --- vendor neutrality ------------------------------------------------------------

// The canonical guidance carries no agent-specific meaning: all of that is the
// target's presentation.
func TestGuidance_IsVendorNeutral(t *testing.T) {
	g := instruction.Guidance()
	for _, word := range []string{"Claude", "CLAUDE.md", "Anthropic", "mcp__"} {
		if strings.Contains(g, word) {
			t.Errorf("canonical guidance contains the agent-specific term %q", word)
		}
	}
	for _, tool := range instruction.GuidanceTools() {
		if strings.HasPrefix(tool, "mcp__") {
			t.Errorf("canonical guidance must use bare MCP tool names, found %q", tool)
		}
	}
}

// The Claude target changes presentation only: it names each tool the way
// Claude Code does, and otherwise leaves the guidance untouched.
func TestClaudeTarget_OnlyPresentationDiffers(t *testing.T) {
	out := render(t, "claude")
	if strings.Contains(out, "Claude") || strings.Contains(out, "CLAUDE.md") {
		t.Error("the Claude target should not add agent prose to the guidance")
	}
	for _, tool := range instruction.GuidanceTools() {
		if !strings.Contains(out, "`mcp__ark__"+tool+"`") {
			t.Errorf("tool %s is not presented as mcp__ark__%s", tool, tool)
		}
		if strings.Contains(out, "`"+tool+"`") {
			t.Errorf("bare tool name %s left in the Claude output", tool)
		}
	}
	// Undoing the prefix recovers the canonical text exactly.
	if back := strings.ReplaceAll(out, "mcp__ark__", ""); back != instruction.Guidance() {
		t.Error("the Claude output differs from the guidance by more than tool naming")
	}
}

// --- plain targets (codex, cursor, cline, copilot-vscode, copilot-cli) --------------

var plainTargets = []string{"codex", "cursor", "cline", "copilot-vscode", "copilot-cli"}

// No agent other than Claude has an officially documented, model-visible MCP
// tool-naming convention, so every other target renders the canonical
// guidance byte-identical to Guidance(): no synthetic prefix, no qualified
// name, nothing invented.
func TestPlainTargets_ByteIdenticalToGuidance(t *testing.T) {
	want := instruction.Guidance()
	for _, name := range plainTargets {
		if got := render(t, name); got != want {
			t.Errorf("Render(%q) differs from Guidance():\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
		}
	}
}

// The five plain targets necessarily produce byte-identical output to each
// other too (same input, same identity renderer).
func TestPlainTargets_ByteIdenticalToEachOther(t *testing.T) {
	first := render(t, plainTargets[0])
	for _, name := range plainTargets[1:] {
		if got := render(t, name); got != first {
			t.Errorf("Render(%q) differs from Render(%q)", name, plainTargets[0])
		}
	}
}

// --- renderer mapping ---------------------------------------------------------------

// claude is the only target with a dedicated renderer; every other target
// uses the shared plain renderer. This is checked by behavior (does the
// output carry the mcp__ark__ prefix?), not by reaching into the registry.
func TestRendererMapping(t *testing.T) {
	if out := render(t, "claude"); !strings.Contains(out, "mcp__ark__") {
		t.Error("claude must use the Claude renderer (mcp__ark__ prefix)")
	}
	for _, name := range plainTargets {
		if out := render(t, name); strings.Contains(out, "mcp__ark__") {
			t.Errorf("%s must use the plain renderer, not Claude's mcp__ark__ prefix", name)
		}
	}
}

// --- destinations ---------------------------------------------------------------------

func TestDestinations(t *testing.T) {
	want := map[string]string{
		"claude":         "CLAUDE.md",
		"codex":          "AGENTS.md",
		"cursor":         "AGENTS.md",
		"cline":          ".clinerules/ark.md",
		"copilot-vscode": ".github/copilot-instructions.md",
		"copilot-cli":    ".github/copilot-instructions.md",
	}
	for name, dest := range want {
		got, err := instruction.Destination(name)
		if err != nil {
			t.Errorf("Destination(%q): %v", name, err)
			continue
		}
		if got != dest {
			t.Errorf("Destination(%q) = %q, want %q", name, got, dest)
		}
	}
	for _, name := range instruction.Targets() {
		if _, ok := want[name]; !ok {
			t.Errorf("target %q has no expected destination in this test; update the fixture", name)
		}
	}
	if _, err := instruction.Destination("unknown"); err == nil {
		t.Error("Destination(\"unknown\") must error")
	}
}

// --- MCP tool inventory --------------------------------------------------------------

// Every tool the guidance names exists in the current MCP server, so the text
// can never keep recommending a removed or renamed tool.
func TestGuidanceToolsExistInMCPServer(t *testing.T) {
	registered := map[string]bool{}
	for _, tool := range mcp.NewToolsHandler(".", nil).ListTools() {
		registered[tool.Name] = true
	}
	if len(registered) == 0 {
		t.Fatal("no MCP tools registered")
	}
	names := instruction.GuidanceTools()
	if len(names) < 10 {
		t.Fatalf("guidance mentions only %d tools: %v", len(names), names)
	}
	for _, name := range names {
		if !registered[name] {
			t.Errorf("guidance recommends %q, which the MCP server does not register", name)
		}
	}
}

// --- misc/CLAUDE.md.example ---------------------------------------------------------------

// The example file's Ark section is exactly what `ark instruction claude`
// prints (the project-specific placeholder around it is the example's own).
func TestExampleFileContainsCanonicalClaudeInstruction(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "misc", "CLAUDE.md.example"))
	if err != nil {
		t.Fatal(err)
	}
	if want := render(t, "claude"); !strings.Contains(string(data), want) {
		t.Errorf("misc/CLAUDE.md.example does not contain the canonical Claude instruction; regenerate its Ark section with `ark instruction claude`")
	}
	for _, stale := range []string{"First step", "last resort", "NEVER read entire files", "### Workflow"} {
		if strings.Contains(string(data), stale) {
			t.Errorf("misc/CLAUDE.md.example still has the obsolete %q", stale)
		}
	}
}
