package setup

// State classifies the existing Ark configuration for a client before mutation
// (plan §10). The setup core and every adapter speak this vocabulary.
type State int

const (
	// StateAbsent: no Ark entry exists. Action: create.
	StateAbsent State = iota
	// StateEquivalent: an Ark entry exists and is semantically identical to the
	// desired configuration. Action: no-op (do not rewrite the file).
	StateEquivalent
	// StateConflict: an Ark entry exists but differs from the desired
	// configuration. Action: error without --force; replace Ark entry only with
	// --force.
	StateConflict
	// StateMalformed: the configuration file cannot be parsed. Action: error
	// even with --force. Ark never "repairs" a broken config.
	StateMalformed
	// StateUnavailable: the target cannot be resolved or safely accessed
	// (home dir, permissions, non-regular target, parent dir). Action: explicit
	// error, never a silent fallback.
	StateUnavailable
)

func (s State) String() string {
	switch s {
	case StateAbsent:
		return "absent"
	case StateEquivalent:
		return "equivalent"
	case StateConflict:
		return "conflict"
	case StateMalformed:
		return "malformed"
	case StateUnavailable:
		return "unavailable"
	default:
		return "unknown"
	}
}
