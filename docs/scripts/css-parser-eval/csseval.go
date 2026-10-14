//go:build ignore

// csseval compares CSS tokenizers on the css-parsing-tests suite (CSS Syntax
// Level 3) and on real stylesheets.
//
//	csseval conform TESTS_DIR      run each candidate on every *.json test file
//	csseval tokens FILE...         differential: token boundaries of spec vs tdewolff
//
// Candidates: "spec" (syntax.go, CSS Syntax Level 3 over the source bytes)
// and "tdewolff" (github.com/tdewolff/parse/v2/css's lexer: its token
// boundaries and types; values decoded by syntax.go from the token's bytes).
// Parsing (parse.go) is shared.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tdewolff/parse/v2"
	tdcss "github.com/tdewolff/parse/v2/css"
)

func specTokens(src []byte, legacy bool) []token {
	z := &tokenizer{src: src, legacy: legacy}
	z.pos = bomLength(src)
	var out []token
	for {
		t := z.nextToken()
		if t.typ == tEOF {
			return out
		}
		out = append(out, t)
	}
}

// tdTypes maps tdewolff's token types to the spec's.
var tdTypes = map[tdcss.TokenType]tokType{
	tdcss.IdentToken: tIdent, tdcss.FunctionToken: tFunction, tdcss.AtKeywordToken: tAtKeyword,
	tdcss.HashToken: tHash, tdcss.StringToken: tString, tdcss.BadStringToken: tBadString,
	tdcss.URLToken: tURL, tdcss.BadURLToken: tBadURL, tdcss.DelimToken: tDelim,
	tdcss.NumberToken: tNumber, tdcss.PercentageToken: tPercentage, tdcss.DimensionToken: tDimension,
	tdcss.UnicodeRangeToken: tUnicodeRange, tdcss.IncludeMatchToken: tMatch, tdcss.DashMatchToken: tMatch,
	tdcss.PrefixMatchToken: tMatch, tdcss.SuffixMatchToken: tMatch, tdcss.SubstringMatchToken: tMatch,
	tdcss.ColumnToken: tMatch, tdcss.WhitespaceToken: tWhitespace, tdcss.CDOToken: tCDO, tdcss.CDCToken: tCDC,
	tdcss.ColonToken: tColon, tdcss.SemicolonToken: tSemicolon, tdcss.CommaToken: tComma,
	tdcss.LeftBracketToken: tLBracket, tdcss.RightBracketToken: tRBracket,
	tdcss.LeftParenthesisToken: tLParen, tdcss.RightParenthesisToken: tRParen,
	tdcss.LeftBraceToken: tLBrace, tdcss.RightBraceToken: tRBrace, tdcss.CommentToken: tComment,
	tdcss.CustomPropertyNameToken: tIdent, // "--x": an ident to the standard
}

// tdTokens lexes src with tdewolff, keeping its boundaries and types; a
// token's value is what syntax.go decodes from the token's bytes (the
// first token there). It also returns the end offset tdewolff reached and
// its error.
func tdTokens(src []byte) ([]token, int, error) {
	in := parse.NewInputBytes(append([]byte(nil), src...))
	l := tdcss.NewLexer(in)
	var out []token
	off := 0
	for {
		tt, data := l.Next()
		if tt == tdcss.ErrorToken {
			return out, off, l.Err()
		}
		start := off
		off += len(data)
		typ, ok := tdTypes[tt]
		if !ok {
			typ = tDelim
		}
		t := token{typ: typ, start: start, end: off, value: string(data)}
		if sub := specTokens(src[start:off], true); len(sub) > 0 {
			s := sub[0]
			t.value, t.hashID, t.repr, t.num, t.integer, t.unit, t.ur0, t.ur1, t.eofIn =
				s.value, s.hashID, s.repr, s.num, s.integer, s.unit, s.ur0, s.ur1, s.eofIn && off == len(src)
			if typ == tDelim || typ == tMatch {
				t.value = string(data)
			}
		}
		out = append(out, t)
	}
}

func withoutComments(ts []token) []token {
	var out []token
	for _, t := range ts {
		if t.typ != tComment {
			out = append(out, t)
		}
	}
	return out
}

func run(kind string, toks []token) any {
	p := &parser{toks: withoutComments(toks)}
	switch kind {
	case "component_value_list":
		return p.componentValues()
	case "one_component_value":
		return p.oneComponentValue()
	case "declaration_list":
		return p.declarationList()
	case "blocks_contents":
		return p.blockContents()
	case "one_declaration":
		return p.oneDeclaration()
	case "one_rule":
		return p.oneRule()
	case "rule_list":
		return p.ruleList(false)
	case "stylesheet":
		return p.ruleList(true)
	}
	return nil
}

