package typescript

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// TestFidelity_TypeScriptCompiler compares Ark's call, construction and type
// references with what the TypeScript compiler's parser sees (and its binder,
// for type parameters), over the .ts / .tsx files under the directories
// ARK_TS_FIDELITY_ROOTS lists. It needs Node.js and the typescript package
// (ARK_TYPESCRIPT_MODULE: the package directory), so it is skipped unless
// both are given; Ark itself never needs either.
//
// A reference only Ark has is a false reference and fails the test; a
// reference Ark lacks is logged (Ark observes a subset of the language:
// see internal/conformance/IMPROVEMENTS.md).
func TestFidelity_TypeScriptCompiler(t *testing.T) {
	roots, tsmod := os.Getenv("ARK_TS_FIDELITY_ROOTS"), os.Getenv("ARK_TYPESCRIPT_MODULE")
	if roots == "" || tsmod == "" {
		t.Skip("ARK_TS_FIDELITY_ROOTS / ARK_TYPESCRIPT_MODULE not set")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "fidelity", "tsrefs.js"))
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, root := range filepath.SplitList(roots) {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if !d.IsDir() && (strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx")) && !strings.HasSuffix(p, ".d.ts") {
				files = append(files, p)
			}
			return nil
		})
	}
	dir := t.TempDir()
	list, out := filepath.Join(dir, "files.txt"), filepath.Join(dir, "tsc.jsonl")
	if err := os.WriteFile(list, []byte(strings.Join(files, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command(node, script, tsmod, list, out).CombinedOutput(); err != nil {
		t.Fatalf("tsrefs.js: %v\n%s", err, b)
	}

	type key struct {
		kind, name, recv string
		line, col        uint32
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	compared, ok := 0, 0
	falseRefs, missing := map[string][]string{}, map[string]int{}
	for sc.Scan() {
		var row struct {
			File   string  `json:"file"`
			Errors int     `json:"errors"`
			Refs   [][]any `json:"refs"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if row.Errors > 0 {
			continue // not valid TypeScript: nothing to compare with
		}
		src, err := os.ReadFile(row.File)
		if err != nil {
			t.Fatal(err)
		}
		p := NewProvider()
		if strings.HasSuffix(row.File, ".tsx") {
			p = NewTSXProvider()
		}
		ex, err := p.Extract(context.Background(), source.FileID(row.File), src)
		if err != nil {
			t.Fatal(err)
		}
		compared++
		want := map[key]int{}
		names := map[key]bool{} // kind, name and position, receiver text aside
		typeParams := map[key]bool{}
		for _, r := range row.Refs {
			k := key{r[0].(string), r[1].(string), r[2].(string), uint32(r[3].(float64)), uint32(r[4].(float64))}
			if k.kind == "type_parameter" {
				typeParams[key{"type_use", k.name, "", k.line, k.col}] = true
				continue
			}
			want[k]++
			names[key{k.kind, k.name, "", k.line, k.col}] = true
		}
		got := map[key]int{}
		gotNames := map[key]bool{}
		for _, r := range ex.References {
			switch r.Kind {
			case "call", "construction", "type_use":
				at := r.Location.Range.Start
				got[key{r.Kind, r.Name, r.ReceiverExpr, at.Line, at.Column}]++
				gotNames[key{r.Kind, r.Name, "", at.Line, at.Column}] = true
			}
		}
		for k, n := range got {
			bare := key{k.kind, k.name, "", k.line, k.col}
			switch {
			case n <= want[k]:
				ok += n
			case typeParams[bare]:
				falseRefs["type parameter recorded as a type use"] = append(falseRefs["type parameter recorded as a type use"], fmt.Sprintf("%s:%d:%d %s", row.File, k.line, k.col, k.name))
			case names[bare]:
				// Same name at the same place; the receiver is kept verbatim from
				// a different tree: not a different target.
				ok += n
			default:
				falseRefs[k.kind] = append(falseRefs[k.kind], fmt.Sprintf("%s:%d:%d %s.%s", row.File, k.line, k.col, k.recv, k.name))
			}
		}
		for k, n := range want {
			if got[k] < n && !gotNames[key{k.kind, k.name, "", k.line, k.col}] {
				missing[k.kind] += n - got[k]
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d files compared, %d references agree, missing: %v", compared, ok, missing)
	kinds := make([]string, 0, len(falseRefs))
	for k := range falseRefs {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		ex := falseRefs[k]
		if len(ex) > 5 {
			ex = ex[:5]
		}
		t.Errorf("%d false %s references, e.g. %v", len(falseRefs[k]), k, ex)
	}
}
