// Package golden provides deterministic, human-readable serializers for the
// outputs of Ark's extraction and indexing pipeline. They are used by the
// baseline regression suite (Phase 0 of the Multi-Language Expansion plan) to
// freeze existing behaviour for Go/TypeScript/TSX/JavaScript/Python before new
// language providers are added.
//
// Every serializer MUST produce byte-for-byte identical output for identical
// inputs. All iteration over maps is replaced by explicit sorting so that the
// snapshots never depend on Go map iteration order.
package golden

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// ExtractionSnapshot serializes a single-file extraction result. It covers the
// symbols / references / imports / diagnostics required by the Phase 0 golden
// set. Output is stable regardless of the provider's internal ordering.
func ExtractionSnapshot(fileID source.FileID, ext language.Extraction) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# file: %s\n", fileID)

	syms := slices.Clone(ext.Symbols)
	sort.Slice(syms, func(i, j int) bool { return symbolDraftLess(syms[i], syms[j]) })
	b.WriteString("## symbols\n")
	for _, s := range syms {
		fmt.Fprintf(&b, "symbol %s kind=%s qualified=%q parent=%q receiver=%q exported=%t%s %s\n",
			s.Name, s.Kind, s.Qualified, s.Parent, s.Receiver, s.Exported, memberScope(s.MemberScope, s.MembersOutside)+parameterScope(s.ParameterScope), loc(s.Location))
	}

	refs := slices.Clone(ext.References)
	sort.Slice(refs, func(i, j int) bool { return refDraftLess(refs[i], refs[j]) })
	b.WriteString("## references\n")
	for _, r := range refs {
		fmt.Fprintf(&b, "ref %s kind=%s container=%q receiver=%q%s%s call=%t %s\n",
			r.Name, r.Kind, r.Container, r.ReceiverExpr, receiverType(r.ReceiverType), qualifiedIdentity(r.NameQualified, r.ReceiverTypeQualified)+inRepository(r.IdentityInRepository)+namedArgument(r.NamedArgument)+dynamicName(r.Dynamic), r.IsCall, loc(r.Location))
	}

	imps := slices.Clone(ext.Imports)
	sort.Slice(imps, func(i, j int) bool {
		if imps[i].Path != imps[j].Path {
			return imps[i].Path < imps[j].Path
		}
		return imps[i].Alias < imps[j].Alias
	})
	b.WriteString("## imports\n")
	for _, im := range imps {
		fmt.Fprintf(&b, "import path=%q alias=%q %s\n", im.Path, im.Alias, loc(im.Location))
	}

	// Module-binding evidence is printed only when a provider emits it, so
	// snapshots of providers that do not model bindings stay byte-identical.
	writeModuleBindings(&b, ext)

	b.WriteString("## diagnostics\n")
	for _, d := range sortedDiagnostics(ext.Diagnostics) {
		fmt.Fprintf(&b, "diag %s %q %s\n", d.Severity, d.Message, loc(d.Location))
	}

	return b.String()
}

// IndexSnapshot serializes a whole repository index: symbols, references, and
// resolved graph edges with confidence. This is the artefact whose byte-level
// stability the determinism contract (index 100x => identical) is asserted on.
func IndexSnapshot(idx *index.RepositoryIndex) string {
	var b strings.Builder

	// --- stats ---
	st := idx.Stats()
	langs := make([]string, 0, len(st.Languages))
	for l := range st.Languages {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	b.WriteString("## stats\n")
	fmt.Fprintf(&b, "files=%d symbols=%d references=%d relations=%d skipped=%d\n",
		st.Files, st.Symbols, st.References, st.Relations, st.Skipped)
	for _, l := range langs {
		fmt.Fprintf(&b, "lang %s=%d\n", l, st.Languages[l])
	}

	// --- symbols (sorted by file, line, qualified) ---
	syms := idx.FindSymbols("")
	sort.Slice(syms, func(i, j int) bool { return symbolLess(syms[i], syms[j]) })
	b.WriteString("## symbols\n")
	for _, s := range syms {
		fmt.Fprintf(&b, "symbol %s kind=%s lang=%s qualified=%q receiver=%q exported=%t%s %s\n",
			s.Name, s.Kind, s.Language, s.Qualified, s.Receiver, s.Exported, memberScope(s.MemberScope, s.MembersOutside)+parameterScope(s.ParameterScope), loc(s.Location))
	}

	// --- references (sorted by file order, then location) ---
	b.WriteString("## references\n")
	for _, f := range idx.Files() {
		refs := idx.ReferencesByFile(f)
		sort.Slice(refs, func(i, j int) bool { return refLess(refs[i], refs[j]) })
		for _, r := range refs {
			fmt.Fprintf(&b, "ref %s kind=%s container=%q receiver=%q%s%s call=%t %s\n",
				r.Name, r.Kind, r.Container, r.ReceiverExpr, receiverType(r.ReceiverType), qualifiedIdentity(r.NameQualified, r.ReceiverTypeQualified)+inRepository(r.IdentityInRepository)+namedArgument(r.NamedArgument)+dynamicName(r.Dynamic), r.IsCall, loc(r.Location))
		}
	}

	// --- resolved edges (resolutions) ---
	// Symbol IDs are opaque; render edges by qualified name so the golden stays
	// readable and stable across ID-scheme changes.
	b.WriteString("## edges\n")
	var lines []string
	for _, s := range syms {
		for _, e := range idx.GetCallees(s.ID) {
			to, ok := idx.GetSymbol(e.To)
			toName := string(e.To)
			if ok {
				toName = to.Qualified
			}
			lines = append(lines, fmt.Sprintf("edge %s -%s-> %s confidence=%s%s",
				s.Qualified, e.Kind, toName, e.Confidence, evidence(e.Evidence)))
		}
	}
	sort.Strings(lines)
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}

	// --- diagnostics ---
	b.WriteString("## diagnostics\n")
	for _, d := range sortedDiagnostics(idx.Diagnostics()) {
		fmt.Fprintf(&b, "diag %s %q %s\n", d.Severity, d.Message, loc(d.Location))
	}

	return b.String()
}

