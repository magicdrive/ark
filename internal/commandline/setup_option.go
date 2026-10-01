package commandline

import (
	"flag"
	"fmt"
	"os"

	"github.com/magicdrive/ark/internal/common"
)

type SetupOption struct {
	Name       string
	ArkPath    string
	RootDir    string
	GlobalFlag bool
	ForceFlag  bool
	HelpFlag   bool
	FlagSet    *flag.FlagSet
}

func SetupOptParse(args []string) (int, *SetupOption, error) {
	optLength := len(args)

	fs := flag.NewFlagSet("setup", flag.ExitOnError)

	currentDir := common.GetCurrentDir()

	// --name
	nameOpt := fs.String("name", "", "Project name for the skill and slash command.")
	fs.StringVar(nameOpt, "n", "", "Project name for the skill and slash command.")

	// --ark-path
	arkPathOpt := fs.String("ark-path", "", "Path to ark binary (default: auto-detect).")
	fs.StringVar(arkPathOpt, "p", "", "Path to ark binary (default: auto-detect).")

	// --root
	rootDirOpt := fs.String("root", currentDir, "Root directory to serve.")
	fs.StringVar(rootDirOpt, "r", currentDir, "Root directory to serve.")

	// --global
	globalFlagOpt := fs.Bool("global", false, "Write MCP config to ~/.claude/settings.json.")
	fs.BoolVar(globalFlagOpt, "g", false, "Write MCP config to ~/.claude/settings.json.")

	// --force
	forceFlagOpt := fs.Bool("force", false, "Overwrite existing entries.")
	fs.BoolVar(forceFlagOpt, "f", false, "Overwrite existing entries.")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark setup --help")
	}

	if err := fs.Parse(args); err != nil {
		return optLength, nil, err
	}

	result := &SetupOption{
		Name:       *nameOpt,
		ArkPath:    *arkPathOpt,
		RootDir:    *rootDirOpt,
		GlobalFlag: *globalFlagOpt,
		ForceFlag:  *forceFlagOpt,
		HelpFlag:   *helpFlagOpt,
		FlagSet:    fs,
	}

	OverRideHelp(fs)

	return optLength, result, nil
}
