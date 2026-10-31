package actions

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/languages/yaml/yamldoc"
)

func extract(t *testing.T, file, src string) Result {
	t.Helper()
	r, ok := Extract(file, yamldoc.Parse([]byte(src)), 1000, 20000)
	if !ok {
		t.Fatalf("%s is no GitHub Actions file", file)
	}
	return r
}

// decls renders the declarations as "kind qualified".
func decls(r Result) []string {
	var out []string
	for _, s := range r.Symbols {
		out = append(out, string(s.Kind)+" "+s.Qualified)
	}
	return out
}

// refs renders the references as "container -kind-> target", the target
// being the identity with its evidence (file: path evidence, repo: an
// identity of the repository, cap) or "dynamic" or the name alone.
func refs(r Result) []string {
	var out []string
	for _, x := range r.Refs {
		target := x.NameQualified
		switch {
		case x.Dynamic:
			target = "dynamic " + x.Name
		case x.FileIdentity:
			target = "file:" + target
		}
		if x.InRepository && !x.Dynamic {
			target += " [repo]"
		}
		if x.Cap != "" {
			target += " [" + x.Cap + "]"
		}
		out = append(out, fmt.Sprintf("%s -%s-> %s (%s)", x.Container, x.Kind, target, x.Name))
	}
	return out
}

func problems(r Result) []string {
	var out []string
	for _, p := range r.Problems {
		out = append(out, p.Code)
	}
	return out
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got:\n  %s\n want:\n  %s", what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func has(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("%s lacks %q:\n  %s", what, w, strings.Join(got, "\n  "))
		}
	}
}

const wf = ".github/workflows/ci.yml"

func TestClassify(t *testing.T) {
	for _, c := range []struct {
		file, src string
		kind      fileKind
		root      string
	}{
		{".github/workflows/ci.yml", "on: push\n", workflowFile, ""},
		{".github/workflows/ci.yaml", "on: push\n", workflowFile, ""},
		{"sub/repo/.github/workflows/ci.yml", "on: push\n", workflowFile, "sub/repo/"},
		{`.github\workflows\ci.yml`, "on: push\n", workflowFile, ""},
		// GitHub reads only the directory's own files.
		{".github/workflows/nested/ci.yml", "on: push\n", notActions, ""},
		{".github/workflows/README.md", "on: push\n", notActions, ""},
		{"workflows/ci.yml", "on: push\n", notActions, ""},
		{"action.yml", "runs:\n  using: node20\n", actionFile, ""},
		{".github/actions/a/action.yaml", "runs:\n  using: node20\n", actionFile, ""},
		{"x/.github/actions/a/action.yml", "runs:\n  using: node20\n", actionFile, "x/"},
		// An action.yml without runs is no action's metadata.
		{"deploy/action.yml", "name: x\n", notActions, ""},
		{"config.yml", "runs:\n  using: node20\n", notActions, ""},
	} {
		kind, root := classify(c.file, yamldoc.Parse([]byte(c.src)))
		if kind != c.kind || root != c.root {
			t.Errorf("%s: kind %d root %q, want %d %q", c.file, kind, root, c.kind, c.root)
		}
	}
}

func TestTriggers_EveryForm(t *testing.T) {
	for src, want := range map[string][]string{
		"on: push\n":                      {"push"},
		"on: [push, pull_request]\n":      {"push", "pull_request"},
		"\"on\":\n  workflow_dispatch:\n": {"workflow_dispatch"},
		"on:\n  push:\n    branches: [main]\n  pull_request:\n  schedule:\n    - cron: '0 0 * * *'\n  workflow_call:\n    inputs:\n      v:\n        type: string\n": {"push", "pull_request", "schedule", "workflow_call"},
		"on: &ev [push]\n":    {"push"},
		"name: no triggers\n": nil,
	} {
		r := extract(t, wf, src)
		var got []string
		for _, s := range r.Symbols {
			if s.Kind == "trigger" {
				got = append(got, s.Name)
				if s.Qualified != wf+"::on:"+s.Name || s.Parent != wf {
					t.Errorf("%q: trigger %+v", src, s)
				}
			}
		}
		equal(t, src, got, want)
		for _, name := range want {
			has(t, src, refs(r), fmt.Sprintf("%s -contains-> %s::on:%s [repo] (%s::on:%s)", wf, wf, name, wf, name))
		}
	}
}

