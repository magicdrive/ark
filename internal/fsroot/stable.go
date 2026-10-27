package fsroot

import (
	"io"
	"io/fs"
	"sync/atomic"
	"time"
)

// Stable reads: a file read whole between two fstat calls of its open
// descriptor, for content whose consistency matters more than its
// availability (the .arkignore rules, the secret policy).
//
// A stable read succeeds only if both fstat calls report the same object
// (device and inode, or volume and file index), the same size, the same
// modification time and the same status-change time (ctime; the change time
// on Windows), and the bytes read up to end of file are exactly that size.
// Otherwise the file changed while it was read: ErrChanged, and no bytes.
//
// What that proves depends on the timestamps. Every write, truncate, chmod,
// utimes (os.Chtimes) or link change sets the ctime to the current time, and
// nothing but a clock change can set it back: restoring the modification
// time after a rewrite (ABA) changes the ctime. But a file system stamps
// times with a granularity (a kernel tick on Linux, a second on some file
// systems): two changes in the same tick leave the same ctime. Stamp.Settled
// closes that window: a read is settled when the ctime it saw is far enough
// — more than the granularity — from the interval of the read itself, so a
// change during the read would have set a ctime the second fstat must see.
//
// What is still not detected (no reader alone can detect it):
//   - a single write(2) that began, and stamped the ctime, before the read
//     and still copies bytes in place, without changing the size, after it;
//   - writes through a shared memory mapping (the times are updated lazily);
//   - a system clock set backwards during the read, file systems that do
//     not maintain ctime (or whose server clock is not this one: network
//     file systems), and timestamps coarser than the granularity assumed
//     (see grain).
//
// Writers that need their readers never to see a partial file must replace
// it atomically: write a temporary file and rename(2) it over the old one.
// A stable read of a file replaced by rename sees either object whole.

// Stamp is what fstat reported of a regular file a stable read read: its
// identity, size and times (nanoseconds since the Unix epoch), and the
// interval of the read (wall clock).
type Stamp struct {
	ID    Identity
	Size  int64
	Mtime int64
	Ctime int64 // status-change time (Unix), change time (Windows)

	read       bool  // taken by a stable read
	start, end int64 // the read's interval: before the first fstat, after the second
}

// Same reports whether two stamps describe the same object with the same
// size and times.
func (a Stamp) Same(b Stamp) bool {
	return a.ID.Same(b.ID) && a.Size == b.Size && a.Mtime == b.Mtime && a.Ctime == b.Ctime
}

const (
	// fineGrain: a file system that records sub-second times records the
	// time of a kernel tick at worst (Linux: at most 10ms at HZ=100); a
	// tenfold margin.
	fineGrain = int64(100 * time.Millisecond)
	// coarseGrain: whole-second times (ext3, HFS+, ext4 with small inodes;
	// FAT's two seconds), with the same margin.
	coarseGrain = int64(2*time.Second + 100*time.Millisecond)
)

// grain is the timestamp granularity assumed for s: whole seconds when
// neither time has a fraction (a sub-second time that falls on a whole
// second costs a wait, never a wrong answer), else sub-second.
func (s Stamp) grain() int64 {
	const sec = int64(time.Second)
	if s.Ctime%sec == 0 && s.Mtime%sec == 0 {
		return coarseGrain
	}
	return fineGrain
}

// Settled reports whether a stable read that took s would have seen any
// change made during it: the ctime (or, where the platform has none, the
// modification time) is more than the granularity before the read began or
// after it ended. A read whose file changed just before it (or whose clock
// disagrees with the file's by about the granularity) is not settled: read
// again after SettleTime.
func (s Stamp) Settled() bool {
	if !s.read {
		return false
	}
	c, g := s.changed(), s.grain()
	return c < s.start-g || c > s.end+g
}

// SettleTime is when a read of the file, unchanged, would be settled.
func (s Stamp) SettleTime() time.Time {
	return time.Unix(0, s.changed()+s.grain()+1)
}

