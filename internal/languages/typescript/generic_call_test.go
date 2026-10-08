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

// misparsedGenericCalls is valid TypeScript whose two generic calls
// gotreesitter v0.55.1 derives as comparisons, without an error. The defect
// depends on the context: the first statement alone parses as a call.
const misparsedGenericCalls = `const c = processors.aggregateChecks<number>(inst).minimum ?? null;
z.number().apply<z.ZodMiniNumber>((schema) => schema.check(0));
`

// parsedAsCall is the first statement of misparsedGenericCalls on its own,
// which the parser reads as a call: the control for the contract below.
const parsedAsCall = "const c = processors.aggregateChecks<number>(inst).minimum ?? null;\n"

// --- gotreesitter v0.55.1 behavior (the upgrade gate) ---
//
// It pins the misparse the recovery exists for. A failure after a
// gotreesitter upgrade means the parser changed: the contract test below must
// still pass; re-measure the recovery (ARCHITECTURE.md §3) before updating
// this one.
func TestParserBehavior_GenericCallsDerivedAsComparisons(t *testing.T) {
	lang := grammars.TypescriptLanguage()
	tree, err := tsparse.Parse(lang, []byte(misparsedGenericCalls))
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Release()
	if tree.RootNode().HasError() {
		t.Fatal("the misparse has no error: the tree must not have one")
	}
	want := "(program " +
		// (processors.aggregateChecks < number) > ((inst).minimum) ?? null
		"(lexical_declaration (variable_declarator (identifier) (binary_expression (binary_expression " +
		"(binary_expression (member_expression (identifier) (property_identifier)) (identifier)) " +
		"(member_expression (parenthesized_expression (identifier)) (property_identifier))) (null)))) " +
		// (z.number().apply < z.ZodMiniNumber) > ((schema) => ...)
		"(expression_statement (binary_expression (binary_expression " +
		"(member_expression (call_expression (member_expression (identifier) (property_identifier)) (arguments)) (property_identifier)) " +
		"(member_expression (identifier) (property_identifier))) " +
		"(parenthesized_expression (arrow_function (formal_parameters (required_parameter (identifier))) " +
		"(call_expression (member_expression (identifier) (property_identifier)) (arguments (number))))))))"
	if got := tree.RootNode().SExpr(lang); got != want {
		t.Errorf("gotreesitter's tree changed:\n got %s\nwant %s", got, want)
	}
	ctl, err := tsparse.Parse(lang, []byte(parsedAsCall))
	if err != nil {
		t.Fatal(err)
	}
	defer ctl.Release()
	wantCtl := "(program (lexical_declaration (variable_declarator (identifier) (binary_expression " +
		"(member_expression (call_expression (member_expression (identifier) (property_identifier)) " +
		"(type_arguments (predefined_type)) (arguments (identifier))) (property_identifier)) (null)))))"
	if got := ctl.RootNode().SExpr(lang); ctl.RootNode().HasError() || got != wantCtl {
		t.Errorf("the control is no longer parsed as a call:\n got %s\nwant %s", got, wantCtl)
	}
}

// --- The contract Ark's users rely on (any parser behavior) ---

// The generic calls and their type arguments are extracted exactly — kind,
// name, receiver, position, once each — however the parser derives them.
func TestExtract_GenericCallReadAsComparison(t *testing.T) {
	want := []string{
		"call processors.aggregateChecks L1:22",
		"call schema.check L2:54",
		"call z.number().apply L2:12", // the receiver is kept verbatim
		"call z.number L2:3",
		"type_use z.ZodMiniNumber L2:18",
	}
	slices.Sort(want)
	if got := refKeys(t, misparsedGenericCalls, "call", "type_use", "construction"); !slices.Equal(got, want) {
		t.Errorf("misparsed source:\n got %v\nwant %v", got, want)
	}
	// The statement parsed as a call yields the same reference.
	if got := refKeys(t, parsedAsCall, "call", "type_use", "construction"); !slices.Equal(got, want[:1]) {
		t.Errorf("control:\n got %v\nwant %v", got, want[:1])
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

// typeArgsAccepted reports whether the recovery's type grammar accepts all of
// args as type arguments.
func typeArgsAccepted(args string) bool {
	p := &typeArgParser{src: []byte(args + "(x)")}
	return p.typeArgs() && p.pos == len(args)
}

// The type grammar accepts only what TypeScript parses as type arguments
// (checked against the compiler by TestFidelity_TypeArgumentsMatchCompiler);
// everything else is rejected, never guessed.
func TestTypeArgs_AcceptOnlyWhatTypeScriptParses(t *testing.T) {
	for _, a := range []string{
		"<number>", "<string, number>", "<Array<string>>", "<Map<K, Array<V>>>", "<T[K]>",
		"<a.b.C>", "<typeof x>", "<typeof x.y>", "<keyof T>", "<A | B>", "<| A | B>", "<A & B>",
		"<A[]>", "<[A, B]>", "<[]>", "<{ a: A }>", "<{ a?: A; b: B }>", "<{ if: A }>", "<1>",
		"<-1>", "<1.5>", "<0>", "<1n>", "<'a'>", `<"a\"b">`, `<"\x41\u0041">`, `<"\0">`, "<café>",
		"<A /* c */>", "<A // c\n>", "<A<B<C>>>", "<A,>", "<this>", "<A['k']>", "<out>",
		"<let>", "<type>", "<a.if>", "<z.infer<typeof s>>",
	} {
		if !typeArgsAccepted(a) {
			t.Errorf("%q rejected", a)
		}
	}
	for _, a := range []string{
		"<T extends U>", "<() => void>", "<{ [K in keyof T]: T[K] }>", "<A extends B ? C : D>",
		"<infer A>", "<1.2.3>", "<1__0>", "<1_0>", "<1_>", "<01>", "<00>", "<1e3>", "<0x1F>",
		"<1.>", "<.5>", "<1x>", "<- 1>", "<if>", "<new>", "<class>", "<extends>", "<infer>",
		"<readonly A>", "<unique symbol>", "<typeof if>", "<a\u00a0b>", "<a\u2028b>",
		"<\xff>", "<a\xffb>", `<\u0041>`, `<a\u0041>`, `<"\x4">`, `<"\u00">`, `<'\01'>`, `<"\1">`, `<"\9">`, `<"\08">`,
		`<"a>`, "<'a\nb'>", "<`a`>", "<A /* c>", "<A\n<B>>", "<A\n[]>", "<A // c\n[K]>",
		"<a // c\n.b>", "<a. b>", "<A || B>", "<A && B>", "<>", "<,A>", "<A,,B>", "<A B>",
		"<A?>", "<!A>", "<(A)>", "<A = B>", "<{ a }>",
	} {
		if typeArgsAccepted(a) {
			t.Errorf("%q accepted", a)
		}
	}
}
