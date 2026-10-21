package java

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// resolveRepo extracts and resolves the files of a repository given as
// path → source, and returns each reference's resolution as
// "file:line name kind" → "confidence target" (target "" when none;
// "outside" when outside the repository; several targets joined by "|").
func resolveRepo(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	return resolveRepoAs(t, files, false)
}

// resolveRepoLines is resolveRepo naming each target with its line
// ("p.A.m@3"): overloads of one name told apart.
func resolveRepoLines(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	return resolveRepoAs(t, files, true)
}

func resolveRepoAs(t *testing.T, files map[string]string, withLines bool) map[string]string {
	t.Helper()
	p := NewProvider()
	var paths []string
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var fis []resolver.FileIndex
	for _, path := range paths {
		ex, err := p.Extract(context.Background(), source.FileID(path), []byte(files[path]))
		if err != nil {
			t.Fatal(err)
		}
		fis = append(fis, index.NewFileIndex(p, source.FileID(path), ex))
	}
	r := resolver.New(fis)
	lineOf := map[symbol.SymbolID]uint32{}
	for _, fi := range fis {
		for _, sym := range fi.Symbols {
			lineOf[sym.ID] = sym.Location.Range.Start.Line
		}
	}
	refs := map[reference.ReferenceID]reference.Reference{}
	for _, fi := range fis {
		for _, ref := range fi.References {
			refs[ref.ID] = ref
		}
	}
	out := map[string]string{}
	for _, res := range r.Resolve() {
		ref := refs[res.ReferenceID]
		key := fmt.Sprintf("%s:%d %s %s", ref.Location.File, ref.Location.Range.Start.Line, ref.Name, ref.Kind)
		var targets []string
		for _, c := range res.Candidates {
			if withLines {
				targets = append(targets, fmt.Sprintf("%s@%d", c.Qualified, lineOf[c.SymbolID]))
			} else {
				targets = append(targets, c.Qualified)
			}
		}
		if withLines {
			sort.Strings(targets)
		}
		v := res.Confidence.String() + " " + strings.Join(targets, "|")
		if res.OutsideRepository {
			v = res.Confidence.String() + " outside"
		}
		if prev, dup := out[key]; dup && prev != v {
			v = prev + " / " + v
		}
		out[key] = v
	}
	return out
}

// expect checks resolutions: want maps "file:line name kind" to
// "confidence target" ("unresolved " for none, "unresolved outside").
func expect(t *testing.T, got map[string]string, want map[string]string) {
	t.Helper()
	var keys []string
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if got[k] != want[k] {
			t.Errorf("%s: got %q, want %q", k, got[k], want[k])
		}
	}
}

// absent checks that no reference was stated for the keys (a local, a
// label, a name the provider must not observe).
func absent(t *testing.T, got map[string]string, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if v, ok := got[k]; ok {
			t.Errorf("%s: stated (%s), want no reference", k, v)
		}
	}
}

