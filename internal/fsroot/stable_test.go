package fsroot

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Stable reads (stable.go). The checks of readStable are tested with a fake
// descriptor (deterministic on every platform and file system); the
// timestamps a real file system keeps, with files changed while they are
// read (SetReadHookForTest).

// fakeFile is a descriptor whose stat results and read chunks are scripted.
type fakeFile struct {
	stamps []Stamp // returned by successive stats (the last repeats)
	chunks []string
	nstat  int
}

func (f *fakeFile) stat() (Stamp, error) {
	s := f.stamps[min(f.nstat, len(f.stamps)-1)]
	f.nstat++
	return s, nil
}

func (f *fakeFile) read(p []byte) (int, error) {
	if len(f.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, f.chunks[0])
	f.chunks[0] = f.chunks[0][n:]
	if f.chunks[0] == "" {
		f.chunks = f.chunks[1:]
	}
	return n, nil
}

func stampN(size int64, mtime, ctime int64) Stamp {
	return Stamp{ID: Identity{dev: 1, ino: 2}, Size: size, Mtime: mtime, Ctime: ctime}
}

func TestReadStable_Checks(t *testing.T) {
	old := time.Now().Add(-time.Hour).UnixNano() + 123
	cases := []struct {
		name   string
		stamps []Stamp
		chunks []string
		ok     bool
	}{
		{"unchanged", []Stamp{stampN(6, old, old)}, []string{"x.txt\n"}, true},
		{"read in several calls", []Stamp{stampN(6, old, old)}, []string{"x.", "txt", "\n"}, true},
		// The bytes read are not the size both stats report: the file
		// was truncated and rewritten within one timestamp tick.
		{"short read, same stamps", []Stamp{stampN(6, old, old)}, []string{"*\n"}, false},
		{"long read, same stamps", []Stamp{stampN(2, old, old)}, []string{"x.txt\n"}, false},
		// Torn ("*\n" then the rest of "x.txt\n"): the size matches, the
		// times do not.
		{"ctime changed", []Stamp{stampN(6, old, old), stampN(6, old, old+1)}, []string{"*\n", "txt\n"}, false},
		{"mtime changed", []Stamp{stampN(6, old, old), stampN(6, old+1, old)}, []string{"x.txt\n"}, false},
		{"size changed", []Stamp{stampN(6, old, old), stampN(2, old, old)}, []string{"x.txt\n"}, false},
		{"another object", []Stamp{stampN(6, old, old), {ID: Identity{dev: 1, ino: 3}, Size: 6, Mtime: old, Ctime: old}}, []string{"x.txt\n"}, false},
	}
	for _, c := range cases {
		f := &fakeFile{stamps: c.stamps, chunks: append([]string(nil), c.chunks...)}
		b, st, err := readStable(Identity{}, f.read, f.stat, "f")
		if c.ok {
			if err != nil || string(b) != "x.txt\n" || !st.Settled() {
				t.Errorf("%s: %q %v settled=%v", c.name, b, err, st.Settled())
			}
			continue
		}
		if !errors.Is(err, ErrChanged) || b != nil {
			t.Errorf("%s: %q, %v; want ErrChanged and no bytes", c.name, b, err)
		}
	}
	// The object opened must be the one asked for.
	f := &fakeFile{stamps: []Stamp{stampN(6, old, old)}, chunks: []string{"x.txt\n"}}
	if _, _, err := readStable(Identity{dev: 9, ino: 9}, f.read, f.stat, "f"); !errors.Is(err, ErrChanged) {
		t.Errorf("another identity: %v", err)
	}
}

func TestStamp_SameComparesEveryField(t *testing.T) {
	a := stampN(6, 10, 20)
	for name, b := range map[string]Stamp{
		"size":     stampN(7, 10, 20),
		"mtime":    stampN(6, 11, 20),
		"ctime":    stampN(6, 10, 21),
		"identity": {ID: Identity{dev: 1, ino: 3}, Size: 6, Mtime: 10, Ctime: 20},
	} {
		if a.Same(b) || b.Same(a) {
			t.Errorf("%s differs, Same reports true", name)
		}
	}
	if !a.Same(stampN(6, 10, 20)) {
		t.Error("equal stamps not Same")
	}
}

