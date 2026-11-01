package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// clineAdapter configures Ark for the Cline CLI (plan §14 Cline).
//
// The Cline CLI reads its MCP servers from cline_mcp_settings.json, located as
// the CLI locates it (@cline/shared storage paths):
//
//  1. $CLINE_MCP_SETTINGS_PATH
//  2. $CLINE_DATA_DIR/settings/cline_mcp_settings.json
//  3. $CLINE_DIR/data/settings/cline_mcp_settings.json
//  4. <home>/.cline/data/settings/cline_mcp_settings.json
//
// where <home> is $HOME, then %USERPROFILE%, then %HOMEDRIVE%%HOMEPATH%, then
// the OS home directory (clineHome). Values are trimmed, and empty means unset,
// as in Cline. The CLI's --config / --data-dir flags pick other directories
// per run; Ark cannot see those, so users of them set the variables above.
//
// Earlier Ark versions wrote ~/.cline/mcp.json, which no Cline version reads.
// Ark reports servers it finds there (never changes the file); see
// clineLegacyNotice.
//
// The MCP settings of Cline's VS Code / Cursor / Windsurf extensions
// (globalStorage/cline_mcp_settings.json) are out of scope: Ark never probes
// OS/editor-specific storage paths.
//
// Cline's configuration is user-level, so there is no distinct project scope;
// --global is accepted but resolves to the same file. The served repository is
// still conveyed via --root in args.
type clineAdapter struct {
	base jsonClient
}

const clineSettingsFile = "cline_mcp_settings.json"

func newClineAdapter() adapter {
	return clineAdapter{
		base: jsonClient{
			id:         ClientCline,
			serverName: "ark",
			managedBy:  "config file",
			resolvePath: func(Options) (string, error) {
				return clineSettingsPath()
			},
			buildEntry: func(opts Options) map[string]any {
				return stdioEntry(opts.ArkPath, []string{"mcp-server", "--root", opts.RootDir})
			},
			equal: clineEntryEqual,
		},
	}
}

func (a clineAdapter) run(opts Options) (*Result, error) {
	current, _ := clineSettingsPath()
	res, err := a.base.applyJSON(opts)
	if err != nil {
		if notice := clineLegacyNotice(current); notice != "" {
			return nil, fmt.Errorf("%w\n\nNote: %s", err, notice)
		}
		return nil, err
	}
	// Cline CLI config is user-level; report the scope honestly regardless of
	// the --global flag.
	res.Scope = "user"
	if notice := clineLegacyNotice(current); notice != "" {
		res.Warnings = append(res.Warnings, notice)
	}
	return res, nil
}

// clineSettingsPath resolves the Cline CLI's MCP settings file (see
// clineAdapter). A relative value of a variable is refused: Cline resolves it
// against its own working directory, which Ark cannot know.
func clineSettingsPath() (string, error) {
	abs := func(name, v string) (string, error) {
		if !filepath.IsAbs(v) {
			return "", fmt.Errorf("%s is %q, a relative path; Cline resolves it against the directory it is started in, so Ark cannot tell which file Cline reads\n"+
				"set %s to an absolute path and re-run `ark setup cline`", name, v, name)
		}
		return filepath.Clean(v), nil
	}
	if v := strings.TrimSpace(os.Getenv("CLINE_MCP_SETTINGS_PATH")); v != "" {
		return abs("CLINE_MCP_SETTINGS_PATH", v)
	}
	if v := strings.TrimSpace(os.Getenv("CLINE_DATA_DIR")); v != "" {
		dir, err := abs("CLINE_DATA_DIR", v)
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "settings", clineSettingsFile), nil
	}
	if v := strings.TrimSpace(os.Getenv("CLINE_DIR")); v != "" {
		dir, err := abs("CLINE_DIR", v)
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "data", "settings", clineSettingsFile), nil
	}
	home, err := clineHome()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(home) {
		// Ark would write under its own working directory, a file Cline
		// started anywhere else does not read.
		return "", fmt.Errorf("the home directory %q is not an absolute path; set HOME (or CLINE_DATA_DIR) to an absolute path", home)
	}
	return filepath.Join(home, ".cline", "data", "settings", clineSettingsFile), nil
}

