//go:build !race

package css_test

// raceEnabled: this test binary runs without the race detector (the unit
// job): a time bound applies.
const raceEnabled = false
