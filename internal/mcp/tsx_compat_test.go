package mcp

import "testing"

// TestTSXCompatBehavior locks the PR-1 compatibility decision: repository
// indexing / relations do NOT handle .tsx (historical behavior preserved),
// while find_references DOES. See tsxCompatExclusion and IMPROVEMENTS.md (Q5).
func TestTSXCompatBehavior(t *testing.T) {
	for _, p := range defaultProviders() {
		if p.Language() == tsxCompatExclusion {
			t.Error("defaultProviders() must exclude tsx for PR-1 compatibility")
		}
	}
	if _, ok := refProviderRegistry[".tsx"]; !ok {
		t.Error("find_references must still handle .tsx")
	}
}
