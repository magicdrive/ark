//go:build windows

package setup

import (
	"os"
	"path/filepath"
	"strings"
)

// isExecutableFile reports whether path (already known to be a regular file) is
// a plausible Windows executable (task §15, §16).
//
// Windows has no Unix execute-permission bit, so a valid executable must never
// be rejected merely because mode&0111 == 0. Executability is determined by the
// file extension against PATHEXT (defaulting to the usual set). If the path has
// no extension we accept it rather than risk a false negative — Ark does not run
// the binary to probe it.
func isExecutableFile(path string, info os.FileInfo) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return true
	}
	pathext := os.Getenv("PATHEXT")
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD;.VBS;.JS;.WSF;.MSC;.PS1"
	}
	for _, e := range strings.Split(pathext, ";") {
		if ext == strings.ToLower(strings.TrimSpace(e)) {
			return true
		}
	}
	return false
}
