//go:build race

package markdown

// raceEnabled: this test binary runs under the race detector (go test
// -race): the CI race shards. A timing test (no goroutines of its own)
// measures smaller inputs there; the unit job, without the race detector,
// measures them whole.
const raceEnabled = true
