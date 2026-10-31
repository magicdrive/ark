package yaml

import (
	"context"
	"reflect"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtractYAML: on any input, as a workflow, an action and a data file,
// extraction never panics or fails, every range lies within the source and
// is ordered, a symbol's bytes are its location's, every reference's
// container is a declaration of the file, and a second extraction is the
// same.
func FuzzExtractYAML(f *testing.F) {
	for _, s := range []string{
		"on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: actions/checkout@v4\n      - uses: ./.github/actions/x\n",
		"on: [push]\njobs:\n  a: &j {runs-on: x, steps: &s [{run: x}]}\n  b: {needs: [a], steps: *s}\n  c: *j\n",
		"runs:\n  using: composite\n  steps:\n    - uses: ./x\n",
		"a: &a [*a]\nb: *a\n<<: *a\n",
		"%YAML 1.2\n---\na: 1\n...\n---\nb: |\n  x\n  # y\n\n# z\nc: 'q''q'\n",
		"k: \"é\u2028\"\r\nv: \xEF\xBB\xBF\n",
		"? [a, b]\n: c\n!t d: !!str e\n",
		"a:\n\t- b\n",
	} {
		f.Add([]byte(s))
	}
	p := NewProvider()
	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 1<<16 {
			return
		}
		for _, file := range []string{".github/workflows/w.yml", ".github/actions/a/action.yml", "data.yaml"} {
			ex, err := p.Extract(context.Background(), source.FileID(file), src)
			if err != nil {
				t.Fatal(err)
			}
			qualified := map[string]bool{}
			for _, s := range ex.Symbols {
				qualified[s.Qualified] = true
				if int(s.StartByte) > int(s.EndByte) || int(s.EndByte) > len(src) {
					t.Fatalf("%s: symbol %s at [%d,%d) of %d bytes", file, s.Qualified, s.StartByte, s.EndByte, len(src))
				}
				if s.Location.Range.Start != (lines(newLines(src))).pos(int(s.StartByte)) {
					t.Fatalf("%s: symbol %s location %v is not its bytes'", file, s.Qualified, s.Location.Range)
				}
				if s.Name == "" && s.Kind != "key" {
					t.Fatalf("%s: unnamed %s", file, s.Kind)
				}
			}
			for _, r := range ex.References {
				rg := r.Location.Range
				if rg.Start.Line > rg.End.Line || rg.Start.Line == rg.End.Line && rg.Start.Column > rg.End.Column {
					t.Fatalf("%s: reference %s range %v", file, r.Name, rg)
				}
				if r.Container != "" && !qualified[r.Container] {
					t.Fatalf("%s: reference %s in %q, no declaration of the file", file, r.Name, r.Container)
				}
			}
			for _, d := range ex.Diagnostics {
				if d.Code == "" {
					t.Fatalf("%s: diagnostic without a code: %s", file, d.Message)
				}
			}
			again, _ := p.Extract(context.Background(), source.FileID(file), src)
			if !reflect.DeepEqual(ex, again) {
				t.Fatalf("%s: two extractions differ", file)
			}
		}
	})
}
