package tomldoc

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	bstoml "github.com/BurntSushi/toml"
	gotoml "github.com/pelletier/go-toml/v2"
)

// tree renders the tree as "pointer kind span" lines.
func tree(d *Doc) []string {
	var out []string
	var walk func(n *Node, at string)
	walk = func(n *Node, at string) {
		for i, c := range n.Children {
			p := at + "/" + c.Key
			if n.Kind == ArrayOfTables {
				p = fmt.Sprintf("%s[%d]", at, i)
			}
			kind := map[Kind]string{Table: "table", ArrayOfTables: "aot", Value: "value"}[c.Kind]
			if c.Kind == Table {
				kind += map[How]string{Header: ":header", Implicit: ":implicit", Dotted: ":dotted", Inline: ":inline", Element: ":element"}[c.How]
			} else if c.Kind == Value {
				kind += ":" + string(c.VKind)
			}
			out = append(out, fmt.Sprintf("%s %s %q", p, kind, d.Src[c.Span.Start:c.Span.End]))
			walk(c, p)
		}
	}
	walk(d.Root, "")
	return out
}

func codes(d *Doc) []string {
	var out []string
	for _, p := range d.Problems {
		out = append(out, fmt.Sprintf("%s@%d", p.Code, p.Start))
	}
	return out
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got:\n  %s\n want:\n  %s", what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestParse_Tree(t *testing.T) {
	src := "top = 1\n[a.'b c']\nx = \"s\" # c\r\ny.z = [1,\n 2]\n\n[[bin]]\nname = \"x\"\n[[bin]]\nt = {u = 1, v.w = 2}\n[a]\nd = 1979-05-27\n"
	d := Parse([]byte(src))
	equal(t, "problems", codes(d), nil)
	equal(t, "tree", tree(d), []string{
		`/top value:integer "top = 1"`,
		// [a] holds [a.'b c'], written before it: the hull of both.
		`/a table:header "[a.'b c']\nx = \"s\" # c\r\ny.z = [1,\n 2]\n\n[[bin]]\nname = \"x\"\n[[bin]]\nt = {u = 1, v.w = 2}\n[a]\nd = 1979-05-27"`,
		`/a/b c table:header "[a.'b c']\nx = \"s\" # c\r\ny.z = [1,\n 2]"`,
		`/a/b c/x value:string "x = \"s\""`,
		`/a/b c/y table:dotted "y.z = [1,\n 2]"`,
		`/a/b c/y/z value:array "y.z = [1,\n 2]"`,
		`/a/d value:local-date "d = 1979-05-27"`,
		`/bin aot "[[bin]]\nname = \"x\"\n[[bin]]\nt = {u = 1, v.w = 2}"`,
		`/bin[0] table:element "[[bin]]\nname = \"x\""`,
		`/bin[0]/name value:string "name = \"x\""`,
		`/bin[1] table:element "[[bin]]\nt = {u = 1, v.w = 2}"`,
		`/bin[1]/t table:inline "t = {u = 1, v.w = 2}"`,
		`/bin[1]/t/u value:integer "u = 1"`,
		`/bin[1]/t/v table:inline "t = {u = 1, v.w = 2}"`,
		`/bin[1]/t/v/w value:integer "v.w = 2"`,
	})
	if d.Root.Get("a").Get("b c").Get("x").Str.Value != "s" {
		t.Error("string value")
	}
}

func TestParse_Problems(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want []string
	}{
		{"a = 1\na = 2\nb = 1\nb = 3\n", []string{"toml_duplicate_key@6", "toml_duplicate_key@18"}},
		{"[a]\n[a]\n", []string{"toml_table_redefined@5"}},
		{"[a]\nb.c = 1\n[a.b]\n", []string{"toml_table_redefined@15"}},
		{"a = 1\n[a.b]\n", []string{"toml_table_redefined@7"}},
		{"a = {b = 1}\na.c = 2\n", []string{"toml_duplicate_key@12"}},
		{"[[a]]\n[a]\n", []string{"toml_table_redefined@7"}},
		{"x = [1, 2\n", []string{"toml_syntax_error@9"}},
		{"bad = 1979-13-27\n", []string{"toml_invalid@6"}},
		{"ok = 1\n= 2\n", []string{"toml_syntax_error@7"}},
		{"[a.b.c]\nz = 9\n[a]\nb.c.t = 1\n", []string{"toml_duplicate_key@18"}},
		{"d = 1985-06-18 17:04:07+12:60\n", []string{"toml_invalid@4"}},
		// BurntSushi/toml accepts these three; TOML forbids them (a header
		// redefining a table dotted keys define, dotted keys extending a
		// table a header implies, a sealed inline table), as go-toml does.
		{"a.b = 1\n[a]\n", []string{"toml_table_redefined@9"}},
		{"[a.b.c]\n[a]\nb.d = 1\n", []string{"toml_duplicate_key@12"}},
		{"a = {b = {}, b.c = 1}\n", []string{"toml_duplicate_key@13"}},
		// Valid (both decoders agree).
		{"[a]\nb.c = 1\n[a.b.d]\n", nil},
		{"a = {b.c = 1, b.d = 2}\n", nil},
	} {
		d := Parse([]byte(tc.src))
		got := codes(d)
		if tc.want == nil {
			var m map[string]any
			err := gotoml.Unmarshal([]byte(tc.src), &m)
			if _, err2 := bstoml.Decode(tc.src, &m); (err == nil) != (err2 == nil) {
				t.Errorf("%q: the decoders disagree: %v / %v", tc.src, err, err2)
			}
			if err != nil {
				if len(got) == 0 {
					t.Errorf("%q: the decoder rejects it (%v), Parse reports nothing", tc.src, err)
				}
			} else if len(got) != 0 {
				t.Errorf("%q: the decoder accepts it, Parse reports %v", tc.src, got)
			}
			continue
		}
		equal(t, fmt.Sprintf("%q", tc.src), got, tc.want)
	}
}

