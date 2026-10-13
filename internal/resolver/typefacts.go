package resolver

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Declared-type evaluation: receiver chains and promoted members.
//
// A provider states, for each declaration, the types its own text writes
// (language.TypeFact) and, for a receiver without a proven type, how the
// receiver is built (language.ReferenceDraft.ReceiverChain). This file
// evaluates both, generically:
//
//   - A chain's steps are looked up by the referencing file's own scoping
//     (package scoping, packagescope.go: the file's package, its imports, its
//     dot imports), a fact's types by the declaring file's. Every step must
//     name exactly one declaration; a step that names none or several — a
//     name declared outside the repository, an ambiguous one, a
//     declaration stating no type, a result typed by a type parameter — ends
//     the evaluation without a result, and the reference is resolved as if
//     it carried no chain (resolvePackageScoped). Nothing is guessed.
//   - Member lookup on a type finds its own members first (membersOf: a
//     member declared on the type itself is the selection, whatever its
//     embedded types declare). Without one, a member is looked for through
//     the type's embedded types (TypeFact.Fields, Embedded): depth by
//     depth, the shallowest depth with a field or a method of that name
//     decides; two at that depth are ambiguous (Candidate), a field is a
//     field (no method is meant). The search stops without a target when a
//     participant's members are unknown at or above the depth it decides:
//     an embedded type declared outside the repository, of ambiguous
//     identity, or whose fields could not be read — an unknown participant
//     must not disappear behind a known one (ARCHITECTURE.md §3.5). An
//     unknown participant at the deciding depth leaves the found members
//     Candidates: it could declare the name too.
//   - An interface's members are its own methods and those of the
//     interfaces it embeds, all of one depth.
//
// A resolution reached through a chain or a promotion is at most Strong: it
// is the one target the declarations' types lead to, not a lexically
// contained member.

// EvidenceReceiverChain: the receiver's type was derived from declarations
// by a provider-stated chain (language.ReferenceDraft.ReceiverChain).
const EvidenceReceiverChain EvidenceKind = "receiver_chain"

// EvidencePromotedMember: the member was found through embedded types
// (language.FieldFact.Embedded).
const EvidencePromotedMember EvidenceKind = "promoted_member"

// maxChainDepth bounds the evaluation of chains that name variables whose
// own facts are chains (var a = b.c(); var b = d()...).
const maxChainDepth = 16

type factIndex struct {
	once sync.Once
	byID map[symbol.SymbolID]*language.TypeFact
}

// factOf returns the type fact of declaration s, or nil.
func (r *Resolver) factOf(s symbol.Symbol) *language.TypeFact {
	r.facts.once.Do(r.buildFacts)
	return r.facts.byID[s.ID]
}

func (r *Resolver) buildFacts() {
	r.facts.byID = map[symbol.SymbolID]*language.TypeFact{}
	for i := range r.files {
		fi := &r.files[i]
		if len(fi.TypeFacts) == 0 {
			continue
		}
		byQualified := map[string][]symbol.Symbol{}
		for _, s := range fi.Symbols {
			byQualified[s.Qualified] = append(byQualified[s.Qualified], s)
		}
		for j := range fi.TypeFacts {
			f := &fi.TypeFacts[j]
			for _, s := range byQualified[f.Decl] {
				if st := s.Location.Range.Start; st.Line == f.Line && st.Column == f.Column {
					r.facts.byID[s.ID] = f
				}
			}
		}
	}
}

// declFile is the file index of a declaration.
func (r *Resolver) declFile(s symbol.Symbol) *FileIndex { return r.byFile[s.Location.File] }

// uniqueDecl returns the one package-level declaration N or q.N names in
// file fi by package scoping, or false.
func (r *Resolver) uniqueDecl(fi *FileIndex, q, n string) (symbol.Symbol, bool) {
	if fi == nil || !fi.PackageScoped {
		return symbol.Symbol{}, false
	}
	var cands []symbol.Symbol
	if q != "" {
		imp, ok := r.importNamed(q, *fi)
		if !ok || imp.dir == "" {
			return symbol.Symbol{}, false
		}
		cands = r.importableDecls(imp.dir, imp.pkg, n, *fi)
	} else {
		probe := reference.Reference{Name: n, Kind: reference.KindValueReference, Language: fi.Language, Location: fileStart(fi)}
		res := r.resolvePackageName(Resolution{Confidence: ConfidenceUnresolved}, probe, *fi)
		if !res.HasUniqueTarget() {
			return symbol.Symbol{}, false
		}
		s, ok := r.byID[res.Candidates[0].SymbolID]
		if !ok || r.declNamesakes(s) {
			return symbol.Symbol{}, false
		}
		cands = []symbol.Symbol{s}
	}
	if len(cands) != 1 {
		return symbol.Symbol{}, false
	}
	return cands[0], true
}

