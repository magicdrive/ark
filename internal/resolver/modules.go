package resolver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Module-binding resolution.
//
// Everything here is language-neutral: it consumes only the provider-emitted
// ModuleSpec / BindingDraft / ExportDraft evidence. Module specifiers are never
// interpreted; candidate files are matched exactly against the indexed FileIDs
// and ranked only by the provider-supplied Priority.
//
// Export traversal (re-exports, export-all, barrel chains) is bounded and
// cycle-safe:
//   - a (file, name) pair already on the current path is a cycle and yields
//     nothing for that branch;
//   - depth is capped at maxExportDepth;
//   - every top-level lookup has a budget of maxExportSteps visited states;
//   - iteration follows provider source order and sorted candidate files only,
//     so results never depend on map iteration or discovery timing.
// A lookup that hits a cycle, the depth cap or the budget is "incomplete": it
// contributes no target (never a guessed one) and is not memoised.

const (
	maxExportDepth = 16
	maxExportSteps = 512
)

// moduleStatus is the outcome of matching a ModuleSpec against the repository.
type moduleStatus uint8

const (
	moduleUnresolved moduleStatus = iota // external, unsupported, or no candidate file exists
	moduleResolved                       // exactly one best-priority candidate exists
	moduleAmbiguous                      // several equally-ranked candidates exist
)

// resolveModule matches m's candidates against indexed files. Only the lowest
// Priority tier that has at least one existing file is considered; more than
// one existing file in that tier is ambiguity, never a choice.
func (r *Resolver) resolveModule(m language.ModuleSpec) ([]source.FileID, moduleStatus) {
	if len(m.Candidates) == 0 {
		return nil, moduleUnresolved
	}
	var tier []source.FileID
	best := 0
	for _, c := range m.Candidates {
		if _, ok := r.byFile[c.File]; !ok {
			continue
		}
		switch {
		case len(tier) == 0:
			tier, best = []source.FileID{c.File}, c.Priority
		case c.Priority < best:
			tier, best = []source.FileID{c.File}, c.Priority
		case c.Priority == best:
			tier = append(tier, c.File)
		}
	}
	sort.Slice(tier, func(i, j int) bool { return tier[i] < tier[j] })
	switch len(tier) {
	case 0:
		return nil, moduleUnresolved
	case 1:
		return tier, moduleResolved
	default:
		return tier, moduleAmbiguous
	}
}

// bindTarget is what a module binding or export denotes: either one symbol or
// a whole module (namespace import / namespace re-export).
type bindTarget struct {
	sym    symbol.Symbol
	module source.FileID // non-empty for a module (namespace) target
}

func (t bindTarget) key() string {
	if t.module != "" {
		return "module\x00" + string(t.module)
	}
	return "symbol\x00" + string(t.sym.ID)
}

// bindResult is the outcome of following a binding or export.
type bindResult struct {
	targets   []bindTarget
	ambiguous bool // some step offered several equally-valid options
	typeOnly  bool // some step was type-only
	complete  bool // no cycle / depth / budget cut-off was hit
	chain     []string
}

func (b *bindResult) merge(o bindResult) {
	b.ambiguous = b.ambiguous || o.ambiguous
	b.typeOnly = b.typeOnly || o.typeOnly
	b.complete = b.complete && o.complete
	b.chain = append(b.chain, o.chain...)
	for _, t := range o.targets {
		dup := false
		for _, have := range b.targets {
			if have.key() == t.key() {
				dup = true
				break
			}
		}
		if !dup {
			b.targets = append(b.targets, t)
		}
	}
}

// exportWalk carries the per-lookup traversal state.
type exportWalk struct {
	r      *Resolver
	onPath map[string]bool
	steps  int
}

func (r *Resolver) newWalk() *exportWalk {
	return &exportWalk{r: r, onPath: make(map[string]bool)}
}

// bindingTargets follows the import bindings of fi named local.
func (w *exportWalk) bindingTargets(fi *FileIndex, local string, depth int) bindResult {
	out := bindResult{complete: true}
	for _, b := range fi.Bindings {
		if b.Local != local {
			continue
		}
		mods, st := w.r.resolveModule(b.Module)
		if st == moduleAmbiguous {
			out.ambiguous = true
		}
		if b.TypeOnly {
			out.typeOnly = true
		}
		out.chain = append(out.chain, fmt.Sprintf("%s ← %q", fi.FileID, b.Module.Specifier))
		for _, m := range mods {
			switch b.Kind {
			case language.BindingNamespace:
				out.merge(bindResult{targets: []bindTarget{{module: m}}, complete: true})
			case language.BindingNamed:
				out.merge(w.export(m, b.Imported, depth+1))
			}
		}
	}
	return out
}

