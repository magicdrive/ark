package resolver

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
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
		r.aliases = make(map[langQualified][]string)
		r.memberAliases = make(map[langQualified][]string)
		r.aliasCaps = make(map[langQualified]Confidence)
		r.heads = make(map[langQualified]bool)
		r.fallbacks = make(map[langQualified][]string)
		r.openAliases = make(map[langQualified]bool)
		for _, fi := range r.files {
			for _, a := range fi.Aliases {
				if a.Qualified != "" && a.Target != "" {
					k := langQualified{fi.Language, a.Qualified}
					head, _, _ := strings.Cut(a.Qualified, "::")
					r.heads[langQualified{fi.Language, head}] = true
					if a.ConfidenceCap != "" {
						limit := ConfidenceCandidate // unknown caps fail safe
						if a.ConfidenceCap == ConfidenceStrong.String() {
							limit = ConfidenceStrong
						}
						if prev, ok := r.aliasCaps[k]; !ok || limit < prev {
							r.aliasCaps[k] = limit
						}
					}
					if a.Open {
						r.openAliases[k] = true
					}
					switch {
					case a.Fallback:
						r.fallbacks[k] = append(r.fallbacks[k], a.Target)
					case a.Members:
						r.memberAliases[k] = append(r.memberAliases[k], a.Target)
					default:
						r.aliases[k] = append(r.aliases[k], a.Target)
					}
				}
			}
			for _, s := range fi.Symbols {
				if s.Qualified == "" || (!fi.IdentityOnly && !isTypeLike(s.Kind)) {
					continue
				}
				k := langQualified{s.Language, s.Qualified}
				r.qualified[k] = append(r.qualified[k], s)
				head, _, _ := strings.Cut(s.Qualified, "::")
				r.heads[langQualified{s.Language, head}] = true
			}
		}
		for _, syms := range r.qualified {
			sortSymbols(syms)
		}
		for _, m := range []map[langQualified][]string{r.aliases, r.memberAliases, r.fallbacks} {
			for k, targets := range m {
				// Several files may state one alias: it is one reading.
				sort.Strings(targets)
				m[k] = slices.Compact(targets)
			}
		}
	})
	return r.qualified[langQualified{language, qualified}]
}

// MaxAliasDepth bounds a chain of aliases (language.AliasDraft): what a
// longer chain names is not followed.
const MaxAliasDepth = 8

// ExpandAliases returns the declarations an identity names: its own
// (decls), those its aliases name, and those the identity with a leading
// part of its path replaced by that part's alias names (a re-exported
// module: `pub use a::b as m;` then m::x) — all of them, so an identity two
// readings give (#[cfg] alternatives: an item in one configuration, an alias
// in the other) names both, as does a path two of whose leading parts are
// aliases. aliases(q, below) returns the targets of q's aliases: below is
// false for the identity being read itself, true for a leading part of its
// path, which an alias stated for the members below its identity only
// (language.AliasDraft.Members) also reads. via lists the aliases followed.
// Each identity is expanded once, by its shortest chain, so a cycle ends and
// shared aliases cost nothing more; a chain longer than MaxAliasDepth is not
// followed. Both R0 and the index's lookup of a re-exported name
// (index.SymbolsThroughAliases) use it.
func ExpandAliases(qualified string, decls func(string) []symbol.Symbol, aliases func(q string, below bool) []string) (out []symbol.Symbol, via []string) {
	out, via, _, _ = expandAliases(qualified, decls, aliases, nil, nil)
	return out, via
}

