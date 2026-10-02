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

var goRefSource = []byte(`package service

import (
	"fmt"
	repo "github.com/example/repo"
)

type UserService struct{}

func (s *UserService) Create(name string) error {
	u := User{Name: name}
	if err := repo.Save(u); err != nil {
		fmt.Println(err)
		return err
	}
	NewLogger().Log("created")
	return nil
}
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

func TestGoProvider_References_Calls(t *testing.T) {
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("service.go"), goRefSource)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	callNames := map[string]bool{}
	for _, r := range ext.References {
		if r.Kind == "call" {
			callNames[r.Name] = true
		}
	}
	for _, want := range []string{"Save", "Println", "Log"} {
		if !callNames[want] {
			t.Errorf("expected call reference %q, got calls: %v", want, callNames)
		}
	}
}

func TestGoProvider_References_Receiver(t *testing.T) {
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("service.go"), goRefSource)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	for _, r := range ext.References {
		if r.Name == "Save" && r.Kind == "call" {
			if r.ReceiverExpr != "repo" {
				t.Errorf("Save ReceiverExpr = %q, want %q", r.ReceiverExpr, "repo")
			}
			return
		}
	}
	t.Error("reference to Save not found")
}

func TestGoProvider_References_Container(t *testing.T) {
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("service.go"), goRefSource)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	for _, r := range ext.References {
		if r.Name == "Save" && r.Kind == "call" {
			if r.Container != "UserService.Create" {
				t.Errorf("Save container = %q, want %q", r.Container, "UserService.Create")
			}
			return
		}
	}
	t.Error("reference to Save not found")
}

func TestGoProvider_Imports(t *testing.T) {
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("service.go"), goRefSource)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	importPaths := map[string]string{} // path → alias
	for _, imp := range ext.Imports {
		importPaths[imp.Path] = imp.Alias
	}

	if _, ok := importPaths["fmt"]; !ok {
		t.Errorf("expected import fmt, got %v", importPaths)
	}
	if alias, ok := importPaths["github.com/example/repo"]; !ok {
		t.Errorf("expected import github.com/example/repo, got %v", importPaths)
	} else if alias != "repo" {
		t.Errorf("import alias = %q, want %q", alias, "repo")
	}
}

func TestGoProvider_References_Deterministic(t *testing.T) {
	p := NewProvider()
	ext1, _ := p.Extract(context.Background(), source.FileID("service.go"), goRefSource)
	ext2, _ := p.Extract(context.Background(), source.FileID("service.go"), goRefSource)

	if len(ext1.References) != len(ext2.References) {
		t.Fatalf("non-deterministic reference count: %d vs %d",
			len(ext1.References), len(ext2.References))
	}
	for i := range ext1.References {
		r1, r2 := ext1.References[i], ext2.References[i]
		if r1.Name != r2.Name || r1.Kind != r2.Kind {
			t.Errorf("non-deterministic reference at index %d: %+v vs %+v", i, r1, r2)
		}
	}
}

func TestGoProvider_Construction(t *testing.T) {
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("service.go"), goRefSource)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	for _, r := range ext.References {
		if r.Name == "User" && r.Kind == "construction" {
			return
		}
	}
	t.Errorf("expected construction reference for User; refs: %+v", ext.References)
}
