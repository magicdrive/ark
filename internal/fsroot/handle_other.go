//go:build !unix && !windows

package fsroot

import (
	"errors"
	"io/fs"
	"os"
)

// Platforms without descriptor-relative opens (js, wasip1, plan9): Pin
// fails, so nothing is read without the guarantees this package gives.

var errUnsupported = errors.New("fsroot: not supported on this platform")

func openRootDir(path string) (handle, error) {
	return nil, &fs.PathError{Op: "open", Path: path, Err: errUnsupported}
}

func pathIdentity(path string) (Identity, error) { return Identity{}, errUnsupported }

func fileIdentity(*os.File, fs.FileInfo) (Identity, error) { return Identity{}, errUnsupported }

func openNoBlock(path string) (*os.File, error) { return nil, errUnsupported }
