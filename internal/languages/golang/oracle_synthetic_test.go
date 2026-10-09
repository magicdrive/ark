package golang_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A synthetic module whose every call has one known meaning, checked against
// the go/types oracle: Ark must never claim a Strong/Exact target the type
// checker contradicts, and must resolve the calls whose evidence is local
// and syntactic (same package, imports, aliases, dot imports, proven
// receivers). Sites marked `// want:<qualified>` must resolve to that
// declaration; every other Strong answer must agree with the oracle.
var syntheticModule = map[string]string{
	"go.mod": "module example.com/syn\n\ngo 1.22\n",
	"a/a.go": `package a

func Helper()  {}
func Unique()  {}
func Shared()  {}

type Svc struct{}

func (Svc) Run()   {}
func (*Svc) Stop() {}

type Runner interface{ Run() }

type Base struct{}

func (Base) Hello() {}

type Wrap struct{ Base }

func Map[T any](x T) T { return x }

type ID int

type cancel struct{}

func init() {}
func init() { Helper() } // want:Helper
`,
	"b/b.go": `package b

func Helper() {}
func Shared() {}
`,
	"d/d.go": `package d

func DFunc() {}

// len shadows the builtin inside package d.
func len(s string) int { return 0 }

func useLen() int { return len("x") } // want:len
`,
	"e/e.go": `package e

func init() {}
`,
	"internal/event/event.go": `package event

func Log() {}
`,
	"event/event.go": `package event

func Log() {}
`,
	"c/c.go": `package c

import (
	"strings"

	"example.com/syn/a"
	al "example.com/syn/b"
	. "example.com/syn/d"
	_ "example.com/syn/e"
	"example.com/syn/internal/event"
)

func local() {}

func Exported() {}

type T1 struct{}

func (T1) Do() {}

type T2 struct{}

func (T2) Do() {}

func use(fn func()) {
	a.Helper()  // want:Helper@a
	al.Helper() // want:Helper@b
	local()     // want:local
	event.Log() // want:Log@internal/event
	cancel := func() {}
	cancel()
	f := a.Unique
	f()
	fn()
	var s a.Svc
	s.Run()
	var r a.Runner
	r.Run()
	var w a.Wrap
	w.Hello()
	_ = a.Map(1)    // want:Map
	_ = a.ID(3)     // want:ID
	_ = len("x")
	_ = append([]int{}, 1)
	func() {}()
	DFunc()                  // want:DFunc
	_ = strings.ToUpper("x")
	_ = &a.Svc{}             // want:Svc
	t := T1{}
	t.Do() // want:T1.Do
	g := t.Do
	g()
}

// A local declared in a nested block shadows nothing at the call outside it,
// but declarations are counted per function: the call stays a Candidate
// (a missed edge, never a wrong one).
func blockShadow() {
	if true {
		local := 1
		_ = local
	}
	local()
}
`,
	"c/c_internal_test.go": `package c

func helperForTests() { local() } // want:local
`,
	"c/c_external_test.go": `package c_test

import "example.com/syn/c"

func local2() {}

func useExternal() {
	c.Exported() // want:Exported
	local2()     // want:local2
}
`,
	"c/tagged.go": `//go:build ignore

package c

func onlyTagged() { local() }
`,
}

func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestGoOracle_SyntheticModule(t *testing.T) {
	root := writeModule(t, syntheticModule)
	rep, err := compareWithOracle(root, 50)
	if err != nil {
		t.Fatal(err)
	}
	if rep.StrongFP != 0 {
		t.Errorf("Strong answers the type checker contradicts:\n%s", rep)
	}

	// Every `// want:` site resolves to that declaration.
	ark, syms, err := arkGo(root)
	if err != nil {
		t.Fatal(err)
	}
	for file, src := range syntheticModule {
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		for i, line := range strings.Split(src, "\n") {
			_, want, ok := strings.Cut(line, "// want:")
			if !ok {
				continue
			}
			want = strings.TrimSpace(want)
			wantName, wantDir, _ := strings.Cut(want, "@")
			var got []string
			found := false
			for k, a := range ark {
				f, rest, _ := strings.Cut(string(k), ":")
				if f != file || !strings.HasPrefix(rest, strconv.Itoa(i+1)+":") {
					continue
				}
				for sf, ss := range syms {
					for _, s := range ss {
						if s.ID == a.target {
							got = append(got, s.Qualified+"@"+filepath.Dir(sf))
							if s.Qualified == wantName && (wantDir == "" || filepath.Dir(sf) == wantDir) {
								found = true
							}
						}
					}
				}
				if a.target == "" {
					got = append(got, string(rune('0'+int(a.conf)))+":"+a.rule)
				}
			}
			if !found {
				t.Errorf("%s:%d %q: want %s, Ark answered %v", file, i+1, strings.TrimSpace(line), want, got)
			}
		}
	}
}
