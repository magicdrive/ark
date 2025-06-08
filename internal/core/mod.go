package core

import (
	"github.com/magicdrive/ark/internal/commandline"
)

func Apply(opt *commandline.Option) error {
	if treeStr, err := GenerateTreeString(opt.TargetDirname, ""); err != nil {
		return err
	} else {
		if err := ReadAndWriteAllFiles(treeStr, "./", "output.txt", opt); err != nil {
			return err
		}
	}
	return nil
}
