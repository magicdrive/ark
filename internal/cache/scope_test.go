package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	scopeA = "dev-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	scopeB = "dev-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func scopedEntry(name string) (CacheKey, *CachedExtraction) {
	_ = name
	key := NewCacheKey("a.py", []byte("def f(): pass\n"), "0.1.0", "5")
	return key, &CachedExtraction{Key: key, Language: "python", Symbols: nil}
}

// mustOpenScope opens scope's store in dir and waits for its tidying.
func mustOpenScope(t *testing.T, dir, scope string) *FileStore {
	t.Helper()
	s, err := openScope(dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	<-s.Tidied()
	return s
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// A build reuses its own entries and never another build's, whatever their
// keys: the same file, content, schema and CacheVersion written by build A
// is a miss for build B, and A still finds it afterwards.
func TestScopes_ABuildReusesOnlyItsOwnEntries(t *testing.T) {
	dir := t.TempDir()
	key, entry := scopedEntry("a.py")
	a := mustOpenScope(t, dir, scopeA)
	if err := a.Put(entry); err != nil {
		t.Fatal(err)
	}
	if entry.Key.Build != scopeA {
		t.Errorf("Put stamped %q, want %q", entry.Key.Build, scopeA)
	}
	b := mustOpenScope(t, dir, scopeB)
	if _, hit, _ := b.Get(key); hit {
		t.Fatal("build B reused build A's entry")
	}
	if _, hit, _ := mustOpenScope(t, dir, scopeA).Get(key); !hit {
		t.Fatal("build A lost its entry after build B opened the cache")
	}
	if st := b.Stats(); st.Absent != 1 || st.Reused != 0 {
		t.Errorf("build B stats %+v, want one absent lookup", st)
	}
}

// An entry is checked against the scope it is read in: one copied or moved
// into another build's scope is no entry of that build (and is removed).
func TestScopes_AnEntryOfAnotherBuildIsRejected(t *testing.T) {
	dir := t.TempDir()
	key, entry := scopedEntry("a.py")
	a := mustOpenScope(t, dir, scopeA)
	if err := a.Put(entry); err != nil {
		t.Fatal(err)
	}
	b := mustOpenScope(t, dir, scopeB)
	name := key.storageID() + ".json"
	data, err := os.ReadFile(filepath.Join(dir, scopeA, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, scopeB, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := b.Get(key); hit {
		t.Fatal("build B reused an entry carrying build A's scope")
	}
	if st := b.Stats(); st.OtherBuild != 1 {
		t.Errorf("stats %+v, want one entry of another build", st)
	}
	if _, err := os.Stat(filepath.Join(dir, scopeB, name)); !os.IsNotExist(err) {
		t.Errorf("the foreign entry is still in scope B: %v", err)
	}
	// An entry of no scope (written by NewFileStore, or before scopes) is
	// another build's too.
	plain, err := NewFileStore(filepath.Join(dir, scopeB))
	if err != nil {
		t.Fatal(err)
	}
	_, e2 := scopedEntry("a.py")
	if err := plain.Put(e2); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := b.Get(key); hit {
		t.Fatal("build B reused an entry of no scope")
	}
}

// Opening the cache removes the flat entries of the layout before scopes
// (their writer cannot be known) and keeps the release scope and anything it
// does not recognise.
func TestOpenScope_RemovesTheOldLayout(t *testing.T) {
	dir := t.TempDir()
	legacy := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, name := range []string{legacy + ".json", legacy + ".json.tmp", "notes.txt", "keep.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{ReleaseScope, "dev-short", "other"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mustOpenScope(t, dir, scopeA)
	got := dirNames(t, dir)
	want := []string{scopeA, "dev-short", "keep.json", "notes.txt", "other", ReleaseScope}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("after opening: %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("after opening: %v, want %v", got, want)
		}
	}
}

// The development scopes kept are the current one and the keepDevScopes
// opened most recently — by their mark, not by the directory's time, which a
// restart that writes nothing leaves old; the release scope is never removed.
func TestOpenScope_KeepsTheRecentDevelopmentScopes(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	var scopes []string
	for i := 0; i < 6; i++ {
		sc := devScopePrefix + string(rune('0'+i)) + "0000000000000000000000000000000"
		scopes = append(scopes, sc)
		if err := os.MkdirAll(filepath.Join(dir, sc), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sc, usedMark), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for i, sc := range scopes {
		used := base.Add(time.Duration(i) * time.Minute)
		written := base.Add(time.Duration(10-i) * time.Minute) // the other order
		if err := os.Chtimes(filepath.Join(dir, sc, usedMark), used, used); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(dir, sc), written, written); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ReleaseScope), 0o700); err != nil {
		t.Fatal(err)
	}
	// The oldest is opened again: it is current, the three most recent of
	// the others are kept.
	mustOpenScope(t, dir, scopes[0])
	got := dirNames(t, dir)
	want := []string{scopes[0], scopes[3], scopes[4], scopes[5], ReleaseScope}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("scopes kept: %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("scopes kept: %v, want %v", got, want)
		}
	}
}

// A scope removed under a running store (another build tidied the cache)
// costs that store its entries, never correctness: it misses, and its next
// write recreates the scope.
func TestScopes_RemovedScopeIsRecreated(t *testing.T) {
	dir := t.TempDir()
	key, entry := scopedEntry("a.py")
	a := mustOpenScope(t, dir, scopeA)
	if err := a.Put(entry); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, scopeA)); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := a.Get(key); hit {
		t.Fatal("hit in a removed scope")
	}
	if err := a.Put(entry); err != nil {
		t.Fatalf("Put after the scope was removed: %v", err)
	}
	if _, hit, _ := a.Get(key); !hit {
		t.Fatal("the rewritten entry is not reused")
	}
}

