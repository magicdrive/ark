package index_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The oracle scoring of TestFidelityBaseline (docs/research/multilang-fidelity/
// evaluation.md): each case scores Ark's references of a small corpus
// against hand-written oracle sites and checks the counts it must give.
func TestFidelityScore(t *testing.T) {
	py := writeFiles(t, map[string]string{
		"pkg/__init__.py": "",
		"pkg/util.py": "def helper():\n" + // 1
			"    return 1\n" + // 2
			"def result(x):\n" + // 3
			"    result = helper\n" + // 4
			"    return result()\n", // 5
		"pkg/main.py": "import copy\n" + // 1
			"def helper():\n" + // 2
			"    return 0\n" + // 3
			"def run(helper):\n" + // 4
			"    helper()\n" + // 5
			"    copy.copy([1])\n", // 6
	})
	ts := writeFiles(t, map[string]string{
		"base.ts": "export function other() { return 2 }\n",
		"util.ts": "export function helper() { return 1 }\n",
		"main.ts": "import { helper as h } from './util'\n" + // 1
			"export function run() { return h() }\n", // 2
	})

	site := func(file, name string, line, col uint32, rest string) string {
		return `{"file":"` + file + `","name":"` + name + `","start_line":` + itoa(line) + `,"start_col":` + itoa(col) +
			`,"end_line":` + itoa(line) + `,"end_col":` + itoa(col+uint32(len(name))) + `,` + rest + `}`
	}
	cases := []struct {
		name   string
		root   string
		oracle []string
		want   map[string]int
		scored []string // lines report.scored must hold
	}{
		{
			// helper() in run denotes its parameter: Ark's same-file Exact
			// to the function helper is false.
			name:   "parameter shadowing is a local false Strong",
			root:   py,
			oracle: []string{site("pkg/main.py", "helper", 5, 5, `"class":"declaration","decl":{"file":"pkg/main.py","line":4,"col":9}`)},
			want:   map[string]int{"local_fp": 1, "representable": 0, "exact_tp": 0},
			scored: []string{"pkg/main.py:5:5:helper\tdeclaration\t-\tlocal_fp"},
		},
		{
			// result() denotes the local of line 4, inside the function
			// result: it is not the function.
			name:   "a local inside a function of its name is not the function",
			root:   py,
			oracle: []string{site("pkg/util.py", "result", 5, 12, `"class":"declaration","decl":{"file":"pkg/util.py","line":4,"col":5}`)},
			want:   map[string]int{"exact_tp": 0, "local_fp": 1},
		},
		{
			name: "one Ark reference is scored at one site",
			root: py,
			oracle: []string{
				site("pkg/main.py", "copy", 6, 5, `"class":"external"`),
				site("pkg/main.py", "copy", 6, 10, `"class":"external"`),
			},
			want: map[string]int{"external": 2, "external_fp": 0},
		},
		{
			name: "ambiguous: a target among none of the declarations is false",
			root: py,
			oracle: []string{site("pkg/main.py", "helper", 5, 5,
				`"class":"ambiguous","decls":[{"file":"pkg/main.py","line":4,"col":9},{"file":"pkg/util.py","line":1,"col":5}]`)},
			want: map[string]int{"ambiguous": 1, "ambiguous_fp": 1},
		},
		{
			name: "ambiguous: a target among the declarations is not counted",
			root: py,
			oracle: []string{site("pkg/main.py", "helper", 5, 5,
				`"class":"ambiguous","decls":[{"file":"pkg/main.py","line":2,"col":5},{"file":"pkg/util.py","line":1,"col":5}]`)},
			want: map[string]int{"ambiguous": 1, "ambiguous_fp": 0},
		},
		{
			name:   "declaration_file sites are counted apart",
			root:   py,
			oracle: []string{site("pkg/main.py", "helper", 5, 5, `"class":"declaration_file"`)},
			want:   map[string]int{"declaration_file": 1, "declaration_file_strong": 1, "local_fp": 0},
		},
		{
			name: "a duplicated oracle line is one site",
			root: py,
			oracle: []string{
				site("pkg/main.py", "helper", 5, 5, `"class":"declaration","decl":{"file":"pkg/main.py","line":4,"col":9}`),
				site("pkg/main.py", "helper", 5, 5, `"class":"declaration","decl":{"file":"pkg/main.py","line":4,"col":9}`),
			},
			want: map[string]int{"sites": 1, "local_fp": 1},
		},
		{
			// h is an alias of helper: the declaration is named helper, and
			// Ark's binding resolution to it is right.
			name:   "an alias import resolved to its declaration is a true Exact",
			root:   ts,
			oracle: []string{site("main.ts", "h", 2, 32, `"class":"declaration","decl":{"file":"util.ts","line":1,"col":17,"name":"helper"}`)},
			want:   map[string]int{"representable": 1, "recovered": 1, "exact_tp": 1, "local_fp": 0},
			scored: []string{"main.ts:2:32:h\tdeclaration\trepresentable\ttp"},
		},
		{
			// An unresolved site is in Series 1 only when representable: a
			// local's is not.
			name: "report.scored tells representable sites from the others",
			root: py,
			oracle: []string{
				site("pkg/main.py", "copy", 6, 10, `"class":"declaration","decl":{"file":"pkg/util.py","line":1,"col":5,"name":"helper"}`),
				site("pkg/main.py", "copy", 6, 5, `"class":"declaration","decl":{"file":"pkg/main.py","line":4,"col":9}`),
			},
			want: map[string]int{"representable": 1, "unresolved": 1},
			scored: []string{
				"pkg/main.py:6:10:copy\tdeclaration\trepresentable\tunresolved",
				"pkg/main.py:6:5:copy\tdeclaration\t-\t-",
			},
		},
		{
			name:   "module sites are counted apart",
			root:   py,
			oracle: []string{site("pkg/main.py", "helper", 5, 5, `"class":"module"`)},
			want:   map[string]int{"module": 1, "unknown_strong": 1, "local_fp": 0},
		},
		{
			// Ark resolves h to util.ts's helper, which the oracle says re-
			// exports base.ts's: a target the alias passes through stops
			// short of the declaration, neither found nor false.
			name: "a target the alias passes through is via",
			root: ts,
			oracle: []string{site("main.ts", "h", 2, 32,
				`"class":"declaration","decl":{"file":"base.ts","line":1,"col":17,"name":"helper"},"via":[{"file":"util.ts","line":1,"col":17,"name":"helper"}]`)},
			want: map[string]int{"representable": 0, "via_stop": 1, "exact_fp": 0, "strong_fp": 0, "local_fp": 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := measureCorpus(tc.root, nil)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "oracle.jsonl")
			lines := make([]string, len(tc.oracle))
			for i, l := range tc.oracle {
				lines[i] = strings.Replace(l, "{", `{"lang":"x",`, 1)
			}
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			score, _, perSite, err := scoreAgainstOracle(path, m.files, m.byEnd, m.byStart)
			if err != nil {
				t.Fatal(err)
			}
			got := scoreFields(score)
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %d, want %d\nscore: %v\nsites: %v\nark:\n%s", k, got[k], v, got, perSite, strings.Join(m.sites, "\n"))
				}
			}
			have := map[string]bool{}
			for _, l := range perSite {
				have[l] = true
			}
			for _, l := range tc.scored {
				if !have[l] {
					t.Errorf("report.scored lacks %q:\n%s", l, strings.Join(perSite, "\n"))
				}
			}
		})
	}
}

func itoa(n uint32) string { return strconv.FormatUint(uint64(n), 10) }

// scoreFields is the score by its JSON names.
func scoreFields(s *oracleScore) map[string]int {
	data, _ := json.Marshal(s)
	out := map[string]int{}
	_ = json.Unmarshal(data, &out)
	return out
}
