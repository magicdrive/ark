package language

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Descriptor is the single unit of language capability. It couples a language
// identity with its file extensions, tested support level, and extraction
// Provider.
//
// Descriptor deliberately does NOT carry a Tree-sitter grammar handle: grammar
// ownership stays in the composition root (internal/languages) so that this
// package remains free of any parser dependency. Tree-sitter must terminate
// inside the Provider boundary.
type Descriptor struct {
	Language     Language
	Extensions   []string
	SupportLevel SupportLevel
	Provider     Provider
}

// Registry is an immutable, deterministic lookup over a fixed set of language
// Descriptors. It is the mechanism; the concrete set of supported languages is
// assembled by the composition root (internal/languages) and passed to
// NewRegistry. A Registry is safe for concurrent reads and is never mutated
// after construction.
type Registry struct {
	descriptors []Descriptor            // registration order, canonical
	byLanguage  map[Language]Descriptor // identity lookup
	byExtension map[string]Descriptor   // lower-cased extension (incl. leading dot)
}

// NewRegistry validates and builds a Registry from descriptors. Descriptors are
// retained in the given order, which defines the deterministic iteration order
// of every accessor. It returns an error if any descriptor is malformed or if
// two descriptors collide on a language identity or file extension.
func NewRegistry(descriptors ...Descriptor) (*Registry, error) {
	r := &Registry{
		descriptors: make([]Descriptor, 0, len(descriptors)),
		byLanguage:  make(map[Language]Descriptor, len(descriptors)),
		byExtension: make(map[string]Descriptor),
	}
	for i, d := range descriptors {
		if d.Language == "" {
			return nil, fmt.Errorf("language: descriptor[%d] has empty Language", i)
		}
		if d.Provider == nil {
			return nil, fmt.Errorf("language: descriptor %q has nil Provider", d.Language)
		}
		if len(d.Extensions) == 0 {
			return nil, fmt.Errorf("language: descriptor %q has no Extensions", d.Language)
		}
		if _, dup := r.byLanguage[d.Language]; dup {
			return nil, fmt.Errorf("language: duplicate language %q", d.Language)
		}
		// Copy extensions so later mutation of the caller's slice cannot affect
		// the registry.
		d.Extensions = slices.Clone(d.Extensions)
		for _, ext := range d.Extensions {
			if ext == "" {
				return nil, fmt.Errorf("language: descriptor %q has empty extension", d.Language)
			}
			if !strings.HasPrefix(ext, ".") {
				return nil, fmt.Errorf("language: descriptor %q extension %q must start with '.'", d.Language, ext)
			}
			if ext != strings.ToLower(ext) {
				return nil, fmt.Errorf("language: descriptor %q extension %q must be lower-case", d.Language, ext)
			}
			if prev, dup := r.byExtension[ext]; dup {
				return nil, fmt.Errorf("language: extension %q claimed by both %q and %q", ext, prev.Language, d.Language)
			}
			r.byExtension[ext] = d
		}
		r.byLanguage[d.Language] = d
		r.descriptors = append(r.descriptors, d)
	}
	return r, nil
}

// MustNewRegistry is NewRegistry that panics on error. It is intended for the
// composition root, where the descriptor set is fixed at build time and any
// error is a programming mistake that should fail fast at startup.
func MustNewRegistry(descriptors ...Descriptor) *Registry {
	r, err := NewRegistry(descriptors...)
	if err != nil {
		panic(err)
	}
	return r
}

// Descriptors returns all descriptors in canonical (registration) order.
func (r *Registry) Descriptors() []Descriptor {
	return slices.Clone(r.descriptors)
}

// Languages returns all supported language identities in canonical order.
func (r *Registry) Languages() []Language {
	out := make([]Language, len(r.descriptors))
	for i, d := range r.descriptors {
		out[i] = d.Language
	}
	return out
}

// Providers returns all providers in canonical order.
func (r *Registry) Providers() []Provider {
	out := make([]Provider, len(r.descriptors))
	for i, d := range r.descriptors {
		out[i] = d.Provider
	}
	return out
}

// Lookup returns the descriptor for a language identity.
func (r *Registry) Lookup(lang Language) (Descriptor, bool) {
	d, ok := r.byLanguage[lang]
	return d, ok
}

// Provider returns the extraction provider for a language identity.
func (r *Registry) Provider(lang Language) (Provider, bool) {
	d, ok := r.byLanguage[lang]
	if !ok {
		return nil, false
	}
	return d.Provider, true
}

// SupportLevelFor returns the tested support level for a language identity, or
// SupportLevelNone if the language is not registered.
func (r *Registry) SupportLevelFor(lang Language) SupportLevel {
	if d, ok := r.byLanguage[lang]; ok {
		return d.SupportLevel
	}
	return SupportLevelNone
}

// DetectByExtension returns the descriptor whose extensions include ext. ext is
// matched case-insensitively and must include the leading dot (e.g. ".go").
func (r *Registry) DetectByExtension(ext string) (Descriptor, bool) {
	d, ok := r.byExtension[strings.ToLower(ext)]
	return d, ok
}

// DetectByFilename returns the descriptor for a filename, based on its
// extension.
func (r *Registry) DetectByFilename(name string) (Descriptor, bool) {
	return r.DetectByExtension(filepath.Ext(name))
}

// IsSupportedFilename reports whether the file can be handled by some provider.
func (r *Registry) IsSupportedFilename(name string) bool {
	_, ok := r.DetectByFilename(name)
	return ok
}

// Extensions returns every registered extension, sorted and de-duplicated.
func (r *Registry) Extensions() []string {
	out := make([]string, 0, len(r.byExtension))
	for ext := range r.byExtension {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

// ExtensionsFor returns the extensions registered for a language, sorted.
func (r *Registry) ExtensionsFor(lang Language) []string {
	d, ok := r.byLanguage[lang]
	if !ok {
		return nil
	}
	out := slices.Clone(d.Extensions)
	sort.Strings(out)
	return out
}
