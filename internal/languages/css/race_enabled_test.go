//go:build race

package css_test

// raceEnabled: this test binary runs under the race detector (go test
// -race): the CI race shards. A time bound (TestWorstCases) does not apply
// there; the counts of TestReaderStepsLinear do.
const raceEnabled = true
