// Package php provides a Tree-sitter-based extraction Provider for PHP source.
//
// PHP-3 scope: top-level symbols (PHP-2) PLUS class/interface/trait/enum member
// symbols with semantic containment — method, constructor, property, class
// constant, enum case, and constructor-promoted property. Members carry
// SymbolDraft.Parent (the container's qualified name) and Receiver (the
// container's bare name) so the existing index builder/resolver can construct
// symbol.ParentQualified and perform receiver matching without any PHP-specific
// logic. NOT yet: imports/use, references, resolution, graph relations.
//
// Qualified-name policy (D1): namespace segments keep "\"; member segments use
// ".". Qualified is an Ark-internal symbol identity, not PHP source syntax.
//
// Container safety: member extraction reads ONLY the direct children of a
// container's body list (declaration_list / enum_declaration_list). It never
// descends into method bodies, so closures, arrow functions, and anonymous
// classes are not mis-attributed as members of the enclosing type.
//
// Invariants: Extract never panics, never returns a Tree-sitter node, and keeps
// all byte ranges inside the source.
package php

import (
	"context"
	"strings"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/treediag"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Provider extracts code intelligence from PHP source files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "php" }
func (p *Provider) Extensions() []string        { return []string{".php"} }

// CacheVersion must change whenever extraction semantics change.
func (p *Provider) CacheVersion() string { return "php-12" }

func (p *Provider) Extract(_ context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.PhpLanguage()
	parser := ts.NewParser(lang)
	tree, err := parser.Parse(src)
	if err != nil {
		return language.Extraction{
			Diagnostics: []language.Diagnostic{treediag.ParseFailed(file, err)},
		}, nil
	}
	defer tree.Release()

	var syms []language.SymbolDraft
	var imports []language.ImportDraft
	// Namespace context is threaded explicitly through the walk — never stored
	// in global/shared state — so extraction is deterministic and reentrant.
	extractContainer(tree.RootNode(), lang, src, file, "", &syms, &imports)
	refs := extractReferences(tree.RootNode(), lang, src, file)
	return language.Extraction{Symbols: syms, References: refs, Imports: imports, Diagnostics: treediag.ParseErrors(tree.RootNode(), lang, file)}, nil
}

// extractContainer walks the direct children of node (the program root, or a
// bracketed namespace body) extracting top-level declarations and namespace
// imports. ns is the active namespace path ("" = global namespace).
func extractContainer(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, ns string, out *[]language.SymbolDraft, imp *[]language.ImportDraft) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Type(lang) {
		case "namespace_definition":
			ns = phpNamespace(child, lang, src, file, ns, out, imp)
		case "namespace_use_declaration":
			// Namespace import (NOT trait `use`, which is a use_declaration
			// inside a declaration_list and is never seen here).
			appendImports(child, lang, src, file, imp)
		case "class_declaration":
			appendType(child, lang, src, file, ns, symbol.KindClass, out)
		case "interface_declaration":
			appendType(child, lang, src, file, ns, symbol.KindInterface, out)
		case "trait_declaration":
			appendType(child, lang, src, file, ns, symbol.KindTrait, out)
		case "enum_declaration":
			appendType(child, lang, src, file, ns, symbol.KindEnum, out)
		case "function_definition":
			appendNamed(child, lang, src, file, ns, symbol.KindFunction, out)
		case "const_declaration":
			// Global constants: no container.
			appendConstElements(child, lang, src, file, ns, "", "", out)
		}
	}
}

// phpNamespace handles a namespace_definition. See PHP-2 for the statement vs
// bracketed semantics.
func phpNamespace(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, outerNS string, out *[]language.SymbolDraft, imp *[]language.ImportDraft) string {
	name := namespaceNameOf(node, lang, src)
	if name != "" {
		*out = append(*out, language.SymbolDraft{
			Name:      name,
			Qualified: name,
			Kind:      symbol.KindNamespace,
			Location:  nodeLocation(node, file),
			StartByte: node.StartByte(),
			EndByte:   node.EndByte(),
			Exported:  true,
		})
	}
	if body := childByType(node, lang, "compound_statement"); body != nil {
		extractContainer(body, lang, src, file, name, out, imp)
		return outerNS
	}
	return name
}

// appendImports flattens a namespace_use_declaration into one ImportDraft per
// semantic import clause (D6). It handles non-grouped and grouped forms, plus
// per-clause `function`/`const` kind markers. Import kind (class/function/
// const) is intentionally not represented: the existing ImportDraft has no kind
// field and the import TARGET + alias are what Ark's import consumers use.
func appendImports(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, imp *[]language.ImportDraft) {
	visitUseClauses(node, lang, src, func(clause *ts.Node, prefix string, _ useKind) {
		if d, ok := parseUseClause(clause, lang, src, file, prefix); ok {
			*imp = append(*imp, d)
		}
	})
}

