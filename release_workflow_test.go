package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The release workflow must never publish what the quality gates have not
// passed. These checks pin its structure (GitHub Actions cannot be run here):
// a tag push runs the full CI and the release smoke, and GoReleaser publishes
// only in a job that needs both.

var jobHeader = regexp.MustCompile(`(?m)^  ([A-Za-z0-9_-]+):\s*$`)

// workflowJobs splits a workflow file into its jobs' text, by indentation.
func workflowJobs(t *testing.T, path string) (top string, jobs map[string]string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, "\njobs:\n")
	if i < 0 {
		t.Fatalf("%s: no jobs", path)
	}
	top, body := s[:i], s[i+len("\njobs:\n"):]
	jobs = map[string]string{}
	locs := jobHeader.FindAllStringSubmatchIndex(body, -1)
	for k, loc := range locs {
		end := len(body)
		if k+1 < len(locs) {
			end = locs[k+1][0]
		}
		jobs[body[loc[2]:loc[3]]] = body[loc[1]:end]
	}
	return top, jobs
}

func TestReleaseWorkflow_PublishesOnlyAfterEveryGate(t *testing.T) {
	top, jobs := workflowJobs(t, ".github/workflows/release.yml")
	if !strings.Contains(top, "permissions:\n  contents: read") {
		t.Error("release.yml: the default token permission must be read-only")
	}
	if got := len(jobs); got != 3 || jobs["ci"] == "" || jobs["release-smoke"] == "" || jobs["release"] == "" {
		t.Fatalf("release.yml jobs: want ci, release-smoke, release; got %d: %v", got, keys(jobs))
	}
	if !strings.Contains(jobs["ci"], "uses: ./.github/workflows/ci.yml") {
		t.Error("release.yml: the ci job must run the repository's CI workflow")
	}
	if !strings.Contains(jobs["release"], "needs: [ci, release-smoke]") {
		t.Error("release.yml: the release job must need ci and release-smoke")
	}
	for name, job := range jobs {
		if strings.Contains(job, "args: release --clean") != (name == "release") {
			t.Errorf("release.yml: only the release job may publish (job %s)", name)
		}
		if strings.Contains(job, "contents: write") != (name == "release") {
			t.Errorf("release.yml: only the release job may write (job %s)", name)
		}
	}
	smoke := jobs["release-smoke"]
	for _, want := range []string{"args: check", "args: release --snapshot --clean --skip=publish", ".github/release/verify-dist.sh dist"} {
		if !strings.Contains(smoke, want) {
			t.Errorf("release.yml: release-smoke lacks %q", want)
		}
	}
}