func TestWorkflow_Settings(t *testing.T) {
	r := extract(t, wf, `name: CI
on: push
permissions:
  contents: read
concurrency:
  group: ci-${{ github.ref }}
env:
  A: b
defaults:
  run:
    shell: bash
jobs:
  build:
    runs-on: ubuntu-latest
    if: github.event_name == 'push'
    permissions: write-all
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest]
        go: ['1.26', '1.27']
    concurrency: build
    environment: production
    outputs:
      v: ${{ steps.s.outputs.v }}
    timeout-minutes: 10
    steps:
      - id: s
        run: echo v=1 >> "$GITHUB_OUTPUT"
        shell: bash
        working-directory: sub
        timeout-minutes: 5
        if: always()
        env:
          X: y
`)
	equal(t, "declarations", decls(r), []string{
		"workflow " + wf,
		"trigger " + wf + "::on:push",
		"key " + wf + "#/permissions",
		"key " + wf + "#/concurrency",
		"key " + wf + "#/env",
		"key " + wf + "#/defaults",
		"job " + wf + "::build",
		"key " + wf + "#/jobs/build/if",
		"key " + wf + "#/jobs/build/permissions",
		"key " + wf + "#/jobs/build/strategy",
		"key " + wf + "#/jobs/build/concurrency",
		"key " + wf + "#/jobs/build/environment",
		"key " + wf + "#/jobs/build/outputs",
		"step " + wf + "::build::s",
	})
	if r.Symbols[0].Signature != "name: CI" {
		t.Errorf("workflow signature %q", r.Symbols[0].Signature)
	}
	for _, s := range r.Symbols {
		switch s.Qualified {
		case wf + "::build":
			if s.Signature != "runs-on: ubuntu-latest" || s.Parent != wf {
				t.Errorf("job %+v", s)
			}
		case wf + "::build::s":
			if s.Signature != "run (bash)" || s.Parent != wf+"::build" {
				t.Errorf("step %+v", s)
			}
		case wf + "#/jobs/build/permissions":
			if s.Parent != wf+"::build" {
				t.Errorf("job key %+v", s)
			}
		}
	}
	equal(t, "problems", problems(r), nil)
}

func TestNeeds_StringListAliasAndMissing(t *testing.T) {
	r := extract(t, wf, `on: push
jobs:
  a:
    runs-on: x
  b:
    needs: a
    runs-on: x
  c:
    needs: [a, b, missing]
    runs-on: x
  d:
    needs: &deps [a, c]
    runs-on: x
  e:
    needs: *deps
    runs-on: x
  f:
    needs: ${{ fromJSON('["a"]') }}
    runs-on: x
`)
	equal(t, "needs", filter(refs(r), "-depends_on->"), []string{
		wf + "::b -depends_on-> " + wf + "::a [repo] (a)",
		wf + "::c -depends_on-> " + wf + "::a [repo] (a)",
		wf + "::c -depends_on-> " + wf + "::b [repo] (b)",
		wf + "::c -depends_on-> " + wf + "::missing [repo] (missing)",
		wf + "::d -depends_on-> " + wf + "::a [repo] (a)",
		wf + "::d -depends_on-> " + wf + "::c [repo] (c)",
		wf + "::e -depends_on-> " + wf + "::a [repo] (a)",
		wf + "::e -depends_on-> " + wf + "::c [repo] (c)",
		wf + "::f -depends_on-> dynamic ${{ fromJSON('[\"a\"]') }} (${{ fromJSON('[\"a\"]') }})",
	})
	// The aliased list is located at the alias, inside job e.
	for _, x := range r.Refs {
		if x.Container == wf+"::e" && x.Kind == "depends_on" {
			job := spanOf(r, wf+"::e")
			if x.Span.Start < job.Start || x.Span.End > job.End {
				t.Errorf("e's needs at %v, outside the job %v", x.Span, job)
			}
		}
	}
	equal(t, "problems", problems(r), nil)
}

func filter(lines []string, sub string) []string {
	var out []string
	for _, l := range lines {
		if strings.Contains(l, sub) {
			out = append(out, l)
		}
	}
	return out
}

func spanOf(r Result, qualified string) yamldoc.Span {
	for _, s := range r.Symbols {
		if s.Qualified == qualified {
			return s.Span
		}
	}
	return yamldoc.Span{Start: -1, End: -1}
}