// Overloads are told apart by argument count only; inherited overloads take
// part (a subclass's m(int) does not hide the base's m(String)).
func TestOverloads(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/Base.java": `package p;
public class Base {
    public void m(String s) {}
    public void n() {}
}`,
		"src/p/Sub.java": `package p;
public class Sub extends Base {
    public void m(int i) {}
    public void m(int i, int j) {}
    public void n() {}
    void use() {
        m(1, 2);
        m(1);
        n();
        this.m(1, 2);
        m();
    }
}`,
	})
	expect(t, got, map[string]string{
		"src/p/Sub.java:7 m call": "exact p.Sub.m",
		// 1 is an int: m(int) applies strictly, m(String) only by boxing.
		"src/p/Sub.java:8 m call":  "exact p.Sub.m",
		"src/p/Sub.java:9 n call":  "exact p.Sub.n",
		"src/p/Sub.java:10 m call": "exact p.Sub.m",
		"src/p/Sub.java:11 m call": "unresolved ",
	})
}

// The JLS 15.12.1 example of the soundness contract: run(String) and
// run(Integer) with an Object argument — neither is chosen by its name.
func TestOverloadByTypeIsNotChosen(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/Example.java": `class Example {
    void run(String value) {}
    void run(Integer value) {}
    void execute(Object value) {
        run(value);
    }
}`,
	})
	expect(t, got, map[string]string{"src/Example.java:5 run call": "candidate Example.run|Example.run"})
}

// A local variable or parameter shadows a field; a field of an anonymous
// class's supertype shadows the enclosing method's local.
func TestShadowing(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/Base.java": `package p;
public class Base { protected int count; }`,
		"src/p/A.java": `package p;
class A {
    int count;
    int total;
    void m(int total) {
        total = 1;
        count = 2;
        int count = 3;
        count++;
        new Base() {
            void x() { count = 4; }
        };
    }
}`,
	})
	// A local named count is declared later in the body: never a field.
	absent(t, got, "src/p/A.java:6 total write", "src/p/A.java:7 count write", "src/p/A.java:9 count value_reference")
	expect(t, got, map[string]string{"src/p/A.java:11 count write": "exact p.Base.count"})
}

// A member type inherited from a supertype shadows a single-type import of
// the same name (JLS 6.4.1); a type parameter shadows a class.
func TestTypeShadowing(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/a/Entry.java": `package a;
public class Entry {}`,
		"src/p/Base.java": `package p;
public class Base { public static class Entry {} }`,
		"src/p/Sub.java": `package p;
import a.Entry;
class Sub extends Base {
    Entry e;
}
class Plain {
    Entry e;
}
class Gen<Entry> {
    Entry e;
}`,
	})
	expect(t, got, map[string]string{
		"src/p/Sub.java:4 Entry type_use": "exact p.Base.Entry",
		"src/p/Sub.java:7 Entry type_use": "exact a.Entry",
	})
	absent(t, got, "src/p/Sub.java:10 Entry type_use")
}

// A method an enclosing class has hides a static import of the name,
// whatever the arguments (JLS 15.12.1: the innermost class with a method of
// the name is searched).
func TestStaticImportShadowedByMember(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/u/Util.java": `package u;
public class Util { public static void help(int a, int b) {} public static void only() {} }`,
		"src/p/A.java": `package p;
import static u.Util.help;
import static u.Util.only;
class A {
    void help() {}
    void m() { help(1, 2); only(); }
}`,
	})
	expect(t, got, map[string]string{
		"src/p/A.java:6 help call": "unresolved ",
		"src/p/A.java:6 only call": "exact u.Util.only",
	})
}

// A type the file does not declare or import, in the same package (another
// file), an on-demand import, java.lang: Strong at most (a scope outside the
// repository may hold it), outside when no declaration of the repository is
// found.
func TestPackageAndOnDemandImports(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/Same.java": `package p;
public class Same {}`,
		"src/q/Other.java": `package q;
public class Other {}`,
		"src/p/A.java": `package p;
import q.*;
class A {
    Same s;
    Other o;
    String str;
}`,
	})
	expect(t, got, map[string]string{
		"src/p/A.java:4 Same type_use":   "exact p.Same",
		"src/p/A.java:5 Other type_use":  "strong q.Other",
		"src/p/A.java:6 String type_use": "unresolved outside",
	})
}

// Enum constants in switch labels are not resolved by name (the selector's
// type decides); a qualified one is.
func TestSwitchLabels(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/Color.java": `package p;
public enum Color { RED, GREEN }`,
		"src/p/A.java": `package p;
class A {
    static final int RED = 1;
    void m(Color c) {
        switch (c) { case RED: break; default: break; }
        switch (c) { case GREEN -> {} default -> {} }
        Color x = Color.RED;
    }
}`,
	})
	absent(t, got, "src/p/A.java:5 RED value_reference", "src/p/A.java:6 GREEN value_reference")
	expect(t, got, map[string]string{"src/p/A.java:7 RED value_reference": "exact p.Color.RED"})
}

// Receivers: a typed local, parameter, own field, this, super, a cast, a
// class instance creation; a pattern binding typed where it is in scope.
func TestReceivers(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/Svc.java": `package p;
public class Svc { public void go() {} public static Svc make() { return null; } }`,
		"src/p/Base.java": `package p;
public class Base { public void hook() {} }`,
		"src/p/A.java": `package p;
class A extends Base {
    Svc field;
    public void hook() {}
    void m(Svc param, Object o) {
        Svc local = null;
        local.go();
        param.go();
        field.go();
        this.hook();
        super.hook();
        ((Svc) o).go();
        new Svc().go();
        Svc.make().go();
        if (o instanceof Svc s && s != null) { s.go(); }
        var v = new Svc();
        v.go();
    }
}`,
	})
	expect(t, got, map[string]string{
		"src/p/A.java:7 go call":    "exact p.Svc.go",
		"src/p/A.java:8 go call":    "exact p.Svc.go",
		"src/p/A.java:9 go call":    "exact p.Svc.go",
		"src/p/A.java:10 hook call": "exact p.A.hook",
		"src/p/A.java:11 hook call": "exact p.Base.hook",
		"src/p/A.java:12 go call":   "exact p.Svc.go",
		"src/p/A.java:13 go call":   "exact p.Svc.go",
		"src/p/A.java:14 make call": "exact p.Svc.make",
		"src/p/A.java:14 go call":   "unresolved ",
		"src/p/A.java:15 go call":   "exact p.Svc.go",
		"src/p/A.java:17 go call":   "exact p.Svc.go",
	})
}

// Fully qualified names: a package path, a member type through its outer
// type, a name whose head is a variable.
func TestQualifiedNames(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/a/b/Outer.java": `package a.b;
public class Outer { public static class Inner { public static void f() {} } public static int N; }`,
		"src/p/A.java": `package p;
class A {
    a.b.Outer o;
    a.b.Outer.Inner i;
    void m() {
        a.b.Outer.Inner.f();
        int n = a.b.Outer.N;
    }
}`,
	})
	expect(t, got, map[string]string{
		"src/p/A.java:3 Outer type_use":    "strong a.b.Outer",
		"src/p/A.java:4 Inner type_use":    "strong a.b.Outer.Inner",
		"src/p/A.java:6 f call":            "strong a.b.Outer.Inner.f",
		"src/p/A.java:7 N value_reference": "strong a.b.Outer.N",
	})
}

// A supertype outside the repository leaves what it may declare unknown:
// a call the class itself declares is Strong, one it does not is outside.
func TestExternalSupertype(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/A.java": `package p;
import java.util.ArrayList;
class A extends ArrayList<String> {
    void own() {}
    void m() { own(); add("x"); }
}`,
	})
	expect(t, got, map[string]string{
		"src/p/A.java:5 own call": "strong p.A.own",
		"src/p/A.java:5 add call": "unresolved outside",
	})
}

// java.lang.Object's members are found on every class: toString() an own
// override names the own one (no parameters: an override for sure);
// equals(Object) beside an own equals(A) is a Candidate.
func TestObjectMembers(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/A.java": `package p;
class A {
    public String toString() { return ""; }
    boolean equals(A other) { return false; }
    void m(Object o) { toString(); equals(o); hashCode(); }
}`,
	})
	expect(t, got, map[string]string{
		"src/p/A.java:5 toString call": "exact p.A.toString",
		"src/p/A.java:5 equals call":   "candidate p.A.equals",
		"src/p/A.java:5 hashCode call": "unresolved outside",
	})
}

// Records: components are fields, accessors and the canonical constructor
// are the language's unless declared; enums: values() is the enum's.
func TestRecordsAndEnums(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/P.java": `package p;
record P(int x, int y) {
    int x() { return x; }
    static P origin() { return new P(0, 0); }
}`,
		"src/p/E.java": `package p;
enum E { A, B; static int n() { return values().length; } }`,
		"src/p/U.java": `package p;
class U { void m(P p) { p.x(); p.y(); P.origin(); E.n(); } }`,
	})
	expect(t, got, map[string]string{
		"src/p/P.java:3 x value_reference": "exact p.P.x",
		"src/p/P.java:4 P construction":    "exact p.P",
		"src/p/U.java:2 x call":            "exact p.P.x",
		"src/p/U.java:2 y call":            "unresolved ",
		"src/p/U.java:2 origin call":       "exact p.P.origin",
		"src/p/U.java:2 n call":            "exact p.E.n",
		"src/p/E.java:2 values call":       "unresolved ",
	})
}

// Two source roots holding one class (two flavors of one library): a
// reference of either root names its own; a third root's is ambiguous.
func TestSourceRoots(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"jre/src/p/A.java":     "package p;\npublic class A {}",
		"android/src/p/A.java": "package p;\npublic class A {}",
		"jre/src/q/U.java":     "package q;\nimport p.A;\nclass U { A a; }",
		"android/src/q/U.java": "package q;\nimport p.A;\nclass U { A a; }",
		"tests/q/T.java":       "package q;\nimport p.A;\nclass T { A a; }",
	})
	expect(t, got, map[string]string{
		"jre/src/q/U.java:3 A type_use":     "exact p.A",
		"android/src/q/U.java:3 A type_use": "exact p.A",
		"tests/q/T.java:3 A type_use":       "candidate p.A|p.A",
	})
}

// A region the parser rejects outside a body: the file states no
// references; inside a body: that body states none.
func TestParseErrors(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/S.java": "package p;\npublic class S { public void go() {} }",
		"src/p/Bad.java": `package p;
import q.S;
class Bad { S s; void m() { s.go(); } ) }`,
		"src/p/Body.java": `package p;
