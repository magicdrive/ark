package index

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/asset"
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
	// referenceIDs: the ReferenceIDs of the references ingested so far
	// (uniqueReferences).
	referenceIDs map[reference.ReferenceID]bool
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

	// ids assigns SymbolIDs (symbol.NewDeclarationID unless a test injects
	// collisions); collided holds the IDs found on distinct declarations
	// (identity.go).
	ids      IDFunc
	collided map[symbol.SymbolID]bool

	// rootName is the repository root directory's name (resolver evidence
	// for package-scoped import paths).
	rootName string

	// nameSpaces maps a language to its files' name space
	// (language.NameSpace of its provider).
	nameSpaces map[string]string

	// assetPaths are the index's asset files (internal/asset) the walk
	// found; assets the nodes made for those a reference names
	// (materializeAssets), apart from the source files and their symbols.
	assetPaths map[source.FileID]bool
	assets     []symbol.Symbol
}

// nameSpaces returns each provider's language's name space.
func nameSpaces(providers []language.Provider) map[string]string {
	out := make(map[string]string, len(providers))
	for _, p := range providers {
		out[string(p.Language())] = string(language.NameSpace(p))
	}
	return out
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
		ids:                   symbol.NewDeclarationID,
	}
}

// uniqueReferences keeps the references whose ReferenceID no reference of
// the index has yet. Every map from a reference's ID to the reference (the
// resolver's results, completeness, relations) relies on it: a reference
// sharing an ID — stated twice by a provider, or a hash collision — is left
// out with a diagnostic (language.DiagReferenceIDCollision), never joined to
// the other's resolution. IDs differ for distinct spans by construction
// (reference.NewReferenceID), so no provider of the repository triggers it.
// The first stated is kept, Speculative or not (dropSpeculative runs later):
// a Speculative reference never shares a span and kind with another (Go
// states it as a value_reference of a selector it calls nothing on).
func (b *builder) uniqueReferences(refs []reference.Reference) []reference.Reference {
	if b.referenceIDs == nil {
		b.referenceIDs = make(map[reference.ReferenceID]bool)
	}
	kept := refs[:0:0]
	for _, r := range refs {
		if b.referenceIDs[r.ID] {
			b.addDiagnostic(language.Diagnostic{
				Severity: language.SeverityWarning,
				Message:  fmt.Sprintf("reference %s %q shares its ID %s with another reference: left out of the index", r.Kind, r.Name, r.ID),
				Location: r.Location,
				Code:     language.DiagReferenceIDCollision,
			})
			continue
		}
		b.referenceIDs[r.ID] = true
		kept = append(kept, r)
	}
	return kept
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

	fi := newFileIndex(lang, b.nameSpaces[lang], fileID, ex, b.ids)
	fi.References = b.uniqueReferences(fi.References)
	for _, sym := range fi.Symbols {
		b.observeID(sym)
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

// addAsset records an asset file (internal/asset) the walk found: its path
// only. Its node is made by materializeAssets when a reference names it.
func (b *builder) addAsset(fileID source.FileID) {
	if b.assetPaths == nil {
		b.assetPaths = make(map[source.FileID]bool)
	}
	b.assetPaths[fileID] = true
}

// materializeAssets makes the node of each asset file a reference names by
// path evidence (FileIdentity with the asset kind among its targets): an
// asset nothing links to costs its path only. It runs once every file is
// ingested, before identities are checked and references resolved.
func (b *builder) materializeAssets() {
	if len(b.assetPaths) == 0 {
		return
	}
	named := map[source.FileID]bool{}
	for _, fi := range b.resolverFiles {
		for _, r := range fi.References {
			if !r.FileIdentity || !strings.Contains(","+r.TargetKinds+",", ","+string(symbol.KindAsset)+",") {
				continue
			}
			if id := source.FileID(r.NameQualified); b.assetPaths[id] && !named[id] {
				named[id] = true
			}
		}
	}
	ids := make([]source.FileID, 0, len(named))
	for id := range named {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		b.addAssetNode(id)
	}
}

// addAssetNode adds the node of an asset file: a symbol of kind asset whose
// identity is its path alone — its content is never read, so changing it
// changes nothing — reached by path evidence only (symbol.DeclaresFile). It
// is no source file of the index (Files, SymbolsByFile and the statistics
// leave it out) and has no name in the name index; GetSymbol and
// FindSymbolsByQualified find it, and the resolver sees it as a file of the
// asset language declaring itself.
func (b *builder) addAssetNode(fileID source.FileID) {
	path := string(fileID)
	sym := symbol.Symbol{
		ID:        b.ids(asset.Language, path, symbol.KindAsset, path, 1),
		Name:      filepath.Base(filepath.FromSlash(path)),
		Qualified: path,
		Kind:      symbol.KindAsset,
		Language:  asset.Language,
		Location:  source.Location{File: fileID},
		Signature: asset.Category(filepath.Ext(path)),
	}
	b.observeID(sym)
	b.symbolsByQualified[sym.Qualified] = append(b.symbolsByQualified[sym.Qualified], sym)
	b.symbolsByKind[sym.Kind] = append(b.symbolsByKind[sym.Kind], sym)
	b.assets = append(b.assets, sym)
	b.resolverFiles = append(b.resolverFiles, resolver.FileIndex{
		FileID:       fileID,
		Language:     asset.Language,
		NameSpace:    asset.Language,
		Symbols:      []symbol.Symbol{sym},
		IdentityOnly: true,
	})
}

// NewFileIndex converts one file's Extraction by provider p into the
// resolver's FileIndex, exactly as repository indexing does (newFileIndex):
// symbol identity, containment, reference evidence and the name space
// (language.NameSpace of p) cannot diverge between the index and a resolver
// fed directly.
func NewFileIndex(p language.Provider, fileID source.FileID, ex language.Extraction) resolver.FileIndex {
	return newFileIndex(string(p.Language()), string(language.NameSpace(p)), fileID, ex, symbol.NewDeclarationID)
}

// newFileIndex is the one conversion; space is the file's name space
// (language.NameSpace of its provider).
func newFileIndex(lang, space string, fileID source.FileID, ex language.Extraction, ids IDFunc) resolver.FileIndex {
	// Build symbols. Each declaration is its own symbol: namesakes in one file
	// (same kind and qualified name) get distinct ordinal IDs.
	drafts := uniqueDeclarations(ex.Symbols)
	ordinals := declarationOrdinals(drafts)
	var fileSymbols []symbol.Symbol
	for i, sd := range drafts {
		sym := symbol.Symbol{
			ID:        ids(lang, string(fileID), sd.Kind, sd.Qualified, ordinals[i]),
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
			ParameterScope: sd.ParameterScope,
			MembersOutside: sd.MembersOutside,
			Placement:      sd.Placement,
		}
		// ParentQualified is the provider's statement, kept as is for the
		// resolver's name rules; Parent, the identity, is resolved below.
		sym.ParentQualified = sd.Parent
		fileSymbols = append(fileSymbols, sym)
	}
	resolveParents(fileSymbols)

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
			TargetKinds:           rd.TargetKinds,
			Dynamic:               rd.Dynamic,
			IdentityInRepository:  rd.IdentityInRepository,
			FileIdentity:          rd.FileIdentity,
			NamedArgument:         rd.NamedArgument,
			IsCall:                rd.IsCall,
			ReceiverChain:         rd.ReceiverChain,
			Speculative:           rd.Speculative,
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

		Package:           ex.Package,
		PackageScoped:     ex.PackageScoped,
		MembersShareNames: ex.MembersShareNames,
		NameSpace:         space,
		Aliases:           ex.Aliases,
		TypeFacts:         ex.TypeFacts,
	}
}

// resolveParents sets each symbol's Parent to the SymbolID of the declaration
// its provider names as its enclosing symbol (SymbolDraft.Parent, a qualified
// name in the same file — a provider reads one file). The parent is the one
// declaration of the file that carries that qualified name and whose range
// contains the child's: the name is the provider's evidence, containment is
// what "enclosing" means and tells namesakes apart. Parent stays empty when no
// declaration or several qualify, and for every symbol of a parent cycle: an
// unknown parent is stated as unknown, never invented.
func resolveParents(syms []symbol.Symbol) {
	byQualified := make(map[string][]int)
	for i, s := range syms {
		byQualified[s.Qualified] = append(byQualified[s.Qualified], i)
	}
	for i := range syms {
		c := &syms[i]
		if c.ParentQualified == "" {
			continue
		}
		found := -1
		cands := byQualified[c.ParentQualified]
		// A file's own declaration (its Qualified is the file's path) may
		// share the name of a member (a method js of app, in app.js): the
		// member is the parent, when there is one.
		if slices.ContainsFunc(cands, func(j int) bool { return !symbol.DeclaresFile(syms[j].Kind) }) {
			cands = slices.DeleteFunc(slices.Clone(cands), func(j int) bool { return symbol.DeclaresFile(syms[j].Kind) })
		}
		for _, j := range cands {
			if j == i || syms[j].ID == c.ID || !rangeEncloses(syms[j].Location.Range, c.Location.Range) {
				continue
			}
			if found >= 0 {
				found = -2 // several enclosing namesakes: unknown
				break
			}
			found = j
		}
		if found >= 0 {
			c.Parent = syms[found].ID
		}
	}
	breakParentCycles(syms)
}

// breakParentCycles clears Parent on every symbol that lies on a parent
// cycle. Which symbols are cleared depends only on the cycle, never on the
// order the symbols are visited in.
func breakParentCycles(syms []symbol.Symbol) {
	index := make(map[symbol.SymbolID]int, len(syms))
	for i, s := range syms {
		index[s.ID] = i
	}
	var onCycle []int
	for i := range syms {
		// Walk at most len(syms) steps from i; returning to i means i is on
		// a cycle.
		cur := i
		for range syms {
			p, ok := index[syms[cur].Parent]
			if syms[cur].Parent == "" || !ok {
				break
			}
			if cur = p; cur == i {
				onCycle = append(onCycle, i)
				break
			}
		}
	}
	for _, i := range onCycle {
		syms[i].Parent = ""
	}
}

// rangeEncloses reports whether outer contains inner.
func rangeEncloses(outer, inner source.Range) bool {
	return !lessPosition(inner.Start, outer.Start) && !lessPosition(outer.End, inner.End)
}

// uniqueDeclarations drops a draft that repeats an earlier one exactly: the
// same declaration stated twice is one symbol, not two namesakes. Drafts
// that differ in anything are all kept — choosing one of two conflicting
// statements would depend on which came first.
func uniqueDeclarations(drafts []language.SymbolDraft) []language.SymbolDraft {
	seen := make(map[language.SymbolDraft]bool, len(drafts))
	out := drafts[:0:0]
	for _, d := range drafts {
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// declarationOrdinals numbers each draft among the file's drafts with the
// same kind and qualified name, 1-based in source order (start, then end
// position, then the draft's content for drafts at the same range). Never by
// the provider's emission order: the IDs are a function of the file's
// content.
func declarationOrdinals(drafts []language.SymbolDraft) []int {
	type key struct {
		kind      symbol.SymbolKind
		qualified string
	}
	groups := make(map[key][]int)
	for i, d := range drafts {
		k := key{d.Kind, d.Qualified}
		groups[k] = append(groups[k], i)
	}
	ordinals := make([]int, len(drafts))
	for _, idxs := range groups {
		sort.Slice(idxs, func(a, b int) bool {
			da, db := drafts[idxs[a]], drafts[idxs[b]]
			if da.Location.Range != db.Location.Range {
				return positionBefore(da.Location.Range, db.Location.Range)
			}
			return fmt.Sprintf("%+v", da) < fmt.Sprintf("%+v", db)
		})
		for n, i := range idxs {
			ordinals[i] = n + 1
		}
	}
	return ordinals
}

func positionBefore(a, b source.Range) bool {
	if a.Start != b.Start {
		return lessPosition(a.Start, b.Start)
	}
	return lessPosition(a.End, b.End)
}

func lessPosition(a, b source.Position) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Column < b.Column
}

// resolve runs the resolver and builds graph edges.
func (b *builder) resolve() {
	if len(b.resolverFiles) == 0 {
		return
	}
	r := resolver.NewInRoot(b.resolverFiles, b.rootName)
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

	dropped := b.dropSpeculative(resolutions, func(id reference.ReferenceID) *reference.Reference {
		if m, ok := refMetaMap[id]; ok {
			return m.ref
		}
		return nil
	})

	// A file's own declaration (symbol.DeclaresFile) is reached by path
	// evidence only: its name (the file's) is no name a reference may mean.
	byName := make(map[string][]symbol.Symbol, len(b.symbolsByName))
	for name, syms := range b.symbolsByName {
		for _, s := range syms {
			if !symbol.DeclaresFile(s.Kind) {
				byName[name] = append(byName[name], s)
			}
		}
	}
	b.completeness = NewCompleteness(byName)
	b.nonEdge = newNonEdgeRelations()
	b.unidentified = newUnidentifiedSources()
	for _, res := range resolutions {
		meta, ok := refMetaMap[res.ReferenceID]
		if !ok || dropped[res.ReferenceID] {
			continue
		}
		// The container is identified by (file, qualified) — never by a
		// repository-global qualified name, which may be declared in many files.
		containerID, hasContainer := b.containerSymbol(meta.file, meta.ref.Container, meta.ref.Location)

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
			if ref.Container == "" || dropped[ref.ID] {
				continue
			}
			cid, ok := b.containerSymbol(fi.FileID, ref.Container, ref.Location)
			if !ok {
				continue
			}
			b.referencesByContainer[cid] = append(b.referencesByContainer[cid], ref)
		}
	}
}

// dropSpeculative returns the Speculative references resolved to no
// candidate (language.ReferenceDraft.Speculative): a selector that names a
// member no provider extracts as a symbol, which is no reference at all. They
// are removed from the per-file lists and the statistics here, and from
// completeness, relations and the per-container lists by the caller —
// before anything counts them.
func (b *builder) dropSpeculative(resolutions []resolver.Resolution, refOf func(reference.ReferenceID) *reference.Reference) map[reference.ReferenceID]bool {
	dropped := map[reference.ReferenceID]bool{}
	for _, res := range resolutions {
		if ref := refOf(res.ReferenceID); ref != nil && ref.Speculative && len(res.Candidates) == 0 {
			dropped[res.ReferenceID] = true
		}
	}
	if len(dropped) == 0 {
		return dropped
	}
	for i := range b.resolverFiles {
		fid := b.resolverFiles[i].FileID
		refs := b.referencesByFile[fid]
		var kept []reference.Reference
		for _, ref := range refs {
			if !dropped[ref.ID] {
				kept = append(kept, ref)
			}
		}
		if len(kept) != len(refs) {
			b.stats.References -= len(refs) - len(kept)
			b.referencesByFile[fid] = kept
		}
	}
	return dropped
}

// containerSymbol identifies the enclosing symbol of a reference at loc by
// (file, qualified). Several symbols of the file may carry the qualified name
// (namesake declarations, or one name declared with several kinds); then the
// container is the one whose source range contains the reference — lexical
// containment, which is what "enclosing" means — and only if exactly one does.
// It reports false otherwise: an unidentifiable container produces no edge
// rather than an arbitrarily chosen one.
func (b *builder) containerSymbol(file source.FileID, qualified string, loc source.Location) (symbol.SymbolID, bool) {
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
	// A file's own declaration (its Qualified is the file's path) contains
	// everything: when a member shares the name (a method js of app, in
	// app.js), the member is the container.
	if len(ids) > 1 && slices.ContainsFunc(ids, func(id symbol.SymbolID) bool { return !symbol.DeclaresFile(b.symbolsByID[id].Kind) }) {
		ids = slices.DeleteFunc(slices.Clone(ids), func(id symbol.SymbolID) bool { return symbol.DeclaresFile(b.symbolsByID[id].Kind) })
	}
	switch len(ids) {
	case 0:
		return "", false
	case 1:
		return ids[0], true
	}
	var found symbol.SymbolID
	n := 0
	for _, id := range ids {
		if s, ok := b.symbolsByID[id]; ok && s.Location.File == file && rangeContains(s.Location.Range, loc.Range.Start) {
			found = id
			n++
		}
	}
	if n != 1 {
		return "", false
	}
	return found, true
}

// rangeContains reports whether p lies in the half-open range r.
func rangeContains(r source.Range, p source.Position) bool {
	return !lessPosition(p, r.Start) && lessPosition(p, r.End)
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
		aliases:               b.aliases(),
		assets:                b.sortedAssets(),
		assetPaths:            b.assetPaths,
	}
}