// ContextSnapshot serializes the ranked context result for a scenario. Scores
// are intentionally omitted (they are tuning-sensitive); the golden freezes the
// selected symbols, their order, reason, and confidence.
func ContextSnapshot(res *context.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## stats\ncandidates=%d selected=%d tokens=%d budget=%d truncated=%d targetTruncated=%t\n",
		res.Stats.TotalCandidates, res.Stats.SelectedItems, res.Stats.EstimatedTokens,
		res.Stats.BudgetTokens, res.Stats.TruncatedItems, res.Stats.TargetTruncated)
	b.WriteString("## items\n")
	for i, it := range res.Items {
		fmt.Fprintf(&b, "%d %s kind=%s reason=%q confidence=%s\n",
			i, it.Symbol.Qualified, it.Symbol.Kind, it.Reason, it.Confidence)
	}
	return b.String()
}

// --- helpers ---

func loc(l source.Location) string {
	return fmt.Sprintf("@%s:%d:%d-%d:%d", l.File,
		l.Range.Start.Line, l.Range.Start.Column, l.Range.End.Line, l.Range.End.Column)
}

func evidence(evs []resolver.ResolutionEvidence) string {
	if len(evs) == 0 {
		return ""
	}
	parts := make([]string, len(evs))
	for i, e := range evs {
		parts[i] = fmt.Sprintf("%s:%s", e.Kind, e.Detail)
	}
	sort.Strings(parts)
	return " evidence=[" + strings.Join(parts, "; ") + "]"
}

func sortedDiagnostics(ds []language.Diagnostic) []language.Diagnostic {
	out := slices.Clone(ds)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Location.Range.Start.Line != out[j].Location.Range.Start.Line {
			return out[i].Location.Range.Start.Line < out[j].Location.Range.Start.Line
		}
		if out[i].Severity != out[j].Severity {
			return out[i].Severity < out[j].Severity
		}
		return out[i].Message < out[j].Message
	})
	return out
}

func symbolDraftLess(a, b language.SymbolDraft) bool {
	if a.Location.Range.Start.Line != b.Location.Range.Start.Line {
		return a.Location.Range.Start.Line < b.Location.Range.Start.Line
	}
	if a.Location.Range.Start.Column != b.Location.Range.Start.Column {
		return a.Location.Range.Start.Column < b.Location.Range.Start.Column
	}
	return a.Qualified < b.Qualified
}

