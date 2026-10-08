package terraform

import (
	"slices"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
)

// maxExprDepth bounds the syntax-tree depth the reference walk descends.
// Deeper nesting is reported once per file and not observed.
const maxExprDepth = 256

// contextualRoots are Terraform's built-in and contextual value roots, and
// the roots it reserves and rejects (addrs.ParseRef): they denote no
// declaration (package doc).
var contextualRoots = map[string]bool{
	"path": true, "terraform": true, "count": true, "each": true, "self": true,
	"caller": true, "plan": true, "state": true, "template": true, "lazy": true, "arg": true,
}

// reservedTypes are the managed-resource types whose TYPE.NAME would read as
// another reference root, or as one of the address namespaces this package
// gives non-resource declarations (output.x, check.x, provider.x). A
// resource of such a type has the address resource.TYPE.NAME — the only
// spelling Terraform accepts for it — and a reference whose root is output,
// check or provider (no reference roots in a configuration file, so a
// resource type to Terraform) is a reference to such a resource, never to
// the output, check or provider block.
var reservedTypes = map[string]bool{
	"var": true, "local": true, "module": true, "data": true, "ephemeral": true,
	"resource": true, "list": true, "action": true, "output": true, "check": true,
	"provider": true, "run": true, "terraform": true, "path": true, "count": true,
	"each": true, "self": true, "caller": true, "plan": true, "state": true,
	"template": true, "lazy": true, "arg": true,
}

// scan walks the expressions of one declaration. locals are the names bound
// by enclosing for-expressions, template for directives and dynamic-block
// iterators: they shadow every address root.
type scan struct {
	x         *extractor
	container string
	blockType string
	locals    []string
	// checkData maps the addresses of data sources scoped to the enclosing
	// check block to their qualified identities.
	checkData map[string]string
	// moduleAddr / moduleCall are the address and qualified identity of the
	// enclosing module call.
	moduleAddr, moduleCall string
}

// topBody scans the body of a top-level block, applying the meta-arguments
// Terraform defines for that block type.
func (s *scan) topBody(body *ts.Node) {
	lang, src := s.x.lang, s.x.src
	for i := 0; i < body.ChildCount(); i++ {
		c := body.Child(i)
		switch c.Type(lang) {
		case "attribute":
			id := firstChildOfType(c, lang, "identifier")
			if id == nil {
				continue
			}
			v := attrValue(c, lang)
			switch name := id.Text(src); {
			case name == "depends_on":
				s.expr(v, reference.KindExplicitDependency, 0)
			case name == "provider" && isResourceLike(s.blockType):
				s.providerRef(v)
			case name == "providers" && s.blockType == "module":
				s.providersMap(v)
			case (name == "source" || name == "version") && s.blockType == "module":
			case name == "type" && s.blockType == "variable":
				// a type constraint, not an expression
			case name == "alias" && s.blockType == "provider":
			case s.blockType == "module" && name != "count" && name != "for_each":
				s.moduleArgument(id)
				s.expr(v, reference.KindValueReference, 0)
			default:
				s.expr(v, reference.KindValueReference, 0)
			}
		case "block":
			typ, _, nested, ok := s.x.blockHeader(c)
			if !ok || nested == nil {
				continue
			}
			switch {
			case typ == "lifecycle":
				s.lifecycle(nested)
			case typ == "data" && s.blockType == "check":
				s.checkDataBody(c, nested)
			default:
				s.nestedBody(c, nested, 1)
			}
		}
	}
}

func isResourceLike(blockType string) bool {
	return blockType == "resource" || blockType == "data" || blockType == "ephemeral"
}

// nestedBody scans a nested block's body (provisioner, connection, dynamic,
// precondition, provider-schema blocks, ...).
func (s *scan) nestedBody(block, body *ts.Node, depth int) {
	if !s.deep(body, depth) {
		return
	}
	lang := s.x.lang
	typ, labels, _, _ := s.x.blockHeader(block)
	if typ == "dynamic" {
		s.dynamic(labels, body, depth)
		return
	}
	for i := 0; i < body.ChildCount(); i++ {
		c := body.Child(i)
		switch c.Type(lang) {
		case "attribute":
			s.expr(attrValue(c, lang), reference.KindValueReference, depth+1)
		case "block":
			if _, _, nested, ok := s.x.blockHeader(c); ok && nested != nil {
				s.nestedBody(c, nested, depth+1)
			}
		}
	}
}