func TestParse_KeepsWhatPrecedesASyntaxError(t *testing.T) {
	d := Parse([]byte("[a]\nb = 1\nc = \n"))
	if d.Complete || d.Root.Get("a").Get("b") == nil {
		t.Fatalf("tree %v complete %v", tree(d), d.Complete)
	}
}

func TestParse_Bounds(t *testing.T) {
	deep := strings.Repeat("[", MaxCheckedNesting+1) + strings.Repeat("]", MaxCheckedNesting+1)
	d := Parse([]byte("x = " + deep + "\n"))
	equal(t, "deep", codes(d), []string{"toml_not_checked@0"})
	d = Parse([]byte("x = " + deep[1:len(deep)-1] + "\nd = 2024-02-30\n"))
	equal(t, "checked", codes(d), []string{"toml_invalid@" + strconv.Itoa(4+2*MaxCheckedNesting+5)})
	d = Parse([]byte(strings.Repeat("a.", MaxCheckedKeyParts) + "b = 1\n"))
	equal(t, "long key", codes(d), []string{"toml_not_checked@0"})
	d = Parse(make([]byte, MaxBytes+1))
	equal(t, "large", codes(d), []string{"toml_too_large@0"})
	long := strings.Repeat("a.", MaxKeyParts) + "a = 1\n[" + strings.Repeat("b.", MaxKeyParts) + "b]\nc = 1\n"
	d = Parse([]byte(long))
	if len(d.Root.Children) != 0 {
		t.Errorf("an overlong key defined %v", tree(d)[:1])
	}
	var n int
	for _, p := range d.Problems {
		if p.Code == CodeKeyTooLong {
			n++
		}
	}
	if n != 2 {
		t.Errorf("problems %v", codes(d))
	}
	var b strings.Builder
	for i := 0; i < MaxProblems+10; i++ {
		b.WriteString("a = 1\n")
	}
	d = Parse([]byte(b.String()))
	if len(d.Problems) != MaxProblems || !d.ProblemsTruncated {
		t.Errorf("%d problems, truncated %v", len(d.Problems), d.ProblemsTruncated)
	}
}

func TestParse_UnicodeAndCRLF(t *testing.T) {
	src := "\"ключ\" = \"значение\"\r\n[\"テーブル\"]\r\n'é' = 1\r\n"
	d := Parse([]byte(src))
	equal(t, "problems", codes(d), nil)
	equal(t, "tree", tree(d), []string{
		`/ключ value:string "\"ключ\" = \"значение\""`,
		`/テーブル table:header "[\"テーブル\"]\r\n'é' = 1"`,
		`/テーブル/é value:integer "'é' = 1"`,
	})
}

// TestParse_Linear: a document of many tables, many keys on one line (an
// inline table), a long dotted key chain and deep nesting are read in
// time linear in their size.
func TestParse_Linear(t *testing.T) {
	gen := map[string]func(n int) string{
		"tables": func(n int) string {
			var b strings.Builder
			for i := 0; i < n; i++ {
				fmt.Fprintf(&b, "[t%d.u]\nk = %d\n", i, i)
			}
			return b.String()
		},
		"one line": func(n int) string {
			var b strings.Builder
			b.WriteString("x = {")
			for i := 0; i < n; i++ {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "k%d = \"v\"", i)
			}
			b.WriteString("}\n")
			return b.String()
		},
		"nested": func(n int) string {
			return "x = " + strings.Repeat("[", n/10) + strings.Repeat("]", n/10) + "\n"
		},
		"aot": func(n int) string {
			return strings.Repeat("[[a.b]]\nc = 1\n", n)
		},
	}
	for name, g := range gen {
		small, large := g(2000), g(32000)
		ts, tl := timeParse(small), timeParse(large)
		if tl > 40*ts+20*time.Millisecond {
			t.Errorf("%s: 16x the input took %v vs %v", name, tl, ts)
		}
	}
}

