//go:build race

package ark

// raceEnabled: this test binary runs under the race detector (go test
// -race), so the shared ark binary is built with -race as well.
const raceEnabled = true
