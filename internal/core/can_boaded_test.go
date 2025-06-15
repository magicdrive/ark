package core_test

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
	"github.com/magicdrive/ark/internal/libgitignore"
	"github.com/magicdrive/ark/internal/model"
)

func createTestOption() *commandline.Option {
	return &commandline.Option{
		PatternRegexpString:                "",
		IncludeExtList:                     []string{},
		ExcludeDirList:                     []string{},
		ExcludeExtList:                     []string{},
		GitIgnoreRule:                      nil,
		OutputFormatValue:                  "txt",
		WithLineNumberFlag:                 model.OnOffSwitch("on"),
		IgnoreDotFileFlag:                  model.OnOffSwitch("on"),
		AdditionallyIgnoreRuleFilenameList: []string{},
		WorkingDir:                         ".",
	}
}

func TestCanBoaded_BasicAllow(t *testing.T) {
	opt := createTestOption()
	allowed := core.CanBoaded(opt, "example.go")
	if !allowed {
		t.Errorf("Expected file to be allowed")
	}
}

func TestCanBoaded_PatternRegexp(t *testing.T) {
	opt := createTestOption()
	re := regexp.MustCompile(`^main.*\.go$`)
	opt.PatternRegexp = re

	if !core.CanBoaded(opt, "main.go") {
		t.Errorf("Expected main.go to match pattern")
	}
	if core.CanBoaded(opt, "util.go") {
		t.Errorf("Expected util.go to not match pattern")
	}
}

func TestCanBoaded_ExcludeDir(t *testing.T) {
	opt := createTestOption()
	opt.ExcludeDir = ".git"
	opt.ExcludeDirList = []string{".git"}
	path := filepath.Join(".git", "config.go")
	for dir := range strings.SplitSeq(filepath.ToSlash(filepath.Dir(path)), "/") {
		if slices.Contains(opt.ExcludeDirList, dir) {
			if core.CanBoaded(opt, path) {
				t.Errorf("Expected %s to be excluded", path)
			}
			return
		}
	}
	t.Errorf("No directory in path %s matched exclude list", path)
}

func TestCanBoaded_IncludeExt(t *testing.T) {
	opt := createTestOption()
	opt.IncludeExt = ".go"
	opt.IncludeExtList = []string{".go"}

	if !core.CanBoaded(opt, "foo.go") {
		t.Errorf("Expected .go file to be included")
	}
	if core.CanBoaded(opt, "foo.txt") {
		t.Errorf("Expected .txt file to be excluded")
	}
}

func TestCanBoaded_ExcludeDirRegexp(t *testing.T) {
	opt := createTestOption()
	re := regexp.MustCompile(`.*/tmp($|/)`)
	opt.ExcludeDirRegexp = re
	abs := filepath.Join("/tmp", "file.go")
	if core.CanBoaded(opt, abs) {
		t.Errorf("Expected file in /tmp to be excluded")
	}
}

func TestCanBoaded_ExcludeFileRegexp(t *testing.T) {
	opt := createTestOption()
	re := regexp.MustCompile(`_test\.go$`)
	opt.ExcludeFileRegexp = re
	if core.CanBoaded(opt, "example_test.go") {
		t.Errorf("Expected _test.go to be excluded")
	}
}

func TestCanBoaded_ExcludeExt(t *testing.T) {
	opt := createTestOption()
	opt.ExcludeExt = ".log"
	opt.ExcludeExtList = []string{".log"}
	if core.CanBoaded(opt, "debug.log") {
		t.Errorf("Expected .log file to be excluded")
	}
}

func TestCanBoaded_GitIgnore(t *testing.T) {
	opt := createTestOption()
	matcher, err := libgitignore.CompileIgnoreLines("*.tmp")
	if err != nil {
		t.Fatalf("Failed to compile gitignore: %v", err)
	}
	opt.GitIgnoreRule = matcher
	if core.CanBoaded(opt, "temp.tmp") {
		t.Errorf("Expected .tmp file to be ignored by gitignore")
	}
}

func TestCanBoaded_GitIgnore_NoMatch(t *testing.T) {
	opt := createTestOption()
	matcher, err := libgitignore.CompileIgnoreLines("*.log")
	if err != nil {
		t.Fatalf("Failed to compile gitignore: %v", err)
	}
	opt.GitIgnoreRule = matcher
	if !core.CanBoaded(opt, "main.go") {
		t.Errorf("Expected main.go to be allowed")
	}
}
