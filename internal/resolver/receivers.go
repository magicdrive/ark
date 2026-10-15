package resolver

import (
	"fmt"
	"path/filepath"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// isTypeLike reports whether a symbol kind can own members.
func isTypeLike(k symbol.SymbolKind) bool {
	switch k {
	case symbol.KindClass, symbol.KindInterface, symbol.KindStruct, symbol.KindEnum, symbol.KindTrait:
		return true
	}
	return false
}

// valueWithMembers reports whether s is a value — no type-like declaration —
// that lexically contains members (an object literal, a constructor function
// with prototype methods). Code may reassign a value's members, so a member
// found under it is at most Strong.
func (r *Resolver) valueWithMembers(s symbol.Symbol) bool {
	return !ownsMembers(s.Kind) && r.owners[string(s.Location.File)+"\x00"+s.Qualified]
}

// ownsMembers reports whether a symbol of kind k can be the type a receiver
// is declared as, for member lookup: a type-like kind, or a plain type
// declaration (KindType). The latter owns members only where a language
// attaches them by receiver name — a Go defined type of any underlying type
// (type Level int, type graph map[string]nodeset) has methods — and
// membersOf finds no member of one that has none, so such a receiver
// resolves to nothing rather than to an outside type. isTypeLike, which
// also decides identity and inheritance, is unchanged.
func ownsMembers(k symbol.SymbolKind) bool {
	return isTypeLike(k) || k == symbol.KindType
}

// placementFits reports whether the receiver of ref can reach the member m
// (symbol.Symbol.Placement) of a type a member lookup found for it: a static
// member through the type's name, an instance member through a value of the
// type. A receiver whose type the provider proved (ReceiverType: this, a
// typed or constructed variable) is such a value; the receivers the member
// lookups take without one are the type's name itself (an import binding of
// the type, the type's own name). A member whose placement is not stated
// fits any receiver.
func placementFits(ref reference.Reference, m symbol.Symbol) bool {
	switch m.Placement {
	case symbol.PlacementStatic:
		return ref.ReceiverType == ""
	case symbol.PlacementInstance:
		return ref.ReceiverType != ""
	}
	return true
}

// isMember reports whether a symbol is reached through a receiver.
func isMember(s symbol.Symbol) bool { return s.Receiver != "" }

// withoutMembers drops receiver-attached members: a receiverless name cannot
// denote one.
func withoutMembers(cands []symbol.Symbol) []symbol.Symbol {
	var out []symbol.Symbol
	for _, s := range cands {
		if !isMember(s) {
			out = append(out, s)
		}
	}
	return out
}

// valueOnlyKind reports whether a reference kind needs a runtime value, so a
// type-only binding cannot satisfy it.
func valueOnlyKind(k reference.ReferenceKind) bool {
	return k == reference.KindCall || k == reference.KindConstruction
}

// isModuleReceiver reports whether expr is a legacy ImportDraft module alias
// (e.g. Go `fmt` in fmt.Println). Module-scoped files do not use legacy
// implicit aliases; their module receivers are explicit bindings (R1).
func (r *Resolver) isModuleReceiver(expr string, fi FileIndex) bool {
	if fi.ModuleScoped {
		return false
	}
	for _, imp := range fi.Imports {
		alias := imp.Alias
		if alias == "" {
			alias = filepath.Base(imp.Path)
		}
		if alias == "." || alias == "_" {
			continue
		}
		if alias == expr {
			return true
		}
	}
	return false
}

// capConfidence lowers a resolution (and its candidates) to at most max and
// records why.
func capConfidence(res Resolution, max Confidence, ev ResolutionEvidence) Resolution {
	if res.Confidence <= max {
		return res
	}
	res.Confidence = max
	res.Evidence = append(append([]ResolutionEvidence(nil), res.Evidence...), ev)
	cands := make([]Candidate, len(res.Candidates))
	for i, c := range res.Candidates {
		if c.Confidence > max {
			c.Confidence = max
		}
		c.Evidence = append(append([]ResolutionEvidence(nil), c.Evidence...), ev)
		cands[i] = c
	}
	res.Candidates = cands
	return res
}

// capUntypedReceiver implements R4: a variable/expression receiver without
// type evidence can never yield Exact/Strong (and therefore no graph edge).
// Candidates are kept as evidence.
func capUntypedReceiver(res Resolution, ref reference.Reference) Resolution {
	return capConfidence(res, ConfidenceCandidate, ResolutionEvidence{
		Kind:   EvidenceUntypedReceiver,
		Detail: fmt.Sprintf("receiver %q has no type evidence", ref.ReceiverExpr),
	})
}

// capModuleScope caps proximity / uniqueness matches in a module-scoped file:
// there, a name without a local declaration or binding is not proven.
func capModuleScope(res Resolution, fi FileIndex) Resolution {
	if !fi.ModuleScoped {
		return res
	}
	return capConfidence(res, ConfidenceCandidate, ResolutionEvidence{
		Kind:   EvidenceModuleScope,
		Detail: fmt.Sprintf("%s is module-scoped and the name is neither declared nor bound there", fi.FileID),
	})
}

// membersOf returns the members named name of type t. Lexically contained
// members (ParentQualified == t.Qualified in t's file) are structural proof.
// Languages without lexical containment attach members by receiver type name
// within the declaring type's directory and package (e.g. Go methods — an
// external test package beside it, package foo_test, declares its own
// types); that association is weaker, so it is reported via contained ==
// false.
func (r *Resolver) membersOf(t symbol.Symbol, name string) (members []symbol.Symbol, contained bool) {
	var attached []symbol.Symbol
	dir := filepath.Dir(string(t.Location.File))
	pkg := r.packageOf(t.Location.File)
	for _, s := range r.byName[nameKey{r.spaceOf[t.Location.File], name}] {
		switch {
		case s.ParentQualified != "":
			if s.ParentQualified == t.Qualified && s.Location.File == t.Location.File {
				members = append(members, s)
			}
		case s.Receiver != "" && s.Receiver == t.Name && filepath.Dir(string(s.Location.File)) == dir && r.packageOf(s.Location.File) == pkg:
			attached = append(attached, s)
		}
	}
	if len(members) > 0 {
		sortSymbols(members)
		return members, true
	}
	sortSymbols(attached)
	return attached, false
}

// packageOf is the Package a file states ("" when none).
func (r *Resolver) packageOf(file source.FileID) string {
	if fi, ok := r.byFile[file]; ok {
		return fi.Package
	}
	return ""
}

// memberResolution builds the resolution for members found under resolved
// type evidence. typeConf is the confidence of the type evidence itself.
func (r *Resolver) memberResolution(res Resolution, ref reference.Reference, types []symbol.Symbol, typeConf Confidence, ek EvidenceKind, detail string) Resolution {
	if ref.Speculative {
		// A selector that may name a field resolves only to members known
		// to be the type's own: not those attached by receiver name to a
		// type its package declares more than once (typeNamesakes).
		for _, t := range types {
			if r.typeNamesakes(t) {
				res.Evidence = []ResolutionEvidence{{Kind: ek, Detail: detail + fmt.Sprintf("; %s is declared more than once in its package", t.Name)}}
				return res
			}
		}
	}
	var members []symbol.Symbol
	seen := make(map[symbol.SymbolID]bool)
	allContained := true
	for _, t := range types {
		ms, contained := r.membersOf(t, ref.Name)
		if len(ms) > 0 && !contained {
			allContained = false
		}
		for _, m := range ms {
			if !placementFits(ref, m) {
				continue
			}
			if !seen[m.ID] {
				seen[m.ID] = true
				members = append(members, m)
			}
		}
	}
	if len(members) == 0 {
		// A member of the type's embedded types (typefacts.go), when the
		// type is identified.
		if len(types) == 1 {
			if out, ok := r.promotedResolution(res, ref, types[0], typeConf, detail); ok {
				return out
			}
		}
		// A member a supertype declares (inheritance.go), when every
		// supertype on the way is identified.
		if !ref.Speculative {
			shared := false
			for _, t := range types {
				if fi, ok := r.byFile[t.Location.File]; ok && fi.MembersShareNames {
					shared = true
				}
			}
			if out, ok := r.inheritedResolution(res, ref, types, typeConf, ek, shared, detail); ok {
				return out
			}
		}
		res.Evidence = []ResolutionEvidence{{Kind: ek, Detail: detail + fmt.Sprintf("; no member %q", ref.Name)}}
		return res
	}
	conf := typeConf
	if !allContained && conf > ConfidenceStrong {
		conf = ConfidenceStrong
	}
	if len(types) > 1 && conf > ConfidenceCandidate {
		conf = ConfidenceCandidate
	}
	return r.pickBest(res, members, conf, ek, detail)
}

// resolveTypeName resolves a type name in fi's scope (as a type_use reference
// at ref's position) to type-like symbols. It returns the confidence of that
// evidence; several type-like candidates yield ConfidenceCandidate.
//
// inRepository is set when the name denotes no type but is the repository's
// all the same: it resolves to declarations that own no members (a value —
// a JavaScript constant holding a constructor), or through an import
// binding that stays within the repository. Such a receiver is
// unrepresented, never outside the repository.
func (r *Resolver) resolveTypeName(name string, ref reference.Reference, fi FileIndex) (types []symbol.Symbol, conf Confidence, inRepository bool) {
	probe := reference.Reference{
		Name:      name,
		Kind:      reference.KindTypeUse,
		Language:  ref.Language,
		Location:  ref.Location,
		Container: ref.Container,
	}
	var sub Resolution
	if fi.PackageScoped {
		sub = r.resolvePackageName(Resolution{Confidence: ConfidenceUnresolved}, probe, fi)
	} else {
		sub = r.resolveByName(Resolution{Confidence: ConfidenceUnresolved}, probe, fi)
	}
	values := false
	for _, c := range sub.Candidates {
		if s, ok := r.byID[c.SymbolID]; ok && (ownsMembers(s.Kind) || r.valueWithMembers(s)) {
			types = append(types, s)
			values = values || r.valueWithMembers(s)
		}
	}
	if len(types) == 0 {
		bound := !fi.PackageScoped && hasBinding(&fi, name)
		return nil, ConfidenceUnresolved, len(sub.Candidates) > 0 || (bound && !sub.OutsideRepository)
	}
	conf = sub.Confidence
	if values && conf > ConfidenceStrong {
		conf = ConfidenceStrong // a value's members may be reassigned
	}
	if len(types) > 1 {
		conf = ConfidenceCandidate
	}
	return types, conf, true
}

// resolveViaReceiverType implements R2. It is authoritative: unresolvable
// type evidence yields Unresolved rather than a name heuristic.
func (r *Resolver) resolveViaReceiverType(res Resolution, ref reference.Reference, fi FileIndex) Resolution {
	types, conf, inRepository := r.resolveTypeName(ref.ReceiverType, ref, fi)
	detail := fmt.Sprintf("receiver %q declared as %q", ref.ReceiverExpr, ref.ReceiverType)
	if len(types) == 0 {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceReceiverType, Detail: detail + "; type not resolved in repository"}}
		res.OutsideRepository = !inRepository
		return res
	}
	return r.memberResolution(res, ref, types, conf, EvidenceReceiverType, detail)
}