// fileStart is a location in fi with no enclosing container.
func fileStart(fi *FileIndex) source.Location { return source.Location{File: fi.FileID} }

// namedTypeDecl resolves a named TypeExpr in file fi's scope to the one
// type declaration it names, following aliases; false for any other type,
// one outside the repository, or an ambiguous one.
func (r *Resolver) namedTypeDecl(fi *FileIndex, t language.TypeExpr) (symbol.Symbol, bool) {
	q, n, ok := language.TypeNamed(language.TypeDeref(t))
	if !ok {
		return symbol.Symbol{}, false
	}
	s, ok := r.uniqueDecl(fi, q, n)
	if !ok || !ownsMembers(s.Kind) {
		return symbol.Symbol{}, false
	}
	return r.followAlias(s)
}

// followAlias returns the declaration an alias names (through chains of
// aliases), or s itself when s is no alias.
func (r *Resolver) followAlias(s symbol.Symbol) (symbol.Symbol, bool) {
	for i := 0; i < maxChainDepth; i++ {
		f := r.factOf(s)
		if f == nil || f.Alias == "" {
			return s, true
		}
		next, ok := r.namedTypeDecl(r.declFile(s), f.Alias)
		if !ok {
			return symbol.Symbol{}, false
		}
		s = next
	}
	return symbol.Symbol{}, false
}

// chainValue is the state of a chain's evaluation.
type chainValue struct {
	kind chainKind
	// kindValue: a value of type typ, written in file fi's scope.
	typ language.TypeExpr
	fi  *FileIndex
	// kindFunc: a function or method; kindTypeName: a type declaration.
	sym symbol.Symbol
}

type chainKind int

const (
	kindValue chainKind = iota
	kindFunc
	kindTypeName
)

// evalChain evaluates chain in file fi's scope; false when a step names no
// single declaration or states no type.
func (r *Resolver) evalChain(chain string, fi *FileIndex, depth int) (chainValue, bool) {
	steps, ok := language.ChainSteps(chain)
	if !ok || depth > maxChainDepth {
		return chainValue{}, false
	}
	var v chainValue
	for _, st := range steps {
		switch st.Op {
		case "type":
			v = chainValue{kind: kindValue, typ: st.Arg, fi: fi}
		case "decl":
			q, n := "", st.Arg
			if a, b, dotted := strings.Cut(st.Arg, "."); dotted {
				q, n = a, b
			}
			s, ok := r.uniqueDecl(fi, q, n)
			if !ok {
				return chainValue{}, false
			}
			switch {
			case s.Kind == symbol.KindFunction:
				v = chainValue{kind: kindFunc, sym: s}
			case s.Kind == symbol.KindVariable:
				f := r.factOf(s)
				switch {
				case f == nil:
					return chainValue{}, false
				case f.Type != "":
					v = chainValue{kind: kindValue, typ: f.Type, fi: r.declFile(s)}
				case f.Chain != "":
					if v, ok = r.evalChain(f.Chain, r.declFile(s), depth+1); !ok {
						return chainValue{}, false
					}
				default:
					return chainValue{}, false
				}
			case ownsMembers(s.Kind):
				t, ok := r.followAlias(s)
				if !ok {
					return chainValue{}, false
				}
				v = chainValue{kind: kindTypeName, sym: t}
			default:
				return chainValue{}, false
			}
		case "call":
			switch v.kind {
			case kindFunc:
				f := r.factOf(v.sym)
				if f == nil || st.Call >= len(f.Results) || f.Results[st.Call] == "" {
					return chainValue{}, false
				}
				v = chainValue{kind: kindValue, typ: f.Results[st.Call], fi: r.declFile(v.sym)}
			case kindTypeName:
				// A conversion T(x): a value of type T.
				if st.Call != 0 {
					return chainValue{}, false
				}
				v = chainValue{kind: kindValue, typ: "", sym: v.sym}
			default:
				return chainValue{}, false
			}
		case "member":
			switch v.kind {
			case kindTypeName:
				// A method expression T.M: a method of T's method set (its
				// own or a promoted one), never a field.
				m := r.lookupMember(v.sym, st.Arg)
				if m.status != memberFound || m.field != nil || len(m.methods) != 1 {
					return chainValue{}, false
				}
				v = chainValue{kind: kindFunc, sym: m.methods[0]}
			case kindValue:
				t, ok := r.valueType(v)
				if !ok {
					return chainValue{}, false
				}
				m := r.lookupMember(t, st.Arg)
				switch {
				case m.status == memberFound && m.field != nil:
					v = chainValue{kind: kindValue, typ: m.field.Type, fi: m.fieldFile}
				case m.status == memberFound && len(m.methods) == 1:
					v = chainValue{kind: kindFunc, sym: m.methods[0]}
				default:
					return chainValue{}, false
				}
			default:
				return chainValue{}, false
			}
		case "elem":
			if v.kind == kindFunc || v.kind == kindTypeName {
				// f[X], T[X]: an instantiation — the same declaration.
				continue
			}
			typ, fi2, ok := r.collectionOf(v)
			if !ok {
				return chainValue{}, false
			}
			el := language.TypeElem(typ)
			if el == "" {
				return chainValue{}, false
			}
			v = chainValue{kind: kindValue, typ: el, fi: fi2}
		}
	}
	return v, true
}

