package syntax

import (
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		filename string
		want     SupportedLanguage
	}{
		{"main.go", LangGo},
		{"app.ts", LangTypeScript},
		{"component.tsx", LangTSX},
		{"script.js", LangJavaScript},
		{"module.mjs", LangJavaScript},
		{"app.py", LangPython},
		{"unknown.xyz", LangUnknown},
		{"README.md", LangUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := DetectLanguage(tt.filename)
			if got != tt.want {
				t.Errorf("DetectLanguage(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestIsSupportedFile(t *testing.T) {
	tests := []struct {
		filename string
		want     bool
	}{
		{"main.go", true},
		{"app.ts", true},
		{"README.md", false},
		{"image.png", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := IsSupportedFile(tt.filename)
			if got != tt.want {
				t.Errorf("IsSupportedFile(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestExtractGoSymbols(t *testing.T) {
	source := []byte(`package main

import "fmt"

// Greet prints a greeting message
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

	result, err := ExtractSymbolsFromFile("test.go", source)
	if err != nil {
		t.Fatalf("ExtractSymbolsFromFile failed: %v", err)
	}

	if result.Language != "go" {
		t.Errorf("Language = %q, want %q", result.Language, "go")
	}

	// Check expected symbols
	expectedSymbols := map[string]SymbolKind{
		"Greet":       SymbolFunction,
		"User":        SymbolStruct,
		"Save":        SymbolMethod,
		"DefaultName": SymbolConstant,
		"globalVar":   SymbolVariable,
	}

	symbolMap := make(map[string]Symbol)
	for _, s := range result.Symbols {
		symbolMap[s.Name] = s
	}

	for name, expectedKind := range expectedSymbols {
		sym, ok := symbolMap[name]
		if !ok {
			t.Errorf("Symbol %q not found", name)
			continue
		}
		if sym.Kind != expectedKind {
			t.Errorf("Symbol %q: Kind = %q, want %q", name, sym.Kind, expectedKind)
		}
	}

	// Check exported status
	if sym, ok := symbolMap["Greet"]; ok && !sym.Exported {
		t.Error("Greet should be exported")
	}
	if sym, ok := symbolMap["globalVar"]; ok && sym.Exported {
		t.Error("globalVar should not be exported")
	}

	// Check method receiver
	if sym, ok := symbolMap["Save"]; ok && sym.Receiver != "User" {
		t.Errorf("Save receiver = %q, want %q", sym.Receiver, "User")
	}
}

func TestExtractTypeScriptSymbols(t *testing.T) {
	source := []byte(`
interface User {
  id: number;
  name: string;
}

function greet(name: string): void {
  console.log("Hello, " + name);
}

class UserService {
  private users: User[] = [];

  addUser(user: User): void {
    this.users.push(user);
  }
}

const MAX_USERS = 100;

type Status = "active" | "inactive";
`)

	result, err := ExtractSymbolsFromFile("test.ts", source)
	if err != nil {
		t.Fatalf("ExtractSymbolsFromFile failed: %v", err)
	}

	if result.Language != "typescript" {
		t.Errorf("Language = %q, want %q", result.Language, "typescript")
	}

	expectedSymbols := map[string]SymbolKind{
		"User":        SymbolInterface,
		"greet":       SymbolFunction,
		"UserService": SymbolClass,
		"MAX_USERS":   SymbolConstant,
		"Status":      SymbolTypeAlias,
	}

	symbolMap := make(map[string]Symbol)
	for _, s := range result.Symbols {
		symbolMap[s.Name] = s
	}

	for name, expectedKind := range expectedSymbols {
		sym, ok := symbolMap[name]
		if !ok {
			t.Errorf("Symbol %q not found", name)
			continue
		}
		if sym.Kind != expectedKind {
			t.Errorf("Symbol %q: Kind = %q, want %q", name, sym.Kind, expectedKind)
		}
	}
}

func TestExtractPythonSymbols(t *testing.T) {
	source := []byte(`
def greet(name: str) -> None:
    print(f"Hello, {name}")

class User:
    def __init__(self, id: int, name: str):
        self.id = id
        self.name = name

async def async_fetch(url: str):
    pass

def _private_func():
    pass
`)

	result, err := ExtractSymbolsFromFile("test.py", source)
	if err != nil {
		t.Fatalf("ExtractSymbolsFromFile failed: %v", err)
	}

	if result.Language != "python" {
		t.Errorf("Language = %q, want %q", result.Language, "python")
	}

	expectedSymbols := map[string]SymbolKind{
		"greet":         SymbolFunction,
		"User":          SymbolClass,
		"async_fetch":   SymbolFunction,
		"_private_func": SymbolFunction,
	}

	symbolMap := make(map[string]Symbol)
	for _, s := range result.Symbols {
		symbolMap[s.Name] = s
	}

	for name, expectedKind := range expectedSymbols {
		sym, ok := symbolMap[name]
		if !ok {
			t.Errorf("Symbol %q not found", name)
			continue
		}
		if sym.Kind != expectedKind {
			t.Errorf("Symbol %q: Kind = %q, want %q", name, sym.Kind, expectedKind)
		}
	}

	// Check private detection
	if sym, ok := symbolMap["_private_func"]; ok && sym.Exported {
		t.Error("_private_func should not be exported")
	}
	if sym, ok := symbolMap["greet"]; ok && !sym.Exported {
		t.Error("greet should be exported")
	}
}

func TestLineNumbers1Based(t *testing.T) {
	// Per PROMPT.md Section 12: line numbers must be 1-based
	source := []byte(`package main

func First() {}
func Second() {}
`)

	result, err := ExtractSymbolsFromFile("test.go", source)
	if err != nil {
		t.Fatalf("ExtractSymbolsFromFile failed: %v", err)
	}

	for _, sym := range result.Symbols {
		if sym.StartLine == 0 {
			t.Errorf("Symbol %q has StartLine = 0, should be 1-based", sym.Name)
		}
		if sym.EndLine == 0 {
			t.Errorf("Symbol %q has EndLine = 0, should be 1-based", sym.Name)
		}
		if sym.StartCol == 0 {
			t.Errorf("Symbol %q has StartCol = 0, should be 1-based", sym.Name)
		}
	}
}

func TestUnsupportedLanguage(t *testing.T) {
	source := []byte("Some content")

	_, err := ExtractSymbolsFromFile("unknown.xyz", source)
	if err != ErrUnsupportedLanguage {
		t.Errorf("Expected ErrUnsupportedLanguage, got %v", err)
	}
}
