package php

import (
	"strings"
	"unicode/utf8"

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

	// class is the class whose member bodies are being walked (nil outside a
	// class_declaration, including in traits, where self/static denote the
	// using class).
	class *classScope
}

// classScope is the lexical identity of the enclosing class and of the class
// it extends ("" when it extends none or the name has no single identity).
type classScope struct {
	qual, bare             string
	final                  bool
	parentQual, parentBare string
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
		c.walkMembers(node, body, classQual, name)
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
func (c *refCollector) walkMembers(class, body *ts.Node, classQual, classBare string) {
	props := phpClassPropertyTypes(class, body, c.lang, c.src, c.scope)
	outer := c.class
	c.class = nil
	if class.Type(c.lang) == "class_declaration" {
		c.class = &classScope{qual: classQual, bare: classBare, final: childByType(class, c.lang, "final_modifier") != nil}
		if base := childByType(class, c.lang, "base_clause"); base != nil {
			if p := firstChildOfTypes(base, c.lang, "name", "qualified_name", "relative_name"); p != nil {
				c.class.parentBare = lastName(p, c.lang, c.src)
				c.class.parentQual, _ = c.scope.resolveClass(p, c.lang, c.src)
			}
		}
	}
	defer func() { c.class = outer }()
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
				case "use_list":
					c.emitTraitAdaptations(g, classQual)
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
	case "member_call_expression", "nullsafe_member_call_expression":
		c.emitMemberCall(node, container, selfClass, env)
	case "scoped_call_expression":
		c.emitScopedCall(node, container, env)
	case "object_creation_expression":
		c.emitConstruction(node, container, env)
	case "class_constant_access_expression":
		c.emitConstAccess(node, container, env)
	}
	for i := 0; i < node.ChildCount(); i++ {
		c.walkBody(node.Child(i), container, selfClass, env)
	}
}

func (c *refCollector) emitFunctionCall(node *ts.Node, container string) {
	callee := firstChildOfTypes(node, c.lang, "name", "qualified_name")
	if callee == nil {
		// $fn(), $arr[0](), (fn() => 1)(): a call whose callee is computed.
		if first := node.Child(0); first != nil && first.Type(c.lang) != "arguments" && first.Type(c.lang) != "relative_name" {
			c.addDynamic(first, first, reference.KindCall, container, "")
		}
		return
	}
	c.add(callee, lastName(callee, c.lang, c.src), reference.KindCall, container, "", true)
}

// maxDynamicNameLen bounds the display text of a Dynamic reference's name.
const maxDynamicNameLen = 64

// addDynamic emits a call or construction whose name is computed at run time
// (language.ReferenceDraft.Dynamic): the syntax proves the call exists, the
// name expression (text of nameNode, shortened) is display text only. loc is
// the node whose position is the reference's.
func (c *refCollector) addDynamic(loc, nameNode *ts.Node, kind reference.ReferenceKind, container, receiver string) {
	if hasErrorChild(nameNode.Parent()) || nameNode.HasError() {
		return // a parse the grammar recovered from: no call is proven
	}
	name := strings.Join(strings.Fields(nameNode.Text(c.src)), " ")
	if len(name) > maxDynamicNameLen {
		cut := maxDynamicNameLen
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut] + "…"
	}
	c.addDraft(loc, language.ReferenceDraft{
		Name:         name,
		Kind:         string(kind),
		Container:    container,
		ReceiverExpr: receiver,
		IsCall:       kind == reference.KindCall,
		Dynamic:      true,
	})
}

