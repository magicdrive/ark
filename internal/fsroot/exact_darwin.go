//go:build darwin

package fsroot

import "golang.org/x/sys/unix"

// _PC_CASE_SENSITIVE (sys/unistd.h).
const pcCaseSensitive = 11

// exactNames: an ASCII name on a case-sensitive volume. APFS and HFS+ ignore
// Unicode normalization even when case-sensitive, so a name with non-ASCII
// characters is always canonicalized.
func (d *unixDir) exactNames(name string) bool {
	if !isASCII(name) {
		return false
	}
	v := d.exact.Load()
	if v == 0 {
		v = exactNo
		if n, err := unix.Fpathconf(d.fd, pcCaseSensitive); err == nil && n == 1 {
			v = exactYes
		}
		d.exact.Store(v)
	}
	return v == exactYes
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
