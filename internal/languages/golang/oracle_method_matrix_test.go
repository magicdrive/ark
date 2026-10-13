package golang_test

import "testing"

// The method-resolution matrix: every use of a method, checked against the
// type checker (oracle_refs_test.go) — receiver (value, pointer), form (a
// call, a method value, a method expression), embedding (none, direct,
// nested, through a pointer, shadowed by a method or a field, ambiguous,
// beside a type of unknown members), generics (an instantiated receiver, a
// type parameter's constraint), interfaces (direct, embedded, a concrete
// value behind an interface type), visibility, packages (same, another, one
// outside the repository), how much type evidence there is (written,
// derived through declarations, none), and namesakes (one method name on
// several types, in several packages).
//
// Ark must claim no Strong or Exact target and no OutsideRepository the type
// checker contradicts anywhere; `// want:` sites must resolve to that
// declaration, `// none:` sites to none, `// outside:` sites be proven
// outside the repository.
var methodMatrixModule = map[string]string{
	"go.mod": "module example.com/mm\n\ngo 1.22\n",
	"m/types.go": `package m

type T struct{ inner Inner }

func (T) Value()    {}
func (*T) Pointer() {}
func (T) same()     {}
func (T) Next() U   { return U{} }

// U declares methods named like T's: never T's targets.
type U struct{}

func (U) Value() {}
func (U) same()  {}

type Inner struct{}

func (Inner) M()         {}
func (*Inner) PM()       {}
func (Inner) Shadowed()  {}
func (Inner) Twice() int { return 0 }

type Mid struct{ Inner }

type Outer struct{ Mid }

type PtrOuter struct{ *Inner }

type Shadow struct{ Inner }

func (Shadow) Shadowed() {}

// A field M at depth 0 hides the promoted method M.
type FieldShadow struct {
	Inner
	M func()
}

type Box[X any] struct{ v X }

func (b Box[X]) Get() X    { return b.v }
func (b *Box[X]) Set(v X)  { b.v = v }
func NewBox[X any]() *Box[X] { return &Box[X]{} }

type Reader interface{ Read() int }

type ReadCloser interface {
	Reader
	Close()
}

type impl struct{}

func (impl) Read() int { return 0 }
func (impl) Close()    {}

type Stringer interface{ String() string }

// An interface on one line with a method of its own name.
type Error interface{ Error() string }

// A type named like an interface's method, used in that interface: the
// type use is the type, never the method.
// Embedded: an interface into a struct, a generic type by value and
// through a pointer.
type H struct{ Reader }

type GB struct{ Box[int] }

type PGB struct{ *Box[int] }

// Two embedded interfaces declaring one method: a diamond.
type I1 interface{ Close() }
type I2 interface{ Close() }
type I12 interface {
	I1
	I2
}

type Lang int

type Provider interface{ Lang() Lang }

type named int

func (named) String() string { return "" }

type Alias = T

type Ts []T

var Default = &T{}

// Shape is an interface a *Circle satisfies: a variable declared as one
// stays one whatever is assigned to it.
type Shape interface{ Area() int }

type Circle struct{}

func (*Circle) Area() int { return 0 }

func newShape() (Shape, error)  { return nil, nil }
func newCircle() (int, *Circle) { return 0, nil }
func newCircleOnly() *Circle     { return nil }

// Make's result is typed by its type parameter: it states no type.
func Make[X any](x X) X { return x }

type Counter struct{}

func (Counter) Add(int) {}

func NewT() *T                { return &T{} }
func NewPair() (*T, error)    { return nil, nil }
func lookup() map[string]*T   { return nil }
`,
	"m/use.go": `package m

func direct(t T, p *T) {
	t.Value()   // want:Value=T.Value
	t.Pointer() // want:Pointer=T.Pointer
	p.Value()   // want:Value=T.Value
	p.Pointer() // want:Pointer=T.Pointer
	var u U
	u.Value() // want:Value=U.Value
	t.same()  // want:same=T.same
	u.same()  // want:same=U.same
}

func methodValues(t T, p *T, o Outer) {
	f := t.Value   // want:Value=T.Value
	g := p.Pointer // want:Pointer=T.Pointer
	h := o.M       // want:M=Inner.M
	f()
	g()
	h()
	_ = t.inner // none:inner
	k := NewT().Value // want:Value=T.Value
	k()
}

func methodExpressions() {
	_ = T.Value        // want:Value=T.Value
	_ = (*T).Pointer   // want:Pointer=T.Pointer
	_ = (*T).Value     // want:Value=T.Value
	_ = Box[int].Get   // want:Get=Box.Get
	_ = (*Box[int]).Set // want:Set=Box.Set
	_ = Outer.M         // want:M=Inner.M
	var t T
	T.Value(t) // want:Value=T.Value
}

func promoted(o Outer, m Mid, po PtrOuter, s Shadow, fs FieldShadow) {
	m.M()          // want:M=Inner.M
	o.M()          // want:M=Inner.M
	o.PM()         // want:PM=Inner.PM
	po.M()         // want:M=Inner.M
	po.PM()        // want:PM=Inner.PM
	s.Shadowed()   // want:Shadowed=Shadow.Shadowed
	m.Shadowed()   // want:Shadowed=Inner.Shadowed
	fs.M()         // none:M
	_ = o.Twice()  // want:Twice=Inner.Twice
	_ = o.Mid.M    // want:M=Inner.M
}

func generics(b Box[int], pb *Box[string]) {
	_ = b.Get()            // want:Get=Box.Get
	pb.Set("x")            // want:Set=Box.Set
	nb := NewBox[float64]()
	_ = nb.Get()           // want:Get=Box.Get
}

func constrained[S Stringer](s S) {
	_ = s.String() // none:String
}

func interfaces(r Reader, rc ReadCloser, s Stringer) {
	_ = r.Read()   // want:Read=Reader.Read
	_ = rc.Read()  // want:Read=Reader.Read
	rc.Close()     // want:Close=ReadCloser.Close
	_ = s.String() // want:String=Stringer.String
	var x Reader = impl{}
	_ = x.Read() // want:Read=Reader.Read
	var n named
	_ = n.String() // want:String=named.String
}

func chains(ts Ts) {
	t := NewT()
	t.Value() // want:Value=T.Value
	p, err := NewPair()
	_ = err
	p.Pointer()            // want:Pointer=T.Pointer
	NewT().Value()         // want:Value=T.Value
	t.Next().Value()       // want:Value=U.Value
	Default.Value()        // want:Value=T.Value
	var a Alias
	a.Value() // want:Value=T.Value
	for _, x := range []T{} {
		x.Value() // want:Value=T.Value
	}
	ts[0].Value() // want:Value=T.Value
	for _, y := range ts {
		y.Value() // want:Value=T.Value
	}
	v := any(t).(*T)
	v.Value() // want:Value=T.Value
	w, ok := any(t).(*T)
	_ = ok
	w.Value() // want:Value=T.Value
	n := new(T)
	n.Pointer() // want:Pointer=T.Pointer
	c := t.inner
	c.M() // want:M=Inner.M
	lookup()["k"].Value() // want:Value=T.Value
	if q := NewT(); q != nil {
		q.Value() // want:Value=T.Value
	}
}

func embeddedKinds(h H, gb GB, pgb PGB, d I12, x any) {
	_ = h.Read()   // want:Read=Reader.Read
	_ = gb.Get()   // want:Get=Box.Get
	pgb.Set(1)     // want:Set=Box.Set
	d.Close()      // none:Close
	switch v := x.(type) {
	case *T:
		v.Value() // none:Value
	}
}

func scopes() {
	x := NewT()
	func() {
		x.Value() // want:Value=T.Value
	}()
	{
		x := U{}
		x.Value() // want:Value=U.Value
	}
	x.Value() // want:Value=T.Value
}

func errs(e Error) {
	_ = e.Error() // want:Error=Error.Error
}

func partial(x interface{ Value() }, y any) {
	x.Value() // none:Value
	_ = y
}

// A := redeclaring a variable of its block assigns it: its type is its
// first declaration's; so does a range with =.
func redeclared(circles []*Circle) {
	s, err := newShape()
	n, s := newCircle()
	_, _ = err, n
	_ = s.Area() // want:Area=Shape.Area
	var t Shape
	for _, t = range circles {
		_ = t.Area() // want:Area=Shape.Area
	}
}

// A := in an if, for or range body declares anew, though the body ends
// where the statement whose header declared the outer name ends.
func innerBlocks(shapes []Shape) {
	if x, err := newShape(); err == nil {
		x, y := newCircleOnly(), 1
		_ = y
		_ = x.Area() // want:Area=Circle.Area
	}
	for _, x := range shapes {
		_, x := newCircle()
		_ = x.Area() // want:Area=Circle.Area
	}
	for x, err := newShape(); err == nil; {
		_, x := newCircle()
		_ = x.Area() // want:Area=Circle.Area
		break
	}
}

// A type switch's guard is declared in each clause's own block: a := of it
// there redeclares it, keeping the clause's type.
func typeSwitchRedeclared(v any) {
	switch x := v.(type) {
	case Shape:
		x, y := newCircleOnly(), 1
		_ = y
		_ = x.Area() // none:Area
	}
}

// A local named like a type, whose own type cannot be derived (Make's
// result): a value — never the type T.
func capKept() {
	T := Make(U{})
	T.Value() // none:Value
}

func shadowing() {
	T := U{}
	T.Value() // want:Value=U.Value
	U := NewT()
	U.Value() // want:Value=T.Value
	f := U.Value // want:Value=T.Value
	f()
}
`,
	// A package variable named like an import's guessed name, whose type
	// cannot be derived: a value, never the import.
	"m/v2.go": `package m

import "math/rand/v2"

var v2 = Make(Counter{})

func useV2() {
	_ = rand.IntN(2)
	v2.Add(1) // none:Add
}
`,
	"m2/m2.go": `package m2

import (
	"strings"

	"example.com/mm/amb"
	"example.com/mm/m"
)

// Two import paths of the module establish its import-path prefix
// (package scoping, resolver/packagescope.go).
var _ = amb.A{}

func cross(t m.T, b *strings.Builder) {
	t.Value()          // want:Value=T.Value@m
	m.NewT().Pointer() // want:Pointer=T.Pointer@m
	_ = m.T.Value      // want:Value=T.Value@m
	b.WriteString("x") // outside:WriteString
	x := m.NewT()
	x.Value() // want:Value=T.Value@m
	var sb strings.Builder
	_ = sb.Len() // outside:Len
	var r m.Reader
	_ = r.Read() // want:Read=Reader.Read@m
}

// A local named like an import, whose type cannot be derived: a value,
// never the package amb (whose Add is a function).
func ambShadow() {
	amb := m.Make(m.Counter{})
	amb.Add(1) // none:Add
}

// Types of another package, embedded and aliased.
type W struct{ m.Inner }

type PW struct{ *m.Inner }

type AT = m.T

func crossEmbedded(w W, pw PW, at AT) {
	w.M()      // want:M=Inner.M@m
	pw.PM()    // want:PM=Inner.PM@m
	at.Value() // want:Value=T.Value@m
}

// Value in another package: never m.T's.
type Local struct{}

func (Local) Value() {}

func useLocal(l Local) {
	l.Value() // want:Value=Local.Value
}
`,
	"amb/amb.go": `package amb

import "sync"

type A struct{}

func (A) M() {}

type B struct{}

func (B) M() {}

type AB struct {
	A
	B
}

func (AB) Own() {}

// Add is a function of package amb: never a method of a value named amb.
func Add(int) {}

type Inner struct{}

func (Inner) Do() {}

type Mid struct{ Inner }

// Beside an embedded type of unknown members (sync.Mutex), at the depth of
// the member and above it.
type Mixed struct {
	sync.Mutex
	Inner
}

type Deep struct {
	sync.Mutex
	Mid
}

func use(x AB, mx Mixed, d Deep) {
	x.Own()   // want:Own=AB.Own
	x.A.M()   // want:M=A.M
	mx.Do()   // none:Do
	d.Do()    // none:Do
	mx.Lock() // none:Lock
}
`,
	// x.M is ambiguous (A and B both promote it): an invalid selector, which
	// the type checker reports; Ark must not choose.
	"bad/bad.go": `package bad

func newAB() AB { return AB{} }

func useChained() {
	newAB().M() // none:M
}

// A field and a method of one name at one depth: an ambiguous selector.
type X struct{ N int }
type Y struct{}

func (Y) N() {}

type FM struct {
	X
	Y
}

func useFM(fm FM) {
	fm.N() // none:N
}

type A struct{}

func (A) M() {}

type B struct{}

func (B) M() {}

type AB struct {
	A
	B
}

func use(x AB) {
	x.M() // none:M
}

// D embedded along two paths at one depth: D.M appears twice there.
type D struct{}

func (D) M() {}

type B2 struct{ D }
type C2 struct{ D }
type A2 struct {
	B2
	C2
}

func use2(a A2) {
	a.M() // none:M
}
`,
	// Two programs in one directory (testdata): a method of one program's
	// T is never a member of the other's.
	"progs/one.go": `package main

type T struct{ x int }

func main() {
	var t T
	_ = t.x // none:x
}
`,
	"progs/two.go": `package main

type T struct{}

func (T) x() {}

func main() {}
`,
}

func TestGoRefOracle_MethodMatrix(t *testing.T) {
	rep := checkRefAnnotations(t, methodMatrixModule)
	// The matrix exercises every form: each of them is resolved somewhere.
	for _, tag := range []string{"m:call", "m:value", "m:expr", "m:promoted", "m:generic", "m:interface", "m:cross_package", "m:ptr_recv"} {
		c := rep.ByMethod[tag]
		if c == nil || c.Recovered == 0 {
			t.Errorf("no %s site resolved:\n%s", tag, rep)
		}
	}
}
