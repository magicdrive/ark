package tsparse

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// --- The forest probe (comments blanked before the forest route) ---

// commentedRangeLiteral is nestedRangeLiteral with comments: the production
// and candidate routes still reject it, the forest route still parses it, so
// the probe runs and must accept.
var commentedRangeLiteral = strings.Replace(strings.Replace(nestedRangeLiteral,
	"func v2() {", "// v2 does things.\n/* a block\n   comment */\nfunc v2() { // trailing", 1),
	"if v13() > v13() {", "// before the if\n\t\t\tif v13() > v13() { /* inline */", 1)

// attributedComments is a Rust attribute followed by n doc comments and a
// `try!` call, which the Rust grammar rejects (try is a keyword): the forest
// route declines it after comparing its alternatives across every comment,
// in time cubic in n.
func attributedComments(n int) []byte {
	var b strings.Builder
	b.WriteString("#![warn(missing_docs)]\n")
	for i := 0; i < n; i++ {
		b.WriteString("//! doc line of the crate\n")
	}
	b.WriteString("fn f() -> Result<(), E> {\n    try!(g());\n    Ok(())\n}\n")
	return []byte(b.String())
}

func traced(t *testing.T, lang *ts.Language, src []byte) (*ts.Tree, route, routeTrace) {
	t.Helper()
	var tr routeTrace
	tree, r, err := parseTraced(lang, src, &tr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Release)
	return tree, r, tr
}

func TestProbe_DeclineSkipsTheForestRoute(t *testing.T) {
	lang := grammars.RustLanguage()
	src := attributedComments(20)
	if forestClean(t, lang, string(src)) {
		t.Fatal("precondition: the forest route must decline attributedComments")
	}
	tree, r, tr := traced(t, lang, src)
	if r != routeProduction || !tree.RootNode().HasError() {
		t.Errorf("route %s (error %v), want the production tree with its error", r, tree.RootNode().HasError())
	}
	if tr.probes != 1 || tr.forests != 0 {
		t.Errorf("probes %d, forest runs %d; want 1 probe and no forest run", tr.probes, tr.forests)
	}
	if prod := production(t, lang, string(src)); tree.RootNode().SExpr(lang) != prod.RootNode().SExpr(lang) {
		t.Error("the kept tree is not the production tree")
	}
}

func TestProbe_AcceptRunsTheForestRoute(t *testing.T) {
	lang := grammars.GoLanguage()
	if !production(t, lang, commentedRangeLiteral).RootNode().HasError() || !candidate(t, lang, commentedRangeLiteral).RootNode().HasError() {
		t.Fatal("precondition: the production and candidate routes must reject commentedRangeLiteral")
	}
	if !forestClean(t, lang, commentedRangeLiteral) {
		t.Fatal("precondition: the forest route must parse commentedRangeLiteral")
	}
	tree, r, tr := traced(t, lang, []byte(commentedRangeLiteral))
	if r != routeForest || tr.probes != 1 || tr.forests != 1 {
		t.Errorf("route %s, probes %d, forest runs %d; want the forest route after one probe", r, tr.probes, tr.forests)
	}
	// The returned tree is the forest route's tree of the source itself,
	// comments included — never the probe's.
	if !strings.Contains(tree.RootNode().SExpr(lang), "comment") {
		t.Error("the returned tree lost the comments: it is the probe's tree")
	}
}

// A source without a comment is not probed: the probe would be the forest
// route itself.
func TestProbe_NothingToBlankIsNotProbed(t *testing.T) {
	_, r, tr := traced(t, grammars.GoLanguage(), []byte(nestedRangeLiteral))
	if r != routeForest || tr.probes != 0 || tr.forests != 1 {
		t.Errorf("route %s, probes %d, forest runs %d; want the forest route without a probe", r, tr.probes, tr.forests)
	}
}

