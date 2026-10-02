package language

// SupportLevel describes how thoroughly Ark supports a language.
// Only advertise a level that is actually covered by tests.
type SupportLevel uint8

const (
	SupportLevelNone                    SupportLevel = iota
	SupportLevelParse                                // tree-sitter parse only
	SupportLevelSymbols                              // symbol extraction tested
	SupportLevelReferences                           // reference/import extraction tested
	SupportLevelResolution                           // resolution pipeline tested
	SupportLevelGraph                                // graph queries tested
	SupportLevelContextQualityCertified              // context quality benchmark passed
)

func (l SupportLevel) String() string {
	switch l {
	case SupportLevelParse:
		return "parse"
	case SupportLevelSymbols:
		return "symbols"
	case SupportLevelReferences:
		return "references"
	case SupportLevelResolution:
		return "resolution"
	case SupportLevelGraph:
		return "graph"
	case SupportLevelContextQualityCertified:
		return "context_quality_certified"
	default:
		return "none"
	}
}

// SupportLevelFor returns the tested support level for the given language name.
func SupportLevelFor(lang string) SupportLevel {
	switch lang {
	case "go":
		// Go has symbols, references, resolution, graph, and context quality tests.
		return SupportLevelContextQualityCertified
	case "typescript", "tsx", "javascript":
		// TS/JS have symbol and reference extraction tested.
		return SupportLevelReferences
	case "python":
		// Python has symbol and reference extraction tested.
		return SupportLevelReferences
	default:
		return SupportLevelNone
	}
}
