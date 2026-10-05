package golang

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"
)

// Receiver type evidence (language.ReferenceDraft.ReceiverType).
//
// Only local, structural facts are used — never inference:
//   - the method receiver (`func (s *T) M()` → s: T);
//   - explicitly typed parameters (`func f(r *Repo)` → r: Repo);
//   - a single-name declaration at the top level of the function body whose
//     type is written down: `x := &T{...}`, `x := T{...}`, `var x T`,
//     `var x = &T{...}` (Go variables cannot change type, so this is sound
//     for every use after the declaration);
//   - one field step on such a variable when the struct is declared in the
//     same file with an explicitly typed field (`s.repo` → repo's type).
//
// Only unqualified type names (type_identifier, optionally behind a pointer)
// are recorded; package-qualified and generic types are left unproven. A name
// declared more than once anywhere in the function (shadowing, closures,
// range variables, ...) is poisoned and yields no evidence.

// goVarType is one proven local variable type.
type goVarType struct {
	typ  string
	from uint32 // evidence applies to uses at or after this byte offset
}

// goTypeEnv maps a local identifier to its proven declared type.
type goTypeEnv map[string]goVarType

// goStructFields maps struct name → field name → declared field type, for
// structs declared in the current file.
type goStructFields map[string]map[string]string

// goNamedType returns the unqualified type name of a type node
// (type_identifier or *type_identifier), or "".
func goNamedType(n *ts.Node, lang *ts.Language, src []byte) string {
	switch n.Type(lang) {
	case "type_identifier":
		return n.Text(src)
	case "pointer_type":
		for i := 0; i < n.ChildCount(); i++ {
			if c := n.Child(i); c.Type(lang) == "type_identifier" {
				return c.Text(src)
			}
		}
	}
	return ""
}

// goConstructedType returns T for `T{...}` or `&T{...}`, or "".
func goConstructedType(n *ts.Node, lang *ts.Language, src []byte) string {
	switch n.Type(lang) {
	case "composite_literal":
		if n.ChildCount() > 0 && n.Child(0).Type(lang) == "type_identifier" {
			return n.Child(0).Text(src)
		}
	case "unary_expression":
		if n.ChildCount() == 2 && n.Child(0).Type(lang) == "&" {
			return goConstructedType(n.Child(1), lang, src)
		}
	}
	return ""
}

func namedChildren(n *ts.Node, lang *ts.Language, typ string) []*ts.Node {
	var out []*ts.Node
	for i := 0; i < n.ChildCount(); i++ {
		if c := n.Child(i); c.Type(lang) == typ {
			out = append(out, c)
		}
	}
	return out
}

// collectGoStructFields indexes explicitly typed fields of structs declared at
// the top level of the file.
func collectGoStructFields(root *ts.Node, lang *ts.Language, src []byte) goStructFields {
	out := make(goStructFields)
	for _, decl := range namedChildren(root, lang, "type_declaration") {
		for _, spec := range namedChildren(decl, lang, "type_spec") {
			var name string
			var st *ts.Node
			for i := 0; i < spec.ChildCount(); i++ {
				switch c := spec.Child(i); c.Type(lang) {
				case "type_identifier":
					if name == "" {
						name = c.Text(src)
					}
				case "struct_type":
					st = c
				}
			}
			if name == "" || st == nil {
				continue
			}
			fields := make(map[string]string)
			for _, list := range namedChildren(st, lang, "field_declaration_list") {
				for _, fd := range namedChildren(list, lang, "field_declaration") {
					var names []string
					typ := ""
					for i := 0; i < fd.ChildCount(); i++ {
						c := fd.Child(i)
						switch c.Type(lang) {
						case "field_identifier":
							names = append(names, c.Text(src))
						default:
							if t := goNamedType(c, lang, src); t != "" && len(names) > 0 {
								typ = t
							}
						}
					}
					for _, n := range names {
						if typ != "" {
							fields[n] = typ
						}
					}
				}
			}
			out[name] = fields
		}
	}
	return out
}

