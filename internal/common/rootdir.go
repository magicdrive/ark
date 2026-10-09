package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveRootDir resolves the directory Ark serves: an empty root is the
// current directory, a relative root is resolved against the current directory
// now, and the result is absolute and clean. A missing root or one that is not
// a directory is an error naming the resolved path — never deferred to the
// first request.
//
// Symlinks are kept: the root keeps the spelling it was given, and callers
// that need the physical location resolve it themselves.
func ResolveRootDir(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("cannot determine current directory: %w", err)
		}
		root = cwd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("cannot resolve root %q: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("root directory does not exist: %s", abs)
		}
		return "", fmt.Errorf("cannot access root %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("root is not a directory: %s", abs)
	}
	return abs, nil
}
