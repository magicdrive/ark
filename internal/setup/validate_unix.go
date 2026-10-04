//go:build !windows

package setup

import "os"

// isExecutableFile reports whether path (already known to be a regular file) is
// executable on Unix-like systems: at least one of the execute permission bits
// is set (task §16).
func isExecutableFile(path string, info os.FileInfo) bool {
	return info.Mode().Perm()&0o111 != 0
}
