package resolver

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// FileIndex holds the extraction result for one file.
type FileIndex struct {
	FileID     source.FileID
	Language   string
	Symbols    []symbol.Symbol
	References []reference.Reference
	Imports    []language.ImportDraft

	// Module-binding evidence (see language.Extraction).
	Bindings     []language.BindingDraft
	Exports      []language.ExportDraft
	ModuleScoped bool
	// IdentityOnly: the file's symbols are reached only by qualified
	// identity (see language.Extraction.IdentityOnly).
	IdentityOnly bool
	// Package / PackageScoped: package scoping (see language.Extraction).
	Package       string
	PackageScoped bool
}

// Resolver resolves syntactic references to candidate symbols using
// static evidence without a compiler.
type Resolver struct {
	files []FileIndex

	// byName maps bare symbol name → symbols across all files.
	byName map[string][]symbol.Symbol
	// byQualified maps qualified name → symbols.
	byQualified map[string][]symbol.Symbol
	// byFile maps FileID → FileIndex.
	byFile map[source.FileID]*FileIndex
	// byID maps SymbolID → symbol.
	byID map[symbol.SymbolID]symbol.Symbol

	// exportMemo caches complete module export lookups (see modules.go).
	memoMu     sync.Mutex
	exportMemo map[string]bindResult

	// qualified indexes type declarations by (language, Symbol.Qualified) for
	// authoritative qualified-identity lookup (see identity.go). It is built
	// once, on first use, so repositories whose providers never emit qualified
	// identities pay nothing for it.
	qualOnce  sync.Once
	qualified map[langQualified][]symbol.Symbol

	// Lookup indexes for the name-based stages (see lookup.go). They return
	// exactly the symbols the corresponding full scans would, in a fixed order,
	// so they change the cost of a stage and never its outcome.
	// structural indexes type declarations' qualified relations for member
	// lookup through supertypes and traits (see inheritance.go).
	structural structuralIndex

	// packages: package scoping (packagescope.go), built on first use;
	// rootName is the repository root directory's name ("" if unknown).
	packages packageScope
	rootName string

	members    map[string][]*symbol.Symbol // Name → symbols with Qualified and Receiver
	suffixes   map[string][]*symbol.Symbol // text after any "." in Qualified → symbols
	dirSymbols map[string]map[string][]dirSymbol
}

// New builds a Resolver from a set of file indexes.
func New(files []FileIndex) *Resolver { return NewInRoot(files, "") }

// NewInRoot builds a Resolver for the files of a repository whose root
// directory is named rootName (package scoping uses it as evidence for a
// repository's import-path prefix; see packagescope.go).
func NewInRoot(files []FileIndex, rootName string) *Resolver {
	r := &Resolver{
		files:       files,
		byName:      make(map[string][]symbol.Symbol),
		byQualified: make(map[string][]symbol.Symbol),
		byFile:      make(map[source.FileID]*FileIndex),
		byID:        make(map[symbol.SymbolID]symbol.Symbol),
		exportMemo:  make(map[string]bindResult),
		rootName:    rootName,
	}
	for i := range files {
		fi := &files[i]
		r.byFile[fi.FileID] = fi
		for _, sym := range fi.Symbols {
			r.byID[sym.ID] = sym
			if fi.IdentityOnly {
				// Reached only through the qualified-identity index
				// (identity.go), never through a name-based stage.
				continue
			}
			r.byName[sym.Name] = append(r.byName[sym.Name], sym)
			if sym.Qualified != "" {
				r.byQualified[sym.Qualified] = append(r.byQualified[sym.Qualified], sym)
			}
		}
	}
	r.buildLookupIndexes()
	return r
}

// Resolve resolves all references in all files. Output order is deterministic.
func (r *Resolver) Resolve() []Resolution {
	var results []Resolution
	// Process files in deterministic order.
	fileIDs := make([]string, 0, len(r.files))
	for _, fi := range r.files {
		fileIDs = append(fileIDs, string(fi.FileID))
	}
	sort.Strings(fileIDs)

	for _, fid := range fileIDs {
		fi := r.byFile[source.FileID(fid)]
		for _, ref := range fi.References {
			results = append(results, r.ResolveReference(ref, *fi))
		}
	}
	return results
}