// A corrupt entry is a miss, removed, and the next write repairs it.
func TestScopes_CorruptEntryIsRebuilt(t *testing.T) {
	dir := t.TempDir()
	key, entry := scopedEntry("a.py")
	a := mustOpenScope(t, dir, scopeA)
	p := filepath.Join(dir, scopeA, key.storageID()+".json")
	if err := os.WriteFile(p, []byte(`{"key": {"FilePath": "a.py"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := a.Get(key); hit {
		t.Fatal("hit on a truncated entry")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("the corrupt entry was kept: %v", err)
	}
	if err := a.Put(entry); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := a.Get(key); !hit {
		t.Fatal("not reused after the rewrite")
	}
	if st := a.Stats(); st.Corrupt != 1 || st.Reused != 1 || st.Written != 1 {
		t.Errorf("stats %+v", st)
	}
}

// Processes sharing a root write the same entries at the same time: every
// write goes through a temporary file of its own, so the entry left is one
// whole write and no temporary file is left behind.
func TestScopes_ConcurrentWritersOfOneEntry(t *testing.T) {
	dir := t.TempDir()
	key, _ := scopedEntry("a.py")
	stores := []*FileStore{mustOpenScope(t, dir, scopeA), mustOpenScope(t, dir, scopeA)}
	var wg sync.WaitGroup
	var written atomic.Int32
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := scopedEntry("a.py")
			err := stores[i%2].Put(e)
			switch {
			case err == nil:
				written.Add(1)
			case runtime.GOOS != "windows":
				// Windows may refuse to replace a file another write is
				// replacing: a failed write is a miss there, nothing worse.
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if written.Load() == 0 {
		t.Fatal("no write succeeded")
	}
	names := dirNames(t, filepath.Join(dir, scopeA))
	if len(names) != 2 || names[0] != usedMark || names[1] != key.storageID()+".json" {
		t.Fatalf("scope holds %v, want the mark and one entry", names)
	}
	data, err := os.ReadFile(filepath.Join(dir, scopeA, names[1]))
	if err != nil {
		t.Fatal(err)
	}
	var e CachedExtraction
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("the entry is no whole write: %v", err)
	}
	if _, hit, _ := stores[0].Get(key); !hit {
		t.Fatal("the entry is not reused")
	}
}

// When .ark or .ark/index is a symbolic link, what it points to need not be
// Ark's: nothing there is removed (the cache itself still works).
func TestOpenScope_TidiesNothingThroughASymlink(t *testing.T) {
	for _, linkAt := range []string{"index", "ark"} {
		t.Run(linkAt, func(t *testing.T) {
			root, elsewhere := t.TempDir(), t.TempDir()
			legacy := strings.Repeat("ab", 32) + ".json"
			if err := os.WriteFile(filepath.Join(elsewhere, legacy), []byte("user data"), 0o600); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < keepDevScopes+2; i++ {
				if err := os.MkdirAll(filepath.Join(elsewhere, devScopePrefix+strings.Repeat(string(rune('a'+i)), 32)), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			dir := filepath.Join(root, ".ark", "index")
			if linkAt == "index" {
				if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(elsewhere, dir); err != nil {
					t.Skip("no symbolic links:", err)
				}
			} else {
				if err := os.Symlink(filepath.Dir(elsewhere), filepath.Join(root, ".ark")); err != nil {
					t.Skip("no symbolic links:", err)
				}
				dir = filepath.Join(root, ".ark", filepath.Base(elsewhere))
			}
			before := dirNames(t, elsewhere)
			s := mustOpenScope(t, dir, scopeA)
			key, entry := scopedEntry("a.py")
			if err := s.Put(entry); err != nil {
				t.Fatal(err)
			}
			if _, hit, _ := s.Get(key); !hit {
				t.Error("the cache does not work through the link")
			}
			after := dirNames(t, elsewhere)
			for _, name := range before {
				found := false
				for _, a := range after {
					found = found || a == name
				}
				if !found {
					t.Errorf("%s was removed through the link", name)
				}
			}
		})
	}
}

// A write a process left unfinished leaves its temporary file; opening the
// scope again removes those older than staleTemp, never a recent one (a
// write in progress), and retired scopes left half-deleted are finished.
func TestOpenScope_RemovesLeftovers(t *testing.T) {
	dir := t.TempDir()
	mustOpenScope(t, dir, scopeA)
	id := strings.Repeat("cd", 32)
	old, recent := id+".json.123.tmp", id+".json.456.tmp"
	for _, name := range []string{old, recent} {
		if err := os.WriteFile(filepath.Join(dir, scopeA, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	long := time.Now().Add(-2 * staleTemp)
	if err := os.Chtimes(filepath.Join(dir, scopeA, old), long, long); err != nil {
		t.Fatal(err)
	}
	trash := filepath.Join(dir, trashPrefix+scopeB+"-1")
	if err := os.MkdirAll(filepath.Join(trash, scopeB), 0o700); err != nil {
		t.Fatal(err)
	}
	mustOpenScope(t, dir, scopeA)
	got := dirNames(t, filepath.Join(dir, scopeA))
	if len(got) != 2 || got[0] != usedMark || got[1] != recent {
		t.Errorf("scope holds %v, want the mark and the recent temporary file", got)
	}
	if _, err := os.Stat(trash); !os.IsNotExist(err) {
		t.Errorf("the retired scope is still there: %v", err)
	}
}

// Opening the cache only renames a retired scope: its deletion — the slow
// part, in proportion to its entries — happens in the background.
func TestOpenScope_RetiresAtOnceDeletesInTheBackground(t *testing.T) {
	dir := t.TempDir()
	var scopes []string
	for i := 0; i <= keepDevScopes; i++ {
		sc := devScopePrefix + strings.Repeat(string(rune('c'+i)), 32) // not scopeA or scopeB
		scopes = append(scopes, sc)
		if err := os.MkdirAll(filepath.Join(dir, sc), 0o700); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(time.Duration(i-10) * time.Minute)
		if err := os.WriteFile(filepath.Join(dir, sc, usedMark), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(dir, sc, usedMark), at, at); err != nil {
			t.Fatal(err)
		}
	}
	entry := filepath.Join(dir, scopes[0], strings.Repeat("ef", 32)+".json")
	if err := os.WriteFile(entry, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	beforeSweep = func() { <-release }
	defer func() { beforeSweep = nil }()
	s, err := openScope(dir, scopeA)
	if err != nil {
		t.Fatal(err)
	}
	// Retired, not yet deleted: the oldest scope is in a trash directory,
	// whole.
	if _, err := os.Stat(filepath.Join(dir, scopes[0])); !os.IsNotExist(err) {
		t.Errorf("the oldest scope was not retired at once: %v", err)
	}
	trashed, _ := filepath.Glob(filepath.Join(dir, trashPrefix+scopes[0]+"-*", scopes[0], filepath.Base(entry)))
	if len(trashed) != 1 {
		t.Errorf("the retired scope is not set aside whole before the background deletion: %v", trashed)
	}
	close(release)
	<-s.Tidied()
	if left, _ := filepath.Glob(filepath.Join(dir, trashPrefix+"*")); len(left) != 0 {
		t.Errorf("left after the background deletion: %v", left)
	}
}

// Builds opening one cache at the same time — each retiring the others'
// scopes, sweeping, writing and reading — never read an entry another build
// wrote: once every build has written the same keys, a lookup hits only an
// entry of its own build's scope and extraction.
func TestScopes_RetiringRacesLookupsOfOtherBuilds(t *testing.T) {
	dir := t.TempDir()
	const builds, rounds, files = keepDevScopes + 3, 15, 5
	key := func(f int) CacheKey {
		return NewCacheKey("f"+strconv.Itoa(f)+".py", []byte("same content"), "0.1.0", "5")
	}
	var hits atomic.Int64
	for r := 0; r < rounds; r++ {
		stores := make([]*FileStore, builds)
		var wg sync.WaitGroup
		for b := range stores {
			wg.Add(1)
			go func() {
				defer wg.Done()
				scope := devScopePrefix + strings.Repeat(string(rune('a'+b)), 32)
				s, err := openScope(dir, scope)
				if err != nil {
					t.Error(err)
					return
				}
				stores[b] = s
				for f := 0; f < files; f++ {
					// The extraction this build makes: its scope's name.
					_ = s.Put(&CachedExtraction{Key: key(f), Language: scope})
				}
			}()
		}
		wg.Wait()
		for _, s := range stores {
			if s == nil {
				t.FailNow()
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				for f := 0; f < files; f++ {
					if e, hit, _ := s.Get(key(f)); hit {
						hits.Add(1)
						if e.Key.Build != s.build || e.Language != s.build {
							t.Errorf("build %s read %s's entry (%s)", s.build, e.Key.Build, e.Language)
						}
					}
				}
				<-s.Tidied()
			}()
		}
		wg.Wait()
	}
	if hits.Load() == 0 {
		t.Fatal("no lookup hit: the race exercised nothing")
	}
}
