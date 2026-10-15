package index_test

import (
	"fmt"
	"testing"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestScoreSiteValueContract pins how scoreSite scores evaluation contract
// v2's value layer (oracleSite.Value; docs/development/web-semantic-fidelity/
// f6b5/oracle-design.md): a site without one scores as v1, a target at the
// property or at its value is correct, a target at a declaration the value's
// aliases pass through stops short, and any other Strong or Exact target is
// false.
func TestScoreSiteValueContract(t *testing.T) {
	sym := func(id, file, name string, line uint32, receiver string) symbol.Symbol {
		return symbol.Symbol{ID: symbol.SymbolID(id), Name: name, Qualified: name, Receiver: receiver,
			Location: source.Location{File: source.FileID(file), Range: source.Range{
				Start: source.Position{Line: line, Column: 1}, End: source.Position{Line: line, Column: 40}}}}
	}
	// m.js: 1 function parse, 2 const alias = require(...), 3 function other,
	// 5 module.exports = { parse, X: 1 } (no symbol for the property),
	// 7 the member obj.parse of a non-exported object.
	syms := map[string][]symbol.Symbol{"m.js": {
		sym("parse", "m.js", "parse", 1, ""),
		sym("alias", "m.js", "alias", 2, ""),
		sym("other", "m.js", "other", 3, ""),
		sym("member", "m.js", "parse", 7, "obj"),
	}}
	ref := func(conf resolver.Confidence, targets ...string) scoredRef {
		res := resolver.Resolution{Confidence: conf}
		for _, id := range targets {
			res.Candidates = append(res.Candidates, resolver.Candidate{SymbolID: symbol.SymbolID(id), File: "m.js"})
		}
		return scoredRef{lang: "javascript", ref: &reference.Reference{Name: "parse"}, res: res}
	}
	property := &declSite{File: "m.js", Line: 5, Col: 20, Name: "parse"}
	value := &declSite{File: "m.js", Line: 1, Col: 10, Name: "parse"}
	site := func(decl, val *declSite, via ...declSite) oracleSite {
		return oracleSite{File: "use.js", Name: "parse", StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 6,
			Class: "declaration", Decl: decl, Value: val, ValueVia: via}
	}
	for _, tc := range []struct {
		name    string
		site    oracleSite
		ark     scoredRef
		verdict string
		want    oracleScore
	}{
		{"v1: the property is no Ark symbol", site(property, nil), ref(resolver.ConfidenceExact, "parse"),
			"[local_fp]", oracleScore{Sites: 1, Declaration: 1, LocalFP: 1}},
		{"v2: the value", site(property, value), ref(resolver.ConfidenceExact, "parse"),
			"[tp_value]", oracleScore{Sites: 1, Declaration: 1, Representable: 1, Recovered: 1, ExactTP: 1,
				ValueSites: 1, ValueRepresentable: 1, ValueTP: 1}},
		{"v2: another target", site(property, value), ref(resolver.ConfidenceExact, "other"),
			"[exact_fp]", oracleScore{Sites: 1, Declaration: 1, Representable: 1, ExactFP: 1,
				ValueSites: 1, ValueRepresentable: 1}},
		{"v2: the value's alias", site(property, value, declSite{File: "m.js", Line: 2, Col: 7, Name: "alias"}),
			ref(resolver.ConfidenceExact, "alias"),
			"[via]", oracleScore{Sites: 1, Declaration: 1, Representable: 1, ViaStop: 1,
				ValueSites: 1, ValueRepresentable: 1}},
		{"v2: a candidate", site(property, value), ref(resolver.ConfidenceCandidate, "other", "parse"),
			"[candidate_hit]", oracleScore{Sites: 1, Declaration: 1, Representable: 1, CandHit: 1,
				ValueSites: 1, ValueRepresentable: 1}},
		{"v2: the member property", site(&declSite{File: "m.js", Line: 7, Col: 15, Name: "parse"}, value),
			ref(resolver.ConfidenceStrong, "member"),
			"[tp]", oracleScore{Sites: 1, Declaration: 1, Representable: 1, MemberRepresentable: 1,
				MemberRecovered: 1, Recovered: 1, StrongTP: 1, ValueSites: 1}},
		{"v2: the member's value", site(&declSite{File: "m.js", Line: 7, Col: 15, Name: "parse"}, value),
			ref(resolver.ConfidenceExact, "parse"),
			"[tp_value]", oracleScore{Sites: 1, Declaration: 1, Representable: 1, MemberRepresentable: 1,
				MemberRecovered: 1, Recovered: 1, ExactTP: 1, ValueSites: 1, ValueTP: 1}},
		{"v2: the value is the property", site(&declSite{File: "m.js", Line: 7, Col: 15, Name: "parse"},
			&declSite{File: "m.js", Line: 7, Col: 15, Name: "parse"}), ref(resolver.ConfidenceStrong, "member"),
			"[tp]", oracleScore{Sites: 1, Declaration: 1, Representable: 1, MemberRepresentable: 1,
				MemberRecovered: 1, Recovered: 1, StrongTP: 1, ValueSites: 1}},
		{"v2: a value without a name", site(property, &declSite{File: "m.js", Line: 1, Col: 1}),
			ref(resolver.ConfidenceExact, "parse"),
			"[local_fp]", oracleScore{Sites: 1, Declaration: 1, LocalFP: 1, ValueSites: 1}},
		{"v2: value_via without a value", site(property, &declSite{File: "m.js", Line: 1, Col: 1},
			declSite{File: "m.js", Line: 2, Col: 7, Name: "alias"}), ref(resolver.ConfidenceExact, "alias"),
			"[local_fp]", oracleScore{Sites: 1, Declaration: 1, LocalFP: 1, ValueSites: 1}},
		{"v2: a function", func() oracleSite { o := site(property, value); o.ValueKind = "function"; return o }(),
			ref(resolver.ConfidenceExact, "parse"),
			"[tp_value]", oracleScore{Sites: 1, Declaration: 1, Representable: 1, Recovered: 1, ExactTP: 1,
				ValueSites: 1, ValueRepresentable: 1, ValueTP: 1}},
		{"v2: an export the program may write", func() oracleSite {
			o := site(&declSite{File: "m.js", Line: 1, Col: 10, Name: "parse"}, nil)
			o.ExportUnsafe = true
			return o
		}(), ref(resolver.ConfidenceExact, "parse"),
			"[exact]", oracleScore{Sites: 1, Declaration: 1, UnsafeSites: 1, UnsafeStrong: 1}},
		// value == want: no value layer, so its value_via is no stop either.
		{"v2: the value is the property, through an alias",
			site(&declSite{File: "m.js", Line: 7, Col: 15, Name: "parse"}, &declSite{File: "m.js", Line: 7, Col: 15, Name: "parse"},
				declSite{File: "m.js", Line: 2, Col: 7, Name: "alias"}), ref(resolver.ConfidenceExact, "alias"),
			"[exact_fp]", oracleScore{Sites: 1, Declaration: 1, Representable: 1, MemberRepresentable: 1, ExactFP: 1, ValueSites: 1}},
		// A value_via without a name (a module) is no stop.
		{"v2: a value_via without a name", site(property, value, declSite{File: "m.js", Line: 2, Col: 7}),
			ref(resolver.ConfidenceExact, "alias"),
			"[exact_fp]", oracleScore{Sites: 1, Declaration: 1, Representable: 1, ExactFP: 1, ValueSites: 1, ValueRepresentable: 1}},
		{"v2: data", func() oracleSite { o := site(property, value); o.ValueKind = "data"; return o }(),
			ref(resolver.ConfidenceExact, "parse"),
			"[tp_value]", oracleScore{Sites: 1, Declaration: 1, Representable: 1, Recovered: 1, ExactTP: 1,
				ValueSites: 1, ValueRepresentable: 1, ValueTP: 1, ValueTPData: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s oracleScore
			verdicts, _ := scoreSite(&s, tc.site, []scoredRef{tc.ark}, syms)
			if got := fmt.Sprint(verdicts); got != tc.verdict {
				t.Errorf("verdicts = %s, want %s", got, tc.verdict)
			}
			if s != tc.want {
				t.Errorf("score = %+v\nwant    %+v", s, tc.want)
			}
		})
	}
}

// TestScoreSiteValueSeveralReferences pins a site with several Ark
// references (a call and a read of one identifier): recovered once, and
// every Strong or Exact one to another target is false.
func TestScoreSiteValueSeveralReferences(t *testing.T) {
	syms := map[string][]symbol.Symbol{"m.js": {
		{ID: "parse", Name: "parse", Location: source.Location{File: "m.js", Range: source.Range{
			Start: source.Position{Line: 1, Column: 1}, End: source.Position{Line: 1, Column: 40}}}},
		{ID: "other", Name: "other", Location: source.Location{File: "m.js", Range: source.Range{
			Start: source.Position{Line: 3, Column: 1}, End: source.Position{Line: 3, Column: 40}}}},
	}}
	ref := func(id string) scoredRef {
		return scoredRef{lang: "javascript", ref: &reference.Reference{Name: "parse"},
			res: resolver.Resolution{Confidence: resolver.ConfidenceExact, Candidates: []resolver.Candidate{{SymbolID: symbol.SymbolID(id)}}}}
	}
	o := oracleSite{File: "use.js", Name: "parse", StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 6, Class: "declaration",
		Decl:  &declSite{File: "m.js", Line: 5, Col: 20, Name: "parse"},
		Value: &declSite{File: "m.js", Line: 1, Col: 10, Name: "parse"}}
	var s oracleScore
	verdicts, falses := scoreSite(&s, o, []scoredRef{ref("parse"), ref("parse"), ref("other")}, syms)
	if got := fmt.Sprint(verdicts); got != "[tp_value tp_value exact_fp]" {
		t.Errorf("verdicts = %s", got)
	}
	if s.Recovered != 1 || s.ExactTP != 2 || s.ExactFP != 1 || s.ValueTP != 2 || len(falses) != 1 {
		t.Errorf("score = %+v, falses = %v", s, falses)
	}
}
