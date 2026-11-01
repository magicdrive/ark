package secrets

// WithoutFloor is p without v6.1.0's slack-token floor (Policy.floor): its
// pipeline alone, which the amended v6.1.0 oracle masks as.
func (p *Policy) WithoutFloor() *Policy {
	q := *p
	q.floor = nil
	return &q
}
