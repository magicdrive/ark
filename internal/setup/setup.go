package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Result describes the outcome of a setup run for reporting (plan §18).
type Result struct {
	Client     ClientID
	Scope      string
	ServerName string
	Root       string
	// ConfigPath is the file Ark wrote/inspected. Empty for clients managed
	// through their own CLI (e.g. Codex), where ManagedBy is set instead.
	ConfigPath string
	ManagedBy  string
	State      State
	// Changed reports whether Ark mutated anything. Equivalent/no-op is false.
	Changed bool
	// Warnings are non-fatal advisories surfaced to the user.
	Warnings []string
	// ExtraLines are adapter-specific report lines (e.g. Claude skill output).
	ExtraLines []string
}

// adapter is the minimal per-client contract. Interface-over-function is kept
// deliberately thin (plan §8): shared JSON clients embed jsonClient, Codex is
// its own implementation.
type adapter interface {
	run(opts Options) (*Result, error)
}

// adapterFor returns the adapter implementation for a client.
func adapterFor(id ClientID) (adapter, error) {
	switch id {
	case ClientClaude:
		return newClaudeAdapter(), nil
	case ClientCursor:
		return newCursorAdapter(), nil
	case ClientCline:
		return newClineAdapter(), nil
	case ClientCodex:
		return newCodexAdapter(), nil
	case ClientCopilot:
		return newCopilotAdapter(), nil
	default:
		return nil, fmt.Errorf("no adapter registered for client %q", id)
	}
}

// Run executes the full setup flow for one client (plan §37):
// validate options → resolve root/ark path → dispatch to the client adapter.
// Common, client-neutral preflight lives here; client-specific config access
// lives entirely in the adapter.
func Run(opts Options) (*Result, error) {
	// Note: opts.Name is the Claude skill/slash-command name (Claude only);
	// the MCP server entry name is always "ark" and is set per adapter.
	root, err := resolveRoot(opts.RootDir)
	if err != nil {
		return nil, err
	}
	opts.RootDir = root

	arkPath, warnings, err := resolveArkPath(opts.ArkPath)
	if err != nil {
		return nil, err
	}
	opts.ArkPath = arkPath

	ad, err := adapterFor(opts.Client)
	if err != nil {
		return nil, err
	}

	res, err := ad.run(opts)
	if err != nil {
		return nil, err
	}
	res.Warnings = append(warnings, res.Warnings...)
	return res, nil
}

// resolveRoot normalizes the root to an absolute path and verifies it exists
// and is a directory (plan §17). A missing or non-directory root is a hard
// error — failure is never deferred to MCP start time.
func resolveRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("cannot determine current directory: %w", err)
		}
		root = cwd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("cannot resolve root %q: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("root directory does not exist: %s", abs)
		}
		return "", fmt.Errorf("cannot access root %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("root is not a directory: %s", abs)
	}
	return abs, nil
}

// resolveArkPath defaults the ark command and validates it (plan §16).
//
//   - Explicit --ark-path: must resolve to an existing, executable file. A bad
//     explicit path is a hard error.
//   - Default "ark": looked up on PATH. If absent we keep the portable literal
//     "ark" and only warn (do not canonicalize to an absolute path).
func resolveArkPath(arkPath string) (string, []string, error) {
	explicit := strings.TrimSpace(arkPath) != ""
	if !explicit {
		arkPath = "ark"
	}

	looksLikePath := strings.ContainsRune(arkPath, '/') || strings.ContainsRune(arkPath, os.PathSeparator) || filepath.IsAbs(arkPath)

	if explicit && looksLikePath {
		info, err := os.Stat(arkPath)
		if err != nil {
			return "", nil, fmt.Errorf("ark binary not found at %q: %w", arkPath, err)
		}
		if info.IsDir() {
			return "", nil, fmt.Errorf("ark path is a directory, not an executable: %s", arkPath)
		}
		// Require a regular file: never accept a FIFO, socket, device or other
		// non-regular object as the ark binary. This invariant is enforced here
		// in common preflight so platform executable checks can assume it.
		if !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("ark path is not a regular file: %s", arkPath)
		}
		if !isExecutableFile(arkPath, info) {
			return "", nil, fmt.Errorf("ark path is not executable: %s", arkPath)
		}
		return arkPath, nil, nil
	}

	// Bare command name (explicit or default): resolve on PATH.
	if _, err := exec.LookPath(arkPath); err != nil {
		if explicit {
			return "", nil, fmt.Errorf("ark command %q not found on PATH: %w", arkPath, err)
		}
		return arkPath, []string{
			fmt.Sprintf("ark command %q was not found on PATH. "+
				"The config was written with the literal command %q; "+
				"ensure it is on PATH when the client starts, or pass --ark-path.", arkPath, arkPath),
		}, nil
	}
	return arkPath, nil, nil
}
