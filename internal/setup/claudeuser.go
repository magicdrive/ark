package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Claude Code's user scope (`ark setup claude --global`).
//
// Claude Code keeps user-scoped MCP servers in the top-level "mcpServers" of
// its global state file, ~/.claude.json ($CLAUDE_CONFIG_DIR/.claude.json when
// that is set). The file is Claude Code's own: it also holds account, project
// and UI state that Claude Code rewrites while it runs. So Ark never writes
// it. Ark reads it to classify the Ark entry, and changes it only through the
// official CLI, as codexAdapter does for Codex:
//
//	claude mcp add-json --scope user ark '<json>'
//	claude mcp remove --scope user ark
//
// Claude Code replaces a ~/.claude.json it cannot parse with a default one,
// dropping every server in it (it keeps the original under ~/.claude/backups).
// Ark therefore checks the file first, with the same rules as for the files it
// writes itself (no symlink, a regular file, at most maxConfigSize, valid
// JSON, an object "mcpServers"), and does not start `claude` when any fails.
//
// Claude Code 2.1.316 reads <config home>/.config.json instead (an older
// location; config home is $CLAUDE_CONFIG_DIR or ~/.claude) when that is a
// file, following symlinks, that is not empty, or is empty, writable and
// .claude.json does not exist; and it names the file .claude-custom-oauth.json when
// CLAUDE_CODE_CUSTOM_OAUTH_URL is set. Ark follows the first and refuses the
// second, so that it never classifies, verifies or restores against a file
// claude does not use.
//
// Earlier Ark versions wrote this scope to ~/.claude/settings.json, which
// Claude Code does not read MCP servers from. An Ark entry left there is
// reported (never changed) on every run; see claudeLegacyNotice.

const (
	claudeServerName = "ark"
	// claudeUserScope is the --scope value of the claude CLI for the user scope.
	claudeUserScope = "user"
)

// claudeUserConfigPath returns the file Claude Code keeps user-scoped servers
// in, chosen as Claude Code chooses it: <config home>/.config.json under the
// conditions in the file comment, else $CLAUDE_CONFIG_DIR/.claude.json when
// CLAUDE_CONFIG_DIR is non-empty, else ~/.claude.json. A .config.json chosen
// through a symlink is returned as it is, and refused by the checks of
// loadClaudeUserConfig, as is an empty one.
func claudeUserConfigPath() (string, error) {
	var base, configHome string
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", fmt.Errorf("cannot resolve CLAUDE_CONFIG_DIR %q: %w", dir, err)
		}
		base, configHome = abs, abs
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		base, configHome = home, filepath.Join(home, ".claude")
	}
	legacy, current := filepath.Join(configHome, ".config.json"), filepath.Join(base, ".claude.json")
	if info, err := os.Stat(legacy); err == nil && info.Mode().IsRegular() {
		if info.Size() > 0 {
			return legacy, nil
		}
		if _, err := os.Stat(current); info.Mode().Perm()&0o222 != 0 && err != nil {
			return legacy, nil
		}
	}
	return current, nil
}

// claudeCustomOAuthEnv names the variable under which Claude Code keeps its
// state in another file (see the file comment).
const claudeCustomOAuthEnv = "CLAUDE_CODE_CUSTOM_OAUTH_URL"

// claudeLegacySettingsPath is where Ark before this change wrote
// `ark setup claude --global` and `ark mcp-init --global`.
func claudeLegacySettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// claudeEntryEqual compares two Claude Code server entries as Claude Code
// reads them: "type" defaults to "stdio", and an absent, null or empty "env"
// is no environment. Every other field must match exactly.
func claudeEntryEqual(existing any, desired map[string]any) (bool, error) {
	return semanticEqual(normalizeClaudeEntry(existing), normalizeClaudeEntry(desired))
}

