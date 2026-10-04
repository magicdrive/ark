package setup

import (
	"fmt"
	"os"
	"path/filepath"
)

// newCursorAdapter configures Ark for Cursor (plan §14 Cursor).
//
//	project: <root>/.cursor/mcp.json
//	global:  ~/.cursor/mcp.json
//
// Cursor stores stdio servers as {command, args, env}. Ark manages only its own
// entry; unknown fields and other servers are preserved by jsonConfig.
func newCursorAdapter() adapter {
	return jsonClient{
		id:         ClientCursor,
		serverName: "ark",
		managedBy:  "config file",
		resolvePath: func(opts Options) (string, error) {
			if opts.Global {
				home, err := os.UserHomeDir()
				if err != nil {
					return "", fmt.Errorf("cannot determine home directory: %w", err)
				}
				return filepath.Join(home, ".cursor", "mcp.json"), nil
			}
			return filepath.Join(opts.RootDir, ".cursor", "mcp.json"), nil
		},
		buildEntry: func(opts Options) map[string]any {
			return stdioEntry(opts.ArkPath, []string{"mcp-server", "--root", opts.RootDir})
		},
	}
}

// stdioEntry builds a minimal stdio MCP server entry used by non-Claude JSON
// clients. command/args are kept as structured values, never a shell string
// (plan §34).
func stdioEntry(command string, args []string) map[string]any {
	return map[string]any{
		"command": command,
		"args":    args,
		"env":     map[string]any{},
	}
}
