package fsroot

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Adversarial tests. The deterministic ones change the tree exactly between
// the check (Resolve) and the use (Open) — the window a concurrent writer
// aims at — and state what must hold. The stress ones race a writer against
// readers; zero leaks there is evidence, not proof: the proof is the
// deterministic case plus the mechanism (descriptor-relative opens without
// following, identity verified).

func readResolved(t *testing.T, tr *Tree, r Resolved) (string, error) {
	t.Helper()
	f, err := tr.Open(r)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	return string(b), err
}

// A1: the root symlink is retargeted after the Tree is pinned. The Tree
// keeps reading the directory it pinned; it never reads the other
// repository's file.
func TestA1_RootSwap(t *testing.T) {
	f := newFixture(t)
	tr := pin(t, f.path("root"), Options{})
	id, _ := tr.DirIdentity(".")
	r, err := tr.Resolve("s.txt")
	if err != nil {
		t.Fatal(err)
	}
	swapLink(f.path("B"), f.path("root"))
	if got, err := readResolved(t, tr, r); got != "A_PUBLIC" || err != nil {
		t.Errorf("checked before the swap, read after: %q %v", got, err)
	}
	if got := outcome(tr.ReadFile("s.txt", nil)); got != "A_PUBLIC" {
		t.Errorf("resolved and read after the swap: %s", got)
	}
	if now, _ := tr.DirIdentity("."); !now.Same(id) {
		t.Error("the pinned root changed")
	}
	// The directory moved: the Tree follows it.
	if err := os.Rename(f.path("A"), f.path("A-moved")); err != nil {
		t.Fatal(err)
	}
	if got := outcome(tr.ReadFile("sub/f.txt", nil)); got != "A_SUB" {
		t.Errorf("after the root directory moved: %s", got)
	}
}

// A2: a checked regular file is replaced by a symlink leading outside the
// root. The open fails; the outside file is never read.
func TestA2_FileSwappedForExternalLink(t *testing.T) {
	for _, allowExt := range []bool{false, true} {
		f := newFixture(t)
		f.write(t, "A/x.txt", "PUBLIC")
		tr := pin(t, f.path("root"), Options{AllowExternalSymlinks: allowExt})
		r, err := tr.Resolve("x.txt")
		if err != nil {
			t.Fatal(err)
		}
		swapLink(f.path("outside/secret.txt"), f.path("A/x.txt"))
		got, err := readResolved(t, tr, r)
		if strings.Contains(got, "OUTSIDE") || err == nil {
			t.Errorf("external %v: read %q after the swap (err %v)", allowExt, got, err)
		}
		// Checked again after the swap, it is an external link: refused
		// unless allowed.
		want := "outside"
		if allowExt {
			want = "OUTSIDE_SECRET"
		}
		if got := outcome(tr.ReadFile("x.txt", nil)); got != want {
			t.Errorf("external %v: after the swap: %s, want %s", allowExt, got, want)
		}
	}
}

// A3: the excluded file is renamed onto a checked path. The checked object
// is gone: the open fails with ErrChanged rather than read the other file.
// What this does not prevent: a later check finds the moved file at the
// allowed path and allows it — a path-based policy cannot tell a rename
// from an edit, and a permanent rename gives the same result without racing.
func TestA3_RenameOntoCheckedPath(t *testing.T) {
	f := newFixture(t)
	f.write(t, "A/allowed.txt", "PUBLIC")
	f.write(t, "A/secret.txt", "RENAMED_SECRET")
	tr := pin(t, f.path("root"), Options{})
	allow := excludedBy("secret.txt")
	r, err := tr.Resolve("allowed.txt")
	if err != nil || !allow(r) {
		t.Fatal(err)
	}
	if err := os.Rename(f.path("A/secret.txt"), f.path("A/allowed.txt")); err != nil {
		t.Fatal(err)
	}
	if got, err := readResolved(t, tr, r); !errors.Is(err, ErrChanged) {
		t.Errorf("read the renamed file through the checked path: %q %v", got, err)
	}
	if got := outcome(tr.ReadFile("allowed.txt", allow)); got != "RENAMED_SECRET" {
		t.Errorf("documented limit changed: a fresh check reads %q", got)
	}
}

