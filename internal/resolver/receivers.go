package resolver

import (
	"fmt"
	"path/filepath"

	"github.com/magicdrive/ark/internal/reference"
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
// within the declaring type's directory (e.g. Go methods); that association
// is weaker, so it is reported via contained == false.
func (r *Resolver) membersOf(t symbol.Symbol, name string) (members []symbol.Symbol, contained bool) {
	var attached []symbol.Symbol
	dir := filepath.Dir(string(t.Location.File))
	for _, s := range r.byName[name] {
		switch {
		case s.ParentQualified != "":
			if s.ParentQualified == t.Qualified && s.Location.File == t.Location.File {
				members = append(members, s)
			}
		case s.Receiver != "" && s.Receiver == t.Name && filepath.Dir(string(s.Location.File)) == dir:
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

// memberResolution builds the resolution for members found under resolved
// type evidence. typeConf is the confidence of the type evidence itself.
func (r *Resolver) memberResolution(res Resolution, ref reference.Reference, types []symbol.Symbol, typeConf Confidence, ek EvidenceKind, detail string) Resolution {
	var members []symbol.Symbol
	seen := make(map[symbol.SymbolID]bool)
	allContained := true
	for _, t := range types {
		ms, contained := r.membersOf(t, ref.Name)
		if len(ms) > 0 && !contained {
			allContained = false
		}
		for _, m := range ms {
			if !seen[m.ID] {
				seen[m.ID] = true
				members = append(members, m)
			}
		}
	}
	if len(members) == 0 {
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
func (r *Resolver) resolveTypeName(name string, ref reference.Reference, fi FileIndex) ([]symbol.Symbol, Confidence) {
	probe := reference.Reference{
		Name:      name,
		Kind:      reference.KindTypeUse,
		Language:  ref.Language,
		Location:  ref.Location,
		Container: ref.Container,
	}
	sub := r.resolveByName(Resolution{Confidence: ConfidenceUnresolved}, probe, fi)
	var types []symbol.Symbol
	for _, c := range sub.Candidates {
		if s, ok := r.byID[c.SymbolID]; ok && isTypeLike(s.Kind) {
			types = append(types, s)
		}
	}
	if len(types) == 0 {
		return nil, ConfidenceUnresolved
	}
	conf := sub.Confidence
	if len(types) > 1 {
		conf = ConfidenceCandidate
	}
	return types, conf
}

// resolveViaReceiverType implements R2. It is authoritative: unresolvable
// type evidence yields Unresolved rather than a name heuristic.
func (r *Resolver) resolveViaReceiverType(res Resolution, ref reference.Reference, fi FileIndex) Resolution {
	types, conf := r.resolveTypeName(ref.ReceiverType, ref, fi)
	detail := fmt.Sprintf("receiver %q declared as %q", ref.ReceiverExpr, ref.ReceiverType)
	if len(types) == 0 {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceReceiverType, Detail: detail + "; type not resolved in repository"}}
		res.OutsideRepository = true
		return res
	}
	return r.memberResolution(res, ref, types, conf, EvidenceReceiverType, detail)
}

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
	var members []symbol.Symbol
	var types []symbol.Symbol
	nsHits := bindResult{complete: true}
	for _, t := range br.targets {
		if t.module != "" {
			// Namespace receiver: look the name up in the module's exports.
			nsHits.merge(r.newWalk().export(t.module, ref.Name, 1))
			continue
		}
		if isTypeLike(t.sym.Kind) {
			types = append(types, t.sym)
		}
	}
	if len(types) > 0 {
		return r.memberResolution(res, ref, types, conf, EvidenceModuleBinding, detail)
	}
	members = nsHits.symbolTargets()
	if nsHits.typeOnly && valueOnlyKind(ref.Kind) {
		members = nil
	}
	if len(members) == 0 {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceModuleBinding, Detail: detail + fmt.Sprintf("; %q not resolved", ref.Name)}}
		res.OutsideRepository = true
		return res
	}
	if nsHits.ambiguous || len(nsHits.targets) > 1 {
		conf = ConfidenceCandidate
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
		res.OutsideRepository = true
		return res
	}
	conf := ConfidenceExact
	if br.ambiguous || len(br.targets) > 1 {
		conf = ConfidenceCandidate
	}
	return r.pickBest(res, syms, conf, EvidenceModuleBinding, detail)
}
