package core

import (
	"path/filepath"
	"slices"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/common"
)

func CanBoaded(opt *commandline.Option, path string) bool {
	absPath, _ := filepath.Abs(path)

	if opt.PatternRegexp != nil {
		baseName := filepath.Base(absPath)
		result := opt.PatternRegexp.MatchString(baseName)
		if result == false {
			return false
		}
	}

	if opt.ExcludeDir != "" {
		ext := filepath.Ext(absPath)
		if slices.Contains(opt.ExcludeDirList, ext) {
			return false
		}
	}

	if opt.IncludeExt != "" {
		ext := filepath.Ext(absPath)
		if !slices.Contains(opt.IncludeExtList, ext) {
			return false
		}
	}

	if opt.ExcludeDirRegexp != nil {
		dir := filepath.Dir(absPath)
		result := opt.ExcludeDirRegexp.MatchString(dir)
		if result == true {
			return false
		}
	}

	if opt.ExcludeFileRegexp != nil {
		baseName := filepath.Base(absPath)
		result := opt.ExcludeFileRegexp.MatchString(baseName)
		if result == true {
			return false
		}
	}

	if opt.GitIgnoreRule != nil {
		if opt.GitIgnoreRule.Matches(common.TrimDotSlash(path)) {
			return false
		}
	}

	if opt.ExcludeExt != "" {
		ext := filepath.Ext(absPath)
		if slices.Contains(opt.ExcludeExtList, ext) {
			return false
		}
	}

	return true
}
