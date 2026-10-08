package mcp

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/syntax"
)

// Terraform end to end through the MCP surfaces, on the multi-module fixture
// of the Terraform provider (internal/languages/terraform/testdata/repo).

func terraformFixture(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "languages", "terraform", "testdata", "repo")
}

func tfCall(t *testing.T, tool string, args map[string]interface{}) (string, bool) {
	t.Helper()
	res, text := callTool(t, terraformFixture(t), tool, args)
	return text, res.IsError
}

func tfCallees(t *testing.T, symbol, filePattern string) callersResult {
	t.Helper()
	text, isErr := tfCall(t, "get_callees", map[string]interface{}{"symbol": symbol, "filePattern": filePattern})
	if isErr {
		t.Fatalf("get_callees %s: %s", symbol, text)
	}
	var out callersResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	return out
}

func TestOneRegistrationInvariant_Terraform(t *testing.T) {
	if syntax.DetectLanguage("main.tf") != "terraform" || syntax.DetectLanguage("prod.auto.tfvars") != "terraform" {
		t.Error("syntax detection does not route .tf / .tfvars to terraform")
	}
	if syntax.DetectLanguage("build.pkr.hcl") == "terraform" {
		t.Error(".hcl must not carry Terraform semantics")
	}
	found := false
	for _, p := range defaultProviders() {
		found = found || p.Language() == "terraform"
	}
	if !found {
		t.Error("defaultProviders() misses terraform")
	}
	if _, ok := refProviderRegistry[".tf"]; !ok {
		t.Error("find_references registry misses .tf")
	}
	if lvl := languages.Registry().SupportLevelFor("terraform"); lvl.String() != "graph" {
		t.Errorf("terraform support level %s", lvl)
	}
	h := &ToolsHandler{}
	res, err := h.getLanguageSupport(nil)
	if err != nil {
		t.Fatal(err)
	}
	var entries []languageSupportEntry
	if err := json.Unmarshal([]byte(res.Content[0].Text), &entries); err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, e := range entries {
		if e.Language == "terraform" {
			ok = e.Level == "graph" && containsExt(e.Extensions, ".tf") && containsExt(e.Extensions, ".tfvars")
		}
	}
	if !ok {
		t.Errorf("get_language_support does not report terraform at graph with .tf/.tfvars: %s", res.Content[0].Text)
	}
}

// The same address in two modules is two symbols: a single-target tool
// never picks one.
func TestMCP_Terraform_SameAddressIsAmbiguousAcrossModules(t *testing.T) {
	text, isErr := tfCall(t, "get_callers", map[string]interface{}{"symbol": "aws_vpc.main"})
	if !isErr || !strings.Contains(text, "Ambiguous") ||
		!strings.Contains(text, "modules/network/aws_vpc.main") || !strings.Contains(text, "modules/database/aws_vpc.main") {
		t.Fatalf("want an ambiguity listing both modules:\n%s", text)
	}
	for _, args := range []map[string]interface{}{
		{"symbol": "aws_vpc.main", "filePattern": "modules/network"},
		{"symbol": "modules/network/aws_vpc.main"},
	} {
		text, isErr := tfCall(t, "get_callers", args)
		if isErr {
			t.Fatalf("%v: %s", args, text)
		}
		var out callersResult
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range out.Edges {
			got = append(got, e.To+" "+e.Kind)
		}
		want := []string{"modules/network/aws_subnet.private referenced_by", "modules/network/output.vpc_id referenced_by"}
		if strings.Join(got, ",") != strings.Join(want, ",") || out.Unattributed != 0 {
			t.Errorf("%v: callers %v unattributed %d, want %v", args, got, out.Unattributed, want)
		}
	}
}

// Dependencies are reported as what they are — never as calls.
func TestMCP_Terraform_DependencyKinds(t *testing.T) {
	out := tfCallees(t, "aws_instance.web", "")
	kinds := map[string]string{}
	for _, e := range out.Edges {
		kinds[e.To] = e.Kind
		if e.Kind == "calls" || e.Confidence != "exact" {
			t.Errorf("edge %+v", e)
		}
	}
	if kinds["aws_security_group.web"] != "depends_on" || kinds["modules/network/output.subnet_id"] != "references" {
		t.Errorf("kinds %v", kinds)
	}
	if *out.Unresolved != 0 || *out.OutsideRepository != 0 || out.Unattributed != 0 {
		t.Errorf("aws_instance.web: unresolved=%d outside=%d unattributed=%d", *out.Unresolved, *out.OutsideRepository, out.Unattributed)
	}

	text, _ := tfCall(t, "get_callers", map[string]interface{}{"symbol": "aws_security_group.web"})
	if !strings.Contains(text, `"kind": "depended_on_by"`) || strings.Contains(text, "called_by") {
		t.Errorf("reverse explicit dependency is not depended_on_by:\n%s", text)
	}
	text, _ = tfCall(t, "get_relations", map[string]interface{}{"symbol": "module.network"})
	if !strings.Contains(text, `"kind": "value_reference"`) {
		t.Errorf("get_relations does not carry the reference kind:\n%s", text)
	}
}

