package resolver

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Language lookup (language.ReferenceDraft.Lookup, ReceiverLookup).
//
// A provider whose language finds a name by searching scopes one file cannot
// see — the members a supertype declares, the other files of a package, a
// type an on-demand import may bring — states the search, step by step, and
// this file evaluates it against the repository's declarations. It decides
// nothing by similarity: every alternative names an identity (=K:Q) or a
// member of an identified type (@K:T.N), and a scope Ark cannot see is
// stated as such (?).
//
// What a passed step leaves unknown is kept: a step that found nothing but
// may hold the name outside what the repository declares (an identity no
// declaration carries, a supertype outside the repository, ?) bounds the
// result at Strong — a closed-world claim, like a unique name — and is what
// makes a search that finds nothing a referent outside the repository. A
// step the repository answers completely (a member lookup through identified
// supertypes only) is passed without a trace.

// EvidenceLanguageLookup: the target was found by the language's own search
// as the provider stated it (language.ReferenceDraft.Lookup).
const EvidenceLanguageLookup EvidenceKind = "language_lookup"

// kindClass is a Lookup kind class: T (type-like), V (field or constant), M
// (method), C (constructor).
type kindClass byte

func (k kindClass) admits(s symbol.SymbolKind) bool {
	switch k {
	case 'T':
		return isTypeLike(s)
	case 'V':
		return s == symbol.KindField || s == symbol.KindConstant
	case 'M':
		return s == symbol.KindMethod
	case 'C':
		return s == symbol.KindConstructor
	}
	return false
}

func (k kindClass) callable() bool { return k == 'M' || k == 'C' }

// lookupAlt is one alternative of a step.
type lookupAlt struct {
	op    byte // '=', '@', '?' or '!'
	class kindClass
	q     string // '=': the identity; '@': the type's
	name  string // '@': the member name
	path  []pathStep
}

// pathStep is a member a found type is followed through: a member type
// ('/'), or a field or constant and else a member type ('~').
type pathStep struct {
	op   byte
	name string
}

// parseLookup parses Lookup notation; ok is false for malformed text, which
// then states nothing that can be trusted.
func parseLookup(s string) (steps [][]lookupAlt, ok bool) {
	if s == "" {
		return nil, false
	}
	for _, st := range strings.Split(s, ";") {
		var alts []lookupAlt
		for _, a := range strings.Split(st, "|") {
			if a == "?" || a == "!" {
				alts = append(alts, lookupAlt{op: a[0]})
				continue
			}
			var path []pathStep
			if i := strings.IndexAny(a, "/~"); i >= 0 {
				rest := a[i:]
				a = a[:i]
				for rest != "" {
					j := strings.IndexAny(rest[1:], "/~")
					seg := rest
					if j >= 0 {
						seg = rest[:j+1]
					}
					if len(seg) < 2 {
						return nil, false
					}
					path = append(path, pathStep{op: seg[0], name: seg[1:]})
					rest = rest[len(seg):]
				}
			}
			if len(a) < 4 || (a[0] != '=' && a[0] != '@') || a[2] != ':' || !strings.ContainsRune("TVMC", rune(a[1])) {
				return nil, false
			}
			alt := lookupAlt{op: a[0], class: kindClass(a[1]), q: a[3:], path: path}
			if alt.op == '@' {
				i := strings.LastIndexByte(alt.q, '.')
				if i <= 0 || i == len(alt.q)-1 {
					return nil, false
				}
				alt.q, alt.name = alt.q[:i], alt.q[i+1:]
			}
			alts = append(alts, alt)
		}
		steps = append(steps, alts)
	}
	return steps, true
}

// implicitMember is a member a type's ImplicitMembers state.
type implicitMember struct {
	name, params string
	own          bool // declared implicitly by the type itself ("+"): the repository's
	of           string
}

// parseImplicit splits ImplicitMembers ("a(),+b(x,y)") at the commas outside
// parentheses.
func parseImplicit(s, of string) []implicitMember {
	var out []implicitMember
	depth, start := 0, 0
	add := func(e string) {
		own := strings.HasPrefix(e, "+")
		e = strings.TrimPrefix(e, "+")
		name, params := e, ""
		if i := strings.IndexByte(e, '('); i >= 0 {
			name, params = e[:i], e[i:]
		}
		if name != "" {
			out = append(out, implicitMember{name: name, params: params, own: own, of: of})
		}
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				add(s[start:i])
				start = i + 1
			}
		}
	}
	if start < len(s) {
		add(s[start:])
	}
	return out
}

