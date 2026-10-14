package css

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestHarnessAgreement compares the reader with the implementation the
// parser selection measured (docs/scripts/css-parser-eval, walk.go): on the
// same stylesheets, both list the same rules, at-rules, declarations and
// problems, at the same bytes and depths. Opt-in: ARK_CSS_HARNESS_OUTLINE
// names the output of `csseval outline FILE...` (run.sh's WORKDIR) and
// ARK_CSS_HARNESS_DIRS the directories of those files.
func TestHarnessAgreement(t *testing.T) {
	list := os.Getenv("ARK_CSS_HARNESS_OUTLINE")
	if list == "" {
		t.Skip("set ARK_CSS_HARNESS_OUTLINE to the output of csseval outline")
	}
	fh, err := os.Open(list)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	want := map[string][]string{}
	cur := ""
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "== ") {
			cur = strings.TrimPrefix(l, "== ")
			want[cur] = []string{}
			continue
		}
		if l == "" {
			continue
		}
		want[cur] = append(want[cur], harnessLine(l))
	}
	files, n, items := 0, 0, 0
	for _, dir := range strings.Fields(os.Getenv("ARK_CSS_HARNESS_DIRS")) {
		ms, _ := filepath.Glob(filepath.Join(dir, "*.css"))
		for _, f := range ms {
			w, ok := want[filepath.Base(f)]
			if !ok {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			files++
			got := outline(src)
			items += len(got)
			if strings.Join(got, "\n") != strings.Join(w, "\n") {
				n++
				if n <= 5 {
					t.Errorf("%s\nreader  %q\nharness %q", f, first(got, w), first(w, got))
				}
			}
		}
	}
	if files == 0 {
		t.Fatal("no file of ARK_CSS_HARNESS_DIRS is in the outline")
	}
	t.Logf("%d files, %d items, %d differing files", files, items, n)
}

// first returns the first line of a where a and b differ.
func first(a, b []string) string {
	for i := range a {
		if i >= len(b) || a[i] != b[i] {
			return a[i]
		}
	}
	return "(end)"
}

// harnessLine reduces a line of csseval outline to "depth kind name [s,e)":
// a problem "!x-y" is the reader's "x_y" (the harness's "!encoding utf-16"
// is "encoding", "!encoding <label>" "charset").
func harnessLine(l string) string {
	depth := (len(l) - len(strings.TrimLeft(l, " "))) / 2
	l = strings.TrimLeft(l, " ")
	sp := strings.Index(l, " [")
	head, rest := l[:sp], l[sp+1:]
	span := rest[:strings.Index(rest, ")")+1]
	switch {
	case strings.HasPrefix(head, "!encoding utf-16"):
		head = "!encoding"
	case strings.HasPrefix(head, "!encoding "):
		head = "!charset"
	}
	head = strings.ReplaceAll(head, "-", "_")
	if strings.HasPrefix(head, "@") {
		head = strings.ReplaceAll(l[:sp], "-", "-") // at-rule names keep their dashes
	}
	if strings.HasPrefix(head, "decl ") {
		head = "decl " + strings.TrimPrefix(l[:sp], "decl ")
	}
	return strconv.Itoa(depth) + " " + head + " " + span
}

// outline lists the reader's items as harnessLine does the harness's.
func outline(src []byte) []string {
	var out []string
	for _, it := range read(src).items {
		head := ""
		switch it.kind {
		case itemRule:
			head = "rule"
		case itemAtRule:
			head = "@" + it.name
		case itemDecl:
			head = "decl " + it.name
		case itemProblem:
			head = "!" + it.name
		}
		out = append(out, strconv.Itoa(it.depth)+" "+head+" ["+strconv.Itoa(it.start)+","+strconv.Itoa(it.end)+")")
	}
	return out
}