// The CI workflow is the release gate (release.yml's ci job runs it, and
// release needs ci): every verification runs in some job, as before the
// jobs were split, and nothing lets a failure, a skip or a cancellation of
// any of them pass. The checks look at what the jobs run and how they depend
// on each other, not at job names or step order.
func TestCIWorkflow_IsTheReleaseGate(t *testing.T) {
	top, jobs := workflowJobs(t, ".github/workflows/ci.yml")
	for _, trigger := range []string{"  push:\n    branches:\n      - main", "  pull_request:", "  workflow_call:"} {
		if !strings.Contains(top, trigger) {
			t.Errorf("ci.yml: trigger %q missing (push to main, pull requests and the release must run the same gates)", strings.TrimSpace(strings.SplitN(trigger, "\n", 2)[0]))
		}
	}
	all, _ := os.ReadFile(".github/workflows/ci.yml")
	if strings.Contains(string(all), "continue-on-error") {
		t.Error("ci.yml: continue-on-error would let a failed verification pass")
	}

	// withJob returns the jobs whose text contains every want.
	withJob := func(wants ...string) []string {
		var out []string
		for name, job := range jobs {
			ok := true
			for _, w := range wants {
				ok = ok && strings.Contains(job, w)
			}
			if ok {
				out = append(out, name)
			}
		}
		sort.Strings(out)
		return out
	}
	require := func(what string, wants ...string) string {
		t.Helper()
		found := withJob(wants...)
		if len(found) == 0 {
			t.Errorf("ci.yml: no job runs %s (%q)", what, wants)
			return ""
		}
		return found[0]
	}

	require("gofmt", "gofmt -l")
	require("go vet", "go vet ./...")
	require("the unit tests", "go test -json ./... >")
	require("staticcheck (pinned for Go 1.27.2)",
		"go install honnef.co/go/tools/cmd/staticcheck@v0.7.0-0.dev.0.20261009230814-452d5bb86b45",
		"./internal/cache/...", "./internal/context/...", "./internal/index/...", "./internal/resolver/...",
		"./internal/language/...", "./internal/languages/...", "./internal/mcp/...", "./internal/symbol/...",
		"./internal/reference/...", "./internal/repomap/...", "./internal/impact/...", "./internal/search/...",
		"./internal/fsroot/...", "./internal/accesspolicy/...", "./internal/safewalk/...")
	race := require("the race tests, by shard", `race-shard.py run "${{ matrix.shard }}" "$RUNNER_TEMP/race.json"`, "rc=$?", "exit $rc", `ARK_REQUIRE_SYMLINKS: "1"`)
	if race != "" {
		got, want := matrixValues(t, jobs[race], "shard"), raceShards(t)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("ci.yml: race matrix shards %v, race-shards.txt declares %v", got, want)
		}
		if strings.Join(want, ",") != "1,2,3,4" {
			t.Errorf("race-shards.txt: shards %v, want 1, 2, 3 and 4", want)
		}
		// Each shard keys its Go build cache with a file of its own: a
		// shard without one would share another's cache silently.
		for _, s := range want {
			if _, err := os.Stat(".github/ci/cache-keys/race-" + s); err != nil {
				t.Errorf("race shard %s has no .github/ci/cache-keys/race-%s: %v", s, s, err)
			}
		}
		if strings.Contains(jobs[race], "-short") {
			t.Error("ci.yml: the race tests must not run with -short")
		}
	}
	// What race-shard.py runs: go test -race -json, nothing else added (a
	// split adds only its -run or -skip).
	script, err := os.ReadFile(".github/ci/race-shard.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{`["go", "test", "-race", "-json"] + whole`, `["go", "test", "-race", "-json", flag, rx, p]`} {
		if !strings.Contains(string(script), cmd) {
			t.Errorf("race-shard.py: no %s: whole packages and split halves run as go test -race -json", cmd)
		}
	}
	if !strings.Contains(string(script), "return 1 if failed else 0") {
		t.Error("race-shard.py: a shard must fail when any of its go test commands does")
	}
	for _, flag := range []string{`"-short"`, `"-count`, `"-timeout`, `"-failfast"`, `"-p"`} {
		if strings.Contains(string(script), flag) {
			t.Errorf("race-shard.py: the race run must not add %s", flag)
		}
	}
	fuzz := require("the fuzz smoke, by group", `.github/ci/fuzz-smoke.sh "${{ matrix.group }}"`)
	if fuzz != "" {
		if got, want := matrixValues(t, jobs[fuzz], "group"), fuzzGroups(t); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("ci.yml: fuzz matrix groups %v, fuzz-smoke.txt declares %v", got, want)
		}
	}
	require("the file access security tests",
		"os: [windows-latest, macos-latest]", `ARK_REQUIRE_SYMLINKS: "1"`, `ARK_REQUIRE_CASE_FOLDING: "1"`,
		"go test -race -count=1 -json ./internal/fsroot ./internal/accesspolicy ./internal/safewalk",
		"go test -count=1 -json -race -run 'TestCase_|TestB[0-9]|TestC3_|TestRequestAccess_|TestWarmIndexReads_|TestParseMemo_|TestDumpSymlinkToIgnored|TestMCPCase_|TestCLICase_' ./internal/mcp ./internal/core ./tests/integration",
		"go test -count=1 -json -race -run 'TestParallel' ./internal/index ./internal/golden",
		"exit $rc")
	require("the TypeScript compiler differential", ".github/ts-oracle/run.sh", "node-version: '22.17.0'")

	// The gate: needs every other job, runs whatever they did, and fails
	// unless each succeeded and the race shards ran exactly the unit tests.
	gate := require("the gate", "check-coverage.py reports windows macos")
	if gate != "" {
		job := jobs[gate]
		if !regexp.MustCompile(`(?m)^    if: always\(\)$`).MatchString(job) {
			t.Errorf("ci.yml: %s must run whatever the other jobs did (if: always())", gate)
		}
		m := regexp.MustCompile(`(?m)^    needs: \[([^\]]*)\]`).FindStringSubmatch(job)
		var needs []string
		if m != nil {
			for _, n := range strings.Split(m[1], ",") {
				needs = append(needs, strings.TrimSpace(n))
			}
		}
		sort.Strings(needs)
		var others []string
		for name := range jobs {
			if name != gate {
				others = append(others, name)
			}
		}
		sort.Strings(others)
		if strings.Join(needs, ",") != strings.Join(others, ",") {
			t.Errorf("ci.yml: %s needs %v; every other job is %v", gate, needs, others)
		}
		if !strings.Contains(job, `r != "success"`) || !strings.Contains(job, "toJSON(needs)") {
			t.Errorf("ci.yml: %s must fail unless every job it needs succeeded", gate)
		}
	}
	// No verification job may be skipped by a condition.
	for name, job := range jobs {
		if name != gate && regexp.MustCompile(`(?m)^    if:`).MatchString(job) {
			t.Errorf("ci.yml: job %s has a condition: a skipped job would not fail the release", name)
		}
	}
}