// hasBinding reports whether fi binds local through an import binding.
func hasBinding(fi *FileIndex, local string) bool {
	for _, b := range fi.Bindings {
		if b.Local == local {
			return true
		}
	}
	return false
}

// export resolves the export `name` of module file.
func (w *exportWalk) export(file source.FileID, name string, depth int) bindResult {
	key := string(file) + "\x00" + name
	if memo, ok := w.r.exportMemoGet(key); ok {
		return memo
	}
	if depth > maxExportDepth || w.steps >= maxExportSteps || w.onPath[key] {
		return bindResult{complete: false}
	}
	w.steps++
	w.onPath[key] = true
	defer delete(w.onPath, key)

	out := bindResult{complete: true}
	fi := w.r.byFile[file]
	if fi == nil {
		return out
	}

	// Explicit exports (local / from / namespace) shadow export-all.
	explicit := false
	for _, e := range fi.Exports {
		if e.Kind == language.ExportAll || e.Exported != name {
			continue
		}
		explicit = true
		if e.TypeOnly {
			out.typeOnly = true
		}
		switch e.Kind {
		case language.ExportLocal:
			out.merge(w.local(fi, e.Local, depth))
		case language.ExportFrom, language.ExportNamespace:
			mods, st := w.r.resolveModule(e.Module)
			if st == moduleAmbiguous {
				out.ambiguous = true
			}
			for _, m := range mods {
				if e.Kind == language.ExportNamespace {
					out.merge(bindResult{targets: []bindTarget{{module: m}}, complete: true})
				} else {
					out.merge(w.export(m, e.Local, depth+1))
				}
			}
		}
	}
	if !explicit {
		for _, e := range fi.Exports {
			if e.Kind != language.ExportAll || containsString(e.Except, name) {
				continue
			}
			if e.TypeOnly {
				out.typeOnly = true
			}
			mods, st := w.r.resolveModule(e.Module)
			if st == moduleAmbiguous {
				out.ambiguous = true
			}
			for _, m := range mods {
				out.merge(w.export(m, name, depth+1))
			}
		}
	}
	if len(out.targets) > 0 {
		out.chain = append([]string{fmt.Sprintf("%s exports %q", file, name)}, out.chain...)
	}
	if out.complete {
		w.r.exportMemoPut(key, out)
	}
	return out
}

// local resolves a name bound in fi's own top-level scope: a top-level
// declaration, or (for re-exported imports) an import binding.
func (w *exportWalk) local(fi *FileIndex, name string, depth int) bindResult {
	out := bindResult{complete: true}
	for _, s := range fi.Symbols {
		if s.Name == name && s.ParentQualified == "" {
			out.merge(bindResult{targets: []bindTarget{{sym: s}}, complete: true})
		}
	}
	if len(out.targets) > 0 {
		return out
	}
	return w.bindingTargets(fi, name, depth)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// exportMemoGet / exportMemoPut cache complete export lookups. The memo only
// holds results whose traversal did not hit a cycle or bound, so it never
// changes the outcome — it only avoids repeated work across references.
func (r *Resolver) exportMemoGet(key string) (bindResult, bool) {
	r.memoMu.Lock()
	defer r.memoMu.Unlock()
	v, ok := r.exportMemo[key]
	return v, ok
}

func (r *Resolver) exportMemoPut(key string, v bindResult) {
	r.memoMu.Lock()
	defer r.memoMu.Unlock()
	r.exportMemo[key] = v
}

// symbolTargets returns the symbol targets of a bind result, sorted.
func (b bindResult) symbolTargets() []symbol.Symbol {
	var out []symbol.Symbol
	for _, t := range b.targets {
		if t.module == "" {
			out = append(out, t.sym)
		}
	}
	sortSymbols(out)
	return out
}

func (b bindResult) chainDetail() string {
	return strings.Join(b.chain, " → ")
}

func sortSymbols(syms []symbol.Symbol) {
	sort.Slice(syms, func(i, j int) bool {
		if syms[i].Location.File != syms[j].Location.File {
			return syms[i].Location.File < syms[j].Location.File
		}
		if syms[i].Qualified != syms[j].Qualified {
			return syms[i].Qualified < syms[j].Qualified
		}
		return syms[i].ID < syms[j].ID
	})
}
