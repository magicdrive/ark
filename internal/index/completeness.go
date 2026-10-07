package index

import (
	"sort"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// Graph completeness: what the graph does not show.
//
// The graph holds only unique Strong-or-better resolutions from an identified
// container. An empty caller list therefore cannot by itself mean "nothing
// refers to this symbol". Completeness records, per symbol, how many
// references may involve it without being one of its edges, so a query can
// tell a true zero from an unknown.
//
// A reference r (of a kind that forms graph edges) is unattributed with respect
// to symbol T when no edge connects r to T and one of the following holds:
//
//	candidate  r has no unique Strong+ target and T is among r's candidates;
//	unresolved r has no candidate at all, its resolution does not place the
//	           referent outside the repository (resolver.Resolution.
//	           OutsideRepository), and T has r's name and language — and, when
//	           T is a member, r has a receiver (a receiverless name never
//	           denotes a member);
//	sourceless r resolves uniquely (Strong+) to T but its enclosing code is no
//	           symbol (e.g. top-level script statements), so no edge records it.
//
// Incoming(T) counts the references unattributed with respect to T.
// Outgoing(S) counts the references inside S that are unattributed with respect
// to at least one symbol (candidate or unresolved): calls out of S whose target
// is unknown. References proven to leave the repository are neither: they are
// known, not unknown. Nothing here ever becomes a graph edge.
type Completeness struct {
	byName  map[string][]symbol.Symbol
	in      map[symbol.SymbolID]int
	out     map[symbol.SymbolID]int
	sources map[symbol.SymbolID][]symbol.SymbolID
}

// MaxCandidateSources bounds the candidate callers kept per symbol. The kept
// sources are the first distinct ones in resolution order (files sorted,
// references in source order); Incoming always counts all of them.
const MaxCandidateSources = 10

// NewCompleteness returns an empty Completeness that matches unresolved
// references against byName (bare name → symbols).
func NewCompleteness(byName map[string][]symbol.Symbol) *Completeness {
	return &Completeness{
		byName:  byName,
		in:      make(map[symbol.SymbolID]int),
		out:     make(map[symbol.SymbolID]int),
		sources: make(map[symbol.SymbolID][]symbol.SymbolID),
	}
}

// Observe records ref and its resolution. source is the symbol enclosing ref,
// valid only when hasSource is true.
func (c *Completeness) Observe(ref reference.Reference, res resolver.Resolution, source symbol.SymbolID, hasSource bool) {
	if _, ok := edgeKindFor(ref.Kind); !ok {
		return
	}
	if res.HasUniqueTarget() {
		if !hasSource {
			c.in[res.Candidates[0].SymbolID]++ // sourceless
		}
		return
	}

	var targets []symbol.SymbolID
	seen := make(map[symbol.SymbolID]bool)
	add := func(id symbol.SymbolID) {
		if !seen[id] {
			seen[id] = true
			targets = append(targets, id)
		}
	}
	candidate := len(res.Candidates) > 0
	if candidate {
		for _, cand := range res.Candidates {
			add(cand.SymbolID)
		}
	} else if !res.OutsideRepository {
		for _, s := range c.byName[ref.Name] {
			if s.Language != ref.Language {
				continue
			}
			if s.Receiver != "" && ref.ReceiverExpr == "" {
				continue
			}
			add(s.ID)
		}
	}
	if len(targets) == 0 {
		return
	}
	for _, t := range targets {
		c.in[t]++
		if candidate && hasSource {
			c.addSource(t, source)
		}
	}
	if hasSource {
		c.out[source]++
	}
}

func (c *Completeness) addSource(target, source symbol.SymbolID) {
	cur := c.sources[target]
	if len(cur) >= MaxCandidateSources {
		return
	}
	for _, s := range cur {
		if s == source {
			return
		}
	}
	c.sources[target] = append(cur, source)
}

// Incoming returns the number of references unattributed with respect to id.
func (c *Completeness) Incoming(id symbol.SymbolID) int { return c.in[id] }

// Outgoing returns the number of references inside id whose target is unknown.
func (c *Completeness) Outgoing(id symbol.SymbolID) int { return c.out[id] }

// CandidateSources returns up to MaxCandidateSources symbols containing a
// candidate (non-unique) reference to id, ordered by SymbolID.
func (c *Completeness) CandidateSources(id symbol.SymbolID) []symbol.SymbolID {
	out := append([]symbol.SymbolID(nil), c.sources[id]...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