// matrixValues returns the values of a matrix key of a job ("shard: [1, 2]").
func matrixValues(t *testing.T, job, key string) []string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^\s+` + key + `:\s*\[([^\]]*)\]`).FindStringSubmatch(job)
	if m == nil {
		t.Errorf("ci.yml: no matrix %s", key)
		return nil
	}
	var out []string
	for _, v := range strings.Split(m[1], ",") {
		out = append(out, strings.TrimSpace(v))
	}
	sort.Strings(out)
	return out
}

// raceShards returns the shards race-shards.txt declares.
func raceShards(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(".github/ci/race-shards.txt")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range regexp.MustCompile(`(?m)^\[(\d+)\]\s*$`).FindAllStringSubmatch(string(b), -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

type fuzzEntry struct{ group, pkg, target string }

// fuzzList returns the entries of fuzz-smoke.txt.
func fuzzList(t *testing.T) []fuzzEntry {
	t.Helper()
	b, err := os.ReadFile(".github/ci/fuzz-smoke.txt")
	if err != nil {
		t.Fatal(err)
	}
	var out []fuzzEntry
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if len(f) != 3 {
			t.Fatalf("fuzz-smoke.txt: %q is not <group> <package> <target>", line)
		}
		out = append(out, fuzzEntry{f[0], f[1], f[2]})
	}
	return out
}

func fuzzGroups(t *testing.T) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range fuzzList(t) {
		if e.group != "-" && !seen[e.group] {
			seen[e.group] = true
			out = append(out, e.group)
		}
	}
	sort.Strings(out)
	return out
}

// raceEntry is one line of `race-shard.py all`: a shard runs a package, or
// the half of a split package that -run or -skip <regexp> selects.
type raceEntry struct{ shard, pkg, flag, regexp string }

func raceEntries(t *testing.T) []raceEntry {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found: the CI gate checks the shards' coverage at run time")
	}
	out, err := exec.Command(py, ".github/ci/race-shard.py", "all").CombinedOutput()
	if err != nil {
		t.Fatalf("race-shard.py all: %v\n%s", err, out)
	}
	var entries []raceEntry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 2:
			entries = append(entries, raceEntry{f[0], f[1], "", ""})
		case len(f) == 4 && (f[2] == "-run" || f[2] == "-skip") && f[3] != "":
			entries = append(entries, raceEntry{f[0], f[1], f[2], f[3]})
		default:
			t.Fatalf("race-shard.py all: %q", line)
		}
	}
	return entries
}

// Every package of `go list ./...` runs in exactly one race shard, except a
// package split in two: its -run and -skip halves, by the same regexp, in two
// different shards. No shard is empty.
func TestCIWorkflow_RaceShardsCoverEveryPackage(t *testing.T) {
	entries := raceEntries(t)
	list, err := exec.Command("go", "list", "./...").Output()
	if err != nil {
		t.Fatal(err)
	}
	byPkg := map[string][]raceEntry{}
	perShard := map[string]int{}
	for _, e := range entries {
		byPkg[e.pkg] = append(byPkg[e.pkg], e)
		perShard[e.shard]++
	}
	pkgs := strings.Fields(string(list))
	for _, p := range pkgs {
		uses := byPkg[p]
		switch {
		case len(uses) == 1 && uses[0].flag == "":
		case len(uses) == 2:
			a, b := uses[0], uses[1]
			if a.flag == b.flag || a.flag == "" || b.flag == "" {
				t.Errorf("%s: split halves %q and %q are not one -run and one -skip", p, a.flag, b.flag)
			}
			if a.regexp != b.regexp {
				t.Errorf("%s: split regexps differ: %q, %q", p, a.regexp, b.regexp)
			}
			if a.shard == b.shard {
				t.Errorf("%s: both halves in shard %s", p, a.shard)
			}
		default:
			t.Errorf("%s runs %d times in the race shards: %v", p, len(uses), uses)
		}
		delete(byPkg, p)
	}
	for p := range byPkg {
		t.Errorf("race shards name %s, which go list ./... does not list", p)
	}
	for _, s := range raceShards(t) {
		if perShard[s] == 0 {
			t.Errorf("race shard %s runs nothing", s)
		}
	}
}

// A split's regexp selects some, not all, of the package's top-level tests
// (and fuzz tests, run as tests), and no subtest by name: the -run half and
// the -skip half each run something, and every test — with its subtests —
// in exactly one of them. Tests added later run in the -skip half.
func TestCIWorkflow_RaceSplitsAreComplementary(t *testing.T) {
	splits := 0
	for _, e := range raceEntries(t) {
		if e.flag != "-run" {
			continue
		}
		splits++
		if strings.Contains(e.regexp, "/") {
			t.Errorf("%s: split regexp %q names subtests", e.pkg, e.regexp)
		}
		re, err := regexp.Compile(e.regexp)
		if err != nil {
			t.Errorf("%s: %v", e.pkg, err)
			continue
		}
		out, err := exec.Command("go", "test", "-list", ".", e.pkg).Output()
		if err != nil {
			t.Fatalf("go test -list %s: %v", e.pkg, err)
		}
		var run, skip []string
		for _, name := range strings.Fields(string(out)) {
			if !regexp.MustCompile(`^(Test|Fuzz|Example)`).MatchString(name) {
				continue // "ok <pkg> <time>"
			}
			if re.MatchString(name) {
				run = append(run, name)
			} else {
				skip = append(skip, name)
			}
		}
		if len(run) == 0 || len(skip) == 0 {
			t.Errorf("%s: %q selects %d of %d tests: each half must run something", e.pkg, e.regexp, len(run), len(run)+len(skip))
		}
		t.Logf("%s: -run %q: %v; -skip: %d tests", e.pkg, e.regexp, run, len(skip))
	}
	if splits == 0 {
		t.Log("no split package")
	}
}

// The binary-level case-folding tests the gate requires to pass on every
// filesystem-security system (check-coverage.py REQUIRED_SECURITY_TESTS) are
// at least these two, exist in tests/integration, and are selected by the
// job's -run over that package: a rename, a narrower -run or a shorter list
// fails here instead of turning into a silent skip.
func TestCIWorkflow_RequiredSecurityTestsAreSelected(t *testing.T) {
	gate, err := os.ReadFile(".github/ci/check-coverage.py")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^REQUIRED_SECURITY_TESTS = \(([^)]*)\)`).FindSubmatch(gate)
	if m == nil {
		t.Fatal("check-coverage.py: no REQUIRED_SECURITY_TESTS")
	}
	var required []string
	for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(m[1], -1) {
		required = append(required, string(q[1]))
	}
	for _, want := range []string{"TestMCPCase_ExcludedFilesUnreachable", "TestCLICase_DumpExcludes"} {
		if !strings.Contains(strings.Join(required, ","), want) {
			t.Errorf("check-coverage.py: REQUIRED_SECURITY_TESTS %v lacks %s", required, want)
		}
	}
	if !strings.Contains(string(gate), `REQUIRED_SECURITY_PACKAGE = "/tests/integration"`) {
		t.Error("check-coverage.py: the required tests must be looked up in tests/integration")
	}

	_, jobs := workflowJobs(t, ".github/workflows/ci.yml")
	run := regexp.MustCompile(`(?m)^\s*go test [^\n]*-run '([^']+)' [^\n]*\./tests/integration[ \n]`).FindStringSubmatch(jobs["filesystem-security"])
	if run == nil {
		t.Fatal("ci.yml: filesystem-security runs no -run over ./tests/integration")
	}
	selects := regexp.MustCompile(run[1])
	listed, err := exec.Command("go", "test", "-list", ".", "./tests/integration").Output()
	if err != nil {
		t.Fatalf("go test -list ./tests/integration: %v", err)
	}
	for _, name := range required {
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `$`).Match(listed) {
			t.Errorf("tests/integration has no %s", name)
		}
		if !selects.MatchString(name) {
			t.Errorf("ci.yml: filesystem-security -run %q does not select %s", run[1], name)
		}
	}
}

