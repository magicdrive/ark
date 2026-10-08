package index

import (
	"context"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// EdgeKind names a directed relationship between two symbols.
type EdgeKind string

const (
	EdgeCalls    EdgeKind = "calls"
	EdgeCalledBy EdgeKind = "called_by"
	EdgeUsesType EdgeKind = "uses_type"
	EdgeImports  EdgeKind = "imports"
	// Typed relation edges (D4). Forward-only; the reverse edge remains the
	// generic EdgeCalledBy, consistent with the existing reverse-edge model.
	EdgeExtends    EdgeKind = "extends"
	EdgeImplements EdgeKind = "implements"
	EdgeUsesTrait  EdgeKind = "uses_trait"
)

// GraphEdge is a directed relationship between two symbols with confidence.
type GraphEdge struct {
	From symbol.SymbolID
	To   symbol.SymbolID
	Kind EdgeKind
	// RefKind is the kind of the reference the edge was built from (call,
	// construction, type_use, ...; the first one when several references
	// merge into one edge). Kind is its graph meaning.
	RefKind    reference.ReferenceKind
	Confidence resolver.Confidence
	Evidence   []resolver.ResolutionEvidence
}

// IndexStats summarises the contents of a RepositoryIndex.
type IndexStats struct {
	Files      int
	Symbols    int
	References int
	Relations  int
	Languages  map[string]int // file count per language
	Skipped    int            // files skipped due to errors
}

// RepositoryIndex is an immutable, queryable repository snapshot.
// All public methods are safe for concurrent use.
type RepositoryIndex struct {
	// --- symbol indexes ---
	symbolsByID        map[symbol.SymbolID]symbol.Symbol
	symbolsByFile      map[source.FileID][]symbol.Symbol
	symbolsByName      map[string][]symbol.Symbol
	symbolsByQualified map[string][]symbol.Symbol
	symbolsByKind      map[symbol.SymbolKind][]symbol.Symbol

	// --- reference indexes ---
	referencesByFile      map[source.FileID][]reference.Reference
	referencesByContainer map[symbol.SymbolID][]reference.Reference
	referencesByTarget    map[symbol.SymbolID][]reference.Reference

	// --- graph (forward only; reverse derived on the fly) ---
	callsFrom map[symbol.SymbolID][]GraphEdge // from → callees
	callsTo   map[symbol.SymbolID][]GraphEdge // to   → callers (pre-built for GetCallers)

	// completeness: references that may involve a symbol but are no edge.
	completeness *Completeness

	// nonEdge: resolved relations of reference kinds without graph semantics.
	nonEdge *nonEdgeRelations

	// unidentified: relations into a symbol from code that is no one symbol.
	unidentified *unidentifiedSources

	// fingerprint identifies the inputs the index was built from.
	fingerprint string

	// --- meta ---
	files       []source.FileID // sorted
	diagnostics []language.Diagnostic
	stats       IndexStats
}

// New scans root, extracts symbols/references using providers, resolves
// references, and builds an immutable index. Partial failures are recorded
// in Diagnostics rather than aborting the build.
func New(ctx context.Context, root string, providers []language.Provider) (*RepositoryIndex, error) {
	if err := checkRoot(root); err != nil {
		return nil, err
	}

	b := newBuilder()
	digest := newSourceDigest(providers)

	err := walkSources(ctx, root, providers, func(path, relPath string, prov language.Provider, src []byte, readErr error) {
		digest.add(relPath, src, readErr)
		if readErr != nil {
			b.addDiagnostic(language.Diagnostic{
				Severity: language.SeverityWarning,
				Message:  "read error: " + readErr.Error(),
			})
			b.stats.Skipped++
			return
		}

		fileID := source.FileID(relPath)

		extraction, err := prov.Extract(ctx, fileID, src)
		if err != nil {
			b.addDiagnostic(language.Diagnostic{
				Severity: language.SeverityWarning,
				Message:  path + ": extraction error: " + err.Error(),
			})
			b.stats.Skipped++
			return
		}
		b.addDiagnostics(extraction.Diagnostics)

		lang := string(prov.Language())
		b.ingestExtraction(fileID, lang, extraction)
	})
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		// walkDir errors other than context are soft.
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	b.resolve()
	b.fingerprint = digest.sum()
	return b.freeze(), nil
}

// ---- Symbol queries ----

// SkipDirName reports whether a directory is never indexed: hidden
// directories, vendor and node_modules. Every surface that scans a repository
// for symbols must use this one rule so they all see the same files.
func SkipDirName(name string) bool {
	return strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules"
}

// GetSymbol returns a symbol by its ID.
func (idx *RepositoryIndex) GetSymbol(id symbol.SymbolID) (symbol.Symbol, bool) {
	s, ok := idx.symbolsByID[id]
	return s, ok
}

// FindSymbols returns symbols whose Name has the given prefix (case-sensitive).
func (idx *RepositoryIndex) FindSymbols(prefix string) []symbol.Symbol {
	var out []symbol.Symbol
	for name, syms := range idx.symbolsByName {
		if strings.HasPrefix(name, prefix) {
			out = append(out, syms...)
		}
	}
	sortSymbols(out)
	return out
}

// FindSymbolsByQualified returns symbols with an exact Qualified match.
func (idx *RepositoryIndex) FindSymbolsByQualified(q string) []symbol.Symbol {
	syms := idx.symbolsByQualified[q]
	out := make([]symbol.Symbol, len(syms))
	copy(out, syms)
	sortSymbols(out)
	return out
}

// SymbolsByFile returns all symbols in the given file, sorted by start line.
func (idx *RepositoryIndex) SymbolsByFile(fileID source.FileID) []symbol.Symbol {
	syms := idx.symbolsByFile[fileID]
	out := make([]symbol.Symbol, len(syms))
	copy(out, syms)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Location.Range.Start.Line < out[j].Location.Range.Start.Line
	})
	return out
}

