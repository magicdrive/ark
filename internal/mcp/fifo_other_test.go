//go:build !unix

package mcp

import "errors"

func mkfifo(string) error { return errors.New("no FIFOs here") }
