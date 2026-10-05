package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicdrive/ark/internal/instruction"
)

// SuggestCLAUDEMd checks if CLAUDE.md already has Ark MCP instructions.
// If not, it prints a suggestion to add them. No files are written.
func SuggestCLAUDEMd(rootDir string) {
	if s := CLAUDEMdSuggestion(rootDir); s != "" {
		fmt.Println(s)
	}
}

// CLAUDEMdSuggestion returns the CLAUDE.md advisory text for rootDir, or an
// empty string when CLAUDE.md already contains Ark MCP instructions. No files
// are written. Callers that want to print it directly can use SuggestCLAUDEMd.
func CLAUDEMdSuggestion(rootDir string) string {
	path := filepath.Join(rootDir, "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err == nil && strings.Contains(string(data), "mcp__ark__") {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n")
	if os.IsNotExist(err) {
		b.WriteString("ℹ  CLAUDE.md not found. Create one with:\n")
	} else {
		b.WriteString("ℹ  CLAUDE.md: no Ark MCP instructions found. Add this:\n")
	}
	b.WriteString("\n")
	b.WriteString("   ---  CLAUDE.md  ---\n")
	// The advisory shows exactly what `ark instruction claude` prints: one
	// canonical source (internal/instruction), never a second copy of the text.
	snippet, _ := instruction.Render("claude")
	for _, line := range strings.Split(snippet, "\n") {
		b.WriteString("   " + line + "\n")
	}
	b.WriteString("   --------------------")
	return b.String()
}