// goDeclCounts counts identifier declarations of every form anywhere under n.
func goDeclCounts(n *ts.Node, lang *ts.Language, src []byte, counts map[string]int) {
	switch n.Type(lang) {
	case "parameter_declaration", "variadic_parameter_declaration", "var_spec", "const_spec":
		for _, id := range namedChildren(n, lang, "identifier") {
			counts[id.Text(src)]++
		}
	case "short_var_declaration", "range_clause", "receive_statement", "type_switch_statement", "labeled_statement":
		if n.ChildCount() > 0 {
			left := n.Child(0)
			if left.Type(lang) == "expression_list" {
				for _, id := range namedChildren(left, lang, "identifier") {
					counts[id.Text(src)]++
				}
			} else if left.Type(lang) == "identifier" {
				counts[left.Text(src)]++
			}
		}
		if n.Type(lang) == "type_switch_statement" {
			// `switch v := x.(type)` binds v in every clause.
			for i := 0; i < n.ChildCount(); i++ {
				if c := n.Child(i); c.Type(lang) == "type_switch_header" || c.Type(lang) == "expression_list" {
					for _, id := range namedChildren(c, lang, "identifier") {
						counts[id.Text(src)]++
					}
				}
			}
		}
	}
	for i := 0; i < n.ChildCount(); i++ {
		goDeclCounts(n.Child(i), lang, src, counts)
	}
}

// goFunctionTypeEnv builds the proven receiver-type environment for a
// function_declaration or method_declaration.
func goFunctionTypeEnv(fn *ts.Node, lang *ts.Language, src []byte) goTypeEnv {
	counts := make(map[string]int)
	goDeclCounts(fn, lang, src, counts)

	env := make(goTypeEnv)
	set := func(name, typ string, from uint32) {
		if name == "" || name == "_" || typ == "" || counts[name] != 1 {
			return
		}
		env[name] = goVarType{typ: typ, from: from}
	}

	// Receiver and parameters (whole-function scope).
	for _, pl := range namedChildren(fn, lang, "parameter_list") {
		for _, pd := range namedChildren(pl, lang, "parameter_declaration") {
			typ := ""
			for i := 0; i < pd.ChildCount(); i++ {
				if t := goNamedType(pd.Child(i), lang, src); t != "" {
					typ = t
				}
			}
			for _, id := range namedChildren(pd, lang, "identifier") {
				set(id.Text(src), typ, 0)
			}
		}
	}

	// Top-level declarations of the body (evidence applies after the decl).
	for _, block := range namedChildren(fn, lang, "block") {
		stmts := namedChildren(block, lang, "statement_list")
		if len(stmts) == 0 {
			stmts = []*ts.Node{block}
		}
		for _, sl := range stmts {
			for i := 0; i < sl.ChildCount(); i++ {
				st := sl.Child(i)
				switch st.Type(lang) {
				case "short_var_declaration":
					lists := namedChildren(st, lang, "expression_list")
					if len(lists) != 2 || lists[0].ChildCount() != 1 || lists[1].ChildCount() != 1 {
						continue
					}
					id := lists[0].Child(0)
					if id.Type(lang) != "identifier" {
						continue
					}
					set(id.Text(src), goConstructedType(lists[1].Child(0), lang, src), st.EndByte())
				case "var_declaration":
					for _, vs := range namedChildren(st, lang, "var_spec") {
						ids := namedChildren(vs, lang, "identifier")
						if len(ids) != 1 {
							continue
						}
						typ := ""
						for j := 0; j < vs.ChildCount(); j++ {
							c := vs.Child(j)
							if t := goNamedType(c, lang, src); t != "" {
								typ = t
							}
							if c.Type(lang) == "expression_list" && c.ChildCount() == 1 && typ == "" {
								typ = goConstructedType(c.Child(0), lang, src)
							}
						}
						set(ids[0].Text(src), typ, vs.EndByte())
					}
				}
			}
		}
	}
	return env
}

// receiverType returns the proven declared type of a receiver expression used
// at byte offset at, or "".
func (env goTypeEnv) receiverType(expr string, at uint32, fields goStructFields) string {
	if env == nil || expr == "" {
		return ""
	}
	root, field, hasField := strings.Cut(expr, ".")
	if !isGoIdent(root) {
		return ""
	}
	v, ok := env[root]
	if !ok || at < v.from {
		return ""
	}
	if !hasField {
		return v.typ
	}
	if !isGoIdent(field) {
		return "" // deeper chains are not tracked
	}
	return fields[v.typ][field]
}

func isGoIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		if r > 127 {
			continue
		}
		return false
	}
	return true
}
