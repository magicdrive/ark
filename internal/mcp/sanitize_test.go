package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Synthetic secrets, assembled at run time so that no literal in the
// repository looks like a credential.
var (
	fakeAWSKey = "AKIA" + strings.Repeat("Q", 16)
	fakeGitHub = "ghp_" + strings.Repeat("a", 36)
)

// shape renders a decoded JSON value's structure — keys in order, value
// types, array lengths — without string contents.
func shape(t *testing.T, text string) string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch v := tok.(type) {
		case json.Delim:
			b.WriteString(v.String())
		case string:
			b.WriteString("s")
		case json.Number:
			b.WriteString("n" + v.String())
		case bool:
			b.WriteString("b")
		case nil:
			b.WriteString("0")
		}
		b.WriteByte(' ')
	}
	return b.String()
}

// keysOf lists every object key in order.
func keysOf(t *testing.T, text string) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(text))
	var keys []string
	depthObject := []bool{}
	expectKey := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch v := tok.(type) {
		case json.Delim:
			if v == '{' || v == '[' {
				depthObject = append(depthObject, v == '{')
				expectKey = v == '{'
				continue
			}
			depthObject = depthObject[:len(depthObject)-1]
			expectKey = len(depthObject) > 0 && depthObject[len(depthObject)-1]
			continue
		case string:
			if expectKey {
				keys = append(keys, v)
				expectKey = false
				continue
			}
		}
		if len(depthObject) > 0 && depthObject[len(depthObject)-1] {
			expectKey = true
		}
	}
	return keys
}

// A masked JSON result keeps its structure: same keys in the same order,
// same types, numbers as written, same array lengths. Only string values of
// non-identifier keys change.
func TestSanitize_JSONStructureIsPreserved(t *testing.T) {
	in := `{
  "zeta": 1.50,
  "file": "secrets/api-key-` + fakeAWSKey + `.go",
  "symbolId": "c58fa84d28632b1a",
  "items": [
    {"symbol": "Load", "confidence": "exact", "source": "key := \"` + fakeAWSKey + `\"", "tokens": 12},
    {"symbol": "Other", "source": "no secret", "ok": true, "missing": null}
  ],
  "affected_files": ["a/token-store.go"],
  "extensionStats": {".` + fakeGitHub + `": 1},
  "notes": ["password = \"hunter2-dummy\"", 7],
  "receiver": "\"` + fakeGitHub + `\""
}`
	out := sanitizeText(in, false)
	if !json.Valid([]byte(out)) {
		t.Fatalf("result is not JSON:\n%s", out)
	}
	if shape(t, in) != shape(t, out) {
		t.Errorf("structure changed:\n in %s\nout %s", shape(t, in), shape(t, out))
	}
	if !reflect.DeepEqual(keysOf(t, in), keysOf(t, out)) {
		t.Errorf("keys changed: %v → %v", keysOf(t, in), keysOf(t, out))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	// Identifiers are never altered, even when they look like secrets.
	if got["file"] != "secrets/api-key-"+fakeAWSKey+".go" || got["symbolId"] != "c58fa84d28632b1a" {
		t.Errorf("identifier altered: file=%v symbolId=%v", got["file"], got["symbolId"])
	}
	if files := got["affected_files"].([]any); files[0] != "a/token-store.go" {
		t.Errorf("array of identifiers altered: %v", files)
	}
	// Keys are never masked, even repository-derived ones.
	if _, ok := got["extensionStats"].(map[string]any)["."+fakeGitHub]; !ok {
		t.Errorf("key altered: %v", got["extensionStats"])
	}
	// Repository text is masked.
	items := got["items"].([]any)
	if src := items[0].(map[string]any)["source"].(string); strings.Contains(src, fakeAWSKey) || !strings.Contains(src, maskedValue) {
		t.Errorf("source not masked: %q", src)
	}
	if items[1].(map[string]any)["source"] != "no secret" {
		t.Errorf("clean text altered: %v", items[1])
	}
	if notes := got["notes"].([]any); !strings.Contains(notes[0].(string), maskedValue) {
		t.Errorf("array element not masked: %v", notes)
	}
	if strings.Contains(got["receiver"].(string), fakeGitHub) {
		t.Errorf("receiver expression not masked: %v", got["receiver"])
	}
	// Indentation follows the input's.
	if !strings.Contains(out, "\n  \"items\": [") {
		t.Errorf("indentation not kept:\n%s", out)
	}
}

// Nothing to mask: the result is returned byte for byte.
func TestSanitize_CleanResultIsUnchanged(t *testing.T) {
	for _, in := range []string{
		"{\"query\":\"place\",\"results\":[{\"rank\":1,\"score\":1.0e2}]}",
		"{\n  \"symbol\": \"Place\",\n  \"edges\": [],\n  \"unattributed\": 0\n}\n",
		"[{\"language\":\"go\",\"extensions\":[\".go\"]}]",
		"### orders/orders.go:10-15\nSymbol: Place\n",
	} {
		if out := sanitizeText(in, false); out != in {
			t.Errorf("clean text changed:\n in %q\nout %q", in, out)
		}
	}
}

// Text results are masked as a whole, except those of tools whose text is
// made of identifiers only; text that merely starts like JSON is text.
func TestSanitize_TextResults(t *testing.T) {
	text := "cfg/cfg.go:4:const Key = \"" + fakeAWSKey + "\""
	if out := sanitizeText(text, false); strings.Contains(out, fakeAWSKey) {
		t.Errorf("text not masked: %q", out)
	}
	paths := "api/password-reset/handler.go\napi/token-store.go"
	r := sanitizeToolResult("list_files", &CallToolResult{Content: []Content{{Type: "text", Text: paths}}})
	if r.Content[0].Text != paths {
		t.Errorf("identifier-only text altered: %q", r.Content[0].Text)
	}
	r = sanitizeToolResult("a_future_tool", &CallToolResult{Content: []Content{{Type: "text", Text: text}}})
	if strings.Contains(r.Content[0].Text, fakeAWSKey) {
		t.Errorf("unknown tool's text not masked: %q", r.Content[0].Text)
	}
	notJSON := "[isError] " + text
	if out := sanitizeText(notJSON, false); strings.Contains(out, fakeAWSKey) {
		t.Errorf("non-JSON text starting with '[' not masked: %q", out)
	}
}

// Resource contents and error data pass the same boundary.
func TestSanitize_ResourcesAndErrors(t *testing.T) {
	res := sanitizeResourceResult(&ReadResourceResult{Contents: []ResourceContent{{URI: "file://a.go", Text: "k := \"" + fakeAWSKey + "\""}}})
	if strings.Contains(res.Contents[0].Text, fakeAWSKey) || res.Contents[0].URI != "file://a.go" {
		t.Errorf("resource: %+v", res.Contents[0])
	}
}
