package resolver_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
)

// A Mutable export (a CommonJS exports object property) maps its name to the
// source declaration, but the value an importer reads is not proven to be
// that declaration: a resolution through one is a Candidate at most, whatever
// else the walk proves (docs/development/web-semantic-fidelity/
// commonjs-boundary/contract.md). These tests pin the language-neutral cap.

func (f *fileB) exportMutable(exported, local string) *fileB {
	f.fi.Exports = append(f.fi.Exports, language.ExportDraft{
		Kind: language.ExportLocal, Exported: exported, Local: local, Mutable: true, Location: f.at()})
	return f
}

func TestMutableExport_NamedBindingIsCandidate(t *testing.T) {
	r := newRepo()
	r.file("src/cjs.js", true).fn("parse").exportMutable("parse", "parse")
	r.file("src/esm.js", true).fn("parse").exportLocal("parse", "parse")
	r.file("src/use.js", true).fn("run").
		bind("p", "parse", mod("./cjs", "src/cjs.js"), false).
		bind("q", "parse", mod("./esm", "src/esm.js"), false)
	res := r.resolve(t, "src/use.js", "p", container("run"), kind(reference.KindCall))
	wantTarget(t, res, resolver.ConfidenceCandidate, "src/cjs.js", "parse")
	wantNoEdge(t, res, resolver.ConfidenceCandidate)
	// The control: the same shape through a live binding stays Exact.
	wantTarget(t, r.resolve(t, "src/use.js", "q", container("run"), kind(reference.KindCall)),
		resolver.ConfidenceExact, "src/esm.js", "parse")
}

// The cap follows the walk through a re-export chain: an ESM module that
// re-exports a CommonJS export does not prove its value.
func TestMutableExport_ThroughReExportChain(t *testing.T) {
	r := newRepo()
	r.file("src/cjs.js", true).fn("parse").exportMutable("parse", "parse")
	r.file("src/barrel.js", true).exportFrom("parse", "parse", mod("./cjs", "src/cjs.js"))
	r.file("src/all.js", true).exportAll(mod("./cjs", "src/cjs.js"))
	r.file("src/use.js", true).fn("run").
		bind("a", "parse", mod("./barrel", "src/barrel.js"), false).
		bind("b", "parse", mod("./all", "src/all.js"), false)
	for _, name := range []string{"a", "b"} {
		wantTarget(t, r.resolve(t, "src/use.js", name, container("run"), kind(reference.KindCall)),
			resolver.ConfidenceCandidate, "src/cjs.js", "parse")
	}
}

// A namespace receiver (const m = require('./m'); m.parse()) reaches the name
// through the same export.
func TestMutableExport_NamespaceReceiverIsCandidate(t *testing.T) {
	r := newRepo()
	r.file("src/cjs.js", true).fn("parse").fn("keep").exportMutable("parse", "parse").exportLocal("keep", "keep")
	r.file("src/use.js", true).fn("run").bind("m", "", mod("./cjs", "src/cjs.js"), false)
	wantTarget(t, r.resolve(t, "src/use.js", "parse", recv("m"), container("run"), kind(reference.KindCall)),
		resolver.ConfidenceCandidate, "src/cjs.js", "parse")
	// Only the walk through the Mutable export is capped: another export of
	// the same module keeps its own evidence.
	wantTarget(t, r.resolve(t, "src/use.js", "keep", recv("m"), container("run"), kind(reference.KindCall)),
		resolver.ConfidenceExact, "src/cjs.js", "keep")
}

// A receiver bound to a Mutable export (const api = require('./m') where
// module.exports = Api) caps the member lookup on it too.
func TestMutableExport_ReceiverBindingCapsMembers(t *testing.T) {
	r := newRepo()
	r.file("src/cjs.js", true).class("Api").method("Api", "parse").exportMutable("module.exports", "Api")
	r.file("src/use.js", true).fn("run").bind("api", "module.exports", mod("./cjs", "src/cjs.js"), false)
	res := r.resolve(t, "src/use.js", "parse", recv("api"), container("run"), kind(reference.KindCall))
	wantNoEdge(t, res, resolver.ConfidenceCandidate)
}