// mutableDetail is the evidence a resolution through a Mutable export (a
// CommonJS exports object property) carries: the source mapping is known,
// the value an importer reads is not proven (Candidate at most).
const mutableDetail = "; through a CommonJS export: the value is not proven to be the exported declaration"

// resolveViaReceiverBinding implements R1: the receiver is an import binding
// (a namespace binding, or a named binding of a type).
func (r *Resolver) resolveViaReceiverBinding(res Resolution, ref reference.Reference, fi FileIndex) Resolution {
	br := r.newWalk().bindingTargets(&fi, ref.ReceiverExpr, 0)
	detail := fmt.Sprintf("receiver %q is a module binding (%s)", ref.ReceiverExpr, br.chainDetail())
	if br.typeOnly && valueOnlyKind(ref.Kind) {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceModuleBinding, Detail: detail + "; type-only binding used as a value"}}
		return res
	}
	conf := ConfidenceExact
	if br.ambiguous || len(br.targets) > 1 {
		conf = ConfidenceCandidate
	}
	if br.mutable {
		conf, detail = ConfidenceCandidate, detail+mutableDetail
	}
	var members []symbol.Symbol
	var types, owners []symbol.Symbol
	nsHits := bindResult{complete: true}
	values := false // a repository declaration that is no type: its members are not symbols
	for _, t := range br.targets {
		if t.module != "" {
			// Namespace receiver: look the name up in the module's exports.
			nsHits.merge(r.newWalk().export(t.module, ref.Name, 1))
			continue
		}
		switch {
		case isTypeLike(t.sym.Kind):
			types = append(types, t.sym)
		case r.valueWithMembers(t.sym) && t.sym.Kind != symbol.KindFunction:
			owners = append(owners, t.sym)
		default:
			// A constructor function's stated members are its prototype's —
			// an instance's, not the function's own properties (F.m = …,
			// not modelled): the function itself has none of them.
			values = true
		}
	}
	if len(types) > 0 {
		return r.memberResolution(res, ref, types, conf, EvidenceModuleBinding, detail)
	}
	if len(owners) > 0 {
		// A value with members (an object literal, a constructor with
		// prototype methods): its members may be reassigned — Strong at most.
		if conf > ConfidenceStrong {
			conf = ConfidenceStrong
		}
		return r.memberResolution(res, ref, owners, conf, EvidenceModuleBinding, detail+"; a member of a value")
	}
	members = nsHits.symbolTargets()
	if nsHits.typeOnly && valueOnlyKind(ref.Kind) {
		members = nil
	}
	if len(members) == 0 {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceModuleBinding, Detail: detail + fmt.Sprintf("; %q not resolved", ref.Name)}}
		// Outside only when the receiver's module lies outside: a receiver
		// that is a repository value (an object literal: its members are no
		// symbols) or a repository module without the export is the
		// repository's, unrepresented.
		res.OutsideRepository = !values && len(nsHits.targets) == 0 && (br.outside() || nsHits.outside()) && !br.dangling && !nsHits.dangling
		return res
	}
	if nsHits.ambiguous || len(nsHits.targets) > 1 {
		conf = ConfidenceCandidate
	}
	if nsHits.mutable && !br.mutable {
		conf, detail = ConfidenceCandidate, detail+mutableDetail
	}
	return r.pickBest(res, members, conf, EvidenceModuleBinding, detail+"; "+nsHits.chainDetail())
}

