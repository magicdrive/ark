package html_test

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/symbol"
)

// TestHarnessAgreement compares the provider with the independent
// implementation of its rules in docs/scripts/html-parser-eval (attrlib.go):
// on the same files, the elements the provider states are the verified id
// attributes the harness lists, less the ids the provider refuses (empty,
// whitespace, template syntax) and those the tree gives more elements than
// are listed (a hidden duplicate). Opt-in: ARK_HTML_HARNESS_IDS names the
// output of `ctxcheck ids DIR/*.html...` and ARK_HTML_HARNESS_DIRS the
// directories, both relative to the test's working directory (cd into
// run.sh's WORKDIR via absolute paths: the files are read where listed).
func TestHarnessAgreement(t *testing.T) {
	list := os.Getenv("ARK_HTML_HARNESS_IDS")
	if list == "" {
		t.Skip("set ARK_HTML_HARNESS_IDS to the output of ctxcheck ids")
	}
	fh, err := os.Open(list)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	type listed struct {
		offset, id string
		inTree     int
	}
	byFile := map[string][]listed{}
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 4)
		if len(parts) != 4 {
			t.Fatalf("bad line %q", sc.Text())
		}
		id, err := strconv.Unquote(parts[2])
		if err != nil {
			t.Fatal(err)
		}
		if id == "" || strings.ContainsAny(id, " \t\n\f\r") || strings.Contains(id, "{{") ||
			strings.Contains(id, "{%") || strings.Contains(id, "<%") || strings.Contains(id, "<?") || strings.Contains(id, "${") {
			continue
		}
		n, err := strconv.Atoi(parts[3])
		if err != nil {
			t.Fatal(err)
		}
		byFile[parts[0]] = append(byFile[parts[0]], listed{parts[1], id, n})
	}
	want := map[string][]string{} // file → "offset id"
	for f, ls := range byFile {
		stated := map[string]int{}
		for _, l := range ls {
			stated[l.id]++
		}
		for _, l := range ls {
			if l.inTree <= stated[l.id] {
				want[f] = append(want[f], l.offset+" "+l.id)
			}
		}
	}
	// Every HTML file of ARK_HTML_HARNESS_DIRS (by the path ctxcheck was
	// given, relative to the working directory), so an element only the
	// provider states is seen too.
	var files []string
	for _, dir := range strings.Fields(os.Getenv("ARK_HTML_HARNESS_DIRS")) {
		m, _ := filepath.Glob(filepath.Join(dir, "*.html"))
		files = append(files, m...)
	}
	if len(files) == 0 {
		t.Fatal("set ARK_HTML_HARNESS_DIRS to the corpus directories ctxcheck ids read")
	}
	sort.Strings(files)
	n := 0
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, s := range extract(t, f, string(src)).Symbols {
			if s.Kind == symbol.KindElement {
				got = append(got, strconv.Itoa(int(s.StartByte))+" "+s.Name)
			}
		}
		w := want[f]
		sort.Strings(got)
		sort.Strings(w)
		if strings.Join(got, "\n") != strings.Join(w, "\n") {
			t.Errorf("%s\n provider %q\n harness  %q", f, got, w)
		}
		n += len(got)
	}
	t.Logf("%d files, %d elements agree", len(files), n)
}