func TestNeeds_Cycles(t *testing.T) {
	r := extract(t, wf, `on: push
jobs:
  a: {needs: c, runs-on: x}
  b: {needs: a, runs-on: x}
  c: {needs: b, runs-on: x}
  d: {needs: d, runs-on: x}
  e: {needs: [a, missing], runs-on: x}
`)
	var cyc []string
	for _, p := range r.Problems {
		if p.Code == CodeNeedsCycle {
			cyc = append(cyc, p.Message)
		}
	}
	if len(cyc) != 4 {
		t.Fatalf("cycles: %v", cyc)
	}
	for _, id := range []string{`"a"`, `"b"`, `"c"`, `"d"`} {
		if !slices.ContainsFunc(cyc, func(m string) bool { return strings.Contains(m, id) }) {
			t.Errorf("no cycle reported for %s: %v", id, cyc)
		}
	}
	// The relations stay: a cycle is the workflow's error, not Ark's guess.
	has(t, "needs", refs(r), wf+"::a -depends_on-> "+wf+"::c [repo] (c)", wf+"::d -depends_on-> "+wf+"::d [repo] (d)")
}

func TestUses_EveryForm(t *testing.T) {
	r := extract(t, wf, `on: push
jobs:
  local:
    uses: ./.github/workflows/reusable.yml
    with: {v: 1}
    secrets: inherit
  remote:
    uses: octo/shared/.github/workflows/deploy.yaml@v2
  remote-sha:
    uses: octo/shared/.github/workflows/deploy.yml@0123456789abcdef0123456789abcdef01234567
  steps:
    runs-on: x
    steps:
      - uses: actions/checkout@v4
      - uses: actions/aws/ec2@main
      - uses: ./.github/actions/setup
      - uses: ./
      - uses: docker://alpine:3.20
      - uses: ${{ matrix.action }}
  bad:
    runs-on: x
    steps:
      - uses: actions/checkout
      - uses: ./x@v1
      - uses: ./../outside
      - uses: .//abs
      - uses: docker://
      - uses: owner@v1
      - uses: a//b@v1
  bad-job-1:
    uses: octo/shared@v1
  bad-job-2:
    uses: docker://alpine
  bad-job-3:
    uses: ./../x.yml
`)
	got := filter(refs(r), "-call->")
	equal(t, "uses", got, []string{
		wf + "::local -call-> file:.github/workflows/reusable.yml [repo] (./.github/workflows/reusable.yml)",
		wf + "::remote -call-> remote:octo/shared/.github/workflows/deploy.yaml@v2 (octo/shared/.github/workflows/deploy.yaml)",
		wf + "::remote-sha -call-> remote:octo/shared/.github/workflows/deploy.yml@0123456789abcdef0123456789abcdef01234567 (octo/shared/.github/workflows/deploy.yml)",
		wf + "::steps::actions/checkout -call-> remote:actions/checkout@v4 (actions/checkout)",
		wf + "::steps::actions/aws/ec2 -call-> remote:actions/aws/ec2@main (actions/aws/ec2)",
		wf + "::steps::./.github/actions/setup -call-> .github/actions/setup [repo] [strong] (./.github/actions/setup)",
		wf + "::steps::./ -call-> . [repo] [strong] (./)",
		wf + "::steps::docker://alpine:3.20 -call-> remote:docker://alpine:3.20 (docker://alpine:3.20)",
		wf + "::steps::${{ matrix.action }} -call-> dynamic ${{ matrix.action }} (${{ matrix.action }})",
		// A local path with "@" is a path (the runner reads no ref there).
		wf + "::bad::./x@v1 -call-> x@v1 [repo] [strong] (./x@v1)",
	})
	n := 0
	for _, p := range r.Problems {
		if p.Code == CodeInvalidUses {
			n++
		}
	}
	if n != 9 {
		t.Errorf("invalid uses reported %d times, want 9: %v", n, problems(r))
	}
	for _, x := range r.Refs {
		if x.Kind == "call" && x.TargetKinds != map[bool]string{true: "workflow", false: "action"}[strings.Contains(x.Container, "::local") || strings.Contains(x.Container, "::remote")] {
			t.Errorf("%s: target kinds %q", x.Container, x.TargetKinds)
		}
	}
}

