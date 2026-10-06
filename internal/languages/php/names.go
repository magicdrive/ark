package php

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"
)

// Lexical class-name resolution.
//
// PHP fixes the fully-qualified name (FQN) of every class-like name written in
// source from the file alone: the active `namespace`, the `use` imports in force
// and the name's own syntax. This file implements exactly those rules and
// nothing else — it never looks at other files, composer.json or PSR-4 — so the
// identity it produces can be compared with a declaration's Symbol.Qualified by
// the resolver without any path/module detour.
//
// The resolution rules (PHP manual, "Name resolution rules"), for class names:
//
//	\A\B        fully qualified            → A\B
//	namespace\B relative to the namespace  → <ns>\B
//	A\B         qualified: A is an import  → <import of A>\B, otherwise <ns>\A\B
//	B           unqualified: B is an import→ <import of B>,    otherwise <ns>\B
//
// Import (alias) matching is case-insensitive, as in PHP. The FQN itself is kept
// as written: whether it equals a declaration's name is the resolver's
// case-sensitive, deliberately conservative lookup.

// nameSegments returns the identifier segments of a name node
// (name / namespace_name / qualified_name / relative_name) in source order.
// Separators, a leading `\` and the `namespace` keyword are not segments.
func nameSegments(n *ts.Node, lang *ts.Language, src []byte) []string {
	if n == nil {
		return nil
	}
	if n.Type(lang) == "name" {
		return []string{n.Text(src)}
	}
	var out []string
	for i := 0; i < n.ChildCount(); i++ {
		out = append(out, nameSegments(n.Child(i), lang, src)...)
	}
	return out
}

// joinName renders segments as a canonical PHP namespace path (`A\B\C`).
func joinName(segs []string) string { return strings.Join(segs, "\\") }

// namespaceNameOf returns the canonical namespace path declared by a
// namespace_definition ("" for the global namespace). Declarations and
// references both derive their namespace from this one function, so a
// declaration's Symbol.Qualified and a reference's qualified identity are always
// spelled identically.
func namespaceNameOf(node *ts.Node, lang *ts.Language, src []byte) string {
	return joinName(nameSegments(childByType(node, lang, "namespace_name"), lang, src))
}

// isFullyQualifiedName reports whether a name node starts with `\`.
func isFullyQualifiedName(n *ts.Node, lang *ts.Language) bool {
	return n.ChildCount() > 0 && n.Child(0).Type(lang) == "\\"
}

// nameScope is the lexical name-resolution context at one point of a file: the
// active namespace and the class imports in force. A new scope starts at every
// namespace declaration (bracketed or not); imports never leak across them.
type nameScope struct {
	ns      string
	classes map[string]string // lower-cased alias → FQN (class-like imports only)
	clashes map[string]bool   // aliases imported more than once (invalid PHP): no identity
}

func newNameScope(ns string) *nameScope {
	return &nameScope{ns: ns, classes: map[string]string{}, clashes: map[string]bool{}}
}

// addClassImport records `use fqn as alias`. An alias imported twice is invalid
// PHP; rather than guess which import was meant, it denotes no identity.
func (s *nameScope) addClassImport(alias, fqn string) {
	key := strings.ToLower(alias)
	if _, dup := s.classes[key]; dup {
		s.clashes[key] = true
		return
	}
	s.classes[key] = fqn
}

// lookupImport returns the imported FQN for alias. found is false when there is
// no import; clash is true when the alias is ambiguous.
func (s *nameScope) lookupImport(alias string) (fqn string, found, clash bool) {
	key := strings.ToLower(alias)
	if s.clashes[key] {
		return "", false, true
	}
	fqn, found = s.classes[key]
	return fqn, found, false
}

