package terraform

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// extractor holds the per-file extraction state. It is created per Extract
// call and never shared.
type extractor struct {
	lang *ts.Language
	src  []byte
	file source.FileID
	dir  string // module directory (moduleDir)
	vars bool   // a .tfvars file

	symbols []language.SymbolDraft
	refs    []language.ReferenceDraft
	diags   []language.Diagnostic

	// seen holds the qualified identities declared in this file so far: a
	// second declaration of one address is invalid Terraform and gets no
	// symbol of its own (its SymbolID would be the first one's).
	seen map[string]bool
	// deepReported: the nesting-depth diagnostic was emitted.
	deepReported bool
}

// blockShape is the label count Terraform requires of a top-level block
// type, and the symbol it declares.
type blockShape struct {
	labels int
	kind   symbol.SymbolKind
	prefix string // address prefix ("" for a managed resource)
}

var topLevelBlocks = map[string]blockShape{
	"resource":  {2, symbol.KindResource, ""},
	"data":      {2, symbol.KindDataSource, "data."},
	"ephemeral": {2, symbol.KindResource, "ephemeral."},
	"module":    {1, symbol.KindModule, "module."},
	"variable":  {1, symbol.KindVariable, "var."},
	"output":    {1, symbol.KindOutput, "output."},
	"provider":  {1, symbol.KindConfiguration, "provider."},
	"check":     {1, symbol.KindConfiguration, "check."},
	"terraform": {0, symbol.KindConfiguration, ""},
}

// topLevel extracts the declarations and references of one top-level body.
func (x *extractor) topLevel(body *ts.Node, override bool) {
	for i := 0; i < body.ChildCount(); i++ {
		c := body.Child(i)
		switch c.Type(x.lang) {
		case "attribute":
			if x.vars {
				x.varsAssignment(c)
			}
		case "block":
			if !x.vars {
				x.block(c, override)
			}
		}
	}
}

// varsAssignment records `name = value` of a .tfvars file as a write of the
// input variable name. Which module receives it is decided outside the
// configuration, so it carries no identity and resolves to nothing.
func (x *extractor) varsAssignment(attr *ts.Node) {
	name := firstChildOfType(attr, x.lang, "identifier")
	if name == nil {
		return
	}
	x.refs = append(x.refs, language.ReferenceDraft{
		Name:     "var." + name.Text(x.src),
		Kind:     string(reference.KindWrite),
		Location: nodeLocation(name, x.file),
	})
}

// block handles one top-level block.
func (x *extractor) block(b *ts.Node, override bool) {
	typ, labels, body, ok := x.blockHeader(b)
	if !ok {
		return
	}
	switch typ {
	case "locals":
		x.locals(body, override)
		return
	case "moved", "import", "removed":
		return // address metadata: not observed (package doc)
	}
	shape, known := topLevelBlocks[typ]
	if !known {
		return
	}
	if len(labels) != shape.labels || slices.ContainsFunc(labels, func(l string) bool { return !validName(l) }) {
		x.diags = append(x.diags, language.Diagnostic{
			Severity: language.SeverityWarning,
			Message:  fmt.Sprintf("terraform: a %s block needs %d label(s), each a valid name", typ, shape.labels),
			Location: nodeLocation(b, x.file),
		})
		return
	}
	if typ == "terraform" {
		// Settings only; Terraform permits no references here.
		if !override {
			x.declare(b, "terraform", shape.kind, "terraform", "", false)
		}
		return
	}

	address := shape.prefix + strings.Join(labels, ".")
	if typ == "provider" {
		if alias, ok := x.literalAttr(body, "alias"); ok {
			if !validName(alias) {
				// Neither this alias nor the default configuration.
				x.diags = append(x.diags, language.Diagnostic{
					Severity: language.SeverityWarning,
					Message:  "terraform: a provider alias must be a valid name",
					Location: nodeLocation(b, x.file),
				})
				return
			}
			address += "." + alias
		}
	}
	container := ""
	if !override {
		sig := typ
		for _, l := range labels {
			sig += " " + quote(l)
		}
		d, ok := x.declare(b, address, shape.kind, sig, "", typ == "variable" || typ == "output")
		if ok {
			if typ == "module" {
				d.MemberScope, d.MembersOutside = x.moduleMembers(body)
			}
			container = d.Qualified
		}
	}
	if body == nil {
		return
	}
	s := &scan{x: x, container: container, blockType: typ}
	if typ == "check" {
		s.checkData = x.checkData(body, container, override)
	}
	s.topBody(body)
}

