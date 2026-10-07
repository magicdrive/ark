package resolver_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// A provider's ConfidenceCap only ever lowers a resolution: min(resolved, cap).
func TestConfidenceCap_OnlyLowers(t *testing.T) {
	sym := func(name, qual, parent, recv string, kind symbol.SymbolKind, file string) symbol.Symbol {
		return symbol.Symbol{
			ID: symbol.SymbolID(file + "#" + qual), Name: name, Qualified: qual, ParentQualified: parent,
			Receiver: recv, Kind: kind, Language: "php", Location: source.Location{File: source.FileID(file)},
		}
	}
	files := []resolver.FileIndex{
		{FileID: "a/A.php", Language: "php", Symbols: []symbol.Symbol{
			sym("A", `App\A`, "", "", symbol.KindClass, "a/A.php"),
			sym("m", `App\A.m`, `App\A`, "A", symbol.KindMethod, "a/A.php"),
		}},
		{FileID: "b/B.php", Language: "php", Symbols: []symbol.Symbol{
			sym("B", `App\B`, "", "", symbol.KindClass, "b/B.php"),
			sym("m", `App\B.m`, `App\B`, "B", symbol.KindMethod, "b/B.php"),
		}},
	}
	ref := func(rtq, recv, cap string) reference.Reference {
		return reference.Reference{
			ID: reference.ReferenceID("r/" + rtq + "/" + recv + "/" + cap), Name: "m", Kind: reference.KindCall, Language: "php",
			Location: source.Location{File: "c/C.php"}, Container: `App\C.run`, ReceiverExpr: recv,
			ReceiverTypeQualified: rtq, ConfidenceCap: cap, IsCall: true,
		}
	}
	r := resolver.New(files)
	fi := resolver.FileIndex{FileID: "c/C.php", Language: "php"}
	cases := []struct {
		name     string
		ref      reference.Reference
		want     resolver.Confidence
		capped   bool
		wantCand int
	}{
		{"exact, no cap", ref(`App\A`, "$this->a", ""), resolver.ConfidenceExact, false, 1},
		{"exact, cap strong", ref(`App\A`, "$this->a", "strong"), resolver.ConfidenceStrong, true, 1},
		{"exact, cap candidate", ref(`App\A`, "$this->a", "candidate"), resolver.ConfidenceCandidate, true, 1},
		{"exact, unknown cap fails safe", ref(`App\A`, "$this->a", "exactish"), resolver.ConfidenceCandidate, true, 1},
		{"candidate is never promoted", ref("", "$x", "strong"), resolver.ConfidenceCandidate, false, 2},
		{"unresolved is never promoted", ref(`Vendor\X`, "$this->x", "strong"), resolver.ConfidenceUnresolved, false, 0},
	}
	for _, tc := range cases {
		res := r.ResolveReference(tc.ref, fi)
		if res.Confidence != tc.want || len(res.Candidates) != tc.wantCand {
			t.Errorf("%s: confidence=%s candidates=%d, want %s %d", tc.name, res.Confidence, len(res.Candidates), tc.want, tc.wantCand)
		}
		for _, c := range res.Candidates {
			if c.Confidence > tc.want {
				t.Errorf("%s: candidate %s above the resolution (%s)", tc.name, c.Qualified, c.Confidence)
			}
		}
		hasCapEvidence := false
		for _, e := range res.Evidence {
			if e.Kind == resolver.EvidenceConfidenceCap {
				hasCapEvidence = true
			}
		}
		if hasCapEvidence != tc.capped {
			t.Errorf("%s: cap evidence = %v, want %v (%v)", tc.name, hasCapEvidence, tc.capped, res.Evidence)
		}
	}
}
