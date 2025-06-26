package core

import (
	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/model"
)

func Apply(opt *commandline.Option) error {
	firstIndent := ""
	var firstAllowdFileListMap = map[string]bool{}
	if treeStr, allowdFileList, err := GenerateTreeString(opt.TargetDirname, firstIndent, firstAllowdFileListMap, opt); err != nil {
		return err
	} else {
		if opt.OutputFormat.String() == model.XML {
			if err := WriteAllFilesAsXML(treeStr, opt.TargetDirname, opt.OutputFilename, allowdFileList, opt); err != nil {
				return err
			}
		} else {
			if err := WriteAllFiles(treeStr, opt.TargetDirname, opt.OutputFilename, allowdFileList, opt); err != nil {
				return err
			}
		}
	}
	return nil
}
