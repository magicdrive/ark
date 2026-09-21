package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzer_GoProject(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "internal/core"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "internal/core/core.go"), []byte("package core"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "internal/core/core_test.go"), []byte("package core"), 0644)

	analysis, err := NewAnalyzer(tmpDir).Analyze()
	if err != nil {
		t.Fatal(err)
	}
	if analysis.PrimaryLang != "Go" {
		t.Errorf("expected Go, got %s", analysis.PrimaryLang)
	}
	if !analysis.HasTests {
		t.Error("expected HasTests true")
	}
	if len(analysis.BuildCommands) == 0 {
		t.Error("expected build commands")
	}
}

func TestAnalyzer_NodeProject(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "index.js"), []byte(""), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "src/components"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "src/components/App.tsx"), []byte(""), 0644)

	analysis, _ := NewAnalyzer(tmpDir).Analyze()
	if len(analysis.Languages) == 0 {
		t.Error("expected languages")
	}
	hasNodeCmds := false
	for _, cmd := range analysis.BuildCommands {
		if cmd.Source == "package.json" {
			hasNodeCmds = true
		}
	}
	if !hasNodeCmds {
		t.Error("expected npm commands")
	}
}

func TestAnalyzer_Makefile(t *testing.T) {
	tmpDir := t.TempDir()
	makefile := "build:\n\tgo build\n\ntest:\n\tgo test\n\nlint:\n\tgolint\n"
	os.WriteFile(filepath.Join(tmpDir, "Makefile"), []byte(makefile), 0644)

	analysis, _ := NewAnalyzer(tmpDir).Analyze()
	hasTargets := map[string]bool{}
	for _, cmd := range analysis.BuildCommands {
		if cmd.Source == "Makefile" {
			hasTargets[cmd.Type] = true
		}
	}
	if !hasTargets["build"] || !hasTargets["test"] || !hasTargets["lint"] {
		t.Errorf("expected build/test/lint targets, got %v", hasTargets)
	}
}

func TestAnalyzer_SkipsVendor(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "vendor/lib"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "vendor/lib/lib.go"), []byte("package lib"), 0644)

	analysis, _ := NewAnalyzer(tmpDir).Analyze()
	// Should only count main.go, not vendor
	if analysis.Languages[0].FileCount != 1 {
		t.Errorf("expected 1 file, got %d", analysis.Languages[0].FileCount)
	}
}

func TestAnalyzer_DetectModules(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "cmd/myapp"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "internal/core"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "pkg/utils"), 0755)

	analysis, _ := NewAnalyzer(tmpDir).Analyze()
	if len(analysis.Modules) != 3 {
		t.Errorf("expected 3 modules, got %d", len(analysis.Modules))
	}
	hasCmd := false
	for _, m := range analysis.Modules {
		if m.Path == "cmd/myapp" && m.Type == "command" {
			hasCmd = true
		}
	}
	if !hasCmd {
		t.Error("expected cmd/myapp as command")
	}
}
