package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type MCPInitOptions struct {
	ArkPath    string
	RootDir    string
	ServerName string
	Global     bool
	Force      bool
}

func RunMCPInit(opts *MCPInitOptions) error {
	settingsPath, err := resolveSettingsPath(opts.Global)
	if err != nil {
		return err
	}

	settings, err := readSettings(settingsPath)
	if err != nil {
		return err
	}

	mcpServers := extractMCPServers(settings)

	alreadyExists := false
	if _, exists := mcpServers[opts.ServerName]; exists {
		if !opts.Force {
			return fmt.Errorf("MCP server %q already configured in %s\nUse --force to overwrite.", opts.ServerName, settingsPath)
		}
		alreadyExists = true
	}

	mcpServers[opts.ServerName] = map[string]any{
		"type":    "stdio",
		"command": opts.ArkPath,
		"args":    []string{"mcp-server", "--root", opts.RootDir},
		"env":     map[string]any{},
	}
	settings["mcpServers"] = mcpServers

	if err := writeSettings(settingsPath, settings); err != nil {
		return err
	}

	action := "Added"
	if alreadyExists {
		action = "Updated"
	}
	fmt.Printf("%s MCP server %q in %s\n", action, opts.ServerName, settingsPath)
	fmt.Printf("  command: %s\n", opts.ArkPath)
	fmt.Printf("  root:    %s\n", opts.RootDir)

	return nil
}

// resolveSettingsPath returns the path to the MCP settings file.
// Project scope uses .mcp.json (Claude Code's project-level MCP config).
// Global scope uses ~/.claude/settings.json.
func resolveSettingsPath(global bool) (string, error) {
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine current directory: %w", err)
	}
	return filepath.Join(cwd, ".mcp.json"), nil
}

func readSettings(path string) (map[string]any, error) {
	settings := make(map[string]any)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return settings, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return settings, nil
}

func extractMCPServers(settings map[string]any) map[string]any {
	if v, ok := settings["mcpServers"].(map[string]any); ok {
		return v
	}
	return make(map[string]any)
}

func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}