// expandAliases is ExpandAliases, which also returns used: the aliases of
// via on a chain that reaches a declaration, in via's order. via bounds a
// resolution (AliasDraft.ConfidenceCap: an alias followed may name what
// the reference denotes even where Ark finds nothing); used explains one.
// live, when set, reports whether anything is declared or aliased under an
// identity's leading part: an alias target under none names nothing and is
// not expanded (it stays in via).
func expandAliases(qualified string, decls func(string) []symbol.Symbol, aliases, fallbacks func(q string, below bool) []string, live func(string) bool) (out []symbol.Symbol, via, used []string, truncated bool) {
	// Breadth first: an identity is first reached by its shortest chain, so
	// marking it seen never cuts a shorter path the depth bound would allow.
	type step struct {
		q     string
		depth int
		found bool
	}
	type edge struct {
		alias string
		from  int
		to    int // -1: not followed (names nothing, or past the depth bound)
	}
	type deferred struct {
		from         int
		alias        string
		target, rest string
		done         bool
	}
	var edges []edge
	var pending []deferred
	index := map[string]int{qualified: 0}
	steps := []step{{q: qualified}}
	visit := func(from int, alias, target, rest string, depth int) {
		if live != nil && !live(target) {
			// Nothing is declared or aliased under it: it names nothing.
			via = append(via, alias)
			edges = append(edges, edge{alias, from, -1})
			return
		}
		q := target
		if rest != "" {
			q += "::" + rest
		}
		to, ok := index[q]
		if !ok {
			to = -1
			if depth <= MaxAliasDepth {
				to = len(steps)
				index[q] = to
				steps = append(steps, step{q: q, depth: depth})
			} else {
				truncated = true // what it names is not known
			}
		}
		via = append(via, alias)
		edges = append(edges, edge{alias, from, to})
	}
	expand := func(start int) {
		for i := start; i < len(steps); i++ {
			st := steps[i]
			d := decls(st.q)
			steps[i].found = len(d) > 0
			out = append(out, d...)
			for _, target := range aliases(st.q, false) {
				visit(i, st.q, target, "", st.depth+1)
			}
			if fallbacks != nil {
				for _, target := range fallbacks(st.q, false) {
					pending = append(pending, deferred{from: i, alias: st.q, target: target})
				}
			}
			// Every leading part of the path that is an alias gives a
			// reading: all of them are followed.
			parts := strings.Split(st.q, "::")
			for j := len(parts) - 1; j >= 1; j-- {
				prefix := strings.Join(parts[:j], "::")
				rest := strings.Join(parts[j:], "::")
				for _, target := range aliases(prefix, true) {
					visit(i, prefix, target, rest, st.depth+1)
				}
				if fallbacks != nil {
					for _, target := range fallbacks(prefix, true) {
						pending = append(pending, deferred{from: i, alias: prefix, target: target, rest: rest})
					}
				}
			}
		}
	}
	// A step reaches a declaration when it is one or an edge leads to one
	// that does (to a fixed point: an edge may lead back to an earlier step).
	settle := func() {
		for changed := true; changed; {
			changed = false
			for _, e := range edges {
				if e.to >= 0 && steps[e.to].found && !steps[e.from].found {
					steps[e.from].found, changed = true, true
				}
			}
		}
	}
	expand(0)
	settle()
	// A fallback is read only where the identity names nothing otherwise:
	// from a step that reaches no declaration, once the rest is expanded.
	for more := true; more; {
		more = false
		start := len(steps)
		for k := range pending {
			p := &pending[k]
			if !p.done && !steps[p.from].found {
				p.done, more = true, true
				visit(p.from, p.alias, p.target, p.rest, steps[p.from].depth+1)
			}
		}
		if more {
			expand(start)
			settle()
		}
	}
	for _, e := range edges {
		if e.to >= 0 && steps[e.to].found {
			used = append(used, e.alias)
		}
	}
	return out, via, used, truncated
}

// identityDecls returns the declarations an identity names in language
// (ExpandAliases over the language's declarations and aliases).
func (r *Resolver) identityDecls(language, qualified string) (decls []symbol.Symbol, via, used []string, truncated bool) {
	d, via, used, truncated := expandAliases(qualified,
		func(q string) []symbol.Symbol { return r.typeDecls(language, q) },
		func(q string, below bool) []string {
			k := langQualified{language, q}
			if below {
				return aliasTargets(r.aliases[k], r.memberAliases[k])
			}
			return r.aliases[k]
		},
		func(q string, below bool) []string {
			// Read only where q names nothing else: no declaration, no
			// alias into anything declared or aliased.
			k := langQualified{language, q}
			if len(r.fallbacks[k]) == 0 || len(r.typeDecls(language, q)) > 0 {
				return nil
			}
			for _, t := range aliasTargets(r.aliases[k], r.memberAliases[k]) {
				head, _, _ := strings.Cut(t, "::")
				if r.heads[langQualified{language, head}] {
					return nil
				}
			}
			return r.fallbacks[k]
		},
		func(q string) bool {
			head, _, _ := strings.Cut(q, "::")
			return r.heads[langQualified{language, head}]
		})
	return uniqueSymbols(d), via, used, truncated
}

