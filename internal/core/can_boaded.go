package core

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/magicdrive/ark/internal/commandline"
)

func CanBoaded(opt *commandline.Option, path string) bool {
	absPath, _ := filepath.Abs(path)

	if opt.AccessExclude != nil && opt.AccessExclude(absPath) {
		return false
	}

	// The rules are rooted at their own directory (Option.IgnoreRoot), not at
	// the working directory a relative path is relative to: match the
	// absolute path.
	if opt.GitIgnoreRule.MatchesPath(absPath) {
		return false
	}

	if opt.PatternRegexp != nil {
		baseName := filepath.Base(absPath)
		result := opt.PatternRegexp.MatchString(baseName)
		if result == false {
			return false
		}
	}

	if opt.ExcludeDir != "" {
		for dirname := range strings.SplitSeq(filepath.ToSlash(filepath.Dir(path)), "/") {
			if slices.Contains(opt.ExcludeDirList, dirname) {
				return false
			}
		}
	}

	if opt.IncludeExt != "" {
		ext := filepath.Ext(absPath)
		if !extensionListed(opt.IncludeExtList, ext) {
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

	if opt.ExcludeExt != "" {
		ext := filepath.Ext(absPath)
		if extensionListed(opt.ExcludeExtList, ext) {
			return false
		}
	}

	return true
}

// extensionListed reports whether ext (as filepath.Ext returns it, with a
// leading ".") is in list. List entries are accepted in the documented form
// ("go") as well as with a leading dot (".go").
func extensionListed(list []string, ext string) bool {
	return ext != "" && (slices.Contains(list, ext) || slices.Contains(list, strings.TrimPrefix(ext, ".")))
}

// CanEnterDir reports whether a directory walk should descend into dir. It
// applies exactly the rules CanBoaded applies to a directory path that concern
// directories — ignore rules, excluded directory names and the exclude-dir
// regexp (both of which, as in CanBoaded, look at the path's parent
// directories). The file-level rules (extensions, the file pattern and the
// exclude-file regexp) select files and are checked by CanBoaded for each file;
// applied to a directory they would prune every directory whose own name is
// not, say, a ".php" file.
func CanEnterDir(opt *commandline.Option, dir string) bool {
	if opt.AccessExclude != nil {
		if absDir, err := filepath.Abs(dir); err == nil && opt.AccessExclude(absDir) {
			return false
		}
	}
	if absDir, err := filepath.Abs(dir); err == nil && opt.GitIgnoreRule.MatchesPath(absDir) {
		return false
	}
	if opt.ExcludeDir != "" {
		for dirname := range strings.SplitSeq(filepath.ToSlash(filepath.Dir(dir)), "/") {
			if slices.Contains(opt.ExcludeDirList, dirname) {
				return false
			}
		}
	}
	if opt.ExcludeDirRegexp != nil {
		absDir, _ := filepath.Abs(dir)
		if opt.ExcludeDirRegexp.MatchString(filepath.Dir(absDir)) {
			return false
		}
	}
	return true
}
