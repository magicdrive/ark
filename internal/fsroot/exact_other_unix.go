//go:build unix && !darwin && !linux

package fsroot

// exactNames: not known on this system — d.exact is never set to exactYes —
// so every name is canonicalized.
func (d *unixDir) exactNames(string) bool { return d.exact.Load() == exactYes }
