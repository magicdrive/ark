package commandline

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	MaskSecretsFlagValue               string
	MaskSecretsFlag                    model.OnOffSwitch
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
	ComplessFlag                       bool
	SkipNonUTF8Flag                    bool
	DeleteCommentsFlag                 bool
	SilentFlag                         bool
	HelpFlag                           bool
	VersionFlag                        bool
	FlagSet                            *flag.FlagSet
}

func GeneralOptParse(args []string) (int, *Option, error) {

	optLength := len(args)

	fs := flag.NewFlagSet("ark", flag.ExitOnError)

	// --output-filename
	outputFilenameOpt := fs.String("output-filename", "", "Specify the output file name.")
	fs.StringVar(outputFilenameOpt, "o", "", "Specify the output file name.")

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

	// --with-line-number
	withLineNumberFlagOpt := fs.String("with-line-number", "off", "Specify Whether to include file line numbers when outputting.")
	fs.StringVar(withLineNumberFlagOpt, "n", "off", "Specify Whether to include file line numbers when outputting.")

	// --output-format
	outputFormatOpt := fs.String("output-format", "auto", "Specify the format of the output file.")
	fs.StringVar(outputFormatOpt, "f", "auto", "Specify the format of the output file.")

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

	// --compless
	complessFlagOpt := fs.Bool("compless", false, "Specify flag compress the output result with arklite.")
	fs.BoolVar(complessFlagOpt, "c", false, "Specify flag compress the output result with arklite.")

	// --skik-non-utf8
	skipNonUTF8FlagOpt := fs.Bool("skip-non-utf8", false, "Specify ignore files that do not have utf8 charset.")
	fs.BoolVar(skipNonUTF8FlagOpt, "s", false, "Specify ignore files that do not have utf8 charset.")

	// --silent
	silentFlagOpt := fs.Bool("silent", false, "Specify flag process without displaying messages during processing.")
	fs.BoolVar(silentFlagOpt, "S", false, "Specify flag process without displaying messages during processing.")

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

	currentDir := common.GetCurrentDir()

	var targetDirname = ""
	_args := fs.Args()
	if len(_args) > 0 {
		targetDirname = _args[0]
	}
	if targetDirname == "" {
		// default targetDirname
		targetDirname = currentDir
	}

	result := &Option{
		WorkingDir:                      currentDir,
		TargetDirname:                   targetDirname,
		OutputFilename:                  *outputFilenameOpt,
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
		WithLineNumberFlagValue:         *withLineNumberFlagOpt,
		OutputFormatValue:               *outputFormatOpt,
		ComplessFlag:                    *complessFlagOpt,
		SkipNonUTF8Flag:                 *skipNonUTF8FlagOpt,
		SilentFlag:                      *silentFlagOpt,
		DeleteCommentsFlag:              *deleteCommentsFlagOpt,
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

func (cr *Option) Normalize() error {
	var errorMessages = []string{}

	// File selection: extension/directory lists, the ignore-dotfile switch and
	// the include/exclude regexps.
	errorMessages = append(errorMessages, cr.normalizeFileFilters()...)

	// scan-buffer
	if err := cr.ScanBuffer.Set(cr.ScanBufferValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--scan-buffer %s", err.Error()))
	}

	// mask-secrets
	if err := cr.MaskSecretsFlag.Set(cr.MaskSecretsFlagValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--mask-secrets %s", err.Error()))
	}

	// allow-gitignore
	allowGitignoreValid := true
	if err := cr.AllowGitignoreFlag.Set(cr.AllowGitignoreFlagValue); err != nil {
		allowGitignoreValid = false
		errorMessages = append(errorMessages, fmt.Sprintf("--allow-gitignore %s", err.Error()))
	}

	// with-line-number
	if err := cr.WithLineNumberFlag.Set(cr.WithLineNumberFlagValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--with-line-number %s", err.Error()))
	}

	// --output-format
	if err := cr.OutputFormat.Set(cr.OutputFormatValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--output-format %s", err.Error()))
	}

	// output-format

	if cr.OutputFormat.String() == model.Auto {
		if cr.OutputFilename != "" {
			ext := filepath.Ext(cr.OutputFilename)
			ditectFormatValue := model.Ext2OutputFormat(ext)

			cr.OutputFormatValue = ditectFormatValue
			cr.OutputFormat.Set(cr.OutputFormatValue)
		} else {
			// plaintext
			cr.OutputFormatValue = model.PlainText
			cr.OutputFormat.Set(cr.OutputFormatValue)
		}
	}

	// output-filename
	if cr.OutputFilename == "" {
		switch cr.OutputFormat.String() {
		case model.Markdown:
			cr.OutputFilename = "ark-output.md"
		case model.PlainText:
			cr.OutputFilename = "ark-output.txt"
		case model.XML:
			cr.OutputFilename = "ark-output.xml"
		case model.Arklite:
			cr.OutputFilename = "ark-output.arklite.txt"
		default:
			cr.OutputFilename = "ark-output.txt"
		}
	}

	// compless
	if cr.OutputFormat.CanCompless() && cr.ComplessFlag {
		cr.DeleteCommentsFlag = true
		cr.OutputFilename = fmt.Sprintf("%s%s", cr.OutputFilename, ".arklite.txt")
	}

	// gitignorerule
	if cr.AdditionallyIgnoreRuleFilenames != "" {
		cr.AdditionallyIgnoreRuleFilenameList = common.CommaSeparated2StringList(cr.AdditionallyIgnoreRuleFilenames)
	} else {
		cr.AdditionallyIgnoreRuleFilenameList = []string{}
	}

	// Only an accepted value may be read back: Bool() panics on an unset switch,
	// and the invalid value is already reported through errorMessages below.
	if allowGitignoreValid {
		cr.GitIgnoreRule, _ = libgitignore.GenerateIntegratedGitIgnore(cr.AllowGitignoreFlag.Bool(), cr.WorkingDir, cr.AdditionallyIgnoreRuleFilenameList)
	}

	if len(errorMessages) == 0 {
		return nil
	} else {
		return errors.New(strings.Join(errorMessages, "\n"))
	}
}

// NormalizeFileFilters recomputes the file-selection state derived from the raw
// option values: the extension and directory lists, the ignore-dotfile switch
// and the compiled include/exclude regexps. A caller that changes any of those
// raw values on a copy of a normalized Option (e.g. per MCP request) must call
// it before the copy is used. It never touches repository-dependent state such
// as GitIgnoreRule.
func (cr *Option) NormalizeFileFilters() error {
	if msgs := cr.normalizeFileFilters(); len(msgs) > 0 {
		return errors.New(strings.Join(msgs, "\n"))
	}
	return nil
}

func (cr *Option) normalizeFileFilters() []string {
	var errorMessages []string

	cr.IncludeExtList = common.CommaSeparated2StringList(cr.IncludeExt)
	cr.ExcludeExtList = common.CommaSeparated2StringList(cr.ExcludeExt)
	cr.ExcludeDirList = common.CommaSeparated2StringList(cr.ExcludeDir)

	// ignore-dotfile
	if err := cr.IgnoreDotFileFlag.Set(cr.IgnoreDotFileFlagValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--ignore-dotfile %s", err.Error()))
	}

	// compile regexp
	var err error
	if cr.PatternRegexp, err = compileOptional(cr.PatternRegexpString); err != nil {
		errorMessages = append(errorMessages, fmt.Errorf("failed to compile pattern-regexp: %w", err).Error())
	}
	if cr.ExcludeDirRegexp, err = compileOptional(cr.ExcludeDirRegexpString); err != nil {
		errorMessages = append(errorMessages, fmt.Errorf("failed to compile exclude-dir-regexp: %w", err).Error())
	}
	if cr.ExcludeFileRegexp, err = compileOptional(cr.ExcludeFileRegexpString); err != nil {
		errorMessages = append(errorMessages, fmt.Errorf("failed to compile exclude-file-regexp: %w", err).Error())
	}
	return errorMessages
}

// compileOptional compiles pattern, or returns nil for an empty pattern.
func compileOptional(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}
