package core

import (
	"github.com/magicdrive/ark/internal/commandline"
)

func Apply(opt *commandline.Option) error {
	firstIndent := ""
	if treeStr, err := GenerateTreeString(opt.TargetDirname, firstIndent); err != nil {
		return err
	} else {
		if err := ReadAndWriteAllFiles(treeStr, opt.TargetDirname, opt.OutputFilename, opt); err != nil {
			return err
		}
	}
	return nil
}
