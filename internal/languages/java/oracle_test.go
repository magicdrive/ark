package java

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestOracle_Javac compares every resolution of a Java repository with what
// javac resolves the same names to (docs/scripts/oracle/java-javac.java). It
// is opt-in:
//
//	ARK_JAVA_ORACLE_SRC=<project-dir> ARK_JAVA_ORACLE=<a.tsv>[,<b.tsv>...] \
//	  go test ./internal/languages/java -run TestOracle_Javac -v
//
// Every .java file of the project is extracted and resolved together, as the
// index does; the references of the files an oracle covers are checked:
//
//   - a resolved target (Exact, Strong) must be the declaration javac names
//     — one javac places outside the sources, or does not name at all where
//     javac does name one, is false;
//   - outside the repository must be where javac names no declaration of
//     the sources;
//   - a declaration of the sources javac names that Ark does not resolve to
//     is missed (recall).
//
// ARK_JAVA_ORACLE_MAX_FALSE (default 0) is the number of false resolutions
// the test accepts.
func TestOracle_Javac(t *testing.T) {
	srcDir, tsvs := os.Getenv("ARK_JAVA_ORACLE_SRC"), os.Getenv("ARK_JAVA_ORACLE")
	if srcDir == "" || tsvs == "" {
		t.Skip("set ARK_JAVA_ORACLE_SRC and ARK_JAVA_ORACLE (docs/scripts/oracle/java-javac.java)")
	}
	maxFalse := 0
	if v := os.Getenv("ARK_JAVA_ORACLE_MAX_FALSE"); v != "" {
		maxFalse, _ = strconv.Atoi(v)
	}
	oracle, covered := readOracle(t, strings.Split(tsvs, ","))
	fis := extractTree(t, srcDir)
	r := resolver.New(fis)
	refs := map[reference.ReferenceID]reference.Reference{}
	for _, fi := range fis {
		for _, ref := range fi.References {
			refs[ref.ID] = ref
		}
	}
	syms := map[symbol.SymbolID]symbol.Symbol{}
	for _, fi := range fis {
		for _, s := range fi.Symbols {
			syms[s.ID] = s
		}
	}

	type tally struct{ correct, falseRes, unverified int }
	byConf := map[resolver.Confidence]*tally{}
	var falses []string
	outsideRight, outsideFalse, candidateHas, candidateLacks := 0, 0, 0, 0
	resolvedAt := map[oracleKey]bool{}
	outcomeAt := map[oracleKey]string{} // Ark's outcome at a name: candidate, unresolved, outside, resolved
	var unverified []string
	for _, res := range r.Resolve() {
		ref := refs[res.ReferenceID]
		file := string(ref.Location.File)
		if !covered[file] {
			continue
		}
		key := oracleKey{file, ref.Location.Range.Start.Line, ref.Location.Range.Start.Column, oracleKind(ref)}
		want, known := oracle[key]
		switch {
		case res.HasUniqueTarget():
			outcomeAt[key] = "resolved-elsewhere"
		case res.OutsideRepository:
			outcomeAt[key] = "outside"
		case len(res.Candidates) > 0:
			outcomeAt[key] = "candidate"
		default:
			outcomeAt[key] = "unresolved"
		}
		describe := func(what string) string {
			return fmt.Sprintf("%s:%d:%d %s %q %s; javac %s; %v", file, key.line, key.col, ref.Kind, ref.Name, what, want, res.Evidence)
		}
		switch {
		case res.HasUniqueTarget():
			tl := byConf[res.Confidence]
			if tl == nil {
				tl = &tally{}
				byConf[res.Confidence] = tl
			}
			target := syms[res.Candidates[0].SymbolID]
			switch {
			case !known:
				tl.unverified++
				unverified = append(unverified, fmt.Sprintf("%s:%d:%d %s %q -> %s %s", file, key.line, key.col, ref.Kind, ref.Name, target.Qualified, res.Confidence))
			case want.matches(target):
				tl.correct++
				resolvedAt[key] = true
			default:
				tl.falseRes++
				falses = append(falses, describe(fmt.Sprintf("-> %s (%s:%d) %s", target.Qualified, target.Location.File, target.Location.Range.Start.Line, res.Confidence)))
			}
		case res.OutsideRepository:
			switch {
			case known && want.where == "src":
				outsideFalse++
				falses = append(falses, describe("outside the repository"))
			case known:
				outsideRight++
			}
		case len(res.Candidates) > 0 && known && want.where == "src":
			has := false
			for _, c := range res.Candidates {
				if want.matches(syms[c.SymbolID]) {
					has = true
				}
			}
			if has {
				candidateHas++
			} else {
				candidateLacks++
			}
		}
	}
	var confs []resolver.Confidence
	for c := range byConf {
		confs = append(confs, c)
	}
	sort.Slice(confs, func(i, j int) bool { return confs[i] > confs[j] })
	totalFalse := outsideFalse
	for _, c := range confs {
		tl := byConf[c]
		t.Logf("%-9s correct %6d  false %4d  unverified %5d", c, tl.correct, tl.falseRes, tl.unverified)
		totalFalse += tl.falseRes
	}
	t.Logf("outside   correct %6d  false %4d", outsideRight, outsideFalse)
	t.Logf("candidate sets holding javac's target %d, not holding it %d", candidateHas, candidateLacks)
	srcTotal, found := 0, 0
	byKind := map[string][2]int{}
	missed := map[string]int{}
	for key, want := range oracle {
		if want.where != "src" || !covered[key.file] {
			continue
		}
		srcTotal++
		k := byKind[key.kind]
		k[0]++
		if resolvedAt[key] {
			found++
			k[1]++
		} else {
			o := outcomeAt[key]
			if o == "" {
				o = "no reference"
			}
			missed[key.kind+" "+o]++
			if l := os.Getenv("ARK_JAVA_ORACLE_MISSED"); l != "" && l == key.kind+" "+o {
				t.Logf("MISSED %s:%d:%d %s -> %s", key.file, key.line, key.col, key.kind, want)
			}
		}
		byKind[key.kind] = k
	}
	var mk []string
	for k := range missed {
		mk = append(mk, k)
	}
	sort.Strings(mk)
	for _, k := range mk {
		t.Logf("missed %-28s %6d", k, missed[k])
	}
	if os.Getenv("ARK_JAVA_ORACLE_UNVERIFIED") != "" {
		sort.Strings(unverified)
		for _, u := range unverified {
			t.Logf("UNVERIFIED %s", u)
		}
	}
	if srcTotal > 0 {
		t.Logf("recall: %d of %d names javac resolves to the sources (%.1f%%)", found, srcTotal, 100*float64(found)/float64(srcTotal))
		for _, k := range []string{"type", "method", "ctor", "field"} {
			if v := byKind[k]; v[0] > 0 {
				t.Logf("  %-6s %6d of %6d (%.1f%%)", k, v[1], v[0], 100*float64(v[1])/float64(v[0]))
			}
		}
	}
	sort.Strings(falses)
	for i, f := range falses {
		if i == 400 {
			t.Logf("... %d more", len(falses)-400)
			break
		}
		t.Logf("FALSE %s", f)
	}
	if totalFalse > maxFalse {
		t.Errorf("%d false resolutions (accepted %d)", totalFalse, maxFalse)
	}
}