// valueType returns the type declaration of a value's type: the value's
// own type through a pointer, aliases followed; false for any other type.
func (r *Resolver) valueType(v chainValue) (symbol.Symbol, bool) {
	if v.typ == "" && v.sym.ID != "" {
		return v.sym, true // a conversion's value
	}
	return r.namedTypeDecl(v.fi, v.typ)
}

// collectionOf returns the slice, array or map type of a value — its own
// type, or the underlying type of the defined type it is (type Nodes
// []*Node) — with the file whose scope it is written in.
func (r *Resolver) collectionOf(v chainValue) (language.TypeExpr, *FileIndex, bool) {
	typ, fi := language.TypeDeref(v.typ), v.fi
	for i := 0; i < maxChainDepth; i++ {
		if strings.HasPrefix(typ, "[]") || strings.HasPrefix(typ, "map[") {
			return typ, fi, true
		}
		var s symbol.Symbol
		var ok bool
		if typ == "" && v.sym.ID != "" && i == 0 {
			s, ok = v.sym, true
		} else {
			s, ok = r.namedTypeDecl(fi, typ)
		}
		if !ok {
			return "", nil, false
		}
		f := r.factOf(s)
		if f == nil || f.Underlying == "" {
			return "", nil, false
		}
		typ, fi = f.Underlying, r.declFile(s)
	}
	return "", nil, false
}

// memberStatus is the outcome of a member lookup.
type memberStatus int

const (
	memberUnknown   memberStatus = iota // a participant's members are unknown: no conclusion
	memberFound                         // methods (one, or several: ambiguous) or one field
	memberNone                          // no member of that name: every participant searched
	memberUncertain                     // found, beside an unknown participant at the same depth
)

type memberResult struct {
	status    memberStatus
	methods   []symbol.Symbol
	field     *language.FieldFact
	fieldFile *FileIndex
	promoted  bool // found through an embedded type
}

// methodsOf returns the methods named name of type t — its own, or for an
// interface those of the interfaces it embeds too; ok is false when they are
// unknown: an embedded interface is unknown, or t's name is declared as a
// type more than once in its package (several programs in one directory,
// build-tag variants), so methods attached by receiver name (membersOf) may
// belong to another declaration.
func (r *Resolver) methodsOf(t symbol.Symbol, name string) ([]symbol.Symbol, bool, bool) {
	if r.typeNamesakes(t) {
		return nil, false, false
	}
	if ms, _ := r.membersOf(t, name); len(ms) > 0 {
		return ms, false, true
	}
	f := r.factOf(t)
	if f == nil || !f.Interface {
		return nil, false, true
	}
	ms, ok := r.interfaceMethods(t, name, map[symbol.SymbolID]bool{}, 0)
	return ms, len(ms) > 0, ok
}

// declNamesakes reports whether another declaration of s's name exists in
// s's directory and package — build-tag variants (func newPoller() in two
// files, of two result types), several programs in one directory: package
// scoping still names one (the same file's), but its declared types are no
// evidence of the one the code compiles with.
func (r *Resolver) declNamesakes(s symbol.Symbol) bool {
	dir := filepath.Dir(string(s.Location.File))
	pkg := r.packageOf(s.Location.File)
	for _, ds := range r.dirSymbols[dir][nameKey{r.spaceOf[s.Location.File], s.Name}] {
		o := ds.sym
		if o.ID != s.ID && !isMember(*o) && r.packageOf(o.Location.File) == pkg {
			return true
		}
	}
	return false
}

