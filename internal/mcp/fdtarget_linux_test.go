package mcp

import (
	"os"
	"path/filepath"
)

const fdDir = "/proc/self/fd"

// fdTarget returns the path of the object descriptor fd (a name in fdDir)
// is open on.
func fdTarget(fd string) (string, bool) {
	target, err := os.Readlink(filepath.Join(fdDir, fd))
	return target, err == nil
}
