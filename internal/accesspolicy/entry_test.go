package accesspolicy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/fsroot"
	"github.com/magicdrive/ark/internal/libgitignore"
)

// Entry-based decisions and reads (CheckEntry, ReadEntry). The deterministic
// tests change the tree inside the walk callback — after the entry was
// listed, before it is checked or read — which is the window a concurrent
// writer aims at.

// readIn walks tr and runs at for the entry rel, returning its result.
func readIn(t *testing.T, s *Snapshot, rel string, before func()) ([]byte, error) {
	t.Helper()
	var b []byte
	var err error
	found := false
	s.Tree().Walk(".", func(e fsroot.Entry) error {
		if e.Rel() != rel {
			return nil
		}
		found = true
		if before != nil {
			before()
		}
		b, err = s.ReadEntry(e)
		return fsroot.SkipAll
	})
	if !found {
		t.Fatalf("%s not listed", rel)
	}
	return b, err
}

// T1: the listed file is replaced by another file (renamed over it) after it
// was listed: the object read is the object checked, or nothing.
func TestT1_FileReplacedAfterListing(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "repo/plain.txt", "PLAIN")
	w.write(t, "repo/other.txt", "OTHER_CONTENT")
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	// Replaced before the check: the check examines the new object and the
	// same object is read (a path-legit change, nothing else read).
	b, err := readIn(t, s, "plain.txt", func() {
		mutate(t, "rename over the listed file", os.Rename(w.path("repo/other.txt"), w.path("repo/plain.txt")))
	})
	if err != nil || string(b) != "OTHER_CONTENT" {
		t.Errorf("replaced before the check: %q %v", b, err)
	}
	// Replaced between the check and the open: never the other object.
	w.write(t, "repo/second.txt", "SECOND")
	w.write(t, "repo/swap.txt", "SWAPPED_IN")
	var got []byte
	s.Tree().Walk(".", func(e fsroot.Entry) error {
		if e.Rel() != "second.txt" {
			return nil
		}
		got, err = e.ReadChecked(func(fsroot.Identity) error {
			if err := s.CheckEntry(e); err != nil {
				return err
			}
			// After the decision, before the open.
			mutate(t, "rename over the checked file", os.Rename(w.path("repo/swap.txt"), w.path("repo/second.txt")))
			return nil
		})
		return fsroot.SkipAll
	})
	if strings.Contains(string(got), "SWAPPED_IN") || !errors.Is(err, fsroot.ErrChanged) {
		t.Errorf("replaced after the check: %q %v (want ErrChanged)", got, err)
	}
}

// T2: the listed file is replaced by a symlink to a file outside the root.
func TestT2_FileReplacedByExternalLink(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "repo/plain.txt", "PLAIN")
	w.write(t, "outside/secret.txt", "OUTSIDE_SECRET")
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	b, err := readIn(t, s, "plain.txt", func() {
		os.Remove(w.path("repo/plain.txt"))
		w.link(t, w.path("outside/secret.txt"), "repo/plain.txt")
	})
	if strings.Contains(string(b), "OUTSIDE") || err == nil {
		t.Errorf("read through a link swapped in: %q %v", b, err)
	}
}

// T2b: the listed file is replaced by a symlink to an excluded file inside
// the root. The entry's path is admitted, but the object now at it is a
// link: reading must not follow it (a check of the path followed by a read
// of the path would return the excluded file).
func TestT2b_FileReplacedByLinkToExcluded(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "repo/plain.txt", "PLAIN")
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	b, err := readIn(t, s, "plain.txt", func() {
		os.Remove(w.path("repo/plain.txt"))
		w.link(t, "root-secret.txt", "repo/plain.txt")
	})
	if strings.Contains(string(b), "ROOT_SECRET") || err == nil {
		t.Errorf("read the excluded file through a link swapped in: %q %v", b, err)
	}
}

