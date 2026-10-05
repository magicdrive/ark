package index

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	From       symbol.SymbolID
	To         symbol.SymbolID
	Kind       EdgeKind
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

	// --- meta ---
	files       []source.FileID // sorted
	diagnostics []language.Diagnostic
	stats       IndexStats
}

// New scans root, extracts symbols/references using providers, resolves
// references, and builds an immutable index. Partial failures are recorded
// in Diagnostics rather than aborting the build.
func New(ctx context.Context, root string, providers []language.Provider) (*RepositoryIndex, error) {
	if info, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("index: root %q: %w", root, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("index: root %q is not a directory", root)
	}

	// Build extension → provider map.
	extMap := make(map[string]language.Provider)
	for _, p := range providers {
		for _, ext := range p.Extensions() {
			extMap[ext] = p
		}
	}

	b := newBuilder()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable dirs
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if SkipDirName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		prov, ok := extMap[ext]
		if !ok {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			b.addDiagnostic(language.Diagnostic{
				Severity: language.SeverityWarning,
				Message:  "read error: " + err.Error(),
			})
			b.stats.Skipped++
			return nil
		}

		relPath, _ := filepath.Rel(root, path)
		fileID := source.FileID(relPath)

		extraction, err := prov.Extract(ctx, fileID, src)
		if err != nil {
			b.addDiagnostic(language.Diagnostic{
				Severity: language.SeverityWarning,
				Message:  path + ": extraction error: " + err.Error(),
			})
			b.stats.Skipped++
			return nil
		}
		b.addDiagnostics(extraction.Diagnostics)

		lang := string(prov.Language())
		b.ingestExtraction(fileID, lang, extraction)
		return nil
	})
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		// walkDir errors other than context are soft.
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	b.resolve()
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
