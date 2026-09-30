package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunMCPInit_CreateNew(t *testing.T) {
	tmpDir := t.TempDir()
	opts := &MCPInitOptions{
		ArkPath:    "/usr/bin/ark",
		RootDir:    tmpDir,
		ServerName: "ark",
		Global:     false,
		Force:      false,
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	if err := RunMCPInit(opts); err != nil {
		t.Fatalf("RunMCPInit failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, ".mcp.json"))
	if err != nil {
		t.Fatalf("Failed to read .mcp.json: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	servers, ok := settings["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers key missing")
	}
	entry, ok := servers["ark"].(map[string]any)
	if !ok {
		t.Fatal("ark server entry missing")
	}
	if entry["command"] != "/usr/bin/ark" {
		t.Errorf("command: expected /usr/bin/ark, got %v", entry["command"])
	}
	if entry["type"] != "stdio" {
		t.Errorf("type: expected stdio, got %v", entry["type"])
	}
	args, ok := entry["args"].([]any)
	if !ok || len(args) != 3 {
		t.Fatalf("args: expected 3 elements, got %v", entry["args"])
	}
	if args[0] != "mcp-server" || args[1] != "--root" || args[2] != tmpDir {
		t.Errorf("args: unexpected values %v", args)
	}
}

func TestRunMCPInit_MergesExistingSettings(t *testing.T) {
	tmpDir := t.TempDir()

	existing := map[string]any{
		"theme": "dark",
		"mcpServers": map[string]any{
			"other-tool": map[string]any{"command": "other"},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(tmpDir, ".mcp.json"), append(data, '\n'), 0644)

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	opts := &MCPInitOptions{
		ArkPath: "/bin/ark", RootDir: tmpDir, ServerName: "ark",
	}
	if err := RunMCPInit(opts); err != nil {
		t.Fatalf("RunMCPInit failed: %v", err)
	}

	resultData, _ := os.ReadFile(filepath.Join(tmpDir, ".mcp.json"))
	var settings map[string]any
	json.Unmarshal(resultData, &settings)

	if settings["theme"] != "dark" {
		t.Error("existing theme key was overwritten")
	}
	servers := settings["mcpServers"].(map[string]any)
	if _, ok := servers["other-tool"]; !ok {
		t.Error("existing other-tool server was removed")
	}
	if _, ok := servers["ark"]; !ok {
		t.Error("new ark server not added")
	}
}

func TestRunMCPInit_AlreadyExists_NoForce(t *testing.T) {
	tmpDir := t.TempDir()

	existing := map[string]any{
		"mcpServers": map[string]any{
			"ark": map[string]any{"command": "old"},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(tmpDir, ".mcp.json"), append(data, '\n'), 0644)

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	opts := &MCPInitOptions{
		ArkPath: "/bin/ark", RootDir: tmpDir, ServerName: "ark", Force: false,
	}
	err := RunMCPInit(opts)
	if err == nil {
		t.Fatal("Expected error when ark already configured without --force")
	}
}

func TestRunMCPInit_AlreadyExists_WithForce(t *testing.T) {
	tmpDir := t.TempDir()

	existing := map[string]any{
		"mcpServers": map[string]any{
			"ark": map[string]any{"command": "old-binary"},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(tmpDir, ".mcp.json"), append(data, '\n'), 0644)

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	opts := &MCPInitOptions{
		ArkPath: "/new/ark", RootDir: tmpDir, ServerName: "ark", Force: true,
	}
	if err := RunMCPInit(opts); err != nil {
		t.Fatalf("RunMCPInit with --force failed: %v", err)
	}

	resultData, _ := os.ReadFile(filepath.Join(tmpDir, ".mcp.json"))
	var settings map[string]any
	json.Unmarshal(resultData, &settings)
	servers := settings["mcpServers"].(map[string]any)
	entry := servers["ark"].(map[string]any)
	if entry["command"] != "/new/ark" {
		t.Errorf("command not updated: got %v", entry["command"])
	}
}

func TestRunMCPInit_CustomServerName(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	opts := &MCPInitOptions{
		ArkPath: "/bin/ark", RootDir: tmpDir, ServerName: "my-ark",
	}
	if err := RunMCPInit(opts); err != nil {
		t.Fatalf("RunMCPInit failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(tmpDir, ".mcp.json"))
	var settings map[string]any
	json.Unmarshal(data, &settings)
	servers := settings["mcpServers"].(map[string]any)
	if _, ok := servers["my-ark"]; !ok {
		t.Error("custom server name 'my-ark' not found")
	}
}

func TestReadSettings_NonExistent(t *testing.T) {
	settings, err := readSettings("/nonexistent/path/.mcp.json")
	if err != nil {
		t.Fatalf("Expected no error for missing file, got: %v", err)
	}
	if len(settings) != 0 {
		t.Errorf("Expected empty settings, got %v", settings)
	}
}

func TestReadSettings_InvalidJSON(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "*.json")
	tmpFile.WriteString("not valid json{{{")
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	_, err := readSettings(tmpFile.Name())
	if err == nil {
		t.Fatal("Expected error for invalid JSON")
	}
}

func TestExtractMCPServers_Missing(t *testing.T) {
	settings := map[string]any{"theme": "dark"}
	servers := extractMCPServers(settings)
	if len(servers) != 0 {
		t.Errorf("Expected empty servers, got %v", servers)
	}
}

func TestExtractMCPServers_Present(t *testing.T) {
	settings := map[string]any{
		"mcpServers": map[string]any{
			"foo": map[string]any{"command": "foo"},
		},
	}
	servers := extractMCPServers(settings)
	if _, ok := servers["foo"]; !ok {
		t.Error("Expected 'foo' server")
	}
}

func TestWriteSettings_CreatesDir(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "subdir", "nested", ".mcp.json")
	settings := map[string]any{"key": "value"}

	if err := writeSettings(path, settings); err != nil {
		t.Fatalf("writeSettings failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("File not created: %v", err)
	}
}
