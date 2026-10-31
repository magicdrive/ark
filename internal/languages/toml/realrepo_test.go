package toml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	bstoml "github.com/BurntSushi/toml"
	gotoml "github.com/pelletier/go-toml/v2"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// TestRealRepositories is an opt-in check of real repositories, read only:
// ARK_TOML_REAL_REPOS lists their roots (separated by the OS's path list
// separator). For each, the index (built without a cache, so nothing is
// written) must read every .toml file BurntSushi/toml and go-toml decode
// without an error diagnostic (and report one for every file either
// rejects), and, for a
// manifest, declare the package BurntSushi's decoding names and exactly
// the dependencies it lists — a Cargo manifest's dependency tables (target
// ones too) by key, a pyproject's requirements by their PEP 508 names. A
// path dependency or literal member whose manifest the repository holds
// resolves Exactly.
func TestRealRepositories(t *testing.T) {
	roots := os.Getenv("ARK_TOML_REAL_REPOS")
	if roots == "" {
		t.Skip("ARK_TOML_REAL_REPOS is not set")
	}
	for _, root := range filepath.SplitList(roots) {
		t.Run(filepath.Base(root), func(t *testing.T) {
			checkRealRepository(t, root)
		})
	}
}

func checkRealRepository(t *testing.T, root string) {
	idx, err := index.New(context.Background(), root, []language.Provider{NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	errs := map[source.FileID]bool{}
	for _, d := range idx.Diagnostics() {
		if d.Severity == language.SeverityError {
			errs[d.Location.File] = true
		}
	}
	var files, manifests, deps, symbols, invalid int
	for _, f := range idx.Files() {
		if filepath.Ext(string(f)) != ".toml" {
			continue
		}
		files++
		src, err := os.ReadFile(filepath.Join(root, string(f)))
		if err != nil {
			t.Fatal(err)
		}
		var m, m2 map[string]any
		_, decErr := bstoml.Decode(string(src), &m)
		if goErr := gotoml.Unmarshal(src, &m2); decErr == nil && goErr != nil {
			decErr = goErr // BurntSushi/toml accepts some TOML forbids
		}
		if decErr != nil {
			invalid++
			if !strings.Contains(string(f), "toml-test") {
				t.Logf("%s: rejected: %v", f, decErr)
			}
		}
		if (decErr == nil) == errs[f] {
			t.Errorf("%s: the decoders: %v; Ark reports an error: %v", f, decErr, errs[f])
		}
		syms := idx.SymbolsByFile(f)
		symbols += len(syms)
		base := filepath.Base(string(f))
		if decErr != nil || base != "Cargo.toml" && base != "pyproject.toml" {
			continue
		}
		manifests++
		var gotPkg string
		var gotDeps []string
		for _, s := range syms {
			switch s.Kind {
			case symbol.KindPackage:
				gotPkg = s.Name
			case symbol.KindDependency:
				gotDeps = append(gotDeps, s.Name)
			}
		}
		wantPkg, wantDeps := oracleManifest(base, m)
		sort.Strings(gotDeps)
		deps += len(gotDeps)
		if gotPkg != wantPkg || strings.Join(gotDeps, ",") != strings.Join(wantDeps, ",") {
			t.Errorf("%s: package %q deps %v; the decoder's %q %v", f, gotPkg, gotDeps, wantPkg, wantDeps)
		}
		for _, s := range syms {
			for _, e := range idx.GetCallees(s.ID) {
				if fmt.Sprint(e.Confidence) != "exact" {
					t.Errorf("%s: %s -> %v %s", f, s.Qualified, e.To, e.Confidence)
				}
			}
		}
	}
	t.Logf("%s: %d TOML files (%d the decoder rejects), %d manifests, %d symbols, %d dependencies", root, files, invalid, manifests, symbols, deps)
}

// oracleManifest reads the package and dependencies of a decoded manifest.
func oracleManifest(base string, m map[string]any) (string, []string) {
	tbl := func(v any) map[string]any { t, _ := v.(map[string]any); return t }
	var deps []string
	if base == "Cargo.toml" {
		pkg, _ := tbl(m["package"])["name"].(string)
		add := func(t map[string]any) {
			for _, n := range depTables {
				for k := range tbl(t[n]) {
					if k != "" {
						deps = append(deps, k)
					}
				}
			}
		}
		add(m)
		for _, cfg := range tbl(m["target"]) {
			add(tbl(cfg))
		}
		for k := range tbl(tbl(m["workspace"])["dependencies"]) {
			deps = append(deps, k)
		}
		if _, ok := tbl(m["package"])["name"]; !ok {
			pkg = ""
		}
		sort.Strings(deps)
		return pkg, deps
	}
	proj := tbl(m["project"])
	pkg, _ := proj["name"].(string)
	reqs := func(v any) {
		a, _ := v.([]any)
		for _, x := range a {
			if s, ok := x.(string); ok {
				if n := pep508(s); n != "" {
					deps = append(deps, n)
				}
			}
		}
	}
	reqs(proj["dependencies"])
	for _, g := range tbl(proj["optional-dependencies"]) {
		reqs(g)
	}
	for _, g := range tbl(m["dependency-groups"]) {
		reqs(g)
	}
	reqs(tbl(m["build-system"])["requires"])
	sort.Strings(deps)
	return pkg, deps
}

var depTables = []string{"dependencies", "dev-dependencies", "dev_dependencies", "build-dependencies", "build_dependencies"}

// pep508 is the distribution name a requirement starts with (written
// apart from the manifest package's own pattern, as an oracle).
func pep508(s string) string {
	s = strings.TrimLeft(s, " \t")
	i := 0
	for i < len(s) && (s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' || s[i] >= '0' && s[i] <= '9' || i > 0 && strings.IndexByte("._-", s[i]) >= 0) {
		i++
	}
	return strings.TrimRight(s[:i], "._-")
}
