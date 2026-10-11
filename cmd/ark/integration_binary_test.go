package ark

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// The *_RealBinary tests run the real ark binary (the module's main package).
// It is built once per test process, when a test first needs it, and shared:
// only the executable is shared — no test writes to it — while each test runs
// it with its own repository, HOME and environment.
//
// Under go test -race the binary is built with -race too, so the processes the
// tests start run under the race detector.
var arkBinary = sharedBinary{pkg: filepath.Join("..", ".."), race: raceEnabled}

func TestMain(m *testing.M) {
	arkBinary.snapshot()
	if err := arkBinary.watchRaces(); err != nil {
		fmt.Fprintf(os.Stderr, "cmd/ark tests: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	if arkBinary.unreportedRaces() && code == 0 {
		code = 1
	}
	if err := arkBinary.remove(); err != nil {
		fmt.Fprintf(os.Stderr, "cmd/ark tests: removing the shared ark binary: %v\n", err)
	}
	os.Exit(code)
}

// sharedBinary builds a main package at most once per test process.
type sharedBinary struct {
	pkg  string // the main package, relative to the test's working directory
	race bool   // build with -race and fail the tests whose processes race

	// The build runs in the environment and working directory the test
	// process started with, not in those of the test that happens to need the
	// binary first (t.Setenv, t.Chdir).
	env []string
	wd  string

	once sync.Once
	dir  string // the temporary directory holding the binary
	path string
	err  error

	// With race, the race detector of every process started from the binary
	// writes its reports to raceDir/report.<pid> (GORACE log_path) instead of
	// stderr: a report survives a process that is killed, and never mixes
	// with a process's output (the MCP protocol on stdout).
	raceDir string
	raceMu  sync.Mutex
}

func (b *sharedBinary) snapshot() {
	b.env = os.Environ()
	b.wd, b.err = os.Getwd()
}

// watchRaces points the race detector of the processes the tests start (they
// inherit this process's environment) at raceDir, through GORACE.
func (b *sharedBinary) watchRaces() error {
	if !b.race {
		return nil
	}
	dir, err := os.MkdirTemp("", "ark-cmdtest-race-")
	if err != nil {
		return err
	}
	b.raceDir = dir
	gorace, err := goraceFor(os.Getenv("GORACE"), filepath.Join(dir, "report"))
	if err != nil {
		return err
	}
	return os.Setenv("GORACE", gorace)
}

// goraceFor returns the GORACE of the processes the tests start. Later
// options win: atexit_sleep_ms=0 drops the second each raced process
// otherwise sleeps at exit (for other goroutines to finish reporting), which
// with a process per CLI call multiplies a run's time — a race found before
// exit is still in the report; the caller's GORACE comes next and can set it
// back; log_path comes last, as the reports must land at reportPath.
//
// The race detector's option parser (sanitizer_common) ends a value at a
// space, a comma or a colon — the "C:" of a Windows path among them — unless
// the value is quoted. Nothing is escaped inside the quotes, so the path
// cannot hold a double quote (no Windows path can).
func goraceFor(caller, reportPath string) (string, error) {
	if strings.Contains(reportPath, `"`) {
		return "", fmt.Errorf("the race report path %q contains a double quote, which GORACE log_path cannot hold", reportPath)
	}
	opts := []string{"atexit_sleep_ms=0"}
	if caller = strings.TrimSpace(caller); caller != "" {
		opts = append(opts, caller)
	}
	return strings.Join(append(opts, `log_path="`+reportPath+`"`), " "), nil
}

// get returns the binary's path, building it on first use; a failed build
// fails every test that needs the binary, with the build's output. With race,
// the test fails, once every process it started is done (the cleanups that
// stop them run first), if any of them raced.
func (b *sharedBinary) get(t *testing.T) string {
	t.Helper()
	b.once.Do(b.build)
	if b.err != nil {
		t.Fatalf("go build: %v", b.err)
	}
	if b.raceDir != "" {
		t.Cleanup(func() {
			for _, r := range b.takeRaceReports() {
				t.Errorf("data race in an ark process this test started (%s):\n%s", r.name, r.text)
			}
		})
	}
	return b.path
}

func (b *sharedBinary) build() {
	if b.err != nil {
		return
	}
	dir, err := os.MkdirTemp("", "ark-cmdtest-")
	if err != nil {
		b.err = err
		return
	}
	b.dir = dir
	path := filepath.Join(dir, "ark")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	var flags []string
	if b.race {
		flags = append(flags, "-race")
	}
	if _, err := b.recordInputs(flags); err != nil {
		b.err = err
		return
	}
	cmd := exec.Command("go", append(append([]string{"build"}, flags...), "-o", path, b.pkg)...)
	cmd.Env, cmd.Dir = b.env, b.wd
	if out, err := cmd.CombinedOutput(); err != nil {
		b.err = fmt.Errorf("%v\n%s", err, out)
		return
	}
	b.path = path
}

// recordInputs opens, in this process, every directory the build of pkg
// reads from: the directories of the main module's packages it compiles
// (go.mod and go.sum sit in the module root, the main package's) and those
// of their embedded files. go test caches a test's result against the files
// and directories the test itself opened — a directory by the name, size and
// time of each entry, so a file changed, added or removed there counts — and
// knows nothing of what go build, another process, reads: without this, a
// result cached before a change to ark's sources would be reused after it.
// It runs inside the first test that needs the binary, as go test records
// only what a test opens while m.Run runs; it returns the directories.
func (b *sharedBinary) recordInputs(flags []string) ([]string, error) {
	format := `{{if and .Module .Module.Main}}{{.Dir}}{{range .EmbedFiles}}{{"\t"}}{{.}}{{end}}{{"\n"}}{{end}}`
	cmd := exec.Command("go", append(append([]string{"list", "-deps"}, flags...), "-f", format, b.pkg)...)
	cmd.Env, cmd.Dir = b.env, b.wd
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -deps %s (the build's inputs, for go test's cache): %v\n%s", b.pkg, err, stderr.String())
	}
	dirs := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, "\t")
		if fields[0] == "" {
			continue
		}
		dirs[fields[0]] = true
		for _, embed := range fields[1:] {
			dirs[filepath.Dir(filepath.Join(fields[0], filepath.FromSlash(embed)))] = true
		}
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("go list -deps %s: no package of the main module", b.pkg)
	}
	var recorded []string
	for dir := range dirs {
		if _, err := os.ReadDir(dir); err != nil {
			return nil, fmt.Errorf("recording the build input %s: %v", dir, err)
		}
		recorded = append(recorded, dir)
	}
	sort.Strings(recorded)
	return recorded, nil
}