// dynamic scans a dynamic block: for_each in the enclosing scope, everything
// else (content, labels) with the iterator bound — the block label, or the
// `iterator` argument.
func (s *scan) dynamic(labels []string, body *ts.Node, depth int) {
	lang, src := s.x.lang, s.x.src
	iterator := ""
	if len(labels) == 1 {
		iterator = labels[0]
	}
	for i := 0; i < body.ChildCount(); i++ {
		c := body.Child(i)
		if c.Type(lang) != "attribute" {
			continue
		}
		if id := firstChildOfType(c, lang, "identifier"); id != nil && id.Text(src) == "iterator" {
			if name, ok := bareName(attrValue(c, lang), lang, src); ok {
				iterator = name
			}
		}
	}
	inner := s.with(iterator)
	for i := 0; i < body.ChildCount(); i++ {
		c := body.Child(i)
		switch c.Type(lang) {
		case "attribute":
			id := firstChildOfType(c, lang, "identifier")
			if id == nil {
				continue
			}
			switch id.Text(src) {
			case "iterator":
			case "for_each":
				s.expr(attrValue(c, lang), reference.KindValueReference, depth+1)
			default:
				inner.expr(attrValue(c, lang), reference.KindValueReference, depth+1)
			}
		case "block":
			if _, _, nested, ok := s.x.blockHeader(c); ok && nested != nil {
				inner.nestedBody(c, nested, depth+1)
			}
		}
	}
}

// lifecycle scans a lifecycle block. ignore_changes lists attribute paths of
// the resource itself, never references.
func (s *scan) lifecycle(body *ts.Node) {
	lang, src := s.x.lang, s.x.src
	for i := 0; i < body.ChildCount(); i++ {
		c := body.Child(i)
		switch c.Type(lang) {
		case "attribute":
			if id := firstChildOfType(c, lang, "identifier"); id != nil && id.Text(src) == "ignore_changes" {
				continue
			}
			s.expr(attrValue(c, lang), reference.KindValueReference, 2)
		case "block":
			if _, _, nested, ok := s.x.blockHeader(c); ok && nested != nil {
				s.nestedBody(c, nested, 2)
			}
		}
	}
}

// checkDataBody scans a data source scoped to a check block; its references
// belong to that data source.
func (s *scan) checkDataBody(block, body *ts.Node) {
	_, labels, _, _ := s.x.blockHeader(block)
	inner := *s
	inner.container = ""
	if len(labels) == 2 {
		inner.container = s.checkData["data."+labels[0]+"."+labels[1]]
	}
	inner.blockType = "data"
	inner.topBody(body)
}

// moduleArgument records an input argument of a module call: a reference
// from the call to the child module's variable of that name, through the
// call (NamedArgument, resolved by the call's ParameterScope). It is a
// reference to the variable's declaration — the interface the call binds —
// like `module.NAME.OUTPUT` is to the output's; the value flowing from the
// argument expression into the child is not an edge.
func (s *scan) moduleArgument(name *ts.Node) {
	s.x.refs = append(s.x.refs, language.ReferenceDraft{
		Name:                  name.Text(s.x.src),
		Kind:                  string(reference.KindValueReference),
		Container:             s.container,
		Location:              nodeLocation(name, s.x.file),
		ReceiverExpr:          s.moduleAddr,
		ReceiverTypeQualified: s.moduleCall,
		IdentityInRepository:  true,
		NamedArgument:         true,
	})
}

// providerRef records a `provider = NAME[.ALIAS]` meta-argument: a reference
// to the provider configuration provider.NAME[.ALIAS].
func (s *scan) providerRef(v *ts.Node) {
	if v == nil {
		return
	}
	first, last, parts, ok := traversal(v, s.x.lang, s.x.src)
	if !ok || len(parts) > 2 {
		return
	}
	address := "provider." + parts[0]
	if len(parts) == 2 {
		address += "." + parts[1]
	}
	s.emit(address, reference.KindValueReference, spanLocationNodes(first, last, s.x))
}

// providersMap records the parent-side provider configurations of a module
// call's `providers = { child = parent }`.
func (s *scan) providersMap(v *ts.Node) {
	lang := s.x.lang
	var walk func(n *ts.Node, depth int)
	walk = func(n *ts.Node, depth int) {
		if n == nil || depth > maxExprDepth {
			return
		}
		if n.Type(lang) == "object_elem" {
			if val := lastNamedChild(n); val != nil {
				s.providerRef(val)
			}
			return
		}
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i), depth+1)
		}
	}
	walk(v, 0)
}

