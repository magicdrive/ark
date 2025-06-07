package commandline

import (
	"flag"
	"regexp"
)

type Option struct {
	TargetDirname                string
	AdditionallyIgnoreRuleString string
	IgnoreDotFileFlag            string
	PatternRegexString           string
	PatternRegex                 *regexp.Regexp
	IncludeExt                   string
	ExcludeDirRegexString        string
	ExcludeDirRegex              *regexp.Regexp
	ExcludeFileRegexString       string
	ExcludeFileRegex             *regexp.Regexp
	ExcludeExt                   string
	ExcludeDir                   string
	HelpFlag                     bool
	VersionFlag                  bool
	FlagSet                      *flag.FlagSet
}

func (cr *Option) Normalize() error {
	return nil
}
