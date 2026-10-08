package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

func diagnosticsOf(t *testing.T, h *ToolsHandler, args map[string]interface{}) diagnosticsResult {
	t.Helper()
	args["path"] = "."
	res, err := h.CallTool("get_diagnostics", args)
	if err != nil || res.IsError {
		t.Fatalf("get_diagnostics: %v %+v", err, res)
	}
	var out diagnosticsResult
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, res.Content[0].Text)
	}
	return out
}

func toolText(t *testing.T, h *ToolsHandler, tool string, args map[string]interface{}) string {
	t.Helper()
	args["path"] = "."
	res, err := h.CallTool(tool, args)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return res.Content[0].Text
}

// A valid repository: no diagnostics, and the graph tools' output carries no
// diagnostics field at all — byte-compatible with clients that predate it,
// and making no completeness claim.
func TestDiagnostics_ValidRepository(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go": "package a\n\nfunc A() { B() }\nfunc B() {}\n",
	})
	h := &ToolsHandler{rootDir: dir}
	out := diagnosticsOf(t, h, map[string]interface{}{})
	if out.Total != 0 || len(out.Diagnostics) != 0 || out.Truncated || out.Summary.FilesIndexed != 1 || out.Summary.Errors != 0 {
		t.Errorf("%+v", out)
	}
	for _, tool := range []string{"get_callers", "get_callees", "get_relations"} {
		if text := toolText(t, h, tool, map[string]interface{}{"symbol": "B"}); strings.Contains(text, "indexDiagnostics") {
			t.Errorf("%s reports diagnostics for a clean index:\n%s", tool, text)
		}
	}
	if text := toolText(t, h, "get_context", map[string]interface{}{"symbol": "B"}); strings.Contains(text, "index diagnostics") {
		t.Errorf("get_context: %s", text)
	}
	if text := toolText(t, h, "analyze_change_impact", map[string]interface{}{"symbol": "B", "format": "json"}); strings.Contains(text, "index_diagnostics") {
		t.Errorf("impact: %s", text)
	}
	// The tool never claims that zero diagnostics means a complete analysis.
	for _, tl := range DiagnosticsToolDefinitions() {
		if !strings.Contains(tl.Description, "does NOT mean the analysis is complete") {
			t.Errorf("description overclaims: %s", tl.Description)
		}
	}
}

// Broken files of every language are reported with file, position, code and
// language; valid files beside them keep their results, and the graph tools
// say the index has diagnostics.
func TestDiagnostics_MixedRepositoryAcrossLanguages(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"good.go":         "package a\n\nfunc Good() { Helper() }\nfunc Helper() {}\n",
		"bad.go":          "package a\n\nfunc Bad( {\n",
		"bad.php":         "<?php\nclass Bad { public function m( {\n",
		"bad.ts":          "export class B {\n  m( {\n}\n",
		"bad.tf":          "resource \"aws_vpc\" \"x\" {\n  cidr = var.y +\n}\n",
		"bad_string.tf":   "output \"o\" {\n  value = \"unterminated\n}\n",
		"bad_utf8.py":     "def f(:\n    return '\xff\xfe'\n",
		"truncated.js":    "export function f() { return [1, 2,",
		"vars.tf.json":    `{"variable": {"x": {}}}`,
		"notes/README.md": "not code",
	})
	h := &ToolsHandler{rootDir: dir}
	out := diagnosticsOf(t, h, map[string]interface{}{})
	files := map[string]string{}
	for _, d := range out.Diagnostics {
		if d.Code != "parse_error" || d.Severity != "error" || d.Line == 0 {
			t.Errorf("entry %+v", d)
		}
		files[d.File] = d.Language
	}
	for file, lang := range map[string]string{
		"bad.go": "go", "bad.php": "php", "bad.ts": "typescript", "bad.tf": "terraform",
		"bad_string.tf": "terraform", "bad_utf8.py": "python", "truncated.js": "javascript",
	} {
		if files[file] != lang {
			t.Errorf("%s: language %q in %v", file, files[file], files)
		}
	}
	if _, ok := files["good.go"]; ok {
		t.Error("a valid file was reported")
	}
	// Unsupported formats are not examined: no diagnostic, not indexed.
	if _, ok := files["vars.tf.json"]; ok {
		t.Error(".tf.json is not examined, so it cannot have a diagnostic")
	}
	if out.Summary.FilesIndexed != 8 || out.Summary.FilesWithDiagnostics != 7 || out.Summary.ByCode["parse_error"] != out.Summary.Errors {
		t.Errorf("summary %+v", out.Summary)
	}
	// Valid code keeps its graph; the result says the index has diagnostics.
	text := toolText(t, h, "get_callers", map[string]interface{}{"symbol": "Helper"})
	var callers callersResult
	if err := json.Unmarshal([]byte(text), &callers); err != nil {
		t.Fatal(err)
	}
	if len(callers.Edges) != 1 || callers.IndexDiagnostics == nil || callers.IndexDiagnostics.Files != 7 {
		t.Errorf("get_callers: %s", text)
	}
	if text := toolText(t, h, "get_context", map[string]interface{}{"symbol": "Helper"}); !strings.Contains(text, "--- index diagnostics: 7 file(s)") {
		t.Errorf("get_context: %s", text)
	}
	if text := toolText(t, h, "analyze_change_impact", map[string]interface{}{"symbol": "Helper"}); !strings.Contains(text, "Index diagnostics: 7 file(s)") {
		t.Errorf("impact text: %s", text)
	}
	if text := toolText(t, h, "analyze_change_impact", map[string]interface{}{"symbol": "Helper", "format": "json"}); !strings.Contains(text, `"index_diagnostics"`) {
		t.Errorf("impact json: %s", text)
	}
}