// The probe answers what the forest route answers: whether it derives the
// source (the equivalence the probe rests on), on inputs both routes reach.
func TestProbe_AgreesWithTheForestRoute(t *testing.T) {
	cases := []struct {
		name string
		lang *ts.Language
		src  []byte
	}{
		{"go forest", grammars.GoLanguage(), []byte(commentedRangeLiteral)},
		{"go invalid", grammars.GoLanguage(), []byte("package app\n// c\nfunc Broken( { /* c */\n")},
		// A line break inside a block comment ends no statement: blanked
		// with it kept, `x := a` would end before `.b`, which alone is no
		// statement.
		{"go multi-line comment", grammars.GoLanguage(), []byte("package p\nfunc f() {\n\tx := a /*\n*/ .b\n\t_ = x\n}\n")},
		{"rust attributes", grammars.RustLanguage(), attributedComments(8)},
		{"rust try", grammars.RustLanguage(), []byte("/// doc\nfn f() { /* c */ try!(x); }\n")},
	}
	for _, c := range cases {
		prod := production(t, c.lang, string(c.src))
		blanked, ok := blankComments(c.lang, probeComments[c.lang.Name], prod.RootNode(), c.src)
		if !ok {
			t.Fatalf("%s: precondition: a comment to blank", c.name)
		}
		if got, want := forestClean(t, c.lang, string(blanked)), forestClean(t, c.lang, string(c.src)); got != want {
			t.Errorf("%s: the probe derives %v, the forest route %v", c.name, got, want)
		}
	}
}

func TestBlankComments(t *testing.T) {
	lang := grammars.RustLanguage()
	src := []byte("// line\nfn f() { /* block\n x */ g(); }\n/// doc\nfn h() {}\n")
	blanked, ok := blankComments(lang, probeComments["rust"], production(t, lang, string(src)).RootNode(), src)
	if !ok {
		t.Fatal("nothing blanked")
	}
	want := []byte("       \nfn f() { " + strings.Repeat(" ", len("/* block\n x */")) + " g(); }\n       \nfn h() {}\n")
	if !bytes.Equal(blanked, want) {
		t.Errorf("blanked:\n%q\nwant:\n%q", blanked, want)
	}
	if len(blanked) != len(src) {
		t.Error("blanking moved an offset")
	}
	if string(src) != "// line\nfn f() { /* block\n x */ g(); }\n/// doc\nfn h() {}\n" {
		t.Error("blanking changed the source")
	}
	if _, ok := blankComments(lang, probeComments["rust"], production(t, lang, "fn f() {}\n").RootNode(), []byte("fn f() {}\n")); ok {
		t.Error("a source without a comment was blanked")
	}

	// What an ERROR holds is not blanked: its comments may have been lexed
	// by error recovery, and the ERROR itself is not a comment.
	bad := []byte("fn f() { try!(x /* c */); }\n")
	tree := production(t, lang, string(bad))
	if !tree.RootNode().HasError() {
		t.Fatal("precondition: an error")
	}
	if blanked, ok := blankComments(lang, probeComments["rust"], tree.RootNode(), bad); ok && !bytes.Contains(blanked, []byte("try")) {
		t.Errorf("an ERROR was blanked: %q", blanked)
	}
	inError := []byte("fn f() { let = /* c */ ; }\n")
	if blanked, _ := blankComments(lang, probeComments["rust"], production(t, lang, string(inError)).RootNode(), inError); blanked != nil && !bytes.Contains(blanked, []byte("/* c */")) {
		t.Errorf("a comment inside an ERROR was blanked: %q", blanked)
	}
}

