package index

import (
	"sort"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Relations the graph does not hold as edges, kept so that queries can show
// what the resolver established without inventing anything.
//
// Candidate relations: a reference from an identified symbol whose resolution
// names several (or capped) candidates is, for each candidate, a candidate
// relation — in both directions (possible caller of the candidate, possible
// callee of the source). Ambiguity is evidence: each direction keeps a
// deterministic sample of at most MaxCandidateSources distinct symbols (the
// first in resolution order: files sorted, references in source order) and the
// total number of distinct symbols, so a sample never hides how much it left
// out. Candidate relations are never edges and never promoted to one.
//
// Non-edge relations: references of a kind without graph semantics (read,
// write, ...) that resolve to a unique target, and the candidate relations of
// such references. They are not graph edges and are not counted by
// Completeness, which concerns graph edges only.
//
// Relations from an unidentified source: a resolved (or candidate) reference
// whose enclosing code is named but is not one symbol of its file — no symbol
// carries the name (e.g. a nested function of a bundle) or several do (a
// property and a method of the same name). The source cannot be chosen, so the
// relation has no edge (Completeness counts it as unattributed); it is kept by
// target with the enclosing name and file only, never attributed to a symbol.

// CandidateRelation is one sampled relation: the symbol at the other end, the
// kind of the reference, its confidence and the resolver's first evidence.
type CandidateRelation struct {
	Symbol     symbol.SymbolID
	Kind       reference.ReferenceKind
	Confidence resolver.Confidence
	Evidence   resolver.ResolutionEvidence
}

// CandidateSample is a bounded, deterministic sample of candidate relations
// (ordered by SymbolID) and the number of distinct symbols it was drawn from.
type CandidateSample struct {
	Relations []CandidateRelation
	Total     int
}

// Truncated reports whether the sample leaves symbols out.
func (s CandidateSample) Truncated() bool { return s.Total > len(s.Relations) }

// NonEdgeRelation is a uniquely resolved reference of a kind that forms no
// graph edge (read, write, ...): Source contains the reference, Target is what
// it refers to.
type NonEdgeRelation struct {
	Source     symbol.SymbolID
	Target     symbol.SymbolID
	Kind       reference.ReferenceKind
	Confidence resolver.Confidence
	Evidence   []resolver.ResolutionEvidence
}

// UnidentifiedSourceRelation is a relation into a symbol from code named
// Container in File that is not one identified symbol.
type UnidentifiedSourceRelation struct {
	Container  string
	File       source.FileID
	Kind       reference.ReferenceKind
	Confidence resolver.Confidence
	Evidence   resolver.ResolutionEvidence
}

// UnidentifiedSourceSample: the resolved relations from unidentified sources
// (all of them), a bounded deterministic sample of the candidate ones and the
// number of distinct candidate sources.
type UnidentifiedSourceSample struct {
	Resolved        []UnidentifiedSourceRelation
	Candidates      []UnidentifiedSourceRelation
	CandidatesTotal int
}

// sampler keeps the first MaxCandidateSources distinct items (by key) it is
// given, in order, and counts every distinct key.
type sampler[T any] struct {
	keys  []string
	items []T
	total int
	seen  map[string]bool // only once the sample overflows
}

func (s *sampler[T]) add(key string, item T) {
	if s.seen != nil {
		if !s.seen[key] {
			s.seen[key] = true
			s.total++
		}
		return
	}
	for _, k := range s.keys {
		if k == key {
			return
		}
	}
	s.total++
	if len(s.keys) < MaxCandidateSources {
		s.keys = append(s.keys, key)
		s.items = append(s.items, item)
		return
	}
	// Overflow: remember every key seen so far to keep counting distinct ones.
	s.seen = make(map[string]bool, 2*MaxCandidateSources)
	for _, k := range s.keys {
		s.seen[k] = true
	}
	s.seen[key] = true
}

// sorted returns the kept items ordered by key, and the total.
func (s *sampler[T]) sorted() ([]T, int) {
	if s == nil {
		return nil, 0
	}
	idx := make([]int, len(s.keys))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return s.keys[idx[a]] < s.keys[idx[b]] })
	out := make([]T, len(idx))
	for i, k := range idx {
		out[i] = s.items[k]
	}
	return out, s.total
}

func candidateSample(s *sampler[CandidateRelation]) CandidateSample {
	rels, total := s.sorted()
	return CandidateSample{Relations: rels, Total: total}
}

// samples is a directed pair of sampler maps: by target (possible sources) and
// by source (possible targets).
type samples struct {
	in  map[symbol.SymbolID]*sampler[CandidateRelation]
	out map[symbol.SymbolID]*sampler[CandidateRelation]
}

func newSamples() samples {
	return samples{in: make(map[symbol.SymbolID]*sampler[CandidateRelation]), out: make(map[symbol.SymbolID]*sampler[CandidateRelation])}
}

