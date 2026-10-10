//go:build !unix

package fsroot

import "errors"

func mkfifo(string) error { return errors.New("no FIFOs") }
