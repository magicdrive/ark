package index_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/symbol"
)

// Symbol.Parent is an identity: the SymbolID of the declaration the provider
// names as the enclosing symbol, or empty when that declaration cannot be
// singled out. It used to be a hash over (file, KindUnknown, parent name) —
// an ID no symbol has.

// checkParents asserts the hierarchy invariants over every symbol of idx and
// returns the number of symbols with a parent.
func checkParents(t *testing.T, idx *index.RepositoryIndex) int {
	t.Helper()
	withParent := 0
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			if s.Parent == "" {
				continue
			}
			withParent++
			p, ok := idx.GetSymbol(s.Parent)
			switch {
			case !ok:
				t.Errorf("%s: dangling parent %s", s.Qualified, s.Parent)
				continue
			case s.Parent == s.ID:
				t.Errorf("%s is its own parent", s.Qualified)
			case p.Location.File != s.Location.File || p.Language != s.Language:
				t.Errorf("%s: parent %s in %s/%s", s.Qualified, p.Qualified, p.Location.File, p.Language)
			case p.Qualified != s.ParentQualified:
				t.Errorf("%s: parent is %s, the provider named %s", s.Qualified, p.Qualified, s.ParentQualified)
			case p.Location.Range.Start.Line > s.Location.Range.Start.Line || p.Location.Range.End.Line < s.Location.Range.End.Line:
				t.Errorf("%s: parent %s does not enclose it", s.Qualified, p.Qualified)
			}
			if p.ID == symbol.NewSymbolID(s.Language, string(s.Location.File), symbol.KindUnknown, s.ParentQualified) {
				t.Errorf("%s: fabricated KindUnknown parent ID", s.Qualified)
			}
			// No cycle: the chain ends within the symbol count.
			seen := map[symbol.SymbolID]bool{s.ID: true}
			for cur := p; cur.Parent != ""; {
				if seen[cur.Parent] {
					t.Errorf("%s: parent chain cycles", s.Qualified)
					break
				}
				seen[cur.Parent] = true
				next, ok := idx.GetSymbol(cur.Parent)
				if !ok {
					break
				}
				cur = next
			}
		}
	}
	return withParent
}

func TestParentIdentity_TypeScriptClassMembers(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"svc.ts": "class UserService {\n  getUser() {\n    return this.loadUser();\n  }\n\n  loadUser() {\n    return null;\n  }\n}\n",
	})
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	cls := symbolIn(t, idx, "svc.ts", "UserService")
	for _, m := range []string{"UserService.getUser", "UserService.loadUser"} {
		s := symbolIn(t, idx, "svc.ts", m)
		if s.Parent != cls.ID || s.ParentQualified != "UserService" {
			t.Errorf("%s: Parent %q (%q), want %s", m, s.Parent, s.ParentQualified, cls.ID)
		}
		if p, ok := idx.GetSymbol(s.Parent); !ok || p.Kind != symbol.KindClass || p.Location != cls.Location {
			t.Errorf("%s: parent round trip gave %+v", m, p)
		}
	}
	if cls.Parent != "" {
		t.Errorf("top-level class has parent %s", cls.Parent)
	}
	// SymbolIDs are unchanged by parent resolution.
	if cls.ID != symbol.NewSymbolID("typescript", "svc.ts", symbol.KindClass, "UserService") {
		t.Error("the class's SymbolID changed")
	}
	// The call graph is not the hierarchy: getUser still calls loadUser.
	get := symbolIn(t, idx, "svc.ts", "UserService.getUser")
	if got := names(idx, idx.GetCallees(get.ID)); !reflect.DeepEqual(got, []string{"UserService.loadUser@6"}) {
		t.Errorf("callees of getUser: %v", got)
	}
	checkParents(t, idx)
}

func TestParentIdentity_GoInitsHaveNoParent(t *testing.T) {
	root := writeFiles(t, map[string]string{"go.mod": "module x\n\ngo 1.22\n", "boot.go": "package example\n\nfunc init() {}\n\nfunc init() {}\n"})
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	inits := declarations(idx, "boot.go", "init")
	if len(inits) != 2 || inits[0].ID == inits[1].ID {
		t.Fatalf("want two distinct init declarations: %+v", inits)
	}
	if inits[0].ID != symbol.NewSymbolID("go", "boot.go", symbol.KindFunction, "init") {
		t.Error("the first init's SymbolID changed")
	}
	for _, s := range inits {
		if s.Parent != "" || s.ParentQualified != "" {
			t.Errorf("init@%d has parent %q/%q", s.Location.Range.Start.Line, s.Parent, s.ParentQualified)
		}
	}
}

