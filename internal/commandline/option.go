package commandline

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "embed"

	"github.com/magicdrive/ark/internal/common"
	"github.com/magicdrive/ark/internal/libgitignore"
	"github.com/magicdrive/ark/internal/model"
)

type Option struct {
	WorkingDir                         string
	TargetDirname                      string
	OutputFilename                     string
	ScanBufferValue                    string
	ScanBuffer                         model.ByteString
	AllowGitignoreFlagValue            string
	AllowGitignoreFlag                 model.OnOffSwitch
	AdditionallyIgnoreRuleFilenames    string
	AdditionallyIgnoreRuleFilenameList []string
	GitIgnoreRule                      *libgitignore.GitIgnore
	IgnoreDotFileFlagValue             string
	IgnoreDotFileFlag                  model.OnOffSwitch
	PatternRegexpString                string
	PatternRegexp                      *regexp.Regexp
	IncludeExt                         string
	IncludeExtList                     []string
	ExcludeDirRegexpString             string
	ExcludeDirRegexp                   *regexp.Regexp
	ExcludeFileRegexpString            string
	ExcludeFileRegexp                  *regexp.Regexp
	ExcludeExt                         string
	ExcludeExtList                     []string
	ExcludeDir                         string
	ExcludeDirList                     []string
	WithLineNumberFlagValue            string
	WithLineNumberFlag                 model.OnOffSwitch
	OutputFormatValue                  string
	OutputFormat                       model.OutputFormat
	SkipNonUTF8Flag                    bool
	HelpFlag                           bool
	VersionFlag                        bool
	FlagSet                            *flag.FlagSet
}

//go:embed help.txt
var helpMessage string

func OptParse(args []string) (int, *Option, error) {

	optLength := len(args)

	fs := flag.NewFlagSet("ark", flag.ExitOnError)

	// --output-filename
	outputFilenameOpt := fs.String("output-filename", "", "Show help message.")
	fs.StringVar(outputFilenameOpt, "o", "", "Show help message.")

	// --scan-buffer
	scanBufferValueOpt := fs.String("scan-buffer", "10M", "Show help message.")
	fs.StringVar(scanBufferValueOpt, "b", "10M", "Show help message.")

	// --allow-gitignore
	allowGitignoreFlagOpt := fs.String("allow-gitignore", "on", "Show help message.")
	fs.StringVar(allowGitignoreFlagOpt, "a", "on", "Show help message.")

	// --additionally-ignorerule
	additionallyIgnoreRuleFilenamesOpt := fs.String("additionally-ignorerule", "", "Show help message.")
	fs.StringVar(additionallyIgnoreRuleFilenamesOpt, "p", "", "Show help message.")

	// --with-line-number
	withLineNumberFlagOpt := fs.String("with-line-number", "off", "Show help message.")
	fs.StringVar(withLineNumberFlagOpt, "n", "off", "Show help message.")

	// --output-format
	outputFormatOpt := fs.String("output-format", "", "Show help message.")
	fs.StringVar(outputFormatOpt, "f", "", "Show help message.")

	// --ignore-dotfile
	ignoreDotfileFlagValueOpt := fs.String("ignore-dotfile", "off", "Show help message.")
	fs.StringVar(ignoreDotfileFlagValueOpt, "d", "off", "Show help message.")

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
	skipNonUTF8FlagOpt := fs.Bool("skip-non-utf8", false, "Show help message.")
	fs.BoolVar(skipNonUTF8FlagOpt, "s", false, "Show help message.")

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
		OutputFilename:                  *outputFilenameOpt,
		ScanBufferValue:                 *scanBufferValueOpt,
		AllowGitignoreFlagValue:         *allowGitignoreFlagOpt,
		AdditionallyIgnoreRuleFilenames: *additionallyIgnoreRuleFilenamesOpt,
		IgnoreDotFileFlagValue:          *ignoreDotfileFlagValueOpt,
		PatternRegexpString:             *patternRegexOpt,
		IncludeExt:                      *includeExtOpt,
		ExcludeDirRegexpString:          *excludeDirRegexpOpt,
		ExcludeFileRegexpString:         *excludeFileRegexpOpt,
		ExcludeExt:                      *excludeExtOpt,
		ExcludeDir:                      *excludeDirOpt,
		WithLineNumberFlagValue:         *withLineNumberFlagOpt,
		OutputFormatValue:               *outputFormatOpt,
		SkipNonUTF8Flag:                 *skipNonUTF8FlagOpt,
		HelpFlag:                        *helpFlagOpt,
		VersionFlag:                     *versionFlagOpt,
		FlagSet:                         fs,
	}

	OverRideHelp(fs)

	if err := result.Normalize(); err != nil {
		return optLength, nil, err
	}

	return optLength, result, nil
}

