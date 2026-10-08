// Package conformance provides a provider-agnostic contract test suite
// (Phase 2 of the Multi-Language Expansion plan). Every language Provider —
// existing and future (PHP/Java/C#…) — must pass RunContract.
//
// RunContract asserts ONLY the invariants that every current provider already
// satisfies. Aspirational quality requirements that are not yet met by all
// providers (e.g. partial extraction and diagnostics on broken source) are NOT
// asserted here; they are tracked in IMPROVEMENTS.md and surfaced by the
// non-failing audit TestQualityCandidates (conformance_test.go).
package conformance

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// Case is a named source input for a provider.
type Case struct {
	Name   string // subtest name, e.g. "basic"
	File   string // logical file id, e.g. "user.go"
	Source []byte
}

// safetyInputs are language-agnostic adversarial inputs. Every provider must
// survive them without panicking and without emitting out-of-bounds ranges.
func safetyInputs() []Case {
	return []Case{
		{Name: "empty", File: "empty", Source: []byte{}},
		{Name: "invalid_utf8", File: "invalid_utf8", Source: []byte{0xff, 0xfe, 0x00, 0x80, 0x81}},
		{Name: "nul_bytes", File: "nul_bytes", Source: []byte{0x00, 0x00, 0x00, 0x00}},
		{Name: "plain_text", File: "plain_text", Source: []byte("the quick brown fox jumps over the lazy dog")},
		{Name: "huge_identifier", File: "huge_identifier", Source: []byte(strings.Repeat("a", 100000))},
	}
}

// RunContract applies the full provider contract to p. valid should be a small
// set of representative, syntactically valid sources for the provider's
// language; broken/garbage inputs are supplied internally.
func RunContract(t *testing.T, p language.Provider, valid []Case) {
	t.Helper()

	t.Run("LanguageIdentity", func(t *testing.T) { checkLanguageIdentity(t, p) })

	t.Run("ValidCorpus", func(t *testing.T) {
		if len(valid) == 0 {
			t.Fatal("no valid corpus cases supplied")
		}
		for _, c := range valid {
			c := c
			t.Run(c.Name, func(t *testing.T) {
				ext := mustExtract(t, p, c)
				checkSymbolInvariants(t, c, ext)
				checkReferenceInvariants(t, ext)
				checkImportInvariants(t, ext)
				checkModuleBindingInvariants(t, ext)
				checkNoErrorDiagnostics(t, ext)
			})
		}
	})

	t.Run("Determinism", func(t *testing.T) {
		for _, c := range valid {
			c := c
			t.Run(c.Name, func(t *testing.T) {
				a := mustExtract(t, p, c)
				b := mustExtract(t, p, c)
				if !reflect.DeepEqual(a, b) {
					t.Errorf("non-deterministic extraction for %q", c.Name)
				}
			})
		}
	})

	// Safety: never panic, never produce out-of-bounds ranges, on ANY input
	// (valid corpus + adversarial inputs).
	t.Run("Safety", func(t *testing.T) {
		all := slices.Clone(valid)
		all = append(all, safetyInputs()...)
		for _, c := range all {
			c := c
			t.Run(c.Name, func(t *testing.T) {
				checkNoPanic(t, p, c)
				ext := mustExtract(t, p, c)
				checkByteRanges(t, c, ext)
			})
		}
	})
}

// checkNoErrorDiagnostics: syntactically valid source yields no error
// diagnostic — a diagnostic must mean something is not analyzed.
func checkNoErrorDiagnostics(t *testing.T, ext language.Extraction) {
	t.Helper()
	for _, d := range ext.Diagnostics {
		if d.Severity == language.SeverityError {
			t.Errorf("valid source produced an error diagnostic: %+v", d)
		}
	}
}

// CheckBrokenSourceDiagnosed asserts that syntactically broken source yields
// at least one error diagnostic naming the file: breakage is never silent.
func CheckBrokenSourceDiagnosed(t *testing.T, p language.Provider, c Case) {
	t.Helper()
	ext := mustExtract(t, p, c)
	for _, d := range ext.Diagnostics {
		if d.Severity == language.SeverityError && string(d.Location.File) == c.File {
			return
		}
	}
	t.Errorf("broken source %q yielded no error diagnostic: %+v", c.Name, ext.Diagnostics)
}