// ResolveReference resolves a single reference within its file context.
//
// A Dynamic reference (its name is computed at run time) is Unresolved before
// any rule is consulted: its Name is display text, and matching it against
// declarations would be guessing. A reference carrying a provider-determined qualified identity is resolved by
// R0 (identity.go) and by nothing else. Otherwise a reference with a receiver is
// classified (R1–R4):
//
//	R1 binding receiver   – the receiver is a module import binding of the file
//	R2 declared type      – the provider proved the receiver's declared type
//	   module receiver    – legacy ImportDraft alias (e.g. Go `fmt.Println`)
//	R3 type-name receiver – the receiver names a repository type (e.g. `User::create`)
//	R4 untyped receiver   – anything else: no type evidence, capped at Candidate
//
// R1 and R2 are authoritative: when they cannot resolve, the reference is
// Unresolved — they never fall back to name heuristics.
func (r *Resolver) ResolveReference(ref reference.Reference, fi FileIndex) Resolution {
	return applyConfidenceCap(restrictTargetKinds(r.resolveReference(ref, fi), ref), ref)
}

// restrictTargetKinds removes the candidates of kinds the reference cannot
// denote (reference.Reference.TargetKinds). It only removes: none left is
// Unresolved, and narrowing several candidates to one never makes it unique —
// the survivor stays at most a Candidate, as the ambiguity was.
func restrictTargetKinds(res Resolution, ref reference.Reference) Resolution {
	if ref.TargetKinds == "" || len(res.Candidates) == 0 {
		return res
	}
	allowed := strings.Split(ref.TargetKinds, ",")
	var kept []Candidate
	for _, c := range res.Candidates {
		if slices.Contains(allowed, string(c.Kind)) {
			kept = append(kept, c)
		}
	}
	if len(kept) == len(res.Candidates) {
		return res
	}
	ev := ResolutionEvidence{Kind: EvidenceTargetKind, Detail: fmt.Sprintf("the reference can denote only %s", ref.TargetKinds)}
	if len(kept) == 0 {
		return Resolution{
			ReferenceID:   res.ReferenceID,
			ReferenceName: res.ReferenceName,
			Confidence:    ConfidenceUnresolved,
			Evidence:      append(append([]ResolutionEvidence(nil), res.Evidence...), ev),
		}
	}
	res.Candidates = kept
	return capConfidence(res, ConfidenceCandidate, ev)
}

// applyConfidenceCap lowers res to the provider's ConfidenceCap. It only ever
// lowers: a resolution already at or below the cap is returned unchanged, so a
// cap can never promote a Candidate or an Unresolved reference.
func applyConfidenceCap(res Resolution, ref reference.Reference) Resolution {
	if ref.ConfidenceCap == "" {
		return res
	}
	limit := ConfidenceCandidate // unknown caps fail safe
	if ref.ConfidenceCap == ConfidenceStrong.String() {
		limit = ConfidenceStrong
	}
	return capConfidence(res, limit, ResolutionEvidence{
		Kind:   EvidenceConfidenceCap,
		Detail: fmt.Sprintf("the reference's evidence permits at most %s", limit),
	})
}

func (r *Resolver) resolveReference(ref reference.Reference, fi FileIndex) Resolution {
	res := Resolution{
		ReferenceID:   ref.ID,
		ReferenceName: ref.Name,
		Confidence:    ConfidenceUnresolved,
	}
	if ref.Dynamic {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceDynamicName, Detail: "the name is computed at run time"}}
		return res
	}
	// R0: a provider-determined qualified identity is authoritative and is
	// resolved before — and instead of — every other rule below.
	if ref.NameQualified != "" || ref.ReceiverTypeQualified != "" {
		return r.resolveQualifiedIdentity(res, ref)
	}
	// In an identity-only file a name without identity evidence denotes
	// nothing a name match could find (FileIndex.IdentityOnly).
	if fi.IdentityOnly {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceIdentityOnly, Detail: fmt.Sprintf("%s resolves names only by qualified identity", fi.FileID)}}
		return res
	}
	// A package-scoped file resolves by its language's package scoping
	// (packagescope.go), never by repository-wide name similarity.
	if fi.PackageScoped {
		return r.resolvePackageScoped(res, ref, fi)
	}
	if ref.ReceiverExpr == "" {
		return r.resolveByName(res, ref, fi)
	}
	switch {
	case hasBinding(&fi, ref.ReceiverExpr):
		return r.resolveViaReceiverBinding(res, ref, fi)
	case ref.ReceiverType != "":
		return r.resolveViaReceiverType(res, ref, fi)
	case r.isModuleReceiver(ref.ReceiverExpr, fi), r.isTypeReceiver(ref.ReceiverExpr):
		return r.resolveByName(res, ref, fi)
	default:
		return capUntypedReceiver(r.resolveByName(res, ref, fi), ref)
	}
}

