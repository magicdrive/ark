package index

import (
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
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
// to at least one symbol (candidate or unresolved): calls out of S that may be
// a missing edge to a symbol of the index. Its population is therefore the
// repository's declarations: a reference no indexed symbol can be is not in
// it, and is reported separately (UnresolvedOutgoing), never dropped.
//
// Every reference of an edge-forming kind inside an identified symbol S is
// exactly one of:
//
//	edge               a unique Strong+ target (a graph edge of S);
//	unattributed       counted by Outgoing(S), as above;
//	unresolved         no candidate, not proven outside the repository, and no
//	                   indexed symbol of its name and language can be its target
//	                   (or its name is Dynamic): the target is unknown — an
//	                   external or built-in name, a run-time computed one, or a
//	                   declaration the provider does not extract;
//	outside repository no candidate, and resolver.Resolution.OutsideRepository:
//	                   authoritative evidence places the referent outside the
//	                   repository. It is known, not unknown.
//
// So an S with no edges, Outgoing 0 and UnresolvedOutgoing zero has no
// observed outgoing reference of a graph kind at all. What a provider does not
// observe as a reference (syntax it does not extract) is in none of these
// counts; no count here claims to bound it. Nothing here ever becomes a graph
// edge.
type Completeness struct {
	byName  map[string][]symbol.Symbol
	in      map[symbol.SymbolID]int
	out     map[symbol.SymbolID]int
	samples samples // candidate relations of graph-edge kinds, both directions

	unresolved map[symbol.SymbolID]*UnresolvedSample // per source: no-candidate references
}

// UnresolvedReason says why an observed reference has no target and which
// count it is in.
type UnresolvedReason string

const (
	// UnresolvedSameName: no candidate, but indexed symbols carry its name; it
	// is counted as unattributed (it may be a missing edge to one of them).
	UnresolvedSameName UnresolvedReason = "unattributed"
	// UnresolvedUnknown: no indexed symbol can be its target and it is not
	// proven outside the repository.
	UnresolvedUnknown UnresolvedReason = "unresolved"
	// UnresolvedDynamic: its name is computed at run time.
	UnresolvedDynamic UnresolvedReason = "dynamic_name"
	// UnresolvedOutside: proven to refer outside the repository.
	UnresolvedOutside UnresolvedReason = "outside_repository"
)

// UnresolvedReference is one observed reference out of a symbol that resolves
// to no candidate at all.
type UnresolvedReference struct {
	Name         string
	Kind         reference.ReferenceKind
	ReceiverExpr string
	Location     source.Location
	Reason       UnresolvedReason
}

// MaxUnresolvedReferences bounds the UnresolvedSample kept per symbol.
const MaxUnresolvedReferences = 10

// UnresolvedSample describes the observed references out of one symbol that
// resolve to no candidate. References are the first MaxUnresolvedReferences of
// them in resolution order (source order); Total counts all of them.
// Unresolved counts those with reason UnresolvedUnknown or UnresolvedDynamic,
// OutsideRepository those with UnresolvedOutside (the rest, UnresolvedSameName,
// are part of Outgoing).
type UnresolvedSample struct {
	References        []UnresolvedReference
	Total             int
	Unresolved        int
	OutsideRepository int
}

// Truncated reports whether the sample leaves references out.
func (s UnresolvedSample) Truncated() bool { return s.Total > len(s.References) }

// MaxCandidateSources bounds every candidate sample (see relations.go): the
// candidate callers kept per symbol, the candidate callees kept per symbol. The
// kept symbols are the first distinct ones in resolution order (files sorted,
// references in source order); the sample's Total, and Incoming / Outgoing,
// always count all of them.
const MaxCandidateSources = 10

// NewCompleteness returns an empty Completeness that matches unresolved
// references against byName (bare name → symbols).
func NewCompleteness(byName map[string][]symbol.Symbol) *Completeness {
	return &Completeness{
		byName:  byName,
		in:      make(map[symbol.SymbolID]int),
		out:     make(map[symbol.SymbolID]int),
		samples: newSamples(),

		unresolved: make(map[symbol.SymbolID]*UnresolvedSample),
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
	reason := UnresolvedSameName
	switch {
	case candidate:
		for _, cand := range res.Candidates {
			add(cand.SymbolID)
		}
	case res.OutsideRepository:
		reason = UnresolvedOutside
	case ref.Dynamic:
		reason = UnresolvedDynamic
	default:
		for _, s := range c.byName[ref.Name] {
			if s.Language != ref.Language {
				continue
			}
			if s.Receiver != "" && ref.ReceiverExpr == "" {
				continue
			}
			add(s.ID)
		}
		if len(targets) == 0 {
			reason = UnresolvedUnknown
		}
	}
	if !candidate && hasSource {
		c.observeUnresolved(source, ref, reason)
	}
	if len(targets) == 0 {
		return
	}
	for _, t := range targets {
		c.in[t]++
	}
	if candidate && hasSource {
		c.samples.observe(source, ref.Kind, res)
	}
	if hasSource {
		c.out[source]++
	}
}

// observeUnresolved records a no-candidate reference inside source.
func (c *Completeness) observeUnresolved(source symbol.SymbolID, ref reference.Reference, reason UnresolvedReason) {
	s := c.unresolved[source]
	if s == nil {
		s = &UnresolvedSample{}
		c.unresolved[source] = s
	}
	s.Total++
	switch reason {
	case UnresolvedUnknown, UnresolvedDynamic:
		s.Unresolved++
	case UnresolvedOutside:
		s.OutsideRepository++
	}
	if len(s.References) < MaxUnresolvedReferences {
		s.References = append(s.References, UnresolvedReference{
			Name:         ref.Name,
			Kind:         ref.Kind,
			ReceiverExpr: ref.ReceiverExpr,
			Location:     ref.Location,
			Reason:       reason,
		})
	}
}

// Incoming returns the number of references unattributed with respect to id.
func (c *Completeness) Incoming(id symbol.SymbolID) int { return c.in[id] }

// Outgoing returns the number of references inside id whose target is unknown.
func (c *Completeness) Outgoing(id symbol.SymbolID) int { return c.out[id] }

// UnresolvedOutgoing describes the references inside id that resolve to no
// candidate (see UnresolvedSample). The result is a copy.
func (c *Completeness) UnresolvedOutgoing(id symbol.SymbolID) UnresolvedSample {
	s := c.unresolved[id]
	if s == nil {
		return UnresolvedSample{}
	}
	out := *s
	out.References = append([]UnresolvedReference(nil), s.References...)
	return out
}

// CandidateSources returns up to MaxCandidateSources symbols containing a
// candidate (non-unique) reference to id, ordered by SymbolID.
func (c *Completeness) CandidateSources(id symbol.SymbolID) []symbol.SymbolID {
	return candidateSymbols(c.samples.in[id])
}

// CandidateCallerSample returns the candidate relations into id: possible
// callers, at most MaxCandidateSources, and how many there are in all.
func (c *Completeness) CandidateCallerSample(id symbol.SymbolID) CandidateSample {
	return candidateSample(c.samples.in[id])
}

// CandidateCalleeSample returns the candidate relations out of id: possible
// callees, at most MaxCandidateSources, and how many there are in all.
func (c *Completeness) CandidateCalleeSample(id symbol.SymbolID) CandidateSample {
	return candidateSample(c.samples.out[id])
}

// seal drops build-time bookkeeping; the Completeness is read-only afterwards.
func (c *Completeness) seal() { c.samples.seal() }
