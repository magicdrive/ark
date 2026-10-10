//go:build unix

package fsroot

import (
	"io/fs"
	"os"
	"sort"

	"golang.org/x/sys/unix"
)

// Unix: directory descriptors. Every open is openat(2) relative to a
// directory descriptor with O_NOFOLLOW, so the kernel refuses a symlink in
// the one component named, atomically; nothing is resolved by path.

type unixDir struct {
	fd   int
	id   Identity // of the directory opened (fstat when opened)
	name string   // for messages and os.File names only
}

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
	// O_NONBLOCK: a FIFO swapped in must not block the server; the type is
	// checked below and a regular file reads the same either way.
	fd, err := unix.Openat(d.fd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, objInfo{}, &fs.PathError{Op: "openat", Path: name, Err: err}
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return nil, objInfo{}, &fs.PathError{Op: "fstat", Path: name, Err: err}
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		unix.Close(fd)
		return nil, objInfo{}, &fs.PathError{Op: "openat", Path: name, Err: ErrNotRegular}
	}
	info := objInfo{size: st.Size, id: statIdentity(&st)}
	if !want.IsZero() && !info.id.Same(want) {
		unix.Close(fd)
		return nil, objInfo{}, &fs.PathError{Op: "openat", Path: name, Err: ErrChanged}
	}
	return os.NewFile(uintptr(fd), name), info, nil
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

func (d *unixDir) identity() (Identity, error) { return d.id, nil }

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