// A workflow under another directory's .github/workflows names that
// directory's paths with "./"; an action outside .github names the
// repository root's.
func TestUses_NestedRepositoryRoot(t *testing.T) {
	r := extract(t, "sub/.github/workflows/ci.yml", `on: push
jobs:
  a:
    uses: ./.github/workflows/r.yml
  b:
    runs-on: x
    steps:
      - uses: ./tools/act
`)
	has(t, "uses", refs(r),
		"sub/.github/workflows/ci.yml::a -call-> file:sub/.github/workflows/r.yml [repo] (./.github/workflows/r.yml)",
		"sub/.github/workflows/ci.yml::b::./tools/act -call-> sub/tools/act [repo] [strong] (./tools/act)")
	r = extract(t, "tools/act/action.yml", "runs:\n  using: composite\n  steps:\n    - uses: ./other\n")
	has(t, "uses", refs(r), "tools/act::./other -call-> other [repo] [strong] (./other)")
}

func TestSteps_KeysAndNameCollisions(t *testing.T) {
	r := extract(t, wf, `on: push
jobs:
  permissions:
    runs-on: x
    permissions: read-all
    steps:
      - run: a
      - run: b
      - name: Build
        run: c
      - name: Build
        run: d
      - id: build
        name: Build
        run: e
      - uses: actions/checkout@v4
      - uses: actions/checkout@v4
      - shell: bash
      - id: dup
        run: f
      - id: dup
        run: g
      - name: ビルド
        run: h
  japanese:
    runs-on: x
    steps:
      - name: テスト 🚀
        run: i
  日本語のジョブ:
    runs-on: x
`)
	steps := filter(decls(r), "step ")
	equal(t, "steps", steps, []string{
		"step " + wf + "::permissions::run",
		"step " + wf + "::permissions::run#2",
		"step " + wf + "::permissions::Build",
		"step " + wf + "::permissions::Build#2",
		"step " + wf + "::permissions::build",
		"step " + wf + "::permissions::actions/checkout",
		"step " + wf + "::permissions::actions/checkout#2",
		"step " + wf + "::permissions::step",
		"step " + wf + "::permissions::dup",
		"step " + wf + "::permissions::dup#2",
		"step " + wf + "::permissions::ビルド",
		"step " + wf + "::japanese::テスト 🚀",
	})
	// A job named like a setting is no setting: the setting is a key.
	has(t, "declarations", decls(r), "job "+wf+"::permissions", "key "+wf+"#/jobs/permissions/permissions", "job "+wf+"::japanese")
	// A step's name may be any text; a job id is what GitHub accepts (ASCII).
	has(t, "problems", problems(r), CodeDuplicateStepID, CodeInvalidJobID)
	// Every contains reference names a declaration of its own: the n-th
	// namesake by its "#n".
	var contained []string
	for _, x := range r.Refs {
		if x.Kind == "contains" {
			contained = append(contained, x.NameQualified)
		}
	}
	for _, s := range r.Symbols {
		if s.Kind == "step" && !slices.Contains(contained, s.Qualified) {
			t.Errorf("no contains reference to %s", s.Qualified)
		}
	}
	for _, x := range r.Refs {
		if x.Kind == "contains" && x.TargetKinds != "step" && x.TargetKinds != "job" && x.TargetKinds != "trigger" {
			t.Errorf("contains %s: target kinds %q", x.NameQualified, x.TargetKinds)
		}
	}
	// Where an alias repeats a list whose steps use one action twice, both
	// are at the alias, each in its own step: every step's reference is
	// stated (their ReferenceIDs differ by the container).
	r = extract(t, wf, "on: push\njobs:\n  a:\n    runs-on: x\n    steps: &s\n      - uses: ./.github/actions/cache\n      - uses: ./.github/actions/cache\n  b:\n    runs-on: x\n    steps: *s\n")
	var callers []string
	for _, x := range r.Refs {
		if x.Kind == "call" {
			callers = append(callers, x.Container)
		}
	}
	equal(t, "cache users", callers, []string{
		wf + "::a::./.github/actions/cache", wf + "::a::./.github/actions/cache#2",
		wf + "::b::./.github/actions/cache", wf + "::b::./.github/actions/cache#2",
	})
	// No reference of one kind, name and container shares a place with
	// another (their ReferenceIDs would be one).
	seen := map[refKey]bool{}
	for _, x := range r.Refs {
		k := refKey{x.Kind, x.Name, x.Container, x.Span}
		if seen[k] {
			t.Errorf("two references %v", k)
		}
		seen[k] = true
	}
}

