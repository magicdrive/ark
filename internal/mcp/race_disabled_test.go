//go:build !race

package mcp

// raceEnabled: this test binary runs without the race detector (the unit
// job): a sampled sweep runs whole.
const raceEnabled = false
