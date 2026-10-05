package php

import (
	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/source"
)

// extractReferences walks the tree for calls, constructions, member/constant
// access, type references, and class/interface/trait relations. Traversal
// mechanics (how we descend) are kept separate from semantic interpretation
// (what each node means); the enclosing container's qualified identity is
// threaded explicitly so references attach to the right symbol.
func extractReferences(root *ts.Node, lang *ts.Language, src []byte, file source.FileID) []language.ReferenceDraft {
	c := &refCollector{lang: lang, src: src, file: file}
	c.walkContainer(root, "")
	return c.refs
}

type refCollector struct {
	lang *ts.Language
	src  []byte
	file source.FileID
	refs []language.ReferenceDraft
}

func (c *refCollector) add(nameNode *ts.Node, name string, kind reference.ReferenceKind, container, receiver string, isCall bool) {
	c.addTyped(nameNode, name, kind, container, receiver, "", isCall)
}

// addTyped is add with provider-proven receiver type evidence.
func (c *refCollector) addTyped(nameNode *ts.Node, name string, kind reference.ReferenceKind, container, receiver, receiverType string, isCall bool) {
	if name == "" || nameNode == nil {
		return
	}
	c.refs = append(c.refs, language.ReferenceDraft{
		Name:         name,
		Kind:         string(kind),
		Container:    container,
		Location:     nodeLocation(nameNode, c.file),
		ReceiverExpr: receiver,
		ReceiverType: receiverType,
		IsCall:       isCall,
	})
}

// walkContainer iterates the direct children of a container node (program root
// or bracketed namespace body), dispatching declarations and relations. ns is
// the active namespace path.
func (c *refCollector) walkContainer(node *ts.Node, ns string) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Type(c.lang) {
		case "namespace_definition":
			name := childText(child, c.lang, c.src, "namespace_name")
			if body := childByType(child, c.lang, "compound_statement"); body != nil {
				c.walkContainer(body, name)
			} else {
				ns = name
			}
		case "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration":
			c.walkType(child, ns)
		case "function_definition":
			fq := qualify(ns, childText(child, c.lang, c.src, "name"))
			c.emitReturnType(child, fq)
			c.emitParamTypes(child, fq)
			if body := childByType(child, c.lang, "compound_statement"); body != nil {
				c.walkBody(body, fq, "", phpFunctionTypeEnv(child, c.lang, c.src, nil))
			}
		case "namespace_use_declaration":
			// Imports are handled in the symbol pass; not references.
		default:
			// Top-level script statements: references attach to no symbol
			// (container ""), so they never form a graph edge but remain
			// findable by name.
			c.walkBody(child, "", "", nil)
		}
	}
}

// walkType extracts class/interface/trait/enum relations and member references.
func (c *refCollector) walkType(node *ts.Node, ns string) {
	name := childText(node, c.lang, c.src, "name")
	if name == "" {
		return
	}
	classQual := qualify(ns, name)
	c.emitRelations(node, classQual)

	body := childByType(node, c.lang, "declaration_list")
	if body == nil {
		body = childByType(node, c.lang, "enum_declaration_list")
	}
	if body != nil {
		c.walkMembers(body, classQual, name)
	}
}

// emitRelations extracts extends / implements relations from a type header.
// base_clause → inheritance (class extends; interface extends), and
// class_interface_clause → implementation.
func (c *refCollector) emitRelations(node *ts.Node, classQual string) {
	if base := childByType(node, c.lang, "base_clause"); base != nil {
		c.emitClauseNames(base, reference.KindInheritance, classQual)
	}
	if impl := childByType(node, c.lang, "class_interface_clause"); impl != nil {
		c.emitClauseNames(impl, reference.KindImplements, classQual)
	}
}

// emitClauseNames emits one reference per name/qualified_name in a clause.
func (c *refCollector) emitClauseNames(clause *ts.Node, kind reference.ReferenceKind, container string) {
	for i := 0; i < clause.ChildCount(); i++ {
		child := clause.Child(i)
		switch child.Type(c.lang) {
		case "name", "qualified_name":
			c.add(child, lastName(child, c.lang, c.src), kind, container, "", false)
		}
	}
}