func TestMCP_Terraform_UnresolvedAndOutside(t *testing.T) {
	cases := []struct {
		symbol, reason string
		unresolved     int
		outside        int
	}{
		{"output.remote_vpc", "outside_repository", 0, 1},
		{"output.missing", "unresolved", 1, 0},
		{"output.undeclared", "unresolved", 1, 0},
	}
	for _, c := range cases {
		out := tfCallees(t, c.symbol, "")
		if *out.Unresolved != c.unresolved || *out.OutsideRepository != c.outside || len(out.UnresolvedReferences) != 1 || out.UnresolvedReferences[0].Reason != c.reason {
			t.Errorf("%s: unresolved=%d outside=%d refs=%+v", c.symbol, *out.Unresolved, *out.OutsideRepository, out.UnresolvedReferences)
		}
	}
}

func TestMCP_Terraform_ContextImpactRepomap(t *testing.T) {
	text, isErr := tfCall(t, "get_context", map[string]interface{}{"symbol": "modules/network/output.subnet_id"})
	if isErr {
		t.Fatal(text)
	}
	for _, want := range []string{"Reason: references", "modules/network/local.subnet_id", "Reason: caller", "module.database"} {
		if !strings.Contains(text, want) {
			t.Errorf("get_context misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "modules/database/aws_vpc.main") {
		t.Errorf("get_context pulled in another module's same-name resource:\n%s", text)
	}

	text, isErr = tfCall(t, "analyze_change_impact", map[string]interface{}{"symbol": "modules/network/aws_vpc.main"})
	if isErr || !strings.Contains(text, "modules/network/output.vpc_id") || strings.Contains(text, "modules/database/aws_db_instance.db") {
		t.Errorf("analyze_change_impact:\n%s", text)
	}

	text, isErr = tfCall(t, "get_repository_map", map[string]interface{}{})
	if isErr || !strings.Contains(text, "modules/network") || !strings.Contains(text, "terraform") {
		t.Errorf("get_repository_map:\n%s", text)
	}
}

func TestMCP_Terraform_FindTools(t *testing.T) {
	text, _ := tfCall(t, "find_symbol", map[string]interface{}{"pattern": "aws_vpc.main"})
	if !strings.Contains(text, "modules/network/main.tf") || !strings.Contains(text, "modules/database/main.tf") || !strings.Contains(text, `"kind": "resource"`) {
		t.Errorf("find_symbol:\n%s", text)
	}
	text, _ = tfCall(t, "find_references", map[string]interface{}{"name": "aws_vpc.main"})
	if !strings.Contains(text, `"count": 4`) {
		t.Errorf("find_references (syntactic, every module):\n%s", text)
	}
	text, _ = tfCall(t, "search_code", map[string]interface{}{"kind": "output"})
	if !strings.Contains(text, "Found 9 symbol(s)") {
		t.Errorf("search_code kind=output:\n%s", text)
	}
}

// analyze_change_impact reports dependents more than one hop upstream, with
// their hop counts, in both output formats.
func TestMCP_Terraform_TransitiveImpact(t *testing.T) {
	text, isErr := tfCall(t, "analyze_change_impact", map[string]interface{}{
		"symbol": "modules/network/aws_subnet.private", "format": "json", "maxDepth": float64(4),
	})
	if isErr {
		t.Fatal(text)
	}
	var out struct {
		Entries []struct {
			Symbol   string `json:"symbol"`
			Category string `json:"category"`
			Distance int    `json:"distance"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	// The entry schema is unchanged: exactly these keys.
	var raw struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil || len(raw.Entries) == 0 {
		t.Fatalf("parse: %v", err)
	}
	for _, e := range raw.Entries {
		var keys []string
		for k := range e {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if strings.Join(keys, ",") != "category,confidence,distance,file,line,symbol" {
			t.Errorf("impact entry keys %v", keys)
		}
	}
	got := map[string]string{}
	for _, e := range out.Entries {
		got[e.Symbol] = e.Category + "@" + string(rune('0'+e.Distance))
	}
	for sym, want := range map[string]string{
		"modules/network/local.subnet_id":  "direct_dependent@1",
		"modules/network/output.subnet_id": "transitive_dependent@2",
		"aws_instance.web":                 "transitive_dependent@3",
		"module.database":                  "transitive_dependent@3",
		"local.web_ids":                    "transitive_dependent@4",
	} {
		if got[sym] != want {
			t.Errorf("%s: %q, want %q (all: %v)", sym, got[sym], want, got)
		}
	}
	text, _ = tfCall(t, "analyze_change_impact", map[string]interface{}{"symbol": "modules/network/aws_vpc.main"})
	i := strings.Index(text, "Transitive dependents:")
	if i < 0 || !strings.Contains(text[i:], "output.network_id") || strings.Contains(text[i:], "(none)\n\nPossible") {
		t.Errorf("text format lacks transitive dependents:\n%s", text)
	}
}