// resolveViaBinding resolves a receiverless name bound by an import binding.
// It is authoritative for that name: an external/unresolved module or a
// type-only binding used as a value yields Unresolved, never a same-name
// repository symbol found by other stages.
func (r *Resolver) resolveViaBinding(res Resolution, ref reference.Reference, fi FileIndex) Resolution {
	br := r.newWalk().bindingTargets(&fi, ref.Name, 0)
	detail := fmt.Sprintf("%q bound by import (%s)", ref.Name, br.chainDetail())
	if br.typeOnly && valueOnlyKind(ref.Kind) {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceModuleBinding, Detail: detail + "; type-only binding used as a value"}}
		return res
	}
	syms := br.symbolTargets()
	if len(syms) == 0 {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceModuleBinding, Detail: detail + "; no repository target"}}
		// Outside only when the binding leads out of the repository; a
		// module of the repository that does not export the name as Ark
		// sees it (a CommonJS module, a namespace used as a value) is not.
		res.OutsideRepository = br.outside() && len(br.targets) == 0
		return res
	}
	conf := ConfidenceExact
	if br.ambiguous || len(br.targets) > 1 {
		conf = ConfidenceCandidate
	}
	if br.mutable {
		conf, detail = ConfidenceCandidate, detail+mutableDetail
	}
	return r.pickBest(res, syms, conf, EvidenceModuleBinding, detail)
}
