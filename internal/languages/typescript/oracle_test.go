package typescript

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// The compiler-backed tests compare Ark with the TypeScript compiler. They
// need Node.js and the typescript package, which Ark itself never does, so
// they are opt-in:
//
//	ARK_TYPESCRIPT_MODULE   the typescript package directory; unset: skip
//	ARK_TS_ORACLE_REQUIRED  "1": never skip — a missing or unloadable
//	                        compiler fails the test instead (CI)
//	ARK_TYPESCRIPT_VERSION  the version the loaded compiler must report
//
// See .github/ts-oracle for the CI setup, which pins the compiler.

// tsOracle returns node and the typescript module directory, or skips (fails
// when ARK_TS_ORACLE_REQUIRED=1) when there is no usable compiler.
func tsOracle(t *testing.T) (node, tsmod string) {
	t.Helper()
	required := os.Getenv("ARK_TS_ORACLE_REQUIRED") == "1"
	unavailable := func(why string) {
		t.Helper()
		if required {
			t.Fatalf("ARK_TS_ORACLE_REQUIRED=1 but %s", why)
		}
		t.Skip(why)
	}
	tsmod = os.Getenv("ARK_TYPESCRIPT_MODULE")
	if tsmod == "" {
		unavailable("ARK_TYPESCRIPT_MODULE not set")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		unavailable("node not found")
	}
	out, err := exec.Command(node, "-e", "process.stdout.write(require(process.argv[1]).version)", tsmod).CombinedOutput()
	if err != nil {
		// A module that is set but cannot be loaded is a broken setup.
		t.Fatalf("cannot load the TypeScript compiler from %s: %v\n%s", tsmod, err, out)
	}
	got := strings.TrimSpace(string(out))
	if want := os.Getenv("ARK_TYPESCRIPT_VERSION"); want != "" && got != want {
		t.Fatalf("TypeScript compiler %s loaded, %s expected", got, want)
	}
	t.Logf("TypeScript compiler %s (%s)", got, tsmod)
	return node, tsmod
}

// envInt reads a positive integer from the environment, or def.
func envInt(t *testing.T, name string, def int) int {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		t.Fatalf("%s=%q: want a positive integer", name, v)
	}
	return n
}
