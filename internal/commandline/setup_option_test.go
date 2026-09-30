package commandline_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

func TestSetupOptParse_Defaults(t *testing.T) {
	_, opt, err := commandline.SetupOptParse([]string{})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.Name != "" {
		t.Errorf("Name: expected empty, got %s", opt.Name)
	}
	if opt.ArkPath != "" {
		t.Errorf("ArkPath: expected empty, got %s", opt.ArkPath)
	}
	if opt.GlobalFlag {
		t.Errorf("GlobalFlag: expected false")
	}
	if opt.ForceFlag {
		t.Errorf("ForceFlag: expected false")
	}
	if opt.HelpFlag {
		t.Errorf("HelpFlag: expected false")
	}
}

func TestSetupOptParse_AllFlags(t *testing.T) {
	args := []string{
		"--name", "my-project",
		"--ark-path", "/usr/bin/ark",
		"--root", "/tmp/project",
		"--global",
		"--force",
	}
	_, opt, err := commandline.SetupOptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.Name != "my-project" {
		t.Errorf("Name: expected my-project, got %s", opt.Name)
	}
	if opt.ArkPath != "/usr/bin/ark" {
		t.Errorf("ArkPath: expected /usr/bin/ark, got %s", opt.ArkPath)
	}
	if opt.RootDir != "/tmp/project" {
		t.Errorf("RootDir: expected /tmp/project, got %s", opt.RootDir)
	}
	if !opt.GlobalFlag {
		t.Errorf("GlobalFlag: expected true")
	}
	if !opt.ForceFlag {
		t.Errorf("ForceFlag: expected true")
	}
}

func TestSetupOptParse_ShortFlags(t *testing.T) {
	args := []string{"-n", "proj", "-p", "/bin/ark", "-r", "/dir", "-g", "-f"}
	_, opt, err := commandline.SetupOptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.Name != "proj" {
		t.Errorf("Name: expected proj, got %s", opt.Name)
	}
	if opt.ArkPath != "/bin/ark" {
		t.Errorf("ArkPath: expected /bin/ark, got %s", opt.ArkPath)
	}
	if opt.RootDir != "/dir" {
		t.Errorf("RootDir: expected /dir, got %s", opt.RootDir)
	}
	if !opt.GlobalFlag {
		t.Errorf("GlobalFlag (-g): expected true")
	}
	if !opt.ForceFlag {
		t.Errorf("ForceFlag (-f): expected true")
	}
}

func TestSetupOptParse_Help(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		_, opt, err := commandline.SetupOptParse([]string{flag})
		if err != nil {
			t.Fatalf("%s: Expected no error, got: %v", flag, err)
		}
		if !opt.HelpFlag {
			t.Errorf("%s: HelpFlag expected true", flag)
		}
	}
}