func (c *refCollector) emitMemberCall(node *ts.Node, container, selfClass string, env *phpTypeEnv) {
	recv := node.Child(0)
	if recv == nil {
		return
	}
	method := childByType(node, c.lang, "name")
	if method == nil {
		// $obj->$m(), $obj->{$expr}(): the method name is computed.
		if dyn := memberNameExpr(node, c.lang); dyn != nil {
			receiver := recv.Text(c.src)
			if phpVarName(recv, c.lang, c.src) == "this" {
				receiver = selfClass
			}
			c.addDynamic(dyn, dyn, reference.KindCall, container, receiver)
		}
		return
	}
	// `$this` is the enclosing class (an explicit type receiver). Every other
	// receiver is recorded verbatim — "" is reserved for receiverless names —
	// with declared-type evidence only when it is proven (receiver_types.go).
	receiver := recv.Text(c.src)
	var typ phpTypeRef
	if phpVarName(recv, c.lang, c.src) == "this" {
		receiver = selfClass
		// Inside a class body $this is an instance of that class (in a trait
		// it is the using class, which is not known here).
		if c.class != nil {
			typ.qual = c.class.qual
		}
	} else {
		typ = env.receiverType(recv, node.StartByte(), c.lang, c.src)
	}
	d := language.ReferenceDraft{
		Name:                  method.Text(c.src),
		Kind:                  string(reference.KindCall),
		Container:             container,
		ReceiverExpr:          receiver,
		ReceiverType:          typ.name,
		ReceiverTypeQualified: typ.qual,
		IsCall:                true,
	}
	if typ.capped && typ.qual != "" {
		d.ConfidenceCap = "strong"
	}
	c.addDraft(method, d)
}