// accepts reports whether a callable of parameters params ("" states none)
// accepts n arguments.
func accepts(params string, n int) bool {
	if params == "" {
		return true
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(params, "("), ")")
	count := 0
	variadic := false
	if inner != "" {
		parts := strings.Split(inner, ",")
		count = len(parts)
		variadic = strings.HasSuffix(parts[len(parts)-1], "...")
	}
	if variadic {
		return n >= count-1
	}
	return n == count
}

// memberSet is what a member search finds.
type memberSet struct {
	decls    []symbol.Symbol
	implicit []implicitMember
	open     bool // a scope searched may hold more than the repository declares
	ambig    bool // a participant is ambiguous: several readings
	presumed bool // an override taken from equal Parameters with unproven types
	tainted  bool // a cycle cut the search: not to be memoized
	// unknown: a type searched cannot list its members (a declaration
	// stating a cap): what it does not show may be anything — neither
	// outside the repository nor proven absent.
	unknown bool
	// uncertain: callables the reference may not be allowed to access (a
	// protected one of another package): kept, but never the one the
	// argument types narrow the others to.
	uncertain map[symbol.SymbolID]bool
}

func (m *memberSet) empty() bool { return len(m.decls) == 0 && len(m.implicit) == 0 }

// applyArity keeps the callables that accept the call's arguments, when the
// call states them (their count, and what is known of their types), and
// sorts what is kept.
func (m *memberSet) applyArity(ref reference.Reference) {
	if ref.CountsArguments {
		var decls []symbol.Symbol
		for _, d := range m.decls {
			if accepts(d.Parameters, ref.ArgumentCount) {
				decls = append(decls, d)
			}
		}
		var implicit []implicitMember
		for _, im := range m.implicit {
			if accepts(im.params, ref.ArgumentCount) {
				implicit = append(implicit, im)
			}
		}
		m.decls, m.implicit = decls, implicit
		if ref.ArgumentTypes != "" {
			m.applyArgumentTypes(ref)
		}
	}
	sortSymbols(m.decls)
}

func (m *memberSet) add(o memberSet) {
	for _, d := range o.decls {
		if !containsSymbol(m.decls, d.ID) {
			m.decls = append(m.decls, d)
		}
	}
	m.implicit = append(m.implicit, o.implicit...)
	m.open = m.open || o.open
	m.ambig = m.ambig || o.ambig
	m.presumed = m.presumed || o.presumed
	m.tainted = m.tainted || o.tainted
	m.unknown = m.unknown || o.unknown
}

func containsSymbol(syms []symbol.Symbol, id symbol.SymbolID) bool {
	for _, s := range syms {
		if s.ID == id {
			return true
		}
	}
	return false
}

// lookupState carries one evaluation: the declarations whose supertypes or
// members are being searched (a cycle ends the search, open) and the file
// the reference is written in (its SourceRoot).
type lookupState struct {
	from     source.FileID
	visiting map[symbol.SymbolID]bool
	depth    int
}

// supertype is one identified relation of a type declaration.
type supertype struct {
	targets []symbol.Symbol // none: outside the repository; several: ambiguous
	capped  bool            // identified at Strong only: what is found through it is at most Strong
	open    bool            // the identification passed a scope Ark cannot see
	unknown bool            // the identification passed a type whose members cannot be listed
}

// lookupMemo holds what evaluations share: supertype identifications and
// member searches computed without a cycle cut, the declarations of an
// identity, a type's implicit members. Each is a function of the
// repository alone, so memoizing it changes the cost, never an outcome;
// the slices it hands out are clipped, so a caller's append copies them.
type lookupMemo struct {
	mu       sync.Mutex
	supers   map[symbol.SymbolID][]supertype
	decls    map[declsKey]declsResult
	members  map[membersKey]memberSet
	implicit map[symbol.SymbolID][]implicitMember
	// relations: a file's inheritance and implements references by container.
	relations map[source.FileID]map[string][]reference.Reference
	// fileDecls: a file's first declaration of each qualified name.
	fileDecls map[source.FileID]map[string]symbol.Symbol
}

