package mcp

import (
	"strings"
	"testing"
)

// TestResolveToolPath verifies that resolveToolPath enforces root containment.
// Every successful resolution must produce a path inside h.rootDir.
func TestResolveToolPath_NormalRelative(t *testing.T) {
	h := &ToolsHandler{rootDir: "/repo"}
	full, rel, err := h.resolveToolPath("internal/foo.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if full != "/repo/internal/foo.go" {
		t.Errorf("fullPath: want /repo/internal/foo.go, got %q", full)
	}
	if rel != "internal/foo.go" {
		t.Errorf("relPath: want internal/foo.go, got %q", rel)
	}
}

func TestResolveToolPath_RootItself(t *testing.T) {
	h := &ToolsHandler{rootDir: "/repo"}
	full, rel, err := h.resolveToolPath(".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if full != "/repo" {
		t.Errorf("fullPath: want /repo, got %q", full)
	}
	if rel != "." {
		t.Errorf("relPath: want ., got %q", rel)
	}
}

func TestResolveToolPath_AbsoluteInsideRoot(t *testing.T) {
	h := &ToolsHandler{rootDir: "/repo"}
	full, rel, err := h.resolveToolPath("/repo/pkg/main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if full != "/repo/pkg/main.go" {
		t.Errorf("fullPath: want /repo/pkg/main.go, got %q", full)
	}
	if rel != "pkg/main.go" {
		t.Errorf("relPath: want pkg/main.go, got %q", rel)
	}
}

func TestResolveToolPath_RelativeEscape(t *testing.T) {
	h := &ToolsHandler{rootDir: "/repo"}
	_, _, err := h.resolveToolPath("../etc/passwd")
	if err == nil {
		t.Fatal("expected error for ../ escape, got nil")
	}
}

func TestResolveToolPath_DeepRelativeEscape(t *testing.T) {
	h := &ToolsHandler{rootDir: "/repo"}
	_, _, err := h.resolveToolPath("../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for ../../ escape, got nil")
	}
}

func TestResolveToolPath_AbsoluteOutsideRoot(t *testing.T) {
	h := &ToolsHandler{rootDir: "/repo"}
	_, _, err := h.resolveToolPath("/etc/passwd")
	if err == nil {
		t.Fatal("expected error for absolute path outside root, got nil")
	}
}

func TestResolveToolPath_PrefixCollision(t *testing.T) {
	// /repo-other must not be considered inside /repo
	h := &ToolsHandler{rootDir: "/repo"}
	_, _, err := h.resolveToolPath("/repo-other/secret")
	if err == nil {
		t.Fatal("expected error for prefix-collision path, got nil")
	}
}

func TestResolveToolPath_InternalDotDotNormalizesInside(t *testing.T) {
	// internal/../internal/foo.go normalises to internal/foo.go — inside root
	h := &ToolsHandler{rootDir: "/repo"}
	full, _, err := h.resolveToolPath("internal/../internal/foo.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(full, "/repo/") && full != "/repo" {
		t.Errorf("expected path inside /repo, got %q", full)
	}
}

func TestResolveToolPath_DotSlash(t *testing.T) {
	h := &ToolsHandler{rootDir: "/repo"}
	full, _, err := h.resolveToolPath("./internal/foo.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if full != "/repo/internal/foo.go" {
		t.Errorf("fullPath: want /repo/internal/foo.go, got %q", full)
	}
}
