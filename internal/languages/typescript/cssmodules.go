package typescript

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// CSS Modules bindings (JavaScript, TypeScript and TSX alike).
//
// A bundler (css-loader, Vite, Next.js) turns a relative import of a
// `.module.css` file into an object whose properties are the file's local
// class names. `import s from './a.module.css'` and `import * as s from …`
// bind s to it, and s.x is the class x; `import { x as y } from …` binds y
// to the class x (named exports), and a use of y as a value is that class.
// Each use of a class names `<that file> .x`, the qualified identity of the CSS
// Modules class in the CSS provider: path evidence
// (language.ReferenceDraft.FileIdentity), which is what lets a reference of
// one language reach a declaration of another. It is capped at Strong: a
// bundler setting (exportLocalsConvention, namedExport) may export a class
// under another name, or not at all.

// cssModuleFile is the repository path a relative specifier of a CSS
// Modules file denotes ("" for any other specifier: bare, aliased, outside
// the root, or not a .module.css file).
func (e *extractor) cssModuleFile(spec string) string {
	if !strings.HasSuffix(strings.ToLower(spec), ".module.css") {
		return ""
	}
	joined, ok := relativeTarget(e.file, spec)
	if !ok {
		return ""
	}
	return joined
}

// cssModuleImports records, before any reference is walked (imports are
// hoisted), the local names bound to a CSS Modules file by a default or
// namespace import, and the local names a named import binds to a class. An
// import with attributes (`with { type: 'css' }`) is a CSS module script —
// a stylesheet object, no map of classes — and binds nothing here.
func (e *extractor) cssModuleImports(root *ts.Node) {
	for i := range root.ChildCount() {
		n := root.Child(i)
		if e.typ(n) != "import_statement" || e.hasToken(n, "type") || e.hasChild(n, "import_attribute") {
			continue
		}
		file := e.cssModuleFile(e.plainString(e.field(n, "source")))
		if file == "" {
			continue
		}
		for j := range n.ChildCount() {
			clause := n.Child(j)
			if e.typ(clause) != "import_clause" {
				continue
			}
			for k := range clause.ChildCount() {
				c := clause.Child(k)
				switch e.typ(c) {
				case "identifier":
					e.bindCSSModule(e.text(c), file, false)
				case "namespace_import":
					for m := range c.ChildCount() {
						if id := c.Child(m); e.typ(id) == "identifier" {
							e.bindCSSModule(e.text(id), file, true)
						}
					}
				case "named_imports":
					for m := range c.ChildCount() {
						spc := c.Child(m)
						if e.typ(spc) != "import_specifier" || e.hasToken(spc, "type") {
							continue
						}
						name := e.field(spc, "name")
						cls := e.text(name)
						if e.typ(name) == "string" {
							cls = e.plainString(name)
						}
						local := cls
						if a := e.field(spc, "alias"); a != nil {
							local = e.text(a)
						}
						if cls != "" && cls != "default" && local != "" {
							if e.cssClasses == nil {
								e.cssClasses = map[string][2]string{}
							}
							e.cssClasses[local] = [2]string{file, cls}
						}
					}
				}
			}
		}
	}
}

// cssModule is what a default or namespace import binds: the CSS Modules
// file, and whether it is the namespace (whose default is the map itself).
type cssModule struct {
	file      string
	namespace bool
}

func (e *extractor) bindCSSModule(local, file string, namespace bool) {
	if local == "" {
		return
	}
	if e.cssModules == nil {
		e.cssModules = map[string]cssModule{}
	}
	e.cssModules[local] = cssModule{file, namespace}
}

func (e *extractor) hasChild(n *ts.Node, typ string) bool {
	for i := range n.ChildCount() {
		if e.typ(n.Child(i)) == typ {
			return true
		}
	}
	return false
}

// cssModuleMember states the class `s.x`, `s?.x`, `s!.x` or `s['x']`
// names when s is bound to a CSS Modules file, no local declaration shadows
// it and the access reads a value (valueUse). A namespace's default is the
// map, no class.
func (e *extractor) cssModuleMember(n *ts.Node, sc scope) {
	if len(e.cssModules) == 0 || !e.valueUse(n) {
		return
	}
	obj := e.unwrap(e.field(n, "object"))
	if e.typ(obj) != "identifier" {
		return
	}
	local := e.text(obj)
	m, ok := e.cssModules[local]
	if !ok || sc.locals.Values[local] {
		return
	}
	switch e.typ(n) {
	case "member_expression":
		if prop := e.field(n, "property"); e.typ(prop) == "property_identifier" && !(m.namespace && e.text(prop) == "default") {
			e.cssClassRef(prop, e.text(prop), m.file, sc.container)
		}
	case "subscript_expression":
		if idx := e.field(n, "index"); e.typ(idx) == "string" {
			cls := e.plainString(idx)
			if cls == "" || m.namespace && cls == "default" || idx.NamedChildCount() != 1 {
				return
			}
			e.cssClassRef(idx.NamedChild(0), cls, m.file, sc.container) // the text, without quotes
		}
	}
}

