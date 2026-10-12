package commandline

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

// SecretsScanOption is `ark secrets-scan [OPTIONS] [dirname]`.
type SecretsScanOption struct {
	TargetDirname       string
	FormatValue         string
	ListRulesFlag       bool
	SuggestIgnoreFlag   bool
	IncludeIgnoredFlag  bool
	AllowGitignoreValue string
	AllowGitignore      bool
	HelpFlag            bool
	FlagSet             *flag.FlagSet
}

func SecretsScanOptParse(args []string) (int, *SecretsScanOption, error) {
	optLength := len(args)

	fs := flag.NewFlagSet("secrets-scan", flag.ContinueOnError)

	// --format
	formatOpt := fs.String("format", "text", "Specify output format.")
	fs.StringVar(formatOpt, "f", "text", "Specify output format.")

	// --list-rules
	listRulesOpt := fs.Bool("list-rules", false, "List the masking rules and their state.")

	// --suggest-ignore
	suggestIgnoreOpt := fs.Bool("suggest-ignore", false, "Suggest .arkignore entries and rules to review.")

	// --include-ignored
	includeIgnoredOpt := fs.Bool("include-ignored", false, "Also scan the files .arkignore and .gitignore exclude.")

	// --allow-gitignore
	allowGitignoreOpt := fs.String("allow-gitignore", "on", "Specify enable .gitignore.")
	fs.StringVar(allowGitignoreOpt, "a", "on", "Specify enable .gitignore.")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark secrets-scan --help")
	}
	if err := fs.Parse(args); err != nil {
		return optLength, nil, err
	}
	if fs.NArg() > 1 {
		return optLength, nil, fmt.Errorf("secrets-scan takes one directory, got %d arguments", fs.NArg())
	}
	result := &SecretsScanOption{
		TargetDirname:       ".",
		FormatValue:         *formatOpt,
		ListRulesFlag:       *listRulesOpt,
		SuggestIgnoreFlag:   *suggestIgnoreOpt,
		IncludeIgnoredFlag:  *includeIgnoredOpt,
		AllowGitignoreValue: *allowGitignoreOpt,
		HelpFlag:            *helpFlagOpt,
		FlagSet:             fs,
	}
	if fs.NArg() == 1 {
		result.TargetDirname = fs.Arg(0)
	}

	OverRideHelp(fs)

	switch result.FormatValue {
	case "text", "json":
	default:
		return optLength, nil, fmt.Errorf("--format %q: must be 'text' or 'json'", result.FormatValue)
	}
	switch result.AllowGitignoreValue {
	case "on":
		result.AllowGitignore = true
	case "off":
	default:
		return optLength, nil, errors.New("--allow-gitignore: must be 'on' or 'off'")
	}
	return optLength, result, nil
}