func OverRideHelp(fs *flag.FlagSet) *flag.FlagSet {
	fs.Usage = func() {
		fmt.Print(helpMessage)
	}
	return fs
}

func (cr *Option) Normalize() error {
	var errorMessages = []string{}

	if cr.IncludeExt != "" {
		cr.IncludeExtList = common.CommaSeparated2StringList(cr.IncludeExt)
	}
	if cr.ExcludeExt != "" {
		cr.ExcludeExtList = common.CommaSeparated2StringList(cr.ExcludeExt)
	}
	if cr.ExcludeDir != "" {
		cr.ExcludeDirList = common.CommaSeparated2StringList(cr.ExcludeDir)
	}

	// scan-buffer
	if err := cr.ScanBuffer.Set(cr.ScanBufferValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--scan-buffer %s", err.Error()))
	}

	// allow-gitignore
	if err := cr.AllowGitignoreFlag.Set(cr.AllowGitignoreFlagValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--allow-gitignore %s", err.Error()))
	}

	// ignore-dotfile
	if err := cr.IgnoreDotFileFlag.Set(cr.IgnoreDotFileFlagValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--ignore-dotfile %s", err.Error()))
	}

	// with-line-number
	if err := cr.WithLineNumberFlag.Set(cr.WithLineNumberFlagValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--with-line-number %s", err.Error()))
	}

	// output-format
	if cr.OutputFormatValue == "" && cr.OutputFilename != "" {
		ext := filepath.Ext(cr.OutputFilename)
		cr.OutputFormatValue = model.Ext2OutputFormat(ext)
	} else if cr.OutputFormatValue == "" {
		cr.OutputFormatValue = model.PlainText
	}

	// output-filename
	if cr.OutputFilename == "" {
		switch cr.OutputFormat.String() {
		case model.Markdown:
			cr.OutputFilename = "ark_output.md"
		case model.PlainText:
			cr.OutputFilename = "ark_output.txt"
		default:
			cr.OutputFilename = "ark_output.txt"
		}
	}

	if err := cr.OutputFormat.Set(cr.OutputFormatValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--output-format %s", err.Error()))
	}

	// gitignorerule
	if cr.AdditionallyIgnoreRuleFilenames != "" {
		cr.AdditionallyIgnoreRuleFilenameList = common.CommaSeparated2StringList(cr.AdditionallyIgnoreRuleFilenames)
	} else {
		cr.AdditionallyIgnoreRuleFilenameList = []string{}
	}
	if arkIgnorePath, err := common.FindArkignore(); err != nil {
		cr.AdditionallyIgnoreRuleFilenameList = append(cr.AdditionallyIgnoreRuleFilenameList, arkIgnorePath)
	}

	var gitignorePath = ""
	if cr.AllowGitignoreFlag.Bool() {
		gitignorePath, _ = common.FindGitignore()
	}

	cr.GitIgnoreRule = libgitignore.GenerateIntegratedGitIgnore(cr.WorkingDir, gitignorePath, cr.AdditionallyIgnoreRuleFilenameList)

	// compile regexp
	if cr.PatternRegexpString != "" {
		re, err := regexp.Compile(cr.PatternRegexpString)
		if err != nil {
			e := fmt.Errorf("failed to compile pattern-regexp: %w", err)
			errorMessages = append(errorMessages, e.Error())
		} else {
			cr.PatternRegexp = re
		}
	}

	if cr.ExcludeDirRegexpString != "" {
		re, err := regexp.Compile(cr.ExcludeDirRegexpString)
		if err != nil {
			e := fmt.Errorf("failed to compile exclude-dir-regexp: %w", err)
			errorMessages = append(errorMessages, e.Error())
		} else {
			cr.ExcludeDirRegexp = re
		}
	}

	if cr.ExcludeFileRegexpString != "" {
		re, err := regexp.Compile(cr.ExcludeFileRegexpString)
		if err != nil {
			e := fmt.Errorf("failed to compile exclude-file-regexp: %w", err)
			errorMessages = append(errorMessages, e.Error())
		} else {
			cr.ExcludeFileRegexp = re
		}
	}

	if len(errorMessages) == 0 {
		return nil
	} else {
		return errors.New(strings.Join(errorMessages, "\n"))
	}
}
