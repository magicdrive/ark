package language

import (
	"fmt"
	"path"
	"strings"
)

// ValidateModuleBindings checks the structural contract of an Extraction's
// module-binding evidence (ModuleSpec / BindingDraft / ExportDraft). It is
// pure and language-neutral; provider conformance runs it on every case.
func ValidateModuleBindings(ex Extraction) []error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	for i, b := range ex.Bindings {
		if b.Local == "" {
			add("binding[%d]: Local is empty", i)
		}
		switch b.Kind {
		case BindingNamed:
			if b.Imported == "" {
				add("binding[%d] %q: named binding has empty Imported", i, b.Local)
			}
		case BindingNamespace:
			if b.Imported != "" {
				add("binding[%d] %q: namespace binding must not set Imported", i, b.Local)
			}
		default:
			add("binding[%d] %q: unknown Kind %q", i, b.Local, b.Kind)
		}
		for _, e := range validateModuleSpec(b.Module) {
			add("binding[%d] %q: %v", i, b.Local, e)
		}
		if b.Location.File == "" {
			add("binding[%d] %q: Location.File is empty", i, b.Local)
		}
	}

	for i, e := range ex.Exports {
		switch e.Kind {
		case ExportLocal:
			if e.Exported == "" || e.Local == "" {
				add("export[%d]: local export needs Exported and Local", i)
			}
			if e.Module.Specifier != "" || e.Module.Candidates != nil {
				add("export[%d] %q: local export must not carry a Module", i, e.Exported)
			}
		case ExportFrom:
			if e.Exported == "" || e.Local == "" {
				add("export[%d]: from-export needs Exported and Local", i)
			}
		case ExportAll:
			if e.Exported != "" || e.Local != "" {
				add("export[%d]: export-all must not set Exported/Local", i)
			}
		case ExportNamespace:
			if e.Exported == "" || e.Local != "" {
				add("export[%d]: namespace export needs Exported and no Local", i)
			}
		default:
			add("export[%d]: unknown Kind %q", i, e.Kind)
		}
		if e.Kind != ExportAll && len(e.Except) > 0 {
			add("export[%d] %q: Except is only valid for export-all", i, e.Exported)
		}
		if e.Kind != ExportLocal {
			if e.Module.Specifier == "" {
				add("export[%d] %q: re-export has empty module specifier", i, e.Exported)
			}
			for _, me := range validateModuleSpec(e.Module) {
				add("export[%d] %q: %v", i, e.Exported, me)
			}
		}
		if e.Location.File == "" {
			add("export[%d] %q: Location.File is empty", i, e.Exported)
		}
	}
	return errs
}

func validateModuleSpec(m ModuleSpec) []error {
	var errs []error
	if m.Specifier == "" {
		errs = append(errs, fmt.Errorf("module specifier is empty"))
	}
	for i, c := range m.Candidates {
		f := string(c.File)
		switch {
		case f == "":
			errs = append(errs, fmt.Errorf("candidate[%d] is empty", i))
		case strings.Contains(f, `\`):
			errs = append(errs, fmt.Errorf("candidate[%d] %q is not slash-separated", i, f))
		case path.IsAbs(f) || path.Clean(f) != f || f == ".." || strings.HasPrefix(f, "../"):
			errs = append(errs, fmt.Errorf("candidate[%d] %q is not a clean root-relative path", i, f))
		}
		if i > 0 {
			p := m.Candidates[i-1]
			if p.Priority > c.Priority || (p.Priority == c.Priority && p.File >= c.File) {
				errs = append(errs, fmt.Errorf("candidates not sorted by (Priority, File) or duplicated at %d", i))
			}
		}
	}
	return errs
}
