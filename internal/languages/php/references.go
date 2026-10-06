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
	c := &refCollector{lang: lang, src: src, file: file, scope: newNameScope("")}
	c.walkContainer(root)
	return c.refs
}

type refCollector struct {
	lang *ts.Language
	src  []byte
	file source.FileID
	refs []language.ReferenceDraft

	// scope is the lexical name-resolution context (active namespace + class
	// imports) at the node being walked. It is per-collector, advanced strictly
	// in source order by walkContainer and restored after a bracketed namespace
	// body, and is used only to turn a written class name into its FQN.
	scope *nameScope
}

func (c *refCollector) add(nameNode *ts.Node, name string, kind reference.ReferenceKind, container, receiver string, isCall bool) {
	c.addTyped(nameNode, name, kind, container, receiver, "", isCall)
}

// addTyped is add with provider-proven receiver type evidence.
func (c *refCollector) addTyped(nameNode *ts.Node, name string, kind reference.ReferenceKind, container, receiver, receiverType string, isCall bool) {
	c.addDraft(nameNode, language.ReferenceDraft{
		Name:         name,
		Kind:         string(kind),
		Container:    container,
		ReceiverExpr: receiver,
		ReceiverType: receiverType,
		IsCall:       isCall,
	})
}

// addClassName emits a reference whose Name is a class-like name: the name node
// is resolved to its FQN by the lexical rules (see names.go) and the identity is
// attached as NameQualified. loc is the node whose position is the reference's.
func (c *refCollector) addClassName(loc, nameNode *ts.Node, kind reference.ReferenceKind, container string) {
	if nameNode == nil {
		return
	}
	qual, _ := c.scope.resolveClass(nameNode, c.lang, c.src)
	c.addDraft(loc, language.ReferenceDraft{
		Name:          lastName(nameNode, c.lang, c.src),
		Kind:          string(kind),
		Container:     container,
		NameQualified: qual,
	})
}

// addDraft completes d with its location and appends it. A draft without a name
// is not a reference.
func (c *refCollector) addDraft(loc *ts.Node, d language.ReferenceDraft) {
	if d.Name == "" || loc == nil {
		return
	}
	d.Location = nodeLocation(loc, c.file)
	c.refs = append(c.refs, d)
}

// walkContainer iterates the direct children of a container node (program root
// or bracketed namespace body), dispatching declarations and relations. ns is
// the active namespace path.
func (c *refCollector) walkContainer(node *ts.Node) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Type(c.lang) {
		case "namespace_definition":
			// Every namespace declaration opens a fresh scope: the namespace
			// changes and the previous namespace's imports no longer apply.
			name := namespaceNameOf(child, c.lang, c.src)
			if body := childByType(child, c.lang, "compound_statement"); body != nil {
				outer := c.scope
				c.scope = newNameScope(name)
				c.walkContainer(body)
				c.scope = outer
			} else {
				c.scope = newNameScope(name)
			}
		case "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration":
			c.walkType(child)
		case "function_definition":
			fq := qualify(c.scope.ns, childText(child, c.lang, c.src, "name"))
			c.emitReturnType(child, fq)
			c.emitParamTypes(child, fq)
			if body := childByType(child, c.lang, "compound_statement"); body != nil {
				c.walkBody(body, fq, "", phpFunctionTypeEnv(child, c.lang, c.src, c.scope, nil))
			}
		case "namespace_use_declaration":
			// Not a reference. It extends the import table of the current
			// namespace scope from here on (a use applies to the code after it).
			classImports(child, c.lang, c.src, c.scope.addClassImport)
		default:
			// Top-level script statements: references attach to no symbol
			// (container ""), so they never form a graph edge but remain
			// findable by name.
			c.walkBody(child, "", "", nil)
		}
	}
}

// walkType extracts class/interface/trait/enum relations and member references.
func (c *refCollector) walkType(node *ts.Node) {
	name := childText(node, c.lang, c.src, "name")
	if name == "" {
		return
	}
	classQual := qualify(c.scope.ns, name)
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
		case "name", "qualified_name", "relative_name":
			c.addClassName(child, child, kind, container)
		}
	}
}