// The probe keeps the parse of a run of commented, attributed code linear:
// four times the comments cost about four times the time, where the forest
// route alone took about sixty-four times as long (cubic).
func TestProbe_ScalesLinearly(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	lang := grammars.RustLanguage()
	best := func(src []byte) time.Duration {
		d := time.Duration(1 << 62)
		for i := 0; i < 3; i++ {
			start := time.Now()
			tree, _, err := parseRoute(lang, src)
			if err != nil {
				t.Fatal(err)
			}
			tree.Release()
			if e := time.Since(start); e < d {
				d = e
			}
		}
		return d
	}
	small, large := best(attributedComments(500)), best(attributedComments(2000))
	t.Logf("500 comments %v, 2000 comments %v (x%.1f)", small, large, float64(large)/float64(small))
	if large > 10*small {
		t.Errorf("4x the comments took %.1fx the time (%v, %v): the parse is not linear", float64(large)/float64(small), small, large)
	}
}

// Only a comment is blanked: text that looks like one inside a string, a
// raw string or a character literal is kept, and a nested block comment is
// blanked whole.
func TestBlankComments_KeepsStringsAndLiterals(t *testing.T) {
	for _, c := range []struct {
		name string
		lang *ts.Language
		src  string
		keep []string
	}{
		{"rust", grammars.RustLanguage(), "fn f() { let a = \"// x /* y */\"; let b = r#\"// z\"#; let c = '/'; /* a /* nested */ b */ }\n// tail\n",
			[]string{`"// x /* y */"`, `r#"// z"#`, `'/'`}},
		{"go", grammars.GoLanguage(), "package p\nvar a = \"// x\"\nvar b = `/* y */ // z`\nvar c = '/'\n// tail\n/* block */\n",
			[]string{`"// x"`, "`/* y */ // z`", `'/'`}},
	} {
		src := []byte(c.src)
		blanked, ok := blankComments(c.lang, probeComments[c.lang.Name], production(t, c.lang, c.src).RootNode(), src)
		if !ok {
			t.Fatalf("%s: nothing blanked", c.name)
		}
		for _, k := range c.keep {
			if !bytes.Contains(blanked, []byte(k)) {
				t.Errorf("%s: %s was blanked: %q", c.name, k, blanked)
			}
		}
		for _, gone := range []string{"tail", "nested", "block"} {
			if bytes.Contains(blanked, []byte(gone)) && bytes.Contains(src, []byte(gone)) {
				t.Errorf("%s: comment text %q kept: %q", c.name, gone, blanked)
			}
		}
		if len(blanked) != len(src) {
			t.Errorf("%s: length changed", c.name)
		}
	}
}

// Where the parse decides the tokens, no probe runs: the forest route runs
// as it did before the probe.
func TestProbe_OnlyForTokenStableLanguages(t *testing.T) {
	for _, c := range []struct {
		lang *ts.Language
		src  string
	}{
		{grammars.TypescriptLanguage(), "// c\nexport function f( { /* c */\n"},
		{grammars.TsxLanguage(), "// c\nexport const A = () => <div>// text</div>;\nexport function f( {\n"},
		{grammars.JavascriptLanguage(), "// c\nconst r = a / b / c; function f( {\n"},
		{grammars.PhpLanguage(), "<?php\n// c ?> html <?php\nfunction f( {\n"},
		{grammars.PythonLanguage(), "# c\ndef f(:\n    pass\n"},
		{grammars.HclLanguage(), "# c\nresource \"a\" \"b\" {\n  x = \n"},
	} {
		if !production(t, c.lang, c.src).RootNode().HasError() {
			t.Fatalf("%s: precondition: the production route must reject the source", c.lang.Name)
		}
		_, _, tr := traced(t, c.lang, []byte(c.src))
		if tr.probes != 0 {
			t.Errorf("%s: %d probes; the probe is only for Go and Rust", c.lang.Name, tr.probes)
		}
	}
}

