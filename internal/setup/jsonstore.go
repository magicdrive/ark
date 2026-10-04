package setup

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// jsonConfig is an MCP configuration file that stores servers under a top-level
// "mcpServers" object, as used by Claude Code, Cursor and the Cline CLI.
//
// The whole document is decoded into a map so unknown top-level keys, unknown
// server entries and unknown fields inside the Ark entry are all preserved
// across a rewrite (plan §13). Ark only ever touches its own entry.
type jsonConfig struct {
	path        string
	serverName  string
	existed     bool
	origBytes   []byte
	origMode    os.FileMode
	doc         map[string]any
	mcpServers  map[string]any
	existing    any // current value of the Ark entry (nil if absent)
	entryExists bool
}

const (
	mcpServersKey = "mcpServers"
	maxConfigSize = 16 << 20 // 16 MiB guard against oversized/hostile config (plan §33)
)

// loadJSONConfig reads and parses the config file, classifying any structural
// problem as Malformed/Unavailable. It performs no mutation.
func loadJSONConfig(path, serverName string) (*jsonConfig, State, error) {
	c := &jsonConfig{path: path, serverName: serverName, origMode: 0644}

	// Reject non-regular targets (symlink, device, etc.) before touching them.
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, StateUnavailable, fmt.Errorf("refusing to modify %s: configuration target is a symlink", path)
		}
		if !info.Mode().IsRegular() {
			return nil, StateUnavailable, fmt.Errorf("refusing to modify %s: not a regular file", path)
		}
		if info.Size() > maxConfigSize {
			return nil, StateUnavailable, fmt.Errorf("configuration file %s is too large (%d bytes)", path, info.Size())
		}
	} else if !os.IsNotExist(err) {
		return nil, StateUnavailable, fmt.Errorf("cannot access %s: %w", path, err)
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		c.doc = map[string]any{}
		c.mcpServers = map[string]any{}
		return c, StateAbsent, nil
	}
	if err != nil {
		return nil, StateUnavailable, fmt.Errorf("cannot read %s: %w", path, err)
	}

	c.existed = true
	c.origBytes = data
	if info, statErr := os.Stat(path); statErr == nil {
		c.origMode = info.Mode().Perm()
	}

	doc := map[string]any{}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &doc); err != nil {
			// Malformed: never overwritten, even with --force (plan §10).
			return nil, StateMalformed, fmt.Errorf("cannot parse %s: %w\nArk did not modify the file; fix the configuration and retry", path, err)
		}
	}
	c.doc = doc

	switch v := doc[mcpServersKey].(type) {
	case nil:
		c.mcpServers = map[string]any{}
	case map[string]any:
		c.mcpServers = v
	default:
		return nil, StateMalformed, fmt.Errorf("%s has an unexpected %q type (%T); Ark cannot safely merge and did not modify the file", path, mcpServersKey, v)
	}

	if entry, ok := c.mcpServers[serverName]; ok {
		c.existing = entry
		c.entryExists = true
	}
	return c, StateAbsent, nil // caller refines state via classify()
}

// classify compares the current Ark entry (if any) against the desired entry.
func (c *jsonConfig) classify(desired map[string]any) (State, error) {
	if !c.entryExists {
		return StateAbsent, nil
	}
	eq, err := semanticEqual(c.existing, desired)
	if err != nil {
		return StateMalformed, err
	}
	if eq {
		return StateEquivalent, nil
	}
	return StateConflict, nil
}

// setEntry installs the desired Ark entry into the in-memory document.
func (c *jsonConfig) setEntry(desired map[string]any) {
	c.mcpServers[c.serverName] = desired
	c.doc[mcpServersKey] = c.mcpServers
}

// describeExisting renders the current Ark entry for conflict messages.
func (c *jsonConfig) describeExisting() string {
	return describeEntry(c.existing)
}

// write serializes the document and replaces the file atomically with
// lost-update protection and verify-after-write (plan §11, §12, §38).
func (c *jsonConfig) write(desired map[string]any) error {
	c.setEntry(desired)

	data, err := json.MarshalIndent(c.doc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize configuration: %w", err)
	}
	data = append(data, '\n')

	if err := atomicReplace(c.path, data, c.origBytes, c.existed, c.origMode); err != nil {
		return err
	}

	return c.verify(desired, data)
}

