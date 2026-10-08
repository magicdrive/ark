package resolver

import (
	"fmt"
	"strings"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// Qualified identity (R0).
//
// A provider that knows from the language's own lexical rules which declaration
// a written name denotes passes that identity as NameQualified (for the name
// itself) or ReceiverTypeQualified (for the type of the receiver). The resolver
// compares it, exactly, with the Symbol.Qualified of the repository's type
// declarations of the same language:
//
//	one declaration   → Exact       (the program text names it)
//	no declaration    → Unresolved  (an external symbol, or one that does not exist)
//	several           → Candidate   (ambiguity is evidence; none is chosen)
//
// This is authoritative evidence. In particular a miss does NOT fall back to
// same-file / import / receiver-suffix / same-package / unique-name matching:
// those rest on repository-local similarity, and similarity must never override
// an identity the language fixed explicitly — a `use Vendor\Request` is not the
// repository's own `App\Models\Request`.
//
// Receiver identity and member lookup stay separate. ReceiverTypeQualified only
// identifies the receiver's type; the member is then looked up exactly as under
// any other proven receiver type (memberResolution). A member that the type does
// not declare itself is looked up structurally through its traits and
// supertypes (inheritance.go); when that finds no target the reference is
// Unresolved. Dispatch through magic methods (__call, __get, ...) is not
// modelled.

// langQualified is the key of the qualified-identity index. Symbol.Qualified has
// a different meaning in each language, so identities are only comparable within
// one.
type langQualified struct {
	language  string
	qualified string
}

// typeDecls returns the repository's declarations named by (language,
// qualified), in deterministic order. Of a file whose names are scoped by
// qualified identity alone (FileIndex.IdentityOnly) every declaration is
// indexed: identity is the only way to reach it. Of every other file only
// type-like symbols are: this lookup answers "which type is this identity?"
// and never matches a member, function, constant or namespace that merely
// shares the string.
func (r *Resolver) typeDecls(language, qualified string) []symbol.Symbol {
	r.qualOnce.Do(func() {
		r.qualified = make(map[langQualified][]symbol.Symbol)
		for _, fi := range r.files {
			for _, s := range fi.Symbols {
				if s.Qualified == "" || (!fi.IdentityOnly && !isTypeLike(s.Kind)) {
					continue
				}
				k := langQualified{s.Language, s.Qualified}
				r.qualified[k] = append(r.qualified[k], s)
			}
		}
		for _, syms := range r.qualified {
			sortSymbols(syms)
		}
	})
	return r.qualified[langQualified{language, qualified}]
}

// resolveQualifiedIdentity implements R0. It never falls back to name
// heuristics, whatever the outcome.
func (r *Resolver) resolveQualifiedIdentity(res Resolution, ref reference.Reference) Resolution {
	// The receiver's type identity, when present, qualifies the receiver; the
	// reference's own Name is then a member of that type.
	if ref.ReceiverTypeQualified != "" {
		types := r.typeDecls(ref.Language, ref.ReceiverTypeQualified)
		detail := fmt.Sprintf("receiver type %q", ref.ReceiverTypeQualified)
		if len(types) == 0 {
			return noQualifiedDeclaration(res, ref, detail)
		}
		// The receiver declaration itself states where its members live.
		if scoped, ok := r.memberScopeResolution(res, ref, types); ok {
			return scoped
		}
		// Several declarations share the identity, but the reference is
		// written inside one of them: that one is the receiver's type (e.g.
		// a class referring to itself through $this or self).
		if len(types) > 1 {
			if enclosing := enclosingDeclaration(types, ref); enclosing != nil {
				types = []symbol.Symbol{*enclosing}
			}
		}
		conf := ConfidenceExact
		if len(types) > 1 {
			conf = ConfidenceCandidate
		}
		own := r.memberResolution(res, ref, types, conf, EvidenceQualifiedIdentity, detail)
		if len(own.Candidates) > 0 {
			return own
		}
		// Not declared by the type itself: the nearest structural declaration
		// (traits, supertypes) — or nothing (inheritance.go).
		if inherited, ok := r.inheritedResolution(res, ref, types, conf, detail); ok {
			return inherited
		}
		return own
	}

	types := r.typeDecls(ref.Language, ref.NameQualified)
	detail := fmt.Sprintf("%q", ref.NameQualified)
	if len(types) == 0 {
		return noQualifiedDeclaration(res, ref, detail)
	}
	where := fmt.Sprintf("declared at %s", types[0].Location.File)
	if len(types) > 1 {
		where = fmt.Sprintf("declared %d times", len(types))
	}
	// pickBest downgrades several declarations to Candidate.
	return r.pickBest(res, types, ConfidenceExact, EvidenceQualifiedIdentity, detail+" is "+where)
}

// noQualifiedDeclaration is the R0 answer when no declaration carries the
// identity: outside the repository — unless the identity names a scope of
// the repository itself (IdentityInRepository), where it is undeclared or
// declared in syntax no provider extracts: Unresolved, and not external.
func noQualifiedDeclaration(res Resolution, ref reference.Reference, detail string) Resolution {
	res.Evidence = []ResolutionEvidence{{
		Kind:   EvidenceQualifiedIdentity,
		Detail: detail + " is not declared in the repository",
	}}
	res.OutsideRepository = !ref.IdentityInRepository
	return res
}

// memberScopeResolution resolves a member through a receiver declaration that
// states its members' home (Symbol.MemberScope / MembersOutside). It reports
// false when no receiver declaration carries such a statement; the member is
// then looked up as under any other receiver type. Several receiver
// declarations never pick one: the members their statements name are
// Candidates.
func (r *Resolver) memberScopeResolution(res Resolution, ref reference.Reference, types []symbol.Symbol) (Resolution, bool) {
	stated := false
	for _, t := range types {
		stated = stated || t.MemberScope != "" || t.MembersOutside
	}
	if !stated {
		return res, false
	}
	if len(types) == 1 && types[0].MembersOutside {
		res.Evidence = []ResolutionEvidence{{
			Kind:   EvidenceMemberScope,
			Detail: fmt.Sprintf("the members of %q are declared outside the repository", types[0].Qualified),
		}}
		res.OutsideRepository = true
		return res, true
	}
	var decls []symbol.Symbol
	var target string
	for _, t := range types {
		if t.MemberScope == "" {
			continue
		}
		target = t.MemberScope + ref.Name
		decls = append(decls, r.typeDecls(ref.Language, target)...)
	}
	detail := fmt.Sprintf("member %q of %q is %q", ref.Name, ref.ReceiverTypeQualified, target)
	conf := ConfidenceExact
	if len(types) > 1 {
		detail = fmt.Sprintf("member %q of %q, declared %d times", ref.Name, ref.ReceiverTypeQualified, len(types))
		conf = ConfidenceCandidate
	}
	if len(decls) == 0 {
		res.Evidence = []ResolutionEvidence{{
			Kind:   EvidenceMemberScope,
			Detail: detail + "; no such declaration in the repository",
		}}
		return res, true
	}
	sortSymbols(decls)
	return r.pickBest(res, decls, conf, EvidenceMemberScope, detail), true
}

// enclosingDeclaration returns the one declaration among types that lexically
// contains ref — same file, and ref's container is the declaration or one of
// its members — or nil when none or several do.
func enclosingDeclaration(types []symbol.Symbol, ref reference.Reference) *symbol.Symbol {
	var found *symbol.Symbol
	for i := range types {
		t := &types[i]
		if t.Location.File != ref.Location.File {
			continue
		}
		if ref.Container != t.Qualified && !strings.HasPrefix(ref.Container, t.Qualified+".") {
			continue
		}
		if found != nil {
			return nil
		}
		found = t
	}
	return found
}
