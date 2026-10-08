package main

import (
	"os"
	"regexp"
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

func TestCIWorkflow_IsTheReleaseGate(t *testing.T) {
	top, jobs := workflowJobs(t, ".github/workflows/ci.yml")
	if !strings.Contains(top, "  workflow_call:") {
		t.Error("ci.yml must be callable by the release workflow (workflow_call)")
	}
	for job, wants := range map[string][]string{
		"test":                             {"go vet ./...", "go test ./...", "go test -race ./...", "staticcheck", "-fuzz='^FuzzTypeArgParser$'"},
		"typescript-compiler-differential": {".github/ts-oracle/run.sh", "node-version: '22.17.0'"},
	} {
		for _, want := range wants {
			if !strings.Contains(jobs[job], want) {
				t.Errorf("ci.yml: job %s lacks %q", job, want)
			}
		}
	}
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