// Settled: a change during the read must have set a ctime the read can tell
// from the one it saw — far enough from the read's interval.
func TestStamp_Settled(t *testing.T) {
	ms := int64(time.Millisecond)
	start := time.Now().UnixNano()/int64(time.Second)*int64(time.Second) + 500*ms // a fraction: fine grain
	end := start + 5*ms
	at := func(ctime int64) Stamp {
		s := stampN(1, ctime, ctime)
		s.read, s.start, s.end = true, start, end
		return s
	}
	cases := []struct {
		name    string
		ctime   int64
		settled bool
	}{
		{"changed long before", start - time.Hour.Nanoseconds() + 7, true},
		{"changed just before the read (one tick)", start - 10*ms, false},
		{"changed within the margin", start - 99*ms, false},
		{"changed beyond the margin", start - 101*ms, true},
		{"changed during the read", start + 2*ms, false},
		{"changed just after (clock skew)", end + 50*ms, false},
		{"far in the future (clock skew)", end + time.Hour.Nanoseconds() + 7, true},
	}
	for _, c := range cases {
		s := at(c.ctime)
		if got := s.Settled(); got != c.settled {
			t.Errorf("%s: settled=%v, want %v", c.name, got, c.settled)
		}
		if !c.settled {
			// A read after SettleTime, of the file unchanged, is settled.
			later := s
			later.start = s.SettleTime().UnixNano()
			later.end = later.start + 5*ms
			if !later.Settled() && c.ctime < end {
				t.Errorf("%s: not settled after SettleTime", c.name)
			}
		}
	}
	// Whole-second times: the margin is two seconds and more.
	whole := start / int64(time.Second) * int64(time.Second)
	s := at(whole - int64(time.Second))
	if s.Settled() {
		t.Error("whole-second ctime one second before the read: settled")
	}
	if !at(whole - 3*int64(time.Second)).Settled() {
		t.Error("whole-second ctime three seconds before the read: not settled")
	}
	if (Stamp{}).Settled() {
		t.Error("a stamp no stable read took is settled")
	}
}

func writeInPlace(t testing.TB, path, body string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(body); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func stableTree(t *testing.T, body string) (*Tree, string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "rules")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := Pin(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })
	return tr, p
}

// coarse reports whether the file system keeps whole-second times (the
// deterministic tests below need sub-second ones).
func coarse(t *testing.T, p string) bool {
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.ModTime().Nanosecond() == 0
}

// The torn read of CI run 38658088692: the file is truncated and rewritten
// in place between the read system calls, so the bytes read are "*\n" and
// then the rest of "x.txt\n" — six bytes, the size fstat reports. The
// modification is two timestamp ticks after the file was last changed (on
// Linux the ctime has the kernel tick's granularity), and the times are
// restored with os.Chtimes: still ErrChanged.
func TestReadStable_TornByInPlaceRewrite(t *testing.T) {
	tr, p := stableTree(t, "x.txt\n")
	if coarse(t, p) {
		t.Skip("whole-second timestamps")
	}
	fi, _ := os.Stat(p)
	mtime := fi.ModTime()
	time.Sleep(25 * time.Millisecond)
	defer SetReadHookForTest(func(_ Identity, off int) {
		switch off {
		case 0:
			writeInPlace(t, p, "*\n")
		case 2:
			writeInPlace(t, p, "x.txt\n")
			os.Chtimes(p, mtime, mtime)
		}
	})()
	b, st, err := tr.ReadFileStable("rules", nil)
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("torn read accepted: %q %v (settled=%v)", b, err, st.Settled())
	}
}

// Same size, same identity, the modification time restored: the change
// time cannot be restored. Rewritten before the read begins, the read is of
// a whole version, but not settled (the change is recent); rewritten during
// it, ErrChanged.
func TestReadStable_SameSizeTimesRestored(t *testing.T) {
	tr, p := stableTree(t, "aaaa\n")
	if coarse(t, p) {
		t.Skip("whole-second timestamps")
	}
	fi, _ := os.Stat(p)
	mtime := fi.ModTime()
	time.Sleep(150 * time.Millisecond)
	b, st, err := tr.ReadFileStable("rules", nil)
	if err != nil || string(b) != "aaaa\n" || !st.Settled() {
		t.Fatalf("unchanged file: %q %v settled=%v", b, err, st.Settled())
	}
	first := st
	time.Sleep(25 * time.Millisecond)
	writeInPlace(t, p, "bbbb\n")
	os.Chtimes(p, mtime, mtime)
	b, st, err = tr.ReadFileStable("rules", nil)
	if err != nil || string(b) != "bbbb\n" {
		t.Fatalf("rewritten file: %q %v", b, err)
	}
	if st.Same(first) {
		t.Error("rewrite with the times restored: the stamp is the same")
	}
	if st.Settled() {
		t.Error("read right after a rewrite: settled")
	}
	time.Sleep(25 * time.Millisecond)
	restore := SetReadHookForTest(func(_ Identity, off int) {
		if off == 0 {
			writeInPlace(t, p, "cccc\n")
			os.Chtimes(p, mtime, mtime)
		}
	})
	b, st, err = tr.ReadFileStable("rules", nil)
	restore()
	if !errors.Is(err, ErrChanged) {
		t.Errorf("rewritten while read: %q %v settled=%v", b, err, st.Settled())
	}
}

