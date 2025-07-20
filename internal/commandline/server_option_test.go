package commandline_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

func TestServerOptParse_Basic(t *testing.T) {
	args := []string{
		"--root", "/my/project",
		"--type", "http",
		"--http-port", "12345",
		"--scan-buffer", "20M",
		"--mask-secrets", "off",
		"--allow-gitignore", "off",
		"--additionally-ignorerule", "custom.ignore",
		"--ignore-dotfile", "on",
		"--pattern-regex", ".*\\.go",
		"--include-ext", ".go,.md",
		"--exclude-file-regex", "^test_",
		"--exclude-dir-regex", "^vendor$",
		"--exclude-ext", ".log",
		"--exclude-dir", "tmp",
		"--skip-non-utf8",
		"--delete-comment",
	}

	_, opt, err := commandline.ServerOptParse("v1.0.0", args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if opt.RootDir != "/my/project" {
		t.Errorf("RootDir mismatch. got=%s", opt.RootDir)
	}
	if opt.McpServerTypeValue != "http" {
		t.Errorf("McpServerTypeValue mismatch. got=%s", opt.McpServerTypeValue)
	}
	if opt.HttpPort != "12345" {
		t.Errorf("HttpPort mismatch. got=%s", opt.HttpPort)
	}
	if opt.GeneralOption.MaskSecretsFlagValue != "off" {
		t.Errorf("MaskSecretsFlagValue mismatch. got=%s", opt.GeneralOption.MaskSecretsFlagValue)
	}
	if opt.GeneralOption.AllowGitignoreFlagValue != "off" {
		t.Errorf("AllowGitignoreFlagValue mismatch. got=%s", opt.GeneralOption.AllowGitignoreFlagValue)
	}
	if opt.GeneralOption.AdditionallyIgnoreRuleFilenames != "custom.ignore" {
		t.Errorf("AdditionallyIgnoreRuleFilenames mismatch. got=%s", opt.GeneralOption.AdditionallyIgnoreRuleFilenames)
	}
	if opt.GeneralOption.IgnoreDotFileFlagValue != "on" {
		t.Errorf("IgnoreDotFileFlagValue mismatch. got=%s", opt.GeneralOption.IgnoreDotFileFlagValue)
	}
	if opt.GeneralOption.PatternRegexpString != ".*\\.go" {
		t.Errorf("PatternRegexpString mismatch. got=%s", opt.GeneralOption.PatternRegexpString)
	}
	if opt.GeneralOption.IncludeExt != ".go,.md" {
		t.Errorf("IncludeExt mismatch. got=%s", opt.GeneralOption.IncludeExt)
	}
	if opt.GeneralOption.ExcludeFileRegexpString != "^test_" {
		t.Errorf("ExcludeFileRegexpString mismatch. got=%s", opt.GeneralOption.ExcludeFileRegexpString)
	}
	if opt.GeneralOption.ExcludeDirRegexpString != "^vendor$" {
		t.Errorf("ExcludeDirRegexpString mismatch. got=%s", opt.GeneralOption.ExcludeDirRegexpString)
	}
	if opt.GeneralOption.ExcludeExt != ".log" {
		t.Errorf("ExcludeExt mismatch. got=%s", opt.GeneralOption.ExcludeExt)
	}
	if opt.GeneralOption.ExcludeDir != "tmp" {
		t.Errorf("ExcludeDir mismatch. got=%s", opt.GeneralOption.ExcludeDir)
	}
	if !opt.GeneralOption.SkipNonUTF8Flag {
		t.Errorf("SkipNonUTF8Flag should be true")
	}
	if !opt.GeneralOption.DeleteCommentsFlag {
		t.Errorf("DeleteCommentsFlag should be true")
	}
}
