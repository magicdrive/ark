package setup

import (
	"errors"
	"path/filepath"
)

// copilotServersKey is the top-level server map of VS Code's own MCP schema
// (.vscode/mcp.json). It differs from the "mcpServers" key of the portable
// .mcp.json format and is supplied to the shared JSON layer as plain data.
const copilotServersKey = "servers"

// errCopilotGlobalUnsupported is returned for `ark setup copilot --global`.
var errCopilotGlobalUnsupported = errors.New(
	"global setup is not supported for copilot: Ark cannot determine VS Code's user-level " +
		"MCP configuration path (it is profile- and remote-dependent and not officially documented); " +
		"run `ark setup copilot` inside the repository to configure .vscode/mcp.json")

// newCopilotAdapter configures Ark for GitHub Copilot Chat / Agent mode in
// VS Code. Scope is deliberately narrow:
//
//	project: <root>/.vscode/mcp.json   (top-level "servers", stdio entry)
//
// Explicitly NOT managed here: the Copilot CLI, the GitHub-hosted Copilot
// agent / GitHub repository settings, VS Code user-level (global) MCP
// configuration, and the workspace's portable .mcp.json — that file belongs to
// the Claude Code adapter and is never read or written by this one.
//
// Entry schema (VS Code MCP configuration reference, stdio server): type
// "stdio" (required), command (required), args, env. Ark manages only its own
// entry; other servers, "inputs" and unknown fields are preserved by jsonConfig.
func newCopilotAdapter() adapter {
	return jsonClient{
		id:         ClientCopilot,
		serversKey: copilotServersKey,
		serverName: "ark",
		managedBy:  "config file",
		resolvePath: func(opts Options) (string, error) {
			if opts.Global {
				// Rejected before any filesystem access.
				return "", errCopilotGlobalUnsupported
			}
			return filepath.Join(opts.RootDir, ".vscode", "mcp.json"), nil
		},
		buildEntry: func(opts Options) map[string]any {
			entry := stdioEntry(opts.ArkPath, []string{"mcp-server", "--root", opts.RootDir})
			entry["type"] = "stdio"
			return entry
		},
	}
}
