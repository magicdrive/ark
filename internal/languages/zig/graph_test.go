package zig

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
)

const fixtureRepo = "testdata/repo"

func buildIndex(t testing.TB, dir string) *index.RepositoryIndex {
	t.Helper()
	idx, err := index.New(context.Background(), dir, []language.Provider{NewProvider()})
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	return idx
}

// edgeLines renders every forward edge as "from -kind-> to confidence".
func edgeLines(idx *index.RepositoryIndex) []string {
	var out []string
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			out = append(out, fmt.Sprintf("%s -%s-> %s %s", s.Qualified, e.Kind, to.Qualified, e.Confidence))
		}
	}
	sort.Strings(out)
	return out
}

// TestGraph_Fixture compares the fixture's whole graph with the expected
// one. Every edge is Exact; main -> Engine::run (`e.run()`, e a result of
// Engine.init of another file) and main -> Vec::length (through the
// re-export math.Vec and init's declared result type) are reached through
// the types those files state. Absent on purpose:
//   - main -> posix / windows Backend::open: Backend is a comptime choice
//     between the two (Candidate);
//   - anything through std, builtin: named modules, which the build maps.
func TestGraph_Fixture(t *testing.T) {
	idx := buildIndex(t, fixtureRepo)
	equalLines(t, "edges", edgeLines(idx), []string{
		"build.zig::build -references-> build.zig::std exact",
		"src/engine.zig::Engine::Self -uses_type-> src/engine.zig::Engine exact",
		"src/engine.zig::Engine::init -uses_type-> src/engine.zig::Engine::Self exact",
		"src/engine.zig::Engine::run -calls-> src/engine.zig::Engine::step exact",
		"src/engine.zig::Engine::run -calls-> src/util/strings.zig::upper exact",
		"src/engine.zig::Engine::run -references-> src/engine.zig::strings exact",
		"src/engine.zig::Engine::run -uses_type-> src/engine.zig::Engine::Self exact",
		"src/engine.zig::Engine::step -uses_type-> src/engine.zig::Engine::Self exact",
		"src/engine.zig::strings -imports-> src/util/strings.zig exact",
		"src/list.zig::List::()::Self -uses_type-> src/list.zig::List::() exact",
		"src/list.zig::List::()::count -uses_type-> src/list.zig::List::()::Self exact",
		"src/list.zig::List::()::push -calls-> src/list.zig::List::()::count exact",
		"src/list.zig::List::()::push -uses_type-> src/list.zig::List::()::Self exact",
		"src/main.zig::Engine -references-> src/engine.zig::Engine exact",
		"src/main.zig::Engine -references-> src/main.zig::engine exact",
		"src/main.zig::engine -imports-> src/engine.zig exact",
		"src/main.zig::list -imports-> src/list.zig exact",
		"src/main.zig::main -calls-> src/engine.zig::Engine::init exact",
		"src/main.zig::main -calls-> src/engine.zig::Engine::run exact",
		"src/main.zig::main -calls-> src/list.zig::List exact",
		"src/main.zig::main -calls-> src/list.zig::List::()::count exact",
		"src/main.zig::main -calls-> src/math.zig::add exact",
		"src/main.zig::main -calls-> src/vec.zig::Vec::init exact",
		"src/main.zig::main -calls-> src/vec.zig::Vec::length exact",
		"src/main.zig::main -references-> src/main.zig::Engine exact",
		"src/main.zig::main -references-> src/main.zig::list exact",
		"src/main.zig::main -references-> src/main.zig::math exact",
		"src/main.zig::main -references-> src/main.zig::platform exact",
		"src/main.zig::main -references-> src/main.zig::std exact",
		"src/main.zig::main -references-> src/math.zig::Vec exact",
		"src/main.zig::main -references-> src/platform.zig::Backend exact",
		"src/main.zig::math -imports-> src/math.zig exact",
		"src/main.zig::platform -imports-> src/platform.zig exact",
		"src/math.zig::Vec -references-> src/math.zig::vec exact",
		"src/math.zig::Vec -references-> src/vec.zig::Vec exact",
		"src/math.zig::vec -imports-> src/vec.zig exact",
		"src/platform.zig::Backend -references-> src/platform.zig::builtin exact",
		"src/platform.zig::Backend -references-> src/platform.zig::posix exact",
		"src/platform.zig::Backend -references-> src/platform.zig::windows exact",
		"src/platform.zig::Backend -references-> src/posix.zig::Backend exact",
		"src/platform.zig::Backend -references-> src/windows.zig::Backend exact",
		"src/platform.zig::posix -imports-> src/posix.zig exact",
		"src/platform.zig::windows -imports-> src/windows.zig exact",
		"src/vec.zig::Vec::init -uses_type-> src/vec.zig::Vec exact",
		"src/vec.zig::Vec::length -uses_type-> src/vec.zig::Vec exact",
	})
}