// Many diagnostics: filters, a stable order, pages that partition the
// listing, and truncation that is always stated.
func TestDiagnostics_LargeAndPaged(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{}
	for i := 0; i < 240; i++ {
		files[fmt.Sprintf("m%03d/broken.tf", i)] = "resource \"a\" \"b\" {\n  x = [\n}\nresource \"a\" {}\nresource \"a\" \"c\" {}\nresource \"a\" \"c\" {}\n"
	}
	writeTree(t, dir, files)
	h := &ToolsHandler{rootDir: dir}
	all := diagnosticsOf(t, h, map[string]interface{}{"maxResults": float64(100000)})
	if all.Truncated || len(all.Diagnostics) != all.Total || all.Total < 240 {
		t.Fatalf("total %d returned %d truncated %t", all.Total, len(all.Diagnostics), all.Truncated)
	}
	var paged []diagnosticEntry
	for offset := 0; ; offset += 37 {
		page := diagnosticsOf(t, h, map[string]interface{}{"maxResults": float64(37), "offset": float64(offset)})
		if page.Total != all.Total || page.Summary.Errors+page.Summary.Warnings != all.Total {
			t.Fatalf("offset %d: total %d, summary %+v", offset, page.Total, page.Summary)
		}
		paged = append(paged, page.Diagnostics...)
		if page.Truncated != (offset+len(page.Diagnostics) < all.Total) {
			t.Fatalf("offset %d: truncated %t", offset, page.Truncated)
		}
		if !page.Truncated {
			break
		}
	}
	if !slices.Equal(paged, all.Diagnostics) {
		t.Error("pages do not partition the full listing in order")
	}
	def := diagnosticsOf(t, h, map[string]interface{}{})
	if len(def.Diagnostics) != 100 || !def.Truncated {
		t.Errorf("default limit: %d truncated %t", len(def.Diagnostics), def.Truncated)
	}
	past := diagnosticsOf(t, h, map[string]interface{}{"offset": float64(all.Total + 5)})
	if len(past.Diagnostics) != 0 || past.Truncated || past.Total != all.Total {
		t.Errorf("past the end: %+v", past)
	}
	warn := diagnosticsOf(t, h, map[string]interface{}{"severity": "warning", "maxResults": float64(100000)})
	for _, d := range warn.Diagnostics {
		if d.Severity != "warning" {
			t.Fatalf("severity filter: %+v", d)
		}
	}
	one := diagnosticsOf(t, h, map[string]interface{}{"filePattern": "m007/"})
	if one.Total == 0 || one.Summary.Errors+one.Summary.Warnings != all.Total {
		t.Errorf("file filter: %+v", one)
	}
	for _, d := range one.Diagnostics {
		if !strings.HasPrefix(d.File, "m007/") {
			t.Fatalf("file filter: %+v", d)
		}
	}
	// Stable across handlers (a fresh index each).
	again := diagnosticsOf(t, &ToolsHandler{rootDir: dir}, map[string]interface{}{"maxResults": float64(100000)})
	if !slices.Equal(again.Diagnostics, all.Diagnostics) {
		t.Error("non-deterministic listing")
	}
	sorted := slices.IsSortedFunc(all.Diagnostics, func(a, b diagnosticEntry) int { return strings.Compare(a.File, b.File) })
	if !sorted {
		t.Error("not ordered by file")
	}
}

