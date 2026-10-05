package completion_test

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

// astCaseStrings returns the string literals of every `case "x":` clause of the
// switch statements inside the named function of cmd/ark/mod.go. This is the
// dispatcher's real routing table, so completion cannot drift from it.
func astCaseStrings(t *testing.T, funcName string) []string {
	t.Helper()
	path := filepath.Join("..", "..", "cmd", "ark", "mod.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var out []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != funcName {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					s, _ := strconv.Unquote(lit.Value)
					out = append(out, s)
				}
			}
			return true
		})
	}
	if len(out) == 0 {
		t.Fatalf("no `case \"...\":` clauses found in %s", funcName)
	}
	return out
}

func cliSubcommands(t *testing.T) []string      { return astCaseStrings(t, "Execute") }
func cliSkillSubcommands(t *testing.T) []string { return astCaseStrings(t, "runSkillCommand") }

// command describes one CLI command path and its real FlagSet.
type command struct {
	name string   // human name, also used in subtests
	path []string // words after `ark` that select it ([] = default command)
	fs   *flag.FlagSet
}

func mustFS(t *testing.T, fs *flag.FlagSet, err error) *flag.FlagSet {
	t.Helper()
	if err != nil || fs == nil {
		t.Fatalf("parser returned fs=%v err=%v", fs, err)
	}
	return fs
}

// cliCommands builds the FlagSet of every command from the real parsers.
func cliCommands(t *testing.T) []command {
	t.Helper()
	_, general, err := commandline.GeneralOptParse(nil)
	gfs := mustFS(t, general.FlagSet, err)
	_, serve, err := commandline.ServerOptParse("test", nil)
	sfs := mustFS(t, serve.GeneralOption.FlagSet, err)
	_, mi, err := commandline.MCPInitOptParse(nil)
	mifs := mustFS(t, mi.FlagSet, err)
	_, st, err := commandline.SetupOptParse(nil)
	stfs := mustFS(t, st.FlagSet, err)
	_, sy, err := commandline.SyntaxOptParse(nil)
	syfs := mustFS(t, sy.FlagSet, err)
	_, sym, err := commandline.SymbolOptParse(nil)
	symfs := mustFS(t, sym.FlagSet, err)
	_, sk, err := commandline.SkillAutoOptParse(nil)
	skfs := mustFS(t, sk.FlagSet, err)
	_, ski, err := commandline.SkillInitOptParse(nil)
	skifs := mustFS(t, ski.FlagSet, err)
	_, ske, err := commandline.SkillAddExplorerOptParse(nil)
	skefs := mustFS(t, ske.FlagSet, err)
	_, sku, err := commandline.SkillUpdateOptParse(nil)
	skufs := mustFS(t, sku.FlagSet, err)
	_, skn, err := commandline.SkillInspectOptParse(nil)
	sknfs := mustFS(t, skn.FlagSet, err)

	return []command{
		{"general", nil, gfs},
		{"mcp-server", []string{"mcp-server"}, sfs},
		{"mcp-init", []string{"mcp-init"}, mifs},
		{"setup", []string{"setup"}, stfs},
		{"syntax", []string{"syntax"}, syfs},
		{"symbol", []string{"symbol"}, symfs},
		{"skill", []string{"skill"}, skfs},
		{"skill init", []string{"skill", "init"}, skifs},
		{"skill add-explorer", []string{"skill", "add-explorer"}, skefs},
		{"skill update", []string{"skill", "update"}, skufs},
		{"skill inspect", []string{"skill", "inspect"}, sknfs},
	}
}

// flagNames returns the sorted long ("--x") and short ("-x") names of a FlagSet.
func flagNames(fs *flag.FlagSet) (long, short []string) {
	fs.VisitAll(func(f *flag.Flag) {
		if len(f.Name) == 1 {
			short = append(short, "-"+f.Name)
		} else {
			long = append(long, "--"+f.Name)
		}
	})
	sort.Strings(long)
	sort.Strings(short)
	return long, short
}
