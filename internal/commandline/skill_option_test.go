package commandline_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

func TestSkillAutoOptParse_Defaults(t *testing.T) {
	_, opt, err := commandline.SkillAutoOptParse([]string{})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.Name != "" {
		t.Errorf("Name: expected empty, got %s", opt.Name)
	}
	if opt.Output != "" {
		t.Errorf("Output: expected empty, got %s", opt.Output)
	}
	if opt.ArchiveFlag {
		t.Errorf("ArchiveFlag: expected false")
	}
	if opt.ForceFlag {
		t.Errorf("ForceFlag: expected false")
	}
	if opt.HelpFlag {
		t.Errorf("HelpFlag: expected false")
	}
}

func TestSkillAutoOptParse_AllFlags(t *testing.T) {
	args := []string{"--name", "my-skill", "--output", "./skills", "--archive", "--force"}
	_, opt, err := commandline.SkillAutoOptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.Name != "my-skill" {
		t.Errorf("Name: expected my-skill, got %s", opt.Name)
	}
	if opt.Output != "./skills" {
		t.Errorf("Output: expected ./skills, got %s", opt.Output)
	}
	if !opt.ArchiveFlag {
		t.Errorf("ArchiveFlag: expected true")
	}
	if !opt.ForceFlag {
		t.Errorf("ForceFlag: expected true")
	}
}

func TestSkillAutoOptParse_Help(t *testing.T) {
	_, opt, err := commandline.SkillAutoOptParse([]string{"--help"})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !opt.HelpFlag {
		t.Errorf("HelpFlag: expected true")
	}

	_, opt, err = commandline.SkillAutoOptParse([]string{"-h"})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !opt.HelpFlag {
		t.Errorf("HelpFlag (-h): expected true")
	}
}

func TestSkillInitOptParse_Defaults(t *testing.T) {
	_, opt, err := commandline.SkillInitOptParse([]string{})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.Name != "repository-development" {
		t.Errorf("Name: expected repository-development, got %s", opt.Name)
	}
	if opt.Output != "" {
		t.Errorf("Output: expected empty, got %s", opt.Output)
	}
	if opt.ArchiveFlag {
		t.Errorf("ArchiveFlag: expected false")
	}
}

func TestSkillInitOptParse_AllFlags(t *testing.T) {
	args := []string{"--name", "my-repo", "--output", "./out", "--archive"}
	_, opt, err := commandline.SkillInitOptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.Name != "my-repo" {
		t.Errorf("Name: expected my-repo, got %s", opt.Name)
	}
	if opt.Output != "./out" {
		t.Errorf("Output: expected ./out, got %s", opt.Output)
	}
	if !opt.ArchiveFlag {
		t.Errorf("ArchiveFlag: expected true")
	}
}

func TestSkillAddExplorerOptParse_Defaults(t *testing.T) {
	_, opt, err := commandline.SkillAddExplorerOptParse([]string{})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.Name != "ark-code-explorer" {
		t.Errorf("Name: expected ark-code-explorer, got %s", opt.Name)
	}
}

func TestSkillUpdateOptParse_Defaults(t *testing.T) {
	_, opt, err := commandline.SkillUpdateOptParse([]string{})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.ForceFlag {
		t.Errorf("ForceFlag: expected false")
	}
	if opt.DryRunFlag {
		t.Errorf("DryRunFlag: expected false")
	}
}

func TestSkillUpdateOptParse_AllFlags(t *testing.T) {
	_, opt, err := commandline.SkillUpdateOptParse([]string{"--force", "--dry-run"})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !opt.ForceFlag {
		t.Errorf("ForceFlag: expected true")
	}
	if !opt.DryRunFlag {
		t.Errorf("DryRunFlag: expected true")
	}
}

func TestSkillInspectOptParse_Defaults(t *testing.T) {
	_, opt, err := commandline.SkillInspectOptParse([]string{})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.HelpFlag {
		t.Errorf("HelpFlag: expected false")
	}
}

func TestSkillInspectOptParse_Help(t *testing.T) {
	_, opt, err := commandline.SkillInspectOptParse([]string{"-h"})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !opt.HelpFlag {
		t.Errorf("HelpFlag: expected true")
	}
}
