package syntax

import (
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"
)

// PoC: gotreesitter API の確認
// このテストで API を把握し、本格実装に移行する

func TestPoCGoParser(t *testing.T) {
	// Go ソースコードのサンプル
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

	// 言語を取得
	lang := grammars.GoLanguage()
	if lang == nil {
		t.Fatal("Failed to get Go language")
	}

	// Parser を作成
	parser := ts.NewParser(lang)

	// Parse
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Failed to parse Go source: %v", err)
	}
	defer tree.Release()

	// Root node を取得
	root := tree.RootNode()
	t.Logf("Root node type: %s", root.Type(lang))
	t.Logf("Root node child count: %d", root.ChildCount())

	// 子ノードを探索
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		nodeType := child.Type(lang)
		t.Logf("Child %d: type=%s, named=%v", i, nodeType, child.IsNamed())

		// function_declaration を見つけたら詳細を出力
		if nodeType == "function_declaration" {
			// 関数名を取得
			for j := 0; j < child.ChildCount(); j++ {
				grandchild := child.Child(j)
				if grandchild.Type(lang) == "identifier" {
					start := grandchild.StartPoint()
					end := grandchild.EndPoint()
					name := grandchild.Text(source)
					t.Logf("  Function: %s (line %d-%d)", name, start.Row+1, end.Row+1)
				}
			}
		}

		// type_declaration を見つけたら詳細を出力
		if nodeType == "type_declaration" {
			t.Logf("  Type declaration found")
		}

		// method_declaration を見つけたら詳細を出力
		if nodeType == "method_declaration" {
			t.Logf("  Method declaration found")
		}
	}
}

func TestPoCTypeScriptParser(t *testing.T) {
	// TypeScript ソースコードのサンプル
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

  getUser(id: number): User | undefined {
    return this.users.find(u => u.id === id);
  }
}

const arrowFunc = (x: number): number => x * 2;

type Status = "active" | "inactive";
`)

	lang := grammars.TypescriptLanguage()
	if lang == nil {
		t.Fatal("Failed to get TypeScript language")
	}

	parser := ts.NewParser(lang)

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Failed to parse TypeScript source: %v", err)
	}
	defer tree.Release()

	root := tree.RootNode()
	t.Logf("TypeScript Root node type: %s", root.Type(lang))
	t.Logf("TypeScript Root node child count: %d", root.ChildCount())

	// 子ノードを探索
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		t.Logf("Child %d: type=%s, named=%v", i, child.Type(lang), child.IsNamed())
	}
}

func TestPoCPythonParser(t *testing.T) {
	// Python ソースコードのサンプル
	source := []byte(`
def greet(name: str) -> None:
    print(f"Hello, {name}")

class User:
    def __init__(self, id: int, name: str):
        self.id = id
        self.name = name
    
    def save(self) -> bool:
        return True

async def async_fetch(url: str):
    pass

CONSTANT = 42
`)

	lang := grammars.PythonLanguage()
	if lang == nil {
		t.Fatal("Failed to get Python language")
	}

	parser := ts.NewParser(lang)

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Failed to parse Python source: %v", err)
	}
	defer tree.Release()

	root := tree.RootNode()
	t.Logf("Python Root node type: %s", root.Type(lang))
	t.Logf("Python Root node child count: %d", root.ChildCount())

	// 子ノードを探索
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		t.Logf("Child %d: type=%s, named=%v", i, child.Type(lang), child.IsNamed())
	}
}

func TestPoCJavaScriptParser(t *testing.T) {
	// JavaScript ソースコードのサンプル
	source := []byte(`
function greet(name) {
  console.log("Hello, " + name);
}

class User {
  constructor(id, name) {
    this.id = id;
    this.name = name;
  }

  save() {
    return true;
  }
}

const arrowFunc = (x) => x * 2;
`)

	lang := grammars.JavascriptLanguage()
	if lang == nil {
		t.Fatal("Failed to get JavaScript language")
	}

	parser := ts.NewParser(lang)

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Failed to parse JavaScript source: %v", err)
	}
	defer tree.Release()

	root := tree.RootNode()
	t.Logf("JavaScript Root node type: %s", root.Type(lang))
	t.Logf("JavaScript Root node child count: %d", root.ChildCount())

	// 子ノードを探索
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		t.Logf("Child %d: type=%s, named=%v", i, child.Type(lang), child.IsNamed())
	}
}

func TestPoCTSXParser(t *testing.T) {
	// TSX ソースコードのサンプル
	source := []byte(`
import React from 'react';

interface Props {
  name: string;
}

function Greeting({ name }: Props): JSX.Element {
  return <div>Hello, {name}!</div>;
}

const ArrowComponent: React.FC<Props> = ({ name }) => {
  return <span>{name}</span>;
};

export default Greeting;
`)

	lang := grammars.TsxLanguage()
	if lang == nil {
		t.Fatal("Failed to get TSX language")
	}

	parser := ts.NewParser(lang)

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Failed to parse TSX source: %v", err)
	}
	defer tree.Release()

	root := tree.RootNode()
	t.Logf("TSX Root node type: %s", root.Type(lang))
	t.Logf("TSX Root node child count: %d", root.ChildCount())

	// 子ノードを探索
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		t.Logf("Child %d: type=%s, named=%v", i, child.Type(lang), child.IsNamed())
	}
}
