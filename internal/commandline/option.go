package commandline

import (
	"errors"
	"flag"
	"fmt"
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
	WithLineNumberFlag                 bool
	SkipNonUTF8Flag                    bool
	HelpFlag                           bool
	VersionFlag                        bool
	FlagSet                            *flag.FlagSet
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

	// scan-buffer
	if err := cr.IgnoreDotFileFlag.Set(cr.IgnoreDotFileFlagValue); err != nil {
		errorMessages = append(errorMessages, fmt.Sprintf("--ignore-dotfile %s", err.Error()))
	}

	// gitignorerule
	if cr.AdditionallyIgnoreRuleFilenames != "" {
		cr.AdditionallyIgnoreRuleFilenameList = common.CommaSeparated2StringList(cr.AdditionallyIgnoreRuleFilenames)
	} else {
		cr.AdditionallyIgnoreRuleFilenameList = []string{}
	}
	gitignorePath, _ := common.FindGitignore()
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
