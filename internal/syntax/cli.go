package syntax

import (
	"encoding/json"
	"fmt"
	"os"

	ts "github.com/odvcencio/gotreesitter"
)

// SyntaxOptions holds options for the syntax command
type SyntaxOptions struct {
	FilePath string
	Lang     string
	Format   string // "text" (S-expression) or "json"
}

// SymbolOptions holds options for the symbol command
type SymbolOptions struct {
	FilePath string
	Lang     string
	Format   string // "text" or "json"
}

// RunSyntaxCommand executes the syntax subcommand
func RunSyntaxCommand(opts SyntaxOptions) error {
	// Read file
	source, err := os.ReadFile(opts.FilePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Determine language
	var lang SupportedLanguage
	if opts.Lang != "" {
		lang = SupportedLanguage(opts.Lang)
		if !IsSupported(lang) {
			return fmt.Errorf("unsupported language: %s", opts.Lang)
		}
	} else {
		lang = DetectLanguage(opts.FilePath)
		if lang == LangUnknown {
			return ErrUnsupportedLanguage
		}
	}

	// Parse
	result, err := Parse(lang, source)
	if err != nil {
		return fmt.Errorf("failed to parse: %w", err)
	}
	defer result.Release()

	// Output
	switch opts.Format {
	case "json":
		return outputSyntaxJSON(result)
	default:
		return outputSyntaxSExpr(result)
	}
}

// RunSymbolCommand executes the symbol subcommand
func RunSymbolCommand(opts SymbolOptions) error {
	// Read file
	source, err := os.ReadFile(opts.FilePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Determine language
	var lang SupportedLanguage
	if opts.Lang != "" {
		lang = SupportedLanguage(opts.Lang)
		if !IsSupported(lang) {
			return fmt.Errorf("unsupported language: %s", opts.Lang)
		}
	} else {
		lang = DetectLanguage(opts.FilePath)
		if lang == LangUnknown {
			return ErrUnsupportedLanguage
		}
	}

	// Parse and extract symbols
	result, err := Parse(lang, source)
	if err != nil {
		return fmt.Errorf("failed to parse: %w", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result)

	fileSymbols := &FileSymbols{
		Path:     opts.FilePath,
		Language: string(lang),
		Symbols:  symbols,
	}

	// Output
	switch opts.Format {
	case "json":
		return outputSymbolsJSON(fileSymbols)
	default:
		return outputSymbolsText(fileSymbols)
	}
}

func outputSyntaxSExpr(result *ParseResult) error {
	root := result.Tree.RootNode()
	sexpr := root.SExpr(result.Language)
	fmt.Println(sexpr)
	return nil
}

func outputSyntaxJSON(result *ParseResult) error {
	root := result.Tree.RootNode()
	node := nodeToMap(root, result.Language, result.Source)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(node)
}

func nodeToMap(node *ts.Node, lang *ts.Language, source []byte) map[string]any {
	m := map[string]any{
		"type":      node.Type(lang),
		"startLine": node.StartPoint().Row + 1,
		"endLine":   node.EndPoint().Row + 1,
		"startCol":  node.StartPoint().Column + 1,
		"endCol":    node.EndPoint().Column + 1,
	}

	// Add text for leaf nodes
	if node.ChildCount() == 0 {
		m["text"] = node.Text(source)
	}

	// Add children
	if node.ChildCount() > 0 {
		children := make([]map[string]any, 0, node.ChildCount())
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			children = append(children, nodeToMap(child, lang, source))
		}
		m["children"] = children
	}

	return m
}

func outputSymbolsJSON(fs *FileSymbols) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(fs)
}

func outputSymbolsText(fs *FileSymbols) error {
	fmt.Printf("File: %s (%s)\n", fs.Path, fs.Language)
	fmt.Printf("Symbols: %d\n\n", len(fs.Symbols))

	for _, sym := range fs.Symbols {
		exported := ""
		if sym.Exported {
			exported = " [exported]"
		}
		receiver := ""
		if sym.Receiver != "" {
			receiver = fmt.Sprintf(" (%s)", sym.Receiver)
		}

		fmt.Printf("  %s %s%s%s (line %d-%d)\n",
			sym.Kind, sym.Name, receiver, exported,
			sym.StartLine, sym.EndLine)
	}

	return nil
}
