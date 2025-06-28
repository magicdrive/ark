package core

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/model"
	"github.com/magicdrive/ark/internal/spinner"
	"github.com/magicdrive/ark/internal/textbank"
)

func Apply(opt *commandline.Option) error {
	if opt.SilentFlag {
		return createDumpFile(opt)
	} else {
		return withSpinner(opt)
	}
}

func withSpinner(opt *commandline.Option) error {

	s := spinner.New(120*time.Millisecond, fmt.Sprintf("%s  Processing...", textbank.EmojiHourglass))
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sig
		s.Stop(fmt.Sprintf("%s  Archiving interrupted:%s", textbank.EmojiInterrupted, opt.OutputFilename))
		os.Exit(1)
	}()

	s.Start()
	createDumpFile(opt)

	s.SetMessage(fmt.Sprintf("%s  Finalizing record...", textbank.EmojiAlmost))
	time.Sleep(1 * time.Second)

	s.Stop(fmt.Sprintf("%s  Archive completed: %s", textbank.EmojiDone, opt.OutputFilename))
	return nil

}

func createDumpFile(opt *commandline.Option) error {
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