// T3: the parent directory is replaced while the walk is inside it; the
// walk's handle keeps the original directory, whose identity the snapshot
// recorded — the original file is read or nothing, never the replacement's.
// A walk that enters the replacement finds an identity the snapshot never
// recorded.
func TestT3_ParentReplacedDuringWalk(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "repo/q/f.txt", "ORIGINAL")
	w.write(t, "q-decoy/f.txt", "DECOY")
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	var reads []string
	s.Tree().Walk(".", func(e fsroot.Entry) error {
		switch e.Rel() {
		case "q":
			// Listed, not yet entered: swap the directory.
			mutate(t, "move the directory", os.Rename(w.path("repo/q"), w.path("q-real")))
			mutate(t, "move the decoy in", os.Rename(w.path("q-decoy"), w.path("repo/q")))
		case "q/f.txt":
			b, err := s.ReadEntry(e)
			if err == nil {
				reads = append(reads, string(b))
			} else if !errors.Is(err, ErrDirectoryChanged) {
				t.Errorf("q/f.txt: %v", err)
			}
		}
		return nil
	})
	for _, r := range reads {
		if r == "DECOY" {
			t.Error("read the replacement directory's file")
		}
	}
	if len(reads) > 0 {
		t.Errorf("read %v through a directory the snapshot did not record", reads)
	}
}

// T4: a directory with a nested .arkignore is replaced by one without it
// after the snapshot; its excluded file (copied into the replacement) is
// never admitted: the directory is not the one whose rules were read.
func TestT4_NestedRulesConcealed(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "d-decoy/x.txt", "D_SECRET_COPY")
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	mutate(t, "move the directory", os.Rename(w.path("repo/d"), w.path("d-real")))
	mutate(t, "move the decoy in", os.Rename(w.path("d-decoy"), w.path("repo/d")))
	s.Tree().Walk(".", func(e fsroot.Entry) error {
		if e.Rel() == "d/x.txt" {
			if err := s.CheckEntry(e); !errors.Is(err, ErrDirectoryChanged) {
				t.Errorf("CheckEntry: %v", err)
			}
			if b, err := s.ReadEntry(e); err == nil {
				t.Errorf("ReadEntry read %q", b)
			}
		}
		return nil
	})
}

// T5: an entry of another tree is refused.
func TestT5_EntryOfAnotherTree(t *testing.T) {
	w := ruleRepo(t)
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	other := pinTree(t, w.path("repo"), fsroot.Options{})
	other.Walk(".", func(e fsroot.Entry) error {
		if e.Rel() == "d/ok.txt" {
			if err := s.CheckEntry(e); !errors.Is(err, ErrTreeMismatch) {
				t.Errorf("CheckEntry: %v", err)
			}
			if _, err := s.ReadEntry(e); !errors.Is(err, ErrTreeMismatch) {
				t.Errorf("ReadEntry: %v", err)
			}
		}
		return nil
	})
}

// T6: an entry kept past its callback is refused.
func TestT6_EntryAfterItsCallback(t *testing.T) {
	w := ruleRepo(t)
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	var kept fsroot.Entry
	s.Tree().Walk(".", func(e fsroot.Entry) error {
		if e.Rel() == "d/ok.txt" {
			kept = e
		}
		return nil
	})
	if err := s.CheckEntry(kept); !errors.Is(err, fsroot.ErrClosed) {
		t.Errorf("CheckEntry: %v", err)
	}
	if _, err := s.ReadEntry(kept); !errors.Is(err, fsroot.ErrClosed) {
		t.Errorf("ReadEntry: %v", err)
	}
	// Used from another goroutine after the callback returned: refused,
	// never on a closed descriptor (go test -race).
	var wg sync.WaitGroup
	release := make(chan struct{})
	s.Tree().Walk(".", func(e fsroot.Entry) error {
		if e.Rel() == "d/ok.txt" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-release
				for i := 0; i < 100; i++ {
					if _, err := s.ReadEntry(e); err != nil && !errors.Is(err, fsroot.ErrClosed) {
						t.Errorf("late ReadEntry: %v", err)
					}
				}
			}()
			close(release)
		}
		return nil
	})
	wg.Wait()
}

// T7: the snapshot and the tree are closed while walks, checks and reads
// run: each operation completes or fails with ErrClosed; no data race.
func TestT7_ConcurrentClose(t *testing.T) {
	w := ruleRepo(t)
	tr, err := fsroot.Pin(w.path("repo"), fsroot.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := snapshotOf(t, tr)
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				err := tr.Walk(".", func(e fsroot.Entry) error {
					if e.IsDir() || e.IsSymlink() {
						return nil
					}
					if _, err := s.ReadEntry(e); err != nil && !errors.Is(err, ErrClosed) && !errors.Is(err, ErrExcluded) {
						t.Errorf("%s: %v", e.Rel(), err)
					}
					return nil
				})
				if err != nil && !errors.Is(err, fsroot.ErrClosed) {
					t.Errorf("walk: %v", err)
				}
			}
		}()
	}
	time.Sleep(3 * time.Millisecond) // let the walks start
	s.Close()
	tr.Close()
	wg.Wait()
}

