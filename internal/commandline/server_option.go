package commandline

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/magicdrive/ark/internal/common"
)

// ServeOption defines options for launching the MCP server
type ServeOption struct {
	RootDir       string
	Port          string
	GeneralOption *Option
}

func ServerOptParse(args []string) (int, *ServeOption, error) {

	optLength := len(args)

	fs := flag.NewFlagSet("ark-server", flag.ExitOnError)

	currentDir := common.GetCurrentDir()

	// --root
	rootDirOpt := fs.String("root", currentDir, "Specify ark mcp server serv directory.")
	fs.StringVar(rootDirOpt, "r", currentDir, "Specify ark mcp server serv directory.")

	// --port
	portOpt := fs.Int("port", 8008, "Specify ark mcp server port.")
	fs.IntVar(portOpt, "p", 8008, "Specify ark mcp server port.")

	// --scan-buffer
	scanBufferValueOpt := fs.String("scan-buffer", "10M", "Specify the line scan buffer size.")
	fs.StringVar(scanBufferValueOpt, "b", "10M", "Specify the line scan buffer size.")

	// --mask-secrets
	maskSecretsFlagOpt := fs.String("mask-secrets", "on", "Specify Detect the secrets string and convert it to masked.")
	fs.StringVar(maskSecretsFlagOpt, "m", "on", "Specify Detect the secrets string and convert it to masked.")

	// --allow-gitignore
	allowGitignoreFlagOpt := fs.String("allow-gitignore", "on", "Specify enable .gitignore.")
	fs.StringVar(allowGitignoreFlagOpt, "a", "on", "Specify enable .gitignore.")

	// --additionally-ignorerule
	additionallyIgnoreRuleFilenamesOpt := fs.String("additionally-ignorerule", "", "Specify a file containing additional ignore rules.")
	fs.StringVar(additionallyIgnoreRuleFilenamesOpt, "A", "", "Specify a file containing additional ignore rules.")

	// --ignore-dotfile
	ignoreDotfileFlagValueOpt := fs.String("ignore-dotfile", "off", "Specify ignore dot files.")
	fs.StringVar(ignoreDotfileFlagValueOpt, "d", "off", "Specify ignore dot files.")

	// --pattern-regex
	patternRegexOpt := fs.String("pattern-regex", "", "Specify watch file pattern regexp (optional)")
	fs.StringVar(patternRegexOpt, "x", "", "Specify watch file pattern regexp (optional)")

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

	// --skik-non-utf8
	skipNonUTF8FlagOpt := fs.Bool("skip-non-utf8", false, "Specify ignore files that do not have utf8 charset.")
	fs.BoolVar(skipNonUTF8FlagOpt, "s", false, "Specify ignore files that do not have utf8 charset.")

	// --delete-comments
	deleteCommentsFlagOpt := fs.Bool("delete-comment", false, "Specify flag delete code comments.")
	fs.BoolVar(deleteCommentsFlagOpt, "D", false, "Specify flag delete code comments.")

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

	generalOpt := &Option{
		ScanBufferValue:                 *scanBufferValueOpt,
		MaskSecretsFlagValue:            *maskSecretsFlagOpt,
		AllowGitignoreFlagValue:         *allowGitignoreFlagOpt,
		AdditionallyIgnoreRuleFilenames: *additionallyIgnoreRuleFilenamesOpt,
		IgnoreDotFileFlagValue:          *ignoreDotfileFlagValueOpt,
		PatternRegexpString:             *patternRegexOpt,
		IncludeExt:                      *includeExtOpt,
		ExcludeDirRegexpString:          *excludeDirRegexpOpt,
		ExcludeFileRegexpString:         *excludeFileRegexpOpt,
		ExcludeExt:                      *excludeExtOpt,
		ExcludeDir:                      *excludeDirOpt,
		SkipNonUTF8Flag:                 *skipNonUTF8FlagOpt,
		DeleteCommentsFlag:              *deleteCommentsFlagOpt,
		WithLineNumberFlagValue:         "off",
		OutputFormatValue:               "auto",
		FlagSet:                         fs,
	}

	if err := generalOpt.Normalize(); err != nil {
		return optLength, nil, err
	}

	result := &ServeOption{
		RootDir:       *rootDirOpt,
		Port:          strconv.Itoa(*portOpt),
		GeneralOption: generalOpt,
	}

	OverRideHelp(fs)

	return optLength, result, nil
}
