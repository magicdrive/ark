package accesspolicy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/fsroot"
)

// A scoped snapshot (BuildScoped) decides every path as the whole tree's
// snapshot (Build) does, capturing only the rule files above it.

func kind(err error) string {
	switch {
	case err == nil:
		return "admit"
	case errors.Is(err, ErrExcluded):
		return "excluded"
	case errors.Is(err, ErrRuleUnavailable):
		return "unavailable"
	}
	return "error: " + err.Error()
}

func scopedOf(t testing.TB, tr *fsroot.Tree) *Snapshot {
	t.Helper()
	s, err := BuildScoped(tr, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// compareScoped decides every walked path (and some that are not walked:
// below links, missing) with the full snapshot, one scoped snapshot shared by
// all decisions, and a fresh scoped snapshot per decision.
func compareScoped(t *testing.T, name, root string, extra ...string) int {
	t.Helper()
	n := 0
	for _, ext := range []bool{false, true} {
		tr := pinTree(t, root, fsroot.Options{AllowExternalSymlinks: ext})
		full := snapshotOf(t, tr)
		shared := scopedOf(t, tr)
		var rels []string
		tr.Walk(".", func(e fsroot.Entry) error {
			if e.IsDir() && (e.Name() == ".git" || e.Name() == ".ark") {
				rels = append(rels, e.Rel(), e.Rel()+"/config")
				return fsroot.SkipDir
			}
			rels = append(rels, e.Rel())
			return nil
		})
		rels = append(rels, extra...)
		for _, rel := range rels {
			r, err := tr.Resolve(rel)
			if err != nil {
				continue
			}
			want := kind(full.Check(r))
			if got := kind(shared.Check(r)); got != want {
				t.Errorf("%s, external %v: %s: shared scoped %s, full %s", name, ext, rel, got, want)
			}
			if got := kind(scopedOf(t, tr).Check(r)); got != want {
				t.Errorf("%s, external %v: %s: fresh scoped %s, full %s", name, ext, rel, got, want)
			}
			n++
		}
	}
	return n
}

func TestScopedDecidesAsFull(t *testing.T) {
	_, root, paths := policyRepo(t)
	n := compareScoped(t, "policy fixture", root, append(paths, "linkrules/linked.txt", "dirlink/x.go", "missing/x")...)
	w := ruleRepo(t)
	n += compareScoped(t, "rule fixture", w.path("repo"))
	for _, env := range []string{"ARK_POLICY_BENCH_ROOT", "ARK_POLICY_DIFF_ROOT2"} {
		if r := os.Getenv(env); r != "" {
			n += compareScoped(t, filepath.Base(r), r)
		}
	}
	t.Logf("%d decisions compared", n)
}

// Entries of a walk are decided as by the full snapshot too.
func TestScopedCheckEntry(t *testing.T) {
	_, root, _ := policyRepo(t)
	tr := pinTree(t, root, fsroot.Options{})
	full, scoped := snapshotOf(t, tr), scopedOf(t, tr)
	tr.Walk(".", func(e fsroot.Entry) error {
		if e.IsDir() && e.Name() == ".git" {
			return fsroot.SkipDir
		}
		if got, want := kind(scoped.CheckEntry(e)), kind(full.CheckEntry(e)); got != want {
			t.Errorf("%s: scoped %s, full %s", e.Rel(), got, want)
		}
		return nil
	})
}

// Only the rule files above the path are read.
func TestScopedReadsOnlyTheChain(t *testing.T) {
	w := ruleRepo(t)
	s := scopedOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	var read []string
	withHook(t, func(event, rel string) {
		if event == "verify" {
			read = append(read, rel)
		}
	})
	if _, err := s.ReadFile("d/ok.txt"); err != nil {
		t.Fatal(err)
	}
	if b, err := s.ReadFile("d/x.txt"); !errors.Is(err, ErrExcluded) {
		t.Errorf("d/x.txt: %q %v", b, err)
	}
	if len(s.scope.caps) != 2 || s.scope.caps["."].rel != ".arkignore" || s.scope.caps["d"].rel != "d/.arkignore" {
		t.Errorf("captured %v", s.scope.caps)
	}
	if _, ok := s.scope.caps["p/d"]; ok {
		t.Error("captured the rules of a directory no decision needed")
	}
	if _, err := s.RuleSet(false); !errors.Is(err, ErrScoped) {
		t.Errorf("RuleSet: %v", err)
	}
}

// A rule file the scoped snapshot cannot read refuses the paths it could
// apply to — and only those (as the path gate's rule reading).
func TestScopedUnreadableRules(t *testing.T) {
	w := ruleRepo(t)
	if err := os.Remove(w.path("repo/d/.arkignore")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(w.path("repo/d/.arkignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := scopedOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	if _, err := s.ReadFile("d/ok.txt"); !errors.Is(err, ErrRuleUnavailable) {
		t.Errorf("below the unreadable rules: %v", err)
	}
	w.write(t, "repo/plain.txt", "P")
	if b, err := s.ReadFile("plain.txt"); err != nil || string(b) != "P" {
		t.Errorf("elsewhere: %q %v", b, err)
	}
}

// B5: a rule file rewritten while the scoped snapshot captures it. A
// rewrite between the two readings makes it capture again, and decide by one
// version; a rewrite that never stops is ErrRuleChanged — nothing read. A
// rewrite after the capture is not seen for the snapshot's life (a request):
// the snapshot's guarantee is one version per request, not the latest.
func TestB5_RuleRewrittenDuringCapture(t *testing.T) {
	w := ruleRepo(t)
	s := scopedOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	n := 0
	withHook(t, func(event, rel string) {
		if event == "verify" && n == 0 {
			n++
			w.write(t, "repo/d/.arkignore", "ok.txt\n") // now ok.txt, not x.txt
		}
	})
	if b, err := s.ReadFile("d/ok.txt"); !errors.Is(err, ErrExcluded) {
		t.Errorf("d/ok.txt after the rewrite: %q %v (want the new version: excluded)", b, err)
	}
	if b, err := s.ReadFile("d/x.txt"); err != nil || string(b) != "D_SECRET" {
		t.Errorf("d/x.txt after the rewrite: %q %v (want the new version: admitted)", b, err)
	}

	// Rewritten at every attempt: refused.
	w2 := ruleRepo(t)
	s2 := scopedOf(t, pinTree(t, w2.path("repo"), fsroot.Options{}))
	k := 0
	withHook(t, func(event, rel string) {
		if event == "verify" {
			k++
			w2.write(t, "repo/d/.arkignore", fmt.Sprintf("x.txt\n# %d\n", k))
		}
	})
	if b, err := s2.ReadFile("d/ok.txt"); !errors.Is(err, ErrRuleChanged) {
		t.Errorf("rules never stable: %q %v (want ErrRuleChanged)", b, err)
	}

	// After the capture: the captured version stands (the known limit).
	withHook(t, func(string, string) {})
	w.write(t, "repo/d/.arkignore", "x.txt\n")
	if _, err := s.ReadFile("d/ok.txt"); !errors.Is(err, ErrExcluded) {
		t.Errorf("a rewrite after the capture changed the snapshot's decision: %v", err)
	}
}

// B6: the file is replaced right after the scoped snapshot admitted it:
// what is read is the object decided, or nothing.
func TestB6_ReplacedAfterTheDecision(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "repo/plain.txt", "PLAIN")
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	s := scopedOf(t, tr)
	for _, replace := range []func(){
		func() { mutate(t, "rename over", os.Rename(w.path("repo/root-secret.txt"), w.path("repo/plain.txt"))) },
		func() {
			os.Remove(w.path("repo/plain.txt"))
			w.link(t, "root-secret.txt", "repo/plain.txt")
		},
	} {
		w.write(t, "repo/root-secret.txt", "ROOT_SECRET")
		os.Remove(w.path("repo/plain.txt"))
		w.write(t, "repo/plain.txt", "PLAIN")
		b, err := tr.ReadFileChecked("plain.txt", func(r fsroot.Resolved) error {
			if err := s.Check(r); err != nil {
				return err
			}
			replace() // after the decision, before the read
			return nil
		})
		if strings.Contains(string(b), "ROOT_SECRET") || (err == nil && string(b) != "PLAIN") {
			t.Errorf("read %q %v", b, err)
		}
	}
}

// B9: the C1 cases (another spelling of an excluded path) decide alike.
func TestScopedDecidesAsFull_C1(t *testing.T) {
	w := c1Repo(t)
	n := compareScoped(t, "c1 fixture", w.path("repo"),
		"SECRET.TXT", "Secret.txt", "SECRETS/api-key.txt", "secrets/API-KEY.TXT", "SRC/PRIVATE.TXT",
		"ALIAS", "HIDDEN-LINK", "DLINK/API-KEY.TXT", "EXT-DIR/SECRET.TXT", "PUBLIC.TXT", "SRC/OPEN.TXT")
	t.Logf("%d decisions compared", n)
	s := scopedOf(t, pinTree(t, w.path("repo"), fsroot.Options{}))
	for _, rel := range []string{"SECRET.TXT", "SRC/PRIVATE.TXT", "SECRETS/api-key.txt", "abs-upper", "reenter"} {
		if b, err := s.ReadFile(rel); !errors.Is(err, ErrExcluded) {
			t.Errorf("%s: %q %v", rel, b, err)
		}
	}
}

// B4 (ABA): the scoped snapshot captured a decoy directory's rules (none);
// the real directory, whose rules exclude a file, is swapped in for the
// resolution that opens the file, and the decoy back before the decision
// captures anything. The resolution's directory is not the one whose rules
// were captured: refused.
func TestB4_SwapBackBeforeTheDecision(t *testing.T) {
	w := ruleRepo(t)
	w.write(t, "decoy/ok.txt", "DECOY_OK")
	w.write(t, "decoy/x.txt", "DECOY_X")
	mutate(t, "move the real directory", os.Rename(w.path("repo/d"), w.path("d-real")))
	mutate(t, "move the decoy in", os.Rename(w.path("decoy"), w.path("repo/d")))
	tr := pinTree(t, w.path("repo"), fsroot.Options{})
	s := scopedOf(t, tr)
	if b, err := s.ReadFile("d/ok.txt"); err != nil || string(b) != "DECOY_OK" {
		t.Fatalf("d/ok.txt: %q %v", b, err)
	}
	mutate(t, "move the decoy out", os.Rename(w.path("repo/d"), w.path("decoy")))
	mutate(t, "move the real directory in", os.Rename(w.path("d-real"), w.path("repo/d")))
	b, err := tr.ReadFileChecked("d/x.txt", func(r fsroot.Resolved) error {
		// The real directory was resolved and its file opened; the decoy
		// comes back before the decision.
		mutate(t, "move the real directory out", os.Rename(w.path("repo/d"), w.path("d-real")))
		mutate(t, "move the decoy back", os.Rename(w.path("decoy"), w.path("repo/d")))
		return s.Check(r)
	})
	if strings.Contains(string(b), "D_SECRET") || !errors.Is(err, ErrDirectoryChanged) {
		t.Errorf("read %q %v (want ErrDirectoryChanged)", b, err)
	}
}