// typeNamesakes reports whether another type declaration of t's name exists
// in t's directory and package.
func (r *Resolver) typeNamesakes(t symbol.Symbol) bool {
	dir := filepath.Dir(string(t.Location.File))
	pkg := r.packageOf(t.Location.File)
	for _, ds := range r.dirSymbols[dir][nameKey{r.spaceOf[t.Location.File], t.Name}] {
		s := ds.sym
		if s.ID == t.ID || isMember(*s) || !ownsMembers(s.Kind) || r.packageOf(s.Location.File) != pkg {
			continue
		}
		return true
	}
	return false
}

// interfaceMethods collects the methods named name of interface t and of
// the interfaces it embeds; ok is false when one of them is unknown.
func (r *Resolver) interfaceMethods(t symbol.Symbol, name string, seen map[symbol.SymbolID]bool, depth int) ([]symbol.Symbol, bool) {
	if seen[t.ID] || depth > maxStructuralDepth {
		return nil, depth <= maxStructuralDepth
	}
	seen[t.ID] = true
	f := r.factOf(t)
	if f == nil || !f.Interface || !f.Complete {
		return nil, false
	}
	ms, _ := r.membersOf(t, name)
	out := append([]symbol.Symbol(nil), ms...)
	for _, fd := range f.Fields {
		if !fd.Embedded {
			continue
		}
		e, ok := r.namedTypeDecl(r.declFile(t), fd.Type)
		if !ok {
			return nil, false
		}
		more, ok := r.interfaceMethods(e, name, seen, depth+1)
		if !ok {
			return nil, false
		}
		out = append(out, more...)
	}
	return out, true
}

// lookupMember finds the member name of type t: its own methods first, then
// — depth by depth — the fields and methods its embedded types promote.
func (r *Resolver) lookupMember(t symbol.Symbol, name string) memberResult {
	t, ok := r.followAlias(t)
	if !ok {
		return memberResult{status: memberUnknown}
	}
	if ms, promoted, ok := r.methodsOf(t, name); len(ms) > 0 {
		return memberResult{status: memberFound, methods: ms, promoted: promoted}
	} else if !ok {
		return memberResult{status: memberUnknown}
	}
	if f := r.factOf(t); f != nil && f.Interface {
		return memberResult{status: memberNone} // every embedded interface searched
	}
	type fieldHit struct {
		field *language.FieldFact
		file  *FileIndex
	}
	level := []symbol.Symbol{t}
	// carried: an embedded type of the current depth could not be
	// identified — its members, at this depth, are unknown.
	carried := false
	seen := map[symbol.SymbolID]bool{}
	for depth := 0; depth <= maxStructuralDepth; depth++ {
		if len(level) == 0 && !carried {
			return memberResult{status: memberNone}
		}
		var methods []symbol.Symbol
		var fields []fieldHit
		unknown := carried
		var next []symbol.Symbol
		nextUnknown := false
		atDepth := map[symbol.SymbolID]int{}
		for _, p := range level {
			atDepth[p.ID]++
		}
		for _, p := range level {
			if seen[p.ID] {
				continue // reached at a shallower depth, or counted at this one
			}
			seen[p.ID] = true
			if atDepth[p.ID] > 1 {
				// One type embedded along two paths at one depth: each of its
				// members appears twice there (Go's "multiples") — a selector
				// of one is ambiguous.
				if ms, _, ok := r.methodsOf(p, name); !ok || len(ms) > 0 {
					return memberResult{status: memberUnknown}
				}
				if f := r.factOf(p); f == nil || (f.Struct && !f.Complete) {
					return memberResult{status: memberUnknown}
				} else {
					for _, fd := range f.Fields {
						fn := fd.Name
						if fd.Embedded {
							_, fn, _ = language.TypeNamed(language.TypeDeref(fd.Type))
						}
						if fn == name {
							return memberResult{status: memberUnknown}
						}
					}
				}
			}
			if depth > 0 {
				ms, _, ok := r.methodsOf(p, name)
				if !ok {
					unknown = true
					continue
				}
				methods = append(methods, ms...)
			}
			f := r.factOf(p)
			switch {
			case f == nil:
				unknown = true
				continue
			case f.Interface:
				continue // its methods are its members; it has no fields
			case !f.Struct:
				// A defined type of a non-struct underlying type has no
				// fields — unless that type is a named one, whose fields
				// (and promotions) it shares.
				if _, _, named := language.TypeNamed(language.TypeDeref(f.Underlying)); named || f.Underlying == "" {
					unknown = true
				}
				continue
			case !f.Complete:
				unknown = true
				continue
			}
			for i := range f.Fields {
				fd := &f.Fields[i]
				fieldName := fd.Name
				if fd.Embedded {
					_, n, _ := language.TypeNamed(language.TypeDeref(fd.Type))
					fieldName = n
					if e, ok := r.namedTypeDecl(r.declFile(p), fd.Type); ok {
						next = append(next, e)
					} else {
						nextUnknown = true
					}
				}
				if fieldName != "" && fieldName == name {
					fields = append(fields, fieldHit{fd, r.declFile(p)})
				}
			}
		}
		hits := len(methods) + len(fields)
		if hits == 0 {
			if unknown {
				return memberResult{status: memberUnknown}
			}
			level, carried = next, nextUnknown
			continue
		}
		if len(fields) > 0 && hits > 1 {
			// A field beside another member of that name at one depth: an
			// ambiguous selector, no conclusion.
			return memberResult{status: memberUnknown}
		}
		out := memberResult{status: memberFound, methods: methods, promoted: depth > 0}
		if len(fields) == 1 {
			out.field, out.fieldFile = fields[0].field, fields[0].file
		}
		if unknown {
			out.status = memberUncertain
		}
		return out
	}
	return memberResult{status: memberUnknown}
}