// A4: rule bytes are read once, with the identity of the object read.
// Rewriting the file in place keeps its identity and changes its bytes; a
// replacement by rename changes the identity. So a rule snapshot must keep
// the bytes (or their hash), not just identities — Phase 2.
func TestA4_RuleFileSnapshot(t *testing.T) {
	f := newFixture(t)
	tr := pin(t, f.path("root"), Options{})
	read := func() ([]byte, Identity) {
		var b []byte
		var id Identity
		tr.Walk(".", func(e Entry) error {
			if e.Rel() == ".arkignore" {
				var err error
				b, id, err = e.ReadAll()
				if err != nil {
					t.Fatal(err)
				}
			}
			return nil
		})
		return b, id
	}
	b1, id1 := read()
	if string(b1) != "excluded.txt\n" {
		t.Fatalf("rules: %q", b1)
	}
	if err := os.WriteFile(f.path("A/.arkignore"), []byte("s.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b2, id2 := read()
	if !id1.Same(id2) || bytes.Equal(b1, b2) {
		t.Errorf("in-place rewrite: same identity %v, same bytes %v", id1.Same(id2), bytes.Equal(b1, b2))
	}
	if string(b1) != "excluded.txt\n" {
		t.Error("bytes read earlier changed")
	}
	f.write(t, "A/.arkignore.new", "other\n")
	if err := os.Rename(f.path("A/.arkignore.new"), f.path("A/.arkignore")); err != nil {
		t.Fatal(err)
	}
	if _, id3 := read(); id3.Same(id2) {
		t.Error("a replaced rule file kept its identity")
	}
}

// A5: a checked directory on the path is replaced by a symlink to an
// excluded directory. Open walks without following and verifies the
// directory: it fails; the excluded file is never read.
func TestA5_DirectorySwappedForLink(t *testing.T) {
	f := newFixture(t)
	f.write(t, "A/secretdir/f.txt", "SECRETDIR")
	tr := pin(t, f.path("root"), Options{})
	allow := excludedBy("secretdir/")
	r, err := tr.Resolve("sub/f.txt")
	if err != nil || !allow(r) {
		t.Fatal(err)
	}
	if err := os.Rename(f.path("A/sub"), f.path("A/sub-real")); err != nil {
		t.Fatal(err)
	}
	f.link(t, "secretdir", "A/sub")
	if got, err := readResolved(t, tr, r); strings.Contains(got, "SECRETDIR") || err == nil {
		t.Errorf("read through the swapped directory: %q %v", got, err)
	}
	// Checked again, the path resolves into the excluded directory: the
	// policy sees it.
	if got := outcome(tr.ReadFile("sub/f.txt", allow)); got != "denied" {
		t.Errorf("after the swap: %s", got)
	}
	// A walk never enters the link.
	tr.Walk(".", func(e Entry) error {
		if strings.HasPrefix(e.Rel(), "sub/") {
			t.Errorf("walk entered the swapped link: %s", e.Rel())
		}
		return nil
	})
}

// A5 during a walk: a directory listed as a directory is replaced by a
// symlink to an excluded directory after it was reported and before the walk
// enters it (the callback is that moment). The walk does not enter the link:
// the directory is reported with Err, and nothing below it is listed or read.
func TestA5_DirectorySwappedDuringWalk(t *testing.T) {
	f := newFixture(t)
	f.write(t, "A/secretdir/f.txt", "SECRETDIR")
	tr := pin(t, f.path("root"), Options{})
	var refused bool
	tr.Walk(".", func(e Entry) error {
		switch {
		case e.Rel() == "sub" && e.Err() == nil:
			os.Rename(f.path("A/sub"), f.path("A/sub-real"))
			f.link(t, "secretdir", "A/sub")
		case e.Rel() == "sub" && e.Err() != nil:
			refused = true
		case strings.HasPrefix(e.Rel(), "sub/"):
			b, _, _ := e.ReadAll()
			t.Errorf("walk entered the swapped directory: %s %q", e.Rel(), b)
		}
		return nil
	})
	if !refused {
		t.Error("the swapped directory was not reported as refused")
	}
}

// A6: a file link checked while it pointed to an allowed file is retargeted
// to an excluded file. Open reads the real path checked — never the link
// again — so it reads the allowed file (or fails), never the excluded one.
func TestA6_LinkRetargeted(t *testing.T) {
	f := newFixture(t)
	f.write(t, "A/ok.txt", "OK_CONTENT")
	f.write(t, "A/hidden.txt", "HIDDEN_ALIAS")
	f.link(t, "ok.txt", "A/in.txt")
	tr := pin(t, f.path("root"), Options{})
	allow := excludedBy("hidden.txt")
	r, err := tr.Resolve("in.txt")
	if err != nil || r.Real() != "ok.txt" || !allow(r) {
		t.Fatalf("resolved %q %v", r.Real(), err)
	}
	swapLink("hidden.txt", f.path("A/in.txt"))
	if got, err := readResolved(t, tr, r); got != "OK_CONTENT" || err != nil {
		t.Errorf("after the retarget: %q %v", got, err)
	}
	if got := outcome(tr.ReadFile("in.txt", allow)); got != "denied" {
		t.Errorf("checked after the retarget: %s", got)
	}
}

// A7: a nested rule file is concealed by replacing its directory. The
// directory identity recorded when the rules were read no longer matches:
// Phase 2 refuses reads below a directory whose identity changed since its
// rules were read. Here: the identity is observable, and a walk reports the
// new directory's identity for its entries.
func TestA7_ConcealedNestedRules(t *testing.T) {
	f := newFixture(t)
	f.write(t, "A/nested/.arkignore", "private.txt\n")
	f.write(t, "A/nested/private.txt", "NESTED_PRIVATE")
	tr := pin(t, f.path("root"), Options{})
	before, err := tr.DirIdentity("nested")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.path("A/nested"), f.path("A/nested-old")); err != nil {
		t.Fatal(err)
	}
	f.write(t, "A/nested/private.txt", "NESTED_PRIVATE")
	after, err := tr.DirIdentity("nested")
	if err != nil {
		t.Fatal(err)
	}
	if after.Same(before) {
		t.Fatal("a replaced directory kept its identity")
	}
	tr.Walk("nested", func(e Entry) error {
		if e.DirIdentity().Same(before) {
			t.Errorf("%s: reported the old directory's identity", e.Rel())
		}
		return nil
	})
	// Renaming it back restores the identity: the snapshot is of objects.
	os.RemoveAll(f.path("A/nested"))
	os.Rename(f.path("A/nested-old"), f.path("A/nested"))
	if again, _ := tr.DirIdentity("nested"); !again.Same(before) {
		t.Error("identity of the original directory changed")
	}
}

// race runs flip against reads for d and returns the leak count.
func race(t *testing.T, d time.Duration, flip func(n int), read func() (string, error), leak string) (reads, leaks, refused int) {
	t.Helper()
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; !stop.Load(); n++ {
			flip(n)
		}
	}()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		got, err := read()
		reads++
		if err != nil {
			refused++
		} else if strings.Contains(got, leak) {
			leaks++
		}
	}
	stop.Store(true)
	wg.Wait()
	return
}