// clineHome is the home directory as Cline determines it: $HOME (unless empty
// or "~"), then %USERPROFILE%, then %HOMEDRIVE%%HOMEPATH%, then the OS home
// directory. Cline applies this order on every OS, so on Windows a HOME set by
// Git Bash or MSYS wins over USERPROFILE, as it does for Cline.
func clineHome() (string, error) {
	if h := strings.TrimSpace(os.Getenv("HOME")); h != "" && h != "~" {
		return h, nil
	}
	if h := strings.TrimSpace(os.Getenv("USERPROFILE")); h != "" {
		return h, nil
	}
	drive, p := strings.TrimSpace(os.Getenv("HOMEDRIVE")), strings.TrimSpace(os.Getenv("HOMEPATH"))
	if drive != "" && p != "" {
		return drive + p, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return h, nil
}

// clineLegacyPath is where Ark before this change wrote `ark setup cline`.
func clineLegacyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cline", "mcp.json"), nil
}

// clineEntryEqual compares two Cline server entries as Cline reads them. Cline
// accepts a stdio server both flat, {command, args, env}, and nested,
// {transport: {type: "stdio", command, args, env}} (the form `cline mcp add`
// writes); the two are the same server. "type" "stdio" may be omitted in the
// flat form, an absent, null or empty env is no environment, and
// "disabled": false (which `cline mcp add` writes) is the default. Any other
// field must match exactly, so an entry Ark does not understand is a conflict,
// never silently taken as equivalent.
func clineEntryEqual(existing any, desired map[string]any) (bool, error) {
	return semanticEqual(normalizeClineEntry(existing), normalizeClineEntry(desired))
}

// normalizeClineEntry rewrites a stdio entry into the flat form without the
// default type and the empty env. Anything else is returned unchanged.
func normalizeClineEntry(entry any) any {
	m, ok := entry.(map[string]any)
	if !ok {
		return entry
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	if tr, nested := out["transport"]; nested {
		t, ok := tr.(map[string]any)
		if !ok || t["type"] != "stdio" {
			return entry
		}
		for _, k := range []string{"command", "args", "env", "type"} {
			if _, clash := out[k]; clash {
				return entry // both forms at once: not an entry Ark can read
			}
		}
		delete(out, "transport")
		for k, v := range t {
			if k == "type" {
				continue
			}
			out[k] = v
		}
	} else if t, ok := out["type"]; ok {
		if t != "stdio" {
			return entry
		}
		delete(out, "type")
	}
	if isEmptyEnv(out["env"]) {
		delete(out, "env")
	}
	if d, ok := out["disabled"]; ok && d == false {
		delete(out, "disabled")
	}
	return out
}

// clineLegacyNotice reports servers in ~/.cline/mcp.json, where earlier Ark
// versions wrote. Cline never read that file, so the Ark entry there (and any
// server added next to it) had no effect. Ark leaves the file unchanged. The
// result is "" when there is nothing to report, or when current (the settings
// file Cline reads) is that very file, as CLINE_MCP_SETTINGS_PATH can make it.
func clineLegacyNotice(current string) string {
	path, err := clineLegacyPath()
	if err != nil || filepath.Clean(path) == filepath.Clean(current) {
		return ""
	}
	peek := peekServers(path, mcpServersKey)
	if !peek.exists {
		return ""
	}
	if peek.problem != "" {
		return fmt.Sprintf("Ark could not check %s, where earlier Ark versions wrote the Cline configuration (%s). "+
			"Cline does not read that file; servers in it have no effect.", path, peek.problem)
	}
	if len(peek.servers) == 0 {
		return ""
	}
	var parts []string
	if _, ok := peek.servers["ark"]; ok {
		parts = append(parts, "the \"ark\" entry an earlier `ark setup cline` wrote")
	}
	if others := otherServerNames(peek.servers, "ark"); len(others) > 0 {
		parts = append(parts, fmt.Sprintf("other servers (%s)", strings.Join(others, ", ")))
	}
	return fmt.Sprintf("%s has %s. Cline does not read that file, so they never took effect; Ark left it unchanged. "+
		"Move any other servers you want into Cline's MCP settings file, then delete %s.",
		path, strings.Join(parts, " and "), path)
}
