package repomap

import (
	"testing"

	"github.com/magicdrive/ark/internal/symbol"
)

func TestScoreSymbol_EntryPointHighest(t *testing.T) {
	main := SymbolEntry{Name: "main", Kind: symbol.KindFunction, Exported: false}
	exported := SymbolEntry{Name: "Foo", Kind: symbol.KindFunction, Exported: true}
	pkg := PackageEntry{}

	if scoreSymbol(main, pkg) <= scoreSymbol(exported, pkg) {
		t.Error("main function should score higher than an ordinary exported symbol")
	}
}

func TestScoreSymbol_ExportedHigherThanUnexported(t *testing.T) {
	exp := SymbolEntry{Name: "Foo", Exported: true}
	unexp := SymbolEntry{Name: "foo", Exported: false}
	pkg := PackageEntry{}

	if scoreSymbol(exp, pkg) <= scoreSymbol(unexp, pkg) {
		t.Error("exported symbol should score higher than unexported")
	}
}

func TestScoreSymbol_GeneratedPenalty(t *testing.T) {
	sym := SymbolEntry{Name: "Foo", Exported: true}
	normal := PackageEntry{}
	generated := PackageEntry{IsGenerated: true}

	if scoreSymbol(sym, generated) >= scoreSymbol(sym, normal) {
		t.Error("generated package should apply a score penalty")
	}
}

func TestScoreSymbol_TestPenalty(t *testing.T) {
	sym := SymbolEntry{Name: "Foo", Exported: true}
	normal := PackageEntry{}
	testPkg := PackageEntry{IsTest: true}

	if scoreSymbol(sym, testPkg) >= scoreSymbol(sym, normal) {
		t.Error("test package should apply a score penalty")
	}
}

func TestScoreSymbol_InboundEdgesIncreasesScore(t *testing.T) {
	low := SymbolEntry{Name: "Foo", Exported: true, InboundEdges: 0}
	high := SymbolEntry{Name: "Bar", Exported: true, InboundEdges: 5}
	pkg := PackageEntry{}

	if scoreSymbol(high, pkg) <= scoreSymbol(low, pkg) {
		t.Error("more inbound edges should increase score")
	}
}

func TestPackageScore_EntryHighest(t *testing.T) {
	entry := PackageEntry{IsEntry: true}
	normal := PackageEntry{}
	vendor := PackageEntry{IsVendor: true}

	if packageScore(entry) <= packageScore(normal) {
		t.Error("entry point package should score higher than normal")
	}
	if packageScore(vendor) >= packageScore(normal) {
		t.Error("vendor package should score lower than normal")
	}
}

// Ranking is kind-aware: with equal graph evidence a type outranks a method,
// which outranks a constructor and data members.
func TestScoreSymbol_KindAware(t *testing.T) {
	pkg := PackageEntry{}
	score := func(k symbol.SymbolKind) int {
		return scoreSymbol(SymbolEntry{Name: "X", Kind: k, Exported: true, InboundEdges: 2}, pkg)
	}
	if !(score(symbol.KindClass) > score(symbol.KindFunction) && score(symbol.KindFunction) > score(symbol.KindMethod) &&
		score(symbol.KindMethod) > score(symbol.KindProperty) && score(symbol.KindProperty) > score(symbol.KindConstructor)) {
		t.Errorf("kind order wrong: class %d function %d method %d property %d constructor %d",
			score(symbol.KindClass), score(symbol.KindFunction), score(symbol.KindMethod), score(symbol.KindProperty), score(symbol.KindConstructor))
	}
}

// A package of many members or constants does not outrank a smaller package
// that defines the structure the rest of the repository depends on; test
// packages come after the code they exercise; members are listed with their
// type, without namespace.
func TestPackageScore_SizeIsSublinearAndCentralityCounts(t *testing.T) {
	giant := PackageEntry{surface: 400 * symbolWeight(symbol.KindConstant)}
	core := PackageEntry{surface: 6 * symbolWeight(symbol.KindClass), inbound: 40}
	if packageScore(giant) >= packageScore(core) {
		t.Errorf("giant constant package %d >= central package %d", packageScore(giant), packageScore(core))
	}
	big := PackageEntry{surface: 200 * symbolWeight(symbol.KindClass), IsTest: true}
	small := PackageEntry{surface: 2 * symbolWeight(symbol.KindClass)}
	if packageScore(big) >= packageScore(small) {
		t.Errorf("test package %d >= production package %d", packageScore(big), packageScore(small))
	}
	for q, want := range map[string]string{`App\Http\Controller.index`: "Controller.index", "UserService.Create": "UserService.Create", "pkg/mod.fn": "mod.fn", "": "name"} {
		if got := (SymbolEntry{Name: "name", Qualified: q}).label(); got != want {
			t.Errorf("label(%q) = %q, want %q", q, got, want)
		}
	}
}