// A file replaced by rename while it is read: the descriptor still reads
// the old object; the next read reads the new one whole.
func TestReadStable_RenameReplacement(t *testing.T) {
	tr, p := stableTree(t, "old\n")
	next := p + ".tmp"
	var once sync.Once
	defer SetReadHookForTest(func(_ Identity, off int) {
		once.Do(func() {
			if err := os.WriteFile(next, []byte("brand-new\n"), 0o644); err != nil {
				t.Error(err)
			}
			if err := os.Rename(next, p); err != nil {
				t.Skipf("rename over an open file: %v", err)
			}
		})
	})()
	// The old object loses its last link (which changes its ctime): either
	// ErrChanged or the old content whole, never a mix.
	b, _, err := tr.ReadFileStable("rules", nil)
	if !errors.Is(err, ErrChanged) && (err != nil || string(b) != "old\n") {
		t.Fatalf("first read: %q %v", b, err)
	}
	b, _, err = tr.ReadFileStable("rules", nil)
	if err != nil || string(b) != "brand-new\n" {
		t.Fatalf("second read: %q %v", b, err)
	}
}

// rewriteSameSize rewrites the file at p in place to other bytes of the size
// of old (its content), restoring the modification time: of what fstat
// reports, only the ctime changes.
func rewriteSameSize(p, old string) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, err = f.WriteString("#" + old[1:])
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Chtimes(p, fi.ModTime(), fi.ModTime())
}

