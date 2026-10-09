package index

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/symbol"
)

// SymbolID collisions.
//
// A SymbolID is the first 64 bits of a SHA-256 over a declaration's identity
// (symbol.NewDeclarationID). Two distinct declarations sharing one is
// astronomically unlikely, but every map of the index is keyed by it: a
// collision would silently merge two declarations — one overwriting the
// other in symbolsByID, their edges, parents and context mixed.
//
// The index therefore refuses to exist with one. Every declaration is checked
// as it is ingested; if two distinct declarations (any field differs) carry
// one ID, the build stops before resolution and returns an
// *IdentityCollisionError instead of an index: no graph, parent or context is
// ever derived from a merged ID, and no consumer needs a guard of its own.
// Keeping one of the two, or dropping both, would not be safe either: the
// survivor would change which declaration a name resolves to.
//
// IDs are not cached (the cache stores extraction drafts), so a warm build
// detects exactly what a cold one does, and a failed build is never published.

// IDFunc assigns a declaration's SymbolID; production uses
// symbol.NewDeclarationID. Another function is only for tests that force
// collisions.
type IDFunc func(lang, repoRelPath string, kind symbol.SymbolKind, qualified string, ordinal int) symbol.SymbolID

// IdentityCollision is one SymbolID carried by several distinct declarations.
type IdentityCollision struct {
	ID           symbol.SymbolID
	Declarations []symbol.Symbol // sorted by file, position, kind, qualified name
}

// IdentityCollisionError is the build failure for SymbolID collisions. Its
// collisions are sorted by ID, so the error is the same whatever order the
// files were read in.
type IdentityCollisionError struct {
	Collisions []IdentityCollision
}

// maxReportedCollisions bounds the collisions listed in Error(); the count
// covers them all.
const maxReportedCollisions = 10

func (e *IdentityCollisionError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "index: %d SymbolID collision(s) between distinct declarations; the index is not built rather than merge them:", len(e.Collisions))
	for i, c := range e.Collisions {
		if i == maxReportedCollisions {
			fmt.Fprintf(&b, " ... and %d more", len(e.Collisions)-i)
			break
		}
		var decls []string
		for _, d := range c.Declarations {
			decls = append(decls, declarationString(d))
		}
		fmt.Fprintf(&b, " [%s: %s]", c.ID, strings.Join(decls, " | "))
	}
	return b.String()
}

// Diagnostics states each colliding declaration as a diagnostic
// (language.DiagSymbolIDCollision), in the error's order.
func (e *IdentityCollisionError) Diagnostics() []language.Diagnostic {
	var out []language.Diagnostic
	for _, c := range e.Collisions {
		for i, d := range c.Declarations {
			var others []string
			for j, o := range c.Declarations {
				if j != i {
					others = append(others, declarationString(o))
				}
			}
			out = append(out, language.Diagnostic{
				Severity: language.SeverityError,
				Code:     language.DiagSymbolIDCollision,
				Location: d.Location,
				Message: fmt.Sprintf("SymbolID %s of %s %s is also the ID of %s; the index is not built",
					c.ID, d.Kind, d.Qualified, strings.Join(others, ", ")),
			})
		}
	}
	return out
}

func declarationString(d symbol.Symbol) string {
	return fmt.Sprintf("%s %s %s at %s:%d:%d", d.Language, d.Kind, d.Qualified,
		d.Location.File, d.Location.Range.Start.Line, d.Location.Range.Start.Column)
}

// observeID records sym under its ID, noting a collision when the ID already
// names a different declaration.
func (b *builder) observeID(sym symbol.Symbol) {
	if prev, ok := b.symbolsByID[sym.ID]; ok && prev != sym {
		if b.collided == nil {
			b.collided = make(map[symbol.SymbolID]bool)
		}
		b.collided[sym.ID] = true
	}
	b.symbolsByID[sym.ID] = sym
}

// identityError returns the collisions found while ingesting, or nil.
func (b *builder) identityError() error {
	if len(b.collided) == 0 {
		return nil
	}
	byID := make(map[symbol.SymbolID][]symbol.Symbol, len(b.collided))
	for _, syms := range b.symbolsByFile {
		for _, s := range syms {
			if !b.collided[s.ID] || slices.Contains(byID[s.ID], s) {
				continue
			}
			byID[s.ID] = append(byID[s.ID], s)
		}
	}
	err := &IdentityCollisionError{}
	for id, decls := range byID {
		sort.Slice(decls, func(i, j int) bool { return declarationLess(decls[i], decls[j]) })
		err.Collisions = append(err.Collisions, IdentityCollision{ID: id, Declarations: decls})
	}
	sort.Slice(err.Collisions, func(i, j int) bool { return err.Collisions[i].ID < err.Collisions[j].ID })
	return err
}

// declarationLess is a total order over declarations (fields of a Symbol
// that can differ between two declarations sharing an ID).
func declarationLess(a, b symbol.Symbol) bool {
	if a.Location.File != b.Location.File {
		return a.Location.File < b.Location.File
	}
	if a.Location.Range != b.Location.Range {
		return positionBefore(a.Location.Range, b.Location.Range)
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Qualified != b.Qualified {
		return a.Qualified < b.Qualified
	}
	return fmt.Sprintf("%+v", a) < fmt.Sprintf("%+v", b)
}

// NewWithIDs builds an index like NewWithCache, assigning SymbolIDs with ids.
// It exists to test collision handling; production builds use New or
// NewWithCache, which assign symbol.NewDeclarationID.
func NewWithIDs(ctx context.Context, root string, providers []language.Provider, store cache.Store, ids IDFunc) (*RepositoryIndex, error) {
	return newWithCache(ctx, root, providers, store, ids)
}