func canon(v any) string {
	b, _ := json.Marshal(v)
	var x any
	_ = json.Unmarshal(b, &x)
	b, _ = json.Marshal(x)
	return string(b)
}

func conform(dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	sort.Strings(files)
	kinds := []string{"component_value_list", "one_component_value", "declaration_list", "blocks_contents",
		"one_declaration", "one_rule", "rule_list", "stylesheet"}
	total := map[string][2]int{}
	for _, f := range files {
		kind := strings.TrimSuffix(filepath.Base(f), ".json")
		known := false
		for _, k := range kinds {
			known = known || k == kind
		}
		if !known {
			continue
		}
		b, _ := os.ReadFile(f)
		var tests []any
		if err := json.Unmarshal(b, &tests); err != nil {
			panic(err)
		}
		for _, cand := range []string{"spec", "tdewolff"} {
			pass, n := 0, 0
			for i := 0; i+1 < len(tests); i += 2 {
				input := []byte(tests[i].(string))
				var toks []token
				if cand == "spec" {
					toks = specTokens(input, true)
				} else {
					toks, _, _ = tdTokens(input)
				}
				got, want := canon(run(kind, toks)), canon(tests[i+1])
				n++
				if got == want {
					pass++
				} else if os.Getenv("SHOW") == cand {
					fmt.Printf("FAIL %s %s %q\n  got  %s\n  want %s\n", cand, kind, tests[i], got, want)
				}
			}
			fmt.Printf("%-9s %-21s %3d/%3d\n", cand, kind, pass, n)
			tt := total[cand]
			total[cand] = [2]int{tt[0] + pass, tt[1] + n}
		}
	}
	for _, c := range []string{"spec", "tdewolff"} {
		fmt.Printf("TOTAL %-9s %d/%d\n", c, total[c][0], total[c][1])
	}
}

func main() {
	switch os.Args[1] {
	case "conform":
		conform(os.Args[2])
	case "tokens":
		tokensDiff(os.Args[2:])
	case "rules":
		rulesDiff(os.Args[2:])
	case "outline":
		for _, f := range os.Args[2:] {
			src, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			fmt.Printf("== %s\n%s\n", filepath.Base(f), strings.Join(outline(src), "\n"))
		}
	case "structure":
		for _, f := range os.Args[2:] {
			src, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			r, d := structure(src)
			fmt.Printf("%s\t%d\t%d\n", f, r, d)
		}
	}
}

// tokensDiff compares the token boundaries and types of the two candidates
// (comments included) on real stylesheets, and whether tdewolff reads the
// whole input. tdewolff's url("x") (one URL token; to the standard a
// function token, a string and a ")") is read as the standard's tokens; every
// other difference is listed, with a category, and both streams resync at
// the next shared token boundary.
func tokensDiff(files []string) {
	short, n := 0, 0
	cats := map[string]int{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		n++
		a := specTokens(src, false)
		b, end, err := tdTokens(src)
		if end != len(src) {
			short++
			fmt.Printf("SHORT %s %d/%d %v\n", f, end, len(src), err)
		}
		i, j := 0, 0
		for i < len(a) && j < len(b) {
			x, y := a[i], b[j]
			if x.start == y.start && x.end == y.end && x.typ == y.typ {
				i++
				j++
				continue
			}
			cat := "other"
			switch {
			case y.typ == tURL && x.typ == tFunction && x.start == y.start:
				cat = "quoted-url" // tdewolff's url("x"): one token
			case y.typ == tUnicodeRange:
				cat = "unicode-range" // the 2014 CR's token; the standard has an ident and numbers
			case y.typ == tMatch:
				cat = "match" // the 2014 CR's ~= |= ^= $= *= ||; the standard has delims
			}
			if cat == "other" {
				lo, hi := x.start, max(x.end, y.end)
				hi = min(hi, len(src), lo+60)
				fmt.Printf("DIFF %s @%d spec=%d[%d,%d) tdewolff=%d[%d,%d) %q\n", f, lo, x.typ, x.start, x.end, y.typ, y.start, y.end, src[lo:hi])
			}
			cats[cat]++
			// resync: advance whichever ends first until both end together
			for i < len(a) && j < len(b) && a[i].end != b[j].end {
				if a[i].end < b[j].end {
					i++
				} else {
					j++
				}
			}
			i++
			j++
		}
	}
	fmt.Printf("files=%d tdewolff_short=%d differences=%v\n", n, short, cats)
}
