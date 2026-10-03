// Package php provides a Tree-sitter-based extraction Provider for PHP source.
//
// PHP-2 scope: top-level symbol extraction with namespace context.
// Extracted: namespace, class, interface, trait, enum, function, global
// constant. NOT yet extracted (later PHP-n stages): class members (method,
// property, class constant, enum case), containment/Parent, imports/use,
// references, resolution, and graph relations.
//
// Qualified-name policy (D1): namespace segments keep PHP's "\" separator;
// member separators (PHP-3+) will use ".". Qualified is an Ark-internal symbol
// identity, not a reproduction of PHP source syntax.
//
// Invariants: Extract never panics, never returns a Tree-sitter node, and keeps
// all byte ranges inside the source.
package php

import (
	"context"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Provider extracts code intelligence from PHP source files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "php" }
func (p *Provider) Extensions() []string        { return []string{".php"} }

// CacheVersion must change whenever extraction semantics change.
func (p *Provider) CacheVersion() string { return "php-2" }

func (p *Provider) Extract(_ context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.PhpLanguage()
	parser := ts.NewParser(lang)
	tree, err := parser.Parse(src)
	if err != nil {
		return language.Extraction{
			Diagnostics: []language.Diagnostic{{
				Severity: language.SeverityError,
				Message:  "parse failed: " + err.Error(),
				Location: source.Location{File: file},
			}},
		}, nil
	}
	defer tree.Release()

	var drafts []language.SymbolDraft
	// Namespace context is threaded explicitly through the walk — never stored
	// in global/shared state — so extraction is deterministic and reentrant.
	extractContainer(tree.RootNode(), lang, src, file, "", &drafts)
	return language.Extraction{Symbols: drafts}, nil
}

// extractContainer walks the direct children of node (the program root, or a
// bracketed namespace body) extracting top-level declarations. ns is the active
// namespace path ("" = global namespace).
func extractContainer(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, ns string, out *[]language.SymbolDraft) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Type(lang) {
		case "namespace_definition":
			ns = phpNamespace(child, lang, src, file, ns, out)
		case "class_declaration":
			appendDecl(child, lang, src, file, ns, symbol.KindClass, out)
		case "interface_declaration":
			appendDecl(child, lang, src, file, ns, symbol.KindInterface, out)
		case "trait_declaration":
			appendDecl(child, lang, src, file, ns, symbol.KindTrait, out)
		case "enum_declaration":
			appendDecl(child, lang, src, file, ns, symbol.KindEnum, out)
		case "function_definition":
			appendDecl(child, lang, src, file, ns, symbol.KindFunction, out)
		case "const_declaration":
			appendConsts(child, lang, src, file, ns, out)
		}
	}
}

// phpNamespace handles a namespace_definition. It emits the namespace symbol and
// returns the namespace path that applies to subsequent sibling declarations.
// For the bracketed form (`namespace X { ... }`) it recurses into the body with
// the namespace in scope and returns the *unchanged* outer namespace, because a
// bracketed namespace does not leak to its siblings.
func phpNamespace(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, outerNS string, out *[]language.SymbolDraft) string {
	name := childText(node, lang, src, "namespace_name")
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
		// Bracketed namespace: scope applies only within the body.
		extractContainer(body, lang, src, file, name, out)
		return outerNS
	}
	// Statement namespace: applies to following siblings.
	return name
}

// appendDecl extracts a single named declaration (class/interface/trait/enum/
// function) whose identifier is a direct `name` child.
func appendDecl(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, ns string, kind symbol.SymbolKind, out *[]language.SymbolDraft) {
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

// appendConsts extracts top-level `const A = ..., B = ...;` declarations. Each
// const_element becomes its own constant symbol. Dynamic define() calls are not
// treated as constant declarations.
func appendConsts(node *ts.Node, lang *ts.Language, src []byte, file source.FileID, ns string, out *[]language.SymbolDraft) {
	for i := 0; i < node.ChildCount(); i++ {
		el := node.Child(i)
		if el.Type(lang) != "const_element" {
			continue
		}
		name := childText(el, lang, src, "name")
		if name == "" {
			continue
		}
		*out = append(*out, language.SymbolDraft{
			Name:      name,
			Qualified: qualify(ns, name),
			Kind:      symbol.KindConstant,
			Location:  nodeLocation(el, file),
			StartByte: el.StartByte(),
			EndByte:   el.EndByte(),
			Exported:  true,
		})
	}
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
