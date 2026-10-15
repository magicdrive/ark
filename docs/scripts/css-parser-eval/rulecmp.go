//go:build ignore
// +build ignore

package main

// Rule-level differential test (hardening Phase D): the walker's outline
// (walk.go, which the provider's reader ports — TestHarnessAgreement) and
// tdewolff's css.Parser, normalized to a sequence of events
//
//	<depth> @name{   an at-rule with a block      <depth> @name;  without
//	<depth> rule{    a qualified rule (a style rule)
//	<depth> decl <name>   a declaration (custom properties included)
//
// (problems and block ends are left out: each event carries its depth). A
// file agrees when the two sequences are equal; otherwise the first
// difference is categorized, and the files are listed per category for
// inspection against the specification.

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

func specEvents(src []byte) []string {
	var out []string
	for _, it := range sheet(src).items {
		switch {
		case it.kind == "decl":
			out = append(out, fmt.Sprintf("%d decl %s", it.depth, strings.ToLower(it.name)))
		case it.kind == "rule":
			out = append(out, fmt.Sprintf("%d rule{", it.depth))
		case strings.HasPrefix(it.kind, "@"):
			mark := ";"
			if it.block {
				mark = "{"
			}
			out = append(out, fmt.Sprintf("%d %s%s", it.depth, strings.ToLower(it.kind), mark))
		}
	}
	return out
}

func tdEvents(src []byte) []string {
	var out []string
	p := css.NewParser(parse.NewInput(bytes.NewReader(src)), false)
	depth := 0
	for {
		gt, _, data := p.Next()
		switch gt {
		case css.ErrorGrammar:
			if p.Err() != nil {
				return out
			}
		case css.AtRuleGrammar:
			out = append(out, fmt.Sprintf("%d %s;", depth, strings.ToLower(string(data))))
		case css.BeginAtRuleGrammar:
			out = append(out, fmt.Sprintf("%d %s{", depth, strings.ToLower(string(data))))
			depth++
		case css.EndAtRuleGrammar:
			depth--
		case css.BeginRulesetGrammar:
			out = append(out, fmt.Sprintf("%d rule{", depth))
			depth++
		case css.EndRulesetGrammar:
			depth--
		case css.DeclarationGrammar, css.CustomPropertyGrammar:
			out = append(out, fmt.Sprintf("%d decl %s", depth, strings.ToLower(string(data))))
		}
	}
}

// rulesDiff compares the event sequences of each file.
func rulesDiff(files []string) {
	same, n, events := 0, 0, 0
	cats := map[string][]string{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		n++
		a, b := specEvents(src), tdEvents(src)
		events += len(a)
		i := 0
		for i < len(a) && i < len(b) && a[i] == b[i] {
			i++
		}
		if i == len(a) && i == len(b) {
			same++
			continue
		}
		get := func(s []string) string {
			if i < len(s) {
				return strings.SplitN(s[i], " ", 2)[1]
			}
			return "(end)"
		}
		key := kindOf(get(a)) + " | " + kindOf(get(b))
		cats[key] = append(cats[key], fmt.Sprintf("%s event %d: spec %q td %q", f, i, get(a), get(b)))
	}
	keys := make([]string, 0, len(cats))
	for k := range cats {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(cats[keys[i]]) > len(cats[keys[j]]) })
	for _, k := range keys {
		fmt.Printf("%5d  %s\n", len(cats[k]), k)
		for _, e := range cats[k][:min(3, len(cats[k]))] {
			fmt.Printf("         %s\n", e)
		}
	}
	fmt.Printf("files=%d agree=%d differ=%d spec_events=%d\n", n, same, n-same, events)
}

// kindOf is an event's kind for the categories: "decl", "rule{", "@x{" with
// the at-rule name kept, "(end)".
func kindOf(e string) string {
	switch {
	case strings.HasPrefix(e, "decl "):
		return "decl"
	default:
		return e
	}
}
