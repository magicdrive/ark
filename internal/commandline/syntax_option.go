package commandline

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

type SyntaxOption struct {
	FilePath    string
	Lang        string
	FormatValue string
	Format      string
	HelpFlag    bool
	FlagSet     *flag.FlagSet
}

type SymbolOption struct {
	FilePath    string
	Lang        string
	FormatValue string
	Format      string
	HelpFlag    bool
	FlagSet     *flag.FlagSet
}

func SyntaxOptParse(args []string) (int, *SyntaxOption, error) {
	optLength := len(args)

	fs := flag.NewFlagSet("syntax", flag.ExitOnError)

	// --lang
	langOpt := fs.String("lang", "", "Specify language (go, typescript, tsx, javascript, python).")

	// --format
	formatOpt := fs.String("format", "text", "Specify output format.")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark syntax --help")
	}

	if err := fs.Parse(args); err != nil {
		return optLength, nil, err
	}

	filePath := ""
	if fs.NArg() > 0 {
		filePath = fs.Arg(0)
	}

	result := &SyntaxOption{
		FilePath:    filePath,
		Lang:        *langOpt,
		FormatValue: *formatOpt,
		HelpFlag:    *helpFlagOpt,
		FlagSet:     fs,
	}

	OverRideHelp(fs)

	if err := result.Normalize(); err != nil {
		return optLength, nil, err
	}

	return optLength, result, nil
}

func (cr *SyntaxOption) Normalize() error {
	var errorMessages []string

	switch cr.FormatValue {
	case "text", "json":
		cr.Format = cr.FormatValue
	default:
		errorMessages = append(errorMessages, fmt.Sprintf("--format invalid value: %q. Allowed values are 'text', 'json'", cr.FormatValue))
	}

	if len(errorMessages) == 0 {
		return nil
	}
	return errors.New(strings.Join(errorMessages, "\n"))
}

func SymbolOptParse(args []string) (int, *SymbolOption, error) {
	optLength := len(args)

	fs := flag.NewFlagSet("symbol", flag.ExitOnError)

	// --lang
	langOpt := fs.String("lang", "", "Specify language (go, typescript, tsx, javascript, python).")

	// --format
	formatOpt := fs.String("format", "text", "Specify output format.")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark symbol --help")
	}

	if err := fs.Parse(args); err != nil {
		return optLength, nil, err
	}

	filePath := ""
	if fs.NArg() > 0 {
		filePath = fs.Arg(0)
	}

	result := &SymbolOption{
		FilePath:    filePath,
		Lang:        *langOpt,
		FormatValue: *formatOpt,
		HelpFlag:    *helpFlagOpt,
		FlagSet:     fs,
	}

	OverRideHelp(fs)

	if err := result.Normalize(); err != nil {
		return optLength, nil, err
	}

	return optLength, result, nil
}

func (cr *SymbolOption) Normalize() error {
	var errorMessages []string

	switch cr.FormatValue {
	case "text", "json":
		cr.Format = cr.FormatValue
	default:
		errorMessages = append(errorMessages, fmt.Sprintf("--format invalid value: %q. Allowed values are 'text', 'json'", cr.FormatValue))
	}

	if len(errorMessages) == 0 {
		return nil
	}
	return errors.New(strings.Join(errorMessages, "\n"))
}