// resolveByName runs the name-based stages. For a module-scoped file, names
// without a local declaration or explicit binding are capped at Candidate and
// the legacy implicit ImportDraft alias match is not applied.
func (r *Resolver) resolveByName(res Resolution, ref reference.Reference, fi FileIndex) Resolution {
	// Explicit type receiver: when the receiver expression itself names a
	// repository type symbol (e.g. PHP `User::create()`, where `User` is a
	// class), the target MUST be a member of that type. We constrain every
	// name-based candidate stage so an explicit type receiver can never be
	// ignored in favour of a weaker bare-name match on an unrelated member.
	typeRecv := ref.ReceiverExpr != "" && r.isTypeReceiver(ref.ReceiverExpr)
	// A receiverless name never denotes a receiver-attached member: members are
	// reached through a receiver (R1–R4), never by their bare name.
	free := ref.ReceiverExpr == ""
	narrow := func(cands []symbol.Symbol) []symbol.Symbol {
		cands = r.constrainReceiver(cands, ref, typeRecv)
		if free {
			cands = withoutMembers(cands)
		}
		return cands
	}

	// Stage 1: same file + same container (lexical scope).
	if candidates := r.constrainReceiver(r.sameFileContainerMatch(ref, fi), ref, typeRecv); len(candidates) > 0 {
		return r.pickBest(res, candidates, ConfidenceExact, EvidenceSameLexicalScope,
			fmt.Sprintf("symbol %q defined in same container %q", ref.Name, ref.Container))
	}

	// Stage 2: same file, any symbol.
	if candidates := narrow(r.sameFileMatch(ref, fi)); len(candidates) > 0 {
		return r.pickBest(res, candidates, ConfidenceExact, EvidenceSameFile,
			fmt.Sprintf("symbol %q defined in same file %s", ref.Name, fi.FileID))
	}

	// Stage 3a: explicit module binding (authoritative for the bound name).
	if free && hasBinding(&fi, ref.Name) {
		return r.resolveViaBinding(res, ref, fi)
	}

	// Stage 3b: legacy explicit import match (implicit ImportDraft alias).
	if !fi.ModuleScoped {
		if candidates := narrow(r.importMatch(ref, fi)); len(candidates) > 0 {
			return r.pickBest(res, candidates, ConfidenceExact, EvidenceExplicitImport,
				fmt.Sprintf("symbol %q resolved via import", ref.Name))
		}
	}

	// Stage 4: qualified receiver match (e.g. Go: repo.Save → ReceiverType.Save).
	if ref.ReceiverExpr != "" {
		if candidates := narrow(r.receiverMatch(ref, fi)); len(candidates) > 0 {
			return r.pickBest(res, candidates, ConfidenceStrong, EvidenceQualifiedReceiver,
				fmt.Sprintf("receiver %q suggests qualified name", ref.ReceiverExpr))
		}
	}

	// Stage 5: same package / directory.
	if candidates := narrow(r.samePackageMatch(ref, fi)); len(candidates) > 0 {
		return capModuleScope(r.pickBest(res, candidates, ConfidenceStrong, EvidenceSamePackage,
			fmt.Sprintf("symbol %q found in same package as %s", ref.Name, fi.FileID)), fi)
	}

	// Stage 6/7: repository-wide search.
	all := r.byName[ref.Name]
	if len(all) == 0 {
		// Stage 6a: try unqualified tail of qualified names.
		all = r.byNameSuffix(ref.Name)
	}
	// An explicit type receiver must not fall back to an incompatible member.
	all = narrow(all)

	switch len(all) {
	case 0:
		// ConfidenceUnresolved — no candidate at all.
	case 1:
		c := symbolToCandidate(all[0], ConfidenceStrong, []ResolutionEvidence{
			{Kind: EvidenceUniqueName, Detail: fmt.Sprintf("only symbol named %q in repository", ref.Name)},
		})
		res.Candidates = []Candidate{c}
		res.Confidence = ConfidenceStrong
		res.Evidence = c.Evidence
	default:
		candidates := make([]Candidate, len(all))
		for i, sym := range all {
			candidates[i] = symbolToCandidate(sym, ConfidenceCandidate, []ResolutionEvidence{
				{Kind: EvidenceCandidateSet, Detail: fmt.Sprintf("one of %d symbols named %q", len(all), ref.Name)},
			})
		}
		sortCandidates(candidates)
		res.Candidates = candidates
		res.Confidence = ConfidenceCandidate
		res.Evidence = []ResolutionEvidence{
			{Kind: EvidenceCandidateSet, Detail: fmt.Sprintf("%d candidates for %q", len(all), ref.Name)},
		}
	}
	return capModuleScope(res, fi)
}