type declsKey struct {
	lang, q string
	class   kindClass
	root    string
}

type declsResult struct {
	decls     []symbol.Symbol
	elsewhere bool
}

type membersKey struct {
	t     symbol.SymbolID
	name  string
	class kindClass
}

// declsOf returns the declarations of identity q in lang admitted by class,
// preferring those of from's SourceRoot (language.Extraction.SourceRoot):
// elsewhere tells whether none of them is in that root (at most Strong when
// one, ambiguous when several).
func (r *Resolver) declsOf(lang, q string, class kindClass, from source.FileID) (decls []symbol.Symbol, elsewhere bool) {
	key := declsKey{lang, q, class, r.sourceRootOf(from)}
	r.lookups.mu.Lock()
	memo, ok := r.lookups.decls[key]
	r.lookups.mu.Unlock()
	if ok {
		return memo.decls, memo.elsewhere
	}
	decls, elsewhere = r.declsOfUncached(lang, q, class, from)
	decls = slices.Clip(decls)
	r.lookups.mu.Lock()
	if r.lookups.decls == nil {
		r.lookups.decls = make(map[declsKey]declsResult)
	}
	r.lookups.decls[key] = declsResult{decls, elsewhere}
	r.lookups.mu.Unlock()
	return decls, elsewhere
}

func (r *Resolver) declsOfUncached(lang, q string, class kindClass, from source.FileID) (decls []symbol.Symbol, elsewhere bool) {
	var all []symbol.Symbol
	for _, s := range r.typeDecls(lang, q) {
		if class.admits(s.Kind) {
			all = append(all, s)
		}
	}
	root := r.sourceRootOf(from)
	if root == "" || len(all) == 0 {
		return all, false
	}
	var same []symbol.Symbol
	for _, s := range all {
		if r.sourceRootOf(s.Location.File) == root {
			same = append(same, s)
		}
	}
	if len(same) > 0 {
		return same, false
	}
	return all, true
}

func (r *Resolver) sourceRootOf(f source.FileID) string {
	if fi, ok := r.byFile[f]; ok && fi.SourceRoot != "" {
		return fi.SourceRoot
	}
	return ""
}

// supertypesOf identifies the relations t's file states for t.
func (r *Resolver) supertypesOf(t symbol.Symbol, st *lookupState) (out []supertype, tainted bool) {
	r.lookups.mu.Lock()
	if r.lookups.supers == nil {
		r.lookups.supers = make(map[symbol.SymbolID][]supertype)
	}
	memo, ok := r.lookups.supers[t.ID]
	r.lookups.mu.Unlock()
	if ok {
		return memo, false
	}
	for _, ref := range r.relationRefs(t.Location.File)[t.Qualified] {
		var rel supertype
		switch {
		case ref.Lookup != "":
			// The relation is written in t's file: its root decides.
			sub := lookupState{from: t.Location.File, visiting: st.visiting, depth: st.depth + 1}
			o := r.evalLookup(ref.Lookup, reference.Reference{Language: ref.Language}, &sub)
			tainted = tainted || o.found.tainted
			for _, d := range o.found.decls {
				if isTypeLike(d.Kind) {
					rel.targets = append(rel.targets, d)
				}
			}
			rel.capped = o.strong || o.found.ambig
			rel.open = len(rel.targets) == 0
			rel.unknown = o.unknown || o.found.unknown
		case ref.NameQualified != "":
			decls, elsewhere := r.declsOf(ref.Language, ref.NameQualified, 'T', t.Location.File)
			rel.targets, rel.capped, rel.open = decls, elsewhere, len(decls) == 0
		default:
			rel.open = true
		}
		out = append(out, rel)
	}
	if !tainted {
		r.lookups.mu.Lock()
		r.lookups.supers[t.ID] = out
		r.lookups.mu.Unlock()
	}
	return out, tainted
}

// implicitOf returns t's ImplicitMembers, parsed once.
func (r *Resolver) implicitOf(t symbol.Symbol) []implicitMember {
	if t.ImplicitMembers == "" {
		return nil
	}
	r.lookups.mu.Lock()
	memo, ok := r.lookups.implicit[t.ID]
	r.lookups.mu.Unlock()
	if ok {
		return memo
	}
	ims := slices.Clip(parseImplicit(t.ImplicitMembers, t.Qualified))
	r.lookups.mu.Lock()
	if r.lookups.implicit == nil {
		r.lookups.implicit = make(map[symbol.SymbolID][]implicitMember)
	}
	r.lookups.implicit[t.ID] = ims
	r.lookups.mu.Unlock()
	return ims
}

