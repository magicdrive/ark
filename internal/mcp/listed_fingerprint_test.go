package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
)

// listedRepo is a Go repository with nested .arkignore files, a negation, an
// internal directory symlink and directories no index enters.
func listedRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	for p, body := range map[string]string{
		"go.mod":                    "module example.com/repo\n\ngo 1.22\n",
		".arkignore":                "secret.go\n",
		"main.go":                   "package repo\n\nfunc Main() string { return Helper() }\n",
		"secret.go":                 "package repo\n\nfunc Secret() string { return \"SECRET_MARKER\" }\n",
		"pkg/a.go":                  "package pkg\n\nfunc A() {}\n",
		"pkg/inner/.arkignore":      "*.go\n!keep.go\n",
		"pkg/inner/drop.go":         "package inner\n\nfunc Drop() {}\n",
		"pkg/inner/keep.go":         "package inner\n\nfunc Keep() {}\n",
		"helper.go":                 "package repo\n\nfunc Helper() string { return \"\" }\n",
		"vendor/v/v.go":             "package v\n\nfunc V() {}\n",
		".tools/t.go":               "package t\n\nfunc T() {}\n",
		"pkg/-early/e.go":           "package early\n\nfunc E() {}\n",
		"pkg/-early/.arkignore":     "e.go\n",
		"pkg/inner/sub/.gitkeep":    "",
		"pkg/inner/sub/deep/z.go":   "package deep\n\nfunc Z() {}\n",
		"pkg/inner/sub/deep/z_test": "",
	} {
		write(t, root, p, body)
	}
	if err := os.Symlink("pkg", filepath.Join(root, "pkglink")); err != nil {
		t.Fatal(err)
	}
	return root
}

// A request's freshness check reuses the request's listing — no second walk
// — and computes exactly the fingerprint a walk computes, for the root, a
// subdirectory, and a subdirectory named through a symlink; it walks where
// the listing cannot stand in (below a directory no index enters).
func TestListedFingerprint_UsedAndEqual(t *testing.T) {
	root := listedRepo(t)
	providers := defaultProviders()
	for _, tc := range []struct {
		path   string
		listed bool
	}{
		{".", true}, {"pkg", true}, {"pkglink", true}, {"pkg/inner", true}, {"pkg/-early", true},
		{"vendor/v", false}, {".tools", false},
	} {
		h := NewToolsHandlerWithCache(root, nil, nil).forRequest()
		policy := h.accessPolicy()
		listing, ok := h.requestListing()
		if !ok {
			t.Fatal("the request's reader recorded no listing")
		}
		canonical, err := h.canonicalDir(filepath.Join(root, tc.path))
		if err != nil {
			t.Fatal(err)
		}
		listed, ok := listing.listedSources(canonical)
		if ok != tc.listed {
			t.Errorf("%s: listing usable %v, want %v", tc.path, ok, tc.listed)
			continue
		}
		if !ok {
			continue
		}
		want, err := index.SourceFingerprintExcluding(context.Background(), canonical, providers, policy.indexExclude)
		if err != nil {
			t.Fatal(err)
		}
		got, err := index.SourceFingerprintListed(context.Background(), canonical, providers, policy.indexExclude, listed)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: listed fingerprint differs from the walk's", tc.path)
		}
	}
}

// Warm requests on an unchanged repository reuse the index: the listed
// fingerprint matches the index's own, so nothing is rebuilt.
func TestListedFingerprint_WarmRequestsReuse(t *testing.T) {
	root := listedRepo(t)
	h := NewToolsHandler(root, nil)
	for i := 0; i < 5; i++ {
		callText(t, h, "get_diagnostics", map[string]interface{}{"path": "."})
		callText(t, h, "get_diagnostics", map[string]interface{}{"path": "pkg"})
	}
	builds, reuses, rebuilds, _ := cacheStats(h)
	if builds != 2 || rebuilds != 0 || reuses == 0 {
		t.Errorf("builds %d, reuses %d, rebuilds %d; want 2 builds, no rebuild", builds, reuses, rebuilds)
	}
}

