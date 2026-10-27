package fsroot

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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

// Writers rewriting in place (and by rename) as fast as they can, readers
// reading (in bursts): a settled read is always one whole version written.
func TestReadStable_ConcurrentWriters(t *testing.T) {
	versions := []string{"x.txt\n", "*\n", "x.txt\nok.txt\nanother-longer-line.txt\n", "yy.txt\n"}
	tr, p := stableTree(t, versions[0])
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; !stop.Load(); n++ {
			v := versions[n%len(versions)]
			if n%5 == 4 {
				tmp := p + ".tmp"
				if os.WriteFile(tmp, []byte(v), 0o644) == nil {
					os.Rename(tmp, p)
				}
			} else if f, err := os.OpenFile(p, os.O_WRONLY|os.O_TRUNC, 0); err == nil {
				f.WriteString(v)
				f.Close()
			}
			if n%200 == 199 {
				time.Sleep(150 * time.Millisecond) // bursts: settled reads between them
			}
		}
	}()
	valid := map[string]bool{"": true} // between truncate and write
	for _, v := range versions {
		valid[v] = true
	}
	var reads, changed, unsettled, settled int
	end := time.Now().Add(time.Second)
	for time.Now().Before(end) {
		reads++
		b, st, err := tr.ReadFileStable("rules", nil)
		switch {
		case errors.Is(err, ErrChanged):
			changed++
		case err != nil:
			// Between rename steps the name may briefly be missing.
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		case !st.Settled():
			unsettled++
		default:
			settled++
			if !valid[string(b)] {
				t.Errorf("settled read of %q, never written", b)
			}
		}
	}
	stop.Store(true)
	wg.Wait()
	t.Logf("%d reads: %d changed, %d unsettled, %d settled", reads, changed, unsettled, settled)
	if settled == 0 {
		t.Error("no read settled between the bursts")
	}
}
