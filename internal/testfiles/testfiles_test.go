package testfiles

import "testing"

func TestIsTestFile(t *testing.T) {
	cases := map[string]bool{
		// Go / TypeScript / JavaScript / Python: the conventions the Context
		// Engine has always used.
		"pkg/a_test.go": true, "pkg/a.go": false, "pkg/latest.go": false,
		"src/a.test.ts": true, "src/a.spec.tsx": true, "src/a.ts": false, "src/test.ts": false,
		"src/a.spec.js": true, "src/a.js": false,
		"pkg/test_a.py": true, "pkg/a_test.py": true, "pkg/a.py": false, "pkg/latest.py": false,
		// PHP: PHPUnit naming and the tests/ layout.
		"tests/Feature/LoginTest.php":   true,
		"tests/TestCase.php":            true,
		"tests/Support/Helpers.php":     true,
		"src/Tests/Unit/FooTest.php":    true,
		"app/Services/UserService.php":  false,
		"app/Http/Controllers/Test.php": false,
		"app/Contest.php":               false,
		"app/Testing/Fake.php":          false,
		"app/LatestTest.php":            true,
		"app/attest/X.php":              false,
	}
	for path, want := range cases {
		if got := IsTestFile(path); got != want {
			t.Errorf("IsTestFile(%q) = %v, want %v", path, got, want)
		}
	}
}
