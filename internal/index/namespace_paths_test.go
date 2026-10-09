package index_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
)

// A TSX file and TypeScript share a name space however their FileIndex is
// built: the index builder, the cached builder and index.NewFileIndex give
// one resolution. app.tsx imports tsUtil (a module binding) and names
// tsHelper without an import (a name-based stage); JavaScript and Go declare
// namesakes, which no path may offer.
var tsxRepo = map[string]string{
	"go.mod": "module example.com/tsx\n\ngo 1.22\n",
	"web/util.ts": `export function tsUtil(): string {
    return "ok";
}
export function tsHelper(): string { return "h" }
`,
	"web/app.tsx": `import { tsUtil } from "./util";

export function App() {
    return <div>{tsUtil()}{tsHelper()}</div>;
}
`,
	"web/legacy.js": "function tsUtil() { return 1 }\nfunction tsHelper() { return 2 }\n",
	"g/g.go":        "package g\n\nfunc tsUtil() {}\n\nfunc tsHelper() {}\n",
}

// relationsOf renders App's outgoing relations as "target confidence".
func appRelations(t *testing.T, idx *index.RepositoryIndex) []string {
	t.Helper()
	app := symbolIn(t, idx, "web/app.tsx", "App")
	var out []string
	for _, e := range idx.GetCallees(app.ID) {
		s, _ := idx.GetSymbol(e.To)
		out = append(out, fmt.Sprintf("%s#%s %s", s.Location.File, s.Qualified, e.Confidence))
	}
	for _, c := range idx.CandidateCalleeSample(app.ID).Relations {
		s, _ := idx.GetSymbol(c.Symbol)
		out = append(out, fmt.Sprintf("%s#%s %s", s.Location.File, s.Qualified, c.Confidence))
	}
	sort.Strings(out)
	return out
}

// directRelations resolves FileIndexes built by index.NewFileIndex and
// renders App's references the same way.
func directRelations(t *testing.T, root string) []string {
	t.Helper()
	reg := languages.Registry()
	var files []resolver.FileIndex
	for rel := range tsxRepo {
		d, ok := reg.DetectByExtension(filepath.Ext(rel))
		if !ok {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		ex, err := d.Provider.Extract(context.Background(), source.FileID(rel), src)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, index.NewFileIndex(d.Provider, source.FileID(rel), ex))
	}
	byID := map[string]string{}
	for _, fi := range files {
		for _, s := range fi.Symbols {
			byID[string(s.ID)] = fmt.Sprintf("%s#%s", s.Location.File, s.Qualified)
		}
	}
	var app []string
	r := resolver.NewInRoot(files, filepath.Base(root))
	for _, fi := range files {
		if fi.FileID != "web/app.tsx" {
			continue
		}
		for _, ref := range fi.References {
			if ref.Container != "App" {
				continue
			}
			res := r.ResolveReference(ref, fi)
			for _, c := range res.Candidates {
				app = append(app, fmt.Sprintf("%s %s", byID[string(c.SymbolID)], res.Confidence))
			}
		}
	}
	sort.Strings(app)
	return app
}

func TestNameSpaces_SameForEveryFileIndexPath(t *testing.T) {
	root := writeFiles(t, tsxRepo)
	providers := languages.Registry().Providers()
	built, err := index.New(context.Background(), root, providers)
	if err != nil {
		t.Fatal(err)
	}
	want := appRelations(t, built)
	// The builder: TSX → TypeScript only — the import (Exact) and the
	// unimported name in the same directory (a Candidate: app.tsx is a module).
	if exp := []string{"web/util.ts#tsHelper candidate", "web/util.ts#tsUtil exact"}; !reflect.DeepEqual(want, exp) {
		t.Fatalf("builder: %q, want %q", want, exp)
	}
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"cold", "warm"} {
		idx, err := index.NewWithCache(context.Background(), root, providers, store)
		if err != nil {
			t.Fatal(err)
		}
		if got := appRelations(t, idx); !reflect.DeepEqual(got, want) {
			t.Errorf("%s cache: %q, want %q", run, got, want)
		}
	}
	if got := directRelations(t, root); !reflect.DeepEqual(got, want) {
		t.Errorf("index.NewFileIndex: %q, want %q (the builder's)", got, want)
	}
}