// aliasTargets returns an identity's alias targets, those of the aliases
// that read it and those that read only the members below it, sorted.
func aliasTargets(own, members []string) []string {
	if len(members) == 0 {
		return own
	}
	if len(own) == 0 {
		return members
	}
	out := append(slices.Clone(own), members...)
	sort.Strings(out)
	return slices.Compact(out)
}

// uniqueSymbols drops repeated symbols, keeping the order.
func uniqueSymbols(syms []symbol.Symbol) []symbol.Symbol {
	if len(syms) < 2 {
		return syms[:len(syms):len(syms)]
	}
	seen := make(map[symbol.SymbolID]bool, len(syms))
	out := syms[:0:0]
	for _, s := range syms {
		if !seen[s.ID] {
			seen[s.ID] = true
			out = append(out, s)
		}
	}
	sortSymbols(out)
	return out
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
		if inherited, ok := r.inheritedResolution(res, ref, types, conf, EvidenceQualifiedIdentity, false, detail); ok {
			return inherited
		}
		return own
	}

	types, via, used, truncated := r.identityDecls(ref.Language, ref.NameQualified)
	detail := fmt.Sprintf("%q", ref.NameQualified)
	// A chain past the depth bound names what Ark did not follow: the
	// result is bounded by nothing it found.
	open := truncated
	for _, q := range via {
		open = open || r.openAliases[langQualified{ref.Language, q}]
	}
	if len(types) == 0 {
		if len(via) > 0 {
			detail += fmt.Sprintf(" (through the re-export of %s)", strings.Join(via, ", "))
		}
		if folded, ok := r.caseFoldedResolution(res, ref, detail); ok {
			folded.Open = open
			return folded
		}
		res = noQualifiedDeclaration(res, ref, detail)
		if open {
			// What the identity stands for may be no declaration of the
			// index: not outside the repository, and unbounded.
			res.OutsideRepository, res.Open = false, true
		}
		return res
	}
	// The aliases on a chain to a declaration explain it; one followed in
	// vain only bounds it (below).
	if len(used) > 0 {
		detail += fmt.Sprintf(" (through the re-export of %s)", strings.Join(used, ", "))
	}
	where := fmt.Sprintf("declared at %s", types[0].Location.File)
	if len(types) > 1 {
		where = fmt.Sprintf("declared %d times", len(types))
	}
	// pickBest downgrades several declarations to Candidate.
	res = r.pickBest(res, types, ConfidenceExact, EvidenceQualifiedIdentity, detail+" is "+where)
	// An alias followed on the way may rest on weaker evidence than the
	// language's rules (AliasDraft.ConfidenceCap): it bounds the result,
	// even where it names nothing Ark finds (what it stands for may be the
	// target).
	capped := map[string]bool{}
	for _, q := range via {
		if limit, ok := r.aliasCaps[langQualified{ref.Language, q}]; ok && !capped[q] {
			capped[q] = true
			res = capConfidence(res, limit, ResolutionEvidence{
				Kind:   EvidenceConfidenceCap,
				Detail: fmt.Sprintf("the alias %s permits at most %s", q, limit),
			})
		}
	}
	if truncated {
		res = capConfidence(res, ConfidenceCandidate, ResolutionEvidence{
			Kind:   EvidenceConfidenceCap,
			Detail: fmt.Sprintf("a chain of aliases longer than %d is not followed: what it names is unknown", MaxAliasDepth),
		})
	}
	res.Open = open
	return res
}

