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
// callee of the source). A relation is identified by the symbol at the other
// end and the reference kind: `new Foo()` and `Foo()` are two relations with
// Foo, two `$x->render()` calls one relation of two references.
//
// Ambiguity is evidence, and truncation is evidence. Each direction keeps a
// deterministic sample of at most MaxCandidateSources relations (the first in
// resolution order: files sorted, references in source order), each with the
// number of references it stands for (only the first one's evidence is kept),
// and counts every distinct relation and every distinct symbol, so a sample
// never hides that it left something out. Candidate relations are never edges
// and never promoted to one.
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
// kind of the reference, the confidence and evidence of its first reference,
// and how many candidate references it stands for.
type CandidateRelation struct {
	Symbol     symbol.SymbolID
	Kind       reference.ReferenceKind
	Confidence resolver.Confidence
	Evidence   resolver.ResolutionEvidence
	References int
}

// CandidateSample is a bounded, deterministic sample of candidate relations
// (ordered by symbol, then kind). Total is the number of distinct symbols in
// all the candidate relations, RelationsTotal the number of distinct relations.
type CandidateSample struct {
	Relations      []CandidateRelation
	Total          int
	RelationsTotal int
}

// Truncated reports whether the sample leaves relations out.
func (s CandidateSample) Truncated() bool { return s.RelationsTotal > len(s.Relations) }

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
// References counts the references it stands for (candidate ones only; each
// resolved reference is listed).
type UnidentifiedSourceRelation struct {
	Container  string
	File       source.FileID
	Kind       reference.ReferenceKind
	Confidence resolver.Confidence
	Evidence   resolver.ResolutionEvidence
	References int
}

// UnidentifiedSourceSample: the resolved relations from unidentified sources
// (all of them), a bounded deterministic sample of the candidate ones, the
// number of distinct candidate sources and of distinct candidate relations.
type UnidentifiedSourceSample struct {
	Resolved                []UnidentifiedSourceRelation
	Candidates              []UnidentifiedSourceRelation
	CandidatesTotal         int
	CandidateRelationsTotal int
}

// sampler keeps the first MaxCandidateSources distinct relations (by relation
// key) it is given, in order, with the number of references each stands for;
// it counts every distinct relation key and every distinct symbol key, and
// remembers the first MaxCandidateSources distinct symbol keys.
type sampler[T any] struct {
	keys      []string // relation keys of the kept items; nil once sealed
	items     []T
	refs      []int
	relations int
	relSeen   map[string]bool // only once the relation sample overflows

	symbols      []string
	symbolsTotal int
	symSeen      map[string]bool // only once the symbol list overflows
}

func (s *sampler[T]) add(symKey, relKey string, item T) {
	s.addSymbol(symKey)
	for i, k := range s.keys {
		if k == relKey {
			s.refs[i]++
			return
		}
	}
	if s.relSeen != nil {
		if !s.relSeen[relKey] {
			s.relSeen[relKey] = true
			s.relations++
		}
		return
	}
	s.relations++
	if len(s.keys) < MaxCandidateSources {
		s.keys = append(s.keys, relKey)
		s.items = append(s.items, item)
		s.refs = append(s.refs, 1)
		return
	}
	// Overflow: remember every relation seen so far to keep counting.
	s.relSeen = make(map[string]bool, 2*MaxCandidateSources)
	for _, k := range s.keys {
		s.relSeen[k] = true
	}
	s.relSeen[relKey] = true
}

func (s *sampler[T]) addSymbol(key string) {
	if s.symSeen != nil {
		if !s.symSeen[key] {
			s.symSeen[key] = true
			s.symbolsTotal++
		}
		return
	}
	for _, k := range s.symbols {
		if k == key {
			return
		}
	}
	s.symbolsTotal++
	if len(s.symbols) < MaxCandidateSources {
		s.symbols = append(s.symbols, key)
		return
	}
	s.symSeen = make(map[string]bool, 2*MaxCandidateSources)
	for _, k := range s.symbols {
		s.symSeen[k] = true
	}
	s.symSeen[key] = true
}

// seal orders the kept relations and symbols by key and drops build-time
// bookkeeping; the sampler is read-only afterwards.
func (s *sampler[T]) seal() {
	s.items, s.refs = s.ordered()
	sort.Strings(s.symbols)
	s.keys, s.relSeen, s.symSeen = nil, nil, nil
}

// ordered returns copies of the kept items and their reference counts ordered
// by relation key (already so once sealed).
func (s *sampler[T]) ordered() ([]T, []int) {
	idx := make([]int, len(s.items))
	for i := range idx {
		idx[i] = i
	}
	if s.keys != nil {
		sort.Slice(idx, func(a, b int) bool { return s.keys[idx[a]] < s.keys[idx[b]] })
	}
	items, refs := make([]T, len(idx)), make([]int, len(idx))
	for i, k := range idx {
		items[i], refs[i] = s.items[k], s.refs[k]
	}
	return items, refs
}

func candidateSample(s *sampler[CandidateRelation]) CandidateSample {
	if s == nil {
		return CandidateSample{}
	}
	items, refs := s.ordered()
	for i := range items {
		items[i].References = refs[i]
	}
	return CandidateSample{Relations: items, Total: s.symbolsTotal, RelationsTotal: s.relations}
}

// candidateSymbols returns the first MaxCandidateSources distinct symbols of
// s's relations, ordered by SymbolID.
func candidateSymbols(s *sampler[CandidateRelation]) []symbol.SymbolID {
	if s == nil {
		return nil
	}
	out := make([]symbol.SymbolID, len(s.symbols))
	for i, k := range s.symbols {
		out[i] = symbol.SymbolID(k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func relationKey(symKey string, k reference.ReferenceKind) string {
	return symKey + "\x00" + string(k)
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
		s.at(s.in, cand.SymbolID).add(string(source), relationKey(string(source), k), CandidateRelation{Symbol: source, Kind: k, Confidence: res.Confidence, Evidence: ev})
		s.at(s.out, source).add(string(cand.SymbolID), relationKey(string(cand.SymbolID), k), CandidateRelation{Symbol: cand.SymbolID, Kind: k, Confidence: res.Confidence, Evidence: ev})
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
			sm.seal()
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
		src := string(file) + "\x00" + ref.Container
		sm.add(src, relationKey(src, ref.Kind), r)
	}
}

func (u *unidentifiedSources) seal() {
	for _, sm := range u.candidates {
		sm.seal()
	}
}

func (u *unidentifiedSources) sample(id symbol.SymbolID) UnidentifiedSourceSample {
	out := UnidentifiedSourceSample{Resolved: append([]UnidentifiedSourceRelation(nil), u.resolved[id]...)}
	if sm := u.candidates[id]; sm != nil {
		items, refs := sm.ordered()
		for i, r := range items {
			r.References = refs[i]
			out.Candidates = append(out.Candidates, r)
		}
		out.CandidatesTotal, out.CandidateRelationsTotal = sm.symbolsTotal, sm.relations
	}
	return out
}