// relationRefs returns the inheritance and implements references of file,
// by container, indexed once.
func (r *Resolver) relationRefs(file source.FileID) map[string][]reference.Reference {
	r.lookups.mu.Lock()
	idx, ok := r.lookups.relations[file]
	r.lookups.mu.Unlock()
	if ok {
		return idx
	}
	idx = map[string][]reference.Reference{}
	if fi, ok := r.byFile[file]; ok {
		for _, ref := range fi.References {
			if ref.Kind == reference.KindInheritance || ref.Kind == reference.KindImplements {
				idx[ref.Container] = append(idx[ref.Container], ref)
			}
		}
	}
	r.lookups.mu.Lock()
	if r.lookups.relations == nil {
		r.lookups.relations = make(map[source.FileID]map[string][]reference.Reference)
	}
	r.lookups.relations[file] = idx
	r.lookups.mu.Unlock()
	return idx
}

// outermost returns the outermost declaration enclosing s (s itself at top
// level); ok is false when the chain of parents cannot be followed.
func (r *Resolver) outermost(s symbol.Symbol) (symbol.Symbol, bool) {
	for i := 0; i < 64; i++ {
		if s.Parent == "" {
			return s, s.ParentQualified == ""
		}
		p, ok := r.byID[s.Parent]
		if !ok {
			return s, false
		}
		s = p
	}
	return s, false
}

// referenceOutermost returns the outermost declaration enclosing ref's
// container; ok is false when the container is no declaration of its file.
func (r *Resolver) referenceOutermost(ref reference.Reference) (symbol.SymbolID, bool) {
	if ref.Container == "" {
		return "", false
	}
	r.lookups.mu.Lock()
	byQ, ok := r.lookups.fileDecls[ref.Location.File]
	r.lookups.mu.Unlock()
	if !ok {
		byQ = map[string]symbol.Symbol{}
		if fi, ok := r.byFile[ref.Location.File]; ok {
			for _, s := range fi.Symbols {
				if _, seen := byQ[s.Qualified]; !seen {
					byQ[s.Qualified] = s
				}
			}
		}
		r.lookups.mu.Lock()
		if r.lookups.fileDecls == nil {
			r.lookups.fileDecls = make(map[source.FileID]map[string]symbol.Symbol)
		}
		r.lookups.fileDecls[ref.Location.File] = byQ
		r.lookups.mu.Unlock()
	}
	s, ok := byQ[ref.Container]
	if !ok {
		return "", false
	}
	top, ok := r.outermost(s)
	return top.ID, ok
}

// filterAccess leaves out the callables the reference certainly may not
// access — private ones outside their outermost enclosing declaration,
// ones without Visibility outside their Package — and marks the protected
// ones of another package uncertain (JLS 6.6, 15.12.2.1: only accessible
// methods are candidates).
func (r *Resolver) filterAccess(m *memberSet, ref reference.Reference) {
	var kept []symbol.Symbol
	refPkg := r.packageOf(ref.Location.File)
	var refTop symbol.SymbolID
	topKnown, topDone := false, false
	for _, d := range m.decls {
		if d.Kind != symbol.KindMethod && d.Kind != symbol.KindConstructor {
			kept = append(kept, d)
			continue
		}
		switch d.Visibility {
		case "private":
			if !topDone {
				refTop, topKnown = r.referenceOutermost(ref)
				topDone = true
			}
			declTop, ok := r.outermost(d)
			switch {
			case !topKnown || !ok:
				m.markUncertain(d.ID)
			case declTop.ID != refTop:
				continue
			}
		case "":
			if r.packageOf(d.Location.File) != refPkg {
				continue
			}
		case "protected":
			if r.packageOf(d.Location.File) != refPkg {
				m.markUncertain(d.ID)
			}
		}
		kept = append(kept, d)
	}
	m.decls = kept
}

func (m *memberSet) markUncertain(id symbol.SymbolID) {
	if m.uncertain == nil {
		m.uncertain = map[symbol.SymbolID]bool{}
	}
	m.uncertain[id] = true
}