func timeParse(s string) time.Duration {
	best := time.Hour
	for i := 0; i < 3; i++ {
		start := time.Now()
		Parse([]byte(s))
		if d := time.Since(start); d < best {
			best = d
		}
	}
	return best
}

// TestTOMLTestSuite is an opt-in check against the toml-test suite (the
// copy github.com/BurntSushi/toml carries in internal/toml-test/tests, or
// any checkout's tests directory): ARK_TOML_TEST_DIR names it. Every valid
// file of TOML 1.0 (and 1.1) reads without an error and with the tree of
// tables the decoder builds; every invalid file of TOML 1.0 reports an
// error, except the 1.1 relaxations tomldoc reads (see the package
// header).
func TestTOMLTestSuite(t *testing.T) {
	dir := os.Getenv("ARK_TOML_TEST_DIR")
	if dir == "" {
		t.Skip("ARK_TOML_TEST_DIR is not set")
	}
	only11 := map[string]bool{ // invalid in 1.0, valid in 1.1
		"invalid/datetime/no-secs": true, "invalid/local-time/no-secs": true, "invalid/local-datetime/no-secs": true,
		"invalid/string/basic-byte-escapes": true, "invalid/inline-table/trailing-comma": true,
		"invalid/inline-table/linebreak-01": true, "invalid/inline-table/linebreak-02": true,
		"invalid/inline-table/linebreak-03": true, "invalid/inline-table/linebreak-04": true,
	}
	var valid, invalid, failed int
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(p) != ".toml" {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = strings.TrimSuffix(filepath.ToSlash(rel), ".toml")
		if strings.HasPrefix(rel, "invalid/spec-1.1.0/") || only11[rel] {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		d := Parse(src)
		hasErr := false
		for _, pr := range d.Problems {
			hasErr = hasErr || pr.Error
			if pr.Start < 0 || pr.End > len(src) || pr.Start > pr.End {
				t.Errorf("%s: problem at [%d,%d)", rel, pr.Start, pr.End)
			}
		}
		switch {
		case strings.HasPrefix(rel, "valid/"):
			valid++
			if hasErr {
				failed++
				t.Errorf("%s: valid, reported %v", rel, d.Problems)
				return nil
			}
			var m map[string]any
			if err := gotoml.Unmarshal(src, &m); err != nil {
				t.Errorf("%s: the decoder rejects it: %v", rel, err)
				return nil
			}
			if msg := sameShape(d.Root, m, ""); msg != "" {
				failed++
				t.Errorf("%s: %s", rel, msg)
			}
		case strings.HasPrefix(rel, "invalid/"):
			invalid++
			if !hasErr {
				failed++
				t.Errorf("%s: invalid, reported nothing", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("toml-test: %d valid, %d invalid files read; %d failed", valid, invalid, failed)
}

// sameShape compares the tree with the decoder's map: the same keys, a
// table where it has a table, the elements of an array of tables.
func sameShape(n *Node, v any, at string) string {
	switch n.Kind {
	case Table:
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: a table, the decoder has %T", at, v)
		}
		var keys []string
		for _, c := range n.Children {
			keys = append(keys, c.Key)
		}
		var want []string
		for k := range m {
			want = append(want, k)
		}
		sort.Strings(keys)
		sort.Strings(want)
		if strings.Join(keys, "\x00") != strings.Join(want, "\x00") {
			return fmt.Sprintf("%s: keys %q, the decoder's %q", at, keys, want)
		}
		for _, c := range n.Children {
			if msg := sameShape(c, m[c.Key], path.Join(at, c.Key)); msg != "" {
				return msg
			}
		}
	case ArrayOfTables:
		a, ok := v.([]any)
		if !ok || len(a) != len(n.Children) {
			return fmt.Sprintf("%s: %d tables, the decoder has %v", at, len(n.Children), v)
		}
		for i, c := range n.Children {
			if msg := sameShape(c, a[i], fmt.Sprintf("%s[%d]", at, i)); msg != "" {
				return msg
			}
		}
	case Value:
		if _, ok := v.(map[string]any); ok {
			return fmt.Sprintf("%s: a value, the decoder has a table", at)
		}
		if s, ok := v.(string); ok && n.VKind == String && s != n.Str.Value {
			return fmt.Sprintf("%s: %q, the decoder has %q", at, n.Str.Value, s)
		}
	}
	return ""
}