func (s Stamp) changed() int64 {
	if s.Ctime != 0 {
		return s.Ctime
	}
	return s.Mtime
}

// readHook, set by tests only (SetReadHookForTest), is called before every
// read system call of a stable read: the file's identity and the bytes read
// so far.
var readHook atomic.Pointer[func(id Identity, off int)]

// SetReadHookForTest installs h before every read system call of stable
// reads (nil removes it) and returns a function that restores the previous
// one. Not an API: for tests that change a file while it is read.
func SetReadHookForTest(h func(id Identity, off int)) (restore func()) {
	var p *func(Identity, int)
	if h != nil {
		p = &h
	}
	old := readHook.Swap(p)
	return func() { readHook.Store(old) }
}

// readStable reads to end of file through read, sized for pre's size, and
// checks the result against post (stat after the read): the same stamp,
// exactly that many bytes. stat reports the descriptor's stamp.
func readStable(id Identity, read func([]byte) (int, error), stat func() (Stamp, error), name string) ([]byte, Stamp, error) {
	start := time.Now().UnixNano()
	pre, err := stat()
	if err != nil {
		return nil, Stamp{}, err
	}
	if !id.IsZero() && !pre.ID.Same(id) {
		return nil, Stamp{}, &fs.PathError{Op: "read", Path: name, Err: ErrChanged}
	}
	size := pre.Size
	if size < 0 || size > 1<<30 {
		size = 512
	}
	buf := make([]byte, 0, size+1)
	for {
		if h := readHook.Load(); h != nil {
			(*h)(pre.ID, len(buf))
		}
		n, err := read(buf[len(buf):cap(buf)])
		if n > 0 {
			buf = buf[:len(buf)+n]
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, Stamp{}, &fs.PathError{Op: "read", Path: name, Err: err}
		}
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}
	}
	post, err := stat()
	if err != nil {
		return nil, Stamp{}, err
	}
	end := time.Now().UnixNano()
	if !pre.Same(post) || int64(len(buf)) != post.Size {
		return nil, Stamp{}, &fs.PathError{Op: "read", Path: name, Err: ErrChanged}
	}
	post.read, post.start, post.end = true, start, end
	return buf, post, nil
}

// ReadStable reads the entry as a regular file in a stable read (see
// Stamp): its content and stamp, or ErrChanged if it changed while read.
func (e Entry) ReadStable() ([]byte, Stamp, error) {
	if e.IsSymlink() {
		return nil, Stamp{}, &fs.PathError{Op: "open", Path: e.rel, Err: ErrSymlink}
	}
	var b []byte
	var st Stamp
	err := e.dir.use(func(h handle) error {
		var err error
		b, st, err = h.readFileStable(e.name, Identity{})
		return err
	})
	if err != nil {
		return nil, Stamp{}, &fs.PathError{Op: "open", Path: e.rel, Err: err}
	}
	return b, st, nil
}

// ReadFileStable is ReadFileChecked in a stable read (see Stamp).
func (t *Tree) ReadFileStable(rel string, check func(Resolved) error) ([]byte, Stamp, error) {
	r, f, err := t.resolve(rel, true)
	if err != nil {
		return nil, Stamp{}, err
	}
	if f == nil {
		return nil, Stamp{}, &fs.PathError{Op: "read", Path: r.logical, Err: ErrNotRegular}
	}
	defer f.Close()
	if check != nil {
		if err := check(r); err != nil {
			return nil, Stamp{}, &fs.PathError{Op: "read", Path: r.logical, Err: err}
		}
	}
	return readStableFile(f, r.info.id, r.logical)
}

// ReadResolvedStable reads the regular file r (opened as Open opens it) in
// a stable read (see Stamp).
func (t *Tree) ReadResolvedStable(r Resolved) ([]byte, Stamp, error) {
	f, err := t.Open(r)
	if err != nil {
		return nil, Stamp{}, err
	}
	defer f.Close()
	return readStableFile(f, r.info.id, r.logical)
}