// The production route's parsers are pooled: parsing in parallel, every
// tree equals the tree parsed alone (no state crosses parses), for valid
// and invalid sources of several languages. Run with -race.
func TestParse_PooledParsersShareNoState(t *testing.T) {
	type input struct {
		lang *ts.Language
		src  string
	}
	inputs := []input{
		{grammars.GoLanguage(), nestedRange},
		{grammars.GoLanguage(), nestedRangeLiteral},
		{grammars.GoLanguage(), commentedRangeLiteral},
		{grammars.GoLanguage(), routesDiffer},
		{grammars.GoLanguage(), candidateOnly},
		{grammars.RustLanguage(), string(attributedComments(5))},
		{grammars.RustLanguage(), "/// doc\npub struct S { a: u8 }\nimpl S { fn f(&self) {} }\n"},
		{grammars.TypescriptLanguage(), "export class A { m(): void {} }\n"},
		{grammars.TypescriptLanguage(), "export function f( {\n"},
		{grammars.PhpLanguage(), "<?php\nclass A { function m() {} }\n"},
		{grammars.PythonLanguage(), "def f():\n    return 1\n"},
	}
	want := make([]string, len(inputs))
	for i, in := range inputs {
		want[i] = parse(t, in.lang, in.src).RootNode().SExpr(in.lang)
	}
	const workers, rounds = 8, 25
	errs := make(chan string, workers*rounds*len(inputs))
	done := make(chan struct{})
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer func() { done <- struct{}{} }()
			for r := 0; r < rounds; r++ {
				for k := range inputs {
					i := (k + w + r) % len(inputs)
					tree, err := Parse(inputs[i].lang, []byte(inputs[i].src))
					if err != nil {
						errs <- err.Error()
						continue
					}
					if got := tree.RootNode().SExpr(inputs[i].lang); got != want[i] {
						errs <- "input " + string(rune('0'+i)) + ": tree differs from the tree parsed alone"
					}
					tree.Release()
				}
			}
		}(w)
	}
	for w := 0; w < workers; w++ {
		<-done
	}
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// A source the forest route parses is never skipped: comments inserted at
// every whitespace of a source only the forest route parses leave it
// derivable, and Parse still returns the forest route's tree.
func TestProbe_NeverSkipsADerivableSource(t *testing.T) {
	lang := grammars.GoLanguage()
	base := nestedRangeLiteral
	checked := 0
	for i := 0; i < len(base); i++ {
		if base[i] != ' ' && base[i] != '\n' {
			continue
		}
		for _, c := range []string{" /* c */ ", " // c\n", " /*\n*/ "} {
			if c[1] == '/' && c[2] == '/' && base[i] != '\n' {
				continue // a line comment only where a line ends
			}
			src := base[:i] + c + base[i+1:]
			if !production(t, lang, src).RootNode().HasError() || !candidate(t, lang, src).RootNode().HasError() || !forestClean(t, lang, src) {
				continue
			}
			checked++
			if _, r, tr := traced(t, lang, []byte(src)); r != routeForest || tr.forests != 1 {
				t.Errorf("comment at %d: route %s (probes %d, forest runs %d), want the forest route", i, r, tr.probes, tr.forests)
			}
		}
	}
	if checked < 20 {
		t.Fatalf("only %d variants reach the forest route", checked)
	}
	t.Logf("%d commented variants take the forest route", checked)
}