func normalizeClaudeEntry(entry any) any {
	m, ok := entry.(map[string]any)
	if !ok {
		return entry
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	if t, ok := out["type"]; !ok || t == nil {
		out["type"] = "stdio"
	}
	if isEmptyEnv(out["env"]) {
		delete(out, "env")
	}
	return out
}

// isEmptyEnv reports whether an "env" value sets no variable: absent (nil) or
// an empty object.
func isEmptyEnv(v any) bool {
	switch e := v.(type) {
	case nil:
		return true
	case map[string]any:
		return len(e) == 0
	case map[string]string:
		return len(e) == 0
	}
	return false
}

// runUserScope configures Ark in Claude Code's user scope through the claude
// CLI. It returns the result and a notice about a stale Ark entry in the old
// location; the notice is also appended to an error, so it is shown whether or
// not setup succeeds.
func (a claudeAdapter) runUserScope(opts Options) (*Result, error) {
	notice := claudeLegacyNotice(claudeEntry(opts))
	res, err := a.applyUserScope(opts)
	if err != nil {
		if notice != "" {
			return nil, fmt.Errorf("%w\n\nNote: %s", err, notice)
		}
		return nil, err
	}
	if notice != "" {
		res.Warnings = append(res.Warnings, notice)
	}
	return res, nil
}

func (a claudeAdapter) applyUserScope(opts Options) (*Result, error) {
	desired := claudeEntry(opts)
	if os.Getenv(claudeCustomOAuthEnv) != "" {
		return nil, fmt.Errorf("%s is set: Claude Code then keeps its MCP servers in a file Ark does not check, so Ark did not run `claude`\n"+
			"Register Ark yourself:\n  %s", claudeCustomOAuthEnv, describeClaudeAddJSON(desired))
	}
	path, err := claudeUserConfigPath()
	if err != nil {
		return nil, err
	}

	cfg, err := loadClaudeUserConfig(path)
	if err != nil {
		return nil, err
	}
	state, err := cfg.classifyWith(desired, claudeEntryEqual)
	if err != nil {
		return nil, err
	}

	res := &Result{
		Client:     ClientClaude,
		Scope:      opts.scopeLabel(),
		ServerName: claudeServerName,
		Root:       opts.RootDir,
		ConfigPath: path,
		ManagedBy:  "claude CLI",
		State:      state,
	}

	switch state {
	case StateEquivalent:
		return res, nil
	case StateConflict:
		if !opts.Force {
			return nil, conflictError(ClientClaude.DisplayName(), cfg.describeExisting(), describeEntry(desired))
		}
		old, ok := cfg.existing.(map[string]any)
		if !ok {
			// add-json takes an object; Ark could not put back anything else
			// if the replacement failed, so it does not remove it.
			return nil, fmt.Errorf(
				"refusing to replace the existing Claude Code %q MCP entry in %s: it is not a JSON object, which Ark cannot restore if replacement fails\n"+
					"remove it yourself (`claude mcp remove --scope user %s`), then re-run `ark setup claude --global`",
				claudeServerName, path, claudeServerName)
		}
		if err := a.requireClaudeCLI(path,
			describeClaudeCommand("mcp", "remove", "--scope", claudeUserScope, claudeServerName),
			describeClaudeAddJSON(desired)); err != nil {
			return nil, err
		}
		if err := a.replaceUserEntry(path, old, desired); err != nil {
			return nil, err
		}
	case StateAbsent:
		if err := a.requireClaudeCLI(path, describeClaudeAddJSON(desired)); err != nil {
			return nil, err
		}
		if err := a.addUserEntry(desired); err != nil {
			return nil, err
		}
		if err := verifyClaudeUserEntry(path, desired); err != nil {
			// Nothing was there before; do not leave a half-registered entry.
			_ = a.removeUserEntry()
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unexpected state %s for %s", state, path)
	}
	res.Changed = true
	res.ExtraLines = append(res.ExtraLines, fmt.Sprintf("✓ Registered with `claude mcp add-json --scope %s %s`", claudeUserScope, claudeServerName))
	return res, nil
}

// loadClaudeUserConfig reads ~/.claude.json read-only, with the checks that
// keep Ark from handing claude a file it would reset (see the file comment).
func loadClaudeUserConfig(path string) (*jsonConfig, error) {
	cfg, _, err := loadJSONConfigKey(path, mcpServersKey, claudeServerName)
	if err == nil && cfg.existed && len(bytes.TrimSpace(cfg.origBytes)) == 0 {
		// jsonstore reads an empty file as {}; claude reports it corrupted
		// and resets it.
		err = fmt.Errorf("cannot parse %s: the file is empty", path)
	}
	if err != nil {
		return nil, fmt.Errorf("%w\n"+
			"Ark did not run `claude`: Claude Code replaces a configuration it cannot read with a default one, which would drop your other MCP servers.\n"+
			"Fix %s (or move it aside), then re-run `ark setup claude --global`", err, path)
	}
	return cfg, nil
}

// requireClaudeCLI fails when `claude` is not on PATH, naming the commands the
// user can run instead. Ark never falls back to editing the file itself.
func (a claudeAdapter) requireClaudeCLI(path string, manual ...string) error {
	if _, err := a.runner.look("claude"); err == nil {
		return nil
	}
	return fmt.Errorf(
		"the Claude Code CLI (`claude`) was not found on PATH\n"+
			"Ark registers user-scoped MCP servers through it and does not edit %s itself.\n"+
			"Install Claude Code and re-run `ark setup claude --global`, or run:\n  %s",
		path, strings.Join(manual, "\n  "))
}

// replaceUserEntry swaps a conflicting Ark entry for desired, restoring the
// previous entry if the replacement fails (codexAdapter.replace).
func (a claudeAdapter) replaceUserEntry(path string, old, desired map[string]any) error {
	if err := a.removeUserEntry(); err != nil {
		return fmt.Errorf("failed to replace Ark MCP configuration: %w\n"+
			"the previous Ark configuration was left unchanged", err)
	}
	if err := a.addUserEntry(desired); err != nil {
		return a.replacementError(old, err)
	}
	if err := verifyClaudeUserEntry(path, desired); err != nil {
		_ = a.removeUserEntry()
		return a.replacementError(old, err)
	}
	return nil
}

// replacementError restores the previous entry and reports the primary
// failure. The previous entry's env is never printed, as it may hold secrets:
// describeEntry shows the command and arguments only, and claude's stderr of
// the restoring call is dropped. (The entry is still an argument of that
// call, as add-json takes it on the command line.)
func (a claudeAdapter) replacementError(old map[string]any, primary error) error {
	if rErr := a.restoreUserEntry(old); rErr != nil {
		return fmt.Errorf("failed to replace Ark MCP configuration: %w\n"+
			"WARNING: restoration of the previous Ark configuration (%s) also failed: %v", primary, describeEntry(old), rErr)
	}
	return fmt.Errorf("failed to replace Ark MCP configuration: %w\n"+
		"the previous Ark configuration was restored", primary)
}

func (a claudeAdapter) addUserEntry(entry map[string]any) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to encode the Ark MCP entry: %w", err)
	}
	if _, stderr, _, err := a.runner.run("claude", "mcp", "add-json", "--scope", claudeUserScope, claudeServerName, string(data)); err != nil {
		return fmt.Errorf("`claude mcp add-json` failed: %w\n%s", err, strings.TrimSpace(string(stderr)))
	}
	return nil
}

