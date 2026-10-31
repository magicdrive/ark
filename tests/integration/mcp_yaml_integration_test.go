package integration

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// copyYAMLFixture copies the YAML provider's graph fixture
// (internal/languages/yaml/testdata/repo: GitHub Actions workflows and
// actions under .github, a data file) into a new project directory.
func copyYAMLFixture(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "internal", "languages", "yaml", "testdata", "repo")
	proj := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(proj, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return proj
}

type yamlEdge struct{ From, To, Kind, Confidence string }

func yamlEdges(t *testing.T, c *rpcClient, tool, sym string) []string {
	t.Helper()
	text, isErr := c.tool(tool, map[string]any{"path": ".", "symbol": sym})
	if isErr {
		t.Fatalf("%s %s: %s", tool, sym, text)
	}
	var out struct{ Edges []yamlEdge }
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s %s: %v\n%s", tool, sym, err, text)
	}
	var lines []string
	for _, e := range out.Edges {
		lines = append(lines, e.Kind+" "+e.To+" "+e.Confidence)
	}
	slices.Sort(lines)
	return lines
}

func wantLines(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got:\n  %s\n want:\n  %s", what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// The exploration scenarios of GitHub Actions through the real binary, over
// MCP: the workflows of a repository, a workflow's jobs, a job's steps and
// the jobs it needs, the steps using actions/checkout, the jobs calling a
// reusable workflow, the triggers and permissions, the needs relation both
// ways, and the steps using a composite action of the repository.
func TestMCPYAML_GitHubActionsScenarios(t *testing.T) {
	bin := buildArk(t)
	proj := copyYAMLFixture(t)
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()

	support, _ := c.tool("get_language_support", map[string]any{})
	if !strings.Contains(support, `"language": "yaml"`) {
		t.Fatalf("yaml is not listed:\n%s", support)
	}

	// 1. The workflows of the repository.
	text, _ := c.tool("search_code", map[string]any{"path": ".", "kind": "workflow"})
	for _, w := range []string{".github/workflows/ci.yml", ".github/workflows/reusable.yml", ".github/workflows/anchors.yaml"} {
		if !strings.Contains(text, "workflow  "+w) {
			t.Errorf("search_code kind=workflow lacks %s:\n%s", w, text)
		}
	}
	if !strings.Contains(text, "Found 3 symbol(s)") {
		t.Errorf("search_code kind=workflow:\n%s", text)
	}

	// 2. A workflow's jobs.
	wantLines(t, "ci.yml contains", filter(yamlEdges(t, c, "get_callees", ".github/workflows/ci.yml"), "contains "),
		"contains .github/workflows/ci.yml::lint exact", "contains .github/workflows/ci.yml::test exact",
		"contains .github/workflows/ci.yml::build exact", "contains .github/workflows/ci.yml::release exact",
		"contains .github/workflows/ci.yml::remote exact",
		"contains .github/workflows/ci.yml::on:push exact", "contains .github/workflows/ci.yml::on:pull_request exact",
		"contains .github/workflows/ci.yml::on:workflow_dispatch exact")

	// 3 and 4. A job's steps and the jobs it needs.
	wantLines(t, "test's callees", yamlEdges(t, c, "get_callees", "ci.yml::test"),
		"contains .github/workflows/ci.yml::test::Checkout exact", "contains .github/workflows/ci.yml::test::setup exact",
		"contains .github/workflows/ci.yml::test::run exact", "contains .github/workflows/ci.yml::test::run#2 exact",
		"depends_on .github/workflows/ci.yml::lint exact")

	// 5. The steps using actions/checkout (an action outside the repository).
	text, _ = c.tool("find_references", map[string]any{"path": ".", "name": "actions/checkout"})
	var refs struct {
		Results []struct{ Container, File string }
		Count   int
	}
	if err := json.Unmarshal([]byte(text), &refs); err != nil {
		t.Fatal(err)
	}
	var containers []string
	for _, r := range refs.Results {
		containers = append(containers, r.Container)
	}
	slices.Sort(containers)
	wantLines(t, "checkout users", containers,
		".github/workflows/anchors.yaml::first::actions/checkout", ".github/workflows/anchors.yaml::second::actions/checkout",
		".github/workflows/ci.yml::build::actions/checkout", ".github/workflows/ci.yml::lint::Checkout",
		".github/workflows/ci.yml::test::Checkout")
	text, _ = c.tool("search_code", map[string]any{"path": ".", "kind": "step", "callsName": "actions/checkout"})
	if !strings.Contains(text, "Found 5 symbol(s)") {
		t.Errorf("search_code callsName=actions/checkout:\n%s", text)
	}

	// 6. The jobs calling a reusable workflow.
	wantLines(t, "reusable's callers", yamlEdges(t, c, "get_callers", ".github/workflows/reusable.yml"),
		"called_by .github/workflows/ci.yml::release exact")

	// 7. The triggers and permissions.
	text, _ = c.tool("search_code", map[string]any{"path": ".", "kind": "trigger", "filePattern": "ci.yml"})
	for _, w := range []string{"ci.yml::on:push", "ci.yml::on:pull_request", "ci.yml::on:workflow_dispatch"} {
		if !strings.Contains(text, w) {
			t.Errorf("triggers lack %s:\n%s", w, text)
		}
	}
	text, isErr := c.tool("get_symbol", map[string]any{"path": ".github/workflows/ci.yml", "name": ".github/workflows/ci.yml#/permissions"})
	if isErr || !strings.Contains(text, `"source": "permissions:\n  contents: read"`) {
		t.Errorf("workflow permissions:\n%s", text)
	}
	text, isErr = c.tool("get_symbol", map[string]any{"path": ".github/workflows/ci.yml", "name": ".github/workflows/ci.yml#/jobs/release/if"})
	if isErr || !strings.Contains(text, `if: github.ref == 'refs/heads/main'`) {
		t.Errorf("job condition:\n%s", text)
	}

	// 8. The needs relation, both ways.
	wantLines(t, "lint's callers", yamlEdges(t, c, "get_callers", "ci.yml::lint"),
		"contained_by .github/workflows/ci.yml exact",
		"depended_on_by .github/workflows/ci.yml::build exact", "depended_on_by .github/workflows/ci.yml::test exact")
	wantLines(t, "release's needs", filter(yamlEdges(t, c, "get_callees", "ci.yml::release"), "depends_on"),
		"depends_on .github/workflows/ci.yml::build exact")

	// 9. The steps using a composite action of the repository (Strong: the
	// path is the workspace's), and what a change to it affects.
	wantLines(t, "setup's callers", yamlEdges(t, c, "get_callers", ".github/actions/setup"),
		"called_by .github/workflows/anchors.yaml::first::./.github/actions/setup strong",
		"called_by .github/workflows/anchors.yaml::second::./.github/actions/setup strong",
		"called_by .github/workflows/ci.yml::test::setup strong",
		"called_by .github/workflows/reusable.yml::publish::./.github/actions/setup strong")
	text, _ = c.tool("analyze_change_impact", map[string]any{"path": ".", "symbol": ".github/actions/lint"})
	for _, w := range []string{".github/workflows/ci.yml::lint::./.github/actions/lint", ".github/actions/setup::./.github/actions/lint", ".github/workflows/ci.yml::test"} {
		if !strings.Contains(text, w) {
			t.Errorf("impact of lint lacks %s:\n%s", w, text)
		}
	}

	// The context of a job: its own text, then what it calls and needs.
	text, _ = c.tool("get_context", map[string]any{"path": ".", "symbol": "ci.yml::release", "maxTokens": 4000})
	for _, w := range []string{"Symbol: .github/workflows/ci.yml::release", "Symbol: .github/workflows/reusable.yml", "Symbol: .github/workflows/ci.yml::build"} {
		if !strings.Contains(text, w) {
			t.Errorf("context of release lacks %s:\n%s", w, text)
		}
	}

	// A remote action and a missing job are reported, never guessed.
	text, _ = c.tool("get_callees", map[string]any{"path": ".", "symbol": "ci.yml::release"})
	if !strings.Contains(text, `"unresolved": 1`) || !strings.Contains(text, `"name": "missing"`) {
		t.Errorf("release's missing need:\n%s", text)
	}
	text, _ = c.tool("get_callees", map[string]any{"path": ".", "symbol": "ci.yml::lint::Checkout"})
	if !strings.Contains(text, `"outsideRepository": 1`) {
		t.Errorf("checkout is outside the repository:\n%s", text)
	}

	// Diagnostics: the cycle and the invalid uses of the fixture.
	text, _ = c.tool("get_diagnostics", map[string]any{"path": "."})
	for _, w := range []string{"actions_needs_cycle", "actions_invalid_uses"} {
		if !strings.Contains(text, w) {
			t.Errorf("diagnostics lack %s:\n%s", w, text)
		}
	}

	// The data file: its keys by JSON Pointer, an alias to its anchor.
	text, _ = c.tool("search_code", map[string]any{"path": ".", "kind": "key", "filePattern": "config/"})
	if !strings.Contains(text, "config/app.yaml#/production/pool/size") {
		t.Errorf("data keys:\n%s", text)
	}
	wantLines(t, "development's callees", yamlEdges(t, c, "get_callees", "config/app.yaml#/development"),
		"references config/app.yaml#&defaults exact")
}

func filter(lines []string, prefix string) []string {
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

// The CLI states the same declarations: `ark symbol` on a workflow.
func TestCLIYAML_Symbol(t *testing.T) {
	bin := buildArk(t)
	proj := copyYAMLFixture(t)
	cmd := exec.Command(bin, "symbol", ".github/workflows/ci.yml")
	cmd.Dir = proj
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ark symbol: %v\n%s", err, out)
	}
	for _, w := range []string{"File: .github/workflows/ci.yml (yaml)", "workflow ci.yml", "trigger push", "job lint", "step Checkout", "key /permissions"} {
		if !strings.Contains(string(out), w) {
			t.Errorf("ark symbol lacks %q:\n%s", w, out)
		}
	}
	cmd = exec.Command(bin, "symbol", "--format", "json", "config/app.yaml")
	cmd.Dir = proj
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ark symbol --format json: %v\n%s", err, out)
	}
	var fs struct {
		Language string
		Symbols  []struct{ Name, Kind, Parent string }
	}
	if err := json.Unmarshal(out, &fs); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if fs.Language != "yaml" || len(fs.Symbols) == 0 || fs.Symbols[0].Kind != "document" {
		t.Errorf("ark symbol --format json:\n%s", out)
	}
}