// isTypeReceiver reports whether expr names a repository type-like symbol
// (class/interface/struct/enum/trait). This is a language-neutral check over
// existing symbol identity — it does not interpret any language's syntax.
func (r *Resolver) isTypeReceiver(expr string) bool {
	for _, s := range r.byName[expr] {
		switch s.Kind {
		case symbol.KindClass, symbol.KindInterface, symbol.KindStruct, symbol.KindEnum, symbol.KindTrait:
			return true
		}
	}
	return false
}

// constrainReceiver narrows candidates to members of an explicit type receiver.
// When typeRecv is false it is a no-op (variable/expression receiver → existing
// behaviour). When true, only members whose declaring-type Receiver exactly
// matches the receiver expression survive; non-members (empty Receiver) and
// members of other types are dropped. This prevents an explicit type receiver
// from resolving to an unrelated same-name member.
func (r *Resolver) constrainReceiver(cands []symbol.Symbol, ref reference.Reference, typeRecv bool) []symbol.Symbol {
	if !typeRecv {
		return cands
	}
	var out []symbol.Symbol
	for _, s := range cands {
		if s.Receiver == ref.ReceiverExpr {
			out = append(out, s)
		}
	}
	return out
}

// sameFileContainerMatch finds symbols in the same file whose ParentQualified
// exactly matches ref.Container.  Top-level symbols (no parent) are never
// considered a lexical match for a non-empty container.
func (r *Resolver) sameFileContainerMatch(ref reference.Reference, fi FileIndex) []symbol.Symbol {
	if ref.Container == "" {
		return nil
	}
	var out []symbol.Symbol
	for _, sym := range fi.Symbols {
		if sym.Name == ref.Name && sym.ParentQualified == ref.Container {
			out = append(out, sym)
		}
	}
	return out
}

// sameFileMatch finds symbols by bare name in the same file.
func (r *Resolver) sameFileMatch(ref reference.Reference, fi FileIndex) []symbol.Symbol {
	var out []symbol.Symbol
	for _, sym := range fi.Symbols {
		if sym.Name == ref.Name {
			out = append(out, sym)
		}
	}
	return out
}

