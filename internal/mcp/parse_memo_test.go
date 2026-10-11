package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var memoCalls = []benchCall{
	{"find_symbol", map[string]interface{}{"pattern": "."}},
	{"find_symbol", map[string]interface{}{"pattern": "Fn", "kind": "function"}},
	{"find_references", map[string]interface{}{"path": ".", "name": "Helper"}},
	{"find_references", map[string]interface{}{"path": "a/a.go", "name": "Helper"}},
}

// jsonPath is rel as the tools' JSON output spells a path: in the operating
// system's form, JSON-encoded (a Windows separator is escaped).
func jsonPath(rel string) string {
	b, _ := json.Marshal(filepath.FromSlash(rel))
	return string(b)
}

func memoRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, filepath.Join(root, "go.mod"), "module example.com/m\n\ngo 1.22\n")
	put(t, filepath.Join(root, "a/a.go"), "package a\n\nfunc Helper() int { return 1 }\n\nfunc AFn() int { return Helper() }\n")
	// b.go and c.go have the same bytes: one parse, two files reported.
	same := "package b\n\nfunc BFn() int { return Helper() }\n\nfunc Helper() int { return 2 }\n"
	put(t, filepath.Join(root, "b/b.go"), same)
	put(t, filepath.Join(root, "b/c.go"), same)
	put(t, filepath.Join(root, "broken.go"), "package main\n\nfunc (\n")
	return root
}

// The memo changes nothing a client sees: every call answers as on a server
// that never parsed anything before, the first time and when served from the
// memo; files with the same bytes are each reported under their own name.
func TestParseMemo_SameOutput(t *testing.T) {
	root := memoRepo(t)
	h := handlerFor(t, root)
	for _, c := range memoCalls {
		want := callOut(t, handlerFor(t, root), c.name, c.args) // a fresh server: no memo
		for i := 0; i < 2; i++ {
			if got := callOut(t, h, c.name, c.args); got != want {
				t.Errorf("%s %v, call %d:\n%s\nwant (fresh server):\n%s", c.name, c.args, i+1, got, want)
			}
		}
	}
	if m := h.parseMemo(); m.hits == 0 {
		t.Error("the memo was never used")
	}
	out := callOut(t, h, "find_references", map[string]interface{}{"path": ".", "name": "Helper"})
	for _, f := range []string{"a/a.go", "b/b.go", "b/c.go"} {
		if !strings.Contains(out, `"file": `+jsonPath(f)) {
			t.Errorf("find_references does not report %s:\n%s", f, out)
		}
	}
}

// Content is the key: an edited file is parsed again; nothing stale is
// returned.
func TestParseMemo_EditedFile(t *testing.T) {
	root := memoRepo(t)
	h := handlerFor(t, root)
	callOut(t, h, "find_symbol", map[string]interface{}{"pattern": "."})
	callOut(t, h, "find_references", map[string]interface{}{"path": ".", "name": "Helper"})
	put(t, filepath.Join(root, "a/a.go"), "package a\n\nfunc Renamed() int { return 1 }\n\nfunc AFn() int { return Renamed() }\n")
	if sym := callOut(t, h, "find_symbol", map[string]interface{}{"pattern": "^Renamed$"}); !strings.Contains(sym, jsonPath("a/a.go")) {
		t.Errorf("find_symbol does not find the edited file's new symbol:\n%s", sym)
	}
	if sym := callOut(t, h, "find_symbol", map[string]interface{}{"pattern": "^Helper$"}); strings.Contains(sym, jsonPath("a/a.go")) {
		t.Errorf("find_symbol returns a symbol the edited file no longer has:\n%s", sym)
	}
	refs := callOut(t, h, "find_references", map[string]interface{}{"path": "a", "name": "Helper"})
	if strings.Contains(refs, `"file"`) {
		t.Errorf("find_references returns a reference the edited file no longer has:\n%s", refs)
	}
}

// Access is decided per request whatever the memo holds: a file parsed
// while admitted is not reported once a rule excludes it, nor after it is
// replaced by a symlink to a file outside the root or to an excluded file.
func TestParseMemo_AccessStillDecided(t *testing.T) {
	ws, repo := walkRepo(t)
	xgo := "package p\n\n// Public is public.\nfunc Public() string { return Shown() }\n\nfunc Shown() string { return \"PUBLIC\" }\n"
	put(t, filepath.Join(repo, "x.go"), xgo)
	h := handlerFor(t, repo)
	for _, c := range []benchCall{
		{"find_symbol", map[string]interface{}{"pattern": "."}},
		{"find_references", map[string]interface{}{"path": ".", "name": "Shown"}},
	} {
		if out := callOut(t, h, c.name, c.args); !strings.Contains(out, "x.go") {
			t.Fatalf("%s does not see x.go:\n%s", c.name, out)
		}
	}
	put(t, filepath.Join(repo, ".arkignore"), "secret.go\nx.go\n")
	for _, c := range memoCalls[:1] {
		if out := callOut(t, h, c.name, c.args); strings.Contains(out, "x.go") || strings.Contains(out, "Public") {
			t.Errorf("after the rule: %s returns x.go:\n%s", c.name, out)
		}
	}
	if out := callOut(t, h, "find_references", map[string]interface{}{"path": ".", "name": "Shown"}); strings.Contains(out, "x.go") {
		t.Errorf("after the rule: find_references returns x.go:\n%s", out)
	}
	put(t, filepath.Join(repo, ".arkignore"), "secret.go\n")
	if os.Symlink("x", filepath.Join(ws, "probe")) != nil {
		return
	}
	// The secret's bytes are parsed here only if it is read: warm the
	// memo with x.go, then swap x.go for links to the secrets.
	callOut(t, h, "find_symbol", map[string]interface{}{"pattern": "."})
	for _, link := range []string{filepath.Join(ws, "outside/o.go"), "secret.go"} {
		replaceWithLink(t, link, filepath.Join(repo, "x.go"))
		for _, c := range []benchCall{
			{"find_symbol", map[string]interface{}{"pattern": "."}},
			{"find_references", map[string]interface{}{"path": ".", "name": "SecretFn"}},
		} {
			if out := callOut(t, h, c.name, c.args); walkLeaked(c.name, out) {
				t.Errorf("x.go linked to %s: %s returns the secret:\n%s", link, c.name, out)
			}
		}
		os.Remove(filepath.Join(repo, "x.go"))
		put(t, filepath.Join(repo, "x.go"), xgo)
	}
}

// The memo stays within its bound, least recently used first out.
func TestParseMemo_Bound(t *testing.T) {
	m := newParseMemo(100)
	keys := make([]memoKey, 5)
	for i := range keys {
		keys[i] = newMemoKey("k", []byte(fmt.Sprint(i)))
		m.put(keys[i], 30, i)
		if m.size > 100 {
			t.Fatalf("size %d over the bound", m.size)
		}
	}
	if _, ok := m.get(keys[0]); ok {
		t.Error("the oldest entry was kept past the bound")
	}
	if v, ok := m.get(keys[4]); !ok || v.(int) != 4 {
		t.Errorf("the newest entry: %v %v", v, ok)
	}
	m.put(newMemoKey("k", []byte("big")), 101, 0)
	if len(m.items) != 3 {
		t.Errorf("an entry over the bound was stored, or evicted others: %d entries", len(m.items))
	}
}