// walkMembers handles trait use, member signature types, and method bodies.
func (c *refCollector) walkMembers(body *ts.Node, classQual, classBare string) {
	props := phpClassPropertyTypes(body, c.lang, c.src, c.scope)
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
				c.walkBody(mbody, mq, classBare, phpFunctionTypeEnv(member, c.lang, c.src, c.scope, props))
			}
		case "property_declaration":
			c.emitTypeChildren(member, classQual)
		case "use_declaration":
			// Trait use: the trait names are the direct name/qualified_name
			// children (adaptations live inside a nested use_list, ignored).
			for j := 0; j < member.ChildCount(); j++ {
				g := member.Child(j)
				switch g.Type(c.lang) {
				case "name", "qualified_name", "relative_name":
					c.addClassName(g, g, reference.KindUsesTrait, classQual)
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
		// The type's name is a plain, qualified (`Sub\T`, `\A\T`) or relative
		// (`namespace\T`) class name; all denote a lexically determined FQN.
		cls := firstChildOfTypes(node, c.lang, "name", "qualified_name", "relative_name")
		if cls == nil {
			return
		}
		if name := lastName(cls, c.lang, c.src); name != "" && !isRelativeType(name) {
			qual, _ := c.scope.resolveClass(cls, c.lang, c.src)
			c.addDraft(node, language.ReferenceDraft{
				Name:          name,
				Kind:          string(reference.KindTypeUse),
				Container:     container,
				NameQualified: qual,
			})
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
	receiver, receiverType, receiverQual := recv.Text(c.src), "", ""
	if phpVarName(recv, c.lang, c.src) == "this" {
		receiver = selfClass
	} else {
		receiverType, receiverQual = env.receiverType(recv, node.StartByte(), c.lang, c.src)
	}
	c.addDraft(method, language.ReferenceDraft{
		Name:                  method.Text(c.src),
		Kind:                  string(reference.KindCall),
		Container:             container,
		ReceiverExpr:          receiver,
		ReceiverType:          receiverType,
		ReceiverTypeQualified: receiverQual,
		IsCall:                true,
	})
}

func (c *refCollector) emitScopedCall(node *ts.Node, container string) {
	scope, scopeNode, member := c.scopeAndMember(node)
	if member == nil {
		return // dynamic member — not fabricated
	}
	c.addMemberAccess(member, scope, scopeNode, reference.KindCall, container, true)
}

// addMemberAccess emits a member reference reached through a static scope
// (`Scope::member`). The scope is a type name, so — when the lexical rules give
// it exactly one identity — that identity is the receiver's type
// (ReceiverTypeQualified). The member itself is never qualified: whether it
// lives on the type, a parent or a trait is not a lexical fact.
func (c *refCollector) addMemberAccess(member *ts.Node, scope string, scopeNode *ts.Node, kind reference.ReferenceKind, container string, isCall bool) {
	qual := ""
	if scopeNode != nil {
		qual, _ = c.scope.resolveClass(scopeNode, c.lang, c.src)
	}
	c.addDraft(member, language.ReferenceDraft{
		Name:                  member.Text(c.src),
		Kind:                  string(kind),
		Container:             container,
		ReceiverExpr:          scope,
		ReceiverTypeQualified: qual,
		IsCall:                isCall,
	})
}

func (c *refCollector) emitConstruction(node *ts.Node, container string) {
	cls := firstChildOfTypes(node, c.lang, "name", "qualified_name", "relative_name")
	if cls == nil {
		return // `new $cls()` or `new self()` — not fabricated
	}
	c.addClassName(cls, cls, reference.KindConstruction, container)
}

func (c *refCollector) emitConstAccess(node *ts.Node, container string) {
	scope, scopeNode, member := c.scopeAndMember(node)
	if member == nil {
		return
	}
	// Class constant / enum case access is a read, not a call.
	c.addMemberAccess(member, scope, scopeNode, reference.KindRead, container, false)
}

// scopeAndMember parses a scoped_call_expression or
// class_constant_access_expression into (receiver class name, scope node, member
// node). The receiver and scope node are empty for relative scopes
// (self/static/parent). member is nil when the member is dynamic.
func (c *refCollector) scopeAndMember(node *ts.Node) (receiver string, scope, member *ts.Node) {
	seenScope := false
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Type(c.lang) {
		case "name", "qualified_name", "relative_name":
			if !seenScope {
				receiver = lastName(child, c.lang, c.src)
				scope = child
				seenScope = true
			} else if member == nil && child.Type(c.lang) == "name" {
				member = child
			}
		case "relative_scope":
			// self/static/parent — do not guess the class.
			seenScope = true
		}
	}
	return receiver, scope, member
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