// A diagnostic disappears when the file is fixed: the served index is
// rebuilt from content, never reused stale.
func TestDiagnostics_FollowEdits(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": "package a\nfunc A( {\n"})
	h := NewToolsHandlerWithCache(dir, nil, nil)
	if out := diagnosticsOf(t, h, map[string]interface{}{}); out.Total == 0 {
		t.Fatal("no diagnostic for a broken file")
	}
	writeTree(t, dir, map[string]string{"a.go": "package a\nfunc A() {}\n"})
	if out := diagnosticsOf(t, h, map[string]interface{}{}); out.Total != 0 {
		t.Errorf("stale diagnostics after the fix: %+v", out)
	}
}

// Tool errors are tool errors: an invalid argument or path is an isError
// result, never a diagnostic listing.
func TestDiagnostics_ToolErrorsAreNotDiagnostics(t *testing.T) {
	h := &ToolsHandler{rootDir: t.TempDir()}
	for _, args := range []map[string]interface{}{
		{"path": ".", "severity": "fatal"},
		{"path": "../../outside"},
	} {
		res, err := h.CallTool("get_diagnostics", args)
		if err != nil {
			continue // a protocol-level error is not a diagnostic either
		}
		if !res.IsError || strings.Contains(res.Content[0].Text, `"diagnostics"`) {
			t.Errorf("%v: %+v", args, res)
		}
	}
}

func TestDiagnostics_MessagesAreBounded(t *testing.T) {
	long := strings.Repeat("é", maxDiagnosticMessage+50)
	got := boundMessage(long + "\xff")
	if n := len([]rune(got)); n != maxDiagnosticMessage+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("bounded to %d runes", n)
	}
	if boundMessage("ok") != "ok" {
		t.Error("short message changed")
	}
}

func TestDiagnostics_SortOrder(t *testing.T) {
	mk := func(file string, line, col uint32, sev, code, msg string) language.Diagnostic {
		return language.Diagnostic{Severity: language.DiagnosticSeverity(sev), Code: code, Message: msg,
			Location: source.Location{File: source.FileID(file), Range: source.Range{Start: source.Position{Line: line, Column: col}}}}
	}
	want := []language.Diagnostic{
		mk("a.go", 0, 0, "warning", "read_error", "x"),
		mk("a.go", 2, 1, "error", "parse_error", "x"),
		mk("a.go", 2, 5, "error", "b", "x"),
		mk("a.go", 2, 5, "error", "c", "x"),
		mk("a.go", 2, 5, "warning", "a", "x"),
		mk("b.go", 1, 1, "error", "", "m"),
		mk("b.go", 1, 1, "error", "", "n"),
	}
	got := slices.Clone(want)
	slices.Reverse(got)
	sortDiagnostics(got)
	if !slices.EqualFunc(got, want, func(a, b language.Diagnostic) bool { return a == b }) {
		t.Errorf("order:\n%v\nwant\n%v", got, want)
	}
}

// A file the index cannot read: skipped, reported with its relative path
// and no position (unknown is omitted, never line 0) and no OS path.
func TestDiagnostics_UnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads anything")
	}
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"ok.go": "package a\n"})
	p := filepath.Join(dir, "locked.go")
	if err := os.WriteFile(p, []byte("package a\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	out := diagnosticsOf(t, &ToolsHandler{rootDir: dir}, map[string]interface{}{})
	if out.Total != 1 || out.Summary.FilesSkipped != 1 || out.Summary.FilesIndexed != 1 {
		t.Fatalf("%+v", out)
	}
	d := out.Diagnostics[0]
	if d.File != "locked.go" || d.Code != "read_error" || d.Line != 0 || d.Language != "go" || strings.Contains(d.Message, dir) {
		t.Errorf("%+v", d)
	}
}

// The index walks directories (a/b.go before a.go); the listing is ordered
// by path, then position, whatever order the diagnostics were produced in.
func TestDiagnostics_ListingIsInPathOrder(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a/b.go": "package b\nfunc B( {\n", "a.go": "package a\nfunc A( {\n"})
	out := diagnosticsOf(t, &ToolsHandler{rootDir: dir}, map[string]interface{}{})
	var files []string
	for _, d := range out.Diagnostics {
		files = append(files, d.File)
	}
	if len(files) < 2 || files[0] != "a.go" || !slices.IsSorted(files) {
		t.Errorf("order %v", files)
	}
}
