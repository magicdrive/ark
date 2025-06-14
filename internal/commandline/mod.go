package commandline

import (
	"flag"
	"fmt"
	"os"

	_ "embed"

	"github.com/magicdrive/ark/internal/common"
)

//go:embed help.txt
var helpMessage string

func OptParse(args []string) (int, *Option, error) {

	optLength := len(args)

	fs := flag.NewFlagSet("ark", flag.ExitOnError)

	// --output-filename
	outputFilenameFlagOpt := fs.String("output-filename", "ark_output.txt", "Show help message.")
	fs.StringVar(outputFilenameFlagOpt, "o", "ark_output.txt", "Show help message.")

	// --scan-buffer
	scanBufferValueOpt := fs.String("scan-buffer", "10M", "Show help message.")
	fs.StringVar(scanBufferValueOpt, "b", "10M", "Show help message.")

	// --additionally-ignorerule
	additionallyIgnoreRuleFilenamesOpt := fs.String("additionally-ignorerule", "", "Show help message.")
	fs.StringVar(additionallyIgnoreRuleFilenamesOpt, "a", "", "Show help message.")

	// --with-line-number
	withLineNumberFlagOpt := fs.Bool("with-line-number", false, "Show help message.")
	fs.BoolVar(withLineNumberFlagOpt, "n", false, "Show help message.")

	// --additionally-ignorerule
	ignoreDotfileFlagValueOpt := fs.String("ignore-dotfile", "on", "Show help message.")
	fs.StringVar(ignoreDotfileFlagValueOpt, "d", "on", "Show help message.")

	// --pattern-regex
	patternRegexOpt := fs.String("pattern-regex", ".*", "Specify watch file pattern regexp (optional)")
	fs.StringVar(patternRegexOpt, "x", ".*", "Specify watch file pattern regexp (optional)")

	// --include-ext
	includeExtOpt := fs.String("include-ext", "", "Specify watch file extension (optional)")
	fs.StringVar(includeExtOpt, "i", "", "Specify watch file extension (optional)")

	// --exclude-file-regexp
	excludeFileRegexpOpt := fs.String("exclude-file-regex", "", "Specify watch file ignore pattern regexp (optional)")
	fs.StringVar(excludeFileRegexpOpt, "g", "", "Specify watch file ignore pattern regexp (optional)")

	// --exclude-dir-regexp
	excludeDirRegexpOpt := fs.String("exclude-dir-regex", "", "Specify watch dir ignore pattern regexp (optional)")
	fs.StringVar(excludeDirRegexpOpt, "G", "", "Specify watch file ignore pattern regexp (optional)")

	// --exclude-ext
	excludeExtOpt := fs.String("exclude-ext", "", "Specify watch exclude file extension (optional)")
	fs.StringVar(excludeExtOpt, "e", "", "Specify watch exclude file extension (optional)")

	// --exclude-dir
	excludeDirOpt := fs.String("exclude-dir", "", "Specify watch exclude directory (optional)")
	fs.StringVar(excludeDirOpt, "E", "", "Specify watch exclude directory (optional)")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	// --version
	versionFlagOpt := fs.Bool("version", false, "Show version.")
	fs.BoolVar(versionFlagOpt, "v", false, "Show version.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark --help")
	}
	err := fs.Parse(args)
	if err != nil {
		return optLength, nil, err
	}

	var targetDirname = ""
	_args := fs.Args()
	if len(_args) > 0 {
		targetDirname = _args[0]
	}

	currentDir := common.GetCurrentDir()

	result := &Option{
		WorkingDir:                      currentDir,
		TargetDirname:                   targetDirname,
		OutputFilename:                  *outputFilenameFlagOpt,
		ScanBufferValue:                 *scanBufferValueOpt,
		AdditionallyIgnoreRuleFilenames: *additionallyIgnoreRuleFilenamesOpt,
		IgnoreDotFileFlagValue:          *ignoreDotfileFlagValueOpt,
		PatternRegexpString:             *patternRegexOpt,
		IncludeExt:                      *includeExtOpt,
		ExcludeDirRegexpString:          *excludeDirRegexpOpt,
		ExcludeFileRegexpString:         *excludeFileRegexpOpt,
		ExcludeExt:                      *excludeExtOpt,
		ExcludeDir:                      *excludeDirOpt,
		WithLineNumberFlag:              *withLineNumberFlagOpt,
		HelpFlag:                        *helpFlagOpt,
		VersionFlag:                     *versionFlagOpt,
		FlagSet:                         fs,
	}

	OverRideHelp(fs)

	return optLength, result, nil
}

func OverRideHelp(fs *flag.FlagSet) *flag.FlagSet {
	fs.Usage = func() {
		fmt.Print(helpMessage)
	}
	return fs
}
