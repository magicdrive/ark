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

// Adversarial tests. A change made at a chosen point of the build (testHook)
// or between the build and the check is deterministic; the stress test races
// a writer against builds. What each guarantees is stated with it.

// withHook installs a build hook for the test.
func withHook(t *testing.T, h func(event, rel string)) {
	t.Helper()
	testHook = h
	t.Cleanup(func() { testHook = nil })
}

func decide(t *testing.T, s *Snapshot, rel string) error {
	t.Helper()
	r, err := s.Tree().Resolve(rel)
	if err != nil {
		return err
	}
	return s.Check(r)
}

func ruleRepo(t *testing.T) ws {
	w := newWS(t)
	w.write(t, "repo/.arkignore", "root-secret.txt\n")
	w.write(t, "repo/root-secret.txt", "ROOT_SECRET")
	w.write(t, "repo/d/.arkignore", "x.txt\n")
	w.write(t, "repo/d/x.txt", "D_SECRET")
	w.write(t, "repo/d/ok.txt", "D_OK")
	w.write(t, "repo/p/d/.arkignore", "x.txt\n")
	w.write(t, "repo/p/d/x.txt", "PD_SECRET")
	return w
}

// A4-1: the rule file is rewritten in place while the build reads it — with
// the same size and with another size. The second reading differs, the build
// starts over and takes the new content; never a mix.
func TestA4_InPlaceRewriteDuringBuild(t *testing.T) {
	// Two larger sizes here; the same size below.
	for _, newRules := range []string{"zzzzzz.txt\n", "ok.txt\nx.txt\nmore.txt\n"} {
		w := ruleRepo(t)
		tr := pinTree(t, w.path("repo"), fsroot.Options{})
		var verifies, rewrites int
		withHook(t, func(event, rel string) {
			switch {
			case event == "rule" && rel == "d/.arkignore" && rewrites == 0:
				rewrites++
				// In place: same file, same inode.
				f, _ := os.OpenFile(w.path("repo/d/.arkignore"), os.O_WRONLY|os.O_TRUNC, 0)
				f.WriteString(newRules)
				f.Close()
			case event == "verify":
				verifies++
			}
		})
		s, err := Build(tr, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if verifies != 2 {
			t.Errorf("%q: %d verifications, want 2 (the first must fail)", newRules, verifies)
		}
		if string(s.rules["d/.arkignore"]) != newRules {
			t.Errorf("captured %q, want the rewritten %q", s.rules["d/.arkignore"], newRules)
		}
	}
	// Same size: an in-place rewrite that keeps size and inode is still
	// caught by the bytes.
	w := ruleRepo(t)
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	same := "y.txt\n" // as long as "x.txt\n"
	done := false
	withHook(t, func(event, rel string) {
		if event == "rule" && rel == "d/.arkignore" && !done {
			done = true
			f, _ := os.OpenFile(w.path("repo/d/.arkignore"), os.O_WRONLY, 0)
			f.WriteAt([]byte(same), 0)
			f.Close()
		}
	})
	s, err := Build(tr, Options{})
	if err != nil || string(s.rules["d/.arkignore"]) != same {
		t.Errorf("same-size rewrite: %q %v", s.rules["d/.arkignore"], err)
	}
	if err := decide(t, s, "d/x.txt"); err != nil {
		t.Errorf("rules in force are the new ones: d/x.txt %v", err)
	}
}

// A4-1, persistent: the file changes on every attempt — the build fails with
// ErrRuleChanged rather than settle on any content.
func TestA4_RewriteOnEveryAttempt(t *testing.T) {
	w := ruleRepo(t)
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	n := 0
	withHook(t, func(event, rel string) {
		if event == "rule" && rel == "d/.arkignore" {
			n++
			w.write(t, "repo/d/.arkignore", []string{"x.txt\n", "y.txt\n"}[n%2])
		}
	})
	if s, err := Build(tr, Options{}); !errors.Is(err, ErrRuleChanged) || s != nil {
		t.Errorf("Build = %v, %v; want ErrRuleChanged and no snapshot", s, err)
	}
}

// A4-2: atomic replacement (write a new file, rename it over the rule file)
// during the build: another object — the build starts over.
func TestA4_AtomicReplacementDuringBuild(t *testing.T) {
	w := ruleRepo(t)
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	done := false
	withHook(t, func(event, rel string) {
		if event == "rule" && rel == "d/.arkignore" && !done {
			done = true
			w.write(t, "repo/d/.arkignore.new", "ok.txt\n")
			os.Rename(w.path("repo/d/.arkignore.new"), w.path("repo/d/.arkignore"))
		}
	})
	s, err := Build(tr, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if string(s.rules["d/.arkignore"]) != "ok.txt\n" {
		t.Errorf("captured %q", s.rules["d/.arkignore"])
	}
	if !errors.Is(decide(t, s, "d/ok.txt"), ErrExcluded) || decide(t, s, "d/x.txt") != nil {
		t.Error("decisions do not follow the replacement")
	}
}

// A4-3: the rule file is removed after it was read: the build starts over
// and the snapshot has no such rule (as if removed before the request).
func TestA4_RemovalDuringBuild(t *testing.T) {
	w := ruleRepo(t)
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	done := false
	withHook(t, func(event, rel string) {
		if event == "rule" && rel == "d/.arkignore" && !done {
			done = true
			os.Remove(w.path("repo/d/.arkignore"))
		}
	})
	s, err := Build(tr, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.rules["d/.arkignore"]; ok {
		t.Error("a removed rule file was kept")
	}
	if err := decide(t, s, "d/x.txt"); err != nil {
		t.Errorf("d/x.txt: %v", err)
	}
}

// A4-4: a rule file added where the walk has already passed is not in this
// snapshot (as if added after the request); a rule file added after the
// build changes nothing until the next snapshot.
func TestA4_AdditionDuringAndAfterBuild(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "repo/e/y.txt", "E_Y")
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	withHook(t, func(event, rel string) {
		if event == "verify" {
			w.write(t, "repo/e/.arkignore", "y.txt\n")
		}
	})
	s, err := Build(tr, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := decide(t, s, "e/y.txt"); err != nil {
		t.Errorf("a rule added behind the walk applied: %v", err)
	}
	testHook = nil
	w.write(t, "repo/.arkignore", "root-secret.txt\nd/ok.txt\n")
	if err := decide(t, s, "d/ok.txt"); err != nil {
		t.Errorf("a rule changed after the build applied: %v", err)
	}
	next := snapshotOf(t, tr)
	if !errors.Is(decide(t, next, "e/y.txt"), ErrExcluded) || !errors.Is(decide(t, next, "d/ok.txt"), ErrExcluded) {
		t.Error("the next snapshot does not see the changes")
	}
}

// A4-5: rule files at several levels rewritten continuously while snapshots
// are built: every snapshot either fails with ErrRuleChanged or holds, for
// each file, one of the versions actually written — never a torn mix.
func TestA4_ConcurrentMultiLevelMutation(t *testing.T) {
	w := ruleRepo(t)
	versions := map[string][]string{
		"repo/.arkignore":     {"root-secret.txt\n", "root-secret.txt\nother-a.txt\nother-b.txt\nother-c.txt\n"},
		"repo/d/.arkignore":   {"x.txt\n", "x.txt\nok.txt\nanother-long-pattern-to-change-size.txt\n"},
		"repo/p/d/.arkignore": {"x.txt\n", "*\n"},
	}
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; !stop.Load(); n++ {
			for p, v := range versions {
				f, err := os.OpenFile(w.path(p), os.O_WRONLY|os.O_TRUNC, 0)
				if err == nil {
					f.WriteString(v[n%2])
					f.Close()
				}
			}
		}
	}()
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	var built, changed int
	end := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(end) {
		s, err := Build(tr, Options{})
		if errors.Is(err, ErrRuleChanged) {
			changed++
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		built++
		for p, v := range versions {
			rel := p[len("repo/"):]
			got := string(s.rules[rel])
			if got != v[0] && got != v[1] && got != "" { // "" : between truncate and write
				t.Errorf("%s: captured %q, never written", rel, got)
			}
		}
	}
	stop.Store(true)
	wg.Wait()
	t.Logf("%d snapshots built, %d refused as changing", built, changed)
}

// A7-1: the directory holding a nested rule file is replaced by another
// directory (without the rule) while the snapshot is built, and put back
// before the check. The directory checked is not the directory read:
// ErrDirectoryChanged.
func TestA7_DirectoryReplacedDuringBuild(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "repo/d-decoy/x.txt", "DECOY")
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	os.Rename(w.path("repo/d"), w.path("repo/d-real"))
	os.Rename(w.path("repo/d-decoy"), w.path("repo/d"))
	s := snapshotOf(t, tr)
	os.Rename(w.path("repo/d"), w.path("repo/d-decoy"))
	os.Rename(w.path("repo/d-real"), w.path("repo/d"))
	if err := decide(t, s, "d/x.txt"); !errors.Is(err, ErrDirectoryChanged) {
		t.Errorf("d/x.txt: %v", err)
	}
	if got, err := s.ReadFile("d/x.txt"); err == nil {
		t.Errorf("read %q through the restored directory", got)
	}
}

// A7-2: the rule file alone is renamed away while the snapshot is built and
// renamed back afterwards. The directory is the same; the snapshot has no
// rule for it. This is not detected: it equals removing the rule and adding
// it again, which a writer of the repository can do permanently. Stated by
// test so a change of this guarantee is noticed.
func TestA7_RuleFileRenamedAwayDuringBuild(t *testing.T) {
	w := ruleRepo(t)
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	os.Rename(w.path("repo/d/.arkignore"), w.path("repo/d/hidden"))
	s := snapshotOf(t, tr)
	os.Rename(w.path("repo/d/hidden"), w.path("repo/d/.arkignore"))
	if err := decide(t, s, "d/x.txt"); err != nil {
		t.Errorf("documented limit changed: d/x.txt %v", err)
	}
}

// A7-3: the parent of the rule-bearing directory is replaced during the
// build and restored: ErrDirectoryChanged on the parent.
func TestA7_ParentReplacedDuringBuild(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "repo/p-decoy/d/x.txt", "DECOY")
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	os.Rename(w.path("repo/p"), w.path("repo/p-real"))
	os.Rename(w.path("repo/p-decoy"), w.path("repo/p"))
	s := snapshotOf(t, tr)
	os.Rename(w.path("repo/p"), w.path("repo/p-decoy"))
	os.Rename(w.path("repo/p-real"), w.path("repo/p"))
	if err := decide(t, s, "p/d/x.txt"); !errors.Is(err, ErrDirectoryChanged) {
		t.Errorf("p/d/x.txt: %v", err)
	}
}

// A7-4: the rule-bearing directory is replaced by a symlink to a directory
// without rules while the snapshot is built (the walk does not enter
// symlinks), and restored: the directory was never walked.
func TestA7_SymlinkConcealmentDuringBuild(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "repo/norules/x.txt", "NORULES")
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	os.Rename(w.path("repo/d"), w.path("repo/d-real"))
	w.link(t, "norules", "repo/d")
	s := snapshotOf(t, tr)
	os.Remove(w.path("repo/d"))
	os.Rename(w.path("repo/d-real"), w.path("repo/d"))
	if err := decide(t, s, "d/x.txt"); !errors.Is(err, ErrDirectoryChanged) {
		t.Errorf("d/x.txt: %v", err)
	}
}

