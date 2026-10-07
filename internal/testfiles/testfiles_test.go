package testfiles

import (
	"testing"

	"github.com/magicdrive/ark/internal/testfiles/testfilestest"
)

// The documented policy (see IsTestFile): each language's conventions only.
// Deno-style foo_test.ts and PHP foo_test.php are not conventions of those
// languages here, so they are production files.
func TestIsTestFile(t *testing.T) {
	want := map[string]bool{
		"pkg/foo_test.go": true, "pkg/contest.go": false, "test_foo.go": false, "pkg/latest.go": false,
		"src/foo.test.ts": true, "src/foo.spec.tsx": true, "src/foo_test.ts": false, "src/latest.ts": false,
		"src/foo.test.js": true, "src/foo_test.js": false, "src/test_foo.ts": false,
		"pkg/test_foo.py": true, "pkg/foo_test.py": true, "pkg/latest.py": false, "test_foo.py": true,
		"tests/Feature/LoginTest.php": true, "tests/TestCase.php": true, "src/FooTest.php": true,
		"src/Test.php": false, "src/foo_test.php": false, "Contest.php": false, "app/Contest.php": false,
		"src/Tests/Unit/Helper.php": true, "test_helper.php": false,
		"internal/x/testdata/a.go": false, "testdata/b.php": false, "mytestdata/c.go": false, "pkg/testdatafile.go": false,
		// Earlier cases.
		"pkg/a_test.go": true, "src/a.spec.js": true, "src/test.ts": false,
		"app/Http/Controllers/Test.php": false, "app/Testing/Fake.php": false, "app/LatestTest.php": true, "app/attest/X.php": false,
	}
	for _, p := range testfilestest.Paths {
		if _, ok := want[p]; !ok {
			t.Errorf("no expectation for shared path %q", p)
		}
	}
	for path, w := range want {
		if got := IsTestFile(path); got != w {
			t.Errorf("IsTestFile(%q) = %v, want %v", path, got, w)
		}
	}
}

func TestIsTestData(t *testing.T) {
	for path, w := range map[string]bool{
		"internal/x/testdata/a.go": true, "testdata/b.php": true,
		"mytestdata/c.go": false, "pkg/testdatafile.go": false, "testdata": false, "pkg/foo_test.go": false,
	} {
		if got := IsTestData(path); got != w {
			t.Errorf("IsTestData(%q) = %v, want %v", path, got, w)
		}
	}
}