// parseUseClause parses one namespace_use_clause into an ImportDraft. prefix is
// the canonical group prefix for grouped use ("" otherwise). Path keeps PHP
// namespace identity with a leading "\" stripped (D1 / §8); Alias is the
// explicit `as` name or "" (no alias → ImportDraft convention of empty = use
// base name).
func parseUseClause(clause *ts.Node, lang *ts.Language, src []byte, file source.FileID, prefix string) (language.ImportDraft, bool) {
	tail, alias, _, ok := useClauseParts(clause, lang, src)
	if !ok {
		return language.ImportDraft{}, false
	}
	path := tail
	if prefix != "" {
		path = prefix + "\\" + tail
	}
	path = normalizeNamespacePath(path)
	return language.ImportDraft{
		Path:     path,
		Alias:    alias,
		Location: nodeLocation(clause, file),
	}, true
}

// normalizeNamespacePath strips a single leading namespace separator so that
// `\App\Model\User` and `App\Model\User` resolve to the same Ark identity (§8).
func normalizeNamespacePath(path string) string {
	return strings.TrimPrefix(path, "\\")
}

// appendNamed extracts a single named declaration (function; also used for the
// container symbol itself) whose identifier is a direct `name` child.
func appendNamed(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, ns string, kind symbol.SymbolKind, out *[]language.SymbolDraft) {
	name := childText(node, lang, src, "name")
	if name == "" {
		return
	}
	*out = append(*out, language.SymbolDraft{
		Name:      name,
		Qualified: qualify(ns, name),
		Kind:      kind,
		Location:  nodeLocation(node, file),
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Exported:  true,
	})
}

// appendType extracts a class/interface/trait/enum declaration AND its members.
func appendType(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, ns string, kind symbol.SymbolKind, out *[]language.SymbolDraft) {
	name := childText(node, lang, src, "name")
	if name == "" {
		return
	}
	qualified := qualify(ns, name)
	*out = append(*out, language.SymbolDraft{
		Name:      name,
		Qualified: qualified,
		Kind:      kind,
		Location:  nodeLocation(node, file),
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Exported:  true,
	})

	// Member body: declaration_list for class/interface/trait, or
	// enum_declaration_list for enum. Only direct children are inspected.
	body := childByType(node, lang, "declaration_list")
	if body == nil {
		body = childByType(node, lang, "enum_declaration_list")
	}
	if body != nil {
		extractMembers(body, lang, src, file, qualified, name, out)
	}
}

// extractMembers reads the DIRECT children of a container body. It never
// descends into method bodies.
func extractMembers(body *ts.Node, lang *ts.Language, src []byte, file source.FileID, containerQual, containerName string, out *[]language.SymbolDraft) {
	for i := 0; i < body.ChildCount(); i++ {
		member := body.Child(i)
		switch member.Type(lang) {
		case "method_declaration":
			appendMethod(member, lang, src, file, containerQual, containerName, out)
		case "property_declaration":
			appendProperties(member, lang, src, file, containerQual, containerName, out)
		case "const_declaration":
			appendConstElements(member, lang, src, file, "", containerQual, containerName, out)
		case "enum_case":
			appendEnumCase(member, lang, src, file, containerQual, containerName, out)
			// Note: use_declaration (trait use) and others are intentionally
			// ignored until PHP-4/PHP-5.
		}
	}
}

// appendMethod extracts a method/constructor and any constructor-promoted
// properties. __construct maps to KindConstructor; other magic methods remain
// KindMethod; __destruct is NOT a constructor.
func appendMethod(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, containerQual, containerName string, out *[]language.SymbolDraft) {
	name := childText(node, lang, src, "name")
	if name == "" {
		return
	}
	kind := symbol.KindMethod
	if name == "__construct" {
		kind = symbol.KindConstructor
	}
	*out = append(*out, language.SymbolDraft{
		Name:      name,
		Qualified: containerQual + "." + name,
		Kind:      kind,
		Location:  nodeLocation(node, file),
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Parent:    containerQual,
		Receiver:  containerName,
		Exported:  visibilityExported(node, lang, src),

		Visibility: visibilityOf(node, lang, src),
	})

	// Constructor property promotion: promoted parameters are class properties.
	// They live in formal_parameters as property_promotion_parameter nodes;
	// plain simple_parameter nodes are NOT properties.
	if fp := childByType(node, lang, "formal_parameters"); fp != nil {
		for i := 0; i < fp.ChildCount(); i++ {
			param := fp.Child(i)
			if param.Type(lang) != "property_promotion_parameter" {
				continue
			}
			pname := variableName(param, lang, src)
			if pname == "" {
				continue
			}
			*out = append(*out, language.SymbolDraft{
				Name:      pname,
				Qualified: containerQual + "." + pname,
				Kind:      symbol.KindProperty,
				Location:  nodeLocation(param, file),
				StartByte: param.StartByte(),
				EndByte:   param.EndByte(),
				Parent:    containerQual,
				Receiver:  containerName,
				Exported:  visibilityExported(param, lang, src),

				Visibility: visibilityOf(param, lang, src),
			})
		}
	}
}

