package commandline

import (
	"flag"
	"fmt"

	_ "embed"
)

//go:embed help.txt
var helpMessage string

func OverRideHelp(fs *flag.FlagSet) *flag.FlagSet {
	fs.Usage = func() {
		fmt.Print(helpMessage)
	}
	return fs
}