func TestAnchors_StepsAndJobsThroughAliases(t *testing.T) {
	src := `on: push
jobs:
  first:
    runs-on: &runner ubuntu-latest
    steps: &common
      - &checkout
        uses: actions/checkout@v4
      - uses: ./.github/actions/setup
  second:
    runs-on: *runner
    steps: *common
  third:
    runs-on: x
    steps:
      - *checkout
      - run: echo
  fourth: *third-missing
`
	r, ok := Extract(wf, yamldoc.Parse([]byte(src)), 1000, 20000)
	if !ok {
		t.Fatal("not a workflow")
	}
	// An alias of an unknown anchor is a syntax error of the document:
	// nothing of it is read.
	if len(r.Symbols) != 1 {
		t.Fatalf("a broken document states its workflow only: %v", decls(r))
	}
	src = strings.Replace(src, "  fourth: *third-missing\n", "", 1)
	r = extract(t, wf, src)
	has(t, "steps", decls(r),
		"step "+wf+"::second::actions/checkout",
		"step "+wf+"::second::./.github/actions/setup",
		"step "+wf+"::third::actions/checkout",
		"step "+wf+"::third::run")
	has(t, "uses", refs(r),
		wf+"::second::./.github/actions/setup -call-> .github/actions/setup [repo] [strong] (./.github/actions/setup)",
		wf+"::third::actions/checkout -call-> remote:actions/checkout@v4 (actions/checkout)")
	// What an alias repeats is located at the alias, within the job.
	for _, job := range []string{"second", "third"} {
		js := spanOf(r, wf+"::"+job)
		for _, s := range r.Symbols {
			if s.Parent == wf+"::"+job && (s.Span.Start < js.Start || s.Span.End > js.End) {
				t.Errorf("%s at %v outside %s %v", s.Qualified, s.Span, job, js)
			}
		}
		for _, x := range r.Refs {
			if strings.HasPrefix(x.Container, wf+"::"+job) && (x.Span.Start < js.Start || x.Span.End > js.End) {
				t.Errorf("%s reference at %v outside %s %v", x.Container, x.Span, job, js)
			}
		}
	}
	for _, s := range r.Symbols {
		if s.Qualified == wf+"::second" && s.Signature != "runs-on: ubuntu-latest" {
			t.Errorf("second's runner through the alias: %q", s.Signature)
		}
	}
}

func TestMergeKeysAndDocuments(t *testing.T) {
	r := extract(t, wf, "on: push\nbase: &b {runs-on: x}\njobs:\n  a:\n    <<: *b\n    steps: [{run: x}]\n")
	has(t, "problems", problems(r), CodeMergeKey)
	r = extract(t, wf, "on: push\n---\non: pull_request\n")
	has(t, "problems", problems(r), CodeMultipleDocuments)
	equal(t, "triggers", filter(decls(r), "trigger"), []string{"trigger " + wf + "::on:push"})
	for _, src := range []string{"- on\n- push\n", "just text\n", "", "# only a comment\n"} {
		r = extract(t, wf, src)
		equal(t, fmt.Sprintf("%q", src), decls(r), []string{"workflow " + wf})
	}
	r = extract(t, wf, "- a\n")
	has(t, "problems", problems(r), CodeNotMapping)
}

// Expressions are text: whatever they hold, nothing runs, and no part of a
// script becomes a name.
func TestExpressionsAndScriptsAreText(t *testing.T) {
	r := extract(t, wf, `on: issues
jobs:
  x:
    runs-on: ${{ github.event.issue.title }}
    if: ${{ contains(github.event.issue.body, '$(rm -rf /)') }}
    steps:
      - run: |
          echo "${{ github.event.issue.title }}"
          curl -H "Authorization: token ghp_abcdefghijklmnopqrstuvwxyz0123456789" https://example.com
      - name: ${{ github.event.issue.title }}; rm -rf /
        run: x
`)
	for _, s := range r.Symbols {
		if strings.Contains(s.Name, "curl") || strings.Contains(s.Qualified, "ghp_") || strings.Contains(s.Signature, "ghp_") {
			t.Errorf("script text in a declaration: %+v", s)
		}
	}
	has(t, "steps", decls(r), "step "+wf+"::x::run", "step "+wf+"::x::${{ github.event.issue.title }}; rm -rf /")
}