// YAML names are data: a mapping key, a step's name or id, an action's
// input, an event, a docker reference's credentials. A secret of a known
// token format in one never leaves the server through any tool — names are
// masked by the token rules (a path, an ID or a kind never is) — and a key
// that is no one-line name is no declaration, so it cannot forge a line of
// a text result.
func TestMCPYAML_NamesNeverLeakSecrets(t *testing.T) {
	bin := buildArk(t)
	proj := t.TempDir()
	files := map[string]string{
		"cfg.yaml": "tokens:\n  " + maskGitHub + ": admin\n  " + maskAWS + ": ci\n" +
			"? |\n  -----BEGIN RSA PRIVATE KEY-----\n  " + maskPEM + "\n  -----END RSA PRIVATE KEY-----\n: pem\n" +
			"\"zz\\n\\nmain.go:3  function  Forged [exported]\": 1\n",
		".github/workflows/ci.yml": "on:\n  " + maskSlack + ":\njobs:\n  a:\n    runs-on: x\n    steps:\n" +
			"      - name: Login with " + maskGitHub + "\n        run: x\n" +
			"      - id: " + maskAWS + "\n        run: y\n" +
			"      - uses: docker://user:" + maskGitHub + "@ghcr.io/o/i:1\n" +
			"      - uses: owner/" + maskStripe + "@v1\n",
		".github/actions/x/action.yml": "inputs:\n  " + maskGitHub + ":\n    required: true\nruns:\n  using: composite\n  steps:\n    - run: x\n      shell: bash\n",
	}
	for name, content := range files {
		p := filepath.Join(proj, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	secrets := []string{maskGitHub, maskAWS, maskSlack, maskStripe, maskPEM, "BEGIN RSA PRIVATE KEY"}
	calls := []struct {
		tool string
		args map[string]any
	}{
		{"search_code", map[string]any{"path": "."}},
		{"search_code", map[string]any{"path": ".", "format": "json"}},
		{"get_symbols", map[string]any{"path": "cfg.yaml"}},
		{"get_symbols", map[string]any{"path": ".github/workflows/ci.yml"}},
		{"get_symbols", map[string]any{"path": ".github/actions/x/action.yml"}},
		{"find_symbol", map[string]any{"path": ".", "pattern": "."}},
		{"find_references", map[string]any{"path": ".", "name": "owner/" + maskStripe}},
		{"get_callees", map[string]any{"path": ".", "symbol": ".github/workflows/ci.yml::a"}},
		{"get_relations", map[string]any{"path": ".", "symbol": ".github/workflows/ci.yml::a"}},
		{"get_callees", map[string]any{"path": ".", "symbol": ".github/workflows/ci.yml"}},
		{"get_repository_map", map[string]any{"path": ".", "detail": "verbose"}},
		{"get_repository_map", map[string]any{"path": ".", "detail": "verbose", "format": "json"}},
		{"get_diagnostics", map[string]any{"path": "."}},
	}
	for _, call := range calls {
		text, _ := c.tool(call.tool, call.args)
		for _, s := range secrets {
			if strings.Contains(text, s) {
				t.Errorf("%s %v leaks %q:\n%s", call.tool, call.args, s, text)
			}
		}
		if strings.Contains(text, "Forged") {
			t.Errorf("%s %v shows a key that is no name:\n%s", call.tool, call.args, text)
		}
	}
	// Ordinary names are untouched, and a masked one is still selected by
	// its symbolId.
	text, _ := c.tool("search_code", map[string]any{"path": ".", "kind": "step"})
	if !strings.Contains(text, ".github/actions/x::run") || !strings.Contains(text, "ci.yml::a::docker://ghcr.io/o/i:1") {
		t.Errorf("ordinary step name altered:\n%s", text)
	}
}