// with returns a copy of s with name bound as a local.
func (s *scan) with(names ...string) *scan {
	inner := *s
	inner.locals = append(slices.Clone(s.locals), names...)
	return &inner
}

// deep reports whether depth is within maxExprDepth, reporting the first
// excess once per file.
func (s *scan) deep(n *ts.Node, depth int) bool {
	if depth <= maxExprDepth {
		return true
	}
	if !s.x.deepReported {
		s.x.deepReported = true
		s.x.diags = append(s.x.diags, language.Diagnostic{
			Severity: language.SeverityWarning,
			Code:     diagNestingTooDeep,
			Message:  "terraform: nesting too deep; references below it are not observed",
			Location: nodeLocation(n, s.x.file),
		})
	}
	return false
}

// expr walks an expression subtree and records every static address
// traversal in it as a reference of kind.
func (s *scan) expr(n *ts.Node, kind reference.ReferenceKind, depth int) {
	if n == nil || !s.deep(n, depth) {
		return
	}
	lang := s.x.lang
	switch n.Type(lang) {
	case "ERROR":
		return // nothing inside a syntax error is observed
	case "for_tuple_expr", "for_object_expr":
		s.forExpr(n, kind, depth)
		return
	case "template_for":
		s.templateFor(n, kind, depth)
		return
	}
	// An object key written as a bare name is a string, never a reference;
	// it needs no case of its own: a bare name is no address (traversalAt).
	for i := 0; i < n.ChildCount(); i++ {
		c := n.Child(i)
		if c.Type(lang) == "variable_expr" {
			s.traversalAt(n, i, kind)
			continue
		}
		s.expr(c, kind, depth+1)
	}
}

// forExpr scans a for expression: the collection in the enclosing scope,
// the body and condition with the loop variables bound.
func (s *scan) forExpr(n *ts.Node, kind reference.ReferenceKind, depth int) {
	lang, src := s.x.lang, s.x.src
	inner := s
	for i := 0; i < n.ChildCount(); i++ {
		c := n.Child(i)
		if c.Type(lang) != "for_intro" {
			inner.expr(c, kind, depth+1)
			continue
		}
		var names []string
		for j := 0; j < c.ChildCount(); j++ {
			g := c.Child(j)
			switch g.Type(lang) {
			case "identifier":
				names = append(names, g.Text(src))
			case "expression":
				s.expr(g, kind, depth+1)
			}
		}
		inner = s.with(names...)
	}
}

// templateFor scans a template `%{ for x in coll }...%{ endfor }`
// directive the same way.
func (s *scan) templateFor(n *ts.Node, kind reference.ReferenceKind, depth int) {
	lang, src := s.x.lang, s.x.src
	inner := s
	for i := 0; i < n.ChildCount(); i++ {
		c := n.Child(i)
		if c.Type(lang) != "template_for_start" {
			inner.expr(c, kind, depth+1)
			continue
		}
		var names []string
		for j := 0; j < c.ChildCount(); j++ {
			g := c.Child(j)
			switch g.Type(lang) {
			case "identifier":
				names = append(names, g.Text(src))
			case "expression":
				s.expr(g, kind, depth+1)
			}
		}
		inner = s.with(names...)
	}
}