// resolveClass returns the FQN denoted by a class-like name node, or ok == false
// when the language rules do not yield exactly one identity: the relative class
// names self/static/parent (late static binding / inheritance, not a lexical
// fact) and ambiguous imports.
func (s *nameScope) resolveClass(n *ts.Node, lang *ts.Language, src []byte) (fqn string, ok bool) {
	segs := nameSegments(n, lang, src)
	if len(segs) == 0 {
		return "", false
	}
	switch n.Type(lang) {
	case "name":
		if isRelativeType(strings.ToLower(segs[0])) {
			return "", false
		}
		return s.resolveFirst(segs)
	case "qualified_name":
		if isFullyQualifiedName(n, lang) {
			return joinName(segs), true
		}
		return s.resolveFirst(segs)
	case "relative_name":
		return qualify(s.ns, joinName(segs)), true
	}
	return "", false
}

// resolveFirst resolves a non-fully-qualified name whose first segment may be an
// import alias.
func (s *nameScope) resolveFirst(segs []string) (string, bool) {
	imp, found, clash := s.lookupImport(segs[0])
	if clash {
		return "", false
	}
	if found {
		return joinName(append([]string{imp}, segs[1:]...)), true
	}
	return qualify(s.ns, joinName(segs)), true
}

// useKind classifies what a `use` clause imports.
type useKind uint8

const (
	useClass useKind = iota // class, interface, trait, enum
	useFunction
	useConst
)

// visitUseClauses calls fn for every namespace_use_clause of a
// namespace_use_declaration with the canonical group prefix ("" when not
// grouped) and the declaration-level kind marker (`use function A\{b, c}`).
// It is the single traversal behind both ImportDraft extraction and the
// reference-side import table, so the two cannot disagree.
func visitUseClauses(node *ts.Node, lang *ts.Language, src []byte, fn func(clause *ts.Node, prefix string, declKind useKind)) {
	declKind := useClass
	for i := 0; i < node.ChildCount(); i++ {
		switch node.Child(i).Type(lang) {
		case "function":
			declKind = useFunction
		case "const":
			declKind = useConst
		}
	}
	if group := childByType(node, lang, "namespace_use_group"); group != nil {
		prefix := joinName(nameSegments(childByType(node, lang, "namespace_name"), lang, src))
		for i := 0; i < group.ChildCount(); i++ {
			if clause := group.Child(i); clause.Type(lang) == "namespace_use_clause" {
				fn(clause, prefix, declKind)
			}
		}
		return
	}
	for i := 0; i < node.ChildCount(); i++ {
		if clause := node.Child(i); clause.Type(lang) == "namespace_use_clause" {
			fn(clause, "", declKind)
		}
	}
}

// useClauseParts parses one namespace_use_clause: the imported path (without
// any group prefix, no leading `\`), the explicit `as` alias ("" when absent)
// and the clause-level kind marker.
func useClauseParts(clause *ts.Node, lang *ts.Language, src []byte) (path, alias string, kind useKind, ok bool) {
	var tail []string
	afterAs := false
	for i := 0; i < clause.ChildCount(); i++ {
		c := clause.Child(i)
		switch c.Type(lang) {
		case "function":
			kind = useFunction
		case "const":
			kind = useConst
		case "as":
			afterAs = true
		case "qualified_name":
			if !afterAs && tail == nil {
				tail = nameSegments(c, lang, src)
			}
		case "name":
			if afterAs {
				alias = c.Text(src)
			} else if tail == nil {
				tail = []string{c.Text(src)}
			}
		}
	}
	if len(tail) == 0 {
		return "", "", useClass, false
	}
	return joinName(tail), alias, kind, true
}

// classImports calls fn with (alias, FQN) for every class-like import of a
// namespace_use_declaration. `use function` and `use const` are not class
// imports and are skipped: functions and constants are out of scope for
// qualified identity (their namespace→global fallback has no single identity).
func classImports(node *ts.Node, lang *ts.Language, src []byte, fn func(alias, fqn string)) {
	visitUseClauses(node, lang, src, func(clause *ts.Node, prefix string, declKind useKind) {
		path, alias, kind, ok := useClauseParts(clause, lang, src)
		if !ok {
			return
		}
		if kind == useClass {
			kind = declKind
		}
		if kind != useClass {
			return
		}
		fqn := path
		if prefix != "" {
			fqn = prefix + "\\" + path
		}
		if alias == "" {
			alias = fqn[strings.LastIndex(fqn, "\\")+1:]
		}
		fn(alias, fqn)
	})
}