// walkMembers handles trait use, member signature types, and method bodies.
func (c *refCollector) walkMembers(body *ts.Node, classQual, classBare string) {
	props := phpClassPropertyTypes(body, c.lang, c.src)
	for i := 0; i < body.ChildCount(); i++ {
		member := body.Child(i)
		switch member.Type(c.lang) {
		case "method_declaration":
			name := childText(member, c.lang, c.src, "name")
			if name == "" {
				continue
			}
			mq := classQual + "." + name
			c.emitReturnType(member, mq)
			c.emitParamTypes(member, mq)
			if mbody := childByType(member, c.lang, "compound_statement"); mbody != nil {
				c.walkBody(mbody, mq, classBare, phpFunctionTypeEnv(member, c.lang, c.src, props))
			}
		case "property_declaration":
			c.emitTypeChildren(member, classQual)
		case "use_declaration":
			// Trait use: the trait names are the direct name/qualified_name
			// children (adaptations live inside a nested use_list, ignored).
			for j := 0; j < member.ChildCount(); j++ {
				g := member.Child(j)
				switch g.Type(c.lang) {
				case "name", "qualified_name":
					c.add(g, lastName(g, c.lang, c.src), reference.KindUsesTrait, classQual, "", false)
				}
			}
		}
	}
}

// emitParamTypes emits type references for each parameter's declared type.
func (c *refCollector) emitParamTypes(node *ts.Node, container string) {
	fp := childByType(node, c.lang, "formal_parameters")
	if fp == nil {
		return
	}
	for i := 0; i < fp.ChildCount(); i++ {
		param := fp.Child(i)
		switch param.Type(c.lang) {
		case "simple_parameter", "property_promotion_parameter", "variadic_parameter":
			c.emitTypeChildren(param, container)
		}
	}
}

// emitReturnType emits type references for a function/method return type, which
// is a direct type-node child (after formal_parameters).
func (c *refCollector) emitReturnType(node *ts.Node, container string) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if isTypeNode(child.Type(c.lang)) {
			c.collectNamedTypes(child, container)
		}
	}
}

// emitTypeChildren emits type references for the type-node children of node
// (used for parameters and property declarations).
func (c *refCollector) emitTypeChildren(node *ts.Node, container string) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if isTypeNode(child.Type(c.lang)) {
			c.collectNamedTypes(child, container)
		}
	}
}

// collectNamedTypes recursively emits a KindTypeUse reference for every
// named_type under node, skipping builtins (primitive_type is a different node
// and is never visited) and the special self/static/parent types.
func (c *refCollector) collectNamedTypes(node *ts.Node, container string) {
	if node.Type(c.lang) == "named_type" {
		name := childText(node, c.lang, c.src, "name")
		if name != "" && !isRelativeType(name) {
			c.add(node, name, reference.KindTypeUse, container, "", false)
		}
		return
	}
	for i := 0; i < node.ChildCount(); i++ {
		c.collectNamedTypes(node.Child(i), container)
	}
}

// walkBody descends into a method/function body collecting expression
// references. It does not descend into nested named declarations or anonymous
// classes (those are separate containers / have no symbol identity). selfClass
// is the enclosing class's bare name, used only to resolve `$this` receivers.
// env is the function's proven receiver-type evidence (nil when none).
func (c *refCollector) walkBody(node *ts.Node, container, selfClass string, env *phpTypeEnv) {
	switch node.Type(c.lang) {
	case "function_definition", "method_declaration", "anonymous_class":
		return
	case "anonymous_function", "arrow_function":
		// Closures have their own variable scope: no receiver evidence inside.
		env = nil
	case "function_call_expression":
		c.emitFunctionCall(node, container)
	case "member_call_expression":
		c.emitMemberCall(node, container, selfClass, env)
	case "scoped_call_expression":
		c.emitScopedCall(node, container)
	case "object_creation_expression":
		c.emitConstruction(node, container)
	case "class_constant_access_expression":
		c.emitConstAccess(node, container)
	}
	for i := 0; i < node.ChildCount(); i++ {
		c.walkBody(node.Child(i), container, selfClass, env)
	}
}