// verify re-reads the file and confirms the Ark entry matches the desired
// configuration.
//
// Crucially, if the file no longer contains exactly the bytes Ark wrote
// (written), another process modified it after Ark's atomic replace. In that
// case Ark leaves the current contents untouched and does NOT roll back —
// rolling back would silently discard that legitimate concurrent change
// (task §19). Rollback to the original bytes only happens when the file still
// holds Ark's own write, so no third-party data can be lost.
func (c *jsonConfig) verify(desired map[string]any, written []byte) error {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return fmt.Errorf("verify failed: cannot re-read %s: %w", c.path, err)
	}
	if !bytes.Equal(data, written) {
		return fmt.Errorf("verify failed: %s was modified by another process immediately after Ark wrote it; Ark left the current contents in place, re-run the command", c.path)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		c.rollback()
		return fmt.Errorf("verify failed: %s is not valid JSON after write: %w", c.path, err)
	}
	servers, _ := doc[mcpServersKey].(map[string]any)
	eq, cmpErr := semanticEqual(servers[c.serverName], desired)
	if cmpErr != nil || !eq {
		c.rollback()
		return fmt.Errorf("verify failed: Ark entry in %s does not match the requested configuration after write", c.path)
	}
	return nil
}

// rollback best-effort restores the original file content after a failed
// verification. It is intentionally silent about secondary failures.
func (c *jsonConfig) rollback() {
	if !c.existed {
		_ = os.Remove(c.path)
		return
	}
	_ = os.WriteFile(c.path, c.origBytes, c.origMode)
}

// atomicReplace writes data to a temp file in the same directory and renames it
// into place. Before renaming it re-reads the current file and compares it to
// the bytes observed at load time; a divergence means another process wrote the
// file concurrently and we abort without clobbering their change (plan §12).
func atomicReplace(path string, data, origBytes []byte, existed bool, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Lost-update check.
	cur, readErr := os.ReadFile(path)
	if existed {
		if readErr != nil {
			return fmt.Errorf("configuration %s disappeared while Ark was updating it; no changes were written, retry the command", path)
		}
		if !bytes.Equal(sum(cur), sum(origBytes)) {
			return fmt.Errorf("configuration %s changed while Ark was updating it; no changes were written, retry the command", path)
		}
	} else if readErr == nil {
		return fmt.Errorf("configuration %s was created while Ark was updating it; no changes were written, retry the command", path)
	}

	tmp, err := os.CreateTemp(dir, ".ark-mcp-*.json.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("failed to set permissions on temp file: %w", err)
	}
	if err := replaceFile(tmpName, path); err != nil {
		return fmt.Errorf("failed to replace %s: %w", path, err)
	}
	return nil
}

func sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

// semanticEqual reports whether two config values are semantically identical,
// independent of key order and of Go type differences introduced by JSON
// round-tripping (e.g. []string vs []any).
func semanticEqual(a, b any) (bool, error) {
	ab, err := json.Marshal(a)
	if err != nil {
		return false, err
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false, err
	}
	// Normalize both through a generic decode so map key ordering is canonical.
	var av, bv any
	if err := json.Unmarshal(ab, &av); err != nil {
		return false, err
	}
	if err := json.Unmarshal(bb, &bv); err != nil {
		return false, err
	}
	na, err := json.Marshal(av)
	if err != nil {
		return false, err
	}
	nb, err := json.Marshal(bv)
	if err != nil {
		return false, err
	}
	return bytes.Equal(na, nb), nil
}

// conflictError builds the shared, staticcheck-clean conflict message used by
// every adapter when an existing Ark entry differs from what was requested.
func conflictError(displayName, existing, requested string) error {
	return fmt.Errorf(
		"found a different Ark MCP configuration for %s\n\n"+
			"Existing:\n  %s\n\nRequested:\n  %s\n\n"+
			"re-run with --force to replace the Ark entry",
		displayName, existing, requested)
}

// describeEntry renders a server entry's command/args for human-facing output.
func describeEntry(entry any) string {
	m, ok := entry.(map[string]any)
	if !ok {
		return fmt.Sprintf("%v", entry)
	}
	cmd, _ := m["command"].(string)
	var args []string
	switch raw := m["args"].(type) {
	case []any:
		for _, a := range raw {
			args = append(args, fmt.Sprintf("%v", a))
		}
	case []string:
		args = append(args, raw...)
	}
	line := cmd
	for _, a := range args {
		line += " " + a
	}
	return line
}