// restoreUserEntry is addUserEntry without claude's stderr in the error.
func (a claudeAdapter) restoreUserEntry(entry map[string]any) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to encode the previous Ark MCP entry: %w", err)
	}
	if _, _, _, err := a.runner.run("claude", "mcp", "add-json", "--scope", claudeUserScope, claudeServerName, string(data)); err != nil {
		return fmt.Errorf("`claude mcp add-json` failed: %w", err)
	}
	return nil
}

func (a claudeAdapter) removeUserEntry() error {
	if _, stderr, _, err := a.runner.run("claude", "mcp", "remove", "--scope", claudeUserScope, claudeServerName); err != nil {
		return fmt.Errorf("`claude mcp remove` failed: %w\n%s", err, strings.TrimSpace(string(stderr)))
	}
	return nil
}

// verifyClaudeUserEntry re-reads the file and confirms that claude stored the
// requested entry where Claude Code reads it.
func verifyClaudeUserEntry(path string, desired map[string]any) error {
	cfg, _, err := loadJSONConfigKey(path, mcpServersKey, claudeServerName)
	if err != nil {
		return fmt.Errorf("verify failed: %w", err)
	}
	state, err := cfg.classifyWith(desired, claudeEntryEqual)
	if err != nil {
		return fmt.Errorf("verify failed: %w", err)
	}
	if state != StateEquivalent {
		return fmt.Errorf("verify failed: `claude` did not store the requested Ark MCP entry in %s", path)
	}
	return nil
}

// claudeLegacyNotice reports an Ark entry in ~/.claude/settings.json, where
// earlier Ark versions wrote the user scope. Claude Code does not read MCP
// servers from that file, so the entry never had an effect; Ark leaves the
// file as it is and says the entry can be deleted. A file Ark cannot inspect
// gets a note saying so. The result is "" when there is nothing to report.
func claudeLegacyNotice(desired map[string]any) string {
	path, err := claudeLegacySettingsPath()
	if err != nil {
		return ""
	}
	peek := peekServers(path, mcpServersKey)
	if peek.problem != "" {
		return fmt.Sprintf("Ark could not check %s for an MCP entry left by an earlier `ark setup claude --global` (%s). "+
			"Claude Code does not read MCP servers from that file; an \"ark\" entry there has no effect and can be deleted.", path, peek.problem)
	}
	entry, ok := peek.servers[claudeServerName]
	if !ok {
		return ""
	}
	how := "it differs from the requested entry"
	if eq, err := claudeEntryEqual(entry, desired); err == nil && eq {
		how = "it is the same as the requested entry"
	}
	msg := fmt.Sprintf("%s has an %q MCP entry (%s), written by an earlier `ark setup claude --global` or `ark mcp-init --global`. "+
		"Claude Code does not read MCP servers from settings.json, so it has no effect; Ark left the file unchanged and you can delete the entry.",
		path, claudeServerName, how)
	if others := otherServerNames(peek.servers, claudeServerName); len(others) > 0 {
		msg += fmt.Sprintf(" The other servers there (%s) are not read either; add them with `claude mcp add-json --scope user`.", strings.Join(others, ", "))
	}
	return msg
}

// describeClaudeAddJSON renders the add-json command for the user to run by
// hand. The entry is Ark's own (command, args, empty env), so it holds no
// secret.
func describeClaudeAddJSON(entry map[string]any) string {
	data, err := json.Marshal(entry)
	if err != nil {
		return "claude mcp add-json --scope user ark '<entry>'"
	}
	return describeClaudeCommand("mcp", "add-json", "--scope", claudeUserScope, claudeServerName, string(data))
}

func describeClaudeCommand(args ...string) string {
	parts := []string{"claude"}
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// shellQuote quotes s for a POSIX shell when it needs quoting.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
