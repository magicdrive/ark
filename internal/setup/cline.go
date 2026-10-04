package setup

import (
	"fmt"
	"os"
	"path/filepath"
)

// clineAdapter configures Ark for the Cline CLI (plan §14 Cline).
//
// v4.1 officially supports only the Cline CLI configuration at
// ~/.cline/mcp.json. The MCP settings used by Cline's VS Code / Cursor /
// Windsurf extensions (globalStorage/cline_mcp_settings.json) are intentionally
// out of scope: Ark never probes OS/editor-specific storage paths. This
// constraint is documented in README / README_ja.
//
// Cline's CLI config is user-level, so there is no distinct project scope;
// --global is accepted but resolves to the same file. The served repository is
// still conveyed via --root in args.
type clineAdapter struct {
	base jsonClient
}

func newClineAdapter() adapter {
	return clineAdapter{
		base: jsonClient{
			id:         ClientCline,
			serverName: "ark",
			managedBy:  "config file",
			resolvePath: func(opts Options) (string, error) {
				home, err := os.UserHomeDir()
				if err != nil {
					return "", fmt.Errorf("cannot determine home directory: %w", err)
				}
				return filepath.Join(home, ".cline", "mcp.json"), nil
			},
			buildEntry: func(opts Options) map[string]any {
				return stdioEntry(opts.ArkPath, []string{"mcp-server", "--root", opts.RootDir})
			},
		},
	}
}

func (a clineAdapter) run(opts Options) (*Result, error) {
	res, err := a.base.applyJSON(opts)
	if err != nil {
		return nil, err
	}
	// Cline CLI config is user-level; report the scope honestly regardless of
	// the --global flag.
	res.Scope = "user"
	return res, nil
}
