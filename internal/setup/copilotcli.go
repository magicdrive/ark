package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// errCopilotCLIGlobalUnsupported is returned for `ark setup copilot-cli --global`.
var errCopilotCLIGlobalUnsupported = errors.New(
	"global setup is not supported for copilot-cli: Ark does not manage the user-level Copilot CLI " +
		"configuration (~/.copilot/mcp-config.json); run `ark setup copilot-cli` inside the repository " +
		"to configure .github/mcp.json")

// copilotCLIAdapter configures Ark for the GitHub Copilot CLI.
//
//	project: <root>/.github/mcp.json   (top-level "mcpServers", local entry)
//
// It is a different client from `copilot-vscode` (.vscode/mcp.json) and
// shares no file with it. Explicitly NOT managed: the user-level
// ~/.copilot/mcp-config.json, the `copilot mcp` CLI (its scope and
// existing-name behavior are undocumented), the GitHub-hosted agent and GitHub
// repository settings. <root>/.mcp.json is read — never written — to detect a
// higher-precedence conflict (see checkRootMcpJSONPrecedence).
//
// Entry schema follows GitHub's documented example for a local server:
// type "local", command, args, env, tools ["*"].
type copilotCLIAdapter struct {
	base jsonClient
}

func newCopilotCLIAdapter() adapter {
	return copilotCLIAdapter{
		base: jsonClient{
			id:         ClientCopilotCLI,
			serverName: "ark",
			managedBy:  "config file",
			resolvePath: func(opts Options) (string, error) {
				if opts.Global {
					return "", errCopilotCLIGlobalUnsupported
				}
				return filepath.Join(opts.RootDir, ".github", "mcp.json"), nil
			},
			buildEntry: func(opts Options) map[string]any {
				entry := stdioEntry(opts.ArkPath, []string{"mcp-server", "--root", opts.RootDir})
				entry["type"] = "local"
				entry["tools"] = []any{"*"}
				return entry
			},
		},
	}
}

func (a copilotCLIAdapter) run(opts Options) (*Result, error) {
	if opts.Global {
		return nil, errCopilotCLIGlobalUnsupported // before any filesystem access
	}
	warning, err := checkRootMcpJSONPrecedence(opts.RootDir, a.base.serverName)
	if err != nil {
		return nil, err
	}
	res, err := a.base.applyJSON(opts)
	if err != nil {
		return nil, err
	}
	if warning != "" {
		res.Warnings = append(res.Warnings, warning)
	}
	return res, nil
}

// checkRootMcpJSONPrecedence guards against a higher-precedence definition.
//
// GitHub's Copilot CLI documentation gives `.mcp.json` precedence over
// `.github/mcp.json` for a server of the same name. If <root>/.mcp.json already
// defines mcpServers.<name>, an entry written to .github/mcp.json would not be
// the one Copilot CLI uses, so setup would "succeed" while the effective
// configuration differs. That is refused — also with --force: it is not a
// conflict in an Ark-owned Copilot CLI entry but an external, higher-precedence
// definition Ark must not override or choose between. <root>/.mcp.json is only
// read here and is never modified.
//
// A .mcp.json that does not exist, does not parse, or has no such entry does not
// block setup (an unparsable file cannot be read by Copilot CLI either; the
// caller surfaces that as a warning). Nested .mcp.json files closer to a future
// working directory cannot be known at setup time (known limitation).
func checkRootMcpJSONPrecedence(root, serverName string) (warning string, err error) {
	path := filepath.Join(root, ".mcp.json")
	info, statErr := os.Stat(path) // follows symlinks: Copilot CLI reads through them too
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return "", nil
		}
		return fmt.Sprintf("could not inspect %s (%v); Ark could not verify that no higher-precedence %q entry exists", path, statErr, serverName), nil
	}
	if !info.Mode().IsRegular() || info.Size() > maxConfigSize {
		return fmt.Sprintf("%s is not a regular file of reasonable size; Ark could not verify that no higher-precedence %q entry exists", path, serverName), nil
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return fmt.Sprintf("could not read %s (%v); Ark could not verify that no higher-precedence %q entry exists", path, readErr, serverName), nil
	}
	var doc map[string]any
	if jsonErr := json.Unmarshal(data, &doc); jsonErr != nil {
		return fmt.Sprintf("%s is not valid JSON; Ark could not verify that no higher-precedence %q entry exists", path, serverName), nil
	}
	servers, _ := doc[mcpServersKey].(map[string]any)
	if _, exists := servers[serverName]; !exists {
		return "", nil
	}
	return "", fmt.Errorf(
		"cannot configure Ark for GitHub Copilot CLI: %s already defines %s.%s, "+
			"and Copilot CLI gives .mcp.json precedence over .github/mcp.json for servers of the same name.\n"+
			"Ark cannot safely decide which entry should be used, so it did not change anything "+
			"(%s and .github/mcp.json are untouched). --force does not override this.\n"+
			"Remove or rename that %q entry if Copilot CLI should use the one Ark writes to .github/mcp.json, then re-run",
		path, mcpServersKey, serverName, path, serverName)
}