// declare appends a symbol for address unless this file already declared it
// (a diagnostic is emitted instead). The returned pointer is valid until the
// next declare.
func (x *extractor) declare(node *ts.Node, address string, kind symbol.SymbolKind, signature, parent string, exported bool) (*language.SymbolDraft, bool) {
	q := qualify(x.dir, address)
	if parent != "" {
		q = parent + "/" + address
	}
	if x.seen[q] {
		if address != "terraform" {
			x.diags = append(x.diags, language.Diagnostic{
				Severity: language.SeverityWarning,
				Message:  "terraform: duplicate declaration of " + address,
				Location: nodeLocation(node, x.file),
			})
		}
		return nil, false
	}
	x.seen[q] = true
	x.symbols = append(x.symbols, language.SymbolDraft{
		Name:      address,
		Qualified: q,
		Kind:      kind,
		Location:  nodeLocation(node, x.file),
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Parent:    parent,
		Signature: signature,
		Exported:  exported,
	})
	return &x.symbols[len(x.symbols)-1], true
}

// locals declares one symbol per attribute of a locals block.
func (x *extractor) locals(body *ts.Node, override bool) {
	if body == nil {
		return
	}
	for i := 0; i < body.ChildCount(); i++ {
		attr := body.Child(i)
		if attr.Type(x.lang) != "attribute" {
			continue
		}
		name := firstChildOfType(attr, x.lang, "identifier")
		if name == nil {
			continue
		}
		address := "local." + name.Text(x.src)
		container := ""
		if !override {
			if d, ok := x.declare(attr, address, symbol.KindVariable, address, "", false); ok {
				container = d.Qualified
			}
		}
		s := &scan{x: x, container: container, blockType: "locals"}
		s.expr(attrValue(attr, x.lang), reference.KindValueReference, 0)
	}
}

// checkData declares the data sources nested in a check block. They are
// scoped to the check: a reference to one inside the check denotes it.
func (x *extractor) checkData(body *ts.Node, check string, override bool) map[string]string {
	scoped := make(map[string]string)
	if check == "" || override {
		return scoped
	}
	for i := 0; i < body.ChildCount(); i++ {
		b := body.Child(i)
		if b.Type(x.lang) != "block" {
			continue
		}
		typ, labels, _, ok := x.blockHeader(b)
		if !ok || typ != "data" || len(labels) != 2 {
			continue
		}
		address := "data." + labels[0] + "." + labels[1]
		if d, ok := x.declare(b, address, symbol.KindDataSource, "data "+quote(labels[0])+" "+quote(labels[1]), check, false); ok {
			scoped[address] = d.Qualified
		}
	}
	return scoped
}

// moduleMembers states where the outputs of a module call live, from its
// `source` argument: a local path names the child module directory; any
// other source is fetched from outside the repository; a source that is no
// literal (or absent) states nothing.
func (x *extractor) moduleMembers(body *ts.Node) (scope string, outside bool) {
	src, ok := x.literalAttr(body, "source")
	if !ok {
		return "", false
	}
	if !strings.HasPrefix(src, "./") && !strings.HasPrefix(src, "../") {
		return "", true
	}
	child := path.Join(x.dir, src)
	if child == ".." || strings.HasPrefix(child, "../") {
		return "", true // leaves the indexed root
	}
	return qualify(child, "output."), false
}

