package php

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

var phpSource = []byte(`<?php
namespace App\Service;

class UserService {
    public function find(int $id): int {
        return $id;
    }
}

function topLevel(): void {
    echo "hi";
}
`)

func TestPHPProvider_Identity(t *testing.T) {
	p := NewProvider()
	if p.Language() != "php" {
		t.Errorf("Language() = %q, want php", p.Language())
	}
	exts := p.Extensions()
	if len(exts) != 1 || exts[0] != ".php" {
		t.Errorf("Extensions() = %v, want [.php]", exts)
	}
	if p.CacheVersion() == "" {
		t.Error("CacheVersion() is empty")
	}
}

// TestPHPProvider_ParseEmpty verifies the PHP-1 contract: valid source parses
// without error and yields an empty Extraction (no extraction implemented yet).
func TestPHPProvider_ParseEmpty(t *testing.T) {
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("svc.php"), phpSource)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(ext.Symbols) != 0 || len(ext.References) != 0 || len(ext.Imports) != 0 {
		t.Errorf("PHP-1 must produce empty extraction; got symbols=%d refs=%d imports=%d",
			len(ext.Symbols), len(ext.References), len(ext.Imports))
	}
	if len(ext.Diagnostics) != 0 {
		t.Errorf("valid source should produce no diagnostics; got %v", ext.Diagnostics)
	}
}

// TestPHPProvider_Safety verifies Extract never panics and never returns an
// error on adversarial inputs (broken php, invalid utf-8, empty, nul bytes).
func TestPHPProvider_Safety(t *testing.T) {
	inputs := map[string][]byte{
		"empty":        {},
		"invalid_utf8": {0xff, 0xfe, 0x00, 0x80},
		"nul":          {0x00, 0x00},
		"no_php_tag":   []byte("just some plain text, no php tag"),
		"broken":       []byte("<?php class Broken { public function m( {\n function good() {} "),
		"only_tag":     []byte("<?php"),
	}
	p := NewProvider()
	for name, src := range inputs {
		src := src
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on %q: %v", name, r)
				}
			}()
			if _, err := p.Extract(context.Background(), source.FileID("x.php"), src); err != nil {
				t.Errorf("unexpected error on %q: %v", name, err)
			}
		})
	}
}

// TestPHPProvider_Deterministic verifies repeated extraction is identical.
func TestPHPProvider_Deterministic(t *testing.T) {
	p := NewProvider()
	a, _ := p.Extract(context.Background(), source.FileID("svc.php"), phpSource)
	b, _ := p.Extract(context.Background(), source.FileID("svc.php"), phpSource)
	if len(a.Symbols) != len(b.Symbols) || len(a.References) != len(b.References) {
		t.Error("non-deterministic extraction")
	}
}
