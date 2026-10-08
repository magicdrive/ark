package typescript

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// typeArgCases are type-argument texts at the edges of the recovery's type
// grammar: accepted ones, ones TypeScript reads differently, malformed ones.
var typeArgCases = []string{
	"<number>", "<string, number>", "<Array<string>>", "<Map<K, Array<V>>>",
	"<T extends U>", "<() => void>", "<T[K]>", "<{ [K in keyof T]: T[K] }>",
	"<a.b.C>", "<typeof x>", "<typeof x.y>", "<keyof T>", "<A | B>", "<A & B>",
	"<| A | B>", "<A || B>", "<A && B>", "<A[]>", "<A[][]>", "<[A, B]>", "<[]>",
	"<[A,]>", "<{ a: A }>", "<{ a?: A; b: B }>", "<{ a: A, }>", "<{ readonly a: A }>",
	"<{ a }>", "<{}>", "<1>", "<-1>", "<- 1>", "<1.5>", "<1.2.3>", "<1__0>", "<1_0>",
	"<1_>", "<0x1F>", "<01>", "<1n>", "<1e3>", "<.5>", "<'a'>", `<"a">`, `<"a\"b">`,
	`<"a>`, "<'a\nb'>", "<`a`>", "<readonly A>", "<readonly A[]>", "<unique symbol>",
	"<unique A>", "<if>", "<new>", "<class>", "<extends>", "<void>", "<this>", "<null>",
	"<undefined>", "<never>", "<true>", "<object>", "<bigint>", "<café>", "<日本>",
	"<a b>", "<a b>", "<\xff>", "<a\xffb>", "<\\u0041>", "<A /* c */>",
	"<A // c\n>", "<A /* c>", "</* c */ A>", "<A<B>>", "<A<B<C>>>", "<A>>", "<<A>>",
	"<>", "<,A>", "<A,>", "<A,,B>", "<A B>", "<A.>", "<.A>", "<A?>", "<!A>", "<(A)>",
	"<A = B>", "<A extends B ? C : D>", "<infer A>", "<A.B<C>>", "<A['k']>", "<A[0]>",
}

// randomTypeArgs generates type-argument-like texts from an alphabet of
// tokens, valid and not, deterministically.
func randomTypeArgs(n int) []string {
	toks := []string{
		"A", "b", "Foo", "x.y", "_a", "$b", "é", "if", "new", "typeof", "keyof", "readonly",
		"unique", "symbol", "string", "number", "extends", "infer", "1", "1.5", "1.2.3", "1_0",
		"1__0", "0x1F", "01", "1n", "-", "'s'", `"s"`, `"s`, "`t`", "/* c */", "// c\n",
		"<", ">", ",", "[", "]", "{", "}", "|", "&", ":", ";", "?", ".", "(", ")", "=>", " ", "\n",
		`"\x4"`, `"\x41"`, `"\u0041"`, `"\u00"`, `'\0'`, `'\01'`, "\"a\\\nb\"", `\u0041`, `a\u0041`,
		"\u00a0", "\u2028", "\xff", "日本", "1n", "0", "00", "1.", "1e3", "this", "void", "class",
		"out", "let", "type", "z.infer", "a.if", "function", "await",
	}
	r := rand.New(rand.NewSource(1))
	out := make([]string, 0, n)
	for len(out) < n {
		var b strings.Builder
		b.WriteString("<")
		for k := r.Intn(8) + 1; k > 0; k-- {
			b.WriteString(toks[r.Intn(len(toks))])
		}
		b.WriteString(">")
		out = append(out, b.String())
	}
	return out
}

// TestFidelity_TypeArgumentsMatchCompiler: every type-argument text the
// recovery accepts in `f<args>(x)` the TypeScript compiler parses as that
// call, without errors, with the same type references. Needs the compiler
// (oracle_test.go). ARK_TYPEARGS_CASES sets how many random cases (seed 1)
// are added to the fixed ones; default 20000.
func TestFidelity_TypeArgumentsMatchCompiler(t *testing.T) {
	node, tsmod := tsOracle(t)
	script, err := filepath.Abs(filepath.Join("testdata", "fidelity", "typeargs.js"))
	if err != nil {
		t.Fatal(err)
	}
	cases := append(slices.Clone(typeArgCases), randomTypeArgs(envInt(t, "ARK_TYPEARGS_CASES", 20000))...)
	dir := t.TempDir()
	in, out := filepath.Join(dir, "cases.json"), filepath.Join(dir, "out.json")
	b, _ := json.Marshal(cases)
	if err := os.WriteFile(in, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command(node, script, tsmod, in, out).CombinedOutput(); err != nil {
		t.Fatalf("typeargs.js: %v\n%s", err, b)
	}
	b, err = os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var res []struct {
		Call bool    `json:"call"`
		Refs [][]any `json:"refs"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	accepted, agreeing := 0, 0
	for i, args := range cases {
		src := "f" + args + "(x);"
		p := &typeArgParser{src: []byte(src), pos: 1}
		if !p.typeArgs() {
			continue
		}
		if p.pos != 1+len(args) {
			continue // closed before the end of args: a different split
		}
		accepted++
		if !res[i].Call {
			t.Errorf("accepted %q, which TypeScript does not parse as the call f<...>(x)", args)
			continue
		}
		var got, want []string
		for _, r := range p.refs {
			got = append(got, fmt.Sprintf("%s.%s@%d", r.receiver, r.name, r.start))
		}
		for _, r := range res[i].Refs {
			want = append(want, fmt.Sprintf("%s.%s@%d", r[1], r[0], int(r[2].(float64))))
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%q: type references %v, TypeScript %v", args, got, want)
			continue
		}
		agreeing++
	}
	t.Logf("%d cases, %d accepted, %d agree with the compiler", len(cases), accepted, agreeing)
	if accepted < len(cases)/50 {
		// The grammar accepts ~10% of the random cases: far fewer means the
		// comparison no longer exercises it.
		t.Errorf("only %d of %d cases accepted", accepted, len(cases))
	}
}