func refDraftLess(a, b language.ReferenceDraft) bool {
	if a.Location.Range.Start.Line != b.Location.Range.Start.Line {
		return a.Location.Range.Start.Line < b.Location.Range.Start.Line
	}
	if a.Location.Range.Start.Column != b.Location.Range.Start.Column {
		return a.Location.Range.Start.Column < b.Location.Range.Start.Column
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Kind < b.Kind
}

func symbolLess(a, b symbol.Symbol) bool {
	if a.Location.File != b.Location.File {
		return a.Location.File < b.Location.File
	}
	if a.Location.Range.Start.Line != b.Location.Range.Start.Line {
		return a.Location.Range.Start.Line < b.Location.Range.Start.Line
	}
	return a.Qualified < b.Qualified
}

func refLess(a, b reference.Reference) bool {
	if a.Location.Range.Start.Line != b.Location.Range.Start.Line {
		return a.Location.Range.Start.Line < b.Location.Range.Start.Line
	}
	if a.Location.Range.Start.Column != b.Location.Range.Start.Column {
		return a.Location.Range.Start.Column < b.Location.Range.Start.Column
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Kind < b.Kind
}

// receiverType renders provider-proven receiver type evidence, or nothing.
// dynamicName renders the Dynamic marker; omitted when false so snapshots of
// providers that never emit it are unaffected.
func dynamicName(d bool) string {
	if d {
		return " dynamic=true"
	}
	return ""
}

func receiverType(t string) string {
	if t == "" {
		return ""
	}
	return fmt.Sprintf(" receiver_type=%q", t)
}

// qualifiedIdentity renders provider-determined qualified identity evidence.
// Like receiverType it prints nothing when absent, so snapshots of providers
// that do not set it are byte-identical to before the fields existed.
func qualifiedIdentity(name, receiverType string) string {
	var s string
	if name != "" {
		s += fmt.Sprintf(" name_qualified=%q", name)
	}
	if receiverType != "" {
		s += fmt.Sprintf(" receiver_type_qualified=%q", receiverType)
	}
	return s
}

func moduleSpec(m language.ModuleSpec) string {
	parts := make([]string, len(m.Candidates))
	for i, c := range m.Candidates {
		parts[i] = fmt.Sprintf("%s@%d", c.File, c.Priority)
	}
	cands := "external"
	if m.Candidates != nil {
		cands = "[" + strings.Join(parts, " ") + "]"
	}
	return fmt.Sprintf("module=%q candidates=%s", m.Specifier, cands)
}

// memberScope renders a declaration's member-scope evidence; nothing when
// absent, so snapshots of providers that never emit it are unaffected.
func memberScope(scope string, outside bool) string {
	switch {
	case outside:
		return " members=outside"
	case scope != "":
		return fmt.Sprintf(" member_scope=%q", scope)
	}
	return ""
}

// parameterScope / namedArgument render the parameter-scope evidence;
// nothing when absent.
func parameterScope(scope string) string {
	if scope == "" {
		return ""
	}
	return fmt.Sprintf(" parameter_scope=%q", scope)
}

func namedArgument(a bool) string {
	if a {
		return " named_argument=true"
	}
	return ""
}

// inRepository renders the IdentityInRepository qualifier; omitted when false.
func inRepository(in bool) string {
	if in {
		return " identity_in_repository=true"
	}
	return ""
}

// writeModuleBindings serializes bindings / exports / module scope in source
// order (providers emit them deterministically; order is part of the contract).
func writeModuleBindings(b *strings.Builder, ext language.Extraction) {
	if ext.ModuleScoped {
		b.WriteString("## module_scoped\n")
	}
	if ext.IdentityOnly {
		b.WriteString("## identity_only\n")
	}
	if len(ext.Bindings) > 0 {
		b.WriteString("## bindings\n")
		for _, bd := range ext.Bindings {
			fmt.Fprintf(b, "binding %s kind=%s imported=%q type_only=%t %s %s\n",
				bd.Local, bd.Kind, bd.Imported, bd.TypeOnly, moduleSpec(bd.Module), loc(bd.Location))
		}
	}
	if len(ext.Exports) > 0 {
		b.WriteString("## exports\n")
		for _, ed := range ext.Exports {
			mod := ""
			if ed.Kind != language.ExportLocal {
				mod = " " + moduleSpec(ed.Module)
			}
			fmt.Fprintf(b, "export kind=%s exported=%q local=%q except=%q type_only=%t%s %s\n",
				ed.Kind, ed.Exported, ed.Local, ed.Except, ed.TypeOnly, mod, loc(ed.Location))
		}
	}
}

// CompletenessSnapshot serializes, per symbol in file/line order, what the
// graph does not show: unattributed incoming / outgoing counts, the outgoing
// references with no candidate (with their reasons) and the candidate
// relations. Symbols with nothing to report are omitted.
func CompletenessSnapshot(idx *index.RepositoryIndex) string {
	var b strings.Builder
	syms := idx.FindSymbols("")
	sort.Slice(syms, func(i, j int) bool { return symbolLess(syms[i], syms[j]) })
	for _, s := range syms {
		in, out := idx.Unattributed(s.ID)
		un := idx.UnresolvedOutgoing(s.ID)
		callers := idx.CandidateCallerSample(s.ID)
		callees := idx.CandidateCalleeSample(s.ID)
		if in == 0 && out == 0 && un.Total == 0 && callers.Total == 0 && callees.Total == 0 {
			continue
		}
		fmt.Fprintf(&b, "symbol %s unattributed_in=%d unattributed_out=%d unresolved=%d outside=%d\n",
			s.Qualified, in, out, un.Unresolved, un.OutsideRepository)
		for _, r := range un.References {
			fmt.Fprintf(&b, "  no_candidate %s kind=%s receiver=%q reason=%s %s\n", r.Name, r.Kind, r.ReceiverExpr, r.Reason, loc(r.Location))
		}
		for _, r := range callees.Relations {
			to, _ := idx.GetSymbol(r.Symbol)
			fmt.Fprintf(&b, "  candidate_callee %s kind=%s confidence=%s refs=%d\n", to.Qualified, r.Kind, r.Confidence, r.References)
		}
		for _, r := range callers.Relations {
			from, _ := idx.GetSymbol(r.Symbol)
			fmt.Fprintf(&b, "  candidate_caller %s kind=%s confidence=%s refs=%d\n", from.Qualified, r.Kind, r.Confidence, r.References)
		}
	}
	return b.String()
}
