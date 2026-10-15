//go:build race

package mcp

// raceEnabled: this test binary runs under the race detector (go test
// -race): the CI race shards. A test whose work is a long sequential sweep
// (no goroutines of its own) samples it there; the unit job, without the
// race detector, runs the whole sweep.
const raceEnabled = true
