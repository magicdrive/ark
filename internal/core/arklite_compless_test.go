package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/core"
	"github.com/magicdrive/ark/internal/model"
)

func TestCompless_SimpleTextFile(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "test.txt.arklite")
	origContent := `
		hello
		  
		  world
		foo
		bar
		  
	`
	if err := os.WriteFile(origPath, []byte(origContent), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	// compless
	err := core.Compless(origPath, model.PlainText)
	if err != nil {
		t.Fatalf("Compless failed: %v", err)
	}

	// Check the contents of the compressed file
	gotBytes, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	got := string(gotBytes)

	// Check that it contains part of the header and compressed content (blank lines removed + ␤ separator).
	if !strings.Contains(got, "Arklite Format Overview") {
		t.Errorf("header missing in output: %s", got)
	}
	if !strings.Contains(got, "@"+strings.TrimSuffix(origPath, ".arklite")) {
		t.Errorf("file dump section missing or wrong")
	}
	// Post-compression text normalization
	wantCompact := "hello␤world␤foo␤bar"
	if !strings.Contains(got, wantCompact) {
		t.Errorf("compact contents not found: want=%q got=%q", wantCompact, got)
	}
}

func TestCompless_SimpleXmlFile(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "test.xml.arklite")
	origContent := `
	<hoge>
		hello
		  
		  world
		foo
		bar
		</hoge>
		  
	`
	if err := os.WriteFile(origPath, []byte(origContent), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	// compless
	err := core.Compless(origPath, model.XML)
	if err != nil {
		t.Fatalf("Compless failed: %v", err)
	}

	// Check the contents of the compressed file
	gotBytes, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	got := string(gotBytes)

	// Check that it contains part of the header and compressed content (blank lines removed + ␤ separator).
	if !strings.Contains(got, "Arklite Format Overview") {
		t.Errorf("header missing in output: %s", got)
	}
	if !strings.Contains(got, "@"+strings.TrimSuffix(origPath, ".arklite")) {
		t.Errorf("file dump section missing or wrong")
	}
	// Post-compression text normalization
	wantCompact := "<hoge>␤hello␤world␤foo␤bar␤</hoge>"
	if !strings.Contains(got, wantCompact) {
		t.Errorf("compact contents not found: want=%q got=%q", wantCompact, got)
	}
}

func TestCompless_SimpleMarkdownFile(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "test.xml.arklite")
	origContent := `
	#Title
		hello
		  
		  world
		* foo
		bar
		
		  
	`
	if err := os.WriteFile(origPath, []byte(origContent), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	// compless
	err := core.Compless(origPath, model.XML)
	if err != nil {
		t.Fatalf("Compless failed: %v", err)
	}

	// Check the contents of the compressed file
	gotBytes, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	got := string(gotBytes)

	// Check that it contains part of the header and compressed content (blank lines removed + ␤ separator).
	if !strings.Contains(got, "Arklite Format Overview") {
		t.Errorf("header missing in output: %s", got)
	}
	if !strings.Contains(got, "@"+strings.TrimSuffix(origPath, ".arklite")) {
		t.Errorf("file dump section missing or wrong")
	}
	// Post-compression text normalization
	wantCompact := "#Title␤hello␤world␤* foo␤bar"
	if !strings.Contains(got, wantCompact) {
		t.Errorf("compact contents not found: want=%q got=%q", wantCompact, got)
	}
}

func TestCompless_FileNotExist(t *testing.T) {
	err := core.Compless("/path/to/nowhere.arklite", model.XML)
	if err == nil {
		t.Error("Compless should fail for missing file")
	}
}
