package syntax

import (
	"errors"
	"unicode"

	ts "github.com/odvcencio/gotreesitter"
)

var (
	ErrUnsupportedLanguage = errors.New("unsupported syntax language")
	ErrParseFailed         = errors.New("failed to parse source")
)

// ParseResult holds the result of parsing a source file
type ParseResult struct {
	Tree     *ts.Tree
	Language *ts.Language
	Source   []byte
	Lang     SupportedLanguage
}

// Release releases resources associated with the parse result
func (r *ParseResult) Release() {
	if r.Tree != nil {
		r.Tree.Release()
	}
}

// ParseFile parses source code and returns a parse result
func ParseFile(filename string, source []byte) (*ParseResult, error) {
	lang := DetectLanguage(filename)
	if lang == LangUnknown {
		return nil, ErrUnsupportedLanguage
	}
	return Parse(lang, source)
}

// Parse parses source code with a specific language
func Parse(lang SupportedLanguage, source []byte) (*ParseResult, error) {
	tsLang := GetLanguage(lang)
	if tsLang == nil {
		return nil, ErrUnsupportedLanguage
	}

	parser := ts.NewParser(tsLang)
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, ErrParseFailed
	}

	return &ParseResult{
		Tree:     tree,
		Language: tsLang,
		Source:   source,
		Lang:     lang,
	}, nil
}

// ExtractSymbols extracts all symbols from a parse result
func ExtractSymbols(result *ParseResult) []Symbol {
	if result == nil || result.Tree == nil {
		return nil
	}

	root := result.Tree.RootNode()
	var symbols []Symbol

	switch result.Lang {
	case LangGo:
		symbols = extractGoSymbols(root, result.Language, result.Source)
	case LangTypeScript, LangTSX:
		symbols = extractTypeScriptSymbols(root, result.Language, result.Source)
	case LangJavaScript:
		symbols = extractJavaScriptSymbols(root, result.Language, result.Source)
	case LangPython:
		symbols = extractPythonSymbols(root, result.Language, result.Source)
	}

	return symbols
}

// ExtractSymbolsFromFile is a convenience function that parses and extracts symbols
func ExtractSymbolsFromFile(filename string, source []byte) (*FileSymbols, error) {
	result, err := ParseFile(filename, source)
	if err != nil {
		return nil, err
	}
	defer result.Release()

	symbols := ExtractSymbols(result)
	return &FileSymbols{
		Path:     filename,
		Language: string(result.Lang),
		Symbols:  symbols,
	}, nil
}

// extractGoSymbols extracts symbols from Go source
func extractGoSymbols(root *ts.Node, lang *ts.Language, source []byte) []Symbol {
	var symbols []Symbol

	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		nodeType := child.Type(lang)

		switch nodeType {
		case "function_declaration":
			if sym := extractGoFunction(child, lang, source); sym != nil {
				symbols = append(symbols, *sym)
			}
		case "method_declaration":
			if sym := extractGoMethod(child, lang, source); sym != nil {
				symbols = append(symbols, *sym)
			}
		case "type_declaration":
			symbols = append(symbols, extractGoTypeDeclaration(child, lang, source)...)
		case "const_declaration":
			symbols = append(symbols, extractGoConstDeclaration(child, lang, source)...)
		case "var_declaration":
			symbols = append(symbols, extractGoVarDeclaration(child, lang, source)...)
		}
	}

	return symbols
}

func extractGoFunction(node *ts.Node, lang *ts.Language, source []byte) *Symbol {
	var name string
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "identifier" {
			name = child.Text(source)
			break
		}
	}
	if name == "" {
		return nil
	}

	return &Symbol{
		Name:      name,
		Kind:      SymbolFunction,
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		StartCol:  node.StartPoint().Column + 1,
		EndCol:    node.EndPoint().Column + 1,
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Exported:  isGoExported(name),
	}
}

