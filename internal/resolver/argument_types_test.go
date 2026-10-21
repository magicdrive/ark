package resolver_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// Argument types leave out the overloads an argument cannot be passed to;
// unknown types leave everything; the most specific of primitive overloads
// is taken only when every argument's type is known.
func TestArgumentTypes(t *testing.T) {
	a := newLK("src/p/A.lk", "p", "src")
	a.decl(symbol.KindClass, "p.A", "")
	ofInt := a.decl(symbol.KindMethod, "p.A.f", "p.A", params("(int)"))
	ofLong := a.decl(symbol.KindMethod, "p.A.f", "p.A", params("(long)"))
	ofString := a.decl(symbol.KindMethod, "p.A.f", "p.A", params("(?String)"))
	arr := a.decl(symbol.KindMethod, "p.A.g", "p.A", params("(boolean[])"))
	a.decl(symbol.KindMethod, "p.A.g", "p.A", params("(int[])"))
	obj := a.decl(symbol.KindMethod, "p.A.g", "p.A", params("(?Object)"))
	a.decl(symbol.KindMethod, "p.A.v", "p.A", params("(?Object)"))
	varg := a.decl(symbol.KindMethod, "p.A.v", "p.A", params("(int...)"))
	files := []*lkFile{a}
	call := func(name, types string, n int) resolver.Resolution {
		r := lkCall("src/p/A.lk", name, "@M:p.A."+name, n)
		r.ArgumentTypes = types
		return lkResolve(t, files, "src/p/A.lk", r)
	}
	// "x": not an int, not a long.
	lkWantTarget(t, call("f", "S", 1), resolver.ConfidenceExact, ofString)
	// 1: int and long apply strictly; int is the most specific; the String
	// overload does not apply.
	lkWantTarget(t, call("f", "int", 1), resolver.ConfidenceExact, ofInt)
	lkWantTarget(t, call("f", "long", 1), resolver.ConfidenceExact, ofLong)
	// short widens to int and long: int.
	lkWantTarget(t, call("f", "short", 1), resolver.ConfidenceExact, ofInt)
	// A reference of unknown type: unboxing may reach int and long, String
	// may be it — no choice.
	lkWantNone(t, call("f", "R", 1), resolver.ConfidenceCandidate, 3, false)
	// Unknown: everything.
	lkWantNone(t, call("f", "", 1), resolver.ConfidenceCandidate, 3, false)
	// null is no primitive.
	lkWantTarget(t, call("f", "null", 1), resolver.ConfidenceExact, ofString)
	// A boolean[] may be passed to boolean[] and to Object, both strictly:
	// the one of the argument's own type is the most specific.
	lkWantTarget(t, call("g", "boolean[]", 1), resolver.ConfidenceExact, arr)
	// "N" is never unboxed: not f(int), not f(long).
	lkWantTarget(t, call("f", "N", 1), resolver.ConfidenceExact, ofString)
	// A lambda is no array: only the Object one.
	lkWantTarget(t, call("g", "F", 1), resolver.ConfidenceExact, obj)
	// v(Object) and v(int...): 1 boxed applies to java.lang.Object, but
	// "?Object" is not proven to be it (another Object would leave only the
	// variadic one): both.
	lkWantNone(t, call("v", "int", 1), resolver.ConfidenceCandidate, 2, false)
	// Two ints: only the variadic one.
	lkWantTarget(t, call("v", "int,int", 2), resolver.ConfidenceExact, varg)
	// A malformed list (not one type per argument) is not used.
	lkWantNone(t, call("f", "int,int", 1), resolver.ConfidenceCandidate, 3, false)
}
