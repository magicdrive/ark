package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/magicdrive/ark/internal/mcp"
)

// userDocs are the documents a user reads. Their relative links and anchors
// must resolve, so a renamed file or heading fails here rather than leaving a
// dead link.
func userDocs(t *testing.T) []string {
	t.Helper()
	docs := []string{"README.md", "README_ja.md", "ARCHITECTURE.md", "SECURITY.md",
		".github/release/NOTES-v6.0.0.md", ".github/release/CHECKLIST-v6.0.0.md"}
	more, err := filepath.Glob("docs/*.md")
	if err != nil || len(more) == 0 {
		t.Fatalf("no docs/*.md (%v)", err)
	}
	return append(docs, more...)
}

var (
	mdLink    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	mdHeading = regexp.MustCompile(`(?m)^#{1,6} +(.+?) *$`)
	fence     = regexp.MustCompile("(?ms)^```.*?^```")
	inline    = regexp.MustCompile("``[^`]*``|`[^`\n]*`")
)

// githubAnchor is the anchor GitHub gives a heading: lower case, markup and
// punctuation other than '-' and '_' removed, spaces turned into '-'.
func githubAnchor(heading string) string {
	heading = strings.NewReplacer("`", "", "*", "").Replace(heading)
	heading = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`).ReplaceAllString(heading, "$1")
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func anchorsOf(t *testing.T, file string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	text := fence.ReplaceAllString(string(data), "")
	anchors := map[string]bool{}
	seen := map[string]int{}
	for _, m := range mdHeading.FindAllStringSubmatch(text, -1) {
		a := githubAnchor(m[1])
		if n := seen[a]; n > 0 {
			anchors[fmt.Sprintf("%s-%d", a, n)] = true
		} else {
			anchors[a] = true
		}
		seen[a]++
	}
	return anchors
}

func TestDocs_LinksResolve(t *testing.T) {
	for _, doc := range userDocs(t) {
		data, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		text := inline.ReplaceAllString(fence.ReplaceAllString(string(data), ""), "")
		for _, m := range mdLink.FindAllStringSubmatch(text, -1) {
			link := m[1]
			if strings.Contains(link, "://") || strings.HasPrefix(link, "mailto:") {
				continue
			}
			target, anchor, _ := strings.Cut(link, "#")
			file := doc
			if target != "" {
				file = filepath.Join(filepath.Dir(doc), filepath.FromSlash(target))
				if _, err := os.Stat(file); err != nil {
					t.Errorf("%s: link %q: %s does not exist", doc, link, file)
					continue
				}
			}
			if anchor != "" && strings.HasSuffix(file, ".md") && !anchorsOf(t, file)[anchor] {
				t.Errorf("%s: link %q: no heading with anchor #%s in %s", doc, link, anchor, file)
			}
		}
	}
}

// TestDocs_MCPToolReferenceMatchesServer keeps docs/mcp-tools.md equal to the
// server's tools/list: the same tools, and per tool the same parameters with
// the same type, required flag and default.
func TestDocs_MCPToolReferenceMatchesServer(t *testing.T) {
	data, err := os.ReadFile("docs/mcp-tools.md")
	if err != nil {
		t.Fatal(err)
	}
	heading := regexp.MustCompile("^### `([a-z_]+)`$")
	row := regexp.MustCompile("^\\| `([A-Za-z0-9]+)` \\| ([^|]*) \\| ([^|]*) \\| ([^|]*) \\|")
	documented := map[string]map[string]string{}
	var current string
	for _, line := range strings.Split(string(data), "\n") {
		if m := heading.FindStringSubmatch(line); m != nil {
			current = m[1]
			documented[current] = map[string]string{}
			continue
		}
		if m := row.FindStringSubmatch(line); m != nil && current != "" {
			documented[current][m[1]] = strings.TrimSpace(m[2]) + "|" + strings.TrimSpace(m[3]) + "|" + strings.TrimSpace(m[4])
		}
	}

	server := map[string]map[string]string{}
	for _, tool := range mcp.NewToolsHandler(t.TempDir(), nil).ListTools() {
		params := map[string]string{}
		props, _ := tool.InputSchema["properties"].(map[string]interface{})
		required := map[string]bool{}
		switch r := tool.InputSchema["required"].(type) {
		case []string:
			for _, n := range r {
				required[n] = true
			}
		case []interface{}:
			for _, n := range r {
				required[fmt.Sprint(n)] = true
			}
		}
		for name, p := range props {
			prop, _ := p.(map[string]interface{})
			typ := fmt.Sprint(prop["type"])
			if typ == "array" {
				items, _ := prop["items"].(map[string]interface{})
				typ = "array of " + fmt.Sprint(items["type"])
			}
			if enum, ok := prop["enum"]; ok {
				var vals []string
				switch e := enum.(type) {
				case []string:
					vals = e
				case []interface{}:
					for _, v := range e {
						vals = append(vals, fmt.Sprint(v))
					}
				}
				for i, v := range vals {
					vals[i] = "`" + v + "`"
				}
				typ += " (" + strings.Join(vals, ", ") + ")"
			}
			req := ""
			if required[name] {
				req = "yes"
			}
			def := ""
			if d, ok := prop["default"]; ok {
				if s, isString := d.(string); isString {
					def = "`" + s + "`"
				} else {
					b, _ := json.Marshal(d)
					def = "`" + string(b) + "`"
				}
			}
			params[name] = typ + "|" + req + "|" + def
		}
		server[tool.Name] = params
	}

	names := func(m map[string]map[string]string) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	if got, want := names(documented), names(server); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("documented tools %v, server tools %v", got, want)
	}
	for tool, params := range server {
		for name, want := range params {
			if got, ok := documented[tool][name]; !ok {
				t.Errorf("%s: parameter %s is not documented", tool, name)
			} else if got != want {
				t.Errorf("%s.%s: documented %q, server %q (type|required|default)", tool, name, got, want)
			}
		}
		for name := range documented[tool] {
			if _, ok := params[name]; !ok {
				t.Errorf("%s: documented parameter %s does not exist", tool, name)
			}
		}
	}
}