func (c *refCollector) emitScopedCall(node *ts.Node, container string, env *phpTypeEnv) {
	if c.emitExpressionScoped(node, reference.KindCall, container, env) {
		return
	}
	scope, scopeNode, member, rel := c.scopeAndMember(node)
	if member == nil {
		// Foo::$m(), Foo::{'m'}(): the member name is computed.
		if dyn := memberNameExpr(node, c.lang); dyn != nil {
			if scope == "" {
				scope = rel
			}
			c.addDynamic(dyn, dyn, reference.KindCall, container, scope)
		}
		return
	}
	if c.addRelativeMemberAccess(member, rel, reference.KindCall, container, true) {
		return
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

func (c *refCollector) emitConstruction(node *ts.Node, container string, env *phpTypeEnv) {
	if childByType(node, c.lang, "anonymous_class") != nil {
		return // `new class {...}`: the class is declared right here
	}
	cls := firstChildOfTypes(node, c.lang, "name", "qualified_name", "relative_name")
	if cls == nil {
		// `new $cls()`, `new ($expr)`: the class is computed — unless $cls is
		// a variable proven to hold one class-string (receiver_types.go).
		expr := node.Child(1)
		if expr == nil || expr.Type(c.lang) == "arguments" {
			return
		}
		if typ := env.classStringType(expr, node.StartByte(), c.lang, c.src); typ.qual != "" {
			c.addDraft(expr, language.ReferenceDraft{
				Name:          typ.name,
				Kind:          string(reference.KindConstruction),
				Container:     container,
				NameQualified: typ.qual,
			})
			return
		}
		c.addDynamic(expr, expr, reference.KindConstruction, container, "")
		return
	}
	if cls.Type(c.lang) == "name" && isRelativeType(cls.Text(c.src)) {
		c.emitRelativeConstruction(cls, container)
		return
	}
	c.addClassName(cls, cls, reference.KindConstruction, container)
}

// emitRelativeConstruction emits `new self` / `new static` / `new parent`
// inside a class body as a construction of the class it denotes: self is the
// enclosing class, parent the class it extends (both exact). static is the
// class of the call at run time: any subclass that merely inherits the
// calling method constructs itself, not the enclosing class, so `new static`
// names a class only when the enclosing class is final (then it is self).
// Elsewhere (traits, free code) the class is not lexically known; the name is
// emitted as written and names no class.
func (c *refCollector) emitRelativeConstruction(cls *ts.Node, container string) {
	rel := cls.Text(c.src)
	d := language.ReferenceDraft{Kind: string(reference.KindConstruction), Container: container}
	if c.class != nil {
		switch {
		case rel == "self", rel == "static" && c.class.final:
			d.Name, d.NameQualified = c.class.bare, c.class.qual
		case rel == "parent":
			d.Name, d.NameQualified = c.class.parentBare, c.class.parentQual
		}
	}
	if d.NameQualified == "" {
		d.Name = rel
	}
	c.addDraft(cls, d)
}

// emitExpressionScoped handles a scoped access whose scope is an expression
// (`$cls::m()`, `$cls::CONST`) rather than a class name. With a variable proven
// to hold one class-string the scope is that class; otherwise the member name
// is still fixed and the access is emitted with the expression as an untyped
// receiver. It reports false when the scope is a class name.
func (c *refCollector) emitExpressionScoped(node *ts.Node, kind reference.ReferenceKind, container string, env *phpTypeEnv) bool {
	scope := node.Child(0)
	if scope == nil {
		return false
	}
	switch scope.Type(c.lang) {
	case "name", "qualified_name", "relative_name", "relative_scope":
		return false
	case "variable_name", "member_access_expression", "nullsafe_member_access_expression",
		"scoped_property_access_expression", "subscript_expression", "parenthesized_expression",
		"function_call_expression", "member_call_expression", "scoped_call_expression":
	default:
		return true // not an expression a class can come from (e.g. error recovery)
	}
	if hasErrorChild(node) {
		return true
	}
	member := memberNameExpr(node, c.lang)
	if member == nil {
		return true
	}
	isCall := kind == reference.KindCall
	if member.Type(c.lang) != "name" {
		if isCall {
			c.addDynamic(member, member, kind, container, scope.Text(c.src))
		}
		return true
	}
	if member.Text(c.src) == "class" {
		return true // $obj::class: the run-time class of a value
	}
	d := language.ReferenceDraft{
		Name:         member.Text(c.src),
		Kind:         string(kind),
		Container:    container,
		ReceiverExpr: scope.Text(c.src),
		IsCall:       isCall,
	}
	if typ := env.classStringType(scope, node.StartByte(), c.lang, c.src); typ.qual != "" {
		d.ReceiverExpr, d.ReceiverTypeQualified = typ.name, typ.qual
	}
	c.addDraft(member, d)
	return true
}

// hasErrorChild reports whether a direct child of n is an error or missing
// node: the grammar recovered from broken syntax there, so the node's shape
// proves nothing.
func hasErrorChild(n *ts.Node) bool {
	if n == nil {
		return false
	}
	for i := 0; i < n.ChildCount(); i++ {
		if ch := n.Child(i); ch.IsError() || ch.IsMissing() {
			return true
		}
	}
	return false
}

// memberNameExpr returns the member-name part of a member or scoped access:
// the node after the `->`, `?->` or `::` token (a name, a variable, or the
// expression inside `{...}`), or nil.
func memberNameExpr(node *ts.Node, lang *ts.Language) *ts.Node {
	for i := 0; i+1 < node.ChildCount(); i++ {
		switch node.Child(i).Type(lang) {
		case "->", "?->", "::":
			next := node.Child(i + 1)
			if next.Type(lang) == "{" && i+2 < node.ChildCount() {
				next = node.Child(i + 2)
			}
			if next.Type(lang) == "arguments" {
				return nil
			}
			return next
		}
	}
	return nil
}

func (c *refCollector) emitConstAccess(node *ts.Node, container string, env *phpTypeEnv) {
	if c.emitExpressionScoped(node, reference.KindRead, container, env) {
		return
	}
	scope, scopeNode, member, rel := c.scopeAndMember(node)
	if member == nil {
		return
	}
	if member.Text(c.src) == "class" {
		c.emitClassString(scopeNode, member, rel, container)
		return
	}
	if c.addRelativeMemberAccess(member, rel, reference.KindRead, container, false) {
		return
	}
	// Class constant / enum case access is a read, not a call.
	c.addMemberAccess(member, scope, scopeNode, reference.KindRead, container, false)
}

// emitClassString emits `Foo::class` — a compile-time string naming the class
// Foo, resolved by the same lexical rules as any class name — as a type use of
// that class. It names the class; it neither calls nor constructs it, so what
// a consumer (a container, a factory) later does with the string is not this
// reference. `self::class` names the enclosing class; static::class and
// parent::class are not emitted (static is a run-time class; parent::class is
// a name only).
func (c *refCollector) emitClassString(scopeNode, member *ts.Node, rel, container string) {
	switch {
	case scopeNode != nil:
		if isRelativeType(lastName(scopeNode, c.lang, c.src)) {
			return
		}
		c.addClassName(scopeNode, scopeNode, reference.KindTypeUse, container)
	case rel == "self" && c.class != nil:
		c.addDraft(member, language.ReferenceDraft{
			Name:          c.class.bare,
			Kind:          string(reference.KindTypeUse),
			Container:     container,
			NameQualified: c.class.qual,
		})
	}
}

// scopeAndMember parses a scoped_call_expression or
// class_constant_access_expression into (receiver class name, scope node, member
// node). The receiver and scope node are empty for relative scopes
// (self/static/parent). member is nil when the member is dynamic.
func (c *refCollector) scopeAndMember(node *ts.Node) (receiver string, scope, member *ts.Node, relative string) {
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
			// self/static/parent — reported as relative; the class is never
			// guessed from it here (see addRelativeMemberAccess).
			relative = child.Text(c.src)
			seenScope = true
		}
	}
	return receiver, scope, member, relative
}