// caseFoldedResolution resolves a reference whose identity's leading path
// names no file of the repository but may name one written in another case
// (ReferenceDraft.PathCaseFold: a case-insensitive file system opens it):
// the declarations the identity names in each such file, never more than a
// Candidate — on a case-sensitive file system the path names nothing.
func (r *Resolver) caseFoldedResolution(res Resolution, ref reference.Reference, detail string) (Resolution, bool) {
	if !ref.PathCaseFold {
		return res, false
	}
	file, rest, _ := strings.Cut(ref.NameQualified, "::")
	// The leading path, or the paths an identity standing for one aliases
	// (an import's own identity, which stands for the file it names).
	paths := []string{file}
	for _, t := range r.aliases[langQualified{ref.Language, file}] {
		if !strings.HasPrefix(t, "{") && !strings.Contains(t, "::") {
			paths = append(paths, t)
		}
	}
	var found []symbol.Symbol
	for _, p := range paths {
		for _, other := range r.filesByFoldedPath(ref.Language, p) {
			if other == p {
				continue
			}
			q := other
			if rest != "" {
				q += "::" + rest
			}
			d, _, _, _ := r.identityDecls(ref.Language, q)
			found = append(found, d...)
		}
	}
	if len(found) == 0 {
		return res, false
	}
	found = uniqueSymbols(found)
	res = r.pickBest(res, found, ConfidenceCandidate, EvidenceQualifiedIdentity,
		fmt.Sprintf("%s names no file; a file of the path in another case declares it", detail))
	return capConfidence(res, ConfidenceCandidate, ResolutionEvidence{
		Kind:   EvidenceConfidenceCap,
		Detail: "a path matching only in another case names the file on a case-insensitive file system alone",
	}), true
}

// filesByFoldedPath returns the paths of the language's files equal to
// path but for case.
func (r *Resolver) filesByFoldedPath(language, path string) []string {
	r.foldOnce.Do(func() {
		r.foldedFiles = map[langQualified][]string{}
		for _, fi := range r.files {
			for _, s := range fi.Symbols {
				if s.Qualified != "" && s.Qualified == string(s.Location.File) { // a file's own declaration
					k := langQualified{fi.Language, source.FoldPath(s.Qualified)}
					r.foldedFiles[k] = append(r.foldedFiles[k], s.Qualified)
				}
			}
		}
		for k, paths := range r.foldedFiles {
			sort.Strings(paths)
			r.foldedFiles[k] = slices.Compact(paths)
		}
	})
	return r.foldedFiles[langQualified{language, source.FoldPath(path)}]
}

// noQualifiedDeclaration is the R0 answer when no declaration carries the
// identity: outside the repository — unless the identity names a scope of
// the repository itself (IdentityInRepository), where it is undeclared or
// declared in syntax no provider extracts: Unresolved, and not external —
// or names the target only if declared (TentativeIdentity): Unresolved,
// with nothing known.
func noQualifiedDeclaration(res Resolution, ref reference.Reference, detail string) Resolution {
	res.Evidence = []ResolutionEvidence{{
		Kind:   EvidenceQualifiedIdentity,
		Detail: detail + " is not declared in the repository",
	}}
	res.OutsideRepository = !ref.IdentityInRepository && !ref.TentativeIdentity
	return res
}