// importMatch resolves using explicit imports in the file.
// For Go: "fmt.Println" → import "fmt" present → find Println in any "fmt" file.
// For TS/JS: import { Save } from './repo' → find Save in repo files.
func (r *Resolver) importMatch(ref reference.Reference, fi FileIndex) []symbol.Symbol {
	// Build alias→importPath map for this file.
	aliasMap := make(map[string]string)
	for _, imp := range fi.Imports {
		alias := imp.Alias
		if alias == "" {
			alias = filepath.Base(imp.Path)
		}
		if alias == "." || alias == "_" {
			continue
		}
		aliasMap[alias] = imp.Path
	}

	// Check if ref.ReceiverExpr or the name prefix matches an alias.
	var importPath string
	// Resolve lookup name: strip package prefix from dotted names (e.g. "fmt.Println" → "Println").
	lookupName := ref.Name
	if parts := strings.SplitN(ref.Name, ".", 2); len(parts) == 2 {
		lookupName = parts[1]
		if ref.ReceiverExpr == "" {
			ref.ReceiverExpr = parts[0]
		}
	}

	if ref.ReceiverExpr != "" {
		if p, ok := aliasMap[ref.ReceiverExpr]; ok {
			importPath = p
		}
	}
	// Fallback: check bare name prefix against aliases.
	if importPath == "" {
		if p, ok := aliasMap[strings.SplitN(ref.Name, ".", 2)[0]]; ok {
			importPath = p
		}
	}
	if importPath == "" {
		return nil
	}

	// Find symbols in files whose FileID matches the import path.
	// importPath may be a full module path (e.g. "github.com/foo/bar/pkg") while
	// FileIDs are root-relative (e.g. "pkg/file.go"). We try both the full path
	// and the base component so both absolute and relative FileIDs are handled.
	importBase := filepath.Base(importPath)
	var out []symbol.Symbol
	for _, f := range r.files {
		if f.IdentityOnly {
			continue
		}
		fid := string(f.FileID)
		matches := strings.Contains(fid, importPath) ||
			strings.HasPrefix(fid, importBase+"/") ||
			strings.HasPrefix(fid, importBase+string(filepath.Separator))
		if !matches {
			continue
		}
		for _, sym := range f.Symbols {
			if sym.Name == lookupName {
				out = append(out, sym)
			}
		}
	}
	return out
}

// receiverMatch looks for a qualified symbol whose type prefix matches ReceiverExpr.
// e.g. ref.ReceiverExpr="repo", ref.Name="Save" → find "UserRepository.Save" if
// there is a local var/param named "repo" of type UserRepository.
// As a heuristic without a type system, we search for symbols named "ReceiverType.Name"
// where ReceiverType is any type containing the receiver expression as a suffix.
func (r *Resolver) receiverMatch(ref reference.Reference, _ FileIndex) []symbol.Symbol {
	var out []symbol.Symbol
	// r.members holds exactly the qualified, receiver-attached symbols per name.
	for _, s := range r.members[ref.Name] {
		// Accept if receiver type name contains the receiver expr (case-insensitive heuristic).
		if strings.EqualFold(s.Receiver, ref.ReceiverExpr) ||
			strings.HasSuffix(strings.ToLower(s.Receiver), strings.ToLower(ref.ReceiverExpr)) {
			out = append(out, *s)
		}
	}
	return out
}

// samePackageMatch finds symbols by name in files sharing the same directory.
func (r *Resolver) samePackageMatch(ref reference.Reference, fi FileIndex) []symbol.Symbol {
	var out []symbol.Symbol
	for _, ds := range r.dirSymbols[filepath.Dir(string(fi.FileID))][ref.Name] {
		if ds.file != fi.FileID {
			out = append(out, *ds.sym)
		}
	}
	return out
}

// byNameSuffix finds symbols where ref.Name matches the suffix of a qualified name.
func (r *Resolver) byNameSuffix(name string) []symbol.Symbol {
	var out []symbol.Symbol
	for _, s := range r.suffixes[name] {
		out = append(out, *s)
	}
	return out
}

func (r *Resolver) pickBest(res Resolution, syms []symbol.Symbol, conf Confidence, ek EvidenceKind, detail string) Resolution {
	ev := []ResolutionEvidence{{Kind: ek, Detail: detail}}
	// Multiple viable candidates must be downgraded to Candidate — never Exact or Strong.
	effectiveConf := conf
	if len(syms) > 1 {
		effectiveConf = ConfidenceCandidate
	}
	candidates := make([]Candidate, len(syms))
	for i, sym := range syms {
		candidates[i] = symbolToCandidate(sym, effectiveConf, ev)
	}
	sortCandidates(candidates)
	res.Candidates = candidates
	res.Confidence = effectiveConf
	res.Evidence = ev
	return res
}

func symbolToCandidate(sym symbol.Symbol, conf Confidence, ev []ResolutionEvidence) Candidate {
	return Candidate{
		SymbolID:   sym.ID,
		Name:       sym.Name,
		Qualified:  sym.Qualified,
		File:       sym.Location.File,
		Kind:       sym.Kind,
		Confidence: conf,
		Evidence:   ev,
	}
}

// sortCandidates ensures deterministic output order.
func sortCandidates(candidates []Candidate) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].File != candidates[j].File {
			return candidates[i].File < candidates[j].File
		}
		return candidates[i].Qualified < candidates[j].Qualified
	})
}
