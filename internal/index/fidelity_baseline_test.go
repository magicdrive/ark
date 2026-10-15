package index_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestFidelityBaseline measures Ark's reference resolution over a corpus, for
// every language or the ones named — opt-in, not in CI
// (docs/scripts/fidelity-baseline.sh; docs/research/multilang-fidelity/).
//
//	ARK_FIDELITY_ROOT    the corpus directory (required; the test skips without it)
//	ARK_FIDELITY_LANG    languages to measure, comma-separated (default: all)
//	ARK_FIDELITY_JSON    where to write the counts (JSON)
//	ARK_FIDELITY_SITES   where to write one line per reference: two runs over
//	                     one corpus compare reference by reference (Series 1)
//	ARK_FIDELITY_ORACLE  an oracle's sites (JSON Lines, oracleSite) to score
//	                     Ark against; without it only Ark's own distribution
//	                     is reported, which says nothing about correctness
//	ARK_FIDELITY_FALSE   where to write every false Strong or Exact the oracle
//	                     shows, one line each, for triage
//	ARK_FIDELITY_SCORED  where to write every oracle site — key (the oracle's
//	                     own site), class, "representable" (":member" for a
//	                     member's declaration) or "-", Ark's
//	                     verdicts: two runs join on the key to form Series 1
//	                     (sites representable in both) and Series 2 (the
//	                     rest), whatever spans Ark's references have in either
//
// It runs the providers and the resolver as the index does (index.NewFileIndex,
// resolver.NewInRoot) and changes nothing.
func TestFidelityBaseline(t *testing.T) {
	root := os.Getenv("ARK_FIDELITY_ROOT")
	if root == "" {
		t.Skip("ARK_FIDELITY_ROOT not set")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	only := map[string]bool{}
	for _, l := range strings.Split(os.Getenv("ARK_FIDELITY_LANG"), ",") {
		if l = strings.TrimSpace(l); l != "" {
			only[l] = true
		}
	}

	m, err := measureCorpus(root, only)
	if err != nil {
		t.Fatal(err)
	}
	report, files, byEnd, byStart, sites := m.report, m.files, m.byEnd, m.byStart, m.sites

	if path := os.Getenv("ARK_FIDELITY_ORACLE"); path != "" {
		var falses, perSite []string
		report.Oracle, falses, perSite, err = scoreAgainstOracle(path, files, byEnd, byStart)
		if err != nil {
			t.Fatal(err)
		}
		if out := os.Getenv("ARK_FIDELITY_SCORED"); out != "" {
			sort.Strings(perSite)
			if err := os.WriteFile(out, []byte(strings.Join(perSite, "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if out := os.Getenv("ARK_FIDELITY_FALSE"); out != "" {
			sort.Strings(falses)
			if err := os.WriteFile(out, []byte(strings.Join(falses, "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	langs := make([]string, 0, len(report.Languages))
	for l := range report.Languages {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	for _, l := range langs {
		c := report.Languages[l]
		t.Logf("%-10s files=%d symbols=%d refs=%d speculative_dropped=%d confidence=%v outside=%d dynamic=%d import_strong=%d",
			l, c.Files, c.Symbols, c.References, c.SpeculativeDropped, c.ByConfidence, c.Outside, c.Dynamic, c.ImportStrong)
	}
	t.Logf("extract=%dms resolve=%dms", report.ExtractMillis, report.ResolveMillis)
	if o := report.Oracle; o != nil {
		data, _ := json.Marshal(o)
		t.Logf("ORACLE %s", data)
	}
	if path := os.Getenv("ARK_FIDELITY_JSON"); path != "" {
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if path := os.Getenv("ARK_FIDELITY_SITES"); path != "" {
		if err := os.WriteFile(path, []byte(strings.Join(sites, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// fidelityMeasure is Ark's side of one corpus: the counts, the files, every
// reference by the end and the start of its span, and one line per reference.
type fidelityMeasure struct {
	report         *fidelityReport
	files          []resolver.FileIndex
	byEnd, byStart map[siteKey][]scoredRef
	sites          []string
}

// measureCorpus runs the providers and the resolver over root as the index
// does, for the languages in only (all when empty).
func measureCorpus(root string, only map[string]bool) (*fidelityMeasure, error) {
	files, extractTime, err := fidelityFiles(root, only)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resolutions := resolver.NewInRoot(files, filepath.Base(root)).Resolve()
	resolveTime := time.Since(start)

	refs := map[reference.ReferenceID]fidelityRef{}
	for i := range files {
		fi := &files[i]
		for j := range fi.References {
			// The first reference of an ID, as index.uniqueReferences keeps.
			if _, dup := refs[fi.References[j].ID]; !dup {
				refs[fi.References[j].ID] = fidelityRef{fi: fi, ref: &fi.References[j]}
			}
		}
	}
	report := &fidelityReport{Root: root, ExtractMillis: extractTime.Milliseconds(), ResolveMillis: resolveTime.Milliseconds(), Languages: map[string]*fidelityCounts{}}
	counts := func(lang string) *fidelityCounts {
		c := report.Languages[lang]
		if c == nil {
			c = &fidelityCounts{ByKind: map[string]int{}, ByConfidence: map[string]int{}, ByRule: map[string]int{}}
			report.Languages[lang] = c
		}
		return c
	}
	for i := range files {
		c := counts(files[i].Language)
		c.Files++
		c.Symbols += len(files[i].Symbols)
	}
	var sites []string
	byEnd, byStart := map[siteKey][]scoredRef{}, map[siteKey][]scoredRef{}
	done := map[reference.ReferenceID]bool{}
	for _, res := range resolutions {
		fr, ok := refs[res.ReferenceID]
		if !ok || done[res.ReferenceID] {
			continue
		}
		done[res.ReferenceID] = true
		r := fr.ref
		c := counts(fr.fi.Language)
		if r.Speculative && len(res.Candidates) == 0 {
			// The index drops such a reference (index/builder.go).
			c.SpeculativeDropped++
			continue
		}
		c.References++
		c.ByKind[string(r.Kind)]++
		c.ByConfidence[res.Confidence.String()]++
		if res.OutsideRepository {
			c.Outside++
		}
		if r.Dynamic {
			c.Dynamic++
		}
		rule := "none"
		if len(res.Evidence) > 0 {
			rule = string(res.Evidence[0].Kind)
		}
		c.ByRule[rule]++
		if r.Kind == reference.KindImport && res.HasUniqueTarget() {
			c.ImportStrong++
		}
		target := "-"
		if res.HasUniqueTarget() {
			cand := res.Candidates[0]
			target = fmt.Sprintf("%s:%s", cand.File, cand.Qualified)
		}
		rg := r.Location.Range
		sites = append(sites, fmt.Sprintf("%s\t%s:%d:%d-%d:%d\t%s\t%s\t%s\t%t\t%s\t%s",
			fr.fi.Language, r.Location.File, rg.Start.Line, rg.Start.Column, rg.End.Line, rg.End.Column,
			r.Kind, r.Name, res.Confidence, res.OutsideRepository, rule, target))
		sr := scoredRef{lang: fr.fi.Language, ref: r, res: res}
		end := siteKey{string(r.Location.File), rg.End.Line, rg.End.Column, r.Name}
		begin := siteKey{string(r.Location.File), rg.Start.Line, rg.Start.Column, r.Name}
		byEnd[end] = append(byEnd[end], sr)
		byStart[begin] = append(byStart[begin], sr)
	}
	sort.Strings(sites)
	return &fidelityMeasure{report, files, byEnd, byStart, sites}, nil
}

type fidelityRef struct {
	fi  *resolver.FileIndex
	ref *reference.Reference
}

type fidelityReport struct {
	Root          string                     `json:"root"`
	ExtractMillis int64                      `json:"extract_ms"`
	ResolveMillis int64                      `json:"resolve_ms"`
	Languages     map[string]*fidelityCounts `json:"languages"`
	Oracle        *oracleScore               `json:"oracle,omitempty"`
}

type fidelityCounts struct {
	Files              int            `json:"files"`
	Symbols            int            `json:"symbols"`
	References         int            `json:"references"`
	SpeculativeDropped int            `json:"speculative_dropped"`
	ByKind             map[string]int `json:"by_kind"`
	ByConfidence       map[string]int `json:"by_confidence"`
	ByRule             map[string]int `json:"by_first_evidence"`
	Outside            int            `json:"outside_repository"`
	Dynamic            int            `json:"dynamic"`
	// ImportStrong: import references with a Strong or Exact target. No
	// oracle lists an import statement as a site, so these are not scored;
	// a language whose imports name modules should have none to a symbol.
	ImportStrong int `json:"import_strong"`
}

// fidelityFiles extracts every file of root a provider of the registry
// handles (of the languages in only, when given), as the index does.
func fidelityFiles(root string, only map[string]bool) ([]resolver.FileIndex, time.Duration, error) {
	reg := languages.Registry()
	var files []resolver.FileIndex
	var spent time.Duration
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && index.SkipDirName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		desc, ok := reg.DetectByExtension(strings.ToLower(filepath.Ext(path)))
		if !ok || (len(only) > 0 && !only[string(desc.Language)]) {
			return nil
		}
		prov, ok := reg.Provider(desc.Language)
		if !ok {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		fid := source.FileID(filepath.ToSlash(rel))
		start := time.Now()
		ex, err := prov.Extract(context.Background(), fid, src)
		spent += time.Since(start)
		if err != nil {
			return nil
		}
		files = append(files, index.NewFileIndex(prov, fid, ex))
		return nil
	})
	return files, spent, err
}

// --- oracle -------------------------------------------------------------------

// oracleSite is one reference an independent analyzer (the oracle) bound, as
// one JSON line. Positions are 1-based; columns count bytes (an adapter
// converts its analyzer's UTF-16 or code point columns). Start and end are the
// referring identifier's own span, the end one past its last byte as in Ark's
// ranges. A site matches the Ark references of its name ending where it does;
// a site with none, those starting where it does, or else the innermost of its
// name whose span holds it (a reference spanning a whole expression) — never
// a reference another site already matched. Class says what the oracle knows:
//
//	declaration       Decl is the one declaration the identifier denotes
//	ambiguous         Decls are the declarations it may denote (a name assigned
//	                  in several places, a conditional definition)
//	external          a declaration outside the corpus (a dependency, the runtime)
//	declaration_file  only a declaration file of the corpus (.d.ts, .pyi) types
//	                  it: what the code assigns at run time is not decided
//	module            the name denotes a module, which Ark does not model
//	dynamic           the language decides at run time; no static answer exists
//	unknown           the oracle could not decide
//
// "declaration", "ambiguous" and "external" are ground truth (for ambiguous: a
// target among none of the Decls is false). The others are counted apart,
// never as Ark's success or error. A declaration's Name is its own (the
// declaration an alias import h2 of helper denotes is named helper); without
// it the site's name is used. Via are the declarations an alias passes
// through (an import binding, const x = require(...)): a target there stops
// short of the declaration — neither found nor false.
type oracleSite struct {
	Lang      string     `json:"lang"`
	File      string     `json:"file"`
	Name      string     `json:"name"`
	StartLine uint32     `json:"start_line"`
	StartCol  uint32     `json:"start_col"`
	EndLine   uint32     `json:"end_line"`
	EndCol    uint32     `json:"end_col"`
	Class     string     `json:"class"`
	Decl      *declSite  `json:"decl,omitempty"`
	Decls     []declSite `json:"decls,omitempty"`
	Via       []declSite `json:"via,omitempty"`
	// Member: the site is the name of a member access (a.b: b), as the
	// oracle sees it — member recall's denominator independent of Ark.
	Member bool `json:"member,omitempty"`
	// Value, ValueVia: evaluation contract v2's semantic value target
	// (docs/scripts/oracle/javascript-tsc-value.js). Decl is then an object
	// literal's property and Value the declaration of the value it holds
	// ({ parse } and { k: parse }: parse's); a target at either is correct,
	// and ValueVia are the declarations the value's aliases pass through.
	// A v1 oracle states neither, and is scored as before.
	// ValueKind is "function", "class" or "data": the value_tp of data is
	// counted apart (value_tp_data), a value a closed world fixes.
	Value     *declSite  `json:"value,omitempty"`
	ValueVia  []declSite `json:"value_via,omitempty"`
	ValueKind string     `json:"value_kind,omitempty"`
	// ExportUnsafe: the site goes through a CommonJS export the module or
	// the program may write another value to (contract v2): the oracle
	// cannot say which value it is, so a Strong or Exact target there is
	// counted apart (unsafe_strong), neither true nor false.
	ExportUnsafe bool `json:"export_unsafe,omitempty"`
}

type declSite struct {
	File string `json:"file"`
	Line uint32 `json:"line"`
	Col  uint32 `json:"col"`
	Name string `json:"name,omitempty"`
}

type siteKey struct {
	file      string
	line, col uint32
	name      string
}

type scoredRef struct {
	lang string
	ref  *reference.Reference
	res  resolver.Resolution
}

// oracleScore is the common (Level 1) score of one corpus. The denominator
// is the oracle's representable sites: those whose declaration is an Ark
// symbol. One Ark states no reference for is an extraction miss; a site whose
// declaration Ark has no symbol for (a local, a parameter) is no part of the
// denominator, but a Strong or Exact target Ark gives it is false.
type oracleScore struct {
	Sites         int `json:"sites"`
	Declaration   int `json:"declaration"`
	Ambiguous     int `json:"ambiguous"`
	External      int `json:"external"`
	DeclFile      int `json:"declaration_file"`
	Module        int `json:"module"`
	Dynamic       int `json:"dynamic"`
	Unknown       int `json:"unknown"`
	Representable int `json:"representable"` // declaration sites whose declaration is an Ark symbol
	// MemberRepresentable / MemberRecovered: the representable sites whose
	// declaration is a member (a symbol with a receiver: a method, a field),
	// and those of them recovered — member recall's own denominator.
	MemberRepresentable int `json:"member_representable"`
	MemberRecovered     int `json:"member_recovered"`
	// OracleMembers / OracleMembersRecovered: the declaration sites the
	// oracle marks as member accesses (oracleSite.Member), whatever Ark
	// represents, and those Ark recovered.
	OracleMembers          int `json:"oracle_members"`
	OracleMembersRecovered int `json:"oracle_members_recovered"`
	NotExtracted           int `json:"not_extracted"`  // ... Ark states no reference for (an extraction miss)
	Recovered              int `json:"recovered"`      // ... Ark resolved to that symbol (unique Strong or Exact)
	ExactTP                int `json:"exact_tp"`       //
	ExactFP                int `json:"exact_fp"`       // false Exact: Ark Exact, another target
	StrongTP               int `json:"strong_tp"`      //
	StrongFP               int `json:"strong_fp"`      // false Strong
	ViaStop                int `json:"via_stop"`       // Ark Strong or Exact to a declaration the alias passes through
	CandHit                int `json:"candidate_hit"`  // Ark Candidate, target among the candidates
	CandMiss               int `json:"candidate_miss"` //
	Unresolved             int `json:"unresolved"`     // Ark Unresolved on a representable site
	FalseOutside           int `json:"false_outside"`  // Ark OutsideRepository, target an Ark symbol
	LocalOutside           int `json:"local_outside"`  // Ark OutsideRepository, target in the corpus but no Ark symbol
	LocalFP                int `json:"local_fp"`       // Ark Strong or Exact where the declaration is no Ark symbol (a local, a parameter)
	ExternalFP             int `json:"external_fp"`    // Ark Strong or Exact to a corpus symbol, oracle: external
	AmbiguousFP            int `json:"ambiguous_fp"`   // Ark Strong or Exact to none of an ambiguous site's declarations
	UnknownStrong          int `json:"unknown_strong"` // Ark Strong or Exact where the oracle says dynamic or unknown
	// DeclFileStrong: Ark Strong or Exact where only a declaration file types
	// the site — reviewed by hand, not counted either way.
	DeclFileStrong int `json:"declaration_file_strong"`
	// UnmatchedStrong: Ark Strong or Exact references (imports aside, which
	// no oracle lists as sites) no oracle site matches.
	UnmatchedStrong int `json:"unmatched_strong"`
	// Contract v2 (oracleSite.Value), zero and left out for a v1 oracle:
	// the declaration sites with a value target, those representable only
	// through it (the property is no Ark symbol), and the Strong or Exact
	// true positives whose target is the value rather than the property.
	ValueSites         int `json:"value_sites,omitempty"`
	ValueRepresentable int `json:"value_only_representable,omitempty"`
	ValueTP            int `json:"value_tp,omitempty"`
	ValueTPData        int `json:"value_tp_data,omitempty"`
	UnsafeSites        int `json:"unsafe_sites,omitempty"`
	UnsafeStrong       int `json:"unsafe_strong,omitempty"`
}

// scoreAgainstOracle scores Ark's references (byEnd, byStart: by the end
// and the start of their spans) against the oracle's sites at path. It
// returns the score, every false Strong or Exact (for triage) and every site
// with Ark's verdict (for Series 1 and 2).
func scoreAgainstOracle(path string, files []resolver.FileIndex, byEnd, byStart map[siteKey][]scoredRef) (*oracleScore, []string, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, err
	}
	var all []oracleSite
	seenLine := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seenLine[line] { // a duplicated line is one site
			continue
		}
		seenLine[line] = true
		var o oracleSite
		if err := json.Unmarshal([]byte(line), &o); err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %v: %s", path, err, line)
		}
		switch o.Class {
		case "declaration", "ambiguous", "external", "declaration_file", "module", "dynamic", "unknown":
		default:
			return nil, nil, nil, fmt.Errorf("%s: unknown class %q: %s", path, o.Class, line)
		}
		all = append(all, o)
	}
	syms := map[string][]symbol.Symbol{}
	for i := range files {
		syms[string(files[i].FileID)] = files[i].Symbols
	}
	byName := map[string][]scoredRef{}
	for _, refs := range byEnd {
		for _, a := range refs {
			key := string(a.ref.Location.File) + "\x00" + a.ref.Name
			byName[key] = append(byName[key], a)
		}
	}

	// An Ark reference is one site's: the site it ends with first (a.f()'s
	// call is f's, not a's, though it starts with a), then by start or
	// enclosure among the references no site claimed.
	matched := map[reference.ReferenceID]bool{}
	matches := make([][]scoredRef, len(all))
	for i, o := range all {
		matches[i] = byEnd[siteKey{o.File, o.EndLine, o.EndCol, o.Name}]
		for _, a := range matches[i] {
			matched[a.ref.ID] = true
		}
	}
	unclaimed := func(refs []scoredRef) []scoredRef {
		var out []scoredRef
		for _, a := range refs {
			if !matched[a.ref.ID] {
				out = append(out, a)
			}
		}
		return out
	}
	for i, o := range all {
		if len(matches[i]) > 0 {
			continue
		}
		ark := unclaimed(byStart[siteKey{o.File, o.StartLine, o.StartCol, o.Name}])
		if len(ark) == 0 {
			ark = enclosing(unclaimed(byName[o.File+"\x00"+o.Name]), o)
		}
		for _, a := range ark {
			matched[a.ref.ID] = true
		}
		matches[i] = ark
	}

	s := &oracleScore{}
	var falses, perSite []string
	for i, o := range all {
		before, beforeMember := s.Representable, s.MemberRepresentable
		verdicts, f := scoreSite(s, o, matches[i], syms)
		falses = append(falses, f...)
		rep := "-"
		switch {
		case s.MemberRepresentable > beforeMember:
			rep = "representable:member"
		case s.Representable > before:
			rep = "representable"
		}
		perSite = append(perSite, fmt.Sprintf("%s:%d:%d:%s\t%s\t%s\t%s", o.File, o.StartLine, o.StartCol, o.Name, o.Class, rep, strings.Join(verdicts, ",")))
	}
	// Ark's Strong and Exact references but imports no oracle site matches:
	// what the score says nothing about (an identifier the oracle does not
	// list, a span the matching rule misses).
	for _, refs := range byEnd {
		for _, a := range refs {
			if !matched[a.ref.ID] && a.res.HasUniqueTarget() && a.ref.Kind != reference.KindImport {
				s.UnmatchedStrong++
			}
		}
	}
	return s, falses, perSite, nil
}

// scoreSite adds one oracle site, matched to the Ark references ark, to s. It
// returns Ark's verdicts on the site (one per reference, "not_extracted" or
// "-" without one) and a line for each false Strong or Exact.
func scoreSite(s *oracleScore, o oracleSite, ark []scoredRef, syms map[string][]symbol.Symbol) (verdicts, falses []string) {
	s.Sites++
	strong := func(a scoredRef) bool { return a.res.HasUniqueTarget() }
	falseSite := func(what string, a scoredRef) {
		decl := "-"
		if o.Decl != nil {
			decl = fmt.Sprintf("%s:%d:%d", o.Decl.File, o.Decl.Line, o.Decl.Col)
		}
		if o.Value != nil {
			decl += fmt.Sprintf(" value=%s:%d:%d", o.Value.File, o.Value.Line, o.Value.Col)
		}
		rule := ""
		if len(a.res.Evidence) > 0 {
			rule = string(a.res.Evidence[0].Kind)
		}
		c := a.res.Candidates[0]
		falses = append(falses, fmt.Sprintf("%s\t%s:%d:%d\t%s\t%s\t%s\t%s:%s\toracle=%s", what, o.File, o.StartLine, o.StartCol,
			o.Name, a.res.Confidence, rule, c.File, c.Qualified, decl))
		verdicts = append(verdicts, what+"_fp")
	}
	// apart counts the Strong or Exact references of a site the oracle cannot
	// judge; every other reference's verdict is its confidence.
	apart := func(n *int) {
		for _, a := range ark {
			if strong(a) {
				*n++
			}
			verdicts = append(verdicts, a.res.Confidence.String())
		}
	}
	via := map[symbol.SymbolID]bool{}
	for i := range o.Via {
		if id := declSymbol(syms, &o.Via[i], ""); id != "" {
			via[id] = true
		}
	}

	switch o.Class {
	case "dynamic":
		s.Dynamic++
		apart(&s.UnknownStrong)
	case "unknown":
		s.Unknown++
		apart(&s.UnknownStrong)
	case "module":
		s.Module++
		apart(&s.UnknownStrong)
	case "declaration_file":
		s.DeclFile++
		apart(&s.DeclFileStrong)
	case "external":
		s.External++
		for _, a := range ark {
			if strong(a) {
				s.ExternalFP++
				falseSite("external", a)
			} else {
				verdicts = append(verdicts, a.res.Confidence.String())
			}
		}
	case "ambiguous":
		s.Ambiguous++
		wants := map[symbol.SymbolID]bool{}
		for i := range o.Decls {
			if id := declSymbol(syms, &o.Decls[i], o.Name); id != "" {
				wants[id] = true
			}
		}
		for _, a := range ark {
			switch {
			case strong(a) && via[a.res.Candidates[0].SymbolID]:
				s.ViaStop++
				verdicts = append(verdicts, "via")
			case strong(a) && !wants[a.res.Candidates[0].SymbolID]:
				s.AmbiguousFP++
				falseSite("ambiguous", a)
			default:
				verdicts = append(verdicts, a.res.Confidence.String())
			}
		}
	case "declaration":
		s.Declaration++
		if o.Member {
			s.OracleMembers++
		}
		if o.ExportUnsafe {
			s.UnsafeSites++
			apart(&s.UnsafeStrong)
			break
		}
		want := declSymbol(syms, o.Decl, o.Name)
		// value: contract v2's value target, "" without one (v1) or when
		// Ark has no symbol for it.
		// A value without a name (a module) is no declaration Ark could
		// name; ValueVia stop short only when the value counts.
		var value symbol.SymbolID
		if o.Value != nil {
			s.ValueSites++
			if o.Value.Name != "" {
				value = declSymbol(syms, o.Value, "")
			}
			if value == want {
				value = ""
			}
		}
		if value != "" {
			for i := range o.ValueVia {
				if id := declSymbol(syms, &o.ValueVia[i], ""); id != "" && o.ValueVia[i].Name != "" {
					via[id] = true
				}
			}
		}
		if want == "" && value == "" {
			// The name denotes a declaration Ark has no symbol for — a
			// parameter, a local shadowing a repository name: any Strong or
			// Exact target Ark gives it is false, and so is Outside.
			for _, a := range ark {
				switch {
				case strong(a) && via[a.res.Candidates[0].SymbolID]:
					s.ViaStop++
					verdicts = append(verdicts, "via")
				case strong(a):
					s.LocalFP++
					falseSite("local", a)
				case a.res.OutsideRepository:
					s.LocalOutside++
					verdicts = append(verdicts, "local_outside")
				default:
					verdicts = append(verdicts, a.res.Confidence.String())
				}
			}
			break
		}
		s.Representable++
		if want == "" {
			s.ValueRepresentable++
		}
		member := want != "" && isMemberSymbol(syms, o.Decl.File, want) ||
			want == "" && isMemberSymbol(syms, o.Value.File, value)
		if member {
			s.MemberRepresentable++
		}
		if len(ark) == 0 {
			s.NotExtracted++
			verdicts = append(verdicts, "not_extracted")
			break
		}
		// Several Ark references at one site (a call and a read of one
		// identifier): the site is recovered when one of them resolves to the
		// declaration; every Strong or Exact one to another target is false.
		recovered := false
		for _, a := range ark {
			res := a.res
			switch {
			case strong(a) && (res.Candidates[0].SymbolID == want || value != "" && res.Candidates[0].SymbolID == value):
				if res.Confidence == resolver.ConfidenceExact {
					s.ExactTP++
				} else {
					s.StrongTP++
				}
				recovered = true
				if res.Candidates[0].SymbolID == want {
					verdicts = append(verdicts, "tp")
				} else {
					s.ValueTP++
					if o.ValueKind == "data" {
						s.ValueTPData++
					}
					verdicts = append(verdicts, "tp_value")
				}
			case strong(a) && via[res.Candidates[0].SymbolID]:
				s.ViaStop++
				verdicts = append(verdicts, "via")
			case strong(a) && res.Confidence == resolver.ConfidenceExact:
				s.ExactFP++
				falseSite("exact", a)
			case strong(a):
				s.StrongFP++
				falseSite("strong", a)
			case res.Confidence == resolver.ConfidenceCandidate:
				hit := false
				for _, c := range res.Candidates {
					hit = hit || c.SymbolID == want || value != "" && c.SymbolID == value
				}
				if hit {
					s.CandHit++
					verdicts = append(verdicts, "candidate_hit")
				} else {
					s.CandMiss++
					verdicts = append(verdicts, "candidate_miss")
				}
			default:
				s.Unresolved++
				if res.OutsideRepository {
					s.FalseOutside++
					verdicts = append(verdicts, "false_outside")
				} else {
					verdicts = append(verdicts, "unresolved")
				}
			}
		}
		if recovered {
			s.Recovered++
			if member {
				s.MemberRecovered++
			}
			if o.Member {
				s.OracleMembersRecovered++
			}
		}
	}
	if len(verdicts) == 0 {
		verdicts = []string{"-"}
	}
	return verdicts, falses
}

// isMemberSymbol reports whether the symbol id of file is a member (it has a
// receiver).
func isMemberSymbol(syms map[string][]symbol.Symbol, file string, id symbol.SymbolID) bool {
	for _, s := range syms[file] {
		if s.ID == id {
			return s.Receiver != ""
		}
	}
	return false
}

// enclosing returns the innermost of refs whose span holds the site's (an
// Ark reference spanning a whole expression: new C(...)), or none.
func enclosing(refs []scoredRef, o oracleSite) []scoredRef {
	var out []scoredRef
	size := func(rg source.Range) [2]uint32 {
		return [2]uint32{rg.End.Line - rg.Start.Line, rg.End.Column - rg.Start.Column}
	}
	for _, a := range refs {
		rg := a.ref.Location.Range
		if !within(rg, o.StartLine, o.StartCol) || !within(rg, o.EndLine, o.EndCol) {
			continue
		}
		if len(out) > 0 {
			best, this := size(out[0].ref.Location.Range), size(rg)
			if this[0] > best[0] || (this[0] == best[0] && this[1] > best[1]) {
				continue
			}
			if this != best {
				out = out[:0]
			}
		}
		out = append(out, a)
	}
	return out
}

// declSymbol is the Ark symbol declared at d: the innermost symbol of d's file
// named as the declaration (d.Name, else name; any name when both are empty)
// that starts on d's line and whose range holds d's position — a local
// declared inside a function of its name is not that function. "" when Ark
// has none (a local, a parameter, a declaration Ark does not model, one whose
// symbol starts on another line — a decorator line Ark counts in its range):
// such a site is not representable. A parameter on its function's first line
// named as the function still maps to the function (a known limit: symbols
// carry no span of their name).
func declSymbol(syms map[string][]symbol.Symbol, d *declSite, name string) symbol.SymbolID {
	if d == nil {
		return ""
	}
	if d.Name != "" {
		name = d.Name
	}
	var best symbol.Symbol
	found := false
	for _, s := range syms[d.File] {
		rg := s.Location.Range
		if (name != "" && s.Name != name) || rg.Start.Line != d.Line || !within(rg, d.Line, d.Col) {
			continue
		}
		if !found || within(best.Location.Range, rg.Start.Line, rg.Start.Column) && within(best.Location.Range, rg.End.Line, rg.End.Column) {
			best, found = s, true
		}
	}
	if !found {
		return ""
	}
	return best.ID
}

func within(rg source.Range, line, col uint32) bool {
	after := line > rg.Start.Line || (line == rg.Start.Line && col >= rg.Start.Column)
	before := line < rg.End.Line || (line == rg.End.Line && col <= rg.End.Column)
	return after && before
}
