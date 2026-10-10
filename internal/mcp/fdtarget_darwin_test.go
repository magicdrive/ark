package mcp

import (
	"bytes"
	"strconv"
	"syscall"
	"unsafe"
)

const fdDir = "/dev/fd"

// fdTarget returns the path of the object descriptor fd (a name in fdDir)
// is open on: fcntl(F_GETPATH).
func fdTarget(fd string) (string, bool) {
	n, err := strconv.Atoi(fd)
	if err != nil {
		return "", false
	}
	var buf [1024]byte // MAXPATHLEN
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(n), syscall.F_GETPATH, uintptr(unsafe.Pointer(&buf[0]))); errno != 0 {
		return "", false
	}
	return string(buf[:bytes.IndexByte(buf[:], 0)]), true
}
