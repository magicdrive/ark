package index

import (
	"slices"
	"sort"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// builder accumulates mutable state during index construction.
type builder struct {
	symbolsByID        map[symbol.SymbolID]symbol.Symbol
	symbolsByFile      map[source.FileID][]symbol.Symbol
	symbolsByName      map[string][]symbol.Symbol
	symbolsByQualified map[string][]symbol.Symbol
	symbolsByKind      map[symbol.SymbolKind][]symbol.Symbol

	// referencesByFile: all references per file
	referencesByFile map[source.FileID][]reference.Reference
	// referencesByContainer: populated in freeze() after symbol IDs are known
	referencesByContainer map[symbol.SymbolID][]reference.Reference
	// referencesByTarget: populated in resolve()
	referencesByTarget map[symbol.SymbolID][]reference.Reference

	callsFrom map[symbol.SymbolID][]GraphEdge
	callsTo   map[symbol.SymbolID][]GraphEdge

	fileSet     map[source.FileID]bool
	diagnostics []language.Diagnostic
	stats       IndexStats

	// for resolution
	resolverFiles []resolver.FileIndex

	// completeness records references that do not become edges; built in
	// resolve().
	completeness *Completeness
	nonEdge      *nonEdgeRelations
	unidentified *unidentifiedSources

	// fingerprint identifies the index's inputs (see sourceDigest).
	fingerprint string

	// containers caches (file, qualified) → distinct symbol IDs for container
	// identification; built lazily during resolve().
	containers map[source.FileID]map[string][]symbol.SymbolID
}

func newBuilder() *builder {
	return &builder{
		symbolsByID:           make(map[symbol.SymbolID]symbol.Symbol),
		symbolsByFile:         make(map[source.FileID][]symbol.Symbol),
		symbolsByName:         make(map[string][]symbol.Symbol),
		symbolsByQualified:    make(map[string][]symbol.Symbol),
		symbolsByKind:         make(map[symbol.SymbolKind][]symbol.Symbol),
		referencesByFile:      make(map[source.FileID][]reference.Reference),
		referencesByContainer: make(map[symbol.SymbolID][]reference.Reference),
		referencesByTarget:    make(map[symbol.SymbolID][]reference.Reference),
		callsFrom:             make(map[symbol.SymbolID][]GraphEdge),
		callsTo:               make(map[symbol.SymbolID][]GraphEdge),
		fileSet:               make(map[source.FileID]bool),
		stats:                 IndexStats{Languages: make(map[string]int)},
	}
}

func (b *builder) addDiagnostic(d language.Diagnostic) {
	b.diagnostics = append(b.diagnostics, d)
}

func (b *builder) addDiagnostics(ds []language.Diagnostic) {
	b.diagnostics = append(b.diagnostics, ds...)
}

func (b *builder) ingestExtraction(fileID source.FileID, lang string, ex language.Extraction) {
	if !b.fileSet[fileID] {
		b.fileSet[fileID] = true
		b.stats.Languages[lang]++
	}

	fi := NewFileIndex(lang, fileID, ex)
	for _, sym := range fi.Symbols {
		b.symbolsByID[sym.ID] = sym
		b.symbolsByFile[fileID] = append(b.symbolsByFile[fileID], sym)
		b.symbolsByName[sym.Name] = append(b.symbolsByName[sym.Name], sym)
		if sym.Qualified != "" {
			b.symbolsByQualified[sym.Qualified] = append(b.symbolsByQualified[sym.Qualified], sym)
		}
		b.symbolsByKind[sym.Kind] = append(b.symbolsByKind[sym.Kind], sym)
		b.stats.Symbols++
	}
	b.referencesByFile[fileID] = fi.References
	b.stats.References += len(fi.References)
	b.resolverFiles = append(b.resolverFiles, fi)
}

// NewFileIndex converts one file's provider Extraction into the resolver's
// FileIndex. It is the single conversion used by every pipeline that feeds the
// resolver (repository indexing and MCP relations), so symbol identity,
// containment and reference evidence cannot diverge between surfaces.
func NewFileIndex(lang string, fileID source.FileID, ex language.Extraction) resolver.FileIndex {
	// Build symbols.
	var fileSymbols []symbol.Symbol
	for _, sd := range ex.Symbols {
		sym := symbol.Symbol{
			ID:        symbol.NewSymbolID(lang, string(fileID), sd.Kind, sd.Qualified),
			Name:      sd.Name,
			Qualified: sd.Qualified,
			Kind:      sd.Kind,
			Language:  lang,
			Location:  sd.Location,
			Receiver:  sd.Receiver,
			Signature: sd.Signature,
			Exported:  sd.Exported,

			Visibility: sd.Visibility,

			MemberScope:    sd.MemberScope,
			MembersOutside: sd.MembersOutside,
		}
		if sd.Parent != "" {
			sym.Parent = symbol.NewSymbolID(lang, string(fileID), symbol.KindUnknown, sd.Parent)
			sym.ParentQualified = sd.Parent
		}
		fileSymbols = append(fileSymbols, sym)
	}

	// Build references. Container stays as a qualified-name string (reference.Reference.Container is string).
	var fileRefs []reference.Reference
	for _, rd := range ex.References {
		rk := reference.ReferenceKind(rd.Kind)
		fileRefs = append(fileRefs, reference.Reference{
			ID:           reference.NewReferenceID(lang, fileID, rk, rd.Name, rd.Location),
			Name:         rd.Name,
			Kind:         rk,
			Language:     lang,
			Location:     rd.Location,
			Container:    rd.Container, // qualified name string
			ReceiverExpr: rd.ReceiverExpr,
			ReceiverType: rd.ReceiverType,

			NameQualified:         rd.NameQualified,
			ReceiverTypeQualified: rd.ReceiverTypeQualified,
			ConfidenceCap:         rd.ConfidenceCap,
			Dynamic:               rd.Dynamic,
			IdentityInRepository:  rd.IdentityInRepository,
			IsCall:                rd.IsCall,
		})
	}
	// Also index imports as KindImport references so package-dependency queries
	// (e.g. repomap) can use ReferencesByFile without a separate import API.
	for _, imp := range ex.Imports {
		fileRefs = append(fileRefs, reference.Reference{
			ID:       reference.NewReferenceID(lang, fileID, reference.KindImport, imp.Path, imp.Location),
			Name:     imp.Path,
			Kind:     reference.KindImport,
			Language: lang,
			Location: imp.Location,
		})
	}

	return resolver.FileIndex{
		FileID:       fileID,
		Language:     lang,
		Symbols:      fileSymbols,
		References:   fileRefs,
		Imports:      ex.Imports,
		Bindings:     ex.Bindings,
		Exports:      ex.Exports,
		ModuleScoped: ex.ModuleScoped,
		IdentityOnly: ex.IdentityOnly,
	}
}

// resolve runs the resolver and builds graph edges.
func (b *builder) resolve() {
	if len(b.resolverFiles) == 0 {
		return
	}
	r := resolver.New(b.resolverFiles)
	resolutions := r.Resolve()

	// Build lookup: referenceID → (file, reference)
	type refMeta struct {
		file source.FileID
		ref  *reference.Reference
	}
	refMetaMap := make(map[reference.ReferenceID]refMeta)
	for i := range b.resolverFiles {
		fi := &b.resolverFiles[i]
		for j := range fi.References {
			refMetaMap[fi.References[j].ID] = refMeta{file: fi.FileID, ref: &fi.References[j]}
		}
	}

	b.completeness = NewCompleteness(b.symbolsByName)
	b.nonEdge = newNonEdgeRelations()
	b.unidentified = newUnidentifiedSources()
	for _, res := range resolutions {
		meta, ok := refMetaMap[res.ReferenceID]
		if !ok {
			continue
		}
		// The container is identified by (file, qualified) — never by a
		// repository-global qualified name, which may be declared in many files.
		containerID, hasContainer := b.containerSymbol(meta.file, meta.ref.Container)

		// Record what the graph will not show: references that may involve a
		// symbol but do not become an edge (see completeness.go).
		b.completeness.Observe(*meta.ref, res, containerID, hasContainer)
		if !hasContainer {
			b.unidentified.observe(meta.file, *meta.ref, res)
		}

		kind, ok := edgeKindFor(meta.ref.Kind)
		if !ok {
			// No graph semantics (read, write, ...): kept as a non-edge
			// relation, never an edge, never counted by completeness.
			if hasContainer {
				b.nonEdge.observe(*meta.ref, res, containerID)
			}
			continue
		}
		// Only create a graph edge when resolution is unambiguous and the
		// container is identified.
		if !res.HasUniqueTarget() || !hasContainer {
			continue
		}
		best := res.Candidates[0]

		edge := GraphEdge{
			From:       containerID,
			To:         best.SymbolID,
			Kind:       kind,
			RefKind:    meta.ref.Kind,
			Confidence: res.Confidence,
			Evidence:   res.Evidence,
		}
		b.callsFrom[containerID] = append(b.callsFrom[containerID], edge)

		// Reverse edge.
		rev := GraphEdge{
			From:       best.SymbolID,
			To:         containerID,
			Kind:       EdgeCalledBy,
			RefKind:    meta.ref.Kind,
			Confidence: res.Confidence,
			Evidence:   res.Evidence,
		}
		b.callsTo[best.SymbolID] = append(b.callsTo[best.SymbolID], rev)
		b.stats.Relations++

		// Populate referencesByTarget.
		b.referencesByTarget[best.SymbolID] = append(b.referencesByTarget[best.SymbolID], reference.Reference{
			ID:        res.ReferenceID,
			Name:      res.ReferenceName,
			Kind:      meta.ref.Kind,
			Container: meta.ref.Container,
		})
	}

	// Populate referencesByContainer using qualified name → SymbolID lookup.
	for _, fi := range b.resolverFiles {
		for _, ref := range fi.References {
			if ref.Container == "" {
				continue
			}
			cid, ok := b.containerSymbol(fi.FileID, ref.Container)
			if !ok {
				continue
			}
			b.referencesByContainer[cid] = append(b.referencesByContainer[cid], ref)
		}
	}
}

// containerSymbol identifies the enclosing symbol of a reference by
// (file, qualified). It reports false when no symbol or more than one distinct
// symbol in that file carries the qualified name: an unidentifiable container
// produces no edge rather than an arbitrarily chosen one.
func (b *builder) containerSymbol(file source.FileID, qualified string) (symbol.SymbolID, bool) {
	if qualified == "" {
		return "", false
	}
	if b.containers == nil {
		b.containers = make(map[source.FileID]map[string][]symbol.SymbolID, len(b.symbolsByFile))
	}
	byQual, ok := b.containers[file]
	if !ok {
		byQual = make(map[string][]symbol.SymbolID)
		for _, s := range b.symbolsByFile[file] {
			if !slices.Contains(byQual[s.Qualified], s.ID) {
				byQual[s.Qualified] = append(byQual[s.Qualified], s.ID)
			}
		}
		b.containers[file] = byQual
	}
	ids := byQual[qualified]
	if len(ids) != 1 {
		return "", false
	}
	return ids[0], true
}

// freeze converts builder state into an immutable RepositoryIndex.
func (b *builder) freeze() *RepositoryIndex {
	files := make([]source.FileID, 0, len(b.fileSet))
	for fid := range b.fileSet {
		files = append(files, fid)
	}
	sort.Slice(files, func(i, j int) bool { return files[i] < files[j] })
	b.stats.Files = len(files)

	// Sort per-file symbol slices by start line.
	for fid := range b.symbolsByFile {
		sl := b.symbolsByFile[fid]
		sort.Slice(sl, func(i, j int) bool {
			return sl[i].Location.Range.Start.Line < sl[j].Location.Range.Start.Line
		})
		b.symbolsByFile[fid] = sl
	}

	if b.completeness != nil {
		b.completeness.seal()
	}
	if b.nonEdge != nil {
		b.nonEdge.candidates.seal()
	}
	if b.unidentified != nil {
		b.unidentified.seal()
	}

	// Deduplicate and sort graph edges.
	for id := range b.callsFrom {
		b.callsFrom[id] = dedupeEdges(b.callsFrom[id])
	}
	for id := range b.callsTo {
		b.callsTo[id] = dedupeEdges(b.callsTo[id])
	}

	return &RepositoryIndex{
		symbolsByID:           b.symbolsByID,
		symbolsByFile:         b.symbolsByFile,
		symbolsByName:         b.symbolsByName,
		symbolsByQualified:    b.symbolsByQualified,
		symbolsByKind:         b.symbolsByKind,
		referencesByFile:      b.referencesByFile,
		referencesByContainer: b.referencesByContainer,
		referencesByTarget:    b.referencesByTarget,
		callsFrom:             b.callsFrom,
		callsTo:               b.callsTo,
		completeness:          b.completeness,
		nonEdge:               b.nonEdge,
		unidentified:          b.unidentified,
		fingerprint:           b.fingerprint,
		files:                 files,
		diagnostics:           b.diagnostics,
		stats:                 b.stats,
	}
}

// edgeKey uniquely identifies a directed relationship including its semantic kind.
// Two edges sharing the same (From, To, Kind) are considered duplicates and merged.
type edgeKey struct {
	From symbol.SymbolID
	To   symbol.SymbolID
	Kind EdgeKind
}

func dedupeEdges(edges []GraphEdge) []GraphEdge {
	type merged struct {
		edge   GraphEdge
		evSeen map[string]bool
	}
	seen := make(map[edgeKey]*merged, len(edges))
	// order preserves first-occurrence ordering before the final sort.
	order := make([]edgeKey, 0, len(edges))

	for _, e := range edges {
		key := edgeKey{e.From, e.To, e.Kind}
		if m, ok := seen[key]; ok {
			// Merge: keep the highest confidence.
			if e.Confidence > m.edge.Confidence {
				m.edge.Confidence = e.Confidence
			}
			// Merge unique evidence entries.
			for _, ev := range e.Evidence {
				ek := string(ev.Kind) + "\x00" + ev.Detail
				if !m.evSeen[ek] {
					m.evSeen[ek] = true
					m.edge.Evidence = append(m.edge.Evidence, ev)
				}
			}
		} else {
			evSeen := make(map[string]bool, len(e.Evidence))
			for _, ev := range e.Evidence {
				evSeen[string(ev.Kind)+"\x00"+ev.Detail] = true
			}
			m := &merged{edge: e, evSeen: evSeen}
			seen[key] = m
			order = append(order, key)
		}
	}

	out := make([]GraphEdge, 0, len(order))
	for _, key := range order {
		m := seen[key]
		// Sort evidence deterministically so output is stable.
		sort.Slice(m.edge.Evidence, func(i, j int) bool {
			ki, kj := string(m.edge.Evidence[i].Kind), string(m.edge.Evidence[j].Kind)
			if ki != kj {
				return ki < kj
			}
			return m.edge.Evidence[i].Detail < m.edge.Evidence[j].Detail
		})
		out = append(out, m.edge)
	}
	sortEdges(out)
	return out
}

// edgeKindFor maps a reference kind to the graph edge it forms. Construction is
// modelled as a call-like edge — an explicit decision, NOT a default fallback:
// `new T()` depends on T much like a call (this preserves the pre-D4 behaviour
// that relied on the old default). A value reference and an explicit
// dependency (configuration languages) form dependency edges of their own,
// never call edges. Every other kind (read/write/unknown or any
// future kind) has no graph semantics and never forms an edge.
func edgeKindFor(k reference.ReferenceKind) (EdgeKind, bool) {
	switch k {
	case reference.KindCall, reference.KindConstruction:
		return EdgeCalls, true
	case reference.KindTypeUse:
		return EdgeUsesType, true
	case reference.KindImport:
		return EdgeImports, true
	case reference.KindInheritance:
		return EdgeExtends, true
	case reference.KindImplements:
		return EdgeImplements, true
	case reference.KindUsesTrait:
		return EdgeUsesTrait, true
	case reference.KindValueReference:
		return EdgeReferences, true
	case reference.KindExplicitDependency:
		return EdgeDependsOn, true
	}
	return "", false
}
