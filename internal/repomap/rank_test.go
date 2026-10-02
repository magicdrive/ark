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
