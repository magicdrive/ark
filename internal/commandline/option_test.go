package commandline_test

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

func TestOptParse_ValidInputs(t *testing.T) {
	args := []string{
		"-o", "out.txt",
		"-b", "20K",
		"-a", "on",
		"-m", "on",
		"-p", "ignore1,.ignore2",
		"-n", "on",
		"-f", "md",
		"-d", "off",
		"-x", "^.*\\.go$",
		"-i", "go,md",
		"-g", ".*_test\\.go",
		"-G", "vendor",
		"-e", "exe,bin",
		"-E", "tmp,cache",
		"-c",
		"-s",
		"-S",
		"-D",
		"./example",
	}

	_, opt, err := commandline.OptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if opt.OutputFilename != "out.txt.arklite.txt" {
		t.Errorf("Expected OutputFilename = out.txt.arklite.txt, got %s", opt.OutputFilename)
	}
	if opt.ScanBuffer.String() != "20K" {
		t.Errorf("Expected ScanBuffer = 20K, got %s", opt.ScanBuffer.String())
	}
	if opt.WithLineNumberFlag.String() != "on" {
		t.Errorf("Expected WithLineNumberFlag = on, got %s", opt.WithLineNumberFlag.String())
	}
	if opt.IgnoreDotFileFlag.String() != "off" {
		t.Errorf("Expected IgnoreDotFileFlag = off, got %s", opt.IgnoreDotFileFlag.String())
	}
	if opt.OutputFormat.String() != "markdown" {
		t.Errorf("Expected OutputFormat = markdown, got %s", opt.OutputFormat.String())
	}
	if opt.TargetDirname != "./example" {
		t.Errorf("Expected TargetDirname = ./example, got %s", opt.TargetDirname)
	}
	if opt.ComplessFlag != true {
		t.Errorf("Expected ComplessFlag = true, got %t", opt.ComplessFlag)
	}
	if opt.SkipNonUTF8Flag != true {
		t.Errorf("Expected SkipNonUTF8Flag = true, got %t", opt.SkipNonUTF8Flag)
	}
	if opt.SilentFlag != true {
		t.Errorf("Expected SilentFlag = true, got %t", opt.SilentFlag)
	}
	if opt.DeleteCommentsFlag != true {
		t.Errorf("Expected DeleteCommentsFlag = true, got %t", opt.DeleteCommentsFlag)
	}
	expectList := []string{"go", "md"}
	if !reflect.DeepEqual(opt.IncludeExtList, expectList) {
		t.Errorf("IncludeExtList mismatch: expected %v, got %v", expectList, opt.IncludeExtList)
	}
}

func TestOptParse_InvalidRegex(t *testing.T) {
	args := []string{"-x", "[invalid"}

	_, _, err := commandline.OptParse(args)
	if err == nil {
		t.Fatal("Expected error due to invalid regexp, got nil")
	}
	if got := err.Error(); got == "" || !regexp.MustCompile(`failed to compile`).MatchString(got) {
		t.Errorf("Expected regex compile error, got: %v", got)
	}
}

func TestOptParse_Defaults(t *testing.T) {
	args := []string{}

	_, opt, err := commandline.OptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if opt.OutputFilename != "ark-output.txt" {
		t.Errorf("Expected default output filename, got %s", opt.OutputFilename)
	}
	if opt.WithLineNumberFlag.String() != "off" {
		t.Errorf("Expected WithLineNumberFlag default 'off', got %s", opt.WithLineNumberFlag.String())
	}
	if opt.OutputFormat.String() != "plaintext" {
		t.Errorf("Expected default output format = plaintext, got %s", opt.OutputFormat.String())
	}
	if opt.ComplessFlag != false {
		t.Errorf("Expected ComplessFlag = false, got %t", opt.ComplessFlag)
	}
	if opt.SkipNonUTF8Flag != false {
		t.Errorf("Expected SkipNonUTF8Flag = false, got %t", opt.SkipNonUTF8Flag)
	}
	if opt.SilentFlag != false {
		t.Errorf("Expected SilentFlag = false, got %t", opt.SilentFlag)
	}
	if opt.DeleteCommentsFlag != false {
		t.Errorf("Expected DeleteCommentsFlag = false, got %t", opt.DeleteCommentsFlag)
	}
}

func TestOptParse_HelpAndVersion(t *testing.T) {
	args := []string{"--help"}

	_, opt, err := commandline.OptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !opt.HelpFlag {
		t.Errorf("Expected HelpFlag to be true")
	}

	args = []string{"--version"}
	_, opt, err = commandline.OptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !opt.VersionFlag {
		t.Errorf("Expected VersionFlag to be true")
	}
}

func TestOptParse_CommaSeparatedListParsing(t *testing.T) {
	args := []string{
		"-i", "go,md",
		"-e", "exe,bin",
		"-E", "tmp,cache",
	}

	_, opt, err := commandline.OptParse(args)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expect := []string{"go", "md"}
	if !reflect.DeepEqual(opt.IncludeExtList, expect) {
		t.Errorf("IncludeExtList mismatch: expected %v, got %v", expect, opt.IncludeExtList)
	}
	expect = []string{"exe", "bin"}
	if !reflect.DeepEqual(opt.ExcludeExtList, expect) {
		t.Errorf("ExcludeExtList mismatch: expected %v, got %v", expect, opt.ExcludeExtList)
	}
	expect = []string{"tmp", "cache"}
	if !reflect.DeepEqual(opt.ExcludeDirList, expect) {
		t.Errorf("ExcludeDirList mismatch: expected %v, got %v", expect, opt.ExcludeDirList)
	}
}
