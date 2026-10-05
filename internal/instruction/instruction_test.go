package instruction_test

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
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

func TestTargets_ClaudeOnly(t *testing.T) {
	if got := strings.Join(instruction.Targets(), ","); got != "claude" {
		t.Fatalf("Targets() = %q, want only claude", got)
	}
	if _, err := instruction.Render("claude"); err != nil {
		t.Fatalf("claude must be valid: %v", err)
	}
}

// Every other agent — including all other setup clients — is unsupported, with
// the supported targets listed in the error.
func TestRender_UnknownTargets(t *testing.T) {
	bad := []string{"cursor", "codex", "cline", "copilot-vscode", "copilot-cli", "copilot", "agents", "unknown", "", "Claude", "claude "}
	for _, name := range bad {
		out, err := instruction.Render(name)
		if err == nil || out != "" {
			t.Errorf("Render(%q) = %q, %v; want an error and no output", name, out, err)
			continue
		}
		if !strings.Contains(err.Error(), "supported targets: claude") {
			t.Errorf("Render(%q) error does not list the supported targets: %v", name, err)
		}
	}
}

// Instruction targets and setup clients are different registries: only one
// name is shared today, by coincidence — adding a setup client adds no target.
func TestTargetsAreIndependentOfSetupClients(t *testing.T) {
	targets := map[string]bool{}
	for _, name := range instruction.Targets() {
		targets[name] = true
	}
	var setupOnly int
	for _, id := range setup.SupportedClientStrings() {
		if !targets[id] {
			setupOnly++
			if _, err := instruction.Render(id); err == nil {
				t.Errorf("setup client %q became an instruction target", id)
			}
		}
	}
	if setupOnly == 0 {
		t.Fatal("fixture assumption: setup clients that are not instruction targets exist")
	}
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
