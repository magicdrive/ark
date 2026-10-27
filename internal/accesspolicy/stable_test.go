package accesspolicy

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/fsroot"
)

// Rule files changed while they are read (fsroot.SetReadHookForTest: between
// the read system calls of one reading). A snapshot holds, for each rule
// file, one version written, whole — or the build fails (ErrRuleChanged):
// an inconsistent capture is never published.

// withReadHook runs h before every read system call of a stable read of
// the file rel of tr (its identity now).
func withReadHook(t *testing.T, tr *fsroot.Tree, rel string, h func(off int)) {
	t.Helper()
	r, err := tr.Resolve(rel)
	if err != nil {
		t.Fatal(err)
	}
	id := r.Identity()
	restore := fsroot.SetReadHookForTest(func(got fsroot.Identity, off int) {
		if got.Same(id) {
			h(off)
		}
	})
	t.Cleanup(restore)
}

func rewrite(t testing.TB, path, body string) {
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

// scopedRules returns the bytes a scoped snapshot captured for dir.
func scopedRules(s *Snapshot, dir string) (string, bool) {
	s.scope.mu.Lock()
	defer s.scope.mu.Unlock()
	c, ok := s.scope.caps[dir]
	return string(c.file.Data), ok
}

// tornBuilds builds a full and a scoped snapshot of a tree whose
// p/d/.arkignore (written "x.txt\n" and "*\n") is torn by tear at every
// reading, and fails on a capture of bytes never written.
func tornBuilds(t *testing.T, tear func(path string, mtime time.Time, off int)) {
	w := ruleRepo(t)
	path := w.path("repo/p/d/.arkignore")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime := fi.ModTime()
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	withReadHook(t, tr, "p/d/.arkignore", func(off int) { tear(path, mtime, off) })
	valid := map[string]bool{"x.txt\n": true, "*\n": true}

	s, err := Build(tr, Options{})
	switch {
	case errors.Is(err, ErrRuleChanged):
	case err != nil:
		t.Fatal(err)
	default:
		if got := string(s.rules["p/d/.arkignore"]); !valid[got] {
			t.Errorf("full snapshot: p/d/.arkignore captured %q, never written", got)
		}
	}

	sc := scopedOf(t, tr)
	err = decide(t, sc, "p/d/x.txt")
	if err != nil && !errors.Is(err, ErrExcluded) && !errors.Is(err, ErrRuleChanged) {
		t.Fatal(err)
	}
	if got, ok := scopedRules(sc, "p/d"); ok && !valid[got] {
		t.Errorf("scoped snapshot: p/d/.arkignore captured %q (decision %v), never written", got, err)
	}
}

// The failure of CI run 38658088692 (TestA4_ConcurrentMultiLevelMutation,
// macOS: `p/d/.arkignore: captured "*\ntxt\n", never written`), made
// deterministic: the file is truncated and rewritten in place between the
// read system calls — "*\n" read first, then the rest of "x.txt\n" — at the
// capture and again at the second reading, which therefore finds the same
// bytes and the same object. Six bytes: the size fstat reports.
func TestA4_TornReadAtBothReadings(t *testing.T) {
	tornBuilds(t, func(path string, _ time.Time, off int) {
		switch off {
		case 0:
			rewrite(t, path, "*\n")
		case 2:
			rewrite(t, path, "x.txt\n")
		}
	})
}

// The same, with the modification time put back after each tear (the
// size, the identity and the modification time are those of the file
// before): only the change time tells, and the content.
func TestA4_TornReadTimesRestored(t *testing.T) {
	tornBuilds(t, func(path string, mtime time.Time, off int) {
		switch off {
		case 0:
			rewrite(t, path, "*\n")
			os.Chtimes(path, mtime, mtime)
		case 2:
			rewrite(t, path, "x.txt\n")
			os.Chtimes(path, mtime, mtime)
		}
	})
}

// Torn at the first reading only: the build reads again and captures the
// version the file holds.
func TestA4_TornOnceThenStable(t *testing.T) {
	w := ruleRepo(t)
	path := w.path("repo/p/d/.arkignore")
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	var n atomic.Int32
	withReadHook(t, tr, "p/d/.arkignore", func(off int) {
		if n.Load() > 0 {
			return
		}
		switch off {
		case 0:
			rewrite(t, path, "*\n")
		case 2:
			rewrite(t, path, "x.txt\n")
			n.Add(1)
		}
	})
	s := snapshotOf(t, tr)
	if got := string(s.rules["p/d/.arkignore"]); got != "x.txt\n" {
		t.Errorf("captured %q, want %q", got, "x.txt\n")
	}
}

// Same size, same identity, the modification time restored, between the
// capture and the second reading: the new version, never the old one mixed
// in.
func TestA4_SameSizeTimesRestoredBetweenReadings(t *testing.T) {
	w := ruleRepo(t)
	path := w.path("repo/d/.arkignore")
	fi, _ := os.Stat(path)
	mtime := fi.ModTime()
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	done := false
	withHook(t, func(event, rel string) {
		if event == "verify" && !done {
			done = true
			rewrite(t, path, "y.txt\n") // as long as "x.txt\n"
			os.Chtimes(path, mtime, mtime)
		}
	})
	s := snapshotOf(t, tr)
	if got := string(s.rules["d/.arkignore"]); got != "y.txt\n" {
		t.Errorf("captured %q, want the rewrite %q", got, "y.txt\n")
	}
}

// Replaced by rename (the atomic replacement writers should use) during the
// walk: the new version, whole.
func TestA4_RenameReplacementDuringBuild(t *testing.T) {
	w := ruleRepo(t)
	path := w.path("repo/p/d/.arkignore")
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	done := false
	withHook(t, func(event, rel string) {
		if event == "rule" && rel == "p/d/.arkignore" && !done {
			done = true
			if err := os.WriteFile(path+".tmp", []byte("*\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(path+".tmp", path); err != nil {
				t.Fatal(err)
			}
		}
	})
	s := snapshotOf(t, tr)
	if got := string(s.rules["p/d/.arkignore"]); got != "*\n" {
		t.Errorf("captured %q, want the replacement %q", got, "*\n")
	}
}

// Rewritten several times in quick succession (sizes up and down) while the
// build runs: the last version.
func TestA4_RapidRewritesDuringBuild(t *testing.T) {
	w := ruleRepo(t)
	path := w.path("repo/d/.arkignore")
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	done := false
	withHook(t, func(event, rel string) {
		if event == "rule" && rel == "d/.arkignore" && !done {
			done = true
			for _, v := range []string{"*\n", "x.txt\nok.txt\nmore.txt\n", "\n", "ok.txt\n"} {
				rewrite(t, path, v)
			}
		}
	})
	s := snapshotOf(t, tr)
	if got := string(s.rules["d/.arkignore"]); got != "ok.txt\n" {
		t.Errorf("captured %q, want the last version %q", got, "ok.txt\n")
	}
}

// confirms: a second reading confirms the first only if it found the same
// stamp and bytes and was settled.
func TestConfirms(t *testing.T) {
	w := ruleRepo(t)
	path := w.path("repo/d/.arkignore")
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	read := func() ([]byte, fsroot.Stamp) {
		t.Helper()
		b, st, err := tr.ReadFileStable("d/.arkignore", nil)
		if err != nil {
			t.Fatal(err)
		}
		return b, st
	}
	b1, st1 := read() // just written: not settled
	if st1.Settled() {
		t.Skip("the file was written long enough ago to be settled")
	}
	if confirms(st1, b1, st1, b1) {
		t.Error("an unsettled reading confirms")
	}
	time.Sleep(time.Until(st1.SettleTime()))
	b2, st2 := read()
	if !st2.Settled() || !confirms(st2, b2, st1, b1) {
		t.Errorf("settled reading of the unchanged file does not confirm (settled=%v)", st2.Settled())
	}
	if confirms(st2, []byte("other\n"), st1, b1) {
		t.Error("other bytes confirm")
	}
	// Rewritten with the same bytes and the times restored: another ctime.
	fi, _ := os.Stat(path)
	time.Sleep(25 * time.Millisecond)
	rewrite(t, path, string(b1))
	os.Chtimes(path, fi.ModTime(), fi.ModTime())
	time.Sleep(time.Until(st1.SettleTime().Add(150 * time.Millisecond)))
	b3, st3 := read()
	if confirms(st3, b3, st1, b1) && !coarseTimes(st3) {
		t.Error("a rewrite (times restored) between the readings confirms")
	}
}

// coarseTimes reports a file system with whole-second times, where two
// changes 25ms apart may share a ctime.
func coarseTimes(st fsroot.Stamp) bool {
	return st.Ctime%int64(time.Second) == 0 && st.Mtime%int64(time.Second) == 0
}

// Writers rewriting rule files at several levels in place, in bursts, while
// scoped snapshots decide paths and full snapshots are built: every capture
// is one version written.
func TestA4_ConcurrentMutationScoped(t *testing.T) {
	w := ruleRepo(t)
	versions := map[string][]string{
		".":   {"root-secret.txt\n", "root-secret.txt\nother-a.txt\nother-b.txt\n"},
		"d":   {"x.txt\n", "x.txt\nok.txt\nanother-long-pattern-to-change-size.txt\n"},
		"p/d": {"x.txt\n", "*\n"},
	}
	file := func(dir string) string {
		if dir == "." {
			return w.path("repo/.arkignore")
		}
		return w.path("repo/" + dir + "/.arkignore")
	}
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; !stop.Load(); n++ {
			for dir, v := range versions {
				if f, err := os.OpenFile(file(dir), os.O_WRONLY|os.O_TRUNC, 0); err == nil {
					f.WriteString(v[n%2])
					f.Close()
				}
			}
			if n%300 == 299 {
				time.Sleep(150 * time.Millisecond)
			}
		}
	}()
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	var decided, built, changed int
	end := time.Now().Add(time.Second)
	for time.Now().Before(end) {
		s := scopedOf(t, tr)
		err := decide(t, s, "p/d/x.txt")
		if err == nil || errors.Is(err, ErrExcluded) {
			decided++
		} else if errors.Is(err, ErrRuleChanged) {
			changed++
		} else {
			t.Fatal(err)
		}
		for dir, v := range versions {
			if got, ok := scopedRules(s, dir); ok && got != v[0] && got != v[1] && got != "" {
				t.Errorf("scoped %s: captured %q, never written", dir, got)
			}
		}
		full, err := Build(tr, Options{})
		if errors.Is(err, ErrRuleChanged) {
			changed++
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		built++
		for dir, v := range versions {
			rel := ".arkignore"
			if dir != "." {
				rel = dir + "/.arkignore"
			}
			if got := string(full.rules[rel]); got != v[0] && got != v[1] && got != "" {
				t.Errorf("full %s: captured %q, never written", rel, got)
			}
		}
	}
	stop.Store(true)
	wg.Wait()
	t.Logf("%d decided (scoped), %d built (full), %d refused as changing", decided, built, changed)
}

// Rewritten between the two readings with the same bytes and the
// modification time put back: the second reading does not confirm the first
// (another change time), so the capture is read again — by Build, by a
// scoped snapshot's chain capture, and by its capture from a walk's handles.
// The bytes are the same either way; what is tested is that the stamp, not
// only the bytes, is compared at each second reading.
func TestA4_SameBytesNewStampIsReadAgain(t *testing.T) {
	for _, mode := range []string{"full", "chain", "walk"} {
		w := ruleRepo(t)
		path := w.path("repo/d/.arkignore")
		fi, _ := os.Stat(path)
		mtime := fi.ModTime()
		tr := pinTree(t, w.path("repo"), fsroot.Options{})
		verifies := 0
		withHook(t, func(event, rel string) {
			if event != "verify" || (mode != "full" && rel != "d") {
				return
			}
			verifies++
			if verifies == 1 {
				time.Sleep(25 * time.Millisecond) // another timestamp tick
				rewrite(t, path, "x.txt\n")       // the same bytes
				os.Chtimes(path, mtime, mtime)
			}
		})
		switch mode {
		case "full":
			if s := snapshotOf(t, tr); string(s.rules["d/.arkignore"]) != "x.txt\n" {
				t.Errorf("full: captured %q", s.rules["d/.arkignore"])
			}
		case "chain":
			if err := decide(t, scopedOf(t, tr), "d/x.txt"); !errors.Is(err, ErrExcluded) {
				t.Errorf("chain: d/x.txt %v", err)
			}
		case "walk":
			s := scopedOf(t, tr)
			var got error
			tr.Walk(".", func(e fsroot.Entry) error {
				if err := s.CheckEntry(e); e.Rel() == "d/x.txt" {
					got = err
				}
				return nil
			})
			if !errors.Is(got, ErrExcluded) {
				t.Errorf("walk: d/x.txt %v", got)
			}
		}
		if verifies < 2 {
			t.Errorf("%s: a second reading with another stamp confirmed the first (%d readings)", mode, verifies)
		}
	}
}
