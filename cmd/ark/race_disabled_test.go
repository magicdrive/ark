//go:build !race

package ark

// raceEnabled: this test binary runs without the race detector, so the
// shared ark binary is an ordinary build.
const raceEnabled = false
