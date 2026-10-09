package mcp

import (
	"os"
	"path/filepath"
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

// FuzzResolveToolPath_PhysicalRoot runs the path gate against a real root that
// contains symlinks both inside and out of it. Invariant: an accepted path is
// lexically inside the root and, when it exists, physically inside it too.
func FuzzResolveToolPath_PhysicalRoot(f *testing.F) {
	for _, s := range []string{"main.go", "alias.go", "leak.go", "leakdir/x.go", "sub/../main.go", "../outside/x.go", "."} {
		f.Add(s)
	}
	base := f.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			f.Fatal(err)
		}
	}
	for p, c := range map[string]string{filepath.Join(root, "main.go"): "package main\n", filepath.Join(outside, "x.go"): "package x\n"} {
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			f.Fatal(err)
		}
	}
	links := map[string]string{"alias.go": filepath.Join(root, "main.go"), "leak.go": filepath.Join(outside, "x.go"), "leakdir": outside}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			f.Skip("symlinks unavailable:", err)
		}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		f.Fatal(err)
	}
	h := NewToolsHandler(root, nil)

	f.Fuzz(func(t *testing.T, input string) {
		full, _, err := h.resolveToolPath(input)
		if err != nil {
			return
		}
		if _, ok := relInside(root, full); !ok {
			t.Fatalf("resolveToolPath(%q) = %q, outside %q", input, full, root)
		}
		if real, err := filepath.EvalSymlinks(full); err == nil {
			if _, ok := relInside(realRoot, real); !ok {
				t.Fatalf("resolveToolPath(%q) = %q resolves to %q, outside %q", input, full, real, realRoot)
			}
		}
	})
}
