package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/instruction"
)

// advisoryBlock extracts the text between the advisory's CLAUDE.md markers and
// removes the 3-space display indent.
func advisoryBlock(t *testing.T, advisory string) string {
	t.Helper()
	const open, closeMark = "   ---  CLAUDE.md  ---\n", "   --------------------"
	i := strings.Index(advisory, open)
	j := strings.LastIndex(advisory, closeMark)
	if i < 0 || j < i {
		t.Fatalf("advisory has no CLAUDE.md block:\n%s", advisory)
	}
	lines := strings.Split(strings.TrimSuffix(advisory[i+len(open):j], "\n"), "\n")
	for k, l := range lines {
		lines[k] = strings.TrimPrefix(l, "   ")
	}
	return strings.Join(lines, "\n")
}

// The advisory shown after `ark setup claude` / `ark mcp-init` is exactly the
// output of `ark instruction claude` — one canonical source.
func TestCLAUDEMdSuggestion_IsTheCanonicalClaudeInstruction(t *testing.T) {
	want, err := instruction.Render("claude")
	if err != nil {
		t.Fatal(err)
	}
	for name, setup := range map[string]func(dir string){
		"no CLAUDE.md":      func(string) {},
		"CLAUDE.md without": func(dir string) { _ = os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Project\n"), 0o644) },
	} {
		dir := t.TempDir()
		setup(dir)
		got := advisoryBlock(t, CLAUDEMdSuggestion(dir))
		if got != want {
			t.Errorf("%s: advisory text differs from `ark instruction claude`\n--- advisory ---\n%s\n--- canonical ---\n%s", name, got, want)
		}
	}
}

// Pasting the instruction into CLAUDE.md satisfies the advisory's own check, so
// the suggestion stops once the user has followed it.
func TestCLAUDEMdSuggestion_StopsAfterInstructionIsPasted(t *testing.T) {
	dir := t.TempDir()
	text, _ := instruction.Render("claude")
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Project\n\n"+text), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := CLAUDEMdSuggestion(dir); s != "" {
		t.Errorf("advisory still shown after pasting the instruction:\n%s", s)
	}
}

// The advisory never writes anything.
func TestCLAUDEMdSuggestion_WritesNothing(t *testing.T) {
	dir := t.TempDir()
	_ = CLAUDEMdSuggestion(dir)
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("advisory created files: %v", entries)
	}
}