// cssModuleName states the class a use of a name a named import binds to
// one denotes: an identifier or a shorthand property ({ x }) that reads a
// value (valueUse), not shadowed by a local declaration.
func (e *extractor) cssModuleName(n *ts.Node, sc scope) {
	b, ok := e.cssClasses[e.text(n)]
	if !ok || sc.locals.Values[e.text(n)] || !e.valueUse(n) {
		return
	}
	e.cssClassRef(n, b[1], b[0], sc.container)
}

// valueUse reports whether the expression n is read as a value where it
// stands: an operand, an argument, an element, an initializer, a property's
// value or computed key, a condition, a returned or interpolated value, the
// object of a member access.
// Anything else — a callee, a constructor, an assignment's target, the
// operand of delete or ++, a parameter or a key, a name in a type, a JSX
// tag or attribute name — is no use of a class (a list of what is allowed:
// a syntax not listed is no use).
func (e *extractor) valueUse(n *ts.Node) bool {
	// Parentheses and a non-null assertion do not change where the value
	// stands: (s.x) = 1 assigns to s.x.
	p := n.Parent()
	for p != nil && (e.typ(p) == "parenthesized_expression" || e.typ(p) == "non_null_expression") {
		n, p = p, p.Parent()
	}
	if p == nil {
		return false
	}
	field := ""
	for i := range p.ChildCount() {
		if c := p.Child(i); c.StartByte() == n.StartByte() && c.EndByte() == n.EndByte() && e.typ(c) == e.typ(n) {
			field = p.FieldNameForChild(i, e.lang)
			break
		}
	}
	switch e.typ(p) {
	case "arguments", "array", "binary_expression", "ternary_expression",
		"jsx_expression", "template_substitution", "spread_element", "await_expression",
		"return_statement", "expression_statement", "sequence_expression", "yield_expression",
		"throw_statement", "object", "computed_property_name":
		return true
	case "if_statement", "while_statement", "do_statement", "for_statement":
		return field == "condition"
	case "switch_statement":
		return field == "value"
	case "for_in_statement":
		return field == "right"
	case "unary_expression":
		return !strings.HasPrefix(e.text(p), "delete")
	case "as_expression", "satisfies_expression":
		return e.typ(n) != "type_identifier"
	case "variable_declarator", "pair", "public_field_definition", "field_definition",
		"required_parameter", "optional_parameter", "switch_case":
		return field == "value"
	case "assignment_expression", "augmented_assignment_expression", "assignment_pattern", "object_assignment_pattern":
		return field == "right"
	case "arrow_function":
		return field == "body"
	case "export_statement":
		return field == "value" // export default <expression>
	case "member_expression", "subscript_expression":
		return field == "object" || field == "index"
	}
	return false
}

// cssModuleDestructure states the classes a destructuring of a CSS Modules
// file's class map names — const { btn, title: t } = s — at their keys: the
// pattern reads the map's properties btn and title. Computed keys and rest
// elements name no class.
func (e *extractor) cssModuleDestructure(n *ts.Node, sc scope) {
	if len(e.cssModules) == 0 {
		return
	}
	val, pat := e.field(n, "value"), e.field(n, "name")
	if e.typ(val) != "identifier" || e.typ(pat) != "object_pattern" {
		return
	}
	m, ok := e.cssModules[e.text(val)]
	if !ok || sc.locals.Values[e.text(val)] {
		return
	}
	for i := range pat.NamedChildCount() {
		var key *ts.Node
		cls := ""
		switch c := pat.NamedChild(i); e.typ(c) {
		case "shorthand_property_identifier_pattern":
			key, cls = c, e.text(c)
		case "pair_pattern":
			switch k := e.field(c, "key"); e.typ(k) {
			case "property_identifier":
				key, cls = k, e.text(k)
			case "string":
				if k.NamedChildCount() == 1 {
					key, cls = k.NamedChild(0), e.plainString(k)
				}
			}
		}
		if key == nil || cls == "" || m.namespace && cls == "default" {
			continue
		}
		e.cssClassRef(key, cls, m.file, sc.container)
	}
}

// cssClassRef appends the reference to the class cls of the CSS Modules
// file at file.
func (e *extractor) cssClassRef(at *ts.Node, cls, file, container string) {
	e.refs = append(e.refs, language.ReferenceDraft{
		Name:                 cls,
		Kind:                 string(reference.KindValueReference),
		Container:            container,
		Location:             nodeLocation(at, e.file),
		NameQualified:        file + " ." + cls,
		ConfidenceCap:        "strong",
		TargetKinds:          string(symbol.KindCSSClass),
		IdentityInRepository: true,
		FileIdentity:         true,
	})
}

// plainString is the value of a string literal without escapes or line
// breaks ("" otherwise: its value is not its text).
func (e *extractor) plainString(n *ts.Node) string {
	v := e.stringValue(n)
	if strings.ContainsAny(v, "\\\n\r") {
		return ""
	}
	return v
}