// memberScopeResolution resolves a member — or, for a NamedArgument
// reference, a parameter — through a receiver declaration that states where
// they live (Symbol.MemberScope / ParameterScope / MembersOutside). It
// reports false when no receiver declaration carries such a statement and the
// reference is no named argument; the member is then looked up as under any
// other receiver type. A named argument is never looked up as a member.
// Several receiver declarations never pick one: the declarations their
// statements name are Candidates.
func (r *Resolver) memberScopeResolution(res Resolution, ref reference.Reference, types []symbol.Symbol) (Resolution, bool) {
	scopeOf := func(t symbol.Symbol) string {
		if ref.NamedArgument {
			return t.ParameterScope
		}
		return t.MemberScope
	}
	what := "member"
	if ref.NamedArgument {
		what = "parameter"
	}
	stated := false
	for _, t := range types {
		stated = stated || scopeOf(t) != "" || t.MembersOutside
	}
	if !stated {
		if !ref.NamedArgument {
			return res, false
		}
		res.Evidence = []ResolutionEvidence{{
			Kind:   EvidenceMemberScope,
			Detail: fmt.Sprintf("%q states no parameters for argument %q", ref.ReceiverTypeQualified, ref.Name),
		}}
		return res, true
	}
	if len(types) == 1 && types[0].MembersOutside {
		res.Evidence = []ResolutionEvidence{{
			Kind:   EvidenceMemberScope,
			Detail: fmt.Sprintf("the %ss of %q are declared outside the repository", what, types[0].Qualified),
		}}
		res.OutsideRepository = true
		return res, true
	}
	var decls []symbol.Symbol
	var target string
	for _, t := range types {
		if scopeOf(t) == "" {
			continue
		}
		target = scopeOf(t) + ref.Name
		decls = append(decls, r.typeDecls(ref.Language, target)...)
	}
	detail := fmt.Sprintf("%s %q of %q is %q", what, ref.Name, ref.ReceiverTypeQualified, target)
	conf := ConfidenceExact
	if len(types) > 1 {
		detail = fmt.Sprintf("%s %q of %q, declared %d times", what, ref.Name, ref.ReceiverTypeQualified, len(types))
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

// fileDecls returns the declarations of the file at path itself — symbols
// of a kind that declares its file (symbol.DeclaresFile) whose Qualified is
// their own FileID — and the declarations a path names inside its file
// (symbol.FileScoped, Qualified "<FileID> <name>"), of every language, in
// deterministic order, keyed by Qualified. It is the target index of path evidence (FileIdentity), the one
// lookup not keyed by language: a path names one file whatever it holds,
// while a qualified name means something else in each language.
func (r *Resolver) fileDecls(path string) []symbol.Symbol {
	r.fileOnce.Do(func() {
		r.fileDecl = make(map[string][]symbol.Symbol)
		for _, fi := range r.files {
			for _, s := range fi.Symbols {
				if symbol.DeclaresFile(s.Kind) && s.Qualified == string(s.Location.File) ||
					symbol.FileScoped(s.Kind) && strings.HasPrefix(s.Qualified, string(s.Location.File)+" ") {
					r.fileDecl[s.Qualified] = append(r.fileDecl[s.Qualified], s)
				}
			}
		}
		for _, syms := range r.fileDecl {
			sortSymbols(syms)
		}
	})
	return r.fileDecl[path]
}

// resolveFileIdentity resolves a reference whose NameQualified is a file's
// path to the declaration of that file, or a file's path and a name to the
// declaration of that name in that file (FileIdentity): Exact when one
// declaration states it, Candidate when several, Unresolved — or
// outside the repository, unless IdentityInRepository — when none. It never
// falls back to names.
func (r *Resolver) resolveFileIdentity(res Resolution, ref reference.Reference) Resolution {
	// A reference to a declaration in a file (a CSS Modules class) reaches
	// those only, one to a file's own declaration those only: a file named
	// "a.module.css .c" is no class c of a.module.css.
	inFile := false
	for _, k := range strings.Split(ref.TargetKinds, ",") {
		if symbol.FileScoped(symbol.SymbolKind(k)) {
			inFile = true
		}
	}
	var decls []symbol.Symbol
	for _, s := range r.fileDecls(ref.NameQualified) {
		if symbol.FileScoped(s.Kind) == inFile {
			decls = append(decls, s)
		}
	}
	detail := fmt.Sprintf("the file %q", ref.NameQualified)
	if inFile {
		detail = fmt.Sprintf("the declaration %q of a file", ref.NameQualified)
	}
	if len(decls) == 0 {
		return noQualifiedDeclaration(res, ref, detail)
	}
	return r.pickBest(res, decls, ConfidenceExact, EvidenceQualifiedIdentity, detail+" is declared")
}