class Body { S s; void m() { s.go( ; } void n() { s.go(); } }`,
	})
	for k := range got {
		if strings.HasPrefix(k, "src/p/Bad.java") {
			t.Errorf("a broken file stated %s", k)
		}
	}
	if v := got["src/p/Body.java:2 go call"]; v != "exact p.S.go" {
		t.Errorf("the intact body: %q", v)
	}
}

// Every declaration kind gets a symbol with the identity the package
// header states.
func TestSymbols(t *testing.T) {
	ex, err := NewProvider().Extract(context.Background(), "src/p/All.java", []byte(`package p;
public class All<T> {
    int f, g;
    All() {}
    void m(int a, String... rest) {}
    class Inner {}
    interface I { int C = 1; void x(); }
    enum E { A, B { void b() {} } }
    record R(int x) {}
    @interface Ann { String value() default ""; }
    void local() { class L { void y() {} } Runnable r = new Runnable() { public void run() {} }; }
}`))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range ex.Symbols {
		got = append(got, fmt.Sprintf("%s %s %s", s.Kind, s.Qualified, s.Parameters))
	}
	want := []string{
		"class p.All ",
		"field p.All.f ", "field p.All.g ",
		"constructor p.All.<init> ()",
		"method p.All.m (int,?String...)",
		"class p.All.Inner ",
		"interface p.All.I ", "field p.All.I.C ", "method p.All.I.x ()",
		"enum p.All.E ", "constant p.All.E.A ", "constant p.All.E.B ",
		"class p.All.R ", "field p.All.R.x ",
		"interface p.All.Ann ", "method p.All.Ann.value ()",
		"method p.All.local ()",
		"class p.All.local.L ", "method p.All.local.L.y ()",
		"class p.All.<anonymous> ", "method p.All.<anonymous>.run ()",
		"class p.All.E.<anonymous> ", "method p.All.E.<anonymous>.b ()",
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("symbols:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, s := range ex.Symbols {
		if s.Qualified == "p.All.R" && !strings.Contains(s.ImplicitMembers, "+x()") {
			t.Errorf("record accessor not implicit: %q", s.ImplicitMembers)
		}
		if s.Kind == symbol.KindClass && s.Qualified == "p.All" && !strings.Contains(s.ImplicitMembers, "toString()") {
			t.Errorf("Object's members not implicit: %q", s.ImplicitMembers)
		}
	}
	if !ex.IdentityOnly || ex.Package != "p" || ex.SourceRoot != "src/" {
		t.Errorf("extraction: identityOnly %v package %q root %q", ex.IdentityOnly, ex.Package, ex.SourceRoot)
	}
}

func TestSourceRoot(t *testing.T) {
	for _, c := range []struct{ file, pkg, want string }{
		{"src/main/java/a/b/C.java", "a.b", "src/main/java/"},
		{"a/b/C.java", "a.b", "/"},
		{"C.java", "", "/"},
		{"x/C.java", "", "x/"},
		{"src/a/C.java", "b", ""},
	} {
		if got := sourceRoot(source.FileID(c.file), c.pkg); got != c.want {
			t.Errorf("sourceRoot(%s, %s) = %q, want %q", c.file, c.pkg, got, c.want)
		}
	}
}

// Static imports bring member types as well (JLS 7.5.3, 7.5.4).
func TestStaticImportOfMemberTypes(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/F.java": `package p;
public interface F { interface Byte {} interface Char {} static void make() {} }`,
		"src/q/A.java": `package q;
import static p.F.Byte;
import static p.F.make;
class A { Byte b; }`,
		"src/q/B.java": `package q;
import static p.F.*;
class B { Char c; }`,
	})
	expect(t, got, map[string]string{
		"src/q/A.java:4 Byte type_use": "exact p.F.Byte",
		"src/q/B.java:3 Char type_use": "strong p.F.Char",
	})
}

// A superclass declared further down the file, and one written with type
// arguments on its outer type, are the class's superclass.
func TestSupertypesAnywhereInTheFile(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/A.java": `package p;
class A {
    static class Facade<T> extends Old<T> {
        boolean set(T t) { return super.set(t); }
    }
    static class Red<X> {
        class Orange { Object self() { return null; } }
        class Yellow<B> extends Red<B>.Orange {
            Object b() { return self(); }
        }
    }
    abstract static class Old<V> { boolean set(V v) { return true; } }
}`,
	})
	expect(t, got, map[string]string{
		"src/p/A.java:4 set call":  "exact p.A.Old.set",
		"src/p/A.java:9 self call": "exact p.A.Red.Orange.self",
	})
}

// Argument types the file proves narrow overloads (JLS 15.12.2): literals,
// typed locals, casts; an argument of unknown type leaves them all.
func TestOverloadsByArgumentTypes(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/U.java": `package p;
class U {
    static void put(int v) {}
    static void put(long v) {}
    static void put(String v) {}
    static void put(boolean[] v) {}
    void m(Object o, short s, int[] ints, Integer boxed) {
        put(1);
        put(1L);
        put("x");
        put(s);
        put(new boolean[0]);
        put(boxed);
        put((String) o);
        put(ints);
        put(1 + 2L);
        put("a" + o);
    }
}`,
	})
	expect(t, got, map[string]string{
		"src/p/U.java:8 put call":  "exact p.U.put",
		"src/p/U.java:9 put call":  "exact p.U.put",
		"src/p/U.java:10 put call": "exact p.U.put",
		"src/p/U.java:11 put call": "exact p.U.put",
		"src/p/U.java:12 put call": "exact p.U.put",
		"src/p/U.java:13 put call": "candidate p.U.put|p.U.put|p.U.put",
		"src/p/U.java:14 put call": "exact p.U.put",
		// int[]: no int[] overload; in code that compiles, String is a type
		// variable or Object's name here.
		"src/p/U.java:15 put call": "exact p.U.put",
		"src/p/U.java:16 put call": "exact p.U.put",
		"src/p/U.java:17 put call": "exact p.U.put",
	})
}

// A type annotation in an array type is no part of the type: an int[]
// argument is an int[].
func TestAnnotatedArrayTypes(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/A.java": `package p;
class A {
    static void eq(int @Nullable [] a, Object x) {}
    static void eq(@Nullable Object @Nullable [] a, Object x) {}
    static void m(int @Nullable [] ints) { eq(ints, null); }
}`,
	})
	expect(t, got, map[string]string{"src/p/A.java:5 eq call": "exact p.A.eq"})
	for k, v := range got {
		if strings.HasPrefix(k, "src/p/A.java:5 eq") && !strings.Contains(v, "p.A.eq") {
			t.Errorf("%s: %s", k, v)
		}
	}
}

// Through a type name only static members are reached; through a value,
// instance members (a static one through a value is legal Java, but not
// followed). Removing a member a receiver cannot reach never raises the
// confidence: what is left of several is a Candidate.
func TestStaticAndInstanceMembers(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"src/p/T.java": `package p;
public class T {
    public static T of(Object o) { return null; }
    public T of(String s) { return this; }
}`,
		"src/p/U.java": `package p;
class U {
    void m(T t, Object x) {
        T.of(x);
        t.of(x);
    }
}`,
	})
	expect(t, got, map[string]string{
		"src/p/U.java:4 of call": "candidate p.T.of",
		"src/p/U.java:5 of call": "candidate p.T.of",
	})
}

// Only the methods and constructors a call may access are its candidates
// (JLS 6.6, 15.12.2.1): a private overload outside its top-level class, a
// package-private one outside its package, is none — and a protected one of
// another package is never what the others are narrowed to.
func TestAccessibility(t *testing.T) {
	got := resolveRepoLines(t, map[string]string{
		"a/Other.java": `package a;
public class Other {
    private void m(int x) {}
    public void m(long x) {}
    void p(int x) {}
    public void p(long x) {}
    void self() { m(1); }
}`,
		"a/Same.java": `package a;
class Same {
    void f(Other o) { o.m(1); o.p(1); }
}`,
		"a/Sub.java": `package a;
public class Sub {
    private Sub(int x) {}
    public Sub(long x) {}
    private void v(int x) {}
    public void v(int... x) {}
}`,
		"a/U.java": `package a;
public class U {
    protected U(int x) {}
    public U(long x) {}
    protected void q(int x) {}
    public void q(long x) {}
}`,
		"b/Main.java": `package b;
import a.Other;
import a.Sub;
import a.U;
class Main {
    void f(Other o, Sub s, U u) {
        o.m(1);
        o.p(1);
        new Sub(1);
        s.v(1);
        u.q(1);
    }
}`,
	})
	expect(t, got, map[string]string{
		"a/Other.java:7 m call":     "exact a.Other.m@3",
		"a/Same.java:3 m call":      "exact a.Other.m@4",
		"a/Same.java:3 p call":      "exact a.Other.p@5",
		"b/Main.java:7 m call":      "exact a.Other.m@4",
		"b/Main.java:8 p call":      "exact a.Other.p@6",
		"b/Main.java:9 <init> call": "exact a.Sub.<init>@4",
		"b/Main.java:10 v call":     "exact a.Sub.v@6",
		// protected q(int) of another package: maybe accessible, so not
		// what q(long) is narrowed away for.
		"b/Main.java:11 q call": "candidate a.U.q@5|a.U.q@6",
	})
}

// An interface's static method is not inherited (JLS 8.4.8): a class
// implementing the interface does not have it, and the enclosing class's
// method of the name is the one called.
func TestInterfaceStaticNotInherited(t *testing.T) {
	got := resolveRepoLines(t, map[string]string{
		"a/I.java": "package a;\npublic interface I {\n    static void helper() {}\n}",
		"b/Main.java": `package b;
import a.I;
class Main {
    static void helper() {}
    static class Inner implements I {
        void g() { helper(); }
    }
}`,
	})
	expect(t, got, map[string]string{"b/Main.java:6 helper call": "exact b.Main.helper@4"})
}

// A file whose import or header the parser rejects states no references —
// its extends clause neither —: its types' members and supertypes are not
// known, and what a subtype's lookup finds past them is a Candidate.
func TestBrokenFileTypesAreOpen(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"p/Base.java": "package p;\npublic class Base { public void m() {} public int f; }",
		"p/X.java":    "package p;\nimport java.util.;\npublic class X extends Base {}",
		"p/Outer.java": `package p;
class Outer {
    void m() {}
    int f;
    class Z extends X {
        void g() { m(); int y = f; }
    }
    void h(X x) { x.m(); }
}`,
	})
	expect(t, got, map[string]string{
		"p/Outer.java:6 m call":            "candidate p.Outer.m",
		"p/Outer.java:6 f value_reference": "candidate p.Outer.f",
		"p/Outer.java:8 m call":            "unresolved ",
	})
}

// A type variable's bound is read where the type parameter is declared: a
// class's type parameter section sees no member type of the class (JLS
// 6.3), a method's does.
func TestTypeVariableBoundScope(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"p/Entry.java": "package p;\npublic class Entry { public void k(int x) {} }",
		"p/Cache.java": `package p;
public class Cache<E extends Entry> {
    E head;
    static class Entry { void k(int x) {} }
    void f(E e) { head.k(1); e.k(2); }
    <F extends Entry> void g(F f) { f.k(3); }
    class Inner { void h(E e) { e.k(4); } }
}`,
	})
	expect(t, got, map[string]string{
		"p/Cache.java:5 k call": "exact p.Entry.k",
		"p/Cache.java:6 k call": "exact p.Cache.Entry.k",
		"p/Cache.java:7 k call": "exact p.Entry.k",
	})
}

// I.super.d() calls the default method of the interface named: d of I.
func TestInterfaceSuperCall(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"p/I.java": "package p;\npublic interface I { default void d() {} }",
		"p/C.java": "package p;\nclass C implements I {\n    public void d() { I.super.d(); }\n}",
	})
	expect(t, got, map[string]string{"p/C.java:3 d call": "exact p.I.d"})
}

// A protected callable of another package the call may not access does not
// end the phases (JLS 15.12.2): an accessible one a later phase finds — by
// boxing, as variadic — stays a candidate beside it.
func TestProtectedOfAnotherPackageInPhases(t *testing.T) {
	got := resolveRepoLines(t, map[string]string{
		"a/Base.java": `package a;
public class Base {
    protected void m(int x) {}
    public void m(Object x) {}
    protected void v(int x) {}
    public void v(int x, int... rest) {}
    protected Base(int x) {}
    public Base(Object x) {}
    protected static void s(int x) {}
    public static void s(long... x) {}
}`,
		"b/Main.java": `package b;
import static a.Base.*;
import a.Base;
class Main {
    void f(Base o) {
        o.m(1);
        o.v(1);
        new Base(1);
        Base.s(1);
        s(1);
    }
}`,
	})
	expect(t, got, map[string]string{
		"b/Main.java:6 m call":      "candidate a.Base.m@3|a.Base.m@4",
		"b/Main.java:7 v call":      "candidate a.Base.v@5|a.Base.v@6",
		"b/Main.java:8 <init> call": "candidate a.Base.<init>@7|a.Base.<init>@8",
		"b/Main.java:9 s call":      "candidate a.Base.s@10|a.Base.s@9",
		"b/Main.java:10 s call":     "candidate a.Base.s@10|a.Base.s@9",
	})
}

// A member path through a type of a broken file (its members and
// supertypes cannot be listed): what it does not show is unknown — never
// outside the repository — and what is found past it is a Candidate.
func TestMemberPathThroughABrokenType(t *testing.T) {
	got := resolveRepo(t, map[string]string{
		"p/Base.java": "package p;\npublic class Base { public static class Nest { public void m() {} } }",
		"p/X.java":    "package p;\nimport java.util.;\npublic class X extends Base { }",
		"p/Use.java": `package p;
class Use {
    X.Nest v = new X.Nest();
    void m() {}
    class Z extends X.Nest { void g() { m(); } }
}`,
	})
	expect(t, got, map[string]string{
		"p/Use.java:3 Nest type_use":     "unresolved ",
		"p/Use.java:3 Nest construction": "unresolved ",
		"p/Use.java:5 Nest inheritance":  "unresolved ",
		"p/Use.java:5 m call":            "candidate p.Use.m",
	})
}