// observe records source → each candidate of res, for a reference of kind k.
func (s samples) observe(source symbol.SymbolID, k reference.ReferenceKind, res resolver.Resolution) {
	var ev resolver.ResolutionEvidence
	if len(res.Evidence) > 0 {
		ev = res.Evidence[0]
	}
	for _, cand := range res.Candidates {
		s.at(s.in, cand.SymbolID).add(string(source), CandidateRelation{Symbol: source, Kind: k, Confidence: res.Confidence, Evidence: ev})
		s.at(s.out, source).add(string(cand.SymbolID), CandidateRelation{Symbol: cand.SymbolID, Kind: k, Confidence: res.Confidence, Evidence: ev})
	}
}

func (s samples) at(m map[symbol.SymbolID]*sampler[CandidateRelation], id symbol.SymbolID) *sampler[CandidateRelation] {
	sm := m[id]
	if sm == nil {
		sm = &sampler[CandidateRelation]{}
		m[id] = sm
	}
	return sm
}

// seal drops the build-time bookkeeping.
func (s samples) seal() {
	for _, m := range []map[symbol.SymbolID]*sampler[CandidateRelation]{s.in, s.out} {
		for _, sm := range m {
			sm.seen = nil
		}
	}
}

// nonEdgeRelations holds the relations of references without graph semantics.
type nonEdgeRelations struct {
	from       map[symbol.SymbolID][]NonEdgeRelation
	to         map[symbol.SymbolID][]NonEdgeRelation
	candidates samples
}

func newNonEdgeRelations() *nonEdgeRelations {
	return &nonEdgeRelations{
		from:       make(map[symbol.SymbolID][]NonEdgeRelation),
		to:         make(map[symbol.SymbolID][]NonEdgeRelation),
		candidates: newSamples(),
	}
}

// observe records ref (a kind without graph semantics) from source.
func (n *nonEdgeRelations) observe(ref reference.Reference, res resolver.Resolution, source symbol.SymbolID) {
	switch {
	case res.Confidence == resolver.ConfidenceUnresolved:
	case res.HasUniqueTarget():
		r := NonEdgeRelation{Source: source, Target: res.Candidates[0].SymbolID, Kind: ref.Kind, Confidence: res.Confidence, Evidence: res.Evidence}
		n.from[source] = append(n.from[source], r)
		n.to[r.Target] = append(n.to[r.Target], r)
	default:
		n.candidates.observe(source, ref.Kind, res)
	}
}

func copyNonEdge(rs []NonEdgeRelation) []NonEdgeRelation {
	out := make([]NonEdgeRelation, len(rs))
	for i, r := range rs {
		r.Evidence = append([]resolver.ResolutionEvidence(nil), r.Evidence...)
		out[i] = r
	}
	return out
}

// unidentifiedSources holds, by target, the relations from unidentified
// sources.
type unidentifiedSources struct {
	resolved   map[symbol.SymbolID][]UnidentifiedSourceRelation
	candidates map[symbol.SymbolID]*sampler[UnidentifiedSourceRelation]
}

func newUnidentifiedSources() *unidentifiedSources {
	return &unidentifiedSources{
		resolved:   make(map[symbol.SymbolID][]UnidentifiedSourceRelation),
		candidates: make(map[symbol.SymbolID]*sampler[UnidentifiedSourceRelation]),
	}
}

// observe records ref, inside code named ref.Container in file that is not one
// identified symbol.
func (u *unidentifiedSources) observe(file source.FileID, ref reference.Reference, res resolver.Resolution) {
	if ref.Container == "" || res.Confidence == resolver.ConfidenceUnresolved {
		return
	}
	var ev resolver.ResolutionEvidence
	if len(res.Evidence) > 0 {
		ev = res.Evidence[0]
	}
	r := UnidentifiedSourceRelation{Container: ref.Container, File: file, Kind: ref.Kind, Confidence: res.Confidence, Evidence: ev}
	if res.HasUniqueTarget() {
		t := res.Candidates[0].SymbolID
		u.resolved[t] = append(u.resolved[t], r)
		return
	}
	for _, cand := range res.Candidates {
		sm := u.candidates[cand.SymbolID]
		if sm == nil {
			sm = &sampler[UnidentifiedSourceRelation]{}
			u.candidates[cand.SymbolID] = sm
		}
		sm.add(string(file)+"\x00"+ref.Container, r)
	}
}

func (u *unidentifiedSources) seal() {
	for _, sm := range u.candidates {
		sm.seen = nil
	}
}

func (u *unidentifiedSources) sample(id symbol.SymbolID) UnidentifiedSourceSample {
	out := UnidentifiedSourceSample{Resolved: append([]UnidentifiedSourceRelation(nil), u.resolved[id]...)}
	out.Candidates, out.CandidatesTotal = u.candidates[id].sorted()
	return out
}
