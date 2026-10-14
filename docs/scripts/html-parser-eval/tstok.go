//go:build ignore

// tstok prints gotreesitter's HTML start tags and attributes in htmltok's
// JSONL shape (P "ts"), and per file the root's end byte and whether the
// tree reports an error.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type rec struct {
	P, F, K, Name    string
	S, E, VS, VE     int
	Consumed         int  `json:",omitempty"`
	HasError, Missed bool `json:",omitempty"`
}

func out(r rec) { b, _ := json.Marshal(r); fmt.Println(string(b)) }

func main() {
	lang := grammars.HtmlLanguage()
	for _, f := range os.Args[1:] {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		tree, err := ts.NewParser(lang).Parse(src)
		if err != nil || tree == nil || tree.RootNode() == nil {
			out(rec{P: "ts", F: f, K: "eof", Consumed: -1, HasError: true})
			continue
		}
		root := tree.RootNode()
		typ := func(n *ts.Node) string { return n.Type(lang) }
		var walk func(n *ts.Node)
		walk = func(n *ts.Node) {
			switch typ(n) {
			case "start_tag", "self_closing_tag":
				name := ""
				for i := range n.NamedChildCount() {
					c := n.NamedChild(i)
					switch typ(c) {
					case "tag_name":
						name = string(src[c.StartByte():c.EndByte()])
					case "attribute":
						r := rec{P: "ts", F: f, K: "attr", VS: -1, VE: -1}
						for j := range c.NamedChildCount() {
							a := c.NamedChild(j)
							switch typ(a) {
							case "attribute_name":
								r.Name = string(src[a.StartByte():a.EndByte()])
								r.S, r.E = int(a.StartByte()), int(a.EndByte())
							case "attribute_value":
								r.VS, r.VE = int(a.StartByte()), int(a.EndByte())
							case "quoted_attribute_value":
								r.VS, r.VE = int(a.StartByte())+1, int(a.EndByte())-1
								for k := range a.NamedChildCount() {
									v := a.NamedChild(k)
									r.VS, r.VE = int(v.StartByte()), int(v.EndByte())
								}
							}
						}
						defer out(r)
					}
				}
				out(rec{P: "ts", F: f, K: "tag", Name: name, S: int(n.StartByte()), E: int(n.EndByte())})
			}
			for i := range n.ChildCount() {
				walk(n.Child(i))
			}
		}
		walk(root)
		out(rec{P: "ts", F: f, K: "eof", Consumed: int(root.EndByte()), HasError: root.HasError()})
	}
}