// ABA during the build: after the walk recorded directory d and before it
// entered it, d is swapped for a decoy (outside the walk, so nothing else
// changes), and swapped back before the build ends. The walk read the
// decoy's (absent) rules under d's identity; the entries' parent identity
// gives it away and the build starts over. Without that check, d's own rule
// would be lost while every directory identity still matched.
func TestA7_ABADuringBuild(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "decoy/x.txt", "DECOY")
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	var swaps, verifies int
	withHook(t, func(event, rel string) {
		switch {
		case event == "dir" && rel == "d" && swaps == 0:
			swaps++
			os.Rename(w.path("repo/d"), w.path("d-real"))
			os.Rename(w.path("decoy"), w.path("repo/d"))
		case event == "verify" && swaps == 1:
			swaps++
			os.Rename(w.path("repo/d"), w.path("decoy"))
			os.Rename(w.path("d-real"), w.path("repo/d"))
		}
		if event == "verify" {
			verifies++
		}
	})
	s, err := Build(tr, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if swaps != 2 {
		t.Fatalf("%d swaps", swaps)
	}
	// Either the snapshot is of d itself (its rule excludes x.txt) or of
	// the decoy, whose identity d no longer has: never admitted.
	err = decide(t, s, "d/x.txt")
	if !errors.Is(err, ErrExcluded) && !errors.Is(err, ErrDirectoryChanged) {
		t.Errorf("d/x.txt after A-B-A: %v (decided by the decoy's rules)", err)
	}
	t.Logf("after A-B-A: %v (%d verifications)", err, verifies)
	// ABA after a clean build: the same directory object is back — its
	// rules apply.
	testHook = nil
	s = snapshotOf(t, tr)
	os.Rename(w.path("repo/d"), w.path("d-real"))
	os.Rename(w.path("d-real"), w.path("repo/d"))
	if err := decide(t, s, "d/x.txt"); !errors.Is(err, ErrExcluded) {
		t.Errorf("d/x.txt after a round trip: %v", err)
	}
}