// ownMembers returns the members named name of class lexically declared by t.
func (r *Resolver) ownMembers(t symbol.Symbol, name string, class kindClass) []symbol.Symbol {
	var out []symbol.Symbol
	for _, s := range r.typeDecls(t.Language, t.Qualified+"."+name) {
		if s.ParentQualified == t.Qualified && s.Location.File == t.Location.File && s.Name == name && class.admits(s.Kind) {
			out = append(out, s)
		}
	}
	sortSymbols(out)
	return out
}

// inherited reports whether a member declared by decl reaches a subtype
// along a path all of whose types are in pkgs: a private member never does,
// one without Visibility only within its own package.
func (r *Resolver) inherited(decl symbol.Symbol, pkgs []string) bool {
	switch decl.Visibility {
	case "private":
		return false
	case "":
		own := r.packageOf(decl.Location.File)
		for _, p := range pkgs {
			if p != own {
				return false
			}
		}
	}
	return true
}

// searchMembers finds the members named name of class of type t — its own
// and those it inherits (language.ReferenceDraft.Lookup) — as seen from a
// subtype path whose types' packages are pkgs (nil: t is where the search
// starts).
func (r *Resolver) searchMembers(t symbol.Symbol, name string, class kindClass, pkgs []string, st *lookupState) memberSet {
	// A search from the type itself (no subtype path) is memoized.
	if pkgs == nil {
		key := membersKey{t.ID, name, class}
		r.lookups.mu.Lock()
		memo, ok := r.lookups.members[key]
		r.lookups.mu.Unlock()
		if ok {
			return memo
		}
		set := r.searchMembersCapped(t, name, class, pkgs, st)
		if !set.tainted {
			set.decls, set.implicit = slices.Clip(set.decls), slices.Clip(set.implicit)
			r.lookups.mu.Lock()
			if r.lookups.members == nil {
				r.lookups.members = make(map[membersKey]memberSet)
			}
			r.lookups.members[key] = set
			r.lookups.mu.Unlock()
		}
		return set
	}
	return r.searchMembersCapped(t, name, class, pkgs, st)
}

func (r *Resolver) searchMembersCapped(t symbol.Symbol, name string, class kindClass, pkgs []string, st *lookupState) memberSet {
	set := r.searchMembersOf(t, name, class, pkgs, st)
	// A type whose declaration states a cap (its members cannot all be
	// listed: a parse error in its body) bounds what is found in it, and
	// what it does not show may be there.
	switch {
	case t.ConfidenceCap == "":
	case set.empty():
		set.open, set.unknown = true, true
	case t.ConfidenceCap == ConfidenceStrong.String():
		set.open = true
	default:
		set.ambig = true
	}
	return set
}