// appendProperties extracts each property_element in a property_declaration.
func appendProperties(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, containerQual, containerName string, out *[]language.SymbolDraft) {
	exported := visibilityExported(node, lang, src)
	for i := 0; i < node.ChildCount(); i++ {
		el := node.Child(i)
		if el.Type(lang) != "property_element" {
			continue
		}
		pname := variableName(el, lang, src)
		if pname == "" {
			continue
		}
		*out = append(*out, language.SymbolDraft{
			Name:      pname,
			Qualified: containerQual + "." + pname,
			Kind:      symbol.KindProperty,
			Location:  nodeLocation(el, file),
			StartByte: el.StartByte(),
			EndByte:   el.EndByte(),
			Parent:    containerQual,
			Receiver:  containerName,
			Exported:  exported,

			Visibility: visibilityOf(node, lang, src),
		})
	}
}

// appendConstElements extracts each const_element in a const_declaration. When
// containerQual is empty the constants are global (no parent); otherwise they
// are class/enum constants with Parent set.
func appendConstElements(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, ns, containerQual, containerName string, out *[]language.SymbolDraft) {
	exported := visibilityExported(node, lang, src)
	for i := 0; i < node.ChildCount(); i++ {
		el := node.Child(i)
		if el.Type(lang) != "const_element" {
			continue
		}
		name := childText(el, lang, src, "name")
		if name == "" {
			continue
		}
		d := language.SymbolDraft{
			Name:      name,
			Kind:      symbol.KindConstant,
			Location:  nodeLocation(el, file),
			StartByte: el.StartByte(),
			EndByte:   el.EndByte(),
			Exported:  exported,
		}
		if containerQual != "" {
			d.Qualified = containerQual + "." + name
			d.Parent = containerQual
			d.Receiver = containerName
			d.Visibility = visibilityOf(node, lang, src)
		} else {
			d.Qualified = qualify(ns, name)
		}
		*out = append(*out, d)
	}
}

// appendEnumCase extracts an enum case as a constant whose parent is the enum.
func appendEnumCase(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, containerQual, containerName string, out *[]language.SymbolDraft) {
	name := childText(node, lang, src, "name")
	if name == "" {
		return
	}
	*out = append(*out, language.SymbolDraft{
		Name:      name,
		Qualified: containerQual + "." + name,
		Kind:      symbol.KindConstant,
		Location:  nodeLocation(node, file),
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Parent:    containerQual,
		Receiver:  containerName,
		Exported:  true, // enum cases are always publicly accessible

		Visibility: "public",
	})
}

// visibilityExported maps PHP member visibility to the Ark Exported flag.
// public (or omitted → PHP default public) → true; protected/private → false.
func visibilityExported(node *ts.Node, lang *ts.Language, src []byte) bool {
	v := childText(node, lang, src, "visibility_modifier")
	return v == "" || v == "public"
}

// visibilityOf returns a member's declared visibility; an omitted modifier is
// PHP's default, public.
func visibilityOf(node *ts.Node, lang *ts.Language, src []byte) string {
	if v := childText(node, lang, src, "visibility_modifier"); v != "" {
		return v
	}
	return "public"
}

// variableName returns the bare name (no leading "$") of the variable_name
// child of node, e.g. property_element or property_promotion_parameter.
func variableName(node *ts.Node, lang *ts.Language, src []byte) string {
	vn := childByType(node, lang, "variable_name")
	if vn == nil {
		return ""
	}
	return childText(vn, lang, src, "name")
}

// qualify joins a namespace path and a name using PHP's "\" namespace separator
// (D1). A global-namespace symbol has a bare qualified name.
func qualify(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + "\\" + name
}

func childByType(node *ts.Node, lang *ts.Language, nodeType string) *ts.Node {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == nodeType {
			return child
		}
	}
	return nil
}

func childText(node *ts.Node, lang *ts.Language, src []byte, nodeType string) string {
	if c := childByType(node, lang, nodeType); c != nil {
		return c.Text(src)
	}
	return ""
}

func nodeLocation(node *ts.Node, file source.FileID) source.Location {
	return source.Location{
		File: file,
		Range: source.Range{
			Start: source.Position{Line: node.StartPoint().Row + 1, Column: node.StartPoint().Column + 1},
			End:   source.Position{Line: node.EndPoint().Row + 1, Column: node.EndPoint().Column + 1},
		},
	}
}