// A writer rewriting the file in place (and by rename) in bursts, a reader
// reading it: a settled read is always one whole version written. The
// writer and the reader take turns through channels, not time, so each step
// below happens in every run, whatever the scheduler does:
//
//  1. The reader waits until the file is settled, then lets the writer
//     write a burst and reads while it does: a read may fail with
//     ErrChanged (and no bytes) or, between rename steps, find no file;
//     one that succeeds and is settled (one that began before the burst's
//     first change) is a whole version. How many reads settle is up to the
//     scheduler: none is required.
//  2. The writer stops after the burst's last version and says which. The
//     reader reads it again after its SettleTime: settled, that version.
//  3. The writer resumes while the reader reads the settled file: it writes
//     once between the read's first fstat and its first read system call
//     (SetReadHookForTest), and waits for the next round before writing
//     again. In place, to other bytes of the same size with the
//     modification time restored, so that only the ctime tells: ErrChanged,
//     and no bytes. By rename, to another version: ErrChanged or the old
//     version whole (TestReadStable_RenameReplacement).
func TestReadStable_ConcurrentWriters(t *testing.T) {
	versions := []string{"x.txt\n", "*\n", "x.txt\nok.txt\nanother-longer-line.txt\n", "yy.txt\n"}
	tr, p := stableTree(t, versions[0])
	valid := map[string]bool{"": true} // between truncate and write
	for _, v := range versions {
		valid[v] = true
		valid["#"+v[1:]] = true // rewriteSameSize
	}
	rounds := 8
	if coarse(t, p) {
		rounds = 2 // each settle waits two seconds and more
	}
	const burst = 50
	// A failed write is a test failure, but for Windows, where a write or a
	// rename can be refused for a while (the file is open, or scanned).
	mayFail := runtime.GOOS == "windows"

	write := func(v string, rename bool) error {
		if rename {
			tmp := p + ".tmp"
			if err := os.WriteFile(tmp, []byte(v), 0o644); err != nil {
				return err
			}
			return os.Rename(tmp, p)
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_TRUNC, 0)
		if err != nil {
			return err
		}
		_, err = f.WriteString(v)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}
	// settle waits until a read of the file, unchanged, is settled (the
	// stamp's own bound, as accesspolicy's settle waits; the margin covers
	// the monotonic and wall clocks drifting apart), and returns that read.
	settle := func(r int) ([]byte, Stamp) {
		t.Helper()
		b, st, err := tr.ReadFileStable("rules", nil)
		if err != nil {
			t.Fatalf("round %d: read with the writer stopped: %v", r, err)
		}
		time.Sleep(time.Until(st.SettleTime()) + 10*time.Millisecond)
		again, st2, err := tr.ReadFileStable("rules", nil)
		if err != nil || string(again) != string(b) || !st2.Same(st) || !st2.Settled() {
			t.Fatalf("round %d: read after SettleTime: %q %v (before: %q) same=%v settled=%v", r, again, err, b, st2.Same(st), st2.Settled())
		}
		return again, st2
	}

	start := make(chan struct{})  // reader -> writer: the file is settled, write a burst
	paused := make(chan string)   // writer -> reader: the burst's last version, now unchanged
	resume := make(chan struct{}) // reader -> writer: write once now
	wrote := make(chan error)     // writer -> reader: that write's result
	done := make(chan struct{})   // test end: the writer returns
	var wg sync.WaitGroup
	t.Cleanup(func() { close(done); wg.Wait() }) // also after a failure below
	wg.Add(1)
	go func() {
		defer wg.Done()
		for r := range rounds {
			select {
			case <-start:
			case <-done:
				return
			}
			var last string
			for i := range burst {
				last = versions[(r+i)%len(versions)]
				if err := write(last, i%5 == 4); err != nil && !mayFail {
					t.Errorf("round %d: write: %v", r, err)
				}
			}
			// The burst ends in place, so that the file holds last.
			if err := write(last, false); err != nil {
				t.Errorf("round %d: write: %v", r, err)
			}
			select {
			case paused <- last:
			case <-done:
				return
			}
			select {
			case <-resume:
			case <-done:
				return
			}
			var err error
			if r%2 == 1 {
				err = write(versions[(r+2)%len(versions)], true) // not last
			} else {
				err = rewriteSameSize(p, last)
			}
			select {
			case wrote <- err:
			case <-done:
				return
			}
		}
	}()

	var changed, missing, unsettled, settled int
	for r := range rounds {
		// 1. A settled file, then reads during the burst.
		settle(r)
		start <- struct{}{}
		var last string
	burst:
		for {
			select {
			case last = <-paused:
				break burst
			default:
			}
			b, st, err := tr.ReadFileStable("rules", nil)
			switch {
			case errors.Is(err, ErrChanged):
				if b != nil {
					t.Fatalf("round %d: ErrChanged with bytes %q", r, b)
				}
				changed++
			case errors.Is(err, os.ErrNotExist):
				missing++
			case err != nil:
				t.Fatalf("round %d: %v", r, err)
			case !st.Settled():
				unsettled++
			default:
				settled++
				if !valid[string(b)] {
					t.Fatalf("round %d: settled read of %q, never written", r, b)
				}
			}
		}

		// 2. The writer has stopped (the receive above happens after its
		// last write): the file is last, settled after its SettleTime.
		if b, _ := settle(r); string(b) != last {
			t.Fatalf("round %d: read with the writer stopped: %q, want %q", r, b, last)
		}

		// 3. The writer writes once during the next read.
		var once sync.Once
		var hooked bool
		var werr error
		restore := SetReadHookForTest(func(Identity, int) {
			once.Do(func() {
				hooked = true
				resume <- struct{}{}
				werr = <-wrote
			})
		})
		b, st, err := tr.ReadFileStable("rules", nil)
		restore()
		if !hooked {
			// No read system call: the writer still waits to resume (the
			// cleanup releases it).
			t.Fatalf("round %d: the read made no read system call: %q %v", r, b, err)
		}
		inPlace := r%2 == 0
		if werr != nil && (inPlace || !mayFail) {
			t.Fatalf("round %d: write during the read: %v", r, werr)
		}
		switch {
		case errors.Is(err, ErrChanged) && b == nil:
			changed++
		case !inPlace && err == nil && string(b) == last:
			// By rename: the descriptor read the old object whole (or, on
			// Windows, the rename was refused while the file was open).
		default:
			t.Fatalf("round %d: written during a settled read (in place: %v): %q %v settled=%v", r, inPlace, b, err, st.Settled())
		}
	}
	t.Logf("%d rounds: %d changed, %d missing, %d unsettled, %d settled during bursts", rounds, changed, missing, unsettled, settled)
}