func (r *Resolver) searchMembersOf(t symbol.Symbol, name string, class kindClass, pkgs []string, st *lookupState) memberSet {
	if st.visiting[t.ID] || st.depth > maxStructuralDepth {
		return memberSet{open: true, tainted: true}
	}
	st.visiting[t.ID] = true
	defer delete(st.visiting, t.ID)

	var set memberSet
	inSuper := pkgs != nil
	declared := r.ownMembers(t, name, class)
	for _, d := range declared {
		if inSuper && d.Kind == symbol.KindMethod && d.Placement == symbol.PlacementStatic && t.Kind == symbol.KindInterface {
			continue // an interface's static method is not inherited (JLS 8.4.8)
		}
		if !inSuper || r.inherited(d, pkgs) {
			set.decls = append(set.decls, d)
		}
	}
	// Implicit members: the type's own ("+") beside its declarations, those
	// of its implicit supertypes outside the repository as theirs.
	var fromSupers memberSet
	openRel, foundRels := false, 0
	if class.callable() {
		for _, im := range r.implicitOf(t) {
			switch {
			case im.name != name:
			case im.own:
				set.implicit = append(set.implicit, im)
			case class == 'M':
				fromSupers.implicit = append(fromSupers.implicit, im)
			}
		}
	}
	if class == 'C' {
		return set // constructors are not inherited
	}
	if class != 'M' && (len(declared) > 0 || !set.empty()) {
		// A nearer field or type hides those of the supertypes — one that
		// is not inherited (private) as well (JLS 8.3, 8.5).
		return set
	}

	rels, tainted := r.supertypesOf(t, st)
	set.tainted = set.tainted || tainted
	sub := append(append([]string(nil), pkgs...), r.packageOf(t.Location.File))
	distinct := 0
	for _, rel := range rels {
		if len(rel.targets) == 0 {
			fromSupers.open = true
			fromSupers.unknown = fromSupers.unknown || rel.unknown
			openRel = true
			continue
		}
		var relSet memberSet
		for _, target := range rel.targets {
			st.depth++
			found := r.searchMembers(target, name, class, sub, st)
			st.depth--
			relSet.add(found)
		}
		if relSet.empty() && relSet.open {
			openRel = true
		} else if !relSet.empty() {
			foundRels++
		}
		if len(rel.targets) > 1 && !relSet.empty() {
			relSet.ambig = true
		}
		if rel.unknown && !relSet.empty() {
			relSet.ambig = true
		}
		if rel.capped && !relSet.empty() {
			relSet.open = true
		}
		if !relSet.empty() {
			distinct++
		}
		fromSupers.add(relSet)
	}
	if class != 'M' {
		// Inherited from several supertypes: ambiguous (the language rejects
		// a use of such a name).
		if distinct > 1 {
			fromSupers.ambig = true
		}
		return fromSupers
	}
	// A method one supertype has, while another supertype, outside the
	// repository, may have it as well: that one may be the member (a
	// superclass's method implementing an interface's) — never hidden
	// behind the one seen.
	if foundRels > 0 && openRel {
		fromSupers.ambig = true
	}
	// An own method stated to override a supertype's member, while no
	// supertype the repository declares has the name and one is outside it:
	// the outside one has it — with what parameters, and beside which
	// overloads, is not known.
	if fromSupers.open && fromSupers.empty() {
		for _, d := range set.decls {
			if d.Overrides {
				fromSupers.implicit = append(fromSupers.implicit, implicitMember{name: name, of: t.Qualified + "'s supertype"})
				break
			}
		}
	}
	// Methods: the supertypes' ones accumulate, but those an own one
	// overrides (equal Parameters).
	own := map[string]bool{}
	for _, d := range set.decls {
		if d.Parameters != "" {
			own[d.Parameters] = true
		}
	}
	for _, im := range set.implicit {
		if im.params != "" {
			own[im.params] = true
		}
	}
	for _, d := range fromSupers.decls {
		if d.Parameters != "" && own[d.Parameters] {
			set.presumed = set.presumed || strings.Contains(d.Parameters, "?")
			continue
		}
		set.decls = append(set.decls, d)
	}
	for _, im := range fromSupers.implicit {
		if im.params != "" && own[im.params] {
			set.presumed = set.presumed || strings.Contains(im.params, "?")
			continue
		}
		set.implicit = append(set.implicit, im)
	}
	set.open = set.open || fromSupers.open
	set.unknown = set.unknown || fromSupers.unknown
	set.ambig = set.ambig || fromSupers.ambig
	set.presumed = set.presumed || fromSupers.presumed
	set.tainted = set.tainted || fromSupers.tainted
	return set
}

