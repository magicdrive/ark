package java

import (
	"strings"
	"unicode"

	ts "github.com/odvcencio/gotreesitter"
)

// argumentTypes states what the file proves of each argument's type, in
// the notation of language.ReferenceDraft.ArgumentTypes ("" when nothing is
// proven of any).
func (x *extractor) argumentTypes(args *ts.Node, f *frame) string {
	parts := []string{}
	known := false
	for _, a := range x.named(args) {
		t := x.argType(a, f, 0)
		known = known || t != ""
		parts = append(parts, t)
	}
	if !known {
		return ""
	}
	return strings.Join(parts, ",")
}

// argType is what the file proves of expression e's type.
func (x *extractor) argType(e *ts.Node, f *frame, depth int) string {
	if e == nil || depth > 50 {
		return ""
	}
	switch x.typ(e) {
	case "decimal_integer_literal", "hex_integer_literal", "octal_integer_literal", "binary_integer_literal":
		if t := x.text(e); strings.HasSuffix(t, "L") || strings.HasSuffix(t, "l") {
			return "long"
		}
		return "int"
	case "decimal_floating_point_literal", "hex_floating_point_literal":
		if t := x.text(e); strings.HasSuffix(t, "f") || strings.HasSuffix(t, "F") {
			return "float"
		}
		return "double"
	case "character_literal":
		return "char"
	case "true", "false":
		return "boolean"
	case "string_literal":
		return "S"
	case "null_literal":
		return "null"
	case "class_literal", "this", "object_creation_expression":
		return "R"
	case "lambda_expression", "method_reference":
		return "F"
	case "parenthesized_expression":
		if in := x.named(e); len(in) == 1 {
			return x.argType(in[0], f, depth+1)
		}
	case "cast_expression":
		if ty := x.field(e, "type"); ty != nil && len(x.named(e)) == 2 {
			return x.typeClassAt(x.typeText(ty), f, ty.StartByte())
		}
	case "array_creation_expression":
		ty := x.field(e, "type")
		dims := 0
		for _, ch := range x.named(e) {
			switch x.typ(ch) {
			case "dimensions_expr":
				dims++
			case "dimensions":
				dims += strings.Count(x.text(ch), "[")
			}
		}
		if ty == nil || dims == 0 {
			return ""
		}
		return x.typeClassAt(x.typeText(ty)+strings.Repeat("[]", dims), f, ty.StartByte())
	case "instanceof_expression":
		return "boolean"
	case "unary_expression":
		op := x.operator(e)
		operand := x.named(e)
		if len(operand) != 1 {
			return ""
		}
		t := x.argType(operand[0], f, depth+1)
		switch op {
		case "!":
			return "boolean"
		case "-", "+", "~":
			if primitiveNumeric(t) {
				return promote(t, "int")
			}
		}
	case "binary_expression":
		l, r := x.argType(x.field(e, "left"), f, depth+1), x.argType(x.field(e, "right"), f, depth+1)
		switch x.operator(e) {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			return "boolean"
		case "+":
			if l == "S" || r == "S" {
				return "S"
			}
			fallthrough
		case "-", "*", "/", "%":
			if primitiveNumeric(l) && primitiveNumeric(r) {
				return promote(l, r)
			}
		}
	case "identifier":
		vs := x.valueSteps(x.text(e), f, e.StartByte())
		switch {
		case vs.local != nil && len(vs.steps) == 1:
			return x.typeClassAt(vs.local.typeText, vs.local.scope, vs.local.pos)
		case vs.ownField && len(vs.steps) == 1 && vs.ownFieldType != nil:
			return x.typeClassAt(x.typeText(vs.ownFieldType), bodyFrame(vs.ownFieldIn), vs.ownFieldType.StartByte())
		}
	}
	return ""
}

// boxNames are the simple names of the types unboxing converts (JLS
// 5.1.8): a type of another name — no type variable — never unboxes.
var boxNames = map[string]bool{"Boolean": true, "Byte": true, "Short": true, "Character": true, "Integer": true, "Long": true, "Float": true, "Double": true}

// typeClassAt is a declared type's class in the ArgumentTypes notation, its
// text read in chain f at pos (nil f: no type variable is known there).
func (x *extractor) typeClassAt(text string, f *frame, pos uint32) string {
	if text == "" || text == "var" {
		return ""
	}
	if i := strings.IndexByte(text, '<'); i >= 0 {
		// type arguments: the erasure's class
		depth := 0
		var b strings.Builder
		for _, r := range text {
			switch {
			case r == '<':
				depth++
			case r == '>':
				depth--
			case depth == 0:
				b.WriteRune(r)
			}
		}
		text = b.String()
	}
	dims := ""
	for strings.HasSuffix(text, "[]") {
		dims += "[]"
		text = strings.TrimSuffix(text, "[]")
	}
	switch text {
	case "byte", "short", "char", "int", "long", "float", "double", "boolean":
		return text + dims
	case "void", "":
		return ""
	}
	for _, r := range text {
		if r != '.' && r != '_' && r != '$' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return "" // not a type name as the notation reads it
		}
	}
	simple := lastSegment(text)
	if dims != "" || boxNames[simple] || f == nil {
		return "R" + dims
	}
	if !strings.Contains(text, ".") {
		if l := x.typeSteps(simple, f, pos); l.typeVar {
			return "R" // a type variable may unbox (a box bound)
		}
	}
	return "N"
}

func primitiveNumeric(t string) bool {
	switch t {
	case "byte", "short", "char", "int", "long", "float", "double":
		return true
	}
	return false
}

// promote is binary numeric promotion (JLS 5.6).
func promote(a, b string) string {
	for _, t := range []string{"double", "float", "long"} {
		if a == t || b == t {
			return t
		}
	}
	return "int"
}
