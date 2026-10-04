package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// codexAdapter configures Ark for Codex via the official `codex` CLI
// (plan §13: prefer the client's own CLI over editing config.toml directly, and
// never add a TOML dependency without approval).
//
// Codex MCP configuration is user-level (~/.codex/config.toml). The CLI, not
// Ark, owns that file, so unrelated servers and settings are preserved by
// construction. Ark only ever asks the CLI about / mutates its own "ark" entry.
//
// Contract (confirmed against the current Codex CLI; see testdata/codex):
//   - `codex mcp list --json` prints an array of server objects; stdio details
//     live under a nested "transport" object:
//     {"name":"ark","transport":{"type":"stdio","command":...,"args":[...]}}
//   - `codex mcp add <name> -- <command> <args...>` adds/replaces one server.
//   - `codex mcp remove <name>` removes one server.
//
// If the Codex CLI is absent, or does not emit parseable JSON, setup fails
// explicitly with guidance toward manual configuration — it never falls back to
// guessing or to rewriting config.toml.
type codexAdapter struct {
	runner commandRunner
}

func newCodexAdapter() adapter {
	return codexAdapter{runner: execRunner{}}
}

const codexServerName = "ark"

func (a codexAdapter) run(opts Options) (*Result, error) {
	if _, err := a.runner.look("codex"); err != nil {
		return nil, fmt.Errorf(
			"the Codex CLI (`codex`) was not found on PATH\n" +
				"install Codex and ensure `codex` is runnable, then re-run `ark setup codex`,\n" +
				"or configure Ark manually in Codex's MCP configuration (see README)")
	}

	desiredCmd := opts.ArkPath
	desiredArgs := []string{"mcp-server", "--root", opts.RootDir}

	servers, err := a.listServers()
	if err != nil {
		return nil, err
	}

	res := &Result{
		Client:     ClientCodex,
		Scope:      "user",
		ServerName: codexServerName,
		Root:       opts.RootDir,
		ManagedBy:  "codex CLI",
	}

	existing, found := servers[codexServerName]
	switch {
	case !found:
		res.State = StateAbsent
		if err := a.addServer(codexServerName, desiredCmd, desiredArgs); err != nil {
			return nil, err
		}
		if err := a.verify(desiredCmd, desiredArgs); err != nil {
			// Nothing valid existed before; remove the entry we just added so we
			// do not leave a half-registered server behind.
			_ = a.removeServer(codexServerName)
			return nil, err
		}
		res.Changed = true
		return res, nil

	case existing.equal(desiredCmd, desiredArgs):
		res.State = StateEquivalent
		return res, nil

	default:
		res.State = StateConflict
		if !opts.Force {
			return nil, conflictError(ClientCodex.DisplayName(), existing.describe(), describeCmd(desiredCmd, desiredArgs))
		}
		// Safety: Ark can only restore a stdio entry via `codex mcp add`. If the
		// existing entry is not stdio, Ark cannot safely undo a failed removal,
		// so it must not destroy it — not even with --force (task §1):
		// "If Ark cannot safely restore what it is about to destroy, Ark must
		// not destroy it." No mutation happens here.
		if existing.transportType != "stdio" {
			return nil, fmt.Errorf(
				"refusing to replace the existing Codex %q MCP entry: it uses the %q transport, which Ark cannot safely restore if replacement fails\n"+
					"remove or update it manually (e.g. `codex mcp remove %s`), then re-run `ark setup codex`",
				codexServerName, existing.transportType, codexServerName)
		}
		if err := a.replace(existing, desiredCmd, desiredArgs); err != nil {
			return nil, err
		}
		res.Changed = true
		return res, nil
	}
}

