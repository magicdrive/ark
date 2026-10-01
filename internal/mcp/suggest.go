package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const claudeMdSnippet = `## Code Exploration

This project uses Ark MCP tools. Prefer these over Read/Bash for code navigation:

- ` + "`mcp__ark__get_symbols`" + `      — list functions/types in a file (use before Read)
- ` + "`mcp__ark__find_symbol`" + `      — locate a symbol by name across the repo
- ` + "`mcp__ark__get_symbol`" + `       — get exact source of a specific function/type
- ` + "`mcp__ark__search_in_files`" + `  — full-text/regex search across files
- ` + "`mcp__ark__get_file_content`" + ` — read a whole file (last resort only)`

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
