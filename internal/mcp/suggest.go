package mcp

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed claudemd_snippet.md
var claudeMdSnippet string

// SuggestCLAUDEMd checks if CLAUDE.md already has Ark MCP instructions.
// If not, it prints a suggestion to add them. No files are written.
func SuggestCLAUDEMd(rootDir string) {
	path := filepath.Join(rootDir, "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err == nil && strings.Contains(string(data), "mcp__ark__") {
		return
	}

	fmt.Println()
	if os.IsNotExist(err) {
		fmt.Println("ℹ  CLAUDE.md not found. Create one with:")
	} else {
		fmt.Println("ℹ  CLAUDE.md: no Ark MCP instructions found. Add this:")
	}
	fmt.Println()
	fmt.Println("   ---  CLAUDE.md  ---")
	for _, line := range strings.Split(claudeMdSnippet, "\n") {
		fmt.Printf("   %s\n", line)
	}
	fmt.Println("   --------------------")
}
