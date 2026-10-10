package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/common"
	"github.com/magicdrive/ark/internal/fsroot"
)

func GenerateTreeString(path string, indent string, allowedFileListMap map[string]bool, opt *commandline.Option) (string, map[string]bool, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return "", nil, fmt.Errorf("Error reading directory %s: %v", path, err)
	}

	ApplySort(files)

	var b strings.Builder

	for i, file := range files {
		if opt.IgnoreDotFileFlag.Bool() && IsHiddenFile(file.Name()) {
			continue
		}

		fullPath := filepath.Join(path, file.Name())

		if IsMetadataDirName(file.Name()) {
			continue
		}

		if !CanBoaded(opt, fullPath) || aliasIgnored(opt, fullPath, file) {
			continue
		}

		isLastItem := i == len(files)-1

		if file.IsDir() {
			if isLastItem {
				b.WriteString(indent)
				b.WriteString("└── ")
				b.WriteString(file.Name())
				b.WriteString("/\n")
				treeStr, fl, _ := GenerateTreeString(fullPath, indent+"    ", allowedFileListMap, opt)
				allowedFileListMap = common.MergeAllowFileList(fl, allowedFileListMap)
				allowedFileListMap[fullPath] = true
				b.WriteString(treeStr)
			} else {
				b.WriteString(indent)
				b.WriteString("├── ")
				b.WriteString(file.Name())
				b.WriteString("/\n")
				treeStr, fl, _ := GenerateTreeString(fullPath, indent+"│   ", allowedFileListMap, opt)
				allowedFileListMap = common.MergeAllowFileList(fl, allowedFileListMap)
				allowedFileListMap[fullPath] = true
				b.WriteString(treeStr)
			}
		} else {
			b.WriteString(indent)
			if isLastItem {
				b.WriteString("└── ")
			} else {
				b.WriteString("├── ")
			}
			b.WriteString(file.Name())
			b.WriteString("\n")
			if readableEntry(fullPath, file) {
				allowedFileListMap[fullPath] = true
			}
		}

	}

	return b.String(), allowedFileListMap, nil
}

// readableEntry reports whether a non-directory entry of a listing has content
// to dump. A symlink is listed but its content is read only when it leads to a
// file: a link to a directory is not followed (walks never descend into
// symlinked directories), and a dangling or looping link has nothing to read.
func readableEntry(fullPath string, entry os.DirEntry) bool {
	if entry.Type()&os.ModeSymlink == 0 {
		return true
	}
	fi, err := os.Stat(fullPath)
	return err == nil && !fi.IsDir()
}

// aliasIgnored reports whether entry is a symlink leading to a path inside the
// ignore rules' root that the rules ignore (the path or a directory above
// it): a link must not expose an ignored file under another name. Where a
// link leads outside the root, no rule of the root applies to its target.
func aliasIgnored(opt *commandline.Option, fullPath string, entry os.DirEntry) bool {
	if entry.Type()&os.ModeSymlink == 0 || opt.GitIgnoreRule == nil {
		return false
	}
	real, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return false // dangling or looping: nothing is read through it
	}
	root := opt.GitIgnoreRule.Root()
	if root == "" {
		return false
	}
	rel, ok := relUnder(root, real)
	if !ok {
		if realRoot, err := filepath.EvalSymlinks(root); err == nil {
			rel, ok = relUnder(realRoot, real)
		}
	}
	if ok && rel != "." && ignoredWithParents(opt, rel) {
		return true
	}
	// The rules match names as the file system lists them: the link's text
	// may spell its target in another case, or reach the root by another
	// spelling of its path, on a case-insensitive file system.
	linkRel, inRoot := relUnder(root, fullPath)
	if !inRoot {
		return false
	}
	_, canonReal, err := fsroot.Canonicalize(root, filepath.ToSlash(linkRel), fsroot.Options{AllowExternalSymlinks: true})
	switch {
	case err == nil:
		return canonReal != "" && canonReal != "." && ignoredWithParents(opt, canonReal)
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fsroot.ErrNotDir):
		_, statErr := os.Stat(fullPath)
		return statErr == nil // reached only by the path API's rewriting: not dumped
	case errors.Is(err, fsroot.ErrSymlinkLoop):
		return false // nothing is read through it
	}
	return true // cannot be verified: not dumped
}

// ignoredWithParents reports whether rel (relative to the rules' root) or a
// directory above it matches the ignore rules.
func ignoredWithParents(opt *commandline.Option, rel string) bool {
	rel = filepath.ToSlash(rel)
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' && opt.GitIgnoreRule.MatchesRel(rel[:i]) {
			return true
		}
	}
	return opt.GitIgnoreRule.MatchesRel(rel)
}

// relUnder returns p relative to root when p is root or below it.
func relUnder(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// walkFrom walks start like filepath.WalkDir, entering it when it is the
// processed root given through a symlink (common.WalkDirRoot). A directory
// link named as any other start is not entered.
func walkFrom(opt *commandline.Option, start string, fn fs.WalkDirFunc) error {
	if common.SamePath(start, opt.IgnoreRoot()) {
		return common.WalkDirRoot(start, fn)
	}
	return filepath.WalkDir(start, fn)
}
