package mcp

import (
	"encoding/json"
	"reflect"
	"testing"
)

// find_symbol returns matches in path order and reports truncated only when a
// match beyond maxResults exists; repository metadata directories are never
// parsed, while ordinary directories (vendor included) still are.
func TestFindSymbol_TruncationAndOrder(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a/one.go":      "package a\n\nfunc Alpha1() {}\n\nfunc Alpha2() {}\n",
		"b/two.go":      "package b\n\nfunc Alpha3() {}\n",
		"vendor/v/v.go": "package v\n\nfunc AlphaV() {}\n",
		".ark/x.go":     "package x\n\nfunc AlphaArk() {}\n",
		".git/y.go":     "package y\n\nfunc AlphaGit() {}\n",
	})
	h := NewToolsHandler(root, nil)

	type match struct {
		Path   string `json:"path"`
		Symbol struct {
			Name string `json:"name"`
		} `json:"symbol"`
	}
	run := func(max int) (names []string, truncated bool) {
		t.Helper()
		text, isErr := callText(t, h, "find_symbol", map[string]interface{}{
			"pattern": "^Alpha", "path": ".", "maxResults": float64(max),
		})
		if isErr {
			t.Fatalf("find_symbol error: %s", text)
		}
		var out struct {
			Matches   []match `json:"matches"`
			Truncated bool    `json:"truncated"`
		}
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("%v: %s", err, text)
		}
		names = []string{}
		for _, m := range out.Matches {
			names = append(names, m.Path+"#"+m.Symbol.Name)
		}
		return names, out.Truncated
	}

	all := []string{"a/one.go#Alpha1", "a/one.go#Alpha2", "b/two.go#Alpha3", "vendor/v/v.go#AlphaV"}
	cases := []struct {
		name          string
		max           int
		want          []string
		wantTruncated bool
	}{
		{"below limit", 5, all, false},
		{"exactly at limit", 4, all, false},
		{"above limit", 3, all[:3], true},
		{"above limit within one file", 1, all[:1], true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated := run(tc.max)
			if !reflect.DeepEqual(got, tc.want) || truncated != tc.wantTruncated {
				t.Errorf("got %v truncated=%v, want %v truncated=%v", got, truncated, tc.want, tc.wantTruncated)
			}
			again, againTruncated := run(tc.max)
			if !reflect.DeepEqual(got, again) || truncated != againTruncated {
				t.Errorf("non-deterministic: %v/%v then %v/%v", got, truncated, again, againTruncated)
			}
		})
	}
}
