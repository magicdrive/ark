package mcp

import (
	"strings"
	"testing"
)

// Test-file classification (internal/testfiles) is one contract for every
// language: a caller in a test file is left out of the Context unless
// includeTests is set, is still a caller in the graph and its completeness,
// and is classified as a test (not dropped) by impact analysis.
func TestTestCallers_SameContractAcrossLanguages(t *testing.T) {
	cases := []struct {
		lang, target         string
		prodFile, testFile   string
		prodCaller, testCall string
		files                map[string]string
	}{
		{
			lang: "php", target: `App\Policy.allowed`,
			prodFile: "app/Controller.php", testFile: "tests/Feature/PolicyTest.php",
			prodCaller: `App\Controller.run`, testCall: `Tests\Feature\PolicyTest.testAllowed`,
			files: map[string]string{
				"app/Policy.php":               "<?php\nnamespace App;\nclass Policy { public function allowed() { return true; } }\n",
				"app/Controller.php":           "<?php\nnamespace App;\nclass Controller { public function run(Policy $p) { return $p->allowed(); } }\n",
				"tests/Feature/PolicyTest.php": "<?php\nnamespace Tests\\Feature;\nuse App\\Policy;\nclass PolicyTest { public function testAllowed(Policy $p) { return $p->allowed(); } }\n",
			},
		},
		{
			lang: "go", target: "Allowed",
			prodFile: "svc/controller.go", testFile: "svc/policy_test.go",
			prodCaller: "Run", testCall: "TestAllowed",
			files: map[string]string{
				"go.mod":             "module example.com/m\n\ngo 1.21\n",
				"svc/policy.go":      "package svc\n\nfunc Allowed() bool { return true }\n",
				"svc/controller.go":  "package svc\n\nfunc Run() bool { return Allowed() }\n",
				"svc/policy_test.go": "package svc\n\nimport \"testing\"\n\nfunc TestAllowed(t *testing.T) { Allowed() }\n",
			},
		},
		{
			lang: "typescript", target: "allowed",
			prodFile: "src/controller.ts", testFile: "src/policy.test.ts",
			prodCaller: "run", testCall: "testAllowed",
			files: map[string]string{
				"src/policy.ts":      "export function allowed() { return true; }\n",
				"src/controller.ts":  "import { allowed } from \"./policy\";\nexport function run() { return allowed(); }\n",
				"src/policy.test.ts": "import { allowed } from \"./policy\";\nexport function testAllowed() { return allowed(); }\n",
			},
		},
		{
			lang: "tsx", target: "allowed",
			prodFile: "src/App.tsx", testFile: "src/App.spec.tsx",
			prodCaller: "App", testCall: "testApp",
			files: map[string]string{
				"src/Policy.tsx":   "export function allowed() { return true; }\n",
				"src/App.tsx":      "import { allowed } from \"./Policy\";\nexport function App() { return <div>{allowed()}</div>; }\n",
				"src/App.spec.tsx": "import { allowed } from \"./Policy\";\nexport function testApp() { return allowed(); }\n",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			root := t.TempDir()
			writeTree(t, root, tc.files)
			h := NewToolsHandler(root, nil)

			// The graph and its completeness keep both callers.
			callers, text, isErr := callGraphTool(t, root, "get_callers", map[string]interface{}{"path": ".", "symbol": tc.target})
			if isErr {
				t.Fatalf("get_callers: %s", text)
			}
			got := map[string]bool{}
			for _, e := range callers.Edges {
				got[e.To] = true
			}
			if !got[tc.prodCaller] || !got[tc.testCall] || *callers.Unattributed != 0 {
				t.Fatalf("graph callers %v (unattributed %d), want %s and %s", got, *callers.Unattributed, tc.prodCaller, tc.testCall)
			}

			// Context: production only by default, both with includeTests.
			for _, includeTests := range []bool{false, true} {
				ctx, isErr := callText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": tc.target, "includeTests": includeTests})
				if isErr {
					t.Fatalf("get_context: %s", ctx)
				}
				if !strings.Contains(ctx, "### "+tc.prodFile) {
					t.Errorf("includeTests=%v: production caller %s missing:\n%s", includeTests, tc.prodFile, ctx)
				}
				if has := strings.Contains(ctx, "### "+tc.testFile); has != includeTests {
					t.Errorf("includeTests=%v: test caller %s present=%v:\n%s", includeTests, tc.testFile, has, ctx)
				}
			}

			// Impact: the test caller is a test, not dropped; the production
			// caller is a direct dependent.
			imp, _ := callText(t, h, "analyze_change_impact", map[string]interface{}{"path": ".", "symbol": tc.target})
			direct, tests := impactSection(imp, "Direct dependents (callers):"), impactSection(imp, "Tests:")
			if !strings.Contains(direct, tc.prodFile) || strings.Contains(direct, tc.testFile) {
				t.Errorf("impact direct dependents:\n%s", direct)
			}
			if !strings.Contains(tests, tc.testFile) {
				t.Errorf("impact tests do not list %s:\n%s", tc.testFile, imp)
			}
		})
	}
}

// impactSection returns the lines under header in analyze_change_impact text.
func impactSection(text, header string) string {
	i := strings.Index(text, header)
	if i < 0 {
		return ""
	}
	rest := text[i+len(header):]
	if j := strings.Index(rest, "\n\n"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}
