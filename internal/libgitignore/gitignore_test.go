package libgitignore_test

import (
	"os"
	"path/filepath"
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

	gi, err := libgitignore.CompileIgnoreLines(patterns, "./", 1)
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
	gi, err := libgitignore.CompileIgnoreLines(patterns, "./", 1)
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
		got := gi.MatchesPath(path)
		if got != want {
			t.Errorf("Matches(%q) = %v; want %v", path, got, want)
		}
	}
}

func TestGitIgnore_SubdirAndParentGitignore(t *testing.T) {
	tmp := t.TempDir()

	// 1. root .gitignore
	rootGitignore := filepath.Join(tmp, ".gitignore")
	err := os.WriteFile(rootGitignore, []byte("*.log\n!keep.log\nfoo/\n"), 0644)
	if err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	// 2. sub dir .gitignore
	subdir := filepath.Join(tmp, "sub")
	err = os.Mkdir(subdir, 0755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	subGitignore := filepath.Join(subdir, ".gitignore")
	err = os.WriteFile(subGitignore, []byte("*.txt\nbar/\n!note.txt\n"), 0644)
	if err != nil {
		t.Fatalf("write sub/.gitignore: %v", err)
	}

	gi, err := libgitignore.GenerateIntegratedGitIgnore(true, tmp, []string{})
	if err != nil {
		t.Fatalf("GenerateIntegratedGitIgnore: %v", err)
	}

	tests := []struct {
		path     string
		expected bool
		fromDir  string
	}{
		{"foo/bar.log", true, tmp},                            // ルート.gitignore "*.log"
		{"keep.log", false, ""},                               // ルート.gitignore "!keep.log" (除外)
		{"sub/file.txt", true, filepath.Join(tmp, "sub")},     // サブdir .gitignore "*.txt"
		{"sub/note.txt", false, ""},                           // sub/.gitignore "!note.txt"
		{"sub/bar/data.txt", true, filepath.Join(tmp, "sub")}, // sub/.gitignore "*.txt"
		{"foo/baz.txt", true, ""},                             // ルート.gitignore効かない（*.txt無い）
		{"sub/bar/baz.log", true, filepath.Join(tmp, "sub")},  // ルート.gitignore "*.log"適用
		{"sub/bar/abc.md", true, ""},                          // どちらにもマッチしない
		{"sub/bar/", true, filepath.Join(tmp, "sub")},         // sub/.gitignore "bar/"
		{"foo/", true, tmp},                                   // ルート.gitignore "foo/"
	}

	for _, tc := range tests {
		absPath := filepath.Join(tmp, tc.path)
		relPath, err := filepath.Rel(tmp, absPath)
		if err != nil {
			t.Fatalf("filepath.Rel: %v", err)
		}
		matched, pat := gi.MatchesPathHow(relPath)
		t.Logf("path=%q, matched=%v, pat=%v (dir=%v)", tc.path, matched, pat.Raw, pat.Dir)

		if matched != tc.expected {
			t.Errorf("MatchesPathHow(%q) = %v; want %v", tc.path, matched, tc.expected)
		}
		// by match
		if matched && tc.fromDir != "" && pat != nil && pat.Dir != tc.fromDir {
			t.Errorf("Pattern for %q: got Dir=%q, want Dir=%q", tc.path, pat.Dir, tc.fromDir)
		}
	}
}
