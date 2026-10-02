package resolver

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

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
}

// New builds a Resolver from a set of file indexes.
func New(files []FileIndex) *Resolver {
	r := &Resolver{
		files:       files,
		byName:      make(map[string][]symbol.Symbol),
		byQualified: make(map[string][]symbol.Symbol),
		byFile:      make(map[source.FileID]*FileIndex),
	}
	for i := range files {
		fi := &files[i]
		r.byFile[fi.FileID] = fi
		for _, sym := range fi.Symbols {
			r.byName[sym.Name] = append(r.byName[sym.Name], sym)
			if sym.Qualified != "" {
				r.byQualified[sym.Qualified] = append(r.byQualified[sym.Qualified], sym)
			}
		}
	}
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
func (r *Resolver) ResolveReference(ref reference.Reference, fi FileIndex) Resolution {
	res := Resolution{
		ReferenceID:   ref.ID,
		ReferenceName: ref.Name,
		Confidence:    ConfidenceUnresolved,
	}

	// Stage 1: same file + same container (lexical scope).
	if candidates := r.sameFileContainerMatch(ref, fi); len(candidates) > 0 {
		return r.pickBest(res, candidates, ConfidenceExact, EvidenceSameLexicalScope,
			fmt.Sprintf("symbol %q defined in same container %q", ref.Name, ref.Container))
	}

	// Stage 2: same file, any symbol.
	if candidates := r.sameFileMatch(ref, fi); len(candidates) > 0 {
		return r.pickBest(res, candidates, ConfidenceExact, EvidenceSameFile,
			fmt.Sprintf("symbol %q defined in same file %s", ref.Name, fi.FileID))
	}

	// Stage 3: explicit import match.
	if candidates := r.importMatch(ref, fi); len(candidates) > 0 {
		return r.pickBest(res, candidates, ConfidenceExact, EvidenceExplicitImport,
			fmt.Sprintf("symbol %q resolved via import", ref.Name))
	}

	// Stage 4: qualified receiver match (e.g. Go: repo.Save → ReceiverType.Save).
	if ref.ReceiverExpr != "" {
		if candidates := r.receiverMatch(ref, fi); len(candidates) > 0 {
			return r.pickBest(res, candidates, ConfidenceStrong, EvidenceQualifiedReceiver,
				fmt.Sprintf("receiver %q suggests qualified name", ref.ReceiverExpr))
		}
	}

	// Stage 5: same package / directory.
	if candidates := r.samePackageMatch(ref, fi); len(candidates) > 0 {
		return r.pickBest(res, candidates, ConfidenceStrong, EvidenceSamePackage,
			fmt.Sprintf("symbol %q found in same package as %s", ref.Name, fi.FileID))
	}

	// Stage 6/7: repository-wide search.
	all := r.byName[ref.Name]
	if len(all) == 0 {
		// Stage 6a: try unqualified tail of qualified names.
		all = r.byNameSuffix(ref.Name)
	}
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
	return res
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
	target := ref.Name
	var out []symbol.Symbol
	for _, sym := range r.byQualified {
		for _, s := range sym {
			if s.Name == target && s.Receiver != "" {
				// Accept if receiver type name contains the receiver expr (case-insensitive heuristic).
				if strings.EqualFold(s.Receiver, ref.ReceiverExpr) ||
					strings.HasSuffix(strings.ToLower(s.Receiver), strings.ToLower(ref.ReceiverExpr)) {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// samePackageMatch finds symbols by name in files sharing the same directory.
func (r *Resolver) samePackageMatch(ref reference.Reference, fi FileIndex) []symbol.Symbol {
	dir := filepath.Dir(string(fi.FileID))
	var out []symbol.Symbol
	for _, f := range r.files {
		if string(f.FileID) == string(fi.FileID) {
			continue
		}
		if filepath.Dir(string(f.FileID)) != dir {
			continue
		}
		for _, sym := range f.Symbols {
			if sym.Name == ref.Name {
				out = append(out, sym)
			}
		}
	}
	return out
}

// byNameSuffix finds symbols where ref.Name matches the suffix of a qualified name.
func (r *Resolver) byNameSuffix(name string) []symbol.Symbol {
	var out []symbol.Symbol
	for q, syms := range r.byQualified {
		if strings.HasSuffix(q, "."+name) {
			out = append(out, syms...)
		}
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