func TestCompositeAction(t *testing.T) {
	r := extract(t, ".github/actions/setup/action.yml", `name: Setup
inputs:
  go-version: {required: false}
  a/b: {}
outputs:
  path:
    value: ${{ steps.s.outputs.path }}
runs:
  using: composite
  steps:
    - id: s
      uses: actions/setup-go@v5
    - uses: ./.github/actions/other
    - run: echo
      shell: bash
`)
	equal(t, "declarations", decls(r), []string{
		"action .github/actions/setup",
		"key .github/actions/setup/action.yml#/inputs",
		"key .github/actions/setup/action.yml#/outputs",
		"key .github/actions/setup/action.yml#/runs",
		"key .github/actions/setup/action.yml#/inputs/go-version",
		"key .github/actions/setup/action.yml#/inputs/a~1b",
		"key .github/actions/setup/action.yml#/outputs/path",
		"step .github/actions/setup::s",
		"step .github/actions/setup::./.github/actions/other",
		"step .github/actions/setup::run",
	})
	if r.Symbols[0].Name != "setup" || r.Symbols[0].Signature != "using: composite" {
		t.Errorf("action %+v", r.Symbols[0])
	}
	has(t, "refs", refs(r),
		".github/actions/setup -contains-> .github/actions/setup::s [repo] (.github/actions/setup::s)",
		".github/actions/setup::./.github/actions/other -call-> .github/actions/other [repo] [strong] (./.github/actions/other)")
	r = extract(t, "action.yaml", "runs:\n  using: docker\n  image: Dockerfile\n")
	if r.Symbols[0].Qualified != "." || r.Symbols[0].Name != "action" {
		t.Errorf("root action %+v", r.Symbols[0])
	}
}

func TestBudget(t *testing.T) {
	var b strings.Builder
	b.WriteString("on: push\njobs:\n")
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "  j%d:\n    runs-on: x\n    steps: &s%d\n", i, i)
		for k := 0; k < 30; k++ {
			fmt.Fprintf(&b, "      - run: %d\n", k)
		}
	}
	// An alias bomb of steps: every job repeats the others' steps.
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "  a%d:\n    runs-on: x\n    steps: *s%d\n", i, i%50)
	}
	r, _ := Extract(wf, yamldoc.Parse([]byte(b.String())), 500, 20000)
	if len(r.Symbols) != 500 {
		t.Errorf("%d declarations, want the budget of 500", len(r.Symbols))
	}
	n := 0
	for _, p := range r.Problems {
		if p.Code == CodeTruncated {
			n++
		}
	}
	if n != 1 {
		t.Errorf("truncation reported %d times", n)
	}
}

func TestNotActions(t *testing.T) {
	if _, ok := Extract("config/app.yml", yamldoc.Parse([]byte("on: push\njobs: {}\n")), 10, 20000); ok {
		t.Error("a file outside .github/workflows is no workflow")
	}
}

// The forms of uses the audit found read differently than GitHub and its
// linter: a local reusable workflow outside .github/workflows itself, a
// second "@", a trailing "/" of an action's path, credentials in a docker
// reference, a uses that is not one line.
func TestUses_AuditedForms(t *testing.T) {
	r := extract(t, wf, `on: push
jobs:
  sub:
    uses: ./sub/.github/workflows/s.yml
  nested:
    uses: ./.github/workflows/nested/n.yml
  data:
    uses: ./config/app.yaml
  ok:
    uses: ./.github/workflows/r.yaml
  s:
    runs-on: x
    steps:
      - uses: owner/repo@v1@v2
      - uses: owner/repo/sub/@v1
      - uses: docker://user:ghp_abcdefghijklmnopqrstuvwxyz0123456789@ghcr.io/o/i:1
      - uses: "a\u0000b@v1"
      - uses: "line\nbreak@v1"
`)
	equal(t, "uses", filter(refs(r), "-call->"), []string{
		wf + "::ok -call-> file:.github/workflows/r.yaml [repo] (./.github/workflows/r.yaml)",
		wf + "::s::owner/repo/sub -call-> remote:owner/repo/sub/@v1 (owner/repo/sub)",
		wf + "::s::docker://ghcr.io/o/i:1 -call-> remote:docker://ghcr.io/o/i:1 (docker://ghcr.io/o/i:1)",
	})
	n := 0
	for _, p := range r.Problems {
		if p.Code == CodeInvalidUses {
			n++
			if strings.Contains(p.Message, "ghp_") || strings.ContainsAny(p.Message, "\x00\n") {
				t.Errorf("a problem shows what is no name: %q", p.Message)
			}
		}
	}
	if n != 6 {
		t.Errorf("invalid uses reported %d times, want 6: %v", n, problems(r))
	}
	for _, s := range r.Symbols {
		if strings.Contains(s.Name, "ghp_") || strings.Contains(s.Qualified, "ghp_") || strings.Contains(s.Signature, "ghp_") {
			t.Errorf("credentials in a declaration: %+v", s)
		}
	}
}

