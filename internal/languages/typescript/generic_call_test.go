package typescript

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/magicdrive/ark/internal/tsparse"
)

// refKeys renders the references of kinds as "kind recv.name L<line>:<col>".
func refKeys(t *testing.T, src string, kinds ...string) []string {
	t.Helper()
	ext := extractTS(t, src)
	var out []string
	for _, r := range ext.References {
		if slices.Contains(kinds, r.Kind) {
			out = append(out, fmt.Sprintf("%s %s.%s L%d:%d", r.Kind, r.ReceiverExpr, r.Name, r.Location.Range.Start.Line, r.Location.Range.Start.Column))
		}
	}
	slices.Sort(out)
	return out
}

// The parser derives these generic calls as comparisons, without an error;
// TypeScript parses them as calls.
func TestExtract_GenericCallReadAsComparison(t *testing.T) {
	src := `const c = processors.aggregateChecks<number>(inst).minimum ?? null;
z.number().apply<z.ZodMiniNumber>((schema) => schema.check(0));
`
	tree, _ := tsparse.Parse(grammars.TypescriptLanguage(), []byte(src))
	if sx := tree.RootNode().SExpr(grammars.TypescriptLanguage()); !strings.Contains(sx, "binary_expression (binary_expression") {
		t.Log("the parser now reads these as calls: the recovery is no longer exercised")
	}
	tree.Release()
	got := refKeys(t, src, "call", "type_use")
	want := []string{
		"call processors.aggregateChecks L1:22",
		"call schema.check L2:54",
		"call z.number().apply L2:12", // the receiver is kept verbatim
		"call z.number L2:3",
		"type_use z.ZodMiniNumber L2:18",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

// Only text TypeScript itself parses as type arguments followed by `(` is a
// call; comparisons stay comparisons.
func TestExtract_ComparisonsAreNotGenericCalls(t *testing.T) {
	for _, src := range []string{
		"const a = i < n && m > (k);\n",
		"if (a < b || c > (d)) {}\n",
		"const c = a < b.c > d;\n",
		"const d = a < 1 + 2 > (3);\n",
		"const e = a < b > c && (d);\n",
		"for (let i = 0; i < n; i++) { if (x > (y)) {} }\n",
	} {
		if got := refKeys(t, src, "call"); len(got) != 0 {
			t.Errorf("%q: comparison read as a call: %v", src, got)
		}
	}
}

// Type parameters of nested signatures, mapped-type keys and `infer` names
// are declarations, not type uses; `bigint` is a keyword. The types around
// them are still type uses.
func TestExtract_TypeScopedNamesAreNotTypeUses(t *testing.T) {
	src := `type M<T> = { [K in keyof T as Keys]: T[K] };
type C<T> = T extends (infer U extends Base)[] ? U : never;
type F = <V>(v: V) => Out<V>;
type I = { m<W>(w: W): W; new <X>(x: X): Y; <Z>(z: Z): Z };
let n: bigint;
`
	got := refKeys(t, src, "type_use")
	var names []string
	for _, k := range got {
		names = append(names, strings.Fields(k)[1])
	}
	slices.Sort(names)
	names = slices.Compact(names)
	if want := []string{".Base", ".Keys", ".Out", ".Y"}; !slices.Equal(names, want) {
		t.Errorf("type uses %v, want %v (all: %v)", names, want, got)
	}
}