// One server, changed between requests: every change is seen by the next
// request — including a rule change that brings an excluded file back.
func TestListedFingerprint_ChangesApplyToTheNextRequest(t *testing.T) {
	root := listedRepo(t)
	h := NewToolsHandler(root, nil)
	has := func(symbol string) bool {
		if strings.HasSuffix(symbol, ".go") { // a file: is it indexed at all?
			text, _ := callText(t, h, "search_code", map[string]interface{}{"path": ".", "format": "json"})
			return strings.Contains(text, `"`+symbol+`"`)
		}
		text, isErr := callText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": symbol})
		return !isErr && strings.Contains(text, symbol)
	}
	steps := []struct {
		name   string
		change func()
		symbol string
		want   bool
	}{
		{"initial: excluded", func() {}, "Secret", false},
		{"source added", func() { write(t, root, "added.go", "package repo\n\nfunc Added() {}\n") }, "Added", true},
		{"source edited", func() { write(t, root, "added.go", "package repo\n\nfunc Renamed() {}\n") }, "Renamed", true},
		{"source deleted", func() { _ = os.Remove(filepath.Join(root, "added.go")) }, "Renamed", false},
		{".arkignore edited: excluded file comes back", func() { write(t, root, ".arkignore", "helper.go\n") }, "Secret", true},
		{".arkignore edited: file excluded", func() {}, "Helper", false},
		{".arkignore deleted", func() { _ = os.Remove(filepath.Join(root, ".arkignore")) }, "Helper", true},
		{".arkignore added", func() { write(t, root, ".arkignore", "secret.go\n") }, "Secret", false},
		{"nested .arkignore added", func() { write(t, root, "pkg/.arkignore", "a.go\n") }, "A", false},
		{"negation changed", func() { write(t, root, "pkg/inner/.arkignore", "*.go\n!drop.go\n") }, "Drop", true},
		{"negation changed: other file excluded", func() {}, "Keep", false},
		{"symlink retargeted into an excluded file", func() {
			_ = os.Remove(filepath.Join(root, "pkglink"))
			if err := os.Symlink("secret.go", filepath.Join(root, "alias.go")); err != nil {
				t.Fatal(err)
			}
		}, "Secret", false},
		{"symlink retargeted to an admitted file", func() {
			_ = os.Remove(filepath.Join(root, "alias.go"))
			write(t, root, "lib/real.go", "package lib\n\nfunc Real() {}\n")
			if err := os.Symlink("lib/real.go", filepath.Join(root, "alias.go")); err != nil {
				t.Fatal(err)
			}
		}, "alias.go", true},
	}
	for _, s := range steps {
		s.change()
		if got := has(s.symbol); got != s.want {
			t.Errorf("%s: %s indexed %v, want %v", s.name, s.symbol, got, s.want)
		}
	}
}

// A server root that is itself a symlink: the rules under it are read, so no
// tool — including the index tools, which index the directory the link leads
// to — returns an excluded file.
func TestListedFingerprint_SymlinkedRootKeepsTheRules(t *testing.T) {
	root := listedRepo(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandler(link, nil)
	for _, call := range []struct {
		tool string
		args map[string]interface{}
	}{
		{"get_context", map[string]interface{}{"path": ".", "symbol": "Secret"}},
		{"search_context", map[string]interface{}{"query": "Secret"}},
		{"get_repository_map", map[string]interface{}{"path": ".", "format": "json", "detail": "verbose"}},
		{"search_code", map[string]interface{}{"path": ".", "format": "json"}},
		{"get_file_content", map[string]interface{}{"path": "secret.go"}},
	} {
		text, _ := callText(t, h, call.tool, call.args)
		// A refusal echoes the client's own path; only content and listings count.
		if strings.Contains(text, "SECRET_MARKER") || (call.tool != "get_file_content" && strings.Contains(text, "secret.go\"")) {
			t.Errorf("%s through a symlinked root returns the excluded file:\n%s", call.tool, text)
		}
	}
	if text, _ := callText(t, h, "get_context", map[string]interface{}{"path": ".", "symbol": "Main"}); !strings.Contains(text, "Main") {
		t.Errorf("symlinked root: visible code missing:\n%s", text)
	}
}

// The listing and the policy of one request come from one walk: a file added
// after the request read its policy is in neither.
func TestListedFingerprint_ListingIsThePolicysWalk(t *testing.T) {
	root := listedRepo(t)
	h := NewToolsHandler(root, nil).forRequest()
	h.accessPolicy()
	write(t, root, "late.go", "package repo\n\nfunc Late() {}\n")
	listing, ok := h.requestListing()
	if !ok {
		t.Fatal("no listing")
	}
	for _, e := range listing.entries {
		if filepath.Base(e.Path) == "late.go" {
			t.Fatal("the listing is of a later walk than the policy")
		}
	}
	if next, _ := NewToolsHandler(root, nil).forRequest().requestListing(); !strings.Contains(fmtEntries(next), "late.go") {
		t.Error("the next request does not list the new file")
	}
}

func fmtEntries(l sourceListing) string {
	var b strings.Builder
	for _, e := range l.entries {
		b.WriteString(e.Path + "\n")
	}
	return b.String()
}