// evalPathAlt evaluates an alternative followed by a member path: the
// alternative's declarations must be one type, whose members along the path
// are found in turn. The alternative counts as found once its own
// declarations are (the language has classified the head as a type): a
// member missing on the way is then no declaration, and a field or constant
// on the way a value whose members Ark does not follow.
func (r *Resolver) evalPathAlt(a lookupAlt, ref reference.Reference, st *lookupState) (memberSet, bool) {
	head := a
	head.path = nil
	var cur memberSet
	elsewhere := false
	switch head.op {
	case '=':
		decls, other := r.declsOf(ref.Language, head.q, head.class, st.from)
		if len(decls) == 0 {
			return memberSet{open: true}, false
		}
		elsewhere = other
		cur = memberSet{decls: decls, ambig: other && len(decls) > 1}
	case '@':
		owners, other := r.declsOf(ref.Language, head.q, 'T', st.from)
		if len(owners) == 0 {
			return memberSet{open: true}, false
		}
		cur.ambig = len(owners) > 1
		for _, o := range owners {
			found := r.searchMembers(o, head.name, head.class, nil, st)
			if other && !found.empty() {
				elsewhere = true
			}
			cur.add(found)
		}
	default:
		return memberSet{open: head.op == '?'}, false
	}
	if cur.empty() {
		return cur, elsewhere
	}
	for _, p := range a.path {
		var types []symbol.Symbol
		for _, d := range cur.decls {
			if isTypeLike(d.Kind) {
				types = append(types, d)
			}
		}
		if len(types) == 0 || len(types) != len(cur.decls) || len(cur.implicit) > 0 {
			// A value (its members are not followed) or nothing Ark
			// represents: found, unrepresented.
			return memberSet{implicit: []implicitMember{{name: "(unrepresented)", own: true}}}, elsewhere
		}
		next := memberSet{open: cur.open, ambig: cur.ambig || len(types) > 1, presumed: cur.presumed, tainted: cur.tainted}
		for _, t := range types {
			var found memberSet
			if p.op == '~' {
				found = r.searchMembers(t, p.name, 'V', nil, st)
			}
			if found.empty() {
				found = r.searchMembers(t, p.name, 'T', nil, st)
			}
			next.add(found)
		}
		if next.empty() {
			// The language took the head for a type: a member it lacks is
			// no declaration — nothing more to find; outside the
			// repository when a supertype outside may declare it.
			// One a type whose members cannot be listed does not show is
			// unknown, not outside.
			return memberSet{implicit: []implicitMember{{name: p.name, own: !next.open || next.unknown}}, unknown: next.unknown}, elsewhere
		}
		cur = next
	}
	return cur, elsewhere
}

// lookupOutcome is the evaluation of a Lookup.
type lookupOutcome struct {
	found   memberSet // the first step that found anything (empty: none did)
	strong  bool      // a passed step was open, an alternative found elsewhere than the reference's root, or an override presumed
	unknown bool      // a passed step searched a type whose members cannot be listed: at most a Candidate
	outside bool      // nothing found, and a passed step may hold the name outside the repository
	detail  string
}

// evalLookup evaluates Lookup text for ref (its language; its argument
// count applies to the step found).
func (r *Resolver) evalLookup(text string, ref reference.Reference, st *lookupState) lookupOutcome {
	steps, ok := parseLookup(text)
	if !ok {
		return lookupOutcome{detail: "malformed lookup"}
	}
	var out lookupOutcome
	passedOpen, passedUnknown := false, false
	for i, alts := range steps {
		var step memberSet
		elsewhere := false
		for _, a := range alts {
			if len(a.path) > 0 {
				found, other := r.evalPathAlt(a, ref, st)
				elsewhere = elsewhere || other
				step.add(found)
				continue
			}
			switch a.op {
			case '?':
				step.open = true
			case '!':
				step.implicit = append(step.implicit, implicitMember{name: "(unrepresented)", own: true})
			case '=':
				decls, other := r.declsOf(ref.Language, a.q, a.class, st.from)
				if len(decls) == 0 {
					step.open = true
					continue
				}
				elsewhere = elsewhere || other
				if other && len(decls) > 1 {
					step.ambig = true
				}
				step.add(memberSet{decls: decls})
			case '@':
				owners, other := r.declsOf(ref.Language, a.q, 'T', st.from)
				if len(owners) == 0 {
					step.open = true
					continue
				}
				if len(owners) > 1 {
					step.ambig = true
				}
				for _, o := range owners {
					found := r.searchMembers(o, a.name, a.class, nil, st)
					if other && !found.empty() {
						elsewhere = true
					}
					step.add(found)
				}
			}
		}
		if step.empty() {
			passedOpen = passedOpen || step.open
			passedUnknown = passedUnknown || step.unknown
			step.open = false
			out.found.tainted = out.found.tainted || step.tainted
			continue
		}
		step.tainted = step.tainted || out.found.tainted
		r.filterAccess(&step, ref)
		step.applyArity(ref)
		out.found = step
		out.strong = passedOpen || step.open || step.presumed || elsewhere
		out.unknown = passedUnknown || step.unknown
		out.detail = fmt.Sprintf("step %d of %d of the language's lookup", i+1, len(steps))
		return out
	}
	out.outside = passedOpen && !passedUnknown
	out.unknown = passedUnknown
	out.detail = fmt.Sprintf("none of the %d steps of the language's lookup", len(steps))
	return out
}

