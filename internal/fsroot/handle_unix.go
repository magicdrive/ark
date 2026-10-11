//go:build unix

package fsroot

import (
	"io/fs"
	"os"
	"sort"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

// Unix: directory descriptors. Every open is openat(2) relative to a
// directory descriptor with O_NOFOLLOW, so the kernel refuses a symlink in
// the one component named, atomically; nothing is resolved by path.

type unixDir struct {
	fd    int
	id    Identity     // of the directory opened (fstat when opened)
	name  string       // for messages and os.File names only
	exact atomic.Int32 // exactNames, once known: exactYes or exactNo
}

const (
	exactYes = 1
	exactNo  = 2
)

func openRootDir(path string) (handle, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return nil, &fs.PathError{Op: "fstat", Path: path, Err: err}
	}
	return &unixDir{fd: fd, id: statIdentity(&st), name: path}, nil
}

func statIdentity(st *unix.Stat_t) Identity {
	return Identity{dev: uint64(st.Dev), ino: uint64(st.Ino)}
}

func modeOf(st *unix.Stat_t) fs.FileMode {
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		return fs.ModeDir
	case unix.S_IFLNK:
		return fs.ModeSymlink
	case unix.S_IFREG:
		return 0
	case unix.S_IFIFO:
		return fs.ModeNamedPipe
	case unix.S_IFSOCK:
		return fs.ModeSocket
	case unix.S_IFCHR:
		return fs.ModeDevice | fs.ModeCharDevice
	case unix.S_IFBLK:
		return fs.ModeDevice
	}
	return fs.ModeIrregular
}

func (d *unixDir) lstat(name string) (objInfo, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(d.fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return objInfo{}, &fs.PathError{Op: "lstat", Path: name, Err: err}
	}
	return objInfo{mode: modeOf(&st), size: st.Size, id: statIdentity(&st)}, nil
}

func (d *unixDir) openDir(name string, want Identity) (handle, error) {
	fd, err := unix.Openat(d.fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		// A symlink (ELOOP on Linux, ENOTDIR on macOS) or a non-directory
		// where a directory was listed: the tree changed, or the caller
		// named something it may not enter.
		return nil, &fs.PathError{Op: "openat", Path: name, Err: err}
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return nil, &fs.PathError{Op: "fstat", Path: name, Err: err}
	}
	id := statIdentity(&st)
	if !want.IsZero() && !id.Same(want) {
		unix.Close(fd)
		return nil, &fs.PathError{Op: "openat", Path: name, Err: ErrChanged}
	}
	return &unixDir{fd: fd, id: id, name: name}, nil
}

func (d *unixDir) openFile(name string, want Identity) (*os.File, objInfo, error) {
	fd, info, err := d.openFD(name, want)
	if err != nil {
		return nil, objInfo{}, err
	}
	return os.NewFile(uintptr(fd), name), info, nil
}

// openFD is openFile's descriptor: opened, checked to be a regular file
// (and want), not yet wrapped.
func (d *unixDir) openFD(name string, want Identity) (int, objInfo, error) {
	// O_NONBLOCK: a FIFO swapped in must not block the server; the type is
	// checked below and a regular file reads the same either way.
	fd, err := unix.Openat(d.fd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return -1, objInfo{}, &fs.PathError{Op: "openat", Path: name, Err: err}
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return -1, objInfo{}, &fs.PathError{Op: "fstat", Path: name, Err: err}
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		unix.Close(fd)
		return -1, objInfo{}, &fs.PathError{Op: "openat", Path: name, Err: ErrNotRegular}
	}
	info := objInfo{size: st.Size, id: statIdentity(&st)}
	if !want.IsZero() && !info.id.Same(want) {
		unix.Close(fd)
		return -1, objInfo{}, &fs.PathError{Op: "openat", Path: name, Err: ErrChanged}
	}
	return fd, info, nil
}

