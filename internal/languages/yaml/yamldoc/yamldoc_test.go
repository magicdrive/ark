package yamldoc

import (
	"strconv"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

// text returns the source of the entry of key in the mapping m.
func entryText(s *Stream, m *yaml.Node, key string) string {
	k, v := Get(m, key)
	if k == nil {
		return "<none>"
	}
	sp := s.EntrySpan(k, v)
	return string(s.Src[sp.Start:sp.End])
}

func TestSpans_Entries(t *testing.T) {
	src := "# head\n" +
		"plain: value # comment\n" +
		"quoted: \"a \\\" b\"\n" +
		"single: 'it''s'\n" +
		"flow: {a: 1, b: [x, y]}  # c\n" +
		"block: |\n" +
		"  line 1\n" +
		"  # still text\n" +
		"\n" +
		"# a comment of the next key\n" +
		"folded: >-\n" +
		"  one\n" +
		"  two\n" +
		"seq:\n" +
		"  - a\n" +
		"  - k: v\n" +
		"    m: |\n" +
		"      x\n" +
		"  - last\n" +
		"multi: one\n" +
		"  two\n" +
		"  # trailing\n" +
		"empty:\n" +
		"é: ü\n"
	s := Parse([]byte(src))
	if len(s.Problems) != 0 || len(s.Docs) != 1 {
		t.Fatalf("problems %v docs %d", s.Problems, len(s.Docs))
	}
	root := s.Docs[0].Root
	for key, want := range map[string]string{
		"plain":  "plain: value",
		"quoted": "quoted: \"a \\\" b\"",
		"single": "single: 'it''s'",
		"flow":   "flow: {a: 1, b: [x, y]}",
		"block":  "block: |\n  line 1\n  # still text",
		"folded": "folded: >-\n  one\n  two",
		"seq":    "seq:\n  - a\n  - k: v\n    m: |\n      x\n  - last",
		"multi":  "multi: one\n  two",
		"empty":  "empty:",
		"é":      "é: ü",
	} {
		if got := entryText(s, root, key); got != want {
			t.Errorf("%s: got %q, want %q", key, got, want)
		}
	}
	_, seq := Get(root, "seq")
	item := seq.Content[1]
	if got := string(s.Src[s.Start(item):s.End(item)]); got != "k: v\n    m: |\n      x" {
		t.Errorf("sequence item: %q", got)
	}
}

func TestSpans_LineBreaksAndBOM(t *testing.T) {
	for _, src := range []string{
		"\xEF\xBB\xBFa: 1\nb: x\n",
		"a: 1\rb: x\r\n",
		"a: \"1\u2028\"\nb: x\n",
		"a: \"1\u0085\"\nb: x\n",
	} {
		s := Parse([]byte(src))
		k, v := Get(s.Docs[0].Root, "b")
		if k == nil || string(s.Src[s.Start(k):s.End(v)]) != "b: x" {
			t.Errorf("%q: entry b at %d:%d", src, s.Start(k), s.End(v))
		}
		// The first line too: a byte order mark is before the key.
		k, v = Get(s.Docs[0].Root, "a")
		if k == nil || !strings.HasPrefix(string(s.Src[s.Start(k):s.End(v)]), "a: ") {
			t.Errorf("%q: entry a at %d:%d", src, s.Start(k), s.End(v))
		}
	}
}

func TestDocuments_RecoverAfterAnError(t *testing.T) {
	src := "a: 1\n---\nbad: [\n---\nc: 3\n...\n---\n# only a comment\n"
	s := Parse([]byte(src))
	if len(s.Docs) != 4 {
		t.Fatalf("docs %d, want 4", len(s.Docs))
	}
	if s.Docs[1].Root != nil || s.Docs[1].Index != 1 {
		t.Errorf("the broken document keeps its place: %+v", s.Docs[1])
	}
	if k, _ := Get(s.Docs[2].Root, "c"); k == nil || s.Docs[2].Index != 2 {
		t.Errorf("document after the error not read")
	}
	if r := s.Docs[3].Root; r != nil && (r.Kind != yaml.ScalarNode || r.Tag != "!!null") {
		t.Errorf("a comment-only document has no content: %+v", r)
	}
	if len(s.Problems) != 1 || s.Problems[0].Code != CodeSyntax || !s.Problems[0].Error {
		t.Fatalf("problems %+v", s.Problems)
	}
	if p := s.Problems[0]; !strings.HasPrefix(src[p.Start:], "bad: [") && !strings.HasPrefix(src[p.Start:], "---") {
		t.Errorf("problem at %q", src[p.Start:p.End])
	}
}

func TestProblems(t *testing.T) {
	cases := []struct {
		src  string
		code string
	}{
		{"a: 1\na: 2\n", CodeDuplicateKey},
		{"\xFF\xFEa\x00", CodeEncoding},
		{"a: \xFF\n", CodeSyntax},
		{"a:\n\t- b\n", CodeSyntax},
		{strings.Repeat("[", 20000) + strings.Repeat("]", 20000), CodeSyntax},
		{strings.Repeat("a", MaxBytes+1), CodeTooLarge},
	}
	for _, c := range cases {
		s := Parse([]byte(c.src))
		if len(s.Problems) == 0 || s.Problems[0].Code != c.code {
			t.Errorf("%.20q: problems %+v, want %s", c.src, s.Problems, c.code)
		}
		for _, p := range s.Problems {
			if p.Start < 0 || p.End > len(c.src) || p.Start > p.End {
				t.Errorf("%.20q: problem out of range %+v", c.src, p)
			}
		}
	}
}

func TestYAML12DirectiveIsRead(t *testing.T) {
	s := Parse([]byte("%YAML 1.2\n---\na: 1\n"))
	if len(s.Problems) != 0 || len(s.Docs) != 1 {
		t.Fatalf("problems %v docs %d", s.Problems, len(s.Docs))
	}
	if k, _ := Get(s.Docs[0].Root, "a"); k == nil || s.Start(k) != len("%YAML 1.2\n---\n") {
		t.Errorf("key a not at its offset")
	}
}

func TestAliases_NeverExpanded(t *testing.T) {
	// A cycle (an anchor whose node holds its own alias) and an alias bomb.
	var b strings.Builder
	b.WriteString("a0: &a0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i < 40; i++ {
		b.WriteString("a" + itoa(i) + ": &a" + itoa(i) + " [*a" + itoa(i-1) + ", *a" + itoa(i-1) + ", *a" + itoa(i-1) + "]\n")
	}
	b.WriteString("self: &s [*s]\n")
	s := Parse([]byte(b.String()))
	if len(s.Problems) != 0 {
		t.Fatalf("problems %v", s.Problems)
	}
	_, self := Get(s.Docs[0].Root, "self")
	if n, ok := Deref(self.Content[0]); !ok || n != self {
		t.Errorf("Deref of the self alias")
	}
	loop := &yaml.Node{Kind: yaml.AliasNode}
	loop.Alias = loop
	if _, ok := Deref(loop); ok {
		t.Errorf("an alias chain that does not end")
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

// A stream records at most maxProblems problems, and says it has more.
func TestProblems_Bounded(t *testing.T) {
	s := Parse([]byte(strings.Repeat("a: 1\n", 3*maxProblems)))
	if len(s.Problems) != maxProblems || !s.ProblemsTruncated {
		t.Errorf("%d problems, truncated %v", len(s.Problems), s.ProblemsTruncated)
	}
}

// An anchor or a tag in flow ends at the flow's indicator; a name holds no
// invisible character.
func TestThirdAudit_FlowAnchorsAndNames(t *testing.T) {
	src := "seq: [&s 1, &w]\nm: {e: &z , g: 1}\n"
	s := Parse([]byte(src))
	_, seq := Get(s.Docs[0].Root, "seq")
	if got := string(s.Src[s.Start(seq.Content[1]):s.End(seq.Content[1])]); got != "&w" {
		t.Errorf("&w: %q", got)
	}
	for _, n := range []string{"a\u061cb", "a\u2060b", "a\u00adb", "a\u180eb", "a\u202eb"} {
		if IsName(n) {
			t.Errorf("%q is a name", n)
		}
	}
	if !IsName("日本語のジョブ 🚀") {
		t.Error("an ordinary Unicode name is a name")
	}
}
