package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The directories the shared binary records for go test's result cache
// (recordInputs) cover the build of ark: the module root (main.go, go.mod,
// go.sum), cmd/ark, the internal packages and the directories of embedded
// files — and nothing outside the module. Were they to come back empty or
// partial, a cached result could outlive a change to ark's sources.
func TestRecordInputs_CoverTheBuild(t *testing.T) {
	b := sharedBinary{pkg: arkBinary.pkg, env: os.Environ(), wd: arkBinary.wd}
	var flags []string
	if raceEnabled {
		flags = append(flags, "-race")
	}
	dirs, err := b.recordInputs(flags)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join(b.wd, b.pkg))
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, d := range dirs {
		have[d] = true
		if d != root && !strings.HasPrefix(d, root+string(filepath.Separator)) {
			t.Errorf("recorded %s, outside the module %s", d, root)
		}
	}
	for _, rel := range []string{".", "cmd/ark", "internal/commandline", "internal/mcp", "internal/textbank/description_template"} {
		if d := filepath.Join(root, filepath.FromSlash(rel)); !have[d] {
			t.Errorf("not recorded: %s (of %d directories)", d, len(dirs))
		}
	}
}
