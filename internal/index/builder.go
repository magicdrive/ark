package index

import (
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
		}
		if sd.Parent != "" {
			sym.Parent = symbol.NewSymbolID(lang, string(fileID), symbol.KindUnknown, sd.Parent)
			sym.ParentQualified = sd.Parent
		}

		b.symbolsByID[sym.ID] = sym
		b.symbolsByFile[fileID] = append(b.symbolsByFile[fileID], sym)
		b.symbolsByName[sym.Name] = append(b.symbolsByName[sym.Name], sym)
		if sym.Qualified != "" {
			b.symbolsByQualified[sym.Qualified] = append(b.symbolsByQualified[sym.Qualified], sym)
		}
		b.symbolsByKind[sym.Kind] = append(b.symbolsByKind[sym.Kind], sym)
		fileSymbols = append(fileSymbols, sym)
		b.stats.Symbols++
	}

	// Build references. Container stays as a qualified-name string (reference.Reference.Container is string).
	var fileRefs []reference.Reference
	for _, rd := range ex.References {
		rk := reference.ReferenceKind(rd.Kind)
		ref := reference.Reference{
			ID:           reference.NewReferenceID(lang, fileID, rk, rd.Name, rd.Location),
			Name:         rd.Name,
			Kind:         rk,
			Language:     lang,
			Location:     rd.Location,
			Container:    rd.Container, // qualified name string
			ReceiverExpr: rd.ReceiverExpr,
			IsCall:       rd.IsCall,
		}
		fileRefs = append(fileRefs, ref)
		b.stats.References++
	}
	// Also index imports as KindImport references so package-dependency queries
	// (e.g. repomap) can use ReferencesByFile without a separate import API.
	for _, imp := range ex.Imports {
		ref := reference.Reference{
			ID:       reference.NewReferenceID(lang, fileID, reference.KindImport, imp.Path, imp.Location),
			Name:     imp.Path,
			Kind:     reference.KindImport,
			Language: lang,
			Location: imp.Location,
		}
		fileRefs = append(fileRefs, ref)
		b.stats.References++
	}
	b.referencesByFile[fileID] = fileRefs

	b.resolverFiles = append(b.resolverFiles, resolver.FileIndex{
		FileID:     fileID,
		Language:   lang,
		Symbols:    fileSymbols,
		References: fileRefs,
		Imports:    ex.Imports,
	})
}

// resolve runs the resolver and builds graph edges.
func (b *builder) resolve() {
	if len(b.resolverFiles) == 0 {
		return
	}
	r := resolver.New(b.resolverFiles)
	resolutions := r.Resolve()

	// Build lookup: referenceID → (container qualified name, kind)
	type refMeta struct {
		containerQual string
		kind          reference.ReferenceKind
	}
	refMetaMap := make(map[reference.ReferenceID]refMeta)
	for _, fi := range b.resolverFiles {
		for _, ref := range fi.References {
			refMetaMap[ref.ID] = refMeta{containerQual: ref.Container, kind: ref.Kind}
		}
	}

	for _, res := range resolutions {
		// Only create a graph edge when resolution is unambiguous.
		if !res.HasUniqueTarget() {
			continue
		}
		best := res.Candidates[0]
		meta := refMetaMap[res.ReferenceID]

		if meta.containerQual == "" {
			continue
		}

		// Look up container SymbolID from qualified name.
		containerSyms := b.symbolsByQualified[meta.containerQual]
		if len(containerSyms) == 0 {
			continue
		}
		containerID := containerSyms[0].ID

		var kind EdgeKind
		switch meta.kind {
		case reference.KindCall:
			kind = EdgeCalls
		case reference.KindTypeUse:
			kind = EdgeUsesType
		case reference.KindImport:
			kind = EdgeImports
		default:
			kind = EdgeCalls
		}

		edge := GraphEdge{
			From:       containerID,
			To:         best.SymbolID,
			Kind:       kind,
			Confidence: res.Confidence,
			Evidence:   res.Evidence,
		}
		b.callsFrom[containerID] = append(b.callsFrom[containerID], edge)

		// Reverse edge.
		rev := GraphEdge{
			From:       best.SymbolID,
			To:         containerID,
			Kind:       EdgeCalledBy,
			Confidence: res.Confidence,
			Evidence:   res.Evidence,
		}
		b.callsTo[best.SymbolID] = append(b.callsTo[best.SymbolID], rev)
		b.stats.Relations++

		// Populate referencesByTarget.
		b.referencesByTarget[best.SymbolID] = append(b.referencesByTarget[best.SymbolID], reference.Reference{
			ID:        res.ReferenceID,
			Name:      res.ReferenceName,
			Kind:      meta.kind,
			Container: meta.containerQual,
		})
	}

	// Populate referencesByContainer using qualified name → SymbolID lookup.
	for _, fi := range b.resolverFiles {
		for _, ref := range fi.References {
			if ref.Container == "" {
				continue
			}
			syms := b.symbolsByQualified[ref.Container]
			if len(syms) == 0 {
				continue
			}
			cid := syms[0].ID
			b.referencesByContainer[cid] = append(b.referencesByContainer[cid], ref)
		}
	}
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