func (b *builder) sortedAssets() []symbol.Symbol {
	sort.Slice(b.assets, func(i, j int) bool { return b.assets[i].Qualified < b.assets[j].Qualified })
	return b.assets
}

// aliases collects the files' aliases (language.AliasDraft) by language and
// identity, targets sorted.
func (b *builder) aliases() map[aliasKey][]string {
	out := map[aliasKey][]string{}
	for _, fi := range b.resolverFiles {
		for _, a := range fi.Aliases {
			if a.Qualified != "" && a.Target != "" {
				k := aliasKey{fi.Language, a.Qualified}
				out[k] = append(out[k], a.Target)
			}
		}
	}
	for k, t := range out {
		sort.Strings(t)
		out[k] = t
	}
	return out
}

// edgeKey uniquely identifies a directed relationship including its semantic kind.
// Two edges sharing the same (From, To, Kind) are considered duplicates and merged.
type edgeKey struct {
	From symbol.SymbolID
	To   symbol.SymbolID
	Kind EdgeKind
}

// refKindRank orders the reference kinds of one merged edge: the edge reports
// the first (a symbol that both calls and names another is its caller).
func refKindRank(k reference.ReferenceKind) int {
	for i, r := range []reference.ReferenceKind{
		reference.KindCall, reference.KindConstruction, reference.KindImplements,
		reference.KindInheritance, reference.KindUsesTrait, reference.KindTypeUse,
		reference.KindValueReference, reference.KindIDReference, reference.KindExplicitDependency,
		reference.KindResourceLink, reference.KindImport,
	} {
		if k == r {
			return i
		}
	}
	return 100
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
			// Keep the reference kind that says the most (a call before a
			// type use), whatever order the references came in.
			if refKindRank(e.RefKind) < refKindRank(m.edge.RefKind) {
				m.edge.RefKind = e.RefKind
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
// never call edges; an identifier reference (markup) is a reference edge, a
// resource link a links edge. Every other kind (read/write/unknown or any
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
	case reference.KindValueReference, reference.KindIDReference:
		return EdgeReferences, true
	case reference.KindResourceLink:
		return EdgeLinks, true
	case reference.KindExplicitDependency:
		return EdgeDependsOn, true
	}
	return "", false
}