// Every provider that states parents, over its typical constructs: each
// stated parent resolves to the real enclosing declaration or stays empty,
// and the provider's statement (ParentQualified) is kept.
func TestParentIdentity_AllLanguages(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"go.mod":       "module x\n\ngo 1.22\n",
		"a.go":         "package x\ntype T struct{ F int }\nfunc (T) M() {}\nfunc init() {}\nfunc init() {}\n",
		"a.ts":         "export interface I { a: string; m(): void }\nexport class C { p = 1; m() {} static s() {} }\nexport namespace N { export const x = 1 }\nexport function o(a: string): void;\nexport function o(a: any) {}\n",
		"a.tsx":        "export class V { render() { return null } }\n",
		"a.js":         "class K { m() {} }\nfunction g() {}\nfunction g() {}\n",
		"a.py":         "class A:\n    def m(self):\n        pass\n\ndef f():\n    return 1\n\ndef f():\n    return 2\n",
		"a.php":        "<?php\nnamespace App;\ninterface J { public function j(); }\ntrait Tr { public function t() {} }\nenum E { case A; }\nclass Cmd\n{\n    const K = 1;\n    protected $cache;\n    public function cache() {}\n}\n",
		"m/main.tf":    "variable \"v\" {}\nresource \"aws_vpc\" \"main\" {}\nmodule \"child\" { source = \"./child\" }\ncheck \"ami\" {\n  data \"aws_ami\" \"latest\" {\n    owners = [\"self\"]\n  }\n}\n",
		"m/child/c.tf": "output \"o\" { value = 1 }\n",
	})
	idx, err := index.New(context.Background(), root, languages.Registry().Providers())
	if err != nil {
		t.Fatal(err)
	}
	if n := checkParents(t, idx); n == 0 {
		t.Fatal("no symbol got a parent")
	}
	want := map[string]string{ // child → its parent's qualified name ("" = none)
		"C.m": "C", "C.p": "C", "C.s": "C", "I.a": "I", "I.m": "I", "V.render": "V",
		`App\Cmd.K`: `App\Cmd`, `App\Cmd.cache`: `App\Cmd`, `App\J.j`: `App\J`, `App\Tr.t`: `App\Tr`, `App\E.A`: `App\E`,
		"T.M": "", "init": "", "f": "", "g": "",
		// Terraform: a data source scoped to a check block has the block as
		// its parent; top-level blocks have none.
		"m/check.ami/data.aws_ami.latest": "m/check.ami", "m/check.ami": "", "m/var.v": "", "m/aws_vpc.main": "",
	}
	got := map[string]string{}
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			if _, ok := want[s.Qualified]; !ok {
				continue
			}
			pq := ""
			if p, ok := idx.GetSymbol(s.Parent); ok {
				pq = p.Qualified
			}
			if prev, dup := got[s.Qualified]; dup && prev != pq {
				t.Errorf("%s: namesakes got different parents %q / %q", s.Qualified, prev, pq)
			}
			got[s.Qualified] = pq
		}
	}
	for child, parent := range want {
		if g, ok := got[child]; !ok {
			t.Errorf("%s not extracted", child)
		} else if g != parent {
			t.Errorf("%s: parent %q, want %q", child, g, parent)
		}
	}
}

func member(name, parent string, kind symbol.SymbolKind, start, end uint32) language.SymbolDraft {
	d := draftAt(name, kind, start, end)
	d.Parent = parent
	if parent != "" {
		d.Qualified = parent + "." + name
	}
	return d
}

func parentsByLine(t *testing.T, syms []language.SymbolDraft) map[string]string {
	t.Helper()
	idx := buildFakeIndex(t, fakeProvider{symbols: syms})
	out := map[string]string{}
	for _, s := range idx.SymbolsByFile("x.fake") {
		key := declKey(s)
		out[key] = ""
		if p, ok := idx.GetSymbol(s.Parent); ok {
			out[key] = declKey(p)
		}
	}
	checkParents(t, idx)
	return out
}

// declKey names a declaration by qualified name, kind and range:
// "C:class@1" for one line, "C:class@1-5" otherwise.
func declKey(s symbol.Symbol) string {
	r := s.Location.Range
	if r.End.Line == r.Start.Line {
		return fmt.Sprintf("%s:%s@%d", s.Qualified, s.Kind, r.Start.Line)
	}
	return fmt.Sprintf("%s:%s@%d-%d", s.Qualified, s.Kind, r.Start.Line, r.End.Line)
}

