package setup

import (
	"fmt"
)

// jsonClient is the shared implementation for clients whose MCP configuration
// is a JSON file with one top-level server-map object: "mcpServers" for Claude,
// Cursor and Cline, or the key another client documents (see serversKey).
//
// Per-client variation is expressed as data/functions, not subclasses
// (plan §8): where the config file lives, and how the desired Ark entry is
// encoded. The safe read → classify → act → atomic-write → verify flow is
// identical and lives in applyJSON.
type jsonClient struct {
	id ClientID
	// serversKey is the top-level server-map key of the client's schema. Empty
	// means the default "mcpServers". It is data supplied by the adapter; this
	// layer has no per-client conditions.
	serversKey  string
	serverName  string
	managedBy   string
	resolvePath func(opts Options) (string, error)
	buildEntry  func(opts Options) map[string]any
}

// applyJSON runs the generic state machine for a JSON MCP config file.
func (c jsonClient) applyJSON(opts Options) (*Result, error) {
	path, err := c.resolvePath(opts)
	if err != nil {
		return nil, err
	}
	desired := c.buildEntry(opts)

	serversKey := c.serversKey
	if serversKey == "" {
		serversKey = mcpServersKey
	}
	cfg, baseState, err := loadJSONConfigKey(path, serversKey, c.serverName)
	if err != nil {
		return nil, err
	}

	res := &Result{
		Client:     c.id,
		Scope:      opts.scopeLabel(),
		ServerName: c.serverName,
		Root:       opts.RootDir,
		ConfigPath: path,
		ManagedBy:  c.managedBy,
	}

	state := baseState
	if state == StateAbsent {
		// loadJSONConfig returns Absent as a placeholder; refine against desired.
		state, err = cfg.classify(desired)
		if err != nil {
			return nil, err
		}
	}
	res.State = state

	switch state {
	case StateEquivalent:
		res.Changed = false
		return res, nil
	case StateConflict:
		if !opts.Force {
			return nil, conflictError(c.id.DisplayName(), cfg.describeExisting(), describeEntry(desired))
		}
		if err := cfg.write(desired); err != nil {
			return nil, err
		}
		res.Changed = true
		return res, nil
	case StateAbsent:
		if err := cfg.write(desired); err != nil {
			return nil, err
		}
		res.Changed = true
		return res, nil
	default:
		return nil, fmt.Errorf("unexpected state %s for %s", state, path)
	}
}

func (c jsonClient) run(opts Options) (*Result, error) {
	return c.applyJSON(opts)
}
