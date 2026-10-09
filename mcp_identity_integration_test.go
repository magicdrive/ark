package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Declaration identity end to end, through the real binary: two Go init
// functions in one file stay two symbols in every tool.
func TestMCPIdentity_NamesakeDeclarations(t *testing.T) {
	bin := buildArk(t)
	proj := t.TempDir()
	for p, c := range map[string]string{
		"go.mod":  "module example.com/x\n\ngo 1.22\n",
		"boot.go": "package example\n\nfunc init() {\n\tregisterA()\n}\n\nfunc init() {\n\tregisterB()\n}\n\nfunc registerA() {}\nfunc registerB() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(proj, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()

	// search_context: two candidates, two IDs, each with its own context.
	text, isErr := c.tool("search_context", map[string]any{"query": "init", "contextLimit": 2})
	if isErr {
		t.Fatalf("search_context: %s", text)
	}
	r := decodeSearch(t, text)
	if len(r.Results) != 2 || r.Results[0].Symbol.ID == r.Results[1].Symbol.ID {
		t.Fatalf("want two init candidates with distinct IDs: %s", text)
	}
	for i, want := range []string{"registerA", "registerB"} {
		x := r.Results[i]
		other := map[string]string{"registerA": "registerB", "registerB": "registerA"}[want]
		var srcs []string
		for _, it := range x.Context.Items {
			srcs = append(srcs, it.Source)
		}
		joined := strings.Join(srcs, "\n")
		if x.Context.Status != "included" || !strings.Contains(joined, want+"()") || strings.Contains(joined, other+"()") {
			t.Errorf("init #%d context mixes declarations: %+v", i+1, x.Context)
		}
	}

	// get_context by name: ambiguous, listing each declaration's symbolId.
	text, isErr = c.tool("get_context", map[string]any{"path": ".", "symbol": "init"})
	ids := regexp.MustCompile(`boot\.go:(\d+)  symbolId=([0-9a-f]+)`).FindAllStringSubmatch(text, -1)
	if !isErr || len(ids) != 2 {
		t.Fatalf("get_context(init) must list two candidates: %s", text)
	}
	// get_context by symbolId: exactly that declaration.
	for _, m := range ids {
		line, id := m[1], m[2]
		text, isErr = c.tool("get_context", map[string]any{"path": ".", "symbol": "init", "symbolId": id})
		want := map[string]string{"3": "registerA", "7": "registerB"}[line]
		if isErr || !strings.Contains(text, "### boot.go:"+line+"-") || !strings.Contains(text, "Symbol: "+want+"\n") {
			t.Errorf("get_context init@%s: %s", line, text)
		}
	}
	// get_callers: each register function has its own init as caller.
	for callee, line := range map[string]string{"registerA": "3", "registerB": "7"} {
		text, _ = c.tool("get_callers", map[string]any{"path": ".", "symbol": callee})
		var out struct {
			Edges []struct{ To string } `json:"edges"`
		}
		if err := json.Unmarshal([]byte(text), &out); err != nil || len(out.Edges) != 1 || out.Edges[0].To != "init" {
			t.Errorf("get_callers %s: %s", callee, text)
		}
		ctx, _ := c.tool("get_context", map[string]any{"path": ".", "symbol": callee})
		if !strings.Contains(ctx, "### boot.go:"+line+"-") {
			t.Errorf("get_context %s does not show its caller init@%s:\n%s", callee, line, ctx)
		}
	}
}

// Symbol.Parent through the real binary: search_code's JSON exposes each
// symbol's Parent, which must be the ID of the enclosing declaration in the
// same answer — never a fabricated one. get_symbol's "parent" stays the
// provider's qualified name.
func TestMCPIdentity_ParentRoundTrip(t *testing.T) {
	bin := buildArk(t)
	proj := t.TempDir()
	for p, c := range map[string]string{
		"svc.ts":  "export class UserService {\n  getUser() {\n    return this.loadUser();\n  }\n\n  loadUser() {\n    return null;\n  }\n}\n",
		"a.php":   "<?php\nnamespace App;\nclass Cmd\n{\n    protected $cache;\n    public function cache() {}\n}\n",
		"go.mod":  "module example.com/x\n\ngo 1.22\n",
		"boot.go": "package example\n\nfunc init() {}\n\nfunc init() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(proj, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()

	type sym struct {
		ID, Name, Qualified, Kind, Parent, ParentQualified string
	}
	text, isErr := c.tool("search_code", map[string]any{"path": ".", "format": "json"})
	if isErr {
		t.Fatalf("search_code: %s", text)
	}
	var out struct {
		Matches []struct{ Symbol *sym } `json:"matches"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	byID := map[string]sym{}
	for _, m := range out.Matches {
		if m.Symbol != nil {
			byID[m.Symbol.ID] = *m.Symbol
		}
	}
	parents := map[string]string{}
	for _, s := range byID {
		if s.Parent == "" {
			continue
		}
		p, ok := byID[s.Parent]
		if !ok || p.Qualified != s.ParentQualified || p.ID == s.ID {
			t.Errorf("%s: parent %s is not the declaration %s", s.Qualified, s.Parent, s.ParentQualified)
		}
		parents[s.Qualified+"/"+s.Kind] = p.Qualified + "/" + p.Kind
	}
	for child, parent := range map[string]string{
		"UserService.getUser/method": "UserService/class", "UserService.loadUser/method": "UserService/class",
		`App\Cmd.cache/method`: `App\Cmd/class`, `App\Cmd.cache/property`: `App\Cmd/class`,
	} {
		if parents[child] != parent {
			t.Errorf("%s: parent %q, want %q", child, parents[child], parent)
		}
	}
	for _, s := range byID {
		if s.Name == "init" && s.Parent != "" {
			t.Errorf("init has parent %s", s.Parent)
		}
	}
	// Same answer on a repeated call.
	if again, _ := c.tool("search_code", map[string]any{"path": ".", "format": "json"}); again != text {
		t.Error("search_code output is not deterministic")
	}
	// get_symbol keeps reporting the provider's parent name.
	gs, isErr := c.tool("get_symbol", map[string]any{"path": "svc.ts", "name": "loadUser", "includeSource": false})
	if isErr || !strings.Contains(gs, `"parent": "UserService"`) {
		t.Errorf("get_symbol: %s", gs)
	}
}