type raceReport struct{ name, text string }

// takeRaceReports returns, and removes, the race reports written so far.
func (b *sharedBinary) takeRaceReports() []raceReport {
	b.raceMu.Lock()
	defer b.raceMu.Unlock()
	// os.ReadDir, not filepath.Glob: the directory's own path is not a
	// pattern (a backslash or a bracket in it would make one).
	entries, _ := os.ReadDir(b.raceDir)
	var reports []raceReport
	for _, e := range entries { // sorted by name
		if !strings.HasPrefix(e.Name(), "report.") {
			continue
		}
		f := filepath.Join(b.raceDir, e.Name())
		text, err := os.ReadFile(f)
		if err != nil {
			text = []byte(fmt.Sprintf("(unreadable: %v)", err))
		}
		reports = append(reports, raceReport{e.Name(), string(text)})
		_ = os.Remove(f)
	}
	return reports
}

// unreportedRaces reports, on stderr, races no test claimed — a process that
// outlived the test that started it.
func (b *sharedBinary) unreportedRaces() bool {
	if b.raceDir == "" {
		return false
	}
	reports := b.takeRaceReports()
	for _, r := range reports {
		fmt.Fprintf(os.Stderr, "cmd/ark tests: data race in an ark process (%s):\n%s\n", r.name, r.text)
	}
	return len(reports) > 0
}

// remove deletes the binary and the race reports once every test, and every
// process a test started, is done.
func (b *sharedBinary) remove() error {
	var errs []error
	for _, dir := range []string{b.dir, b.raceDir} {
		if dir != "" {
			errs = append(errs, os.RemoveAll(dir))
		}
	}
	return errors.Join(errs...)
}
