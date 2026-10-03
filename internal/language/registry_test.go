package language_test

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// stubProvider is a minimal Provider for registry tests.
type stubProvider struct {
	lang language.Language
	exts []string
}

func (s stubProvider) Language() language.Language { return s.lang }
func (s stubProvider) Extensions() []string        { return s.exts }
func (s stubProvider) CacheVersion() string        { return "test" }
func (s stubProvider) Extract(context.Context, source.FileID, []byte) (language.Extraction, error) {
	return language.Extraction{}, nil
}

func desc(lang string, level language.SupportLevel, exts ...string) language.Descriptor {
	return language.Descriptor{
		Language:     language.Language(lang),
		Extensions:   exts,
		SupportLevel: level,
		Provider:     stubProvider{lang: language.Language(lang), exts: exts},
	}
}

func TestNewRegistry_Valid(t *testing.T) {
	r, err := language.NewRegistry(
		desc("go", language.SupportLevelGraph, ".go"),
		desc("python", language.SupportLevelReferences, ".py", ".pyw"),
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Canonical order is registration order.
	langs := r.Languages()
	if len(langs) != 2 || langs[0] != "go" || langs[1] != "python" {
		t.Errorf("Languages() = %v, want [go python]", langs)
	}

	if d, ok := r.DetectByFilename("main.go"); !ok || d.Language != "go" {
		t.Errorf("DetectByFilename(main.go) = %v,%v", d.Language, ok)
	}
	if d, ok := r.DetectByFilename("mod.PYW"); !ok || d.Language != "python" {
		t.Errorf("DetectByFilename is not case-insensitive: %v,%v", d.Language, ok)
	}
	if _, ok := r.DetectByFilename("x.rb"); ok {
		t.Error("DetectByFilename(x.rb) should be unsupported")
	}
	if r.SupportLevelFor("go") != language.SupportLevelGraph {
		t.Error("SupportLevelFor(go) wrong")
	}
	if r.SupportLevelFor("ruby") != language.SupportLevelNone {
		t.Error("SupportLevelFor(unknown) must be None")
	}
	if got := r.ExtensionsFor("python"); len(got) != 2 || got[0] != ".py" || got[1] != ".pyw" {
		t.Errorf("ExtensionsFor(python) = %v", got)
	}
}

func TestNewRegistry_Invalid(t *testing.T) {
	cases := map[string][]language.Descriptor{
		"empty language":    {desc("", language.SupportLevelNone, ".x")},
		"no extensions":     {desc("go", language.SupportLevelNone)},
		"non-dot extension": {desc("go", language.SupportLevelNone, "go")},
		"uppercase ext":     {desc("go", language.SupportLevelNone, ".GO")},
		"duplicate language": {
			desc("go", language.SupportLevelNone, ".go"),
			desc("go", language.SupportLevelNone, ".go2"),
		},
		"duplicate extension": {
			desc("go", language.SupportLevelNone, ".x"),
			desc("other", language.SupportLevelNone, ".x"),
		},
	}
	for name, descs := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := language.NewRegistry(descs...); err == nil {
				t.Errorf("expected error for %q", name)
			}
		})
	}
}

func TestNewRegistry_NilProvider(t *testing.T) {
	_, err := language.NewRegistry(language.Descriptor{
		Language:   "go",
		Extensions: []string{".go"},
	})
	if err == nil {
		t.Error("expected error for nil provider")
	}
}
