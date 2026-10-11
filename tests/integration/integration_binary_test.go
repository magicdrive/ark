package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// The integration tests run the real ark binary. It is built once per test
// process, when a test first needs it, and shared: only the executable is
// shared — no test writes to it — while each test runs it with its own
// fixture, HOME, working directory and environment. The binary is the
// module's main package, two directories up from this one (go test runs a
// package's tests in its own directory).
var arkBinary = sharedBinary{pkg: filepath.Join("..", "..")}

func TestMain(m *testing.M) {
	arkBinary.snapshot()
	code := m.Run()
	if err := arkBinary.remove(); err != nil {
		fmt.Fprintf(os.Stderr, "integration tests: removing the shared ark binary: %v\n", err)
	}
	os.Exit(code)
}

// sharedBinary builds a main package at most once per test process.
type sharedBinary struct {
	pkg string // the main package, relative to the test's working directory

	// The build runs in the environment and working directory the test
	// process started with, not in those of the test that happens to need the
	// binary first (t.Setenv, t.Chdir).
	env []string
	wd  string

	once sync.Once
	dir  string // the temporary directory holding the binary
	path string
	err  error
}

func (b *sharedBinary) snapshot() {
	b.env = os.Environ()
	b.wd, b.err = os.Getwd()
}

// get returns the binary's path, building it on first use; a failed build
// fails every test that needs the binary, with the build's output.
func (b *sharedBinary) get(t *testing.T) string {
	t.Helper()
	b.once.Do(b.build)
	if b.err != nil {
		t.Fatalf("build ark: %v", b.err)
	}
	return b.path
}

func (b *sharedBinary) build() {
	if b.err != nil {
		return
	}
	dir, err := os.MkdirTemp("", "ark-itest-")
	if err != nil {
		b.err = err
		return
	}
	b.dir = dir
	path := filepath.Join(dir, "ark-itest")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", path, b.pkg)
	cmd.Env, cmd.Dir = b.env, b.wd
	if out, err := cmd.CombinedOutput(); err != nil {
		b.err = fmt.Errorf("%v\n%s", err, out)
		return
	}
	b.path = path
}

// remove deletes the binary once every test, and every process a test
// started, is done.
func (b *sharedBinary) remove() error {
	if b.dir == "" {
		return nil
	}
	return os.RemoveAll(b.dir)
}