// The tests that build ark from source in another process (tests/integration,
// cmd/ark) must run whenever those sources change: their shared build records
// the build's input directories for go test's result cache before it builds
// (recordInputs), and the gate fails a report in which either package passed
// from the cache (check-coverage.py MUST_RUN_PACKAGES).
func TestCIWorkflow_SourceBuildingTestsAlwaysRun(t *testing.T) {
	gate, err := os.ReadFile(".github/ci/check-coverage.py")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^MUST_RUN_PACKAGES = \(([^)]*)\)`).FindSubmatch(gate)
	if m == nil {
		t.Fatal("check-coverage.py: no MUST_RUN_PACKAGES")
	}
	for _, pkg := range []string{"tests/integration", "cmd/ark"} {
		if !strings.Contains(string(m[1]), `"/`+pkg+`"`) {
			t.Errorf("check-coverage.py: MUST_RUN_PACKAGES %s lacks /%s", m[1], pkg)
		}
		src, err := os.ReadFile(filepath.Join(pkg, "integration_binary_test.go"))
		if err != nil {
			t.Fatal(err)
		}
		record := strings.Index(string(src), "b.recordInputs(flags)")
		build := strings.Index(string(src), `exec.Command("go", append(append([]string{"build"}, flags...)`)
		if record < 0 || build < 0 || record > build {
			t.Errorf("%s/integration_binary_test.go: the shared build must record its inputs (b.recordInputs(flags)) before go build", pkg)
		}
	}
}

// The fuzz smoke runs 22 targets, each once and for 10 seconds without the
// unit tests, and every Fuzz function of the repository is either in it or
// listed as deliberately left out.
func TestCIWorkflow_FuzzSmokeIsComplete(t *testing.T) {
	script, err := os.ReadFile(".github/ci/fuzz-smoke.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), `go test -run '^$' -fuzz="^${target}\$" -fuzztime=10s "$pkg"`) {
		t.Error("fuzz-smoke.sh: each target must run alone (anchored), for 10s, with -run '^$'")
	}
	smoke, listed := 0, map[string]bool{}
	for _, e := range fuzzList(t) {
		key := e.pkg + " " + e.target
		if listed[key] {
			t.Errorf("fuzz-smoke.txt: %s listed twice", key)
		}
		listed[key] = true
		if e.group != "-" {
			smoke++
		}
		files, _ := filepath.Glob(filepath.Join(filepath.FromSlash(strings.TrimPrefix(e.pkg, "./")), "*_test.go"))
		found := false
		for _, f := range files {
			b, _ := os.ReadFile(f)
			found = found || strings.Contains(string(b), "func "+e.target+"(f *testing.F)")
		}
		if !found {
			t.Errorf("fuzz-smoke.txt: no %s in %s", e.target, e.pkg)
		}
	}
	if smoke != 23 {
		t.Errorf("fuzz smoke: %d targets, want 23", smoke)
	}
	fuzzFunc := regexp.MustCompile(`(?m)^func (Fuzz\w+)\(f \*testing\.F\)`)
	filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
				return filepath.SkipDir // not packages of go list ./...
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, _ := os.ReadFile(path)
		for _, m := range fuzzFunc.FindAllStringSubmatch(string(b), -1) {
			if key := "./" + filepath.ToSlash(filepath.Dir(path)) + " " + m[1]; !listed[key] {
				t.Errorf("%s (%s) is neither in the fuzz smoke nor listed as left out (fuzz-smoke.txt)", m[1], path)
			}
		}
		return nil
	})
}

// Every GoReleaser invocation uses the same major version as the
// configuration's schema (v1: no `version: 2` header).
func TestGoReleaserVersionIsConsistent(t *testing.T) {
	b, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	uses := strings.Count(string(b), "goreleaser/goreleaser-action@")
	if v1 := strings.Count(string(b), "version: '~> v1'"); uses == 0 || v1 != uses {
		t.Errorf("release.yml: %d GoReleaser steps, %d pinned to ~> v1", uses, v1)
	}
	cfg, err := os.ReadFile(".goreleaser.yml")
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`(?m)^version:\s*2`).Match(cfg) {
		t.Error(".goreleaser.yml declares the v2 schema but the workflow runs GoReleaser v1")
	}
}

// Only a published release is a release build for the index cache
// (internal/cache/build.go): GoReleaser sets internal/cache.release for a
// release and leaves it empty for a snapshot or a modified tree (even with
// validation skipped), and no other build sets it.
func TestGoReleaser_OnlyReleasesShareTheReleaseCache(t *testing.T) {
	cfg, err := os.ReadFile(".goreleaser.yml")
	if err != nil {
		t.Fatal(err)
	}
	const flag = `-X "github.com/magicdrive/ark/internal/cache.release={{ if and (not .IsSnapshot) (not .IsGitDirty) }}true{{ end }}"`
	if !strings.Contains(string(cfg), "- "+flag+"\n") {
		t.Errorf(".goreleaser.yml: the ark build's ldflags lack %s", flag)
	}
	src, err := os.ReadFile("internal/cache/build.go")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^var release string$`).Match(src) {
		t.Error("internal/cache/build.go: no `var release string` for the release flag to set")
	}
	for _, path := range []string{"Makefile", ".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "internal/cache.release") {
			t.Errorf("%s sets internal/cache.release: only a GoReleaser release may", path)
		}
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A run re-run keeps every attempt's artifacts, and download-artifact keeps,
// of several artifacts of one name, the one of the highest artifact id —
// which need not be the latest attempt (run 38325791896: the failed first
// attempt's race report outranked its successful rerun's, and the gate
// failed on every later attempt). So every test report is named by attempt,
// and the gate checks each job's latest (select-reports.py).
func TestCIWorkflow_GateChecksEachJobsLatestAttempt(t *testing.T) {
	_, jobs := workflowJobs(t, ".github/workflows/ci.yml")
	uploads := 0
	for name, body := range jobs {
		for _, m := range regexp.MustCompile(`(?m)^\s*name: (test-report-[^\n]*)$`).FindAllStringSubmatch(body, -1) {
			uploads++
			if !strings.HasSuffix(m[1], "-attempt-${{ github.run_attempt }}") {
				t.Errorf("ci.yml %s: test report artifact %s is not named by attempt", name, m[1])
			}
		}
	}
	if uploads != 3 {
		t.Errorf("ci.yml: %d test report uploads, want 3 (unit, race, filesystem-security)", uploads)
	}
	gate := jobs["ci-gate"]
	steps := []string{
		"pattern: test-report-*\n          path: downloaded-reports",
		// whatever the jobs did: a skipped selection would hide the coverage
		// check's own diagnosis behind a missing directory
		"if: always()\n        run: python3 .github/ci/select-reports.py downloaded-reports reports",
		"python3 .github/ci/check-coverage.py reports windows macos",
	}
	at := 0
	for _, s := range steps {
		i := strings.Index(gate[at:], s)
		if i < 0 {
			t.Fatalf("ci-gate: no %q after the previous step", s)
		}
		at += i + len(s)
	}
}

// select-reports.py keeps, of each job, the report of its highest attempt —
// by number, not by name or order — and refuses what it cannot attribute.
func TestSelectReports_LatestAttemptOfEachJob(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found: the CI gate selects the reports at run time")
	}
	src, dest := t.TempDir(), filepath.Join(t.TempDir(), "reports")
	write := func(dir, content string) {
		if err := os.MkdirAll(filepath.Join(src, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, dir, "report.tests.tsv"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("test-report-race-4-attempt-1", "failed")
	write("test-report-race-4-attempt-2", "passed")
	write("test-report-unit-attempt-9", "attempt 9")
	write("test-report-unit-attempt-10", "attempt 10")
	write("test-report-race-1-attempt-1", "only attempt")
	out, err := exec.Command(py, ".github/ci/select-reports.py", src, dest).CombinedOutput()
	if err != nil {
		t.Fatalf("select-reports.py: %v\n%s", err, out)
	}
	for job, want := range map[string]string{
		"test-report-race-4": "passed", "test-report-unit": "attempt 10", "test-report-race-1": "only attempt",
	} {
		got, err := os.ReadFile(filepath.Join(dest, job, "report.tests.tsv"))
		if err != nil || string(got) != want {
			t.Errorf("%s: %q (%v), want %q", job, got, err, want)
		}
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 3 {
		t.Errorf("selected %d reports, want 3", len(entries))
	}
	for _, bad := range []string{"test-report-unit", "test-report-unit-attempt-0", "lone-file.tests.tsv"} {
		dir := t.TempDir()
		p := filepath.Join(dir, bad)
		if strings.HasSuffix(bad, ".tsv") {
			err = os.WriteFile(p, nil, 0o644)
		} else {
			err = os.MkdirAll(p, 0o755)
		}
		if err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(py, ".github/ci/select-reports.py", dir, filepath.Join(t.TempDir(), "r")).CombinedOutput(); err == nil {
			t.Errorf("%s: selected without an attempt\n%s", bad, out)
		}
	}
}

// A failing fuzz smoke target leaves what its next failure needs to be
// diagnosed, and a wedged one cannot hold the job for hours: the step is
// bounded, passes a diagnostics directory to fuzz-smoke.sh and keeps it, on
// failure or timeout, as an artifact of its own attempt — never one the
// gate's test-report-* download would take for a test report.
func TestCIWorkflow_FuzzSmokeKeepsFailureDiagnostics(t *testing.T) {
	b, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	ci := string(b)
	run := strings.Index(ci, "timeout-minutes: 20\n"+
		`        run: .github/ci/fuzz-smoke.sh "${{ matrix.group }}" "$RUNNER_TEMP/fuzz-diagnostics"`)
	upload := strings.Index(ci, "if: failure() || cancelled()\n"+
		"        uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2\n"+
		"        with:\n"+
		"          name: fuzz-diagnostics-${{ matrix.group }}-attempt-${{ github.run_attempt }}\n"+
		"          path: ${{ runner.temp }}/fuzz-diagnostics\n")
	// Diagnostics are kept by target name: within a group, names are unique.
	seen := map[string]string{}
	for _, e := range fuzzList(t) {
		if e.group == "-" {
			continue
		}
		if pkg, ok := seen[e.group+" "+e.target]; ok {
			t.Errorf("fuzz-smoke.txt: group %s has %s in both %s and %s: their diagnostics would share a name", e.group, e.target, pkg, e.pkg)
		}
		seen[e.group+" "+e.target] = e.pkg
	}
	if run < 0 || upload < 0 || upload < run {
		t.Error("ci.yml: the fuzz smoke must be bounded (timeout-minutes: 20), write its diagnostics to $RUNNER_TEMP/fuzz-diagnostics and upload them on failure or timeout as fuzz-diagnostics-<group>-attempt-<n>")
	}
}

// fuzz-smoke.sh runs every target of its group, fails when any fails, and
// keeps each failing target's output and the inputs its run added (the
// crash input go test writes to testdata/fuzz/<target>), not the corpus
// that was there before.
func TestFuzzSmoke_ReportsEveryFailingTarget(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found: the fuzz smoke runs on Linux runners")
	}
	script, err := os.ReadFile(".github/ci/fuzz-smoke.sh")
	if err != nil {
		t.Fatal(err)
	}
	// The log exists before go test starts (not when tee gets to open it): a
	// step its timeout stops keeps it, and the stand-in below can check it.
	create, run := strings.Index(string(script), `: > "$log"`), strings.Index(string(script), `| tee "$log"`)
	if create < 0 || run < 0 || create > run {
		t.Error(`fuzz-smoke.sh: each target's log must be created (: > "$log") before its go test runs`)
	}
	root := t.TempDir()
	write := func(path, content string, mode os.FileMode) {
		p := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(".github/ci/fuzz-smoke.sh", string(script), 0o755)
	write(".github/ci/fuzz-smoke.txt", "# test list\n"+
		"7 ./a FuzzOK\n7 ./a FuzzBad\n8 ./a FuzzOther\n7 ./b FuzzCrash\n- ./b FuzzLeftOut\n"+
		"10 ./a FuzzBad\n10 ./a FuzzOK\n11 ./a FuzzMissing\n12 ./c FuzzNoFiles\n", 0o644)
	write("a/testdata/fuzz/FuzzBad/old", "go test fuzz v1\n", 0o644)
	// A stand-in for go: FuzzBad fails on a new input it records, FuzzCrash
	// dies without one, FuzzMissing is not there and FuzzNoFiles's package
	// has no test file (go test passes either way). The others pass after
	// reading all their stdin (were that the target list, the targets after
	// them would never run), once their log is in the diagnostics directory
	// while they run (a step stopped by its timeout keeps it). Every
	// invocation is logged.
	write("bin/go", `#!/bin/sh
[ "$1" = version ] && { echo "go version stub"; exit 0; }
echo "$*" >> "$CALLS"
case "$*" in
*FuzzBad*) mkdir -p ./a/testdata/fuzz/FuzzBad && echo crash > ./a/testdata/fuzz/FuzzBad/1f2e
	echo "--- FAIL: FuzzBad (0.10s)"; echo "    Failing input written to testdata/fuzz/FuzzBad/1f2e"; exit 1 ;;
*FuzzCrash*) echo "fuzzing process hung or terminated unexpectedly: exit status 2"; exit 2 ;;
*FuzzMissing*) echo "testing: warning: no fuzz tests to fuzz"; echo PASS; exit 0 ;;
*FuzzNoFiles*) echo "?   	example/c	[no test files]"; exit 0 ;;
esac
cat >/dev/null
target=${4#-fuzz=^}; target=${target%$}
[ -f "$DIAG/$target.log" ] || { echo "no $DIAG/$target.log while running"; exit 3; }
echo "fuzz: elapsed: 0s, execs: 1 (1/sec), new interesting: 0 (total: 1)"
echo ok
`, 0o755)
	runGroup := func(group string) (string, string, string, error) {
		diag := filepath.Join(t.TempDir(), "diag")
		calls := filepath.Join(t.TempDir(), "calls")
		summary := filepath.Join(t.TempDir(), "summary.md")
		cmd := exec.Command(bash, ".github/ci/fuzz-smoke.sh", group, diag)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "PATH="+filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
			"CALLS="+calls, "GITHUB_STEP_SUMMARY="+summary, "DIAG="+diag)
		out, err := cmd.CombinedOutput()
		c, _ := os.ReadFile(calls)
		s, _ := os.ReadFile(summary)
		return diag, string(out) + "\n" + string(c), string(s), err
	}

	diag, out, summary, err := runGroup("7")
	if err == nil {
		t.Fatalf("group 7 has failing targets but passed:\n%s", out)
	}
	for _, want := range []string{
		"-fuzz=^FuzzOK$ -fuzztime=10s ./a", "-fuzz=^FuzzBad$ -fuzztime=10s ./a", "-fuzz=^FuzzCrash$ -fuzztime=10s ./b",
		"fuzz FuzzBad (./a): exit 1", "fuzz FuzzCrash (./b): exit 2", "failed: FuzzBad FuzzCrash (of 3 targets",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("group 7: no %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "FuzzOther") || strings.Contains(out, "FuzzLeftOut") {
		t.Errorf("group 7 ran another group's target:\n%s", out)
	}
	var kept []string
	filepath.WalkDir(diag, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(diag, p)
			kept = append(kept, filepath.ToSlash(rel))
		}
		return nil
	})
	if got, want := strings.Join(kept, ","), "FuzzBad/1f2e,FuzzBad.log,FuzzCrash.log"; got != want {
		t.Errorf("diagnostics %s, want %s", got, want)
	}
	for _, want := range []string{"### fuzz FuzzBad (./a): exit 1", "Failing input written to testdata/fuzz/FuzzBad/1f2e",
		"### fuzz FuzzCrash (./b): exit 2", "hung or terminated", "No new input in ./b/testdata/fuzz/FuzzCrash"} {
		if !strings.Contains(summary, want) {
			t.Errorf("step summary: no %q in\n%s", want, summary)
		}
	}

	// A failure, then a pass: the group fails on any target, not its last.
	if _, out, _, err := runGroup("10"); err == nil || !strings.Contains(out, "failed: FuzzBad (of 2 targets") {
		t.Errorf("group 10 has a failing target, then a passing one, and must fail: %v\n%s", err, out)
	}
	// A target go test passes without fuzzing has not run.
	for group, target := range map[string]string{"11": "FuzzMissing (./a)", "12": "FuzzNoFiles (./c)"} {
		if _, out, _, err := runGroup(group); err == nil || !strings.Contains(out, "fuzz "+target+": passed without fuzzing") {
			t.Errorf("group %s fuzzes nothing and must fail: %v\n%s", group, err, out)
		}
	}
	if _, out, _, err := runGroup("8"); err != nil {
		t.Errorf("group 8 passes: %v\n%s", err, out)
	}
	if _, out, _, err := runGroup("9"); err == nil || !strings.Contains(out, "no target in group 9") {
		t.Errorf("group 9 has no target and must fail: %v\n%s", err, out)
	}
}

// make staticcheck runs what the lint job of ci.yml runs: the same pinned
// version over the same packages, so the local gate fails exactly when CI's
// does.
func TestMakefile_StaticcheckMatchesCI(t *testing.T) {
	mk, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	variable := func(name string) string {
		m := regexp.MustCompile(`(?m)^` + name + ` = ((?:.*\\\n)*.*)$`).FindSubmatch(mk)
		if m == nil {
			t.Fatalf("Makefile: no %s", name)
		}
		return strings.Join(strings.Fields(strings.ReplaceAll(string(m[1]), "\\\n", " ")), " ")
	}
	_, jobs := workflowJobs(t, ".github/workflows/ci.yml")
	lint := jobs["lint"]
	install := regexp.MustCompile(`go install honnef\.co/go/tools/cmd/staticcheck@(\S+)`).FindStringSubmatch(lint)
	run := regexp.MustCompile(`(?s)run: \|\n\s+staticcheck \\\n(.*?)\n\n`).FindStringSubmatch(lint + "\n\n")
	if install == nil || run == nil {
		t.Fatal("ci.yml: the lint job does not install and run staticcheck")
	}
	if got := variable("STATICCHECK_VERSION"); got != install[1] {
		t.Errorf("Makefile: STATICCHECK_VERSION %s, ci.yml installs %s", got, install[1])
	}
	ci := strings.Join(strings.Fields(strings.ReplaceAll(run[1], "\\", " ")), " ")
	if got := variable("STATICCHECK_PACKAGES"); got != ci {
		t.Errorf("Makefile: STATICCHECK_PACKAGES\n  %s\nci.yml runs staticcheck over\n  %s", got, ci)
	}
	recipe := regexp.MustCompile(`(?m)^staticcheck:\n((?:\t.*\n)+)`).FindSubmatch(mk)
	if recipe == nil || !strings.Contains(string(recipe[1]), "go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) $(STATICCHECK_PACKAGES)") {
		t.Error("Makefile: make staticcheck must run STATICCHECK_VERSION over STATICCHECK_PACKAGES")
	}
}
