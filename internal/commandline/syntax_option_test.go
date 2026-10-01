package commandline_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

func TestSyntaxOptParse_Defaults(t *testing.T) {
	_, opt, err := commandline.SyntaxOptParse([]string{"main.go"})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.FilePath != "main.go" {
		t.Errorf("FilePath: expected main.go, got %s", opt.FilePath)
	}
	if opt.Lang != "" {
		t.Errorf("Lang: expected empty, got %s", opt.Lang)
	}
	if opt.Format != "text" {
		t.Errorf("Format: expected text, got %s", opt.Format)
	}
	if opt.HelpFlag {
		t.Errorf("HelpFlag: expected false")
	}
}

func TestSyntaxOptParse_AllFlags(t *testing.T) {
	args := []string{"--lang", "typescript", "--format", "json", "app.ts"}
	_, opt, err := commandline.SyntaxOptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.FilePath != "app.ts" {
		t.Errorf("FilePath: expected app.ts, got %s", opt.FilePath)
	}
	if opt.Lang != "typescript" {
		t.Errorf("Lang: expected typescript, got %s", opt.Lang)
	}
	if opt.Format != "json" {
		t.Errorf("Format: expected json, got %s", opt.Format)
	}
}

func TestSyntaxOptParse_Help(t *testing.T) {
	_, opt, err := commandline.SyntaxOptParse([]string{"--help"})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !opt.HelpFlag {
		t.Errorf("HelpFlag: expected true")
	}

	_, opt, err = commandline.SyntaxOptParse([]string{"-h"})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !opt.HelpFlag {
		t.Errorf("HelpFlag (-h): expected true")
	}
}

func TestSyntaxOptParse_InvalidFormat(t *testing.T) {
	_, _, err := commandline.SyntaxOptParse([]string{"--format", "xml", "main.go"})
	if err == nil {
		t.Fatal("Expected error for invalid --format, got nil")
	}
}

func TestSyntaxOptParse_NoArgs(t *testing.T) {
	_, opt, err := commandline.SyntaxOptParse([]string{})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.FilePath != "" {
		t.Errorf("FilePath: expected empty, got %s", opt.FilePath)
	}
}

func TestSymbolOptParse_Defaults(t *testing.T) {
	_, opt, err := commandline.SymbolOptParse([]string{"main.go"})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.FilePath != "main.go" {
		t.Errorf("FilePath: expected main.go, got %s", opt.FilePath)
	}
	if opt.Format != "text" {
		t.Errorf("Format: expected text, got %s", opt.Format)
	}
}

func TestSymbolOptParse_AllFlags(t *testing.T) {
	args := []string{"--lang", "python", "--format", "json", "app.py"}
	_, opt, err := commandline.SymbolOptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.FilePath != "app.py" {
		t.Errorf("FilePath: expected app.py, got %s", opt.FilePath)
	}
	if opt.Lang != "python" {
		t.Errorf("Lang: expected python, got %s", opt.Lang)
	}
	if opt.Format != "json" {
		t.Errorf("Format: expected json, got %s", opt.Format)
	}
}

func TestSymbolOptParse_InvalidFormat(t *testing.T) {
	_, _, err := commandline.SymbolOptParse([]string{"--format", "plaintext", "main.go"})
	if err == nil {
		t.Fatal("Expected error for invalid --format, got nil")
	}
}