// The root symlink is retargeted after the tree is pinned: the snapshot and
// every check stay with the pinned directory; another tree's paths are
// refused.
func TestRootSwapAndTreeMismatch(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "other/root-secret.txt", "OTHER")
	w.link(t, w.path("repo"), "root")
	tr := pinTree(t, w.path("root"), fsroot.Options{})
	s := snapshotOf(t, tr)
	os.Remove(w.path("root"))
	w.link(t, w.path("other"), "root")
	if err := decide(t, s, "root-secret.txt"); !errors.Is(err, ErrExcluded) {
		t.Errorf("after the root swap: %v", err)
	}
	other := pinTree(t, w.path("root"), fsroot.Options{})
	r, err := other.Resolve("root-secret.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Check(r); !errors.Is(err, ErrTreeMismatch) {
		t.Errorf("another tree's path: %v", err)
	}
}

// Symlink swaps through the snapshot's ReadFile: a file swapped for a link
// to an excluded file, a directory swapped for a link to an excluded
// directory — never read.
func TestSymlinkSwapsThroughSnapshot(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "repo/plain.txt", "PLAIN")
	w.write(t, "repo/sub/f.txt", "SUB")
	w.write(t, "repo/secretdir/f.txt", "SECRETDIR")
	w.write(t, "repo/.arkignore", "root-secret.txt\nsecretdir/\n")
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	s := snapshotOf(t, tr)
	os.Remove(w.path("repo/plain.txt"))
	w.link(t, "root-secret.txt", "repo/plain.txt")
	if b, err := s.ReadFile("plain.txt"); !errors.Is(err, ErrExcluded) {
		t.Errorf("file swapped for a link to an excluded file: %q %v", b, err)
	}
	os.Rename(w.path("repo/sub"), w.path("repo/sub-real"))
	w.link(t, "secretdir", "repo/sub")
	if b, err := s.ReadFile("sub/f.txt"); err == nil {
		t.Errorf("directory swapped for a link to an excluded directory: %q", b)
	}
}

