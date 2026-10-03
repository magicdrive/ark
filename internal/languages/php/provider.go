// Package php provides a Tree-sitter-based extraction Provider for PHP source.
//
// PHP-1 scope: grammar + parser wiring only. Extract parses the source and
// returns an empty Extraction (no symbols/references/imports yet); a parse
// failure is reported as a diagnostic rather than an error. Symbol, reference,
// import, and relation extraction arrive in later PHP-n stages.
//
// Invariants that already apply at this stage: Extract never panics, never
// returns a Tree-sitter node, and keeps all byte ranges inside the source.
package php

import (
	"context"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// Provider extracts code intelligence from PHP source files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "php" }
func (p *Provider) Extensions() []string        { return []string{".php"} }

// CacheVersion must change whenever extraction semantics change. Bump it as
// PHP-n stages add symbol/reference extraction.
func (p *Provider) CacheVersion() string { return "php-1" }

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

	// PHP-1: parser is wired, but no symbols/references/imports are extracted
	// yet. The tree is parsed and released here so the pipeline is exercised
	// end-to-end without leaking any Tree-sitter node past this boundary.
	_ = tree.RootNode()
	return language.Extraction{}, nil
}
