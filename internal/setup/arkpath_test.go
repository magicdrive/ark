package setup

import (
	"testing"
)

func TestResolveArkPath_RegularExecutableAccepted(t *testing.T) {
	ark := fakeArk(t) // 0755 regular file
	got, _, err := resolveArkPath(ark)
	if err != nil {
		t.Fatalf("regular executable should be accepted: %v", err)
	}
	if got != ark {
		t.Errorf("got %q want %q", got, ark)
	}
}

func TestResolveArkPath_DirectoryRejected(t *testing.T) {
	dir := t.TempDir()
	// Use a path that "looks like a path" (contains a separator) so the explicit
	// filesystem-validation branch is taken.
	_, _, err := resolveArkPath(dir)
	if err == nil {
		t.Fatal("a directory must be rejected as --ark-path")
	}
}