func extractGoMethod(node *ts.Node, lang *ts.Language, source []byte) *Symbol {
	var name string
	var receiver string

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		childType := child.Type(lang)

		if childType == "parameter_list" && receiver == "" {
			// First parameter_list is the receiver
			receiver = extractGoReceiverType(child, lang, source)
		} else if childType == "field_identifier" || childType == "identifier" {
			if name == "" {
				name = child.Text(source)
			}
		}
	}

	if name == "" {
		return nil
	}

	return &Symbol{
		Name:      name,
		Kind:      SymbolMethod,
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		StartCol:  node.StartPoint().Column + 1,
		EndCol:    node.EndPoint().Column + 1,
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Receiver:  receiver,
		Exported:  isGoExported(name),
	}
}

func extractGoReceiverType(node *ts.Node, lang *ts.Language, source []byte) string {
	// Traverse to find the type identifier in the receiver
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		childType := child.Type(lang)

		if childType == "parameter_declaration" {
			for j := 0; j < child.ChildCount(); j++ {
				grandchild := child.Child(j)
				gcType := grandchild.Type(lang)
				if gcType == "type_identifier" {
					return grandchild.Text(source)
				} else if gcType == "pointer_type" {
					// Handle *Type
					for k := 0; k < grandchild.ChildCount(); k++ {
						ggc := grandchild.Child(k)
						if ggc.Type(lang) == "type_identifier" {
							return ggc.Text(source)
						}
					}
				}
			}
		}
	}
	return ""
}

func extractGoTypeDeclaration(node *ts.Node, lang *ts.Language, source []byte) []Symbol {
	var symbols []Symbol

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		childType := child.Type(lang)

		if childType == "type_spec" {
			if sym := extractGoTypeSpec(child, lang, source); sym != nil {
				symbols = append(symbols, *sym)
			}
		}
	}

	return symbols
}

func extractGoTypeSpec(node *ts.Node, lang *ts.Language, source []byte) *Symbol {
	var name string
	var kind SymbolKind = SymbolType

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		childType := child.Type(lang)

		if childType == "type_identifier" && name == "" {
			name = child.Text(source)
		} else if childType == "struct_type" {
			kind = SymbolStruct
		} else if childType == "interface_type" {
			kind = SymbolInterface
		}
	}

	if name == "" {
		return nil
	}

	return &Symbol{
		Name:      name,
		Kind:      kind,
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		StartCol:  node.StartPoint().Column + 1,
		EndCol:    node.EndPoint().Column + 1,
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Exported:  isGoExported(name),
	}
}

func extractGoConstDeclaration(node *ts.Node, lang *ts.Language, source []byte) []Symbol {
	var symbols []Symbol

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "const_spec" {
			for j := 0; j < child.ChildCount(); j++ {
				grandchild := child.Child(j)
				if grandchild.Type(lang) == "identifier" {
					name := grandchild.Text(source)
					symbols = append(symbols, Symbol{
						Name:      name,
						Kind:      SymbolConstant,
						StartLine: child.StartPoint().Row + 1,
						EndLine:   child.EndPoint().Row + 1,
						StartCol:  child.StartPoint().Column + 1,
						EndCol:    child.EndPoint().Column + 1,
						StartByte: child.StartByte(),
						EndByte:   child.EndByte(),
						Exported:  isGoExported(name),
					})
					break
				}
			}
		}
	}

	return symbols
}

func extractGoVarDeclaration(node *ts.Node, lang *ts.Language, source []byte) []Symbol {
	var symbols []Symbol

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "var_spec" {
			for j := 0; j < child.ChildCount(); j++ {
				grandchild := child.Child(j)
				if grandchild.Type(lang) == "identifier" {
					name := grandchild.Text(source)
					symbols = append(symbols, Symbol{
						Name:      name,
						Kind:      SymbolVariable,
						StartLine: child.StartPoint().Row + 1,
						EndLine:   child.EndPoint().Row + 1,
						StartCol:  child.StartPoint().Column + 1,
						EndCol:    child.EndPoint().Column + 1,
						StartByte: child.StartByte(),
						EndByte:   child.EndByte(),
						Exported:  isGoExported(name),
					})
					break
				}
			}
		}
	}

	return symbols
}