// SymbolsByKind returns all symbols of the given kind, deterministically ordered.
func (idx *RepositoryIndex) SymbolsByKind(kind symbol.SymbolKind) []symbol.Symbol {
	syms := idx.symbolsByKind[kind]
	out := make([]symbol.Symbol, len(syms))
	copy(out, syms)
	sortSymbols(out)
	return out
}

// ---- Reference queries ----

// ReferencesByFile returns all references in the given file.
func (idx *RepositoryIndex) ReferencesByFile(fileID source.FileID) []reference.Reference {
	refs := idx.referencesByFile[fileID]
	out := make([]reference.Reference, len(refs))
	copy(out, refs)
	return out
}

// ReferencesByContainer returns references whose Container matches id.
func (idx *RepositoryIndex) ReferencesByContainer(id symbol.SymbolID) []reference.Reference {
	refs := idx.referencesByContainer[id]
	out := make([]reference.Reference, len(refs))
	copy(out, refs)
	return out
}

// ReferencesByTarget returns references that have been resolved to id.
func (idx *RepositoryIndex) ReferencesByTarget(id symbol.SymbolID) []reference.Reference {
	refs := idx.referencesByTarget[id]
	out := make([]reference.Reference, len(refs))
	copy(out, refs)
	return out
}

// ---- Graph queries ----

// GetCallees returns edges from id to symbols it calls.
// The returned Evidence slices are copies; callers may modify them freely.
func (idx *RepositoryIndex) GetCallees(id symbol.SymbolID) []GraphEdge {
	return copyEdges(idx.callsFrom[id])
}

// GetCallers returns edges from symbols that call id.
// The returned Evidence slices are copies; callers may modify them freely.
func (idx *RepositoryIndex) GetCallers(id symbol.SymbolID) []GraphEdge {
	return copyEdges(idx.callsTo[id])
}

// Fingerprint identifies the inputs the index was built from — provider
// configuration and every source file's path and content — exactly as
// SourceFingerprint computes them. Equal fingerprints mean an index built now
// would be identical, so the index is still current.
func (idx *RepositoryIndex) Fingerprint() string { return idx.fingerprint }

// Unattributed returns, for id, the number of incoming references that may
// target it and the number of its own outgoing references whose target is
// unknown — neither of which is a graph edge (see Completeness). Zero means
// the graph is complete for that direction.
func (idx *RepositoryIndex) Unattributed(id symbol.SymbolID) (incoming, outgoing int) {
	if idx.completeness == nil {
		return 0, 0
	}
	return idx.completeness.Incoming(id), idx.completeness.Outgoing(id)
}

// UnresolvedOutgoing describes the references inside id that resolve to no
// candidate: the unresolved ones (the target is unknown and no indexed symbol
// can be it), the ones proven outside the repository, and — by reason only —
// the same-name ones already counted by Unattributed (see
// Completeness and UnresolvedSample).
func (idx *RepositoryIndex) UnresolvedOutgoing(id symbol.SymbolID) UnresolvedSample {
	if idx.completeness == nil {
		return UnresolvedSample{}
	}
	return idx.completeness.UnresolvedOutgoing(id)
}

// CandidateCallers returns up to MaxCandidateSources symbols that contain a
// candidate (ambiguous or capped) reference to id, ordered by SymbolID. They
// are possible callers only: never graph edges.
func (idx *RepositoryIndex) CandidateCallers(id symbol.SymbolID) []symbol.SymbolID {
	if idx.completeness == nil {
		return nil
	}
	return idx.completeness.CandidateSources(id)
}