// Symlink entries: decided by where they lead (as Check), never read by
// ReadEntry; a symlink entry that stopped being a symlink is refused.
func TestSymlinkEntries(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "repo/plain.txt", "PLAIN")
	w.link(t, "root-secret.txt", "repo/alias-secret")
	w.link(t, "plain.txt", "repo/alias-plain")
	w.write(t, "outside/o.txt", "O")
	w.link(t, w.path("outside/o.txt"), "repo/ext")
	s := snapshotOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	want := map[string]error{"alias-secret": ErrExcluded, "alias-plain": nil, "ext": fsroot.ErrOutsideRoot}
	s.Tree().Walk(".", func(e fsroot.Entry) error {
		wantErr, ok := want[e.Rel()]
		if !ok {
			return nil
		}
		if err := s.CheckEntry(e); !errors.Is(err, wantErr) && !(wantErr == nil && err == nil) {
			t.Errorf("CheckEntry(%s) = %v, want %v", e.Rel(), err, wantErr)
		}
		if _, err := s.ReadEntry(e); !errors.Is(err, fsroot.ErrSymlink) {
			t.Errorf("ReadEntry(%s) = %v, want ErrSymlink", e.Rel(), err)
		}
		if e.Rel() == "alias-plain" {
			os.Remove(w.path("repo/alias-plain"))
			w.write(t, "repo/alias-plain", "NOW_A_FILE")
			if err := s.CheckEntry(e); err == nil {
				t.Error("a symlink entry that became a file was admitted as the link")
			}
		}
		return nil
	})
}

// Differential: on every entry of a tree, CheckEntry decides as Check of the
// same path resolved, and the rule sets (with and without .gitignore) decide
// as today's IgnoreReader rule sets.
func TestEntryDifferential(t *testing.T) {
	roots := map[string]string{}
	_, root, _ := policyRepo(t)
	roots["policy fixture"] = root
	for _, env := range []string{"ARK_POLICY_BENCH_ROOT", "ARK_POLICY_DIFF_ROOT2"} {
		if r := os.Getenv(env); r != "" {
			roots[filepath.Base(r)] = r
		}
	}
	for name, root := range roots {
		for _, allowExt := range []bool{false, true} {
			tr := pinTree(t, root, fsroot.Options{AllowExternalSymlinks: allowExt})
			s := snapshotOf(t, tr)
			old, err := libgitignore.ReadIgnoreFiles(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			var oldSets, newSets [2]*libgitignore.RuleSet
			for i, git := range []bool{false, true} {
				oldSets[i], _ = old.CompileRuleSet(git)
				newSets[i], _ = s.RuleSet(git)
			}
			n := 0
			tr.Walk(".", func(e fsroot.Entry) error {
				if e.Rel() == "." {
					return nil
				}
				if e.IsDir() && (e.Name() == ".git" || e.Name() == ".ark") {
					return fsroot.SkipDir
				}
				n++
				got := s.CheckEntry(e)
				var want error
				if r, err := tr.Resolve(e.Rel()); err != nil {
					want = err
				} else {
					want = s.Check(r)
				}
				if errors.Is(got, ErrExcluded) != errors.Is(want, ErrExcluded) || (got == nil) != (want == nil) {
					t.Errorf("%s, external %v: %s: CheckEntry %v, Check %v", name, allowExt, e.Rel(), got, want)
				}
				for i := range oldSets {
					if oldSets[i].MatchesRel(e.Rel()) != newSets[i].MatchesRel(e.Rel()) {
						t.Errorf("%s: %s: rule set %d differs", name, e.Rel(), i)
					}
				}
				return nil
			})
			t.Logf("%s, external %v: %d entries compared", name, allowExt, n)
		}
	}
}

// A closed snapshot decides nothing, for entries as for paths.
func TestClosedSnapshotRefusesEntries(t *testing.T) {
	w := ruleRepo(t)
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	s := snapshotOf(t, tr)
	s.Close()
	tr.Walk(".", func(e fsroot.Entry) error {
		if e.Rel() == "d/ok.txt" {
			if err := s.CheckEntry(e); !errors.Is(err, ErrClosed) {
				t.Errorf("CheckEntry after Close: %v", err)
			}
			if _, err := s.ReadEntry(e); !errors.Is(err, ErrClosed) {
				t.Errorf("ReadEntry after Close: %v", err)
			}
		}
		return nil
	})
}
