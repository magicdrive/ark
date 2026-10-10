//go:build unix

package fsroot

import (
	"io/fs"
	"time"

	"golang.org/x/sys/unix"
)

// statInfo is the fs.FileInfo of a stat result, as os.Lstat reports it.
type statInfo_ struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
}

func (s statInfo_) Name() string       { return s.name }
func (s statInfo_) Size() int64        { return s.size }
func (s statInfo_) Mode() fs.FileMode  { return s.mode }
func (s statInfo_) ModTime() time.Time { return s.modTime }
func (s statInfo_) IsDir() bool        { return s.mode.IsDir() }
func (s statInfo_) Sys() any           { return nil }

func statInfo(name string, st *unix.Stat_t) fs.FileInfo {
	mode := modeOf(st) | fs.FileMode(st.Mode&0o777)
	if st.Mode&unix.S_ISUID != 0 {
		mode |= fs.ModeSetuid
	}
	if st.Mode&unix.S_ISGID != 0 {
		mode |= fs.ModeSetgid
	}
	if st.Mode&unix.S_ISVTX != 0 {
		mode |= fs.ModeSticky
	}
	sec, nsec := st.Mtim.Unix()
	return statInfo_{name: name, size: st.Size, mode: mode, modTime: time.Unix(sec, nsec)}
}
