// Package testfilestest holds the representative paths every consumer of
// internal/testfiles is checked against, so that no consumer answers "is this
// file a test?" differently from the shared classifier.
package testfilestest

// Paths covers each language's conventions and their tricky negatives.
var Paths = []string{
	// Go
	"pkg/foo_test.go", "pkg/contest.go", "test_foo.go", "pkg/latest.go",
	// TypeScript / TSX / JavaScript
	"src/foo.test.ts", "src/foo.spec.tsx", "src/foo_test.ts", "src/latest.ts",
	"src/foo.test.js", "src/foo_test.js", "src/test_foo.ts",
	// Python
	"pkg/test_foo.py", "pkg/foo_test.py", "pkg/latest.py", "test_foo.py",
	// PHP
	"tests/Feature/LoginTest.php", "tests/TestCase.php", "src/FooTest.php",
	"src/Test.php", "src/foo_test.php", "Contest.php", "app/Contest.php",
	"src/Tests/Unit/Helper.php", "test_helper.php",
	// Test fixture data
	"internal/x/testdata/a.go", "testdata/b.php", "mytestdata/c.go", "pkg/testdatafile.go",
}