func isGoExported(name string) bool {
	if name == "" {
		return false
	}
	r := []rune(name)
	return unicode.IsUpper(r[0])
}

// extractTypeScriptSymbols extracts symbols from TypeScript/TSX source
func extractTypeScriptSymbols(root *ts.Node, lang *ts.Language, source []byte) []Symbol {
	var symbols []Symbol

	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		nodeType := child.Type(lang)

		switch nodeType {
		case "function_declaration":
			if sym := extractTSFunction(child, lang, source); sym != nil {
				symbols = append(symbols, *sym)
			}
		case "class_declaration":
			if sym := extractTSClass(child, lang, source); sym != nil {
				symbols = append(symbols, *sym)
			}
		case "interface_declaration":
			if sym := extractTSInterface(child, lang, source); sym != nil {
				symbols = append(symbols, *sym)
			}
		case "type_alias_declaration":
			if sym := extractTSTypeAlias(child, lang, source); sym != nil {
				symbols = append(symbols, *sym)
			}
		case "lexical_declaration":
			symbols = append(symbols, extractTSLexicalDeclaration(child, lang, source)...)
		case "export_statement":
			// Handle exported declarations
			for j := 0; j < child.ChildCount(); j++ {
				inner := child.Child(j)
				innerType := inner.Type(lang)
				switch innerType {
				case "function_declaration":
					if sym := extractTSFunction(inner, lang, source); sym != nil {
						sym.Exported = true
						symbols = append(symbols, *sym)
					}
				case "class_declaration":
					if sym := extractTSClass(inner, lang, source); sym != nil {
						sym.Exported = true
						symbols = append(symbols, *sym)
					}
				case "interface_declaration":
					if sym := extractTSInterface(inner, lang, source); sym != nil {
						sym.Exported = true
						symbols = append(symbols, *sym)
					}
				case "type_alias_declaration":
					if sym := extractTSTypeAlias(inner, lang, source); sym != nil {
						sym.Exported = true
						symbols = append(symbols, *sym)
					}
				case "lexical_declaration":
					for _, s := range extractTSLexicalDeclaration(inner, lang, source) {
						s.Exported = true
						symbols = append(symbols, s)
					}
				}
			}
		}
	}

	return symbols
}

func extractTSFunction(node *ts.Node, lang *ts.Language, source []byte) *Symbol {
	var name string
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "identifier" {
			name = child.Text(source)
			break
		}
	}
	if name == "" {
		return nil
	}

	return &Symbol{
		Name:      name,
		Kind:      SymbolFunction,
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		StartCol:  node.StartPoint().Column + 1,
		EndCol:    node.EndPoint().Column + 1,
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
	}
}

func extractTSClass(node *ts.Node, lang *ts.Language, source []byte) *Symbol {
	var name string
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "type_identifier" {
			name = child.Text(source)
			break
		}
	}
	if name == "" {
		return nil
	}

	return &Symbol{
		Name:      name,
		Kind:      SymbolClass,
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		StartCol:  node.StartPoint().Column + 1,
		EndCol:    node.EndPoint().Column + 1,
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
	}
}

func extractTSInterface(node *ts.Node, lang *ts.Language, source []byte) *Symbol {
	var name string
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "type_identifier" {
			name = child.Text(source)
			break
		}
	}
	if name == "" {
		return nil
	}

	return &Symbol{
		Name:      name,
		Kind:      SymbolInterface,
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		StartCol:  node.StartPoint().Column + 1,
		EndCol:    node.EndPoint().Column + 1,
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
	}
}

func extractTSTypeAlias(node *ts.Node, lang *ts.Language, source []byte) *Symbol {
	var name string
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "type_identifier" {
			name = child.Text(source)
			break
		}
	}
	if name == "" {
		return nil
	}

	return &Symbol{
		Name:      name,
		Kind:      SymbolTypeAlias,
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		StartCol:  node.StartPoint().Column + 1,
		EndCol:    node.EndPoint().Column + 1,
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
	}
}