// Job and step ids are case-insensitive, as GitHub reads them: needs of
// another spelling is the job, and two spellings of one id are duplicates.
func TestIDs_CaseInsensitive(t *testing.T) {
	r := extract(t, wf, `on: push
jobs:
  build:
    runs-on: x
    steps:
      - id: Foo
        run: a
      - id: foo
        run: b
  test:
    needs: [Build, BUILD]
    runs-on: x
  Test:
    runs-on: x
`)
	equal(t, "needs", filter(refs(r), "-depends_on->"), []string{
		wf + "::test -depends_on-> " + wf + "::build [repo] (Build)",
		wf + "::test -depends_on-> " + wf + "::build [repo] (BUILD)",
	})
	has(t, "problems", problems(r), CodeDuplicateJobID, CodeDuplicateStepID)
	equal(t, "jobs", filter(decls(r), "job "), []string{"job " + wf + "::build", "job " + wf + "::test"})
}

// A step key that a "#n" of another would make, written as a name, keeps
// every step's identity its own.
func TestSteps_NumberedNamesNeverCollide(t *testing.T) {
	r := extract(t, wf, "on: push\njobs:\n  b:\n    runs-on: x\n    steps:\n      - name: foo\n      - name: \"foo#2\"\n      - name: foo\n      - name: foo\n")
	equal(t, "steps", filter(decls(r), "step "), []string{
		"step " + wf + "::b::foo", "step " + wf + "::b::foo#2", "step " + wf + "::b::foo#3", "step " + wf + "::b::foo#4",
	})
}

// An alias bomb of needs (every job repeating one long list) states no
// more references, and reads no more needs, than the budget.
func TestNeeds_AliasBombIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("on: push\njobs:\n  j0:\n    runs-on: x\n    needs: &d [")
	for i := 0; i < 2000; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "n%d", i)
	}
	b.WriteString("]\n")
	for i := 1; i < 2000; i++ {
		fmt.Fprintf(&b, "  j%d: {runs-on: x, needs: *d}\n", i)
	}
	r, _ := Extract(wf, yamldoc.Parse([]byte(b.String())), 5000, 3000)
	if len(r.Refs) > 3000 {
		t.Errorf("%d references, over the budget", len(r.Refs))
	}
	has(t, "problems", problems(r), CodeTruncated)
}

// A needs list naming a job twice, repeated by an alias, is one reference
// of the job at the alias (two at one place of one container would share a
// ReferenceID); written twice, the two are two references. A uses longer
// than a name may be is no reference.
func TestRefs_OnceAndNames(t *testing.T) {
	r := extract(t, wf, "on: push\njobs:\n  a: {runs-on: x}\n  b: {runs-on: x, needs: &d [a, a], steps: [{uses: owner/"+strings.Repeat("r", 300)+"@v1}]}\n  c: {runs-on: x, needs: *d}\n")
	equal(t, "needs", filter(refs(r), "-depends_on->"), []string{
		wf + "::b -depends_on-> " + wf + "::a [repo] (a)", wf + "::b -depends_on-> " + wf + "::a [repo] (a)",
		wf + "::c -depends_on-> " + wf + "::a [repo] (a)",
	})
	if len(filter(refs(r), "-call->")) != 0 || !slices.Contains(problems(r), CodeInvalidUses) {
		t.Errorf("an overlong uses: %v %v", refs(r), problems(r))
	}
}

