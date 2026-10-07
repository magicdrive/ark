package index_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// Completeness rules, observed one resolution at a time.

func complSym(id, name, receiver, lang string) symbol.Symbol {
	return symbol.Symbol{ID: symbol.SymbolID(id), Name: name, Receiver: receiver, Language: lang}
}

func complRef(name, receiver, lang string, kind reference.ReferenceKind) reference.Reference {
	return reference.Reference{Name: name, ReceiverExpr: receiver, Language: lang, Kind: kind}
}

func cands(conf resolver.Confidence, ids ...string) resolver.Resolution {
	res := resolver.Resolution{Confidence: conf}
	for _, id := range ids {
		res.Candidates = append(res.Candidates, resolver.Candidate{SymbolID: symbol.SymbolID(id), Confidence: conf})
	}
	return res
}

func TestCompleteness_Rules(t *testing.T) {
	byName := map[string][]symbol.Symbol{
		"save":   {complSym("A.save", "save", "A", "php"), complSym("B.save", "save", "B", "php"), complSym("Go.save", "save", "G", "go")},
		"helper": {complSym("helper", "helper", "", "php")},
	}
	unresolved := resolver.Resolution{Confidence: resolver.ConfidenceUnresolved}
	outside := resolver.Resolution{Confidence: resolver.ConfidenceUnresolved, OutsideRepository: true}
	call := reference.KindCall

	type obs struct {
		ref    reference.Reference
		res    resolver.Resolution
		source string // "" = no source symbol
	}
	cases := []struct {
		name    string
		obs     []obs
		wantIn  map[string]int
		wantOut map[string]int
		wantSrc map[string][]symbol.SymbolID
	}{
		{
			name:   "resolved edge is complete",
			obs:    []obs{{complRef("save", "$a", "php", call), cands(resolver.ConfidenceExact, "A.save"), "caller"}},
			wantIn: map[string]int{}, wantOut: map[string]int{},
		},
		{
			name:    "candidate counts for every candidate and the source",
			obs:     []obs{{complRef("save", "$x", "php", call), cands(resolver.ConfidenceCandidate, "A.save", "B.save"), "caller"}},
			wantIn:  map[string]int{"A.save": 1, "B.save": 1},
			wantOut: map[string]int{"caller": 1},
			wantSrc: map[string][]symbol.SymbolID{"A.save": {"caller"}, "B.save": {"caller"}},
		},
		{
			name:    "single capped candidate",
			obs:     []obs{{complRef("save", "$x", "php", call), cands(resolver.ConfidenceCandidate, "A.save"), "caller"}},
			wantIn:  map[string]int{"A.save": 1},
			wantOut: map[string]int{"caller": 1},
			wantSrc: map[string][]symbol.SymbolID{"A.save": {"caller"}},
		},
		{
			name:    "unresolved same-name member call, same language only",
			obs:     []obs{{complRef("save", "$this", "php", call), unresolved, "caller"}},
			wantIn:  map[string]int{"A.save": 1, "B.save": 1},
			wantOut: map[string]int{"caller": 1},
		},
		{
			name:   "unresolved outside the repository is known, not unknown",
			obs:    []obs{{complRef("save", "$r", "php", call), outside, "caller"}},
			wantIn: map[string]int{}, wantOut: map[string]int{},
		},
		{
			name:   "unrelated unresolved name",
			obs:    []obs{{complRef("persist", "$r", "php", call), unresolved, "caller"}},
			wantIn: map[string]int{}, wantOut: map[string]int{},
		},
		{
			name:    "receiverless name never denotes a member",
			obs:     []obs{{complRef("save", "", "php", call), unresolved, "caller"}, {complRef("helper", "", "php", call), unresolved, "caller"}},
			wantIn:  map[string]int{"helper": 1},
			wantOut: map[string]int{"caller": 1},
		},
		{
			name:   "kinds without graph semantics are ignored",
			obs:    []obs{{complRef("save", "$x", "php", reference.KindRead), cands(resolver.ConfidenceCandidate, "A.save", "B.save"), "caller"}},
			wantIn: map[string]int{}, wantOut: map[string]int{},
		},
		{
			name:   "sourceless unique reference",
			obs:    []obs{{complRef("save", "$a", "php", call), cands(resolver.ConfidenceStrong, "A.save"), ""}},
			wantIn: map[string]int{"A.save": 1}, wantOut: map[string]int{},
		},
		{
			name:    "sourceless candidate counts but lists no source",
			obs:     []obs{{complRef("save", "$x", "php", call), cands(resolver.ConfidenceCandidate, "A.save", "B.save"), ""}},
			wantIn:  map[string]int{"A.save": 1, "B.save": 1},
			wantOut: map[string]int{},
		},
	}
	ids := []string{"A.save", "B.save", "Go.save", "helper", "caller"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := index.NewCompleteness(byName)
			for _, o := range tc.obs {
				c.Observe(o.ref, o.res, symbol.SymbolID(o.source), o.source != "")
			}
			for _, id := range ids {
				sid := symbol.SymbolID(id)
				if got := c.Incoming(sid); got != tc.wantIn[id] {
					t.Errorf("Incoming(%s) = %d, want %d", id, got, tc.wantIn[id])
				}
				if got := c.Outgoing(sid); got != tc.wantOut[id] {
					t.Errorf("Outgoing(%s) = %d, want %d", id, got, tc.wantOut[id])
				}
				if got := c.CandidateSources(sid); len(got) > 0 || len(tc.wantSrc[id]) > 0 {
					if !reflect.DeepEqual(got, tc.wantSrc[id]) {
						t.Errorf("CandidateSources(%s) = %v, want %v", id, got, tc.wantSrc[id])
					}
				}
			}
		})
	}
}

// Candidate sources are bounded and deterministic; Incoming still counts all.
func TestCompleteness_CandidateSourcesBoundedAndOrdered(t *testing.T) {
	c := index.NewCompleteness(nil)
	n := index.MaxCandidateSources + 5
	for i := n - 1; i >= 0; i-- {
		src := symbol.SymbolID(fmt.Sprintf("src%02d", i))
		c.Observe(complRef("save", "$x", "php", reference.KindCall), cands(resolver.ConfidenceCandidate, "A.save", "B.save"), src, true)
		c.Observe(complRef("save", "$x", "php", reference.KindCall), cands(resolver.ConfidenceCandidate, "A.save", "B.save"), src, true) // same source again
	}
	if got := c.Incoming("A.save"); got != 2*n {
		t.Errorf("Incoming = %d, want %d", got, 2*n)
	}
	got := c.CandidateSources("A.save")
	if len(got) != index.MaxCandidateSources {
		t.Fatalf("kept %d sources, want %d", len(got), index.MaxCandidateSources)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("sources not strictly ordered: %v", got)
		}
	}
}
