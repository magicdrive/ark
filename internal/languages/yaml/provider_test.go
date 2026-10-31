package yaml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/yaml/actions"
	"github.com/magicdrive/ark/internal/languages/yaml/yamldoc"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func extractYAML(t testing.TB, file, src string) language.Extraction {
	t.Helper()
	ex, err := NewProvider().Extract(context.Background(), source.FileID(file), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

func qualifieds(ex language.Extraction, kind symbol.SymbolKind) []string {
	var out []string
	for _, s := range ex.Symbols {
		if s.Kind == kind {
			out = append(out, s.Qualified)
		}
	}
	return out
}

func codes(ex language.Extraction) []string {
	var out []string
	for _, d := range ex.Diagnostics {
		out = append(out, d.Code)
	}
	return out
}

func sameLines(t testing.TB, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got:\n  %s\n want:\n  %s", what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestKeys_ThroughMappingsByJSONPointer(t *testing.T) {
	ex := extractYAML(t, "config/app.yml", `# settings
server:
  host: localhost
  tls:
    cert: a.pem
    options:
      deep: never a key
  "a/b~c": 1
list:
  - name: never a key
  - [x]
? complex
: value
'7': quoted number
---
second:
  doc: 2
`)
	if !ex.IdentityOnly {
		t.Error("YAML files are identity-only")
	}
	sameLines(t, "keys", qualifieds(ex, symbol.KindKey), []string{
		"config/app.yml#/server",
		"config/app.yml#/server/host",
		"config/app.yml#/server/tls",
		"config/app.yml#/server/tls/cert",
		"config/app.yml#/server/tls/options",
		"config/app.yml#/server/a~1b~0c",
		"config/app.yml#/list",
		"config/app.yml#/complex",
		"config/app.yml#/7",
		"config/app.yml#1/second",
		"config/app.yml#1/second/doc",
	})
	sameLines(t, "documents", qualifieds(ex, symbol.KindDocument), []string{"config/app.yml"})
	for _, s := range ex.Symbols {
		if s.Kind == symbol.KindKey && s.Qualified == "config/app.yml#/server/tls" && s.Parent != "config/app.yml#/server" {
			t.Errorf("parent of tls: %q", s.Parent)
		}
		if s.Kind == symbol.KindKey && s.Qualified == "config/app.yml#/server" && s.Parent != "config/app.yml" {
			t.Errorf("parent of server: %q", s.Parent)
		}
	}
	if len(ex.References) != 0 || len(ex.Diagnostics) != 0 {
		t.Errorf("references %v diagnostics %v", ex.References, ex.Diagnostics)
	}
}

func TestLocations_BytesAndUnicode(t *testing.T) {
	src := "名前: 値\nキー:\n  ネスト: 🚀 rocket\n"
	ex := extractYAML(t, "u.yaml", src)
	for _, s := range ex.Symbols {
		got := src[s.StartByte:s.EndByte]
		switch s.Qualified {
		case "u.yaml#/名前":
			if got != "名前: 値" || s.Location.Range.Start != (source.Position{Line: 1, Column: 1}) {
				t.Errorf("名前: %q at %v", got, s.Location.Range)
			}
		case "u.yaml#/キー/ネスト":
			// Columns count bytes, as every provider's do.
			if got != "ネスト: 🚀 rocket" || s.Location.Range.Start != (source.Position{Line: 3, Column: 3}) ||
				s.Location.Range.End != (source.Position{Line: 3, Column: uint32(3 + len("ネスト: 🚀 rocket"))}) {
				t.Errorf("ネスト: %q at %v", got, s.Location.Range)
			}
		}
	}
}

func TestAnchorsAndAliases(t *testing.T) {
	src := `base: &base
  adapter: pg
dev:
  <<: *base
  name: dev
list: [*base, &item x, *item]
again: &base {redefined: true}
later: *base
`
	ex := extractYAML(t, "db.yml", src)
	sameLines(t, "anchors", qualifieds(ex, symbol.KindAnchor), []string{"db.yml#&base", "db.yml#&item", "db.yml#&base#2"})
	var got []string
	for _, r := range ex.References {
		got = append(got, fmt.Sprintf("%s -%s-> %s at %d:%d", r.Container, r.Kind, r.NameQualified, r.Location.Range.Start.Line, r.Location.Range.Start.Column))
		if !r.IdentityInRepository || r.TargetKinds != "anchor" {
			t.Errorf("alias reference %+v", r)
		}
	}
	sameLines(t, "aliases", got, []string{
		"db.yml#/dev -value_reference-> db.yml#&base at 4:7",
		"db.yml#/list -value_reference-> db.yml#&base at 6:8",
		"db.yml#/list -value_reference-> db.yml#&item at 6:24",
		// An alias names the anchor defined last before it.
		"db.yml#/later -value_reference-> db.yml#&base#2 at 8:8",
	})
}

func TestDiagnostics(t *testing.T) {
	for _, c := range []struct {
		src   string
		code  string
		error bool
	}{
		{"a: [\n", yamldoc.CodeSyntax, true},
		{"a: 1\na: 2\n", yamldoc.CodeDuplicateKey, false},
		{"\xFE\xFF\x00a", yamldoc.CodeEncoding, true},
		{"a: \xC3\x28\n", yamldoc.CodeSyntax, true},
		{"a: *nowhere\n", yamldoc.CodeSyntax, true},
	} {
		ex := extractYAML(t, "x.yaml", c.src)
		if len(ex.Diagnostics) == 0 || ex.Diagnostics[0].Code != c.code ||
			(ex.Diagnostics[0].Severity == language.SeverityError) != c.error {
			t.Errorf("%q: diagnostics %+v, want %s", c.src, ex.Diagnostics, c.code)
		}
		// The file itself is always declared.
		if len(qualifieds(ex, symbol.KindDocument)) != 1 {
			t.Errorf("%q: no document symbol", c.src)
		}
	}
	// Unknown tags are text, never resolved or run.
	ex := extractYAML(t, "tags.yaml", "a: !custom value\nb: !!python/object:os.system ls\n!k key: 1\nc: !<tag:x> [1]\n")
	if len(ex.Diagnostics) != 0 || len(qualifieds(ex, symbol.KindKey)) != 4 {
		t.Errorf("tags: %v %v", codes(ex), qualifieds(ex, symbol.KindKey))
	}
	// A workflow's problems are its diagnostics too.
	ex = extractYAML(t, ".github/workflows/w.yml", "on: push\njobs:\n  a: {needs: a, runs-on: x, steps: [{uses: nope}]}\n")
	sameLines(t, "workflow diagnostics", codes(ex), []string{actions.CodeInvalidUses, actions.CodeNeedsCycle})
}

func TestBounds(t *testing.T) {
	// Larger than the reader takes: the file only, and why.
	big := strings.Repeat("k: v\n", yamldoc.MaxBytes/5+1)
	ex := extractYAML(t, "big.yaml", big)
	if len(ex.Symbols) != 1 || !slices.Contains(codes(ex), yamldoc.CodeTooLarge) {
		t.Errorf("big file: %d symbols, %v", len(ex.Symbols), codes(ex))
	}
	// More keys than the provider states: bounded, and said so once.
	var b strings.Builder
	for i := 0; i < maxSymbols+100; i++ {
		fmt.Fprintf(&b, "k%d: v\n", i)
	}
	ex = extractYAML(t, "many.yaml", b.String())
	if len(ex.Symbols) != maxSymbols || slices.Index(codes(ex), CodeTruncated) < 0 || len(codes(ex)) != 1 {
		t.Errorf("many keys: %d symbols, %v", len(ex.Symbols), codes(ex))
	}
	// A huge scalar and a huge sequence are no declarations.
	ex = extractYAML(t, "seq.yaml", "big: "+strings.Repeat("x", 1<<20)+"\nlist:\n"+strings.Repeat("  - 1\n", 100000))
	if len(ex.Symbols) != 3 || len(ex.Diagnostics) != 0 {
		t.Errorf("huge scalar and sequence: %d symbols, %v", len(ex.Symbols), codes(ex))
	}
}

func TestDeterministicAndStableIdentities(t *testing.T) {
	src := "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: one\n      - name: Two\n        run: two\n  b:\n    needs: a\n    runs-on: x\n"
	first := extractYAML(t, ".github/workflows/ci.yml", src)
	for i := 0; i < 5; i++ {
		if again := extractYAML(t, ".github/workflows/ci.yml", src); !reflect.DeepEqual(first, again) {
			t.Fatal("two extractions of one source differ")
		}
	}
	// Editing other content (a step inserted before, a comment, another
	// job) changes no identity of what was there.
	edited := strings.Replace(src, "    steps:\n", "    steps:\n      - uses: actions/checkout@v4\n", 1)
	edited = "# a comment\n" + strings.Replace(edited, "  b:\n", "  z:\n    runs-on: y\n  b:\n", 1)
	after := extractYAML(t, ".github/workflows/ci.yml", edited)
	for _, q := range qualifieds(first, symbol.KindStep) {
		if !slices.Contains(qualifieds(after, symbol.KindStep), q) {
			t.Errorf("step %s lost its identity", q)
		}
	}
	for _, q := range qualifieds(first, symbol.KindJob) {
		if !slices.Contains(qualifieds(after, symbol.KindJob), q) {
			t.Errorf("job %s lost its identity", q)
		}
	}
}

// The index is the same built cold, warm from the cache and again: edges,
// diagnostics and the fingerprint.
func TestIndex_SameColdAndWarm(t *testing.T) {
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var got [][]string
	var prints []string
	for i := 0; i < 3; i++ {
		idx, err := index.NewWithCache(context.Background(), fixtureRepo, []language.Provider{NewProvider()}, store)
		if err != nil {
			t.Fatal(err)
		}
		lines := edgeLines(idx)
		for _, d := range idx.Diagnostics() {
			lines = append(lines, fmt.Sprintf("%s %s %v", d.Location.File, d.Code, d.Location.Range))
		}
		got = append(got, lines)
		prints = append(prints, idx.Fingerprint())
	}
	if !reflect.DeepEqual(got[0], got[1]) || !reflect.DeepEqual(got[0], got[2]) {
		t.Error("the cached index differs from the cold one")
	}
	if prints[0] != prints[1] || prints[0] != prints[2] {
		t.Error("fingerprints differ")
	}
	idx := buildIndex(t, fixtureRepo)
	if !reflect.DeepEqual(edgeLines(idx), got[0][:len(edgeLines(idx))]) {
		t.Error("the uncached index differs")
	}
}

// In .github the index reads YAML only: a script there is not indexed (no
// provider that reads .github handles it), other hidden directories are not
// entered.
func TestIndex_ReadsDotGithubYAMLOnly(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		".github/workflows/ci.yml":   "on: push\njobs: {a: {runs-on: x}}\n",
		".github/dependabot.yml":     "version: 2\n",
		".github/scripts/release.py": "def main():\n    pass\n",
		".hidden/x.yml":              "a: 1\n",
		".git/config.yml":            "a: 1\n",
		"app.yml":                    "a: 1\n",
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := index.New(context.Background(), dir, []language.Provider{NewProvider(), pyLike{}})
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range idx.Files() {
		files = append(files, string(f))
	}
	slices.Sort(files)
	sameLines(t, "files", files, []string{".github/dependabot.yml", ".github/workflows/ci.yml", "app.yml"})
}

// pyLike is a provider of .py files that reads no hidden directory.
type pyLike struct{}

func (pyLike) Language() language.Language { return "pylike" }
func (pyLike) Extensions() []string        { return []string{".py"} }
func (pyLike) CacheVersion() string        { return "pylike-1" }
func (pyLike) Extract(context.Context, source.FileID, []byte) (language.Extraction, error) {
	return language.Extraction{}, nil
}

// A file whose nodes share one long line (minified JSON, a generated flow
// sequence) is read in time linear in its size: positions are converted
// per line once, not by decoding the line up to each node.
func TestBounds_OneLongLineIsLinear(t *testing.T) {
	var b strings.Builder
	b.WriteString("k: [")
	for i := 0; i < 400000; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("é")
	}
	b.WriteString("]\n")
	start := time.Now()
	ex := extractYAML(t, "one.yaml", b.String())
	if el := time.Since(start); el > 20*time.Second {
		t.Errorf("%d bytes on one line took %v", b.Len(), el)
	}
	if len(ex.Diagnostics) != 0 {
		t.Errorf("diagnostics %v", codes(ex))
	}
}

// Diagnostics are bounded: a file of many duplicate keys reports as many as
// the bound, then that it has more; the duplicates declare nothing.
func TestBounds_Diagnostics(t *testing.T) {
	ex := extractYAML(t, "dup.yaml", strings.Repeat("a: 1\n", 5000))
	if len(ex.Diagnostics) > maxDiagnostics+1 || !slices.Contains(codes(ex), CodeTruncated) {
		t.Errorf("%d diagnostics: %v…", len(ex.Diagnostics), codes(ex)[:3])
	}
	if keys := qualifieds(ex, symbol.KindKey); len(keys) != 1 {
		t.Errorf("duplicate keys declared: %d", len(keys))
	}
}

// A key that is no one-line name — a block of text, a control character,
// a very long key — declares nothing: it would reach the output as an
// identifier. Keys are named by their pointer, anchors by "&name".
func TestKeys_OnlyNames(t *testing.T) {
	ex := extractYAML(t, "k.yaml", "? |\n  -----BEGIN RSA PRIVATE KEY-----\n  MIIE\n: 1\n\"a\\nb\": 2\n\"c\\u0007\": 3\n"+strings.Repeat("x", 300)+": 4\nok: &anchor 5\n")
	var names []string
	for _, s := range ex.Symbols {
		names = append(names, string(s.Kind)+" "+s.Name)
	}
	sameLines(t, "declarations", names, []string{"document k.yaml", "key /ok", "anchor &anchor"})
}

// CRLF line breaks change no structure and no identity: a YAML 1.2
// directive, a %TAG directive with comments before "---".
func TestDocuments_CRLFAndDirectives(t *testing.T) {
	lf := "%YAML 1.2\n---\non: push\njobs:\n  a:\n    runs-on: x\n"
	for _, src := range []string{lf, strings.ReplaceAll(lf, "\n", "\r\n")} {
		ex := extractYAML(t, ".github/workflows/w.yml", src)
		if len(ex.Diagnostics) != 0 || !slices.Contains(qualifieds(ex, symbol.KindJob), ".github/workflows/w.yml::a") {
			t.Errorf("%q: %v %v", src, codes(ex), qualifieds(ex, symbol.KindJob))
		}
	}
	for _, src := range []string{"k: 1\n", "%YAML 1.2\r\n---\r\nk: 1\r\n"} {
		if got := qualifieds(extractYAML(t, "d.yaml", src), symbol.KindKey); !slices.Equal(got, []string{"d.yaml#/k"}) {
			t.Errorf("%q: keys %v", src, got)
		}
	}
	ex := extractYAML(t, "t.yaml", "a: 1\n...\n%TAG !e! tag:example.com,2000:\n# comment\n\n--- !e!foo\nb: 2\n")
	if len(ex.Diagnostics) != 0 || !slices.Contains(qualifieds(ex, symbol.KindKey), "t.yaml#1/b") {
		t.Errorf("directives with comments: %v %v", codes(ex), qualifieds(ex, symbol.KindKey))
	}
}

// A block scalar ends with its text: a comment less indented than the text
// after it is no part of it.
func TestSpans_BlockScalarEndsWithItsText(t *testing.T) {
	src := "a: |\n  line1\n  line2\n# c0\n  # c1\nb: >-\n    x\n\n   # c2\nc: 1\n"
	ex := extractYAML(t, "s.yaml", src)
	for _, s := range ex.Symbols {
		got := src[s.StartByte:s.EndByte]
		switch s.Name {
		case "/a":
			if got != "a: |\n  line1\n  line2" {
				t.Errorf("a: %q", got)
			}
		case "/b":
			if got != "b: >-\n    x" {
				t.Errorf("b: %q", got)
			}
		}
	}
}

// The diagnostics of both layers together are bounded.
func TestBounds_DiagnosticsOfBothLayers(t *testing.T) {
	var b strings.Builder
	b.WriteString("on: push\njobs:\n  a:\n    runs-on: x\n    env:\n")
	for i := 0; i < 900; i++ {
		b.WriteString("      K: v\n")
	}
	b.WriteString("    steps:\n")
	for i := 0; i < 900; i++ {
		fmt.Fprintf(&b, "      - uses: bad%d\n", i)
	}
	ex := extractYAML(t, ".github/workflows/w.yml", b.String())
	if len(ex.Diagnostics) > maxDiagnostics+1 || !slices.Contains(codes(ex), CodeTruncated) {
		t.Errorf("%d diagnostics", len(ex.Diagnostics))
	}
}

// Findings of the second audit: flow scalars with an anchor or a tag end
// with their text; a block scalar's indentation indicator fixes its text;
// an anchor that is no name declares nothing; a name with a bidirectional
// or zero-width character is none.
func TestSecondAudit_Spans(t *testing.T) {
	src := "k: [&a x, &b !t y, &c z]\nm: {p: &d v, q: &e !t w}\na:\n  b: |2\n     x\n    y\nc: 1\n\"e\u202egnp\": 1\n"
	ex := extractYAML(t, "f.yaml", src)
	want := map[string]string{"&a": "&a x", "&b": "&b !t y", "&c": "&c z", "/m/p": "p: &d v", "/m/q": "q: &e !t w", "/a/b": "b: |2\n     x\n    y"}
	for _, s := range ex.Symbols {
		if w, ok := want[s.Name]; ok {
			if got := src[s.StartByte:s.EndByte]; got != w {
				t.Errorf("%s: %q, want %q", s.Name, got, w)
			}
			delete(want, s.Name)
		}
		if strings.ContainsRune(s.Name, '\u202e') {
			t.Errorf("a bidirectional character in a name: %q", s.Name)
		}
	}
	if len(want) != 0 {
		t.Errorf("not declared: %v", want)
	}
	long := strings.Repeat("a", 300)
	ex = extractYAML(t, "g.yaml", "x: &"+long+" 1\ny: *"+long+"\n")
	if len(qualifieds(ex, symbol.KindAnchor)) != 0 || len(ex.References) != 0 {
		t.Errorf("an overlong anchor: %v %v", qualifieds(ex, symbol.KindAnchor), ex.References)
	}
}

// The meaning layer's problems past its bound are reported as such, also
// when they alone reach it.
func TestBounds_ActionsDiagnosticsReported(t *testing.T) {
	var b strings.Builder
	b.WriteString("on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n")
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&b, "      - {id: s%d, uses: bad%d}\n", i, i)
	}
	ex := extractYAML(t, ".github/workflows/w.yml", b.String())
	if !slices.Contains(codes(ex), CodeTruncated) {
		t.Errorf("%d diagnostics, no truncation reported", len(ex.Diagnostics))
	}
}
