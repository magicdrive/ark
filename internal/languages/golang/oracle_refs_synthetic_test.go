package golang_test

import (
	"strings"
	"testing"
)

// A synthetic module for the reference-level oracle (oracle_refs_test.go):
// every use of a declaration — call, type use, value, write — checked
// against go/types. Ark must claim no Strong or Exact target and no
// OutsideRepository the type checker contradicts anywhere, must resolve the
// sites marked `// want:Name=Qualified[@dir]` (Name used on that line,
// resolved to that declaration), and must give no target to the sites
// marked `// none:Name` (a name that only looks like a declaration's: a
// type parameter, a local, a field, a key, a label, an interface method, an
// import shadowed by a variable).
var refSyntheticModule = map[string]string{
	"go.mod": "module example.com/syn\n\ngo 1.22\n",
	// The module's root package, imported by its module path (the checkout
	// directory is a temporary name, so no import path ends in it).
	"syn.go": `package syn

const Version = "1"

type Level int

func (l Level) String() string { return "" }
`,
	"a/a.go": `package a

import (
	"encoding/json"
	"math/rand/v2"
	"sync/atomic"

	"example.com/syn"
	"example.com/syn/b"
	u "example.com/syn/util"
	. "example.com/syn/dot"
	xlog "example.com/syn/x/log"
)

const Name = "n"

var counter int

var (
	first, second = 1, 2
	grouped       = Name // want:Name=Name
)

type node int

type T struct{}

type Alias = T

type graph map[string]int

func (g graph) addNode(n string) { g[n] = 1 }

type Svc struct{ count int }

func (Svc) Run() {}

type Runner interface{ Run() }

type Base struct{}

func (Base) Hello() {}

type Wrap struct{ Base }

type S struct{ Name string }

type Pair[K comparable, V any] struct {
	k K // none:K
	v V // none:V
}

func Map[X any](x X) X { return x }

func Generic[T any](x T) T { // none:T
	var zero T // none:T
	_ = zero
	return T(x) // none:T
}

func (p Pair[K, V]) Key() K { return p.k } // none:K

func uses(g graph, l syn.Level, s Svc, r Runner, w Wrap) {
	g.addNode("x")       // want:addNode=graph.addNode
	_ = l.String()       // a package-qualified receiver type is not proven: Candidate
	_ = syn.Version      // want:Version=Version@.
	_ = u.Const          // want:Const=Const@util
	_ = DotConst         // want:DotConst=DotConst@dot
	_ = b.Shared         // want:Shared=Shared@b
	xlog.Print()         // want:Print=Print@x/log
	_ = map[string]int{Name: 1} // want:Name=Name
	_ = S{Name: "v"}    // none:Name
	counter++           // want:counter=counter
	counter = 2         // want:counter=counter
	u.Var = 3           // want:Var=Var@util
	s.count = 1         // none:count
	r.Run()             // want:Run=Runner.Run
	w.Hello()           // want:Hello=Base.Hello
	_ = Map[Pair[string, T]] // want:Pair=Pair
	var _ Alias         // want:Alias=Alias
	_ = first + second  // want:second=second
	_ = grouped
}

func shadows() {
	type node struct{ next *node } // none:node
	_ = node{}
	counter := 1 // a local
	_ = counter  // none:counter
	var atomic Svc
	atomic.Run() // want:Run=Svc.Run
}

func useAtomic() {
	var n int64
	atomic.AddInt64(&n, 1)
}

type state struct{}

func (*state) step() {}

func sameNameAsType() {
	state := &state{} // the variable is of the package's type state
	state.step()      // want:step=state.step
}

func newPair() Pair[string, int] { return Pair[string, int]{} }

// A generic struct's field typed by its own type parameter has no type of
// its own: Value here is Box's parameter, not the package's Value.
type Value struct{}

func (Value) String() string { return "" }

type Item struct{}

func (Item) String() string { return "" }

type Box[Value any] struct{ v Value }

func useBox(bx Box[Item]) {
	_ = bx.v.String() // none:String
}

// A local type shadowing a package type: its field is the local one's.
type Repo struct{}

func (Repo) Find() {}

type Other struct{}

func (Other) Find() {}

type cfg struct{ r Repo }

func localCfg() {
	type cfg struct{ r Other }
	var c cfg
	c.r.Find() // none:Find
	_ = cfg{}
}

// A field written through a variable of a local type is a repository
// field, never outside the repository.
func localWrite() {
	type local struct{ G int }
	var l local
	l.G = 2
	_ = l
}

// A package variable named like an import's last element (math/rand/v2)
// is that variable, not the import: its initializer states its type.
var v2 = &Svc{}

func useV2() {
	_ = rand.IntN(3)
	v2.Run()    // want:Run=Svc.Run
	v2.count = 1
}

// The standard library's encoding/json beside a repository directory json/:
// json.Marshal is the standard library's.
func useJSON() {
	_, _ = json.Marshal(1) // none:Marshal
}

func shadowedImport() {
	// A variable named like the import: the receiver is neither the import
	// nor outside the repository — it is newPair's declared result.
	atomic := newPair()
	_ = atomic.Key() // want:Key=Pair.Key
}

func methodExpressions() {
	_ = Svc.Run      // want:Run=Svc.Run
	_ = graph.addNode // want:addNode=graph.addNode
}

func genericReceiver(p Pair[string, int], q *b.Holder) {
	_ = p.Key()  // want:Key=Pair.Key
	q.Hold()     // want:Hold=Holder.Hold@b
}

func typeParamReceiver[Svc Runner](s Svc) {
	s.Run() // none:Run
}

func labels() {
	goto Name
Name: // a label is no use of the constant
	return
}
`,
	"b/b.go": `package b

var Shared = 1

// Name in another package: never the target of a.Name.
const Name = "b"

type Holder struct{}

func (*Holder) Hold() {}
`,
	"util/util.go": `package util

const Const = 1

var Var int
`,
	"dot/dot.go": `package dot

const DotConst = 2
`,
	"x/log/log.go": `package log

func Print() {}
`,
	"json/json.go": `package json

func Marshal(v any) ([]byte, error) { return nil, nil }
`,
	"xml/xml.go": `package xml

func Marshal(v any) ([]byte, error) { return nil, nil }
`,
	"a/a_x_test.go": `package a_test

// An external test package beside a declares its own Wrap: its method is
// never a member of a.Wrap.
type Wrap struct{}

func (Wrap) Hello() {}
`,
	"y/log/log.go": `package log

// Same package name, another directory: not xlog.
func Print() {}
`,
}

