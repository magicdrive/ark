//go:build linux

package fsroot

import (
	"errors"

	"golang.org/x/sys/unix"
)

// FS_CASEFOLD_FL (linux/fs.h): a casefolded (case-insensitive) directory.
const fsCasefoldFL = 0x40000000

// exactFS are the file systems whose lookups are byte-exact unless a
// directory is casefolded. Any other (vfat, exfat, ntfs3, cifs, nfs, zfs,
// overlay, fuse, ...) may fold names: its directories are canonicalized.
var exactFS = map[uint32]bool{
	0xEF53:     true, // ext2/3/4
	0x58465342: true, // xfs
	0x9123683E: true, // btrfs
	0x01021994: true, // tmpfs
	0xF2F52010: true, // f2fs
	0x858458f6: true, // ramfs
}

// exactNames: the directory is on a file system listed in exactFS and is not
// casefolded.
func (d *unixDir) exactNames(string) bool {
	v := d.exact.Load()
	if v == 0 {
		v = exactNo
		var st unix.Statfs_t
		if unix.Fstatfs(d.fd, &st) == nil && exactFS[uint32(st.Type)] {
			flags, err := unix.IoctlGetUint32(d.fd, unix.FS_IOC_GETFLAGS)
			switch {
			case err == nil && flags&fsCasefoldFL == 0,
				errors.Is(err, unix.ENOTTY), errors.Is(err, unix.EOPNOTSUPP), errors.Is(err, unix.EINVAL):
				// No casefold flag, or no flags at all (no casefolding).
				v = exactYes
			}
		}
		d.exact.Store(v)
	}
	return v == exactYes
}
