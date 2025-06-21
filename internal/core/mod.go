package core

import (
	"github.com/magicdrive/ark/internal/commandline"
)

func Apply(opt *commandline.Option) error {
	firstIndent := ""
	var firstAllowdFileListMap = map[string]bool{}
	if treeStr, allowdFileList, err := GenerateTreeString(opt.TargetDirname, firstIndent, firstAllowdFileListMap, opt); err != nil {
		return err
	} else {
		if err := ReadAndWriteAllFiles(treeStr, opt.TargetDirname, opt.OutputFilename, allowdFileList, opt); err != nil {
			return err
		}
	}
	return nil
}