// Rule files that cannot be read refuse everything but the root; a rule file
// linked outside the root counts as unreadable without external symlinks
// (its rules are unknown), and is read with them.
func TestUnreadableRules(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "outside/rules", "z.txt\n") // anchored at e/: excludes e/z.txt
	w.link(t, w.path("outside/rules"), "repo/e/.arkignore")
	w.write(t, "repo/e/z.txt", "Z")
	for _, allowExt := range []bool{false, true} {
		tr := pinTree(t, w.path("repo"), fsroot.Options{AllowExternalSymlinks: allowExt})
		s := snapshotOf(t, tr)
		err := decide(t, s, "e/z.txt")
		if allowExt && !errors.Is(err, ErrExcluded) {
			t.Errorf("external rule file, allowed: e/z.txt %v", err)
		}
		if !allowExt && !errors.Is(err, ErrRuleUnavailable) {
			t.Errorf("external rule file, not allowed: e/z.txt %v", err)
		}
		if err := decide(t, s, "."); err != nil {
			t.Errorf("root refused: %v", err)
		}
	}
	if os.Geteuid() != 0 {
		w2 := ruleRepo(t)
		os.Chmod(w2.path("repo/d/.arkignore"), 0)
		defer os.Chmod(w2.path("repo/d/.arkignore"), 0o644)
		s := snapshotOf(t, pinTree(t, w2.path("repo"), fsroot.Options{}))
		if err := decide(t, s, "d/ok.txt"); !errors.Is(err, ErrRuleUnavailable) {
			t.Errorf("unreadable rule file: %v", err)
		}
		if err := decide(t, s, "root-secret.txt"); !errors.Is(err, ErrRuleUnavailable) {
			t.Errorf("unreadable rule file elsewhere: %v", err)
		}
	}
}