func checkLanguageIdentity(t *testing.T, p language.Provider) {
	t.Helper()
	if p.Language() == "" {
		t.Error("Language() is empty")
	}
	exts := p.Extensions()
	if len(exts) == 0 {
		t.Error("Extensions() is empty")
	}
	seen := map[string]bool{}
	for _, e := range exts {
		if e == "" {
			t.Error("Extensions() contains empty string")
			continue
		}
		if e != strings.ToLower(e) {
			t.Errorf("extension %q is not lowercase", e)
		}
		if !strings.HasPrefix(e, ".") {
			t.Errorf("extension %q does not start with '.'", e)
		}
		if seen[e] {
			t.Errorf("duplicate extension %q", e)
		}
		seen[e] = true
	}
	if p.CacheVersion() == "" {
		t.Error("CacheVersion() is empty")
	}
}

func checkSymbolInvariants(t *testing.T, c Case, ext language.Extraction) {
	t.Helper()
	n := uint32(len(c.Source))
	for i, s := range ext.Symbols {
		if s.Name == "" {
			t.Errorf("symbol[%d]: Name is empty", i)
		}
		if s.Qualified == "" {
			t.Errorf("symbol[%d] %q: Qualified is empty", i, s.Name)
		}
		if s.Kind == "" || string(s.Kind) == "unknown" {
			t.Errorf("symbol[%d] %q: Kind is %q (unknown/empty not allowed)", i, s.Name, s.Kind)
		}
		if s.StartByte > s.EndByte {
			t.Errorf("symbol[%d] %q: StartByte(%d) > EndByte(%d)", i, s.Name, s.StartByte, s.EndByte)
		}
		if s.EndByte > n {
			t.Errorf("symbol[%d] %q: EndByte(%d) exceeds source length(%d)", i, s.Name, s.EndByte, n)
		}
		if s.Location.File == "" {
			t.Errorf("symbol[%d] %q: Location.File is empty", i, s.Name)
		}
	}
}

func checkReferenceInvariants(t *testing.T, ext language.Extraction) {
	t.Helper()
	for i, r := range ext.References {
		if r.Name == "" {
			t.Errorf("reference[%d]: Name is empty", i)
		}
		if r.Kind == "" {
			t.Errorf("reference[%d] %q: Kind is empty", i, r.Name)
		}
		if r.Location.File == "" {
			t.Errorf("reference[%d] %q: Location.File is empty", i, r.Name)
		}
	}
}

func checkImportInvariants(t *testing.T, ext language.Extraction) {
	t.Helper()
	for i, im := range ext.Imports {
		if im.Path == "" {
			t.Errorf("import[%d]: Path is empty", i)
		}
		if im.Location.File == "" {
			t.Errorf("import[%d] %q: Location.File is empty", i, im.Path)
		}
	}
}

// checkModuleBindingInvariants enforces the language-neutral ModuleSpec /
// BindingDraft / ExportDraft contract (clean root-relative candidates, sorted
// by (Priority, File), well-formed binding and export kinds).
func checkModuleBindingInvariants(t *testing.T, ext language.Extraction) {
	t.Helper()
	for _, err := range language.ValidateModuleBindings(ext) {
		t.Errorf("module binding contract: %v", err)
	}
}

func checkByteRanges(t *testing.T, c Case, ext language.Extraction) {
	t.Helper()
	n := uint32(len(c.Source))
	for i, s := range ext.Symbols {
		if s.StartByte > s.EndByte || s.EndByte > n {
			t.Errorf("symbol[%d] %q: out-of-bounds range [%d,%d] (len=%d)", i, s.Name, s.StartByte, s.EndByte, n)
		}
	}
}

func checkNoPanic(t *testing.T, p language.Provider, c Case) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic on input %q: %v", c.Name, r)
		}
	}()
	_, _ = p.Extract(context.Background(), source.FileID(c.File), c.Source)
}

func mustExtract(t *testing.T, p language.Provider, c Case) language.Extraction {
	t.Helper()
	ext, err := p.Extract(context.Background(), source.FileID(c.File), c.Source)
	if err != nil {
		t.Fatalf("Extract(%q) returned error: %v", c.Name, err)
	}
	return ext
}
