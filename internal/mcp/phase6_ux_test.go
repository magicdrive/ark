package mcp

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

// get_relations reads the same index as get_callers / get_callees /
// get_context, in the same three layers: its resolved graph relations are the
// graph edges, its candidate relations include every listed candidate caller
// and callee, its unattributed count is theirs; every caller the context shows
// is a resolved relation.
func TestRelations_AgreeWithCallersCalleesAndContext(t *testing.T) {
	dir := phpSemanticsRepo(t)
	h := NewToolsHandler(dir, nil)
	graphKind := map[string]bool{"call": true, "construction": true, "type_use": true, "import": true, "inheritance": true, "implements": true, "uses_trait": true}
	for _, sym := range []string{"LoginScreenPolicy.showsSsoButton", "LoginController.showLoginForm", "LegacyController.render", "Child.run", "LoginScreenPolicy.unused", "Clinic.showsSsoButton"} {
		var callers, callees graphResult
		var rels relationsOut
		for _, x := range []struct {
			tool string
			out  interface{}
		}{{"get_callers", &callers}, {"get_callees", &callees}, {"get_relations", &rels}} {
			text, isErr := callText(t, h, x.tool, map[string]interface{}{"path": ".", "symbol": sym, "maxResults": float64(500)})
			if isErr {
				t.Fatalf("%s(%s): %s", x.tool, sym, text)
			}
			if err := json.Unmarshal([]byte(text), x.out); err != nil {
				t.Fatal(err)
			}
		}
		set := func(xs []string) []string {
			m := map[string]bool{}
			var out []string
			for _, x := range xs {
				if !m[x] {
					m[x] = true
					out = append(out, x)
				}
			}
			sort.Strings(out)
			return out
		}
		var wantIn, wantOut, gotIn, gotOut, gotCandIn, gotCandOut []string
		for _, e := range callers.Edges {
			wantIn = append(wantIn, e.To+" "+e.Confidence)
		}
		for _, e := range callees.Edges {
			wantOut = append(wantOut, e.To+" "+e.Confidence)
		}
		for _, r := range rels.Relations {
			switch {
			case r.SourceUnidentified:
			case r.Confidence == "candidate" && r.Direction == "called_by":
				gotCandIn = append(gotCandIn, r.Qualified)
			case r.Confidence == "candidate":
				gotCandOut = append(gotCandOut, r.Qualified)
			case !graphKind[r.Kind]:
			case r.Direction == "called_by":
				gotIn = append(gotIn, r.Qualified+" "+r.Confidence)
			default:
				gotOut = append(gotOut, r.Qualified+" "+r.Confidence)
			}
		}
		if !reflect.DeepEqual(set(gotIn), set(wantIn)) || !reflect.DeepEqual(set(gotOut), set(wantOut)) {
			t.Errorf("%s: resolved relations disagree with the graph\n callers %v vs %v\n callees %v vs %v", sym, gotIn, wantIn, gotOut, wantOut)
		}
		for _, x := range []struct {
			listed []candidateEntry
			got    []string
		}{{callers.Candidates, gotCandIn}, {callees.Candidates, gotCandOut}} {
			have := map[string]bool{}
			for _, q := range x.got {
				have[q] = true
			}
			for _, c := range x.listed {
				if !have[c.Symbol] {
					t.Errorf("%s: candidate %s listed by get_callers/get_callees is missing from get_relations", sym, c.Symbol)
				}
			}
		}
		if rels.Unattributed != *callers.Unattributed+*callees.Unattributed {
			t.Errorf("%s: relations unattributed %d, callers+callees %d", sym, rels.Unattributed, *callers.Unattributed+*callees.Unattributed)
		}
		ctxText, _ := callText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": sym})
		resolvedIn := map[string]bool{}
		for _, q := range gotIn {
			resolvedIn[strings.Fields(q)[0]] = true
		}
		for _, block := range strings.Split(ctxText, "### ")[1:] {
			if strings.Contains(block, "Reason: caller") {
				q := strings.TrimPrefix(strings.SplitN(strings.SplitN(block, "\n", 3)[1], "\n", 2)[0], "Symbol: ")
				if !resolvedIn[q] {
					t.Errorf("%s: context caller %s is not a resolved relation", sym, q)
				}
			}
		}
	}
}