// addRelativeMemberAccess emits `self::m` / `static::m` / `parent::m` inside a
// class body as a member access on a known class: self is the lexically
// enclosing class, so its identity is exact; parent is the class it extends.
// static is the class of the call at run time — the enclosing class or a
// subclass that may override m — so unless the class is final the reference is
// capped at Strong. Where the member is declared (the class, a trait, an
// ancestor) is the resolver's member lookup. Relative scopes outside a class
// (or parent:: without a known parent) are not handled: it reports false and
// the caller emits the access as before.
func (c *refCollector) addRelativeMemberAccess(member *ts.Node, relative string, kind reference.ReferenceKind, container string, isCall bool) bool {
	if c.class == nil {
		return false
	}
	recv, qual := c.class.bare, c.class.qual
	switch relative {
	case "self", "static":
	case "parent":
		// parent:: starts the member lookup at the extended class, whatever
		// the enclosing class declares.
		if c.class.parentQual == "" {
			return false
		}
		recv, qual = c.class.parentBare, c.class.parentQual
	default:
		return false
	}
	d := language.ReferenceDraft{
		Name:                  member.Text(c.src),
		Kind:                  string(kind),
		Container:             container,
		ReceiverExpr:          recv,
		ReceiverTypeQualified: qual,
		IsCall:                isCall,
	}
	if relative == "static" && !c.class.final {
		d.ConfidenceCap = "strong"
	}
	c.addDraft(member, d)
	return true
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

// emitTraitAdaptations records every member name mentioned in a trait
// adaptation block (`use A, B { A::foo insteadof B; foo as bar; }`): the
// method being adapted and any alias. They tell member lookup that, for these
// names, the class's imported members are not simply its traits' members.
func (c *refCollector) emitTraitAdaptations(list *ts.Node, classQual string) {
	for i := 0; i < list.ChildCount(); i++ {
		clause := list.Child(i)
		switch clause.Type(c.lang) {
		case "use_instead_of_clause", "use_as_clause":
		default:
			continue
		}
		for j := 0; j < clause.ChildCount(); j++ {
			part := clause.Child(j)
			var name *ts.Node
			switch part.Type(c.lang) {
			case "class_constant_access_expression":
				_, _, name, _ = c.scopeAndMember(part) // Trait::method
			case "name":
				name = part // a bare method name, an alias or an excluded trait
			}
			if name == nil {
				continue
			}
			if clause.Type(c.lang) == "use_instead_of_clause" && part.Type(c.lang) == "name" {
				continue // the excluded trait, not a member
			}
			c.addDraft(name, language.ReferenceDraft{
				Name:      name.Text(c.src),
				Kind:      string(reference.KindTraitAdaptation),
				Container: classQual,
			})
		}
	}
}