// replace swaps a conflicting Ark entry for the desired one with best-effort
// restoration of the previous valid entry on failure (plan §7, §35; task §6–9).
//
// Codex mutations are external CLI calls, so a true transaction is impossible;
// the goal is that a failed replacement does not lose the user's prior valid Ark
// configuration. Only Ark's own named entry is ever touched.
func (a codexAdapter) replace(old codexServer, desiredCmd string, desiredArgs []string) error {
	// Step 1: remove the old entry. If this fails, nothing changed — do NOT add.
	if err := a.removeServer(codexServerName); err != nil {
		return fmt.Errorf("failed to replace Ark MCP configuration: %w\n"+
			"the previous Ark configuration was left unchanged", err)
	}

	// Step 2: add the desired entry. On failure, restore the old one.
	if err := a.addServer(codexServerName, desiredCmd, desiredArgs); err != nil {
		return a.replacementError(old, err)
	}

	// Step 3: verify. On failure, remove the (bad) new entry and restore the old.
	if err := a.verify(desiredCmd, desiredArgs); err != nil {
		_ = a.removeServer(codexServerName)
		return a.replacementError(old, err)
	}
	return nil
}

// replacementError restores the previous entry and reports the primary failure,
// never hiding it behind a restore failure (task §9).
func (a codexAdapter) replacementError(old codexServer, primary error) error {
	if rErr := a.restore(old); rErr != nil {
		return fmt.Errorf("failed to replace Ark MCP configuration: %w\n"+
			"WARNING: restoration of the previous Ark configuration also failed: %v", primary, rErr)
	}
	return fmt.Errorf("failed to replace Ark MCP configuration: %w\n"+
		"the previous Ark configuration was restored", primary)
}

// restore re-adds the previous Ark entry from structured data (never a shell
// string; task §8). Only stdio entries can be re-created via `codex mcp add`.
func (a codexAdapter) restore(old codexServer) error {
	if old.transportType != "stdio" {
		return fmt.Errorf("the previous entry used unsupported transport %q and cannot be restored via the Codex CLI", old.transportType)
	}
	return a.addServer(codexServerName, old.command, old.args)
}

// verify re-reads the server list and confirms the Ark entry matches desired.
func (a codexAdapter) verify(desiredCmd string, desiredArgs []string) error {
	after, err := a.listServers()
	if err != nil {
		return fmt.Errorf("verify failed: %w", err)
	}
	entry, ok := after[codexServerName]
	if !ok || !entry.equal(desiredCmd, desiredArgs) {
		return fmt.Errorf("verify failed: Codex did not register the Ark MCP server as requested")
	}
	return nil
}

func (a codexAdapter) listServers() (map[string]codexServer, error) {
	stdout, stderr, _, err := a.runner.run("codex", "mcp", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("could not list Codex MCP servers via `codex mcp list --json`: %w\n%s\n"+
			"your Codex version may not support this; upgrade Codex or configure Ark manually (see README)",
			err, strings.TrimSpace(string(stderr)))
	}
	return parseCodexList(stdout)
}

func (a codexAdapter) addServer(name, command string, args []string) error {
	cliArgs := append([]string{"mcp", "add", name, "--"}, append([]string{command}, args...)...)
	if _, stderr, _, err := a.runner.run("codex", cliArgs...); err != nil {
		return fmt.Errorf("`codex mcp add` failed: %w\n%s", err, strings.TrimSpace(string(stderr)))
	}
	return nil
}

func (a codexAdapter) removeServer(name string) error {
	if _, stderr, _, err := a.runner.run("codex", "mcp", "remove", name); err != nil {
		return fmt.Errorf("`codex mcp remove` failed: %w\n%s", err, strings.TrimSpace(string(stderr)))
	}
	return nil
}

// codexServer is the subset of a Codex MCP server entry Ark reasons about.
// transportType records the transport so a non-stdio entry (e.g. HTTP) is never
// mistaken for an empty-command stdio server.
type codexServer struct {
	transportType string
	command       string
	args          []string
}

// equal reports whether this entry is a stdio server matching command+args.
// A non-stdio transport never equals the desired stdio configuration.
func (s codexServer) equal(command string, args []string) bool {
	if s.transportType != "stdio" {
		return false
	}
	if s.command != command || len(s.args) != len(args) {
		return false
	}
	for i := range args {
		if s.args[i] != args[i] {
			return false
		}
	}
	return true
}

func (s codexServer) describe() string {
	if s.transportType != "" && s.transportType != "stdio" {
		return fmt.Sprintf("(%s transport)", s.transportType)
	}
	return describeCmd(s.command, s.args)
}

