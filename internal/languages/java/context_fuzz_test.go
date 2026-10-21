package java_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	ctxengine "github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// fuzzPeer is a fixed second file, so that the fuzzed one has repository
// declarations to call, extend and import, overloads among them.
const fuzzPeer = `package p;

public class B extends Base implements I {
    public B() { this(0); }
    public B(int n) { super(); }
    public static B of(String s) { return new B(s.length()); }
    public void m(int x) { n(); }
    public void m(String s) { m(s.length()); }
    void n() {}
    @Override public String name() { return "b"; }
}
`

const fuzzBase = `package p;

abstract class Base {
    protected int id;
    void touch() { id++; }
}

interface I {
    String name();
    default String label() { return name(); }
}
`

// FuzzJavaContext builds get_context for the declarations of a repository
// with one arbitrary Java file (the first 20, by file and position) and
// checks the context contract on each: the target is first; every item's
// range lies in its file and its source is exactly those lines; every other
// item is the end of a graph edge — of the target, or of a callee of it for
// a transitive callee — with that edge's confidence, never a candidate; no
// range or symbol is listed twice; the budget holds unless the target alone
// exceeds it; and the same input gives the same context.
func FuzzJavaContext(f *testing.F) {
	for _, s := range []string{
		"package p; class A { void f() { new B().m(1); new B().m(\"x\"); B.of(\"y\"); } }",
		"package p; import static p.B.of; class A extends B { A() { super(1); } void f() { of(\"\"); touch(); label(); m(2); } }",
		"package p; class A { void f(I i, Base b) { i.name(); i.label(); b.touch(); var x = B.of(\"\"); x.n(); } }",
		"package p; record R(int a) { R { if (a < 0) throw new IllegalArgumentException(); } int twice() { return a() * 2; } }",
		"package p; enum E { X { void g() {} }, Y; void g() { new B(); } }",
		"package p; class A { Runnable r() { return new Runnable() { public void run() { new B().n(); } }; } int l() { class L { int v() { return 1; } } return new L().v(); } }",
		"package p; class A { java.util.function.Function<String, B> f() { return B::of; } }",
		"package p; @interface Ann { String value(); } @Ann(\"a\") class A { @Ann(\"b\") void f() {} }",
		"package p; class A { void f( { new B().m(1); } void g() { f(); } }",
		"package p; class A { void f() { } } class A { void f() { } }",
		"package p; class Ä { void ü() { new B().m(1); } } // é",
		"",
	} {
		f.Add([]byte(s), uint16(8000))
		f.Add([]byte(s), uint16(30))
	}
	f.Fuzz(func(t *testing.T, src []byte, budget uint16) {
		dir := t.TempDir()
		files := map[string][]byte{
			"src/main/java/p/A.java":    src,
			"src/main/java/p/B.java":    []byte(fuzzPeer),
			"src/main/java/p/Base.java": []byte(fuzzBase),
		}
		for rel, b := range files {
			path := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		idx, err := index.New(context.Background(), dir, javaProviders())
		if err != nil {
			t.Fatal(err)
		}
		again, err := index.New(context.Background(), dir, javaProviders())
		if err != nil {
			t.Fatal(err)
		}
		var targets []symbol.Symbol
		for _, rel := range idx.Files() {
			targets = append(targets, idx.SymbolsByFile(rel)...)
		}
		if len(targets) > 20 {
			targets = targets[:20]
		}
		max := int(budget)%9000 + 1 // the engine reads 0 as its default budget
		for _, target := range targets {
			req := ctxengine.Request{Target: target.ID, MaxTokens: max, MaxDepth: 2}
			res, err := ctxengine.New(idx, dir).Build(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			checkJavaContext(t, idx, files, target, req, res)
			res2, err := ctxengine.New(again, dir).Build(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if a, b := ctxengine.Format(res), ctxengine.Format(res2); a != b {
				t.Fatalf("context of %s differs between two builds:\n%s\n---\n%s", target.Qualified, a, b)
			}
		}
	})
}

func checkJavaContext(t *testing.T, idx *index.RepositoryIndex, files map[string][]byte, target symbol.Symbol, req ctxengine.Request, res *ctxengine.Result) {
	t.Helper()
	read := func(f source.FileID) ([]byte, bool) {
		b, ok := files[string(f)]
		return b, ok
	}
	if v := contextViolations(idx, read, target, req, res); len(v) > 0 {
		t.Fatalf("context of %s: %s", target.Qualified, v[0])
	}
}

// contextViolations checks one context against the contract (see
// FuzzJavaContext) and returns what it breaks, each violation prefixed with
// its kind ("target:", "range:", "source:", "duplicate:", "edge:",
// "budget:"). read returns a repository file's content.
func contextViolations(idx *index.RepositoryIndex, read func(source.FileID) ([]byte, bool), target symbol.Symbol, req ctxengine.Request, res *ctxengine.Result) []string {
	var out []string
	if len(res.Items) == 0 || res.Items[0].Symbol.ID != target.ID || res.Items[0].Reason != "target" {
		return []string{fmt.Sprintf("target: context does not start with its target: %d items", len(res.Items))}
	}
	// The confidences of the edges from the target to each symbol (a symbol
	// can be the end of several edges: a call and a type use).
	type edgeKey struct {
		to   symbol.SymbolID
		conf resolver.Confidence
	}
	callees, callers := map[edgeKey]bool{}, map[edgeKey]bool{}
	var depth1 []symbol.SymbolID
	for _, e := range idx.GetCallees(target.ID) {
		callees[edgeKey{e.To, e.Confidence}] = true
		depth1 = append(depth1, e.To)
	}
	for _, e := range idx.GetCallers(target.ID) {
		callers[edgeKey{e.To, e.Confidence}] = true
	}
	type span struct {
		file       source.FileID
		start, end uint32
	}
	spans := map[span]bool{}
	ids := map[symbol.SymbolID]bool{}
	tokens := 0
	for i, it := range res.Items {
		s := it.Symbol
		data, ok := read(s.Location.File)
		if !ok {
			out = append(out, fmt.Sprintf("range: item %s is in %s, no file of the repository", s.Qualified, s.Location.File))
			continue
		}
		lines := uint32(bytes.Count(data, []byte("\n")) + 1)
		r := s.Location.Range
		if r.Start.Line < 1 || r.Start.Line > r.End.Line || r.End.Line > lines {
			out = append(out, fmt.Sprintf("range: item %s has lines %d-%d of a %d-line file", s.Qualified, r.Start.Line, r.End.Line, lines))
		}
		if want, err := ctxengine.SourceLines(data, r.Start.Line, r.End.Line); err != nil || it.Source != want {
			out = append(out, fmt.Sprintf("source: item %s is not lines %d-%d of %s", s.Qualified, r.Start.Line, r.End.Line, s.Location.File))
		}
		if it.Tokens != ctxengine.EstimateTokens(it.Source) {
			out = append(out, fmt.Sprintf("budget: item %s has %d tokens for %d bytes", s.Qualified, it.Tokens, len(it.Source)))
		}
		k := span{s.Location.File, r.Start.Line, r.End.Line}
		if spans[k] || ids[s.ID] {
			out = append(out, fmt.Sprintf("duplicate: item %s repeats a range or a symbol", s.Qualified))
		}
		spans[k], ids[s.ID] = true, true
		tokens += it.Tokens

		if i == 0 {
			continue
		}
		if it.Confidence < resolver.ConfidenceStrong {
			out = append(out, fmt.Sprintf("edge: item %s (%s) has confidence %s", s.Qualified, it.Reason, it.Confidence))
			continue
		}
		var proven bool
		switch it.Reason {
		case "caller":
			proven = callers[edgeKey{s.ID, it.Confidence}]
		case "transitive callee":
			for _, via := range depth1 {
				for _, e := range idx.GetCallees(via) {
					proven = proven || e.To == s.ID && e.Confidence == it.Confidence
				}
			}
		default:
			proven = callees[edgeKey{s.ID, it.Confidence}]
		}
		if !proven {
			out = append(out, fmt.Sprintf("edge: item %s (%s, %s) is no graph edge of that confidence", s.Qualified, it.Reason, it.Confidence))
		}
	}
	if tokens != res.Stats.EstimatedTokens {
		out = append(out, fmt.Sprintf("budget: items have %d tokens, stats say %d", tokens, res.Stats.EstimatedTokens))
	}
	if res.Stats.TargetTruncated != (res.Items[0].Tokens > req.MaxTokens) {
		out = append(out, fmt.Sprintf("budget: targetTruncated=%v for a %d-token target and budget %d", res.Stats.TargetTruncated, res.Items[0].Tokens, req.MaxTokens))
	}
	if !res.Stats.TargetTruncated && tokens > req.MaxTokens {
		out = append(out, fmt.Sprintf("budget: %d tokens exceed the budget %d", tokens, req.MaxTokens))
	}
	if res.Stats.TargetTruncated && tokens > res.Items[0].Tokens {
		// Over budget with the target alone: nothing else may be added.
		out = append(out, fmt.Sprintf("budget: items added to a target over the budget: %d tokens, target %d", tokens, res.Items[0].Tokens))
	}
	return out
}
