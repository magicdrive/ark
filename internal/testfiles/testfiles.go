// Package testfiles recognises test files by the static, repository-visible
// conventions of each supported language — file names and directory layout
// only; no test-runner configuration is read or executed. It is the single
// authority on "is this file a test?" for every consumer: the Context Engine,
// impact analysis, the repository map, structural search and the skill
// analyzer. No consumer adds naming rules of its own.
//
// Two questions, two classifiers:
//
//	IsTestFile  a test file (or test support code a language keeps with its
//	            tests) — test callers, test packages, includeTests;
//	IsTestData  a file under a testdata directory: fixture input read by tests,
//	            not code of the repository — consumers that leave fixtures out
//	            (impact, the repository map, search's excludeTests) ask both.
package testfiles

import (
	"path/filepath"
	"strings"
)

// IsTestFile reports whether the repository-relative path names a test file
// (or, where a language keeps test support code in a test directory, a file
// of that directory) by its language's conventions:
//
//	Go          *_test.go
//	TypeScript  *.test.ts, *.spec.ts, *.test.tsx, *.spec.tsx
//	JavaScript  *.test.js, *.spec.js
//	Python      test_*.py, *_test.py
//	PHP         FooTest.php (PHPUnit), or any file under a tests/ or test/
//	            directory (PHPUnit's layout, which also holds the suite's
//	            base classes and helpers)
func IsTestFile(path string) bool {
	slashed := filepath.ToSlash(path)
	base := slashed[strings.LastIndex(slashed, "/")+1:]
	switch strings.ToLower(filepath.Ext(base)) {
	case ".go":
		return strings.HasSuffix(base, "_test.go")
	case ".ts", ".tsx", ".js":
		for _, suf := range []string{".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx", ".test.js", ".spec.js"} {
			if strings.HasSuffix(base, suf) {
				return true
			}
		}
	case ".py":
		return strings.HasPrefix(base, "test_") || strings.HasSuffix(strings.TrimSuffix(base, ".py"), "_test")
	case ".php":
		if strings.HasSuffix(base, "Test.php") && len(base) > len("Test.php") {
			return true // FooTest.php; a class named Test is not a test
		}
		dirs := strings.Split(slashed, "/")
		for _, d := range dirs[:len(dirs)-1] {
			switch d {
			case "tests", "Tests", "test":
				return true
			}
		}
	}
	return false
}

// IsTestData reports whether the repository-relative path lies under a
// directory named testdata (the Go toolchain's fixture convention, used by
// other ecosystems too). The name must be a whole path component.
func IsTestData(path string) bool {
	dirs := strings.Split(filepath.ToSlash(path), "/")
	for _, d := range dirs[:len(dirs)-1] {
		if d == "testdata" {
			return true
		}
	}
	return false
}
