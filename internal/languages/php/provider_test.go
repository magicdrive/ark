package php

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// helper: run Extract and index symbols by Name.
func symbolsByName(t *testing.T, src string) map[string]sym {
	t.Helper()
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("test.php"), []byte(src))
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(ext.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", ext.Diagnostics)
	}
	m := make(map[string]sym, len(ext.Symbols))
	for _, d := range ext.Symbols {
		m[d.Name] = sym{kind: d.Kind, qualified: d.Qualified, exported: d.Exported}
	}
	return m
}

type sym struct {
	kind      symbol.SymbolKind
	qualified string
	exported  bool
}

func TestPHPProvider_Identity(t *testing.T) {
	p := NewProvider()
	if p.Language() != "php" {
		t.Errorf("Language() = %q, want php", p.Language())
	}
	if exts := p.Extensions(); len(exts) != 1 || exts[0] != ".php" {
		t.Errorf("Extensions() = %v, want [.php]", exts)
	}
	if p.CacheVersion() == "" {
		t.Error("CacheVersion() is empty")
	}
}

func TestPHPProvider_TopLevelKinds(t *testing.T) {
	m := symbolsByName(t, `<?php
namespace App\Service;

class UserService {}
interface Repository {}
trait LogsActivity {}
enum Status {
    case Active;
}
function helper(): void {}
const MAX = 100;
`)

	want := map[string]struct {
		kind      symbol.SymbolKind
		qualified string
	}{
		"App\\Service": {symbol.KindNamespace, "App\\Service"},
		"UserService":  {symbol.KindClass, "App\\Service\\UserService"},
		"Repository":   {symbol.KindInterface, "App\\Service\\Repository"},
		"LogsActivity": {symbol.KindTrait, "App\\Service\\LogsActivity"},
		"Status":       {symbol.KindEnum, "App\\Service\\Status"},
		"helper":       {symbol.KindFunction, "App\\Service\\helper"},
		"MAX":          {symbol.KindConstant, "App\\Service\\MAX"},
	}
	for name, w := range want {
		got, ok := m[name]
		if !ok {
			t.Errorf("symbol %q not extracted", name)
			continue
		}
		if got.kind != w.kind {
			t.Errorf("symbol %q: kind = %q, want %q", name, got.kind, w.kind)
		}
		if got.qualified != w.qualified {
			t.Errorf("symbol %q: qualified = %q, want %q", name, got.qualified, w.qualified)
		}
		if !got.exported {
			t.Errorf("symbol %q: top-level PHP symbol should be Exported", name)
		}
	}

	// enum case must NOT be extracted in PHP-2.
	if _, ok := m["Active"]; ok {
		t.Error("enum case 'Active' must not be extracted in PHP-2")
	}
}

func TestPHPProvider_GlobalNamespace(t *testing.T) {
	m := symbolsByName(t, `<?php
class Plain {}
function plainFn() {}
const PLAIN = 1;
`)
	for name, wantQ := range map[string]string{"Plain": "Plain", "plainFn": "plainFn", "PLAIN": "PLAIN"} {
		got, ok := m[name]
		if !ok {
			t.Errorf("symbol %q not extracted", name)
			continue
		}
		if got.qualified != wantQ {
			t.Errorf("global symbol %q: qualified = %q, want bare %q", name, got.qualified, wantQ)
		}
	}
}

func TestPHPProvider_BracketedNamespace(t *testing.T) {
	m := symbolsByName(t, `<?php
namespace App\Service {
    class UserService {}
    interface Repository {}
}
namespace App\Other {
    trait LogsActivity {}
}
`)
	for name, wantQ := range map[string]string{
		"UserService":  "App\\Service\\UserService",
		"Repository":   "App\\Service\\Repository",
		"LogsActivity": "App\\Other\\LogsActivity",
	} {
		got, ok := m[name]
		if !ok {
			t.Errorf("symbol %q not extracted", name)
			continue
		}
		if got.qualified != wantQ {
			t.Errorf("symbol %q: qualified = %q, want %q", name, got.qualified, wantQ)
		}
	}
}

func TestPHPProvider_MultipleStatementNamespaces(t *testing.T) {
	m := symbolsByName(t, `<?php
namespace A;
class X {}
namespace B;
class Y {}
`)
	if got := m["X"]; got.qualified != "A\\X" {
		t.Errorf("X qualified = %q, want A\\X", got.qualified)
	}
	if got := m["Y"]; got.qualified != "B\\Y" {
		t.Errorf("Y qualified = %q, want B\\Y", got.qualified)
	}
}

func TestPHPProvider_ByteAndLocation(t *testing.T) {
	src := []byte(`<?php
namespace App;
class UserService {}
`)
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("test.php"), src)
	if err != nil {
		t.Fatal(err)
	}
	n := uint32(len(src))
	for _, d := range ext.Symbols {
		if d.Name == "" || d.Qualified == "" {
			t.Errorf("symbol has empty Name/Qualified: %+v", d)
		}
		if d.Kind == "" || d.Kind == symbol.KindUnknown {
			t.Errorf("symbol %q: invalid kind %q", d.Name, d.Kind)
		}
		if d.Location.File == "" {
			t.Errorf("symbol %q: empty Location.File", d.Name)
		}
		if d.StartByte > d.EndByte || d.EndByte > n {
			t.Errorf("symbol %q: bad byte range [%d,%d] len=%d", d.Name, d.StartByte, d.EndByte, n)
		}
		if d.Location.Range.Start.Line == 0 || d.Location.Range.Start.Column == 0 {
			t.Errorf("symbol %q: location must be 1-based, got %+v", d.Name, d.Location.Range.Start)
		}
	}
}

func TestPHPProvider_Deterministic(t *testing.T) {
	src := []byte(`<?php
namespace App\Service;
class A {}
trait B {}
enum C { case X; }
function f() {}
const K = 1;
`)
	p := NewProvider()
	a, _ := p.Extract(context.Background(), source.FileID("test.php"), src)
	b, _ := p.Extract(context.Background(), source.FileID("test.php"), src)
	if len(a.Symbols) != len(b.Symbols) {
		t.Fatalf("non-deterministic symbol count: %d vs %d", len(a.Symbols), len(b.Symbols))
	}
	for i := range a.Symbols {
		if a.Symbols[i].Qualified != b.Symbols[i].Qualified || a.Symbols[i].Kind != b.Symbols[i].Kind {
			t.Errorf("non-deterministic at %d: %+v vs %+v", i, a.Symbols[i], b.Symbols[i])
		}
	}
}

func TestPHPProvider_Safety(t *testing.T) {
	inputs := map[string]string{
		"empty":      ``,
		"only_tag":   `<?php`,
		"no_tag":     `plain text no php`,
		"broken":     `<?php namespace App; class Broken { public function m( {  function good() {}`,
		"partial_ns": `<?php namespace `,
		"bad_class":  `<?php class {} class Good {}`,
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
			if _, err := p.Extract(context.Background(), source.FileID("x.php"), []byte(src)); err != nil {
				t.Errorf("unexpected error on %q: %v", name, err)
			}
		})
	}
}
