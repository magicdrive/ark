package resolver

// Confidence classifies the evidence behind a resolution. It is an ordered set
// of evidence classes, not a probability or a score: never average, add or
// promote it. A provider's ConfidenceCap and the resolver's caps only lower it.
// Only a single candidate at Strong or Exact becomes a graph edge
// (Resolution.HasUniqueTarget); see ARCHITECTURE.md, "Confidence".
type Confidence uint8

const (
	// ConfidenceUnresolved: no candidate. Either nothing matched, or
	// authoritative evidence (qualified identity, declared receiver type,
	// import binding) found no repository target — then OutsideRepository
	// may be set. A correct answer, not a failure to be filled in.
	ConfidenceUnresolved Confidence = iota
	// ConfidenceCandidate: the target is not identified. Several plausible
	// symbols, or one symbol whose evidence cannot exclude alternatives
	// (an untyped receiver, a module-scoped free name, an unknown structural
	// participant, a provider cap). Candidates are kept as evidence, never
	// as an edge, and none of them is ever chosen.
	ConfidenceCandidate
	// ConfidenceStrong: one target, identified by evidence weaker than the
	// language's own scoping — the only symbol of that name in the
	// repository, the same directory, a receiver-name match, a member
	// attached by receiver name rather than lexical containment, or a
	// provider cap at "strong".
	ConfidenceStrong
	// ConfidenceExact: one target, identified by the language's own scoping
	// as the provider and resolver model it — same container or file, an
	// explicit import or module binding, a qualified identity, or a member
	// lexically contained in a receiver type itself identified at Exact.
	// Static evidence, not a compiler's proof.
	ConfidenceExact
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
