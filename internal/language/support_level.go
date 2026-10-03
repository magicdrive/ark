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
