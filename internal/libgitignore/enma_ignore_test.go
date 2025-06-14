package libgitignore_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/libgitignore"
)

func TestGitIgnore_Matches(t *testing.T) {
	patterns := []string{
		"*.log",
		"/build/",
		"temp/**",
		"docs/**/*.md",
		"!docs/README.md",
		`\!special.txt`,
		"dir with space/",
		"*.tmp",
		"node_modules/",
		"**/generated/*",
		"a/**/b/*.txt",
	}

	gi, err := libgitignore.CompileIgnoreLines(patterns...)
	if err != nil {
		t.Fatalf("failed to compile patterns: %v", err)
	}

	tests := []struct {
		path   string
		expect bool
		match  string
	}{
		{"debug.log", true, "*.log"},
		{"build/main.o", true, "/build/"},
		{"temp/cache/file.txt", true, "temp/**"},
		{"docs/chapter1/intro.md", true, "docs/**/*.md"},
		{"docs/README.md", false, "!docs/README.md"},
		{"!special.txt", true, "\\!special.txt"},
		{"dir with space/file.txt", true, "dir with space/"},
		{"src/main.go", false, ""},
		{"foo.tmp", true, "*.tmp"},
		{"node_modules/pkg/index.js", true, "node_modules/"},
		{"src/generated/code.go", true, "**/generated/*"},
		{"a/b/file.txt", true, "a/**/b/*.txt"},
		{"a/x/b/file.txt", true, "a/**/b/*.txt"},
		{"a/x/y/b/file.txt", true, "a/**/b/*.txt"},
		{"a/b/file.md", false, ""},
		{"docs/image.png", false, ""},
	}

	for _, tt := range tests {
		matched, pat := gi.MatchesPathHow(tt.path)
		if matched != tt.expect {
			t.Errorf("Matches(%q) = %v; want %v", tt.path, matched, tt.expect)
		} else if matched && pat != nil && pat.Raw != tt.match {
			t.Errorf("Matches(%q) matched pattern %q; want %q", tt.path, pat.Raw, tt.match)
		}
	}
}

func TestGitIgnore_ExtraCases(t *testing.T) {
	patterns := []string{
		"*.log",
		"temp-?.txt",
		"/secrets.txt",
		"logs/*.log",
		"!keep.log",
		"docs/**/README.md",
		"build/",
		"*.swp",
		"**/*.bak",
		"lib/**/test/*.go",
	}
	gi, err := libgitignore.CompileIgnoreLines(patterns...)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]bool{
		"debug.log":           true,
		"temp-a.txt":          true,
		"secrets.txt":         true,
		"src/secrets.txt":     false,
		"logs/error.log":      true,
		"keep.log":            false,
		"docs/README.md":      true,
		"docs/api/README.md":  true,
		"build/index.html":    true,
		"build/js/app.js":     true,
		"main.go.swp":         true,
		".vimrc.swp":          true,
		"tmp/old.bak":         true,
		"src/foo/bar.bak":     true,
		"lib/test/foo.go":     true,
		"lib/x/test/bar.go":   true,
		"lib/x/y/test/baz.go": true,
	}

	for path, want := range tests {
		got := gi.Matches(path)
		if got != want {
			t.Errorf("Matches(%q) = %v; want %v", path, got, want)
		}
	}
}