// traversalAt records the address traversal starting at parent's i-th child
// (a variable_expr) and the following get_attr / index / splat siblings.
func (s *scan) traversalAt(parent *ts.Node, i int, kind reference.ReferenceKind) {
	lang, src := s.x.lang, s.x.src
	root := parent.Child(i)
	id := firstChildOfType(root, lang, "identifier")
	if id == nil {
		return
	}
	name := id.Text(src)
	if slices.Contains(s.locals, name) || contextualRoots[name] {
		return
	}
	// The address segments are the leading get_attr siblings; the first
	// index or splat ends them.
	var attrs []*ts.Node
	var rest []*ts.Node // the siblings after the leading get_attrs
	for j := i + 1; j < parent.ChildCount(); j++ {
		c := parent.Child(j)
		if len(rest) == 0 && c.Type(lang) == "get_attr" {
			attrs = append(attrs, c)
			continue
		}
		if c.Type(lang) == "get_attr" || c.Type(lang) == "index" || c.Type(lang) == "splat" {
			rest = append(rest, c)
			continue
		}
		break
	}
	seg := func(k int) string {
		if a := firstChildOfType(attrs[k], lang, "identifier"); a != nil {
			return a.Text(src)
		}
		return ""
	}
	need := 1
	prefix := ""
	switch name {
	case "var", "local", "module":
		prefix = name + "."
	case "data", "ephemeral", "list", "action":
		prefix, need = name+".", 2
	case "resource":
		// resource.TYPE.NAME is Terraform's explicit spelling of TYPE.NAME
		// (an escape for a resource type that collides with a reserved root).
		need = 2
		if len(attrs) > 0 && reservedTypes[seg(0)] {
			prefix = "resource."
		}
	default:
		prefix = name + "." // managed resource TYPE.NAME
		if reservedTypes[name] {
			prefix = "resource." + prefix
		}
	}
	if len(attrs) < need {
		return
	}
	address := prefix
	for k := 0; k < need; k++ {
		sg := seg(k)
		if sg == "" {
			return
		}
		if k > 0 {
			address += "."
		}
		address += sg
	}
	loc := spanLocationNodes(root, attrs[need-1], s.x)
	s.emit(address, kind, loc)
	if name != "module" {
		return
	}
	// module.NAME.OUTPUT, module.NAME[key].OUTPUT, module.NAME[*].OUTPUT:
	// the output through the module call.
	var out *ts.Node
	switch {
	case len(attrs) > 1:
		out = attrs[1]
	case len(rest) > 0:
		switch rest[0].Type(lang) {
		case "index":
			if len(rest) > 1 && rest[1].Type(lang) == "get_attr" {
				out = rest[1]
			}
		case "splat":
			if sp := firstNamedChild(rest[0]); sp != nil {
				out = firstChildOfType(sp, lang, "get_attr")
			}
		}
	}
	if out == nil {
		return
	}
	oid := firstChildOfType(out, lang, "identifier")
	if oid == nil {
		return
	}
	s.x.refs = append(s.x.refs, language.ReferenceDraft{
		Name:                  oid.Text(src),
		Kind:                  string(kind),
		Container:             s.container,
		Location:              spanLocationNodes(root, out, s.x),
		ReceiverExpr:          address,
		ReceiverTypeQualified: qualify(s.x.dir, address),
		IdentityInRepository:  true,
	})
}

// emit records a reference to address in the current module (or to the
// enclosing check's scoped data source).
func (s *scan) emit(address string, kind reference.ReferenceKind, loc source.Location) {
	q, scoped := s.checkData[address]
	if !scoped {
		q = qualify(s.x.dir, address)
	}
	s.x.refs = append(s.x.refs, language.ReferenceDraft{
		Name:                 address,
		Kind:                 string(kind),
		Container:            s.container,
		Location:             loc,
		NameQualified:        q,
		IdentityInRepository: true,
	})
}

// traversal returns the root name and attribute names of an expression that
// is exactly a static traversal (root.a.b), and its first and last nodes.
func traversal(n *ts.Node, lang *ts.Language, src []byte) (first, last *ts.Node, parts []string, ok bool) {
	for n != nil && n.Type(lang) == "expression" && n.ChildCount() == 1 && n.Child(0).Type(lang) == "expression" {
		n = n.Child(0)
	}
	if n == nil || n.Type(lang) != "expression" || n.ChildCount() == 0 || n.Child(0).Type(lang) != "variable_expr" {
		return nil, nil, nil, false
	}
	first = n.Child(0)
	id := firstChildOfType(first, lang, "identifier")
	if id == nil {
		return nil, nil, nil, false
	}
	parts = []string{id.Text(src)}
	last = first
	for i := 1; i < n.ChildCount(); i++ {
		c := n.Child(i)
		if c.Type(lang) != "get_attr" {
			return nil, nil, nil, false
		}
		a := firstChildOfType(c, lang, "identifier")
		if a == nil {
			return nil, nil, nil, false
		}
		parts = append(parts, a.Text(src))
		last = c
	}
	return first, last, parts, true
}

// bareName reports whether n is an expression consisting of one bare
// identifier, and returns it.
func bareName(n *ts.Node, lang *ts.Language, src []byte) (string, bool) {
	_, _, parts, ok := traversal(n, lang, src)
	if !ok || len(parts) != 1 {
		return "", false
	}
	return parts[0], true
}

func firstNamedChild(n *ts.Node) *ts.Node {
	for i := 0; i < n.ChildCount(); i++ {
		if c := n.Child(i); c.IsNamed() {
			return c
		}
	}
	return nil
}

func lastNamedChild(n *ts.Node) *ts.Node {
	for i := n.ChildCount() - 1; i >= 0; i-- {
		if c := n.Child(i); c.IsNamed() {
			return c
		}
	}
	return nil
}

func spanLocationNodes(first, last *ts.Node, x *extractor) source.Location {
	return spanLocation(first, last, x.file)
}