// Findings of the second audit: an event or an input named twice is one
// declaration; a trigger never shares an identity with a step of a job
// named "on"; a nested .github action takes its own repository's root; a
// local reusable workflow is checked as written; a remote workflow of a
// subdirectory is no workflow GitHub calls; a step's key leaves out the
// ref an update bot moves.
func TestSecondAudit_Actions(t *testing.T) {
	r := extract(t, wf, "on: [push, push]\njobs:\n  on:\n    runs-on: x\n    steps:\n      - name: push\n        run: x\n      - uses: actions/checkout@v4\n  a:\n    uses: ./.github/workflows/r.yml/\n  b:\n    uses: o/r/.github/workflows/sub/x.yml@v1\n")
	equal(t, "triggers and steps", filter(decls(r), "::on"), []string{
		"trigger " + wf + "::on:push", "job " + wf + "::on", "step " + wf + "::on::push", "step " + wf + "::on::actions/checkout",
	})
	if n := len(filter(problems(r), CodeInvalidUses)); n != 2 {
		t.Errorf("invalid uses %d, want 2: %v", n, problems(r))
	}
	r = extract(t, ".github/actions/a/action.yml", "inputs:\n  i: {}\n  i: {}\nruns:\n  using: node20\n")
	equal(t, "inputs", filter(decls(r), "/inputs/"), []string{"key .github/actions/a/action.yml#/inputs/i"})
	if kind, root := classify("a/.github/b/.github/actions/z/action.yml", yamldoc.Parse([]byte("runs:\n  using: node20\n"))); kind != actionFile || root != "a/.github/b/" {
		t.Errorf("nested root %q", root)
	}
}

// File IDs spelled with the OS's separator (Windows) give identities of
// the same spelling: a local reusable workflow names the workflow's ID, a
// local action the action's.
func TestUses_BackslashFileIDs(t *testing.T) {
	r := extract(t, `.github\workflows\ci.yml`, "on: push\njobs:\n  a:\n    uses: ./.github/workflows/r.yml\n  b:\n    runs-on: x\n    steps:\n      - uses: ./.github/actions/x\n")
	has(t, "uses", refs(r),
		`.github\workflows\ci.yml::a -call-> file:.github\workflows\r.yml [repo] (./.github/workflows/r.yml)`,
		`.github\workflows\ci.yml::b::./.github/actions/x -call-> .github\actions\x [repo] [strong] (./.github/actions/x)`)
	r = extract(t, `.github\actions\x\action.yml`, "runs:\n  using: node20\n")
	if r.Symbols[0].Qualified != `.github\actions\x` {
		t.Errorf("action %q", r.Symbols[0].Qualified)
	}
	r = extract(t, `.github\workflows\r.yml`, "on: workflow_call\njobs: {}\n")
	if r.Symbols[0].Qualified != `.github\workflows\r.yml` {
		t.Errorf("workflow %q", r.Symbols[0].Qualified)
	}
}

func TestBounds_ActionsProblemsReported(t *testing.T) {
	var b strings.Builder
	b.WriteString("on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n")
	for i := 0; i < 3*maxProblems; i++ {
		fmt.Fprintf(&b, "      - uses: bad%d\n", i)
	}
	r, _ := Extract(wf, yamldoc.Parse([]byte(b.String())), 10*maxProblems, 10*maxProblems)
	if len(r.Problems) != maxProblems || !r.ProblemsTruncated {
		t.Errorf("%d problems, truncated %v", len(r.Problems), r.ProblemsTruncated)
	}
}

// Findings of the third audit: a job id GitHub does not accept is no job
// (the identities of triggers and steps rest on the form), and a name with
// another invisible character is none.
func TestThirdAudit(t *testing.T) {
	r := extract(t, wf, "on: push\njobs:\n  \"on:push\": {runs-on: x}\n  \"a::b\": {runs-on: x}\n  \"ab\u061ccd\": {runs-on: x}\n  \"wj\u2060x\": {runs-on: x}\n  ok_1-x: {runs-on: x}\n")
	equal(t, "jobs", filter(decls(r), "job "), []string{"job " + wf + "::ok_1-x"})
	if n := len(filter(problems(r), CodeInvalidJobID)); n != 4 {
		t.Errorf("invalid job ids %d, want 4: %v", n, problems(r))
	}
}