func describeCmd(command string, args []string) string {
	if len(args) == 0 {
		return command
	}
	return command + " " + strings.Join(args, " ")
}

// parseCodexList extracts server entries from the JSON emitted by
// `codex mcp list --json`. The current contract is an array of objects whose
// stdio details are nested under "transport":
//
//	[{"name":"ark","transport":{"type":"stdio","command":"...","args":[...]}}]
//
// A legacy flat shape ({"name":{"command":...,"args":[...]}} or an array item
// with top-level command/args) is accepted only as a fallback when no transport
// object is present, and never turns malformed data into a valid stdio server.
func parseCodexList(data []byte) (map[string]codexServer, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return map[string]codexServer{}, nil
	}

	// Array form (current Codex contract).
	if data[0] == '[' {
		var arr []map[string]any
		if err := json.Unmarshal(data, &arr); err != nil {
			return nil, codexParseErr(err)
		}
		out := map[string]codexServer{}
		for _, m := range arr {
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			s, err := codexServerFromMap(m)
			if err != nil {
				return nil, codexParseErr(fmt.Errorf("server %q: %w", name, err))
			}
			out[name] = s
		}
		return out, nil
	}

	// Object form: {"name": {...}} or {"mcpServers": {"name": {...}}}.
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, codexParseErr(err)
	}
	if inner, ok := obj["mcpServers"].(map[string]any); ok {
		obj = inner
	}
	out := map[string]codexServer{}
	for name, v := range obj {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		s, err := codexServerFromMap(m)
		if err != nil {
			return nil, codexParseErr(fmt.Errorf("server %q: %w", name, err))
		}
		out[name] = s
	}
	return out, nil
}

// codexServerFromMap decodes one server entry. It prefers the nested transport
// object (current contract) and falls back to a flat shape only when transport
// is absent. Malformed stdio args are a hard error, never silently normalized
// (task §3).
func codexServerFromMap(m map[string]any) (codexServer, error) {
	if tr, ok := m["transport"].(map[string]any); ok {
		s := codexServer{}
		s.transportType, _ = tr["type"].(string)
		if s.transportType == "stdio" {
			s.command, _ = tr["command"].(string)
			args, err := codexArgs(tr["args"])
			if err != nil {
				return codexServer{}, err
			}
			s.args = args
		}
		return s, nil
	}

	// Legacy fallback: only treat as stdio when a non-empty command is present
	// at the top level, so malformed/unknown shapes are not promoted to valid.
	if cmd, ok := m["command"].(string); ok && cmd != "" {
		args, err := codexArgs(m["args"])
		if err != nil {
			return codexServer{}, err
		}
		return codexServer{transportType: "stdio", command: cmd, args: args}, nil
	}
	return codexServer{}, nil
}

// codexArgs converts a Codex stdio "args" value to []string.
//
//   - absent / null  → empty args (nil), which the current contract allows
//   - JSON string array → the strings
//   - anything else (non-array, or an array with a non-string element) → error,
//     so malformed evidence is never silently turned into a valid-looking entry.
func codexArgs(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("args is %T, expected an array of strings", v)
	}
	out := make([]string, 0, len(raw))
	for i, a := range raw {
		str, ok := a.(string)
		if !ok {
			return nil, fmt.Errorf("args[%d] is %T, expected a string", i, a)
		}
		out = append(out, str)
	}
	return out, nil
}

func codexParseErr(err error) error {
	return fmt.Errorf("could not parse `codex mcp list --json` output: %w\n"+
		"your Codex version may be incompatible; configure Ark manually (see README)", err)
}

// commandRunner abstracts external process execution so adapters can be unit
// tested without the real client CLI installed.
type commandRunner interface {
	look(name string) (string, error)
	run(name string, args ...string) (stdout, stderr []byte, exitCode int, err error)
}

type execRunner struct{}

func (execRunner) look(name string) (string, error) {
	return exec.LookPath(name)
}

func (execRunner) run(name string, args ...string) ([]byte, []byte, int, error) {
	cmd := exec.Command(name, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return out.Bytes(), errBuf.Bytes(), exitCode, err
}
