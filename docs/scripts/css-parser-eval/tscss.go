//go:build ignore

// tscss parses each file with gotreesitter's CSS grammar and prints, per
// file: its rule_set / at-rule blocks and declarations, whether the root
// covers the file, and whether the tree reports an error.
package main

import (
	"fmt"
	"os"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func main() {
	lang := grammars.CssLanguage()
	for _, f := range os.Args[1:] {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		tree, err := ts.NewParser(lang).Parse(src)
		if err != nil || tree == nil || tree.RootNode() == nil {
			fmt.Printf("%s\t-1\t-1\t0\ttrue\n", f)
			continue
		}
		rules, decls := 0, 0
		var walk func(n *ts.Node)
		walk = func(n *ts.Node) {
			switch n.Type(lang) {
			case "block":
				rules++
			case "declaration":
				decls++
			}
			for i := range n.ChildCount() {
				walk(n.Child(i))
			}
		}
		root := tree.RootNode()
		walk(root)
		fmt.Printf("%s\t%d\t%d\t%d\t%v\n", f, rules, decls, root.EndByte(), root.HasError())
	}
}