func TestParentIdentity_Resolution(t *testing.T) {
	for _, c := range []struct {
		name string
		syms []language.SymbolDraft
		want map[string]string
	}{
		{"unique enclosing parent", []language.SymbolDraft{
			draftAt("C", symbol.KindClass, 1, 5), member("m", "C", symbol.KindMethod, 2, 3)},
			map[string]string{"C:class@1-5": "", "C.m:method@2-3": "C:class@1-5"}},
		{"namesake parents told apart by enclosure", []language.SymbolDraft{
			draftAt("C", symbol.KindClass, 1, 4), draftAt("C", symbol.KindClass, 6, 9),
			member("a", "C", symbol.KindMethod, 2, 3), member("b", "C", symbol.KindMethod, 7, 8)},
			map[string]string{"C:class@1-4": "", "C:class@6-9": "", "C.a:method@2-3": "C:class@1-4", "C.b:method@7-8": "C:class@6-9"}},
		{"several enclosing namesakes: unknown", []language.SymbolDraft{
			draftAt("C", symbol.KindClass, 1, 9), draftAt("C", symbol.KindNamespace, 1, 9),
			member("m", "C", symbol.KindMethod, 2, 3)},
			map[string]string{"C:class@1-9": "", "C:namespace@1-9": "", "C.m:method@2-3": ""}},
		{"named parent does not enclose: unknown", []language.SymbolDraft{
			draftAt("C", symbol.KindClass, 1, 2), member("m", "C", symbol.KindMethod, 5, 6)},
			map[string]string{"C:class@1-2": "", "C.m:method@5-6": ""}},
		{"named parent does not exist: unknown", []language.SymbolDraft{
			member("m", "Missing", symbol.KindMethod, 2, 3)},
			map[string]string{"Missing.m:method@2-3": ""}},
		{"no self parent", []language.SymbolDraft{
			func() language.SymbolDraft { d := draftAt("C", symbol.KindClass, 1, 3); d.Parent = "C"; return d }()},
			map[string]string{"C:class@1-3": ""}},
		{"parent cycle: unknown", []language.SymbolDraft{
			func() language.SymbolDraft { d := draftAt("A", symbol.KindClass, 1, 3); d.Parent = "B"; return d }(),
			func() language.SymbolDraft { d := draftAt("B", symbol.KindClass, 1, 3); d.Parent = "A"; return d }(),
			member("m", "A", symbol.KindMethod, 2, 2)},
			map[string]string{"A:class@1-3": "", "B:class@1-3": "", "A.m:method@2": "A:class@1-3"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := parentsByLine(t, c.syms); !reflect.DeepEqual(got, c.want) {
				t.Errorf("parents:\n got %v\nwant %v", got, c.want)
			}
			reversed := slices.Clone(c.syms)
			slices.Reverse(reversed)
			if got := parentsByLine(t, reversed); !reflect.DeepEqual(got, c.want) {
				t.Errorf("emission order changed parents:\n got %v\nwant %v", got, c.want)
			}
		})
	}
}

// Parents are recomputed from cached drafts: cold, warm and uncached agree.
func TestParentIdentity_ColdAndWarmCacheAgree(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"svc.ts": "export class UserService {\n  getUser() { return this.loadUser() }\n  loadUser() { return null }\n}\n",
		"a.php":  "<?php\nnamespace App;\nclass Cmd { protected $cache; public function cache() {} }\n",
	})
	providers := languages.Registry().Providers()
	parents := func(idx *index.RepositoryIndex) map[symbol.SymbolID]symbol.SymbolID {
		out := map[symbol.SymbolID]symbol.SymbolID{}
		for _, f := range idx.Files() {
			for _, s := range idx.SymbolsByFile(f) {
				out[s.ID] = s.Parent
			}
		}
		return out
	}
	plain, err := index.New(context.Background(), root, providers)
	if err != nil {
		t.Fatal(err)
	}
	want := parents(plain)
	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"cold", "warm"} {
		idx, err := index.NewWithCache(context.Background(), root, providers, store)
		if err != nil {
			t.Fatal(err)
		}
		if got := parents(idx); !reflect.DeepEqual(got, want) {
			t.Errorf("%s cache: parents differ", run)
		}
	}
}

// FuzzParentIdentity: arbitrary layouts of named parents and ranges keep the
// hierarchy invariants and do not depend on emission order.
func FuzzParentIdentity(f *testing.F) {
	f.Add([]byte{0, 1, 9, 0, 1, 2, 3, 1, 2, 4, 5, 1})
	f.Add([]byte{0, 0, 3, 1, 0, 3, 2, 1, 1})
	f.Fuzz(func(t *testing.T, layout []byte) {
		if len(layout) > 48 {
			layout = layout[:48]
		}
		names := []string{"A", "B", "C"}
		var drafts []language.SymbolDraft
		for i := 0; i+2 < len(layout); i += 3 {
			start := uint32(layout[i+1]%12) + 1
			end := start + uint32(layout[i+2]%6)
			d := draftAt(names[int(layout[i])%3], []symbol.SymbolKind{symbol.KindClass, symbol.KindNamespace}[int(layout[i]/3)%2], start, end)
			if p := int(layout[i]/6) % 4; p < 3 {
				d.Parent = names[p]
			}
			drafts = append(drafts, d)
		}
		// Keyed by SymbolID, a function of each declaration's content, so
		// declarations sharing a name, kind and range stay apart.
		parentsByID := func(ds []language.SymbolDraft) map[symbol.SymbolID]symbol.SymbolID {
			idx := buildFakeIndex(t, fakeProvider{symbols: ds})
			checkParents(t, idx)
			out := map[symbol.SymbolID]symbol.SymbolID{}
			for _, s := range idx.SymbolsByFile("x.fake") {
				out[s.ID] = s.Parent
			}
			return out
		}
		want := parentsByID(drafts)
		reversed := slices.Clone(drafts)
		slices.Reverse(reversed)
		if got := parentsByID(reversed); !reflect.DeepEqual(got, want) {
			t.Fatalf("emission order changed parents:\n got %v\nwant %v", got, want)
		}
	})
}
