package golang

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

var goSource = []byte(`package main

import "fmt"

func Greet(name string) {
	fmt.Println("Hello,", name)
}

type User struct {
	ID   int64
	Name string
}

func (u *User) Save() error {
	return nil
}

const DefaultName = "guest"

var globalVar = 42
`)

func TestGoProvider_Extract(t *testing.T) {
	p := NewProvider()
	if p.Language() != "go" {
		t.Fatalf("Language() = %q, want %q", p.Language(), "go")
	}

	ext, err := p.Extract(context.Background(), source.FileID("test.go"), goSource)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(ext.Diagnostics) > 0 {
		t.Fatalf("unexpected diagnostics: %v", ext.Diagnostics)
	}

	want := map[string]symbol.SymbolKind{
		"Greet":       symbol.KindFunction,
		"User":        symbol.KindStruct,
		"Save":        symbol.KindMethod,
		"DefaultName": symbol.KindConstant,
		"globalVar":   symbol.KindVariable,
	}

	got := make(map[string]symbol.SymbolKind)
	for _, d := range ext.Symbols {
		got[d.Name] = d.Kind
	}

	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("symbol %q: kind = %q, want %q", name, got[name], kind)
		}
	}
}

func TestGoProvider_ExportedFlag(t *testing.T) {
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("test.go"), goSource)
	if err != nil {
		t.Fatal(err)
	}
	m := make(map[string]bool)
	for _, d := range ext.Symbols {
		m[d.Name] = d.Exported
	}
	if !m["Greet"] {
		t.Error("Greet should be exported")
	}
	if m["globalVar"] {
		t.Error("globalVar should not be exported")
	}
}

func TestGoProvider_MethodReceiver(t *testing.T) {
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("test.go"), goSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ext.Symbols {
		if d.Name == "Save" {
			if d.Receiver != "User" {
				t.Errorf("Save receiver = %q, want %q", d.Receiver, "User")
			}
			if d.Qualified != "User.Save" {
				t.Errorf("Save qualified = %q, want %q", d.Qualified, "User.Save")
			}
			return
		}
	}
	t.Error("symbol Save not found")
}

func TestGoProvider_ByteOffsets(t *testing.T) {
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("test.go"), goSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ext.Symbols {
		if d.StartByte >= d.EndByte {
			t.Errorf("symbol %q: StartByte(%d) >= EndByte(%d)", d.Name, d.StartByte, d.EndByte)
		}
	}
}

func TestGoProvider_BrokenSyntax(t *testing.T) {
	src := []byte(`package main
func Broken( {
func Good() {}
`)
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("broken.go"), src)
	// Broken syntax should not return an error from Extract; Tree-sitter does partial parsing.
	if err != nil {
		t.Fatalf("unexpected error on broken syntax: %v", err)
	}
	_ = ext // partial results are acceptable
}

func TestGoProvider_Deterministic(t *testing.T) {
	p := NewProvider()
	ext1, _ := p.Extract(context.Background(), source.FileID("test.go"), goSource)
	ext2, _ := p.Extract(context.Background(), source.FileID("test.go"), goSource)
	if len(ext1.Symbols) != len(ext2.Symbols) {
		t.Fatalf("non-deterministic symbol count: %d vs %d", len(ext1.Symbols), len(ext2.Symbols))
	}
	for i := range ext1.Symbols {
		if ext1.Symbols[i].Name != ext2.Symbols[i].Name {
			t.Errorf("non-deterministic order at index %d: %q vs %q",
				i, ext1.Symbols[i].Name, ext2.Symbols[i].Name)
		}
	}
}
