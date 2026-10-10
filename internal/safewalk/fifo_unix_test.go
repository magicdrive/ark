//go:build unix

package safewalk

import "syscall"

func mkfifo(path string) error { return syscall.Mkfifo(path, 0o644) }
