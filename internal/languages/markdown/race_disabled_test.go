//go:build !race

package markdown

// raceEnabled: this test binary runs without the race detector (the unit
// job): a timing test measures its inputs whole.
const raceEnabled = false
