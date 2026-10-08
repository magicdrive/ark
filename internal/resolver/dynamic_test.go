package resolver_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// A Dynamic reference (its name is computed at run time) is Unresolved and
// never matched against declarations — whatever else the reference carries,
// and even when its display name equals a unique declaration. It is not
// OutsideRepository either: nothing proves where it goes. Language-neutral:
// the evidence is synthetic, driven through two languages.
func TestDynamicName_NeverResolves(t *testing.T) {
	for _, lang := range []string{"php", "javascript"} {
		sym := func(name, qual, parent, recv string, kind symbol.SymbolKind) symbol.Symbol {
			return symbol.Symbol{
				ID: symbol.SymbolID("a#" + qual), Name: name, Qualified: qual, ParentQualified: parent,
				Receiver: recv, Kind: kind, Language: lang, Location: source.Location{File: "a"},
			}
		}
		files := []resolver.FileIndex{{FileID: "a", Language: lang, Symbols: []symbol.Symbol{
			sym("A", "A", "", "", symbol.KindClass),
			sym("run", "A.run", "A", "A", symbol.KindMethod),
			sym("helper", "helper", "", "", symbol.KindFunction),
		}}}
		r := resolver.New(files)
		fi := resolver.FileIndex{FileID: "a", Language: lang}
		base := reference.Reference{Kind: reference.KindCall, Language: lang, Container: "A.run", IsCall: true, Dynamic: true,
			Location: source.Location{File: "a"}}
		cases := map[string]func(reference.Reference) reference.Reference{
			"same name as a free function": func(r reference.Reference) reference.Reference { r.Name = "helper"; return r },
			"typed receiver": func(r reference.Reference) reference.Reference {
				r.Name = "run"
				r.ReceiverExpr = "a"
				r.ReceiverType = "A"
				return r
			},
			"qualified identities": func(r reference.Reference) reference.Reference {
				r.Name = "run"
				r.ReceiverTypeQualified = "A"
				r.NameQualified = "helper"
				return r
			},
			"expression text": func(r reference.Reference) reference.Reference { r.Name = "$m"; r.ReceiverExpr = "$this"; return r },
		}
		for name, mk := range cases {
			ref := mk(base)
			ref.ID = reference.ReferenceID(lang + "/" + name)
			res := r.ResolveReference(ref, fi)
			if res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 || res.OutsideRepository {
				t.Errorf("%s/%s: got confidence=%s candidates=%d outside=%v, want Unresolved, none, not outside",
					lang, name, res.Confidence, len(res.Candidates), res.OutsideRepository)
			}
			if len(res.Evidence) != 1 || res.Evidence[0].Kind != resolver.EvidenceDynamicName {
				t.Errorf("%s/%s: evidence %v, want dynamic_name", lang, name, res.Evidence)
			}
			// The same reference without the marker does resolve: the rule,
			// not the fixture, is what keeps it unresolved.
			ref.Dynamic = false
			if name != "expression text" && len(r.ResolveReference(ref, fi).Candidates) == 0 {
				t.Errorf("%s/%s: control reference did not resolve; the case proves nothing", lang, name)
			}
		}
	}
}
