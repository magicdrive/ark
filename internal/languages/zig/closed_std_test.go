package zig

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestClosedTablesAgainstStd checks the world's std tables against a
// Zig std tree (ARK_ZIG_STD=<zig>/lib/std, Zig 0.17.0's):
//
//   - every method neutralMethods and graphFreeResults list is a pub fn of
//     std.Build (Build.zig, Build/), and no body of a function of the name
//     there creates, roots, binds or registers a module;
//   - every field neutralFields lists is a field of a std.Build or
//     std.Target type;
//   - every step kind stepKinds lists has a create.
//
// Run: ARK_ZIG_STD=/path/to/zig/lib/std go test ./internal/languages/zig -run TestClosedTablesAgainstStd -v
func TestClosedTablesAgainstStd(t *testing.T) {
	std := os.Getenv("ARK_ZIG_STD")
	if std == "" {
		t.Skip("ARK_ZIG_STD not set")
	}
	var sources []string
	read := func(p string) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, string(b))
	}
	read(filepath.Join(std, "Build.zig"))
	if err := filepath.Walk(filepath.Join(std, "Build"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".zig") {
			read(p)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	build := strings.Join(sources, "\n")
	affects := regexp.MustCompile(`Module\.create\(|createModule\(|addModule\(|\.addImport\(|addAnonymousImport\(|\.addOptions\(|\.existing|import_table|root_source_file\s*=|modules\.(put|getOrPut)`)
	for _, table := range []struct {
		name  string
		names map[string]bool
		// a container's method std.Build also declares must keep the graph
		// too (a graph value's method of the name may be std.Build's).
		optional bool
	}{{"neutralMethods", neutralMethods, false}, {"graphFreeResults", graphFreeResults, false}, {"containerMethods", containerMethods, true}} {
		for _, m := range sortedKeys(table.names) {
			def := regexp.MustCompile(`\n( *)pub fn ` + regexp.QuoteMeta(m) + `\(`)
			found := false
			for _, src := range sources {
				for _, loc := range def.FindAllStringSubmatchIndex(src, -1) {
					found = true
					indent := src[loc[2]:loc[3]]
					body := src[loc[1]:]
					if end := strings.Index(body, "\n"+indent+"}"); end >= 0 {
						body = body[:end]
					}
					if affects.MatchString(body) {
						t.Errorf("%s: std's %s creates, roots, binds or registers a module", table.name, m)
					}
				}
			}
			if !found && !table.optional {
				t.Errorf("%s: %s is no pub fn of std.Build", table.name, m)
			}
		}
	}
	for _, f := range sortedKeys(neutralFields) {
		if !regexp.MustCompile(`\n\s+` + regexp.QuoteMeta(f) + `: `).MatchString(build) {
			t.Errorf("neutralFields: %s is no field of std.Build", f)
		}
	}
	steps := filepath.Join(std, "Build", "Step")
	var kinds []string
	for k := range stepKinds {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		b, err := os.ReadFile(filepath.Join(steps, k+".zig"))
		if err != nil || !strings.Contains(string(b), "\npub fn create(") {
			t.Errorf("stepKinds: Step.%s has no create", k)
		}
	}
}
