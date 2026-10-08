package main

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// knowledgeDocs are the repository's design documents. Their value is that
// every rule names its authority (a test or a source file), so a renamed test
// or moved file must fail here rather than leave the document pointing at
// nothing. Only tracked, public documents belong here: local development
// files (AGENTS.md, CLAUDE.md, ...) are git-ignored and may not exist.
var knowledgeDocs = []string{
	"ARCHITECTURE.md",
	"internal/conformance/IMPROVEMENTS.md",
}

var (
	backticked = regexp.MustCompile("`([^`\n]+)`")
	testFunc   = regexp.MustCompile(`(?m)^func (Test\w+)\(`)
	testRef    = regexp.MustCompile(`^Test[A-Za-z0-9_*]+$`)
	pathRef    = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)
)

// TestKnowledgeDocsReferencesExist checks that every `TestName` (globs
// allowed) and every `path/to/file.go`-style reference in the knowledge
// documents names something that exists. Paths are tried from the repository
// root and from internal/.
func TestKnowledgeDocsReferencesExist(t *testing.T) {
	tests := collectTestNames(t)
	for _, doc := range knowledgeDocs {
		data, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		for _, m := range backticked.FindAllStringSubmatch(string(data), -1) {
			ref := m[1]
			switch {
			case testRef.MatchString(ref):
				if !anyTestMatches(tests, ref) {
					t.Errorf("%s: test %s does not exist", doc, ref)
				}
			case pathRef.MatchString(ref) && looksLikePath(ref):
				if !exists(ref) && !exists(filepath.Join("internal", ref)) {
					t.Errorf("%s: path %s does not exist", doc, ref)
				}
			}
		}
	}
}

func looksLikePath(ref string) bool {
	return strings.HasSuffix(ref, ".go") || strings.HasSuffix(ref, ".md") || strings.HasPrefix(ref, "internal/")
}

func exists(p string) bool {
	_, err := os.Stat(filepath.FromSlash(p))
	return err == nil
}

func anyTestMatches(tests []string, pattern string) bool {
	for _, name := range tests {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

func collectTestNames(t *testing.T) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); p != "." && (strings.HasPrefix(n, ".") || n == "testdata" || n == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range testFunc.FindAllStringSubmatch(string(src), -1) {
			names = append(names, m[1])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("collect test names: %v", err)
	}
	return names
}