func TestGoRefOracle_SyntheticModule(t *testing.T) {
	checkRefAnnotations(t, refSyntheticModule)
}

// checkRefAnnotations writes module, compares Ark with the reference-level
// oracle over it and checks its annotations: no answer the type checker
// contradicts anywhere (no false Exact or Strong, no false
// OutsideRepository), every `// want:Name=Qualified[@dir]` site resolved to
// that declaration, no target for any `// none:Name` site, and every
// `// outside:Name` site proven outside the repository (no target).
func checkRefAnnotations(t *testing.T, module map[string]string) *refReport {
	t.Helper()
	root := writeModule(t, module)
	rep, err := compareRefs(root, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total.ExactFP+rep.Total.StrongFP != 0 || rep.Total.FalseOutside != 0 {
		t.Errorf("answers the type checker contradicts:\n%s", rep)
	}

	ark, syms, _, err := arkGoRefs(root)
	if err != nil {
		t.Fatal(err)
	}
	qualified := map[string]string{} // symbol ID → Qualified@dir
	for f, ss := range syms {
		dir := "."
		if i := strings.LastIndex(f, "/"); i >= 0 {
			dir = f[:i]
		}
		for _, s := range ss {
			qualified[string(s.ID)] = s.Qualified + "@" + dir
		}
	}
	// sitesOn returns Ark's answers for name on line of file.
	sitesOn := func(file string, line int, name string) []arkRef {
		var out []arkRef
		prefix := file + ":" + itoa(line) + ":"
		for k, as := range ark {
			if strings.HasPrefix(string(k), prefix) && strings.HasSuffix(string(k), ":"+name) {
				out = append(out, as...)
			}
		}
		return out
	}
	for file, src := range module {
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		dir := "."
		if i := strings.LastIndex(file, "/"); i >= 0 {
			dir = file[:i]
		}
		for i, line := range strings.Split(src, "\n") {
			if _, want, ok := strings.Cut(line, "// want:"); ok {
				name, target, _ := strings.Cut(strings.TrimSpace(want), "=")
				if !strings.Contains(target, "@") {
					target += "@" + dir
				}
				got := sitesOn(file, i+1, name)
				found := false
				var answers []string
				for _, a := range got {
					answers = append(answers, a.kind+" "+a.conf.String()+" "+qualified[string(a.target)])
					found = found || qualified[string(a.target)] == target
				}
				if !found {
					t.Errorf("%s:%d %s: want %s, Ark answered %v", file, i+1, name, target, answers)
				}
			}
			if _, outside, ok := strings.Cut(line, "// outside:"); ok {
				name := strings.TrimSpace(outside)
				got := sitesOn(file, i+1, name)
				if len(got) == 0 {
					t.Errorf("%s:%d %s: no reference, want one proven outside the repository", file, i+1, name)
				}
				for _, a := range got {
					if a.target != "" || !a.outside {
						t.Errorf("%s:%d %s: want outside the repository, Ark answered %s %s %s (outside %v)",
							file, i+1, name, a.kind, a.conf, qualified[string(a.target)], a.outside)
					}
				}
			}
			if _, none, ok := strings.Cut(line, "// none:"); ok {
				name := strings.TrimSpace(none)
				for _, a := range sitesOn(file, i+1, name) {
					if a.target != "" || a.outside {
						t.Errorf("%s:%d %s: no declaration is meant, Ark answered %s %s %s (outside %v)",
							file, i+1, name, a.kind, a.conf, qualified[string(a.target)], a.outside)
					}
				}
			}
		}
	}
	return rep
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
