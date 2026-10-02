package resolver

// Confidence expresses how certain a resolution is.
type Confidence uint8

const (
	ConfidenceUnresolved Confidence = iota // no candidate found
	ConfidenceCandidate                    // multiple plausible candidates
	ConfidenceStrong                       // single repo-wide match
	ConfidenceExact                        // unambiguous (same file/import)
)

func (c Confidence) String() string {
	switch c {
	case ConfidenceUnresolved:
		return "unresolved"
	case ConfidenceCandidate:
		return "candidate"
	case ConfidenceStrong:
		return "strong"
	case ConfidenceExact:
		return "exact"
	default:
		return "unknown"
	}
}