// promotedResolution resolves ref's member through t's embedded types after
// no member of t itself was found (memberResolution); ok is false when the
// lookup is inconclusive or finds no method.
func (r *Resolver) promotedResolution(res Resolution, ref reference.Reference, t symbol.Symbol, typeConf Confidence, detail string) (Resolution, bool) {
	m := r.lookupMember(t, ref.Name)
	if len(m.methods) == 0 || m.field != nil || (m.status != memberFound && m.status != memberUncertain) {
		return res, false
	}
	conf := typeConf
	if conf > ConfidenceStrong {
		conf = ConfidenceStrong
	}
	if m.status == memberUncertain {
		conf = ConfidenceCandidate
	}
	out := r.pickBest(res, m.methods, conf, EvidencePromotedMember,
		fmt.Sprintf("%s; %q found through the embedded types of %s", detail, ref.Name, t.Qualified))
	if m.status == memberUncertain {
		out = capConfidence(out, ConfidenceCandidate, ResolutionEvidence{Kind: EvidencePromotedMember,
			Detail: "an embedded type of unknown members at the same depth may declare it too"})
	}
	return out, true
}

// resolveViaReceiverChain resolves ref through its ReceiverChain. ok is
// false when the chain proves no single type: the caller then resolves ref
// as if it carried no chain. A proven type without a method of that name (a
// field, a function-typed field called) is Unresolved — authoritatively.
func (r *Resolver) resolveViaReceiverChain(res Resolution, ref reference.Reference, fi FileIndex) (Resolution, bool) {
	v, ok := r.evalChain(ref.ReceiverChain, r.byFile[fi.FileID], 0)
	if !ok {
		return res, false
	}
	detail := fmt.Sprintf("receiver %q is %s", ref.ReceiverExpr, ref.ReceiverChain)
	var t symbol.Symbol
	switch v.kind {
	case kindTypeName:
		// A method expression selects among the type's methods — its own
		// and the promoted ones — never a field.
		t = v.sym
	case kindValue:
		if t, ok = r.valueType(v); !ok {
			return res, false
		}
	default:
		return res, false
	}
	m := r.lookupMember(t, ref.Name)
	switch {
	case m.status == memberUnknown:
		return res, false
	case m.status == memberNone || (m.field != nil && len(m.methods) == 0):
		what := "no member"
		if m.field != nil {
			what = "a field, no method,"
		}
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceReceiverChain, Detail: fmt.Sprintf("%s; %s has %s %q", detail, t.Qualified, what, ref.Name)}}
		return res, true
	}
	ek := EvidenceReceiverChain
	if m.promoted {
		ek = EvidencePromotedMember
	}
	out := r.pickBest(res, m.methods, ConfidenceStrong, ek, detail+"; type "+t.Qualified)
	if m.status == memberUncertain {
		out = capConfidence(out, ConfidenceCandidate, ResolutionEvidence{Kind: EvidencePromotedMember,
			Detail: "an embedded type of unknown members at the same depth may declare it too"})
	}
	return out, true
}