// TestProbe_Corpus compares Parse with the routes as they were before the
// probe and the parser pool — fresh parsers, the forest route whenever the
// others erred — file by file: the same route and the same tree. It runs
// only when ARK_TSPARSE_CORPUS lists "language:extension:directory" entries
// separated by ";" (languages: go, rust, typescript, tsx, javascript,
// python, php, hcl), and logs, per entry, the files compared and their
// totals of time. Measured before release on 18,104 files of 8 languages
// (docs/performance.md).
func TestProbe_Corpus(t *testing.T) {
	spec := os.Getenv("ARK_TSPARSE_CORPUS")
	if spec == "" {
		t.Skip("ARK_TSPARSE_CORPUS not set")
	}
	langs := map[string]*ts.Language{"go": grammars.GoLanguage(), "rust": grammars.RustLanguage(), "typescript": grammars.TypescriptLanguage(),
		"tsx": grammars.TsxLanguage(), "javascript": grammars.JavascriptLanguage(), "python": grammars.PythonLanguage(), "php": grammars.PhpLanguage(), "hcl": grammars.HclLanguage()}
	for _, entry := range strings.Split(spec, ";") {
		f := strings.SplitN(entry, ":", 3)
		lang := langs[f[0]]
		if len(f) != 3 || lang == nil {
			t.Fatalf("entry %q: want language:extension:directory", entry)
		}
		var files, erring int
		var before, after time.Duration
		filepath.Walk(f[2], func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, f[1]) {
				return nil
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			files++
			start := time.Now()
			wantTree, wantRoute, prodErr := unprobedRoute(lang, src)
			before += time.Since(start)
			if prodErr {
				erring++
			}
			start = time.Now()
			tree, r, err := parseRoute(lang, src)
			after += time.Since(start)
			if err != nil {
				t.Errorf("%s: %v", p, err)
				return nil
			}
			if got := tree.RootNode().SExpr(lang); got != wantTree || r != wantRoute {
				t.Errorf("%s: route %s, want %s; trees equal %v", p, r, wantRoute, got == wantTree)
			}
			tree.Release()
			return nil
		})
		t.Logf("%s %s: %d files (%d the production route rejects) identical; before %v, now %v", f[0], f[2], files, erring, before, after)
	}
}

// unprobedRoute is Parse as it was before the probe and the pool: the tree
// (as an S-expression) and route of fresh parsers, the forest route tried
// whenever the production and candidate routes err.
func unprobedRoute(lang *ts.Language, src []byte) (string, route, bool) {
	p := ts.NewParser(lang)
	p.SetAdmissionCandidateRoute(false)
	tree, err := p.Parse(src)
	if err != nil || tree == nil {
		return "", "", false
	}
	defer tree.Release()
	if !tree.RootNode().HasError() {
		return tree.RootNode().SExpr(lang), routeProduction, false
	}
	alt := ts.NewParser(lang)
	alt.SetAdmissionCandidateRoute(true)
	if c, err := alt.Parse(src); c != nil && err == nil && !c.RootNode().HasError() {
		defer c.Release()
		return c.RootNode().SExpr(lang), routeCandidate, true
	}
	if f, ok := ts.NewParser(lang).ParseForestExperimental(src); f != nil && ok && !f.RootNode().HasError() {
		defer f.Release()
		return f.RootNode().SExpr(lang), routeForest, true
	}
	return tree.RootNode().SExpr(lang), routeProduction, true
}

// A Go block comment holding a line break is one extra for tree-sitter-go:
// its line break is no `\n` terminator token. The probe blanks the line
// break too, or the copy would hold a terminator the source does not,
// decline, and skip a forest parse that succeeds (found by review). The
// comment must lie outside the production tree's ERROR, or nothing is
// blanked and no probe runs (the first case checks that one does).
func TestProbe_GoMultilineComment(t *testing.T) {
	lang := grammars.GoLanguage()
	for i, src := range []string{
		strings.Replace(nestedRangeLiteral, "func v2() {", "func w() {\n\tx := a /*\n*/ .b\n\t_ = x\n}\nfunc v2() {", 1),
		strings.Replace(nestedRangeLiteral, "v13() > v13()", "v13() /*\n*/ > v13()", 1),
		strings.Replace(nestedRangeLiteral, `v6.v14("")`, "v6 /*\n*/ .v14(\"\")", 1),
	} {
		if !production(t, lang, src).RootNode().HasError() || !candidate(t, lang, src).RootNode().HasError() || !forestClean(t, lang, src) {
			t.Fatal("precondition: only the forest route parses the source")
		}
		_, r, tr := traced(t, lang, []byte(src))
		if r != routeForest {
			t.Errorf("case %d: route %s, want the forest route", i, r)
		}
		if i == 0 && tr.probes != 1 {
			t.Errorf("case %d: %d probes, want the probe to run", i, tr.probes)
		}
	}
}
