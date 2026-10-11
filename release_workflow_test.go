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
	race := require("the race tests, by shard", `go test -race -json $(cat "$RUNNER_TEMP/packages")`, `race-shard.py "${{ matrix.shard }}"`, `ARK_REQUIRE_SYMLINKS: "1"`)
	if race != "" {
		if got, want := matrixValues(t, jobs[race], "shard"), raceShards(t); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("ci.yml: race matrix shards %v, race-shards.txt declares %v", got, want)
		}
		if strings.Contains(jobs[race], "-short") {
			t.Error("ci.yml: the race tests must not run with -short")
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
		"go test -count=1 -json -race -run 'TestCase_|TestB[0-9]|TestC3_|TestRequestAccess_|TestWarmIndexReads_|TestParseMemo_|TestDumpSymlinkToIgnored|TestMCPCase_|TestCLICase_' ./internal/mcp ./internal/core .",
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

// Every package of `go list ./...` runs in exactly one race shard.
func TestCIWorkflow_RaceShardsCoverEveryPackage(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found: the CI gate checks the shards' coverage at run time")
	}
	out, err := exec.Command(py, ".github/ci/race-shard.py", "all").CombinedOutput()
	if err != nil {
		t.Fatalf("race-shard.py: %v\n%s", err, out)
	}
	list, err := exec.Command("go", "list", "./...").Output()
	if err != nil {
		t.Fatal(err)
	}
	shardOf := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			t.Fatalf("race-shard.py all: %q", line)
		}
		if prev, dup := shardOf[f[1]]; dup {
			t.Errorf("%s in race shards %s and %s", f[1], prev, f[0])
		}
		shardOf[f[1]] = f[0]
	}
	declared := map[string]bool{}
	for _, s := range raceShards(t) {
		declared[s] = true
	}
	pkgs := strings.Fields(string(list))
	for _, p := range pkgs {
		s, ok := shardOf[p]
		if !ok {
			t.Errorf("%s runs in no race shard", p)
		} else if !declared[s] {
			t.Errorf("%s runs in undeclared shard %s", p, s)
		}
	}
	if len(shardOf) != len(pkgs) {
		t.Errorf("race shards hold %d packages; go list ./... has %d", len(shardOf), len(pkgs))
	}
	for s := range declared {
		n := 0
		for _, owner := range shardOf {
			if owner == s {
				n++
			}
		}
		if n == 0 {
			t.Errorf("race shard %s runs no package", s)
		}
	}
}

// The fuzz smoke runs 13 targets, each once and for 10 seconds without the
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
	if smoke != 13 {
		t.Errorf("fuzz smoke: %d targets, want 13", smoke)
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

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
