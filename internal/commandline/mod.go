package commandline

import (
	"flag"
	"fmt"
	"os"

	_ "embed"
)

//go:embed help.txt
var helpMessage string

func OptParse(args []string) (int, *Option, error) {

	optLength := len(args)

	fs := flag.NewFlagSet("ark", flag.ExitOnError)

	// --skip-non-utf8
	skipNonUTF8FlagOpt := fs.Bool("skip-non-utf8", false, "Show help message.")

	// --with-line-number
	withLineNumberFlagOpt := fs.Bool("with-line-number", false, "Show help message.")
	fs.BoolVar(withLineNumberFlagOpt, "num", false, "Show help message.")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	// --version
	versionFlagOpt := fs.Bool("version", false, "Show version.")
	fs.BoolVar(versionFlagOpt, "v", false, "Show version.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark --help")
	}
	err := fs.Parse(args)
	if err != nil {
		return optLength, nil, err
	}

	var targetDirname = ""
	_args := fs.Args()
	if len(_args) > 0 {
		targetDirname = _args[0]
	}

	result := &Option{
		TargetDirname:      targetDirname,
		SkipNonUTF8Flag:    *skipNonUTF8FlagOpt,
		WithLineNumberFlag: *withLineNumberFlagOpt,
		HelpFlag:           *helpFlagOpt,
		VersionFlag:        *versionFlagOpt,
		FlagSet:            fs,
	}

	OverRideHelp(fs)

	return optLength, result, nil
}

func OverRideHelp(fs *flag.FlagSet) *flag.FlagSet {
	fs.Usage = func() {
		fmt.Print(helpMessage)
	}
	return fs
}