type oracleKey struct {
	file      string
	line, col uint32
	kind      string
}

// oracleTarget is javac's resolution of one name.
type oracleTarget struct {
	where       string // src, ext, implicit
	file        string
	first, last uint32
	identity    string
}

func (o oracleTarget) String() string {
	if o.where == "src" {
		return fmt.Sprintf("%s:%d-%d %s", o.file, o.first, o.last, o.identity)
	}
	return o.where + ":" + o.identity
}

// matches reports whether s is the declaration o names: in its file, within
// its lines, of its identity (when it has one: a local or anonymous type's
// members have none).
func (o oracleTarget) matches(s symbol.Symbol) bool {
	if o.where != "src" || string(s.Location.File) != o.file {
		return false
	}
	line := s.Location.Range.Start.Line
	if line < o.first || line > o.last {
		return false
	}
	return o.identity == "" || o.identity == s.Qualified
}

// oracleKind is the oracle's kind of what ref names.
func oracleKind(ref reference.Reference) string {
	switch ref.Kind {
	case reference.KindCall:
		if ref.Name == "<init>" {
			return "ctor"
		}
		return "method"
	case reference.KindValueReference, reference.KindWrite, reference.KindRead:
		return "field"
	}
	return "type"
}

func readOracle(t *testing.T, paths []string) (map[oracleKey]oracleTarget, map[string]bool) {
	t.Helper()
	out := map[oracleKey]oracleTarget{}
	covered := map[string]bool{}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			cols := strings.Split(sc.Text(), "\t")
			if len(cols) != 6 {
				continue
			}
			line, _ := strconv.Atoi(cols[1])
			col, _ := strconv.Atoi(cols[2])
			key := oracleKey{cols[0], uint32(line), uint32(col), cols[3]}
			covered[cols[0]] = true
			var tg oracleTarget
			switch {
			case strings.HasPrefix(cols[5], "src:"):
				parts := strings.SplitN(strings.TrimPrefix(cols[5], "src:"), ":", 4)
				if len(parts) != 4 {
					continue
				}
				first, _ := strconv.Atoi(parts[1])
				last, _ := strconv.Atoi(parts[2])
				tg = oracleTarget{where: "src", file: parts[0], first: uint32(first), last: uint32(last), identity: parts[3]}
			case strings.HasPrefix(cols[5], "ext:"):
				tg = oracleTarget{where: "ext", identity: strings.TrimPrefix(cols[5], "ext:")}
			case strings.HasPrefix(cols[5], "implicit:"):
				tg = oracleTarget{where: "implicit", identity: strings.TrimPrefix(cols[5], "implicit:")}
			default:
				continue
			}
			if prev, dup := out[key]; dup && prev != tg {
				// javac names two declarations at one name (never seen):
				// unverifiable.
				delete(out, key)
				continue
			}
			out[key] = tg
		}
		f.Close()
	}
	return out, covered
}

// extractTree extracts every .java file under dir (hidden directories
// excepted) as the index does.
func extractTree(t *testing.T, dir string) []resolver.FileIndex {
	t.Helper()
	p := NewProvider()
	var fis []resolver.FileIndex
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != dir {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".java") {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		ex, err := p.Extract(context.Background(), source.FileID(rel), src)
		if err != nil {
			return err
		}
		fis = append(fis, index.NewFileIndex(p, source.FileID(rel), ex))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fis
}