// CandidateCallerSample returns the candidate relations into id — possible
// callers from ambiguous references, with reference kind, confidence and
// evidence — at most MaxCandidateSources of them, and their total. Never edges.
func (idx *RepositoryIndex) CandidateCallerSample(id symbol.SymbolID) CandidateSample {
	if idx.completeness == nil {
		return CandidateSample{}
	}
	return idx.completeness.CandidateCallerSample(id)
}

// CandidateCalleeSample returns the candidate relations out of id — possible
// callees of its ambiguous references — at most MaxCandidateSources of them,
// and their total. Never edges.
func (idx *RepositoryIndex) CandidateCalleeSample(id symbol.SymbolID) CandidateSample {
	if idx.completeness == nil {
		return CandidateSample{}
	}
	return idx.completeness.CandidateCalleeSample(id)
}

// NonEdgeRelationsFrom returns the uniquely resolved references inside id of a
// kind without graph semantics (read, write, ...). They are not graph edges.
func (idx *RepositoryIndex) NonEdgeRelationsFrom(id symbol.SymbolID) []NonEdgeRelation {
	if idx.nonEdge == nil {
		return nil
	}
	return copyNonEdge(idx.nonEdge.from[id])
}

// NonEdgeRelationsTo returns the uniquely resolved references to id of a kind
// without graph semantics. They are not graph edges.
func (idx *RepositoryIndex) NonEdgeRelationsTo(id symbol.SymbolID) []NonEdgeRelation {
	if idx.nonEdge == nil {
		return nil
	}
	return copyNonEdge(idx.nonEdge.to[id])
}

// NonEdgeCandidates returns the candidate relations of references without
// graph semantics into id (incoming) and out of id (outgoing).
func (idx *RepositoryIndex) NonEdgeCandidates(id symbol.SymbolID) (incoming, outgoing CandidateSample) {
	if idx.nonEdge == nil {
		return CandidateSample{}, CandidateSample{}
	}
	return candidateSample(idx.nonEdge.candidates.in[id]), candidateSample(idx.nonEdge.candidates.out[id])
}

// UnidentifiedSources returns the relations into id from enclosing code that
// is not one identified symbol (see relations.go): all resolved ones and a
// bounded sample of the candidate ones. They are never edges; Completeness
// counts those of graph kinds as unattributed.
func (idx *RepositoryIndex) UnidentifiedSources(id symbol.SymbolID) UnidentifiedSourceSample {
	if idx.unidentified == nil {
		return UnidentifiedSourceSample{}
	}
	return idx.unidentified.sample(id)
}

// GetRelatedSymbols returns all edges (callers + callees + type uses) touching id.
func (idx *RepositoryIndex) GetRelatedSymbols(id symbol.SymbolID) []GraphEdge {
	seen := make(map[symbol.SymbolID]bool)
	var out []GraphEdge
	for _, e := range idx.callsFrom[id] {
		if !seen[e.To] {
			seen[e.To] = true
			out = append(out, e)
		}
	}
	for _, e := range idx.callsTo[id] {
		if !seen[e.From] {
			seen[e.From] = true
			out = append(out, e)
		}
	}
	sortEdges(out)
	return out
}

// ---- Meta ----

// Files returns all indexed file IDs in deterministic order.
func (idx *RepositoryIndex) Files() []source.FileID {
	out := make([]source.FileID, len(idx.files))
	copy(out, idx.files)
	return out
}

// Diagnostics returns all diagnostics collected during index construction.
func (idx *RepositoryIndex) Diagnostics() []language.Diagnostic {
	out := make([]language.Diagnostic, len(idx.diagnostics))
	copy(out, idx.diagnostics)
	return out
}

// Stats returns aggregate statistics for the index.
// The returned Languages map is a copy; callers may modify it freely.
func (idx *RepositoryIndex) Stats() IndexStats {
	s := idx.stats
	if idx.stats.Languages != nil {
		s.Languages = make(map[string]int, len(idx.stats.Languages))
		for k, v := range idx.stats.Languages {
			s.Languages[k] = v
		}
	}
	return s
}

// ---- helpers ----

// copyEdges returns a deep copy of edges, including each edge's Evidence slice.
func copyEdges(edges []GraphEdge) []GraphEdge {
	if len(edges) == 0 {
		return nil
	}
	out := make([]GraphEdge, len(edges))
	for i, e := range edges {
		out[i] = e
		if len(e.Evidence) > 0 {
			ev := make([]resolver.ResolutionEvidence, len(e.Evidence))
			copy(ev, e.Evidence)
			out[i].Evidence = ev
		}
	}
	return out
}

func sortSymbols(syms []symbol.Symbol) {
	sort.Slice(syms, func(i, j int) bool {
		if syms[i].Location.File != syms[j].Location.File {
			return syms[i].Location.File < syms[j].Location.File
		}
		return syms[i].Qualified < syms[j].Qualified
	})
}

func sortEdges(edges []GraphEdge) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})
}