// readFile opens name as openFile does and reads it whole on the
// descriptor itself: an os.File would ask the descriptor's flags and, since
// it is non-blocking, register it with the runtime poller (two system calls
// a file, for nothing a regular file can use).
func (d *unixDir) readFile(name string, want Identity) ([]byte, objInfo, error) {
	fd, info, err := d.openFD(name, want)
	if err != nil {
		return nil, objInfo{}, err
	}
	defer unix.Close(fd)
	b, err := readAllFD(fd, info.size)
	if err != nil {
		return nil, objInfo{}, &fs.PathError{Op: "read", Path: name, Err: err}
	}
	return b, info, nil
}

// readAllFD is readAllSized on a descriptor: size (from fstat) is a hint,
// the content is what is read until end of file.
func readAllFD(fd int, size int64) ([]byte, error) {
	if size <= 0 || size > 1<<30 {
		size = 512
	}
	buf := make([]byte, 0, size+1)
	for {
		n, err := unix.Read(fd, buf[len(buf):cap(buf)])
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return buf, nil
		}
		buf = buf[:len(buf)+n]
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}
	}
}

func (d *unixDir) readlink(name string) (string, error) {
	for size := 256; ; size *= 2 {
		buf := make([]byte, size)
		n, err := unix.Readlinkat(d.fd, name, buf)
		if err != nil {
			return "", &fs.PathError{Op: "readlinkat", Path: name, Err: err}
		}
		if n < size {
			return string(buf[:n]), nil
		}
	}
}

func (d *unixDir) readDir(owned bool) ([]dirent, error) {
	// Listing moves a descriptor's offset. A shared handle (the root) is
	// listed through a description of its own; an owned one through a
	// duplicate of its descriptor (same directory, no path lookup).
	var fd int
	var err error
	if owned {
		fd, err = unix.FcntlInt(uintptr(d.fd), unix.F_DUPFD_CLOEXEC, 0)
	} else {
		fd, err = unix.Openat(d.fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	}
	if err != nil {
		return nil, &fs.PathError{Op: "openat", Path: d.name, Err: err}
	}
	f := os.NewFile(uintptr(fd), d.name)
	defer f.Close()
	// Names and the types the directory reports. A type the file system
	// does not report would be filled in by a path lstat: it stays a hint,
	// because every open re-checks the object it finds.
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := make([]dirent, 0, len(entries))
	for _, e := range entries {
		out = append(out, dirent{name: e.Name(), typ: e.Type()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

func (d *unixDir) scanNames(stop func(string) bool) ([]string, bool, error) {
	fd, err := unix.Openat(d.fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, false, &fs.PathError{Op: "openat", Path: d.name, Err: err}
	}
	f := os.NewFile(uintptr(fd), d.name)
	defer f.Close()
	return scanFile(f, stop)
}

func (d *unixDir) identity() (Identity, error) { return d.id, nil }

func (d *unixDir) lstatInfo(name string) (fs.FileInfo, error) {
	fi, _, err := d.lstatBoth(name)
	return fi, err
}

func (d *unixDir) lstatBoth(name string) (fs.FileInfo, objInfo, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(d.fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, objInfo{}, &fs.PathError{Op: "lstat", Path: name, Err: err}
	}
	return statInfo(name, &st), objInfo{mode: modeOf(&st), size: st.Size, id: statIdentity(&st)}, nil
}

func (d *unixDir) stat() (fs.FileInfo, error) {
	fd, err := unix.FcntlInt(uintptr(d.fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "dup", Path: d.name, Err: err}
	}
	f := os.NewFile(uintptr(fd), d.name)
	defer f.Close()
	return f.Stat()
}

func (d *unixDir) close() error { return unix.Close(d.fd) }

func pathIdentity(path string) (Identity, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return Identity{}, &fs.PathError{Op: "stat", Path: path, Err: err}
	}
	return statIdentity(&st), nil
}

func fileIdentity(f *os.File, _ fs.FileInfo) (Identity, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return Identity{}, err
	}
	return statIdentity(&st), nil
}

func openNoBlock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