func stressDuration(t *testing.T) time.Duration {
	if testing.Short() {
		return 150 * time.Millisecond
	}
	return 600 * time.Millisecond
}

// Stress: the four symlink attacks of the design study, each raced against
// a pinned-per-request reader with the policy applied.
func TestStress_SymlinkAttacks(t *testing.T) {
	d := stressDuration(t)
	t.Run("A1 root swap", func(t *testing.T) {
		f := newFixture(t)
		f.write(t, "B/.arkignore", "s.txt\n")
		reads, leaks, _ := race(t, d,
			func(n int) { swapLink(f.path([]string{"A", "B"}[n%2]), f.path("root")) },
			func() (string, error) {
				tr, err := Pin(f.path("root"), Options{})
				if err != nil {
					return "", err
				}
				defer tr.Close()
				// The rules of the pinned tree, read through it.
				var excluded []string
				tr.Walk(".", func(e Entry) error {
					if e.Rel() == ".arkignore" {
						b, _, _ := e.ReadAll()
						excluded = strings.Fields(string(b))
					}
					if e.IsDir() && e.Rel() != "." {
						return SkipDir
					}
					return nil
				})
				b, err := tr.ReadFile("s.txt", excludedBy(excluded...))
				return string(b), err
			}, "B_SECRET")
		if leaks > 0 {
			t.Errorf("%d of %d reads returned B's excluded file", leaks, reads)
		}
		t.Logf("%d reads, 0 leaks", reads)
	})
	t.Run("A2 file <-> external link", func(t *testing.T) {
		f := newFixture(t)
		f.write(t, "A/x.txt", "PUBLIC")
		reads, leaks, _ := race(t, d,
			func(n int) {
				if n%2 == 0 {
					swapLink(f.path("outside/secret.txt"), f.path("A/x.txt"))
				} else {
					f.write(t, "A/x.tmp", "PUBLIC")
					os.Rename(f.path("A/x.tmp"), f.path("A/x.txt"))
				}
			},
			func() (string, error) {
				tr, err := Pin(f.path("root"), Options{})
				if err != nil {
					return "", err
				}
				defer tr.Close()
				b, err := tr.ReadFile("x.txt", nil)
				return string(b), err
			}, "OUTSIDE")
		if leaks > 0 {
			t.Errorf("%d of %d reads returned the outside file", leaks, reads)
		}
		t.Logf("%d reads, 0 leaks", reads)
	})
	t.Run("A5 directory <-> link to excluded directory", func(t *testing.T) {
		f := newFixture(t)
		f.write(t, "A/secretdir/f.txt", "SECRETDIR")
		os.Rename(f.path("A/sub"), f.path("A/sub-real"))
		f.link(t, "sub-real", "A/sub")
		allow := excludedBy("secretdir/")
		reads, leaks, _ := race(t, d,
			func(n int) { swapLink([]string{"secretdir", "sub-real"}[n%2], f.path("A/sub")) },
			func() (string, error) {
				tr, err := Pin(f.path("root"), Options{})
				if err != nil {
					return "", err
				}
				defer tr.Close()
				b, err := tr.ReadFile("sub/f.txt", allow)
				return string(b), err
			}, "SECRETDIR")
		if leaks > 0 {
			t.Errorf("%d of %d reads returned the excluded directory's file", leaks, reads)
		}
		t.Logf("%d reads, 0 leaks", reads)
	})
	t.Run("A6 link retargeted to excluded file", func(t *testing.T) {
		f := newFixture(t)
		f.write(t, "A/ok.txt", "OK_CONTENT")
		f.write(t, "A/hidden.txt", "HIDDEN_ALIAS")
		f.link(t, "ok.txt", "A/in.txt")
		allow := excludedBy("hidden.txt")
		reads, leaks, _ := race(t, d,
			func(n int) { swapLink([]string{"hidden.txt", "ok.txt"}[n%2], f.path("A/in.txt")) },
			func() (string, error) {
				tr, err := Pin(f.path("root"), Options{})
				if err != nil {
					return "", err
				}
				defer tr.Close()
				b, err := tr.ReadFile("in.txt", allow)
				return string(b), err
			}, "HIDDEN_ALIAS")
		if leaks > 0 {
			t.Errorf("%d of %d reads returned the excluded file", leaks, reads)
		}
		t.Logf("%d reads, 0 leaks", reads)
	})
}

// Concurrent reads and walks on one Tree, closed while they run: no data
// race (go test -race), no panic, and after Close only ErrClosed.
func TestConcurrentUseAndClose(t *testing.T) {
	f := newFixture(t)
	tr, err := Pin(f.path("root"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var closed atomic.Bool
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				var err error
				if g%2 == 0 {
					var b []byte
					b, err = tr.ReadFile("hop1", nil)
					if err == nil && string(b) != "A_PUBLIC" {
						t.Errorf("read %q", b)
					}
				} else {
					err = tr.Walk(".", func(Entry) error { return nil })
				}
				if err != nil && !errors.Is(err, ErrClosed) {
					t.Errorf("unexpected error: %v", err)
				}
				if errors.Is(err, ErrClosed) && !closed.Load() {
					t.Error("ErrClosed before Close")
				}
			}
		}(g)
	}
	time.Sleep(5 * time.Millisecond)
	closed.Store(true)
	if err := tr.Close(); err != nil {
		t.Error(err)
	}
	wg.Wait()
}
