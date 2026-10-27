package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The release workflow must never publish what the quality gates have not
// passed, nor give a token to code it builds. These checks pin its structure
// (GitHub Actions cannot be run here): a tag push runs the full CI and a
// read-only build of the release, and Hephaestus publishes only in a job that
// needs both, from the build's verified dist/.

var jobHeader = regexp.MustCompile(`(?m)^  ([A-Za-z0-9_-]+):\s*$`)

// tb is what the workflow checks need of a test: *testing.T, or a recorder
// that runs them against a mutated workflow (TestWorkflowChecks_CatchMutations).
type tb interface {
	Helper()
	Error(args ...any)
	Errorf(format string, args ...any)
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

// workflowJobs splits a workflow file into its jobs' text, by indentation.
func workflowJobs(t *testing.T, path string) (top string, jobs map[string]string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return splitJobs(t, path, string(b))
}

// splitJobs splits the text s of the workflow path into its jobs' text.
func splitJobs(t tb, path, s string) (top string, jobs map[string]string) {
	t.Helper()
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

// hephaestusAction is the Hephaestus action every workflow step uses: the
// commit of the v1.3.0 tag (HEPHAESTUS_VERSION of the Makefile). From
// v1.2.0 the action runs that release's verified binary and installs no Go:
// the jobs that build set up go.mod's Go themselves.
const hephaestusAction = "uses: magicdrive/hephaestus@e544acf59c79cbafa04a48002b0f30f91a02f8ba # v1.3.0"

// The release-job conventions of `hephaestus publish` (docs/publish.md of
// Hephaestus): a plan step, and exactly these two steps — the only ones given
// the tap token — run only when the plan publishes the tap. publish verifies
// them by name: both skipped for a prerelease, both succeeded otherwise.
const (
	tapRehearsalStep = "Rehearse the formula and tap pull request (read-only)"
	tapPublishStep   = "Generate the formula and open a tap pull request"
	tapCondition     = "steps.plan.outputs.tap == 'publish'"
)

// jobSteps splits a job's text into its steps' text, each starting at its
// first key (`name: ...`).
func jobSteps(job string) []string {
	parts := strings.Split(job, "\n      - ")
	return parts[1:]
}

// stepField returns the value of a top-level key of a step ("" if absent).
func stepField(step, key string) string {
	if strings.HasPrefix(step, key+": ") {
		return strings.TrimSpace(strings.SplitN(step[len(key)+2:], "\n", 2)[0])
	}
	m := regexp.MustCompile(`(?m)^        ` + regexp.QuoteMeta(key) + `: (.*)$`).FindStringSubmatch(step)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// hephaestusSteps returns the `with:` blocks of a job's Hephaestus steps.
func hephaestusSteps(job string) []string {
	var out []string
	parts := strings.Split(job, hephaestusAction)
	for _, p := range parts[1:] {
		if i := strings.Index(p, "\n      - "); i >= 0 {
			p = p[:i]
		}
		out = append(out, p)
	}
	return out
}

func TestReleaseWorkflow_PublishesOnlyAfterEveryGate(t *testing.T) {
	b, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	checkReleaseWorkflow(t, string(b))
}

func checkReleaseWorkflow(t tb, all string) {
	t.Helper()
	top, jobs := splitJobs(t, "release.yml", all)
	if !strings.Contains(top, "on:\n  push:\n    tags:\n      - 'v*'\n\n") {
		t.Error("release.yml: must run on v* tag pushes only")
	}
	if !strings.Contains(top, "permissions:\n  contents: read") {
		t.Error("release.yml: the default token permission must be read-only")
	}
	if !strings.Contains(top, "concurrency:\n  group: release-${{ github.ref }}\n  cancel-in-progress: false") {
		t.Error("release.yml: one release per tag at a time, never cancelled")
	}
	if got := len(jobs); got != 4 || jobs["provenance"] == "" || jobs["ci"] == "" || jobs["build"] == "" || jobs["release"] == "" {
		t.Fatalf("release.yml jobs: want provenance, ci, build, release; got %d: %v", got, keys(jobs))
	}
	if !strings.Contains(jobs["ci"], "uses: ./.github/workflows/ci.yml") {
		t.Error("release.yml: the ci job must run the repository's CI workflow")
	}
	if !regexp.MustCompile(`(?m)^    needs: \[provenance, ci, build\]$`).MatchString(jobs["release"]) {
		t.Error("release.yml: the release job must need provenance, ci and build")
	}
	// The provenance gate: nothing is built or published unless the tag's
	// commit is in main's history (a full clone; the script fetches the tag
	// and main afresh and fails closed).
	if !regexp.MustCompile(`(?m)^    needs: \[provenance\]$`).MatchString(jobs["build"]) {
		t.Error("release.yml: the build job must need provenance")
	}
	prov := jobs["provenance"]
	if !strings.Contains(prov, "permissions:\n      contents: read\n") || strings.Contains(prov, "needs:") {
		t.Error("release.yml: the provenance job runs first, with contents: read")
	}
	if len(hephaestusSteps(prov)) != 0 {
		t.Error("release.yml: the provenance job runs no Hephaestus step")
	}
	inOrder(t, "release.yml provenance", prov,
		"uses: actions/checkout@",
		"fetch-depth: 0\n          persist-credentials: false\n",
		`run: .github/release/verify-tag-provenance.sh --ref "$GITHUB_REF" --sha "$GITHUB_SHA" main`+"\n")
	if n := len(jobSteps(prov)); n != 2 {
		t.Errorf("release.yml: the provenance job has %d steps, want 2 (checkout, verify)", n)
	}
	for _, step := range jobSteps(prov) {
		if cond := stepField(step, "if"); cond != "" || strings.Contains(step, "continue-on-error") {
			t.Errorf("release.yml: provenance step %q may be skipped or ignored (if: %q)", stepField(step, "name"), cond)
		}
	}
	for _, bad := range []string{"PERSONAL_GITHUB_TOKEN", "pull_request_target", "continue-on-error", "goreleaser"} {
		if strings.Contains(strings.ToLower(all), strings.ToLower(bad)) {
			t.Errorf("release.yml: %s", bad)
		}
	}
	// Tokens reach Hephaestus through the action's inputs only, never a job
	// or workflow environment, which build code could read.
	if regexp.MustCompile(`(?m)^(|    )env:`).MatchString(all) {
		t.Error("release.yml: no workflow or job env (tokens go through the action's inputs)")
	}
	for _, m := range regexp.MustCompile(`(?m)^\s+uses: (\S+)(.*)$`).FindAllStringSubmatch(all, -1) {
		if strings.HasPrefix(m[1], "./") {
			continue
		}
		if !regexp.MustCompile(`@[0-9a-f]{40}$`).MatchString(m[1]) || !strings.HasPrefix(m[2], " # v") {
			t.Errorf("release.yml: %s is not pinned to a full commit SHA with its version", m[1])
		}
	}
	for name, job := range jobs {
		if strings.Contains(job, "contents: write") != (name == "release") {
			t.Errorf("release.yml: only the release job may write (job %s)", name)
		}
		if strings.Contains(job, "secrets.") != (name == "release") {
			t.Errorf("release.yml: only the release job may use secrets (job %s)", name)
		}
		if regexp.MustCompile(`(?m)^    environment: release$`).MatchString(job) != (name == "release") {
			t.Errorf("release.yml: only the release job deploys to the release environment (job %s)", name)
		}
		if name != "ci" && !regexp.MustCompile(`(?m)^    timeout-minutes: \d+$`).MatchString(job) {
			t.Errorf("release.yml: job %s has no timeout-minutes", name)
		}
		for _, with := range hephaestusSteps(job) {
			publishes := strings.Contains(with, "command: release") ||
				(strings.Contains(with, "stages: formula,tap") && !strings.Contains(with, `dry-run: "true"`))
			if publishes && name != "release" {
				t.Errorf("release.yml: only the release job may publish (job %s)", name)
			}
		}
	}

	// The build: read-only, no secret and no token for the build commands,
	// a fresh build of the tag, verified as a release before it is handed on.
	build := jobs["build"]
	if !strings.Contains(build, "permissions:\n      contents: read\n") || strings.Contains(build, "id-token") {
		t.Error("release.yml: the build job must have contents: read and nothing else")
	}
	steps := hephaestusSteps(build)
	if len(steps) != 2 || !strings.Contains(steps[0], "command: check") ||
		!strings.Contains(steps[1], "command: run\n          stages: build,archive,checksum\n          fresh: \"true\"\n          github-token: \"\"") {
		t.Errorf("release.yml: the build job must check the configuration, then run build,archive,checksum fresh without a token: %q", steps)
	}
	for _, s := range steps {
		if !strings.Contains(s, `github-token: ""`) || strings.Contains(s, "tap-token") {
			t.Errorf("release.yml: a build step holds a token: %q", s)
		}
	}
	// The action provides no Go (v1.2.0): the build sets up go.mod's, which
	// build.sh pins and asserts, before Hephaestus runs it.
	if regexp.MustCompile(`(?m)^\s+go-version:`).MatchString(all) {
		t.Error("release.yml: Go is go.mod's (go-version-file), not another go-version")
	}
	inOrder(t, "release.yml build", build,
		"uses: actions/setup-go@",
		"go-version-file: 'go.mod'",
		hephaestusAction+"\n        with:\n          command: check\n",
		"stages: build,archive,checksum",
		`run: .github/release/verify-dist.sh dist --tag "$GITHUB_REF_NAME"`,
		"uses: actions/upload-artifact@")

	// The release: the build's dist/, verified again; the release planned
	// (a prerelease skips the tap); the tap rehearsed read-only and the
	// provenance of every asset attested before anything is published; the
	// tap token only where the tap is written, and only when the plan says
	// it is.
	release := jobs["release"]
	for _, want := range []string{"contents: write", "id-token: write", "attestations: write", "persist-credentials: false"} {
		if !strings.Contains(release, want) {
			t.Errorf("release.yml: the release job lacks %q", want)
		}
	}
	if strings.Contains(release, "setup-go") || strings.Contains(release, "make ") || strings.Contains(release, "go build") {
		t.Error("release.yml: the release job must not build anything")
	}
	ifTap := "\n        if: " + tapCondition + "\n"
	inOrder(t, "release.yml release", release,
		"uses: actions/download-artifact@",
		`run: .github/release/verify-dist.sh dist --tag "$GITHUB_REF_NAME"`,
		"id: plan\n        "+hephaestusAction+"\n        with:\n          command: plan\n",
		ifTap+"        env:\n          HAS_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN != '' }}",
		"- name: "+tapRehearsalStep+"\n",
		ifTap+"        "+hephaestusAction+"\n        with:\n          command: run\n          stages: formula,tap\n          dry-run: \"true\"\n          github-token: \"\"\n          tap-token: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}\n",
		"uses: actions/attest-build-provenance@",
		"dist/ark_*.tar.gz\n            dist/ark_checksums.txt",
		"command: release\n",
		"- name: "+tapPublishStep+"\n",
		ifTap+"        "+hephaestusAction+"\n        with:\n          command: run\n          stages: formula,tap\n          github-token: \"\"\n          tap-token: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}")
	rs := hephaestusSteps(release)
	if len(rs) != 4 {
		t.Fatalf("release.yml: the release job has %d Hephaestus steps, want 4 (plan, rehearsal, release, tap)", len(rs))
	}
	if strings.TrimSpace(rs[0]) != "with:\n          command: plan" {
		t.Errorf("release.yml: the plan step takes nothing but command: plan: %q", rs[0])
	}
	if strings.Contains(rs[2], "token") {
		t.Errorf("release.yml: the release step takes the default github.token only: %q", rs[2])
	}
	if strings.Count(release, "secrets.HOMEBREW_TAP_GITHUB_TOKEN }}") != 2 {
		t.Error("release.yml: the tap token goes to the rehearsal and the tap steps only")
	}
	// Step by step: one plan step; every step that touches a secret comes
	// after it and runs only when the tap is published; exactly the two
	// steps publish verifies by name take the tap token; every other step
	// runs unconditionally (a skipped attestation or release step would
	// pass the job).
	plan := -1
	var tokenSteps []string
	for i, step := range jobSteps(release) {
		name, cond := stepField(step, "name"), stepField(step, "if")
		if stepField(step, "id") == "plan" {
			if plan >= 0 {
				t.Errorf("release.yml: two plan steps")
			}
			plan = i
		}
		if strings.Contains(step, "tap-token:") {
			tokenSteps = append(tokenSteps, name)
		}
		if strings.Contains(step, "secrets.") || strings.Contains(step, "tap-token:") {
			if plan < 0 {
				t.Errorf("release.yml: step %q uses the tap token before the plan step", name)
			}
			if cond != tapCondition {
				t.Errorf("release.yml: step %q uses the tap token with if %q; want %s", name, cond, tapCondition)
			}
		} else if cond != "" {
			t.Errorf("release.yml: step %q has a condition (%s): only the tap token's steps may be skipped", name, cond)
		}
	}
	if plan < 0 {
		t.Error("release.yml: the release job has no plan step (id: plan)")
	}
	if got, want := strings.Join(tokenSteps, " | "), tapRehearsalStep+" | "+tapPublishStep; got != want {
		t.Errorf("release.yml: the steps given the tap token are %q, want exactly %q (hephaestus publish verifies them by name)", got, want)
	}
}

// inOrder fails unless every want occurs in s, each after the previous one.
func inOrder(t tb, what, s string, wants ...string) {
	t.Helper()
	at := 0
	for _, w := range wants {
		i := strings.Index(s[at:], w)
		if i < 0 {
			t.Errorf("%s: no %q after the previous step", what, w)
			return
		}
		at += i + len(w)
	}
}

// The CI workflow is the release gate (release.yml's ci job runs it, and
// release needs ci): every verification runs in some job, as before the
// jobs were split, and nothing lets a failure, a skip or a cancellation of
// any of them pass. The checks look at what the jobs run and how they depend
// on each other, not at job names or step order.
func TestCIWorkflow_IsTheReleaseGate(t *testing.T) {
	b, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	checkCIWorkflow(t, string(b))
}

func checkCIWorkflow(t tb, all string) {
	t.Helper()
	top, jobs := splitJobs(t, "ci.yml", all)
	for _, trigger := range []string{"  push:\n    branches:\n      - main", "  pull_request:", "  workflow_call:"} {
		if !strings.Contains(top, trigger) {
			t.Errorf("ci.yml: trigger %q missing (push to main, pull requests and the release must run the same gates)", strings.TrimSpace(strings.SplitN(trigger, "\n", 2)[0]))
		}
	}
	if strings.Contains(all, "continue-on-error") {
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

	// The gates added with Hephaestus: the whole suite natively on macOS,
	// govulncheck, the workflows and renovate.json, and the release
	// itself as a snapshot — built by the action the release uses, verified
	// as a release is, every binary run on its own system and the formula
	// installed by Homebrew on Linux and macOS.
	// The whole suite natively on macOS, with the race detector: the shards of
	// race-shards.txt, planned on macOS (race-shard.py all there), each shard
	// keeping its plan with its report for the gate (check-native.py).
	native := require("the race tests on macOS, by shard", "runs-on: macos-latest",
		`python3 .github/ci/race-shard.py all > "$RUNNER_TEMP/reports/native-${{ matrix.shard }}-macos.plan.txt"`,
		`race-shard.py run "${{ matrix.shard }}" "$RUNNER_TEMP/native.json"`, "rc=$?", "exit $rc",
		`test-report.py "$RUNNER_TEMP/native.json" "native-${{ matrix.shard }}-macos" "$RUNNER_TEMP/reports"`)
	if native != "" {
		want := raceShards(t)
		if got := matrixValues(t, jobs[native], "shard"); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("ci.yml: %s matrix shards %v, race-shards.txt declares %v", native, got, want)
		}
		for _, s := range want {
			if _, err := os.Stat(".github/ci/cache-keys/native-" + s); err != nil {
				t.Errorf("native shard %s has no .github/ci/cache-keys/native-%s: %v", s, s, err)
			}
		}
		if !strings.Contains(jobs[native], "      fail-fast: false\n") {
			t.Errorf("ci.yml: %s must run every shard whatever another does (fail-fast: false)", native)
		}
		if strings.Contains(jobs[native], "-short") {
			t.Error("ci.yml: the native tests must not run with -short")
		}
	}
	require("govulncheck", "run: make vulncheck")
	require("actionlint, shellcheck and the Renovate validator", "run: make actionlint", "run: shellcheck .github/release/*.sh .github/ci/*.sh", "run: make renovate-check")
	snapshot := require("the release snapshot", hephaestusAction, "command: check",
		"command: run\n          stages: build,archive,checksum\n          snapshot: \"true\"\n          fresh: \"true\"\n          github-token: \"\"",
		"run: .github/release/verify-dist.sh dist --snapshot", "name: release-snapshot")
	// The action provides no Go (v1.2.0): the jobs that build ark or run
	// Hephaestus with go run set up go.mod's Go first.
	if regexp.MustCompile(`(?m)^\s+go-version:`).MatchString(all) {
		t.Error("ci.yml: Go is go.mod's (go-version-file), not another go-version")
	}
	if snapshot != "" {
		inOrder(t, "ci.yml "+snapshot, jobs[snapshot], "uses: actions/setup-go@", "go-version-file: 'go.mod'", hephaestusAction)
	}
	binaries := require("the release binaries on their systems", ".github/release/smoke.sh \"$exe\" v0.0.0-SNAPSHOT", "name: release-snapshot")
	if binaries != "" {
		var targets []string
		for _, m := range regexp.MustCompile(`(?m)^\s+targets: (.+)$`).FindAllStringSubmatch(jobs[binaries], -1) {
			targets = append(targets, strings.Fields(m[1])...)
		}
		sort.Strings(targets)
		if got, want := strings.Join(targets, " "), "darwin_amd64 darwin_arm64 linux_386 linux_amd64 linux_arm64 windows_386 windows_amd64 windows_arm64"; got != want {
			t.Errorf("ci.yml: %s runs %s, want every release target natively: %s", binaries, got, want)
		}
	}
	homebrew := require("the Homebrew end-to-end test", "os: [ubuntu-latest, macos-latest]", "run: make homebrew-e2e", "name: release-snapshot")
	if homebrew != "" {
		inOrder(t, "ci.yml "+homebrew, jobs[homebrew], "uses: actions/setup-go@", "go-version-file: 'go.mod'", "run: make homebrew-e2e")
	}
	for _, job := range []string{binaries, homebrew} {
		if job != "" && snapshot != "" && !strings.Contains(jobs[job], "needs: ["+snapshot+"]") {
			t.Errorf("ci.yml: %s must use the archives of %s", job, snapshot)
		}
	}
	for name, job := range jobs {
		if !regexp.MustCompile(`(?m)^    timeout-minutes: \d+$`).MatchString(job) {
			t.Errorf("ci.yml: job %s has no timeout-minutes", name)
		}
	}
	if strings.Contains(all, "pull_request_target") {
		t.Error("ci.yml: pull_request_target would run pull requests with the base repository's secrets")
	}
	if !strings.Contains(top, "concurrency:\n  group: ci-${{ github.workflow }}-${{ github.ref }}\n  cancel-in-progress: ${{ github.event_name == 'pull_request' }}") {
		t.Error("ci.yml: only a pull request's older runs may be cancelled (never main's, never the release's)")
	}

	// The gate: needs every other job, runs whatever they did, and fails
	// unless each succeeded and the race shards ran exactly the unit tests.
	gate := require("the gate", "check-coverage.py reports windows macos")
	// hephaestus publish requires the gate by this name: it is the check the
	// main ruleset requires and the one publish looks for.
	if gate != "" && gate != "ci-gate" {
		t.Errorf("ci.yml: the gate is job %s; hephaestus publish requires it to be named ci-gate", gate)
	}
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
		if !strings.Contains(job, "        if: always()\n        run: python3 .github/ci/check-native.py reports\n") {
			t.Errorf("ci.yml: %s must check, whatever the jobs did, that the macOS native shards ran every package once (check-native.py)", gate)
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
func matrixValues(t tb, job, key string) []string {
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
func raceShards(t tb) []string {
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
func fuzzList(t tb) []fuzzEntry {
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

func fuzzGroups(t tb) []string {
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

// Every workflow runs the one Hephaestus of the Makefile's HEPHAESTUS_VERSION,
// pinned to the commit of its tag, and nothing runs GoReleaser any more.
func TestHephaestusVersionIsConsistent(t *testing.T) {
	mk, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^HEPHAESTUS_VERSION \?= (v\S+)$`).FindSubmatch(mk)
	if m == nil {
		t.Fatal("Makefile: no HEPHAESTUS_VERSION ?= v...")
	}
	if !strings.HasSuffix(hephaestusAction, " # "+string(m[1])) {
		t.Errorf("Makefile: HEPHAESTUS_VERSION %s, the workflows pin %s", m[1], hephaestusAction)
	}
	if !strings.Contains(string(mk), "HEPHAESTUS ?= go run github.com/magicdrive/hephaestus@$(HEPHAESTUS_VERSION)") {
		t.Error("Makefile: HEPHAESTUS must run HEPHAESTUS_VERSION")
	}
	for _, path := range []string{".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		uses := regexp.MustCompile(`(?m)^\s+(uses: magicdrive/hephaestus@.*)$`).FindAllStringSubmatch(string(b), -1)
		if len(uses) == 0 {
			t.Errorf("%s: runs no Hephaestus", path)
		}
		for _, u := range uses {
			if u[1] != hephaestusAction {
				t.Errorf("%s: %q, want %q", path, u[1], hephaestusAction)
			}
		}
	}
	for _, path := range []string{"Makefile", ".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		b, _ := os.ReadFile(path)
		if strings.Contains(strings.ToLower(string(b)), "goreleaser") {
			t.Errorf("%s still runs GoReleaser", path)
		}
	}
	// hephaestus.yaml builds with its command, not from GoReleaser's output.
	if b, _ := os.ReadFile("hephaestus.yaml"); regexp.MustCompile(`(?m)^\s*(goreleaser|brews_from|existing):`).Match(b) {
		t.Error("hephaestus.yaml imports builds or formulae instead of building them")
	}
	for _, path := range []string{".goreleaser.yml", ".goreleaser.yaml"} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s exists: Hephaestus releases Ark", path)
		}
	}
}

// The release keeps the shape of the GoReleaser releases up to v6.7.0, which
// installers, the Homebrew formula and users' scripts rely on: eight targets
// (386 included, no darwin/386), tar.gz for Windows too, ark or ark.exe and
// the completion scripts at the same paths, ark_checksums.txt, and a formula
// in the root of magicdrive/homebrew-tap installing the same completions.
// .github/release/verify-dist.sh checks the archives themselves.
func TestHephaestus_ReleaseAssetsKeepTheirShape(t *testing.T) {
	b, err := os.ReadFile("hephaestus.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	for _, want := range []string{
		"project:\n  name: ark\n",
		"    matrix:\n      os: [darwin, linux, windows]\n      arch: [\"386\", amd64, arm64]\n      exclude:\n        - os: darwin\n          arch: \"386\"\n",
		"    command: [bash, .github/release/build.sh]\n",
		"      CGO_ENABLED: \"0\"\n",
		"    output: \"dist/bin/{{ .OS }}_{{ .Arch }}/ark{{ .Ext }}\"\n",
		"    name: \"{{ .Project }}_{{ .Version }}_{{ .OS }}_{{ .Arch }}\"\n    # Windows too: the releases have always shipped tar.gz only.\n    format: tar.gz\n",
		"      - src: \"dist/bin/{{ .OS }}_{{ .Arch }}/ark{{ .Ext }}\"\n        dst: \"ark{{ .Ext }}\"\n        mode: \"0755\"\n",
		"      - src: misc/completions/ark-completion.sh\n        dst: completions/ark-completion.sh\n        mode: \"0644\"\n",
		"      - src: misc/completions/bash/ark-completion.bash\n        dst: completions/bash/ark-completion.bash\n        mode: \"0644\"\n",
		"      - src: misc/completions/fish/ark.fish\n        dst: completions/fish/ark.fish\n        mode: \"0644\"\n",
		"      - src: misc/completions/zsh/_ark\n        dst: completions/zsh/_ark\n        mode: \"0644\"\n",
		"checksum:\n  name: \"{{ .Project }}_checksums.txt\"\n",
		"    owner: magicdrive\n    name: ark\n",
		"  - name: ark\n    install: |\n      bin.install \"ark\"\n",
		"      files:\n        bash: completions/bash/ark-completion.bash\n        zsh: completions/zsh/_ark\n        fish: completions/fish/ark.fish\n",
		"      assert_match version.to_s, shell_output(\"#{bin}/ark --version\")\n",
		"    repository:\n      owner: magicdrive\n      name: homebrew-tap\n      pull_request:\n        enabled: true\n",
		"    commit_msg_template: \"Brew formula update for {{ .ProjectName }} version {{ .Tag }}\"\n",
		"    skip_upload: auto\n",
		"preflight:\n  workflow: release.yml\n  environment: release\n",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("hephaestus.yaml lacks\n%s", want)
		}
	}
	// The tap keeps its formulae at its root: a directory (Formula/) would
	// hide them from Homebrew. Nothing wraps the archive's content in a
	// directory, nor changes the format per system.
	for _, bad := range []string{"directory:", "folder:", "wrap_in_directory", "format_overrides", "zip"} {
		if strings.Contains(cfg, bad) {
			t.Errorf("hephaestus.yaml: %s changes the release's shape", bad)
		}
	}
	if n := strings.Count(cfg, "  - src: "); n != 5 {
		t.Errorf("hephaestus.yaml: %d archive file entries, want 5 (ark and four completion scripts)", n)
	}
}

// Only a published release is a release build for the index cache
// (internal/cache/build.go): .github/release/build.sh, the build command of
// hephaestus.yaml, sets internal/cache.release for a tagged build of a clean
// tree whose tag names HEAD, leaves it empty for a snapshot, and fails —
// rather than build something that would claim to be the release — for a
// modified tree, an untracked file, a tag that names another commit or does
// not exist, a version that is not the tag's, cgo, or another Go toolchain
// than go.mod's. No other build sets it.
func TestReleaseBuild_OnlyReleasesShareTheReleaseCache(t *testing.T) {
	script, err := os.ReadFile(".github/release/build.sh")
	if err != nil {
		t.Fatal(err)
	}
	const flags = `-ldflags "-s -w -X \"main.version=$ARK_VERSION\" -X \"github.com/magicdrive/ark/internal/cache.release=$release\""`
	if !strings.Contains(string(script), flags+` \`+"\n\t-o \"$HEPHAESTUS_OUTPUT\" ./main.go\n") {
		t.Errorf(".github/release/build.sh: the build lacks %s -o \"$HEPHAESTUS_OUTPUT\" ./main.go", flags)
	}
	cfg, err := os.ReadFile("hephaestus.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`ARK_VERSION: "v{{ .Version }}"`, `ARK_TAG: "{{ .Tag }}"`} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("hephaestus.yaml: the build's env lacks %s", want)
		}
	}
	src, err := os.ReadFile("internal/cache/build.go")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^var release string$`).Match(src) {
		t.Error("internal/cache/build.go: no `var release string` for the release flag to set")
	}
	for _, path := range []string{"Makefile", ".github/workflows/ci.yml", ".github/workflows/release.yml", "hephaestus.yaml"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "internal/cache.release") {
			t.Errorf("%s sets internal/cache.release: only .github/release/build.sh may", path)
		}
	}

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found: the release builds on Linux runners")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}
	gomod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	gover := "go" + string(regexp.MustCompile(`(?m)^go (\S+)$`).FindSubmatch(gomod)[1])

	// A repository with build.sh, go.mod and a stand-in for go that records
	// the build's arguments and answers `go env GOVERSION` for GOTOOLCHAIN.
	repo := t.TempDir()
	write := func(path, content string, mode os.FileMode) {
		p := filepath.Join(repo, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(".github/release/build.sh", string(script), 0o755)
	write("go.mod", string(gomod), 0o644)
	write(".gitignore", "/dist\n/calls\n", 0o644)
	stub := t.TempDir()
	if err := os.WriteFile(filepath.Join(stub, "go"), []byte(`#!/bin/sh
case "$1" in
env) echo "${STUB_GOVERSION:-$GOTOOLCHAIN}"; exit 0 ;;
build) printf '%s\n' "$@" > "$CALLS"; mkdir -p "$(dirname "$HEPHAESTUS_OUTPUT")"; : > "$HEPHAESTUS_OUTPUT"; exit 0 ;;
esac
exit 9
`), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", ".")
	run("commit", "-q", "-m", "release")
	run("tag", "v9.8.7")

	calls := filepath.Join(repo, "calls")
	build := func(env ...string) (string, error) {
		os.Remove(calls)
		cmd := exec.Command(bash, ".github/release/build.sh")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "PATH="+stub+string(os.PathListSeparator)+os.Getenv("PATH"), "CALLS="+calls,
			"HEPHAESTUS_OUTPUT="+filepath.Join(repo, "dist", "ark"), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0",
			"GOFLAGS=-trimpath", "GOAMD64=v3", "STUB_GOVERSION=")
		cmd.Env = append(cmd.Env, env...)
		out, err := cmd.CombinedOutput()
		b, _ := os.ReadFile(calls)
		return string(out) + "\n" + string(b), err
	}
	ldflags := func(version, release string) string {
		return "-s -w -X \"main.version=" + version + "\" -X \"github.com/magicdrive/ark/internal/cache.release=" + release + "\""
	}
	wantBuild := func(what, out, version, release string) {
		t.Helper()
		want := "build\n-ldflags\n" + ldflags(version, release) + "\n-o\n" + filepath.Join(repo, "dist", "ark") + "\n./main.go\n"
		if !strings.HasSuffix(out, "\n"+want) {
			t.Errorf("%s: go was run as\n%s\nwant\n%s", what, out, want)
		}
	}

	out, err := build("ARK_VERSION=v0.0.0-SNAPSHOT", "ARK_TAG=")
	if err != nil {
		t.Fatalf("snapshot: %v\n%s", err, out)
	}
	wantBuild("snapshot", out, "v0.0.0-SNAPSHOT", "")
	if !strings.Contains(out, "("+gover+", release=<empty>)") {
		t.Errorf("snapshot: not built by %s:\n%s", gover, out)
	}
	out, err = build("ARK_VERSION=v9.8.7", "ARK_TAG=v9.8.7")
	if err != nil {
		t.Fatalf("release: %v\n%s", err, out)
	}
	wantBuild("release", out, "v9.8.7", "true")

	refused := func(what, want string, env ...string) {
		t.Helper()
		out, err := build(env...)
		if err == nil || strings.Contains(out, "\nbuild\n") || !strings.Contains(out, want) {
			t.Errorf("%s: must fail with %q before building: %v\n%s", what, want, err, out)
		}
	}
	release := []string{"ARK_VERSION=v9.8.7", "ARK_TAG=v9.8.7"}
	refused("another version than the tag's", "does not match the tag", "ARK_VERSION=v9.8.8", "ARK_TAG=v9.8.7")
	refused("a tag that does not exist", "no tag v9.9.9", "ARK_VERSION=v9.9.9", "ARK_TAG=v9.9.9")
	refused("cgo", "CGO_ENABLED must be 0", append(release, "CGO_ENABLED=1")...)
	refused("another Go", "go.mod names "+gover, append(release, "STUB_GOVERSION=go1.0.0")...)
	refused("no version", "ARK_VERSION", "ARK_TAG=v9.8.7", "ARK_VERSION=")

	write("untracked.go", "package main\n", 0o644)
	refused("an untracked file", "modified tree", release...)
	os.Remove(filepath.Join(repo, "untracked.go"))
	write("go.mod", string(gomod)+"\n", 0o644)
	refused("a modified file", "modified tree", release...)
	run("commit", "-q", "-am", "after the tag")
	refused("a tag that names another commit", "a release is built from its tag", release...)
	// A snapshot of the same tree is still a snapshot.
	if out, err := build("ARK_VERSION=v0.0.0-SNAPSHOT", "ARK_TAG="); err != nil {
		t.Errorf("snapshot of a later commit: %v\n%s", err, out)
	} else {
		wantBuild("snapshot of a later commit", out, "v0.0.0-SNAPSHOT", "")
	}
}

// recorder runs a workflow check without failing the test: it records what
// the check reports, and a Fatal stops the check (fatalStop) as it would stop
// a test.
type recorder struct{ errs []string }

type fatalStop struct{}

func (r *recorder) Helper()           {}
func (r *recorder) Error(args ...any) { r.errs = append(r.errs, fmt.Sprint(args...)) }
func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}
func (r *recorder) Fatal(args ...any) { r.Error(args...); panic(fatalStop{}) }
func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	panic(fatalStop{})
}

// reported runs check on a recorder and returns what it reported.
func reported(check func(tb)) []string {
	r := &recorder{}
	func() {
		defer func() {
			if v := recover(); v != nil {
				if _, ok := v.(fatalStop); !ok {
					panic(v)
				}
			}
		}()
		check(r)
	}()
	return r.errs
}

// The release and CI workflow checks are mutation-tested: each mutation
// below breaks one convention the release relies on — the gates, the
// tokens, the plan and the tap steps hephaestus publish verifies by name,
// the attestation of every asset, the Go the builds need now that the
// action provides none — and the check must report it. A mutation whose
// text is no longer in the workflow fails too, so the list follows the
// workflows.
func TestWorkflowChecks_CatchMutations(t *testing.T) {
	const tapToken = "          tap-token: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}\n"
	ifTap := "        if: " + tapCondition + "\n"
	// replace mutates the one occurrence of old.
	replace := func(old, new string) func(string) (string, bool) {
		return func(src string) (string, bool) {
			return strings.Replace(src, old, new, 1), strings.Count(src, old) == 1
		}
	}
	// move moves the one step block to just before the one occurrence of before.
	move := func(block, before string) func(string) (string, bool) {
		return func(src string) (string, bool) {
			if strings.Count(src, block) != 1 {
				return src, false
			}
			src = strings.Replace(src, block, "", 1)
			return strings.Replace(src, before, block+before, 1), strings.Count(src, before) == 1
		}
	}
	planStep := "      - name: Plan the release\n"
	type mutation struct {
		name   string
		mutate func(string) (string, bool)
	}
	release, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	if i, j := strings.Index(string(release), planStep), strings.Index(string(release), "      - name: The tap token is set\n"); i >= 0 && j > i {
		planStep = string(release)[i:j]
	}
	for _, wf := range []struct {
		path      string
		check     func(tb, string)
		mutations []mutation
	}{
		{".github/workflows/release.yml", checkReleaseWorkflow, []mutation{
			{"rehearsal without the plan condition", replace("succeeded for a stable release.)\n"+ifTap, "succeeded for a stable release.)\n")},
			{"tap step without the plan condition", replace("the owner merges the pull request.\n"+ifTap, "the owner merges the pull request.\n")},
			{"token check without the plan condition", replace("never reaches this step.\n"+ifTap, "never reaches this step.\n")},
			{"tap step on another condition", replace("the owner merges the pull request.\n"+ifTap, "the owner merges the pull request.\n        if: steps.plan.outputs.prerelease == 'false'\n")},
			{"rehearsal under its v1.1.0 name", replace("- name: "+tapRehearsalStep, "- name: Rehearse the formula and the tap pull request (read-only)")},
			{"tap step under its v1.1.0 name", replace("- name: "+tapPublishStep, "- name: Generate the formula and open the tap pull request")},
			{"no plan step id", replace("        id: plan\n", "")},
			{"plan step runs another command", replace("          command: plan\n", "          command: check\n")},
			{"plan step gets the tap token", replace("          command: plan\n", "          command: plan\n"+tapToken)},
			{"plan after the tap rehearsal", move(planStep, "      - name: Attest build provenance\n")},
			{"plan after the tap step", func(src string) (string, bool) {
				if strings.Count(src, planStep) != 1 || !strings.HasSuffix(src, "\n") {
					return src, false
				}
				return strings.Replace(src, planStep, "", 1) + "\n" + strings.TrimSuffix(planStep, "\n"), true
			}},
			{"release step made conditional", replace("      - name: Publish the GitHub release\n", "      - name: Publish the GitHub release\n"+ifTap)},
			{"attestation made conditional", replace("      - name: Attest build provenance\n", "      - name: Attest build provenance\n"+ifTap)},
			{"a third step gets the tap token", replace("          command: release\n", "          command: release\n"+tapToken)},
			{"release step publishes with the tap token", replace("          command: release\n", "          command: release\n          github-token: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}\n")},
			{"checksum file not attested", replace("            dist/ark_checksums.txt\n", "")},
			{"no release environment", replace("    environment: release\n", "")},
			{"release does not wait for the build", replace("    needs: [provenance, ci, build]\n", "    needs: [provenance, ci]\n")},
			{"release does not wait for provenance", replace("    needs: [provenance, ci, build]\n", "    needs: [ci, build]\n")},
			{"build does not wait for provenance", replace("    needs: [provenance]\n", "")},
			{"provenance from a shallow clone", replace("          fetch-depth: 0\n", "")},
			{"provenance allows another branch", replace(`--sha "$GITHUB_SHA" main`+"\n", `--sha "$GITHUB_SHA" main topic/tree-sitter`+"\n")},
			{"provenance check skipped", replace("      - name: Verify the tag is on an allowed release branch\n", "      - name: Verify the tag is on an allowed release branch\n        if: false\n")},
			{"rehearsal publishes", replace("          dry-run: \"true\"\n", "")},
			{"build without go.mod's Go", replace("          go-version-file: 'go.mod'\n          cache: false\n", "          go-version: stable\n          cache: false\n")},
			{"build with another Go as well", replace("          go-version-file: 'go.mod'\n          cache: false\n", "          go-version-file: 'go.mod'\n          go-version: stable\n          cache: false\n")},
			{"build without setup-go", replace("        uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7\n        with:\n          go-version-file: 'go.mod'\n          cache: false\n", "        run: go version\n")},
			{"plan pinned to a branch", replace("uses: magicdrive/hephaestus@e544acf59c79cbafa04a48002b0f30f91a02f8ba # v1.3.0\n        with:\n          command: plan", "uses: magicdrive/hephaestus@main # v1.3.0\n        with:\n          command: plan")},
			{"build verifies nothing", replace(`        run: .github/release/verify-dist.sh dist --tag "$GITHUB_REF_NAME"`+"\n\n      - name: Upload dist", "        run: true\n\n      - name: Upload dist")},
		}},
		{".github/workflows/ci.yml", checkCIWorkflow, []mutation{
			{"gate under another name", replace("\n  ci-gate:\n", "\n  gate:\n")},
			{"gate skipped when a job fails", replace("  ci-gate:\n    if: always()\n", "  ci-gate:\n")},
			{"gate does not need the Homebrew test", replace(", release-binaries, homebrew-e2e]", ", release-binaries]")},
			{"native shards fail fast", replace("      fail-fast: false\n      matrix:\n        shard: [1, 2, 3, 4]\n    runs-on: macos-latest\n", "      fail-fast: true\n      matrix:\n        shard: [1, 2, 3, 4]\n    runs-on: macos-latest\n")},
			{"a native shard dropped", replace("        shard: [1, 2, 3, 4]\n    runs-on: macos-latest\n", "        shard: [1, 2, 3]\n    runs-on: macos-latest\n")},
			{"native shards not planned on macOS", replace(`          python3 .github/ci/race-shard.py all > "$RUNNER_TEMP/reports/native-${{ matrix.shard }}-macos.plan.txt"`+"\n", "")},
			{"native shard runs the whole suite without race-shard.py", replace(`python3 .github/ci/race-shard.py run "${{ matrix.shard }}" "$RUNNER_TEMP/native.json"`, `go test -json ./... > "$RUNNER_TEMP/native.json"`)},
			{"gate does not check the native shards", replace("\n      - name: macOS native shards ran every package exactly once\n        if: always()\n        run: python3 .github/ci/check-native.py reports\n", "\n")},
			{"snapshot with another Go", replace("          go-version-file: 'go.mod'\n          cache-dependency-path: |\n            go.sum\n            .github/ci/cache-keys/release-snapshot\n", "          go-version: stable\n          cache-dependency-path: |\n            go.sum\n            .github/ci/cache-keys/release-snapshot\n")},
			{"Homebrew test without Go", replace("      - name: Set up Go\n        uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7\n        with:\n          go-version-file: 'go.mod'\n          cache-dependency-path: |\n            go.sum\n            .github/ci/cache-keys/homebrew-e2e\n\n", "")},
		}},
	} {
		b, err := os.ReadFile(wf.path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if errs := reported(func(r tb) { wf.check(r, src) }); len(errs) != 0 {
			t.Fatalf("%s: the check fails on the workflow itself: %q", wf.path, errs)
		}
		for _, m := range wf.mutations {
			mutated, ok := m.mutate(src)
			if !ok || mutated == src {
				t.Errorf("%s: mutation %q no longer applies to the workflow", wf.path, m.name)
				continue
			}
			if errs := reported(func(r tb) { wf.check(r, mutated) }); len(errs) == 0 {
				t.Errorf("%s: mutation %q passes the check", wf.path, m.name)
			} else {
				t.Logf("%s: %s: %s", wf.path, m.name, errs[0])
			}
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
	if uploads != 4 {
		t.Errorf("ci.yml: %d test report uploads, want 4 (unit, race, filesystem-security, native-tests)", uploads)
	}
	gate := jobs["ci-gate"]
	steps := []string{
		"pattern: test-report-*\n          path: downloaded-reports",
		// whatever the jobs did: a skipped selection would hide the coverage
		// check's own diagnosis behind a missing directory
		"if: always()\n        run: python3 .github/ci/select-reports.py downloaded-reports reports",
		"python3 .github/ci/check-coverage.py reports windows macos",
		"if: always()\n        run: python3 .github/ci/check-native.py reports",
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

// check-native.py, the gate's check of the macOS native shards: each shard's
// report against the plan the shards ran (race-shard.py all on macOS). It
// passes only if every package of the plan ran exactly in its shard; a
// missing, extra or failed package, a missing shard (a cancelled or skipped
// leg uploads nothing) or plans that differ fail it. A package passed from go
// test's cache counts as run, as it did for the job's `go test -race ./...`.
func TestCheckNative_EveryPackageOnceInItsShard(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found: the CI gate runs check-native.py")
	}
	const m = "github.com/magicdrive/ark"
	plan := []string{
		"1 " + m + "/internal/tsparse",
		"2 " + m + "/internal/resolver",
		"3 " + m + "/internal/languages/golang",
		"3 " + m + "/tests/integration",
		"4 " + m,
		"4 " + m + "/internal/mcp",
	}
	ran := map[string][]string{ // shard -> packages.tsv lines
		"1": {m + "/internal/tsparse\tpass\t30.00\t"},
		"2": {m + "/internal/resolver\tpass\t40.00\t"},
		"3": {m + "/internal/languages/golang\tpass\t35.00\t", m + "/tests/integration\tpass\t20.00\t"},
		"4": {m + "\tpass\t5.00\t", m + "/internal/mcp\tpass\t9.00\t"},
	}
	type fixture struct {
		plans map[string][]string
		ran   map[string][]string
	}
	base := func() fixture {
		f := fixture{map[string][]string{}, map[string][]string{}}
		for s, lines := range ran {
			f.plans[s] = slices.Clone(plan)
			f.ran[s] = slices.Clone(lines)
		}
		return f
	}
	write := func(f fixture) string {
		dir := t.TempDir()
		for s := range f.plans {
			d := filepath.Join(dir, "test-report-native-"+s)
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(d, "native-"+s+"-macos.plan.txt"), []byte(strings.Join(f.plans[s], "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if lines, ok := f.ran[s]; ok {
				if err := os.WriteFile(filepath.Join(d, "native-"+s+"-macos.packages.tsv"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		return dir
	}
	check := func(f fixture) (string, error) {
		out, err := exec.Command(py, ".github/ci/check-native.py", write(f)).CombinedOutput()
		return string(out), err
	}

	if out, err := check(base()); err != nil {
		t.Fatalf("every package once in its shard: %v\n%s", err, out)
	}
	cached := base() // a rerun with the same sources: results from the shard's cache
	cached.ran["3"][1] = m + "/tests/integration\tpass\t0.00\tcached"
	if out, err := check(cached); err != nil {
		t.Fatalf("a package passed from the cache: %v\n%s", err, out)
	}
	split := base() // a package split in two (as race-shards.txt may): one half per shard
	for s := range split.plans {
		split.plans[s] = append(split.plans[s], "1 "+m+"/internal/golden -run ^TestA", "2 "+m+"/internal/golden -skip ^TestA")
	}
	split.ran["1"] = append(split.ran["1"], m+"/internal/golden\tpass\t3.00\t")
	split.ran["2"] = append(split.ran["2"], m+"/internal/golden\tpass\t4.00\t")
	if out, err := check(split); err != nil {
		t.Fatalf("a split package, one half per shard: %v\n%s", err, out)
	}

	for name, tc := range map[string]struct {
		mutate func(*fixture)
		want   string
	}{
		"a package did not run": {func(f *fixture) { f.ran["3"] = f.ran["3"][:1] }, "tests/integration did not run"},
		"a package ran in two shards": {func(f *fixture) {
			f.ran["4"] = append(f.ran["4"], m+"/internal/resolver\tpass\t40.00\t")
		}, "internal/resolver ran, but the plan does not give it to this shard"},
		"a shard uploaded nothing (cancelled, skipped)": {func(f *fixture) { delete(f.plans, "2"); delete(f.ran, "2") }, "native-2-macos: 0 plan.txt files"},
		"a shard has no report":                         {func(f *fixture) { delete(f.ran, "4") }, "native-4-macos: 0 packages.tsv files"},
		"the shards' plans differ":                      {func(f *fixture) { f.plans["4"] = f.plans["4"][:5] }, "the shards' plans differ"},
		"a package failed": {func(f *fixture) {
			f.ran["2"] = []string{m + "/internal/resolver\tfail\t40.00\t"}
		}, "internal/resolver failed"},
		"a package in two shards of the plan": {func(f *fixture) {
			for s := range f.plans {
				f.plans[s] = append(f.plans[s], "1 "+m+"/internal/mcp")
			}
			f.ran["1"] = append(f.ran["1"], m+"/internal/mcp\tpass\t9.00\t")
		}, "plan: " + m + "/internal/mcp in"},
	} {
		f := base()
		tc.mutate(&f)
		out, err := check(f)
		if err == nil {
			t.Errorf("%s: check-native.py passed:\n%s", name, out)
		} else if !strings.Contains(out, tc.want) {
			t.Errorf("%s: want %q in:\n%s", name, tc.want, out)
		}
	}
}
