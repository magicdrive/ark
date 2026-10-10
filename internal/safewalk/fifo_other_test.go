//go:build !unix

package safewalk

import "errors"

func mkfifo(string) error { return errors.New("no FIFOs here") }