// blockHeader returns a block's type, its labels (literal strings or bare
// identifiers) and its body (nil when empty). ok is false when the header is
// not a well-formed static header.
func (x *extractor) blockHeader(b *ts.Node) (typ string, labels []string, body *ts.Node, ok bool) {
	if b.IsError() {
		return "", nil, nil, false
	}
	for i := 0; i < b.ChildCount(); i++ {
		c := b.Child(i)
		switch c.Type(x.lang) {
		case "identifier":
			if typ == "" {
				typ = c.Text(x.src)
			} else {
				labels = append(labels, c.Text(x.src))
			}
		case "string_lit":
			l, lit := x.stringLiteral(c)
			if !lit {
				return "", nil, nil, false
			}
			labels = append(labels, l)
		case "body":
			body = c
		case "ERROR":
			if body == nil && typ != "" && len(labels) == 0 {
				return "", nil, nil, false
			}
		}
	}
	return typ, labels, body, typ != ""
}

// literalAttr returns the literal string value of attribute name directly in
// body.
func (x *extractor) literalAttr(body *ts.Node, name string) (string, bool) {
	if body == nil {
		return "", false
	}
	for i := 0; i < body.ChildCount(); i++ {
		attr := body.Child(i)
		if attr.Type(x.lang) != "attribute" {
			continue
		}
		id := firstChildOfType(attr, x.lang, "identifier")
		if id == nil || id.Text(x.src) != name {
			continue
		}
		v := attrValue(attr, x.lang)
		if v == nil {
			return "", false
		}
		// expression → template_expr → quoted_template, or literal_value →
		// string_lit, depending on the grammar's choice for the literal.
		for n := v; n != nil; {
			switch n.Type(x.lang) {
			case "string_lit", "quoted_template":
				return x.stringLiteral(n)
			case "expression", "template_expr", "literal_value":
				if n.ChildCount() != 1 {
					return "", false
				}
				n = n.Child(0)
			default:
				return "", false
			}
		}
		return "", false
	}
	return "", false
}

// stringLiteral returns the text of a quoted string without interpolation,
// directive or escape sequence; ok is false otherwise.
func (x *extractor) stringLiteral(n *ts.Node) (string, bool) {
	var sb strings.Builder
	for i := 0; i < n.ChildCount(); i++ {
		c := n.Child(i)
		switch c.Type(x.lang) {
		case "quoted_template_start", "quoted_template_end":
		case "template_literal":
			t := c.Text(x.src)
			if strings.ContainsAny(t, `\`) {
				return "", false
			}
			sb.WriteString(t)
		default:
			return "", false
		}
	}
	return sb.String(), true
}

// validName reports Terraform's rule for declaration names, which is HCL's
// identifier syntax (hclsyntax.ValidIdentifier, used by Terraform for
// resource / data / ephemeral types and names, module, variable, output and
// local names and provider aliases): (ID_Start | '_') (ID_Continue | '-')*,
// with ID_Start / ID_Continue the Unicode properties of UAX #31. Names are
// compared as written — HCL does not normalize identifiers — so a
// look-alike in another script is another name.
func validName(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for i, r := range s {
		switch {
		case i == 0 && (r == '_' || isIDStart(r)):
		case i > 0 && (r == '-' || isIDContinue(r)):
		default:
			return false
		}
	}
	return true
}

// isIDStart / isIDContinue derive UAX #31's ID_Start / ID_Continue from the
// general categories and properties in package unicode.
func isIDStart(r rune) bool {
	if unicode.In(r, unicode.Pattern_Syntax, unicode.Pattern_White_Space) {
		return false
	}
	return unicode.In(r, unicode.L, unicode.Nl, unicode.Other_ID_Start)
}

func isIDContinue(r rune) bool {
	if isIDStart(r) {
		return true
	}
	if unicode.In(r, unicode.Pattern_Syntax, unicode.Pattern_White_Space) {
		return false
	}
	return unicode.In(r, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc, unicode.Other_ID_Continue)
}

func quote(s string) string { return `"` + s + `"` }

func firstChildOfType(n *ts.Node, lang *ts.Language, typ string) *ts.Node {
	for i := 0; i < n.ChildCount(); i++ {
		if c := n.Child(i); c.Type(lang) == typ {
			return c
		}
	}
	return nil
}

// attrValue is the expression of an attribute node.
func attrValue(attr *ts.Node, lang *ts.Language) *ts.Node {
	return firstChildOfType(attr, lang, "expression")
}
