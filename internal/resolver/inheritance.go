package resolver

import (
	"fmt"
	"sync"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// Structural member lookup (R0).
//
// A reference with a qualified receiver type names a member of that type, but
// the member may be declared by a supertype or a mixed-in trait rather than by
// the type itself. This lookup follows only the qualified relations providers
// record on the type declaration — inheritance (`extends`), implementation
// (`implements`) and trait use — each identified exactly like any qualified
// identity (typeDecls). It never matches names: a relation whose target is not
// declared in the repository, or is declared more than once, is a gap in the
// evidence, not a reason to look elsewhere.
//
// For member name n on type T, the nearest declaration wins, in the order
// member dispatch uses:
//
//  1. a member of T itself;
//  2. a member imported from T's traits (each trait searched the same way,
//     through the traits it uses). Several traits declaring n is a collision:
//     every declaration is returned (ambiguity), none is chosen;
//  3. a member found on T's parent(s), searched the same way;
//  4. a member declared by an interface T implements (an abstract
//     declaration), searched through the interfaces it extends.
//
// The lookup stops without a target — the reference stays Unresolved — when
// what declares n cannot be determined: a parent, trait or interface outside
// the repository or of ambiguous identity is on the path; n is named by a
// trait adaptation of T (`insteadof` / `as`, not modelled); the nearest
// declaration in a supertype is private (not inherited); or the relations form
// a cycle.
//
// Unknown structural participant must not disappear behind a known candidate.
// When a trait that takes part in the same dispatch cannot be identified (not
// declared in the repository, declared more than once, or not fully
// searchable) while an identified trait declares n, that declaration is only a
// Candidate: the unseen trait may declare n too (a collision, or an abstract
// declaration it implements), so no resolved edge is created and the
// reference stays unattributed, with the observed declaration kept as its
// candidate.

const maxStructuralDepth = 32

type relationKind int

const (
	relParent relationKind = iota
	relTrait
	relInterface
)

// typeRelation is one relation of a type declaration and its target
// declarations (none: outside the repository; several: ambiguous identity).
type typeRelation struct {
	kind    relationKind
	name    string
	targets []symbol.Symbol
}

// typeStructure is what the lookup needs to know about one type declaration.
type typeStructure struct {
	relations []typeRelation
	adapted   map[string]bool // member names named by trait adaptations
}

type structuralIndex struct {
	once  sync.Once
	types map[symbol.SymbolID]*typeStructure
}

// structureOf returns the recorded relations of type declaration t.
func (r *Resolver) structureOf(t symbol.Symbol) *typeStructure {
	r.structural.once.Do(r.buildStructuralIndex)
	return r.structural.types[t.ID]
}

func (r *Resolver) buildStructuralIndex() {
	r.structural.types = make(map[symbol.SymbolID]*typeStructure)
	for i := range r.files {
		fi := &r.files[i]
		// A relation belongs to the type declared in the same file under the
		// reference's container name.
		var decls map[string][]symbol.Symbol
		for _, ref := range fi.References {
			kind, ok := relationOf(ref.Kind)
			if !ok || ref.Container == "" {
				continue
			}
			if decls == nil {
				decls = make(map[string][]symbol.Symbol)
				for _, s := range fi.Symbols {
					if isTypeLike(s.Kind) && s.Qualified != "" {
						decls[s.Qualified] = append(decls[s.Qualified], s)
					}
				}
			}
			for _, d := range decls[ref.Container] {
				st := r.structural.types[d.ID]
				if st == nil {
					st = &typeStructure{adapted: make(map[string]bool)}
					r.structural.types[d.ID] = st
				}
				if ref.Kind == reference.KindTraitAdaptation {
					st.adapted[ref.Name] = true
					continue
				}
				var targets []symbol.Symbol
				if ref.NameQualified != "" {
					targets = r.typeDecls(ref.Language, ref.NameQualified)
				}
				st.relations = append(st.relations, typeRelation{kind: kind, name: ref.NameQualified, targets: targets})
			}
		}
	}
}

func relationOf(k reference.ReferenceKind) (relationKind, bool) {
	switch k {
	case reference.KindInheritance:
		return relParent, true
	case reference.KindUsesTrait:
		return relTrait, true
	case reference.KindImplements:
		return relInterface, true
	case reference.KindTraitAdaptation:
		return 0, true
	}
	return 0, false
}

// inheritedLookup is the outcome of a structural member lookup.
type inheritedLookup struct {
	members   []symbol.Symbol
	ambiguous bool   // a participant is ambiguous or unidentified: at most Candidate
	blocked   bool   // the declaring type cannot be determined
	via       string // how the member was reached, for evidence
}

// lookupInherited finds where member name of type t is declared when t does
// not declare it itself. call selects callable members (a call never targets a
// same-named property or constant); otherwise only non-callable members.
func (r *Resolver) lookupInherited(t symbol.Symbol, name string, call bool) inheritedLookup {
	l := memberLookup{name: name, call: call, visiting: make(map[symbol.SymbolID]bool)}
	return l.find(r, t, true, 0)
}

// memberLookup is one structural lookup of a member name.
type memberLookup struct {
	name     string
	call     bool
	visiting map[symbol.SymbolID]bool
}

// find searches t (own members, traits, parents, interfaces). own is false
// once the search has left the receiver's own type for a supertype, where
// private members are not inherited.
func (l *memberLookup) find(r *Resolver, t symbol.Symbol, own bool, depth int) inheritedLookup {
	name := l.name
	if l.visiting[t.ID] || depth > maxStructuralDepth {
		return inheritedLookup{blocked: true} // cyclic or absurdly deep relations
	}
	l.visiting[t.ID] = true
	defer delete(l.visiting, t.ID)

	if declared := r.declaredMembers(t, name, l.call); len(declared) > 0 {
		if !own && anyPrivate(declared) {
			return inheritedLookup{blocked: true}
		}
		return inheritedLookup{members: declared, via: t.Qualified}
	}
	st := r.structureOf(t)
	if st == nil {
		return inheritedLookup{}
	}
	if st.adapted[name] {
		return inheritedLookup{blocked: true}
	}

	// Traits: members imported into t itself.
	var found inheritedLookup
	unknownTrait := false
	for _, rel := range st.relations {
		if rel.kind != relTrait {
			continue
		}
		if len(rel.targets) != 1 {
			unknownTrait = true
			continue
		}
		sub := l.find(r, rel.targets[0], own, depth+1)
		if sub.blocked {
			unknownTrait = true
			continue
		}
		found.merge(sub, "trait "+rel.targets[0].Qualified)
	}
	if len(found.members) > 0 {
		// An unidentified trait may declare n as well: the identified
		// declaration is a candidate, never a resolved target.
		found.ambiguous = found.ambiguous || unknownTrait
		return found
	}
	if unknownTrait {
		return inheritedLookup{blocked: true}
	}

	// Parents, then implemented interfaces: inherited members.
	for _, kind := range []relationKind{relParent, relInterface} {
		for _, rel := range st.relations {
			if rel.kind != kind {
				continue
			}
			if len(rel.targets) == 0 {
				return inheritedLookup{blocked: true}
			}
			for _, target := range rel.targets {
				sub := l.find(r, target, false, depth+1)
				if sub.blocked {
					return inheritedLookup{blocked: true}
				}
				what := "inherited from " + target.Qualified
				if kind == relInterface {
					what = "declared by interface " + target.Qualified
				}
				found.merge(sub, what)
			}
			if len(rel.targets) > 1 {
				found.ambiguous = true
			}
		}
		if len(found.members) > 0 {
			return found
		}
	}
	return found
}

// merge adds sub's members (deduplicated) under the description what.
func (l *inheritedLookup) merge(sub inheritedLookup, what string) {
	if len(sub.members) == 0 {
		return
	}
	for _, m := range sub.members {
		dup := false
		for _, have := range l.members {
			if have.ID == m.ID {
				dup = true
				break
			}
		}
		if !dup {
			l.members = append(l.members, m)
		}
	}
	l.ambiguous = l.ambiguous || sub.ambiguous
	if l.via == "" {
		l.via = what
	} else {
		l.via += ", " + what
	}
}

// declaredMembers returns the members named name lexically declared by t:
// callable ones (methods, constructors) for a call, the others otherwise.
func (r *Resolver) declaredMembers(t symbol.Symbol, name string, call bool) []symbol.Symbol {
	var out []symbol.Symbol
	for _, s := range r.byName[nameKey{r.spaceOf[t.Location.File], name}] {
		if s.ParentQualified != t.Qualified || s.Location.File != t.Location.File {
			continue
		}
		if callable := s.Kind == symbol.KindMethod || s.Kind == symbol.KindConstructor || s.Kind == symbol.KindFunction; callable != call {
			continue
		}
		out = append(out, s)
	}
	return out
}

func anyPrivate(syms []symbol.Symbol) bool {
	for _, s := range syms {
		if s.Visibility == "private" {
			return true
		}
	}
	return false
}

// inheritedResolution resolves ref's member through the supertypes of types
// (the receiver's type declarations, none of which declares the member). It
// returns ok == false when the lookup yields no target.
func (r *Resolver) inheritedResolution(res Resolution, ref reference.Reference, types []symbol.Symbol, typeConf Confidence, detail string) (Resolution, bool) {
	var found inheritedLookup
	for _, t := range types {
		l := r.lookupInherited(t, ref.Name, ref.Kind == reference.KindCall)
		if l.blocked {
			return res, false
		}
		found.merge(l, l.via)
	}
	if len(found.members) == 0 {
		return res, false
	}
	conf := typeConf
	if found.ambiguous && conf > ConfidenceCandidate {
		conf = ConfidenceCandidate
	}
	sortSymbols(found.members)
	// pickBest lowers several members (a collision) to Candidate.
	return r.pickBest(res, found.members, conf, EvidenceQualifiedIdentity,
		fmt.Sprintf("%s; %q %s", detail, ref.Name, found.via)), true
}
