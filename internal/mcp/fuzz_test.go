package mcp

import (
	"strings"
	"testing"
)

// FuzzResolveToolPath verifies the root-containment invariant under arbitrary input.
//
// Property: if resolveToolPath returns no error, the resolved fullPath must
// always be under h.rootDir (never outside it).
func FuzzResolveToolPath(f *testing.F) {
	// Seed corpus: interesting boundary cases.
	seeds := []string{
		"internal/cache/key.go",
		"../escape",
		"../../etc/passwd",
		"/tmp/outside",
		"./sub/../../../escape",
		"",
		".",
		"..",
		"a/b/c",
		"/abs/path/inside",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	root := "/tmp/ark_fuzz_root"
	h := &ToolsHandler{rootDir: root}

	f.Fuzz(func(t *testing.T, input string) {
		fullPath, _, err := h.resolveToolPath(input)
		if err != nil {
			// Rejection is fine — just must not panic.
			return
		}
		// Invariant: fullPath must be under root.
		rel := strings.TrimPrefix(fullPath, root)
		if rel == fullPath {
			// fullPath doesn't start with root at all.
			t.Errorf("resolveToolPath(%q) = %q, which is outside root %q", input, fullPath, root)
		}
		if strings.HasPrefix(rel, "/../") || rel == "/.." {
			t.Errorf("resolveToolPath(%q) = %q escapes root via ..", input, fullPath)
		}
	})
}