func (c *refCollector) emitFunctionCall(node *ts.Node, container string) {
	callee := firstChildOfTypes(node, c.lang, "name", "qualified_name")
	if callee == nil {
		return // dynamic callee (e.g. $fn()) — not fabricated
	}
	c.add(callee, lastName(callee, c.lang, c.src), reference.KindCall, container, "", true)
}

func (c *refCollector) emitMemberCall(node *ts.Node, container, selfClass string, env *phpTypeEnv) {
	method := childByType(node, c.lang, "name")
	if method == nil {
		return // dynamic method (e.g. $obj->$m()) — not fabricated
	}
	recv := node.Child(0)
	if recv == nil {
		return
	}
	// `$this` is the enclosing class (an explicit type receiver). Every other
	// receiver is recorded verbatim — "" is reserved for receiverless names —
	// with declared-type evidence only when it is proven (receiver_types.go).
	receiver, receiverType := recv.Text(c.src), ""
	if phpVarName(recv, c.lang, c.src) == "this" {
		receiver = selfClass
	} else {
		receiverType = env.receiverType(recv, node.StartByte(), c.lang, c.src)
	}
	c.addTyped(method, method.Text(c.src), reference.KindCall, container, receiver, receiverType, true)
}

func (c *refCollector) emitScopedCall(node *ts.Node, container string) {
	scope, member := c.scopeAndMember(node)
	if member == nil {
		return // dynamic member — not fabricated
	}
	c.add(member, member.Text(c.src), reference.KindCall, container, scope, true)
}

func (c *refCollector) emitConstruction(node *ts.Node, container string) {
	cls := firstChildOfTypes(node, c.lang, "name", "qualified_name")
	if cls == nil {
		return // `new $cls()` or `new self()` — not fabricated
	}
	c.add(cls, lastName(cls, c.lang, c.src), reference.KindConstruction, container, "", false)
}

func (c *refCollector) emitConstAccess(node *ts.Node, container string) {
	scope, member := c.scopeAndMember(node)
	if member == nil {
		return
	}
	// Class constant / enum case access is a read, not a call.
	c.add(member, member.Text(c.src), reference.KindRead, container, scope, false)
}

// scopeAndMember parses a scoped_call_expression or
// class_constant_access_expression into (receiver class name, member node).
// The receiver is empty for relative scopes (self/static/parent). member is nil
// when the member is dynamic.
func (c *refCollector) scopeAndMember(node *ts.Node) (receiver string, member *ts.Node) {
	seenScope := false
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Type(c.lang) {
		case "name", "qualified_name":
			if !seenScope {
				receiver = lastName(child, c.lang, c.src)
				seenScope = true
			} else if member == nil {
				member = child
			}
		case "relative_scope":
			// self/static/parent — do not guess the class.
			seenScope = true
		}
	}
	return receiver, member
}

// --- small AST helpers (mechanics) ---

func isTypeNode(t string) bool {
	switch t {
	case "named_type", "optional_type", "union_type", "intersection_type":
		return true
	}
	return false
}

func isRelativeType(name string) bool {
	return name == "self" || name == "static" || name == "parent"
}

// lastName returns the bare identifier for a `name` or the final segment of a
// `qualified_name`.
func lastName(node *ts.Node, lang *ts.Language, src []byte) string {
	if node.Type(lang) == "name" {
		return node.Text(src)
	}
	// qualified_name: take the last direct `name` child (the class segment).
	last := ""
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "name" {
			last = child.Text(src)
		}
	}
	return last
}

func firstChildOfTypes(node *ts.Node, lang *ts.Language, types ...string) *ts.Node {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		for _, t := range types {
			if child.Type(lang) == t {
				return child
			}
		}
	}
	return nil
}