// Directories under .git and .ark are not walked (no rules apply there):
// their paths are decided by the rules above, and the skipped directory
// itself is verified. A directory created after the build is refused.
func TestSkippedAndNewDirectories(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "repo/.git/objects/ab/cd", "OBJ")
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	s := snapshotOf(t, tr)
	if err := decide(t, s, ".git/objects/ab/cd"); err != nil {
		t.Errorf(".git path: %v", err)
	}
	w.write(t, "repo/new/n.txt", "N")
	if err := decide(t, s, "new/n.txt"); !errors.Is(err, ErrDirectoryChanged) {
		t.Errorf("a directory created after the build: %v", err)
	}
	os.Rename(w.path("repo/.git"), w.path("repo/.git-old"))
	w.write(t, "repo/.git/objects/ab/cd", "OTHER")
	if err := decide(t, s, ".git/objects/ab/cd"); !errors.Is(err, ErrDirectoryChanged) {
		t.Errorf("a replaced .git: %v", err)
	}
}

// Close, and concurrent checks (go test -race).
func TestCloseAndConcurrentChecks(t *testing.T) {
	w := ruleRepo(t)
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	s := snapshotOf(t, tr)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				r, err := tr.Resolve("d/x.txt")
				if err != nil {
					t.Error(err)
					return
				}
				if err := s.Check(r); !errors.Is(err, ErrExcluded) {
					t.Errorf("concurrent check: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); !errors.Is(err, ErrClosed) {
		t.Errorf("second Close: %v", err)
	}
	if err := decide(t, s, "d/ok.txt"); !errors.Is(err, ErrClosed) {
		t.Errorf("check after Close: %v", err)
	}
	if _, err := s.RuleSet(false); !errors.Is(err, ErrClosed) {
		t.Errorf("RuleSet after Close: %v", err)
	}
}

// S1: the root symlink is retargeted while the snapshot is being built. The
// rules are those of the pinned directory — read through the tree, never by
// the root's path — so they match the files the tree reads.
func TestRootSwapDuringBuild(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "other/.arkignore", "d/\n")
	w.write(t, "other/d/.arkignore", "ok.txt\n")
	w.link(t, w.path("repo"), "root")
	tr := pinTree(t, w.path("root"), fsroot.Options{})
	done := false
	withHook(t, func(event, rel string) {
		if event == "dir" && !done {
			done = true
			os.Remove(w.path("root"))
			w.link(t, w.path("other"), "root")
		}
	})
	s, err := Build(tr, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(s.rules[".arkignore"]); got != "root-secret.txt\n" {
		t.Errorf("root rules %q: not the pinned directory's", got)
	}
	if got := string(s.rules["d/.arkignore"]); got != "x.txt\n" {
		t.Errorf("nested rules %q: not the pinned directory's", got)
	}
	if err := decide(t, s, "d/ok.txt"); err != nil {
		t.Errorf("d/ok.txt decided by the other root's rules: %v", err)
	}
}