func extractTSLexicalDeclaration(node *ts.Node, lang *ts.Language, source []byte) []Symbol {
	var symbols []Symbol
	isConst := false

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		childType := child.Type(lang)

		if childType == "const" {
			isConst = true
		} else if childType == "variable_declarator" {
			for j := 0; j < child.ChildCount(); j++ {
				grandchild := child.Child(j)
				if grandchild.Type(lang) == "identifier" {
					name := grandchild.Text(source)
					kind := SymbolVariable
					if isConst {
						kind = SymbolConstant
					}
					symbols = append(symbols, Symbol{
						Name:      name,
						Kind:      kind,
						StartLine: child.StartPoint().Row + 1,
						EndLine:   child.EndPoint().Row + 1,
						StartCol:  child.StartPoint().Column + 1,
						EndCol:    child.EndPoint().Column + 1,
						StartByte: child.StartByte(),
						EndByte:   child.EndByte(),
					})
					break
				}
			}
		}
	}

	return symbols
}

// extractJavaScriptSymbols extracts symbols from JavaScript source
func extractJavaScriptSymbols(root *ts.Node, lang *ts.Language, source []byte) []Symbol {
	// JavaScript uses same extraction as TypeScript (subset)
	return extractTypeScriptSymbols(root, lang, source)
}

// extractPythonSymbols extracts symbols from Python source
func extractPythonSymbols(root *ts.Node, lang *ts.Language, source []byte) []Symbol {
	var symbols []Symbol

	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		nodeType := child.Type(lang)

		switch nodeType {
		case "function_definition":
			if sym := extractPythonFunction(child, lang, source); sym != nil {
				symbols = append(symbols, *sym)
			}
		case "class_definition":
			if sym := extractPythonClass(child, lang, source); sym != nil {
				symbols = append(symbols, *sym)
			}
		case "decorated_definition":
			// Handle decorated functions/classes
			for j := 0; j < child.ChildCount(); j++ {
				inner := child.Child(j)
				innerType := inner.Type(lang)
				if innerType == "function_definition" {
					if sym := extractPythonFunction(inner, lang, source); sym != nil {
						symbols = append(symbols, *sym)
					}
				} else if innerType == "class_definition" {
					if sym := extractPythonClass(inner, lang, source); sym != nil {
						symbols = append(symbols, *sym)
					}
				}
			}
		}
	}

	return symbols
}

func extractPythonFunction(node *ts.Node, lang *ts.Language, source []byte) *Symbol {
	var name string
	var kind SymbolKind = SymbolFunction

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		childType := child.Type(lang)

		if childType == "identifier" && name == "" {
			name = child.Text(source)
		}
	}

	// Check if it's async
	firstChild := node.Child(0)
	if firstChild != nil && firstChild.Type(lang) == "async" {
		kind = SymbolFunction // Still a function, just async
	}

	if name == "" {
		return nil
	}

	return &Symbol{
		Name:      name,
		Kind:      kind,
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		StartCol:  node.StartPoint().Column + 1,
		EndCol:    node.EndPoint().Column + 1,
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Exported:  !isPythonPrivate(name),
	}
}

func extractPythonClass(node *ts.Node, lang *ts.Language, source []byte) *Symbol {
	var name string

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "identifier" {
			name = child.Text(source)
			break
		}
	}

	if name == "" {
		return nil
	}

	return &Symbol{
		Name:      name,
		Kind:      SymbolClass,
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		StartCol:  node.StartPoint().Column + 1,
		EndCol:    node.EndPoint().Column + 1,
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Exported:  !isPythonPrivate(name),
	}
}

func isPythonPrivate(name string) bool {
	// Python convention: names starting with _ are private
	return len(name) > 0 && name[0] == '_'
}
