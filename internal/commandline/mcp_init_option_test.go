package commandline_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

func TestMCPInitOptParse_Defaults(t *testing.T) {
	_, opt, err := commandline.MCPInitOptParse([]string{})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.ArkPath != "" {
		t.Errorf("ArkPath: expected empty, got %s", opt.ArkPath)
	}
	if opt.ServerName != "ark" {
		t.Errorf("ServerName: expected 'ark', got %s", opt.ServerName)
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

func TestMCPInitOptParse_AllFlags(t *testing.T) {
	args := []string{
		"--ark-path", "/usr/bin/ark",
		"--root", "/tmp/project",
		"--name", "my-ark",
		"--global",
		"--force",
	}
	_, opt, err := commandline.MCPInitOptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.ArkPath != "/usr/bin/ark" {
		t.Errorf("ArkPath: expected /usr/bin/ark, got %s", opt.ArkPath)
	}
	if opt.RootDir != "/tmp/project" {
		t.Errorf("RootDir: expected /tmp/project, got %s", opt.RootDir)
	}
	if opt.ServerName != "my-ark" {
		t.Errorf("ServerName: expected my-ark, got %s", opt.ServerName)
	}
	if !opt.GlobalFlag {
		t.Errorf("GlobalFlag: expected true")
	}
	if !opt.ForceFlag {
		t.Errorf("ForceFlag: expected true")
	}
}

func TestMCPInitOptParse_ShortFlags(t *testing.T) {
	args := []string{"-p", "/bin/ark", "-r", "/proj", "-n", "myname", "-g", "-f"}
	_, opt, err := commandline.MCPInitOptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if opt.ArkPath != "/bin/ark" {
		t.Errorf("ArkPath: expected /bin/ark, got %s", opt.ArkPath)
	}
	if opt.RootDir != "/proj" {
		t.Errorf("RootDir: expected /proj, got %s", opt.RootDir)
	}
	if opt.ServerName != "myname" {
		t.Errorf("ServerName: expected myname, got %s", opt.ServerName)
	}
	if !opt.GlobalFlag {
		t.Errorf("GlobalFlag (-g): expected true")
	}
	if !opt.ForceFlag {
		t.Errorf("ForceFlag (-f): expected true")
	}
}

func TestMCPInitOptParse_Help(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		_, opt, err := commandline.MCPInitOptParse([]string{flag})
		if err != nil {
			t.Fatalf("%s: Expected no error, got: %v", flag, err)
		}
		if !opt.HelpFlag {
			t.Errorf("%s: HelpFlag expected true", flag)
		}
	}
}