func treeOf(t *testing.T, h *ToolsHandler, args map[string]interface{}) boundedTreeEntry {
	t.Helper()
	text, isErr := callText(t, h, "get_directory_tree", args)
	if isErr {
		t.Fatalf("get_directory_tree: %s", text)
	}
	var tree boundedTreeEntry
	if err := json.Unmarshal([]byte(text), &tree); err != nil {
		t.Fatal(err)
	}
	return tree
}

func treePaths(e boundedTreeEntry, prefix string, out *[]string) {
	for _, c := range e.Children {
		p := c.Name
		if prefix != "" {
			p = prefix + "/" + c.Name
		}
		mark := ""
		if c.Truncated {
			mark = "…"
		}
		if c.Type == "directory" {
			*out = append(*out, p+"/"+mark)
		} else {
			*out = append(*out, p)
		}
		treePaths(*c, p, out)
	}
}

func TestDirectoryTree_DepthAndExcludes(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a.txt":                         "x",
		"app/Http/C.php":                "x",
		"app/Http/Deep/D.php":           "x",
		"vendor/acme/V.php":             "x",
		"foo/vendorized/K.php":          "x",
		"storage/framework/views/x.php": "x",
		"storage/logs/l.txt":            "x",
		"empty/.keep":                   "x",
	})
	h := NewToolsHandler(root, nil)
	paths := func(args map[string]interface{}) []string {
		var out []string
		treePaths(treeOf(t, h, args), "", &out)
		return out
	}
	// No limits: exactly the previous output.
	plain, _ := callText(t, h, "get_directory_tree", map[string]interface{}{"path": "."})
	if legacy, err := GenerateDirectoryTreeJSON(root, h.ignoreRule(true), h.accessPolicy().excludesWalked); err != nil || plain != legacy {
		t.Errorf("unbounded tree changed:\n%s\n---\n%s", plain, legacy)
	}
	if got, want := paths(map[string]interface{}{"path": ".", "maxDepth": float64(1)}),
		[]string{"a.txt", "app/…", "empty/…", "foo/…", "storage/…", "vendor/…"}; !reflect.DeepEqual(got, want) {
		t.Errorf("maxDepth 1: %v, want %v", got, want)
	}
	got := paths(map[string]interface{}{"path": ".", "maxDepth": float64(2)})
	for _, w := range []string{"app/Http/…", "app/", "storage/framework/…"} {
		if !contains1(got, w) {
			t.Errorf("maxDepth 2: missing %s in %v", w, got)
		}
	}
	for _, no := range []string{"app/Http/C.php", "app/Http/Deep/…"} {
		if contains1(got, no) {
			t.Errorf("maxDepth 2: %s is too deep: %v", no, got)
		}
	}
	got = paths(map[string]interface{}{"path": ".", "excludeDirs": "vendor, storage/framework"})
	for _, no := range []string{"vendor/", "storage/framework/"} {
		if contains1(got, no) {
			t.Errorf("excludeDirs: %s listed: %v", no, got)
		}
	}
	for _, w := range []string{"foo/vendorized/", "foo/vendorized/K.php", "storage/logs/l.txt"} {
		if !contains1(got, w) {
			t.Errorf("excludeDirs over-matched: %s missing from %v", w, got)
		}
	}
}

func contains1(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// The get_file_content security boundary is unchanged: per-request
// maskSecrets / withLineNumbers do not override the server's options.
func TestFileContent_SecurityOverridesStillIgnored(t *testing.T) {
	root := t.TempDir()
	secret := "AKIA" + "IOSFODNN7EXAMPLE"
	writeTree(t, root, map[string]string{"config.env": "AWS_ACCESS_KEY_ID=" + secret + "\n"})
	_, serveOpt, err := commandline.ServerOptParse("test", []string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandlerWithCache(root, serveOpt.GeneralOption, nil)
	text, isErr := callText(t, h, "get_file_content", map[string]interface{}{"path": "config.env", "maskSecrets": false, "withLineNumbers": true})
	if isErr {
		t.Fatal(text)
	}
	if strings.Contains(text, secret) {
		t.Errorf("maskSecrets=false unmasked a secret: %q", text)
	}
	if strings.HasPrefix(text, "1: ") {
		t.Errorf("withLineNumbers=true overrode the server option: %q", text)
	}
}
