
package mcp_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/mcp"
)

func TestProjectWatcher_RefreshOnFileChange(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup initial file and rescan func
	file1 := filepath.Join(tmpDir, "a.txt")
	if err := os.WriteFile(file1, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}

	rescan := func(root string) []string {
		files := []string{}
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				rel, _ := filepath.Rel(root, path)
				files = append(files, rel)
			}
			return nil
		})
		return files
	}

	initialFiles := rescan(tmpDir)
	pw := mcp.NewProjectWatcher(tmpDir, initialFiles, rescan)

	// Wait for watcher goroutine to start and process file event
	time.Sleep(1 * time.Second)

	// Add a new file to trigger refresh
	file2 := filepath.Join(tmpDir, "b.txt")
	if err := os.WriteFile(file2, []byte("world"), 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	time.Sleep(1 * time.Second) // let watcher process the event

	files := pw.GetAllowed()
	found := false
	for _, f := range files {
		if f == "b.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'b.txt' to be in allowed files after refresh: got %v", files)
	}
}
