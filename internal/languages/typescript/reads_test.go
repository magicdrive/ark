package typescript_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
)

// Value reads (reads.go, hardening Phase C; Reviewer B of Phase F): which
// reads are stated and what they resolve to. Each negative names the rule
// it pins.

func readEdges(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	root := t.TempDir()
	for name, src := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			var es []string
			for _, e := range idx.GetCallees(s.ID) {
				if e.RefKind == "value_reference" {
					to, _ := idx.GetSymbol(e.To)
					es = append(es, fmt.Sprintf("%s:%s %s", to.Location.File, to.Qualified, e.Confidence))
				}
			}
			sort.Strings(es)
			out[string(f)+":"+s.Qualified] = strings.Join(es, ", ")
		}
	}
	return out
}

func TestValueReads(t *testing.T) {
	got := readEdges(t, map[string]string{
		"lib.js": "export const LIMIT = 3\nexport function helper() {}\n",
		// Classic scripts: another file's global is no name the file knows.
		"s1.js": "function g() {}\n",
		"s2.js": "function r() { return g }\n",
		"a.js": strings.Join([]string{
			"import { LIMIT, helper } from './lib.js'",
			"const TOP = 1",
			"export function pos() { return [LIMIT, helper, TOP, { TOP }, cb(helper)] }", // reads
			"export function param(LIMIT) { return LIMIT }",                              // a parameter hides it
			"export function local() { const TOP = 2; return TOP }",                      // a local hides it
			"export function caught() { try {} catch (helper) { return helper } }",       // catch
			"export function nested() { function TOP() {} return TOP }",                  // a nested function
			"export function key() { return { LIMIT: 1 }.LIMIT }",                        // a key, a property
			"export function callee() { return helper() }",                               // a call is no read
			"export function inWith(o) { with (o) { return LIMIT } }",                    // with: may be o.LIMIT
			"function cb() {}",
		}, "\n"),
		"t.ts": strings.Join([]string{
			"type Response = { ok: boolean }",
			"interface Node { id: number }",
			"export function isResp(r: unknown) { return r instanceof Response }", // a type's name read as a value: the global
			"export function isNode(n: unknown) { return n instanceof Node }",
			"export const A = 5",
			"export enum E { A = 1, B = A + 1 }", // A is E.A, not the constant
			"export function useE() { return [E, A] }",
		}, "\n"),
	})
	want := map[string]string{
		"a.js:pos":    "a.js:TOP exact, lib.js:LIMIT exact, lib.js:helper exact",
		"a.js:param":  "",
		"a.js:local":  "",
		"a.js:caught": "",
		"a.js:nested": "",
		"a.js:key":    "",
		"a.js:callee": "",
		"a.js:inWith": "",
		"t.ts:isResp": "",
		"t.ts:isNode": "",
		"t.ts:E":      "",
		"t.ts:useE":   "t.ts:A exact, t.ts:E exact",
		"s2.js:r":     "",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: value reads %q, want %q", k, got[k], w)
		}
	}
}
