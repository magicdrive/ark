//go:build windows

package fsroot

import (
	"io/fs"
	"os"
	"sort"
)

// Windows: os.Root handles (NtCreateFile relative to a directory handle).
// os.Root follows symlinks and junctions inside the root by itself, so every
// open is bracketed: the one component is lstat'ed first (it must be a plain
// directory or regular file — a symlink or a reparse point such as a
// junction is never entered), then opened, and the object opened must be
// the object lstat'ed (os.SameFile: volume serial number and file index).
// A swap between the two fails with ErrChanged.
//
// Not verified on Windows hardware: this design rests on os.Root's
// documented behaviour and os.SameFile.

type winDir struct {
	r  *os.Root
	fi fs.FileInfo // of the directory opened
}

func openRootDir(path string) (handle, error) {
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	fi, err := r.Stat(".")
	if err != nil {
		r.Close()
		return nil, err
	}
	return &winDir{r: r, fi: fi}, nil
}

func infoOf(fi fs.FileInfo) objInfo {
	return objInfo{mode: fi.Mode() & fs.ModeType, size: fi.Size(), id: Identity{fi: fi}}
}

// plain reports whether fi is neither a symlink nor another reparse point.
func plain(fi fs.FileInfo) bool {
	return fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0
}

func (d *winDir) lstat(name string) (objInfo, error) {
	fi, err := d.r.Lstat(name)
	if err != nil {
		return objInfo{}, err
	}
	return infoOf(fi), nil
}

func (d *winDir) openDir(name string, want Identity) (handle, error) {
	before, err := d.r.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !plain(before) || !before.IsDir() {
		return nil, &fs.PathError{Op: "open", Path: name, Err: ErrSymlink}
	}
	sub, err := d.r.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := sub.Stat(".")
	if err != nil || !os.SameFile(before, after) || (!want.IsZero() && !(Identity{fi: after}).Same(want)) {
		sub.Close()
		return nil, &fs.PathError{Op: "open", Path: name, Err: ErrChanged}
	}
	return &winDir{r: sub, fi: after}, nil
}

func (d *winDir) openFile(name string, want Identity) (*os.File, objInfo, error) {
	before, err := d.r.Lstat(name)
	if err != nil {
		return nil, objInfo{}, err
	}
	if !plain(before) || !before.Mode().IsRegular() {
		return nil, objInfo{}, &fs.PathError{Op: "open", Path: name, Err: ErrNotRegular}
	}
	f, err := d.r.Open(name)
	if err != nil {
		return nil, objInfo{}, err
	}
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || (!want.IsZero() && !(Identity{fi: after}).Same(want)) {
		f.Close()
		return nil, objInfo{}, &fs.PathError{Op: "open", Path: name, Err: ErrChanged}
	}
	return f, infoOf(after), nil
}

func (d *winDir) readlink(name string) (string, error) { return d.r.Readlink(name) }

func (d *winDir) readDir(bool) ([]dirent, error) {
	entries, err := fs.ReadDir(d.r.FS(), ".")
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

func (d *winDir) scanNames(stop func(string) bool) ([]string, bool, error) {
	f, err := d.r.Open(".")
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	return scanFile(f, stop)
}

func (d *winDir) identity() (Identity, error) { return Identity{fi: d.fi}, nil }

func (d *winDir) stat() (fs.FileInfo, error) { return d.fi, nil }

// exactNames: NTFS names are case-insensitive by default (and a directory
// may be made case-sensitive, or reached by an 8.3 short name): every name
// is canonicalized.
func (d *winDir) exactNames(string) bool { return false }

func (d *winDir) close() error { return d.r.Close() }

func pathIdentity(path string) (Identity, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Identity{}, err
	}
	return Identity{fi: fi}, nil
}

func fileIdentity(_ *os.File, fi fs.FileInfo) (Identity, error) { return Identity{fi: fi}, nil }

func openNoBlock(path string) (*os.File, error) { return os.OpenFile(path, os.O_RDONLY, 0) }