// memberClassOf is the kind class of the member a reference with a receiver
// lookup names.
func memberClassOf(ref reference.Reference) kindClass {
	switch {
	case ref.Name == "<init>":
		return 'C'
	case ref.Kind == reference.KindCall:
		return 'M'
	case ref.Kind == reference.KindTypeUse, ref.Kind == reference.KindConstruction,
		ref.Kind == reference.KindInheritance, ref.Kind == reference.KindImplements:
		return 'T'
	}
	return 'V'
}

// resolveLanguageLookup resolves a reference carrying a Lookup or a
// ReceiverLookup. It never falls back to another rule.
func (r *Resolver) resolveLanguageLookup(res Resolution, ref reference.Reference) Resolution {
	st := &lookupState{from: ref.Location.File, visiting: map[symbol.SymbolID]bool{}}
	var o lookupOutcome
	detail := ""
	if ref.ReceiverLookup != "" {
		recv := r.evalLookup(ref.ReceiverLookup, reference.Reference{Language: ref.Language}, st)
		var types []symbol.Symbol
		values := false
		for _, d := range recv.found.decls {
			if isTypeLike(d.Kind) {
				types = append(types, d)
			} else {
				values = true
			}
		}
		switch {
		case recv.found.empty():
			res.Evidence = []ResolutionEvidence{{Kind: EvidenceLanguageLookup, Detail: fmt.Sprintf("receiver %q: %s finds its type", ref.ReceiverExpr, recv.detail)}}
			res.OutsideRepository = recv.outside && !ref.IdentityInRepository
			return res
		case values || len(types) == 0:
			res.Evidence = []ResolutionEvidence{{Kind: EvidenceLanguageLookup, Detail: fmt.Sprintf("receiver %q is a value whose type is not known", ref.ReceiverExpr)}}
			return res
		}
		class := memberClassOf(ref)
		var set memberSet
		for _, t := range types {
			set.add(r.searchMembers(t, ref.Name, class, nil, st))
		}
		if len(types) > 1 || recv.found.ambig {
			set.ambig = true
		}
		r.filterAccess(&set, ref)
		set.applyArity(ref)
		o = lookupOutcome{found: set, strong: recv.strong || set.open || set.presumed, unknown: recv.unknown || set.unknown}
		if set.empty() {
			o.outside = set.open && !set.unknown
		}
		detail = fmt.Sprintf("member %q of the type %s finds (receiver %q)", ref.Name, types[0].Qualified, ref.ReceiverExpr)
	} else {
		o = r.evalLookup(ref.Lookup, ref, st)
		detail = o.detail
	}

	found := o.found
	switch {
	case found.empty():
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceLanguageLookup, Detail: detail + ": no declaration"}}
		res.OutsideRepository = o.outside && !ref.IdentityInRepository
		return res
	case len(found.decls) == 0:
		// Only members no declaration states: the language's, not the
		// repository's symbols.
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceLanguageLookup, Detail: detail + fmt.Sprintf(": %s, which no declaration states", implicitNames(found.implicit))}}
		outside := true
		for _, im := range found.implicit {
			outside = outside && !im.own
		}
		res.OutsideRepository = outside && !ref.IdentityInRepository
		return res
	}
	conf := ConfidenceExact
	if o.strong {
		conf = ConfidenceStrong
	}
	if found.ambig || len(found.implicit) > 0 || o.unknown {
		conf = ConfidenceCandidate
	}
	detail += fmt.Sprintf(": %s", declsDetail(found.decls))
	if len(found.implicit) > 0 {
		detail += fmt.Sprintf(" beside %s, which no declaration states", implicitNames(found.implicit))
	}
	return r.pickBest(res, found.decls, conf, EvidenceLanguageLookup, detail)
}

func implicitNames(ims []implicitMember) string {
	var parts []string
	for _, im := range ims {
		parts = append(parts, im.of+"."+im.name+im.params)
	}
	return strings.Join(parts, ", ")
}

func declsDetail(decls []symbol.Symbol) string {
	if len(decls) == 1 {
		return fmt.Sprintf("%s declared at %s", decls[0].Qualified, filepath.ToSlash(string(decls[0].Location.File)))
	}
	return fmt.Sprintf("%d declarations", len(decls))
}
