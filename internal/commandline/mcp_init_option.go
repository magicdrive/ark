package commandline

import (
	"flag"
	"fmt"
	"os"

	"github.com/magicdrive/ark/internal/common"
)

type MCPInitOption struct {
	ArkPath    string
	RootDir    string
	ServerName string
	GlobalFlag bool
	ForceFlag  bool
	HelpFlag   bool
	FlagSet    *flag.FlagSet
}

func MCPInitOptParse(args []string) (int, *MCPInitOption, error) {
	optLength := len(args)

	fs := flag.NewFlagSet("mcp-init", flag.ExitOnError)

	currentDir := common.GetCurrentDir()

	// --ark-path
	arkPathOpt := fs.String("ark-path", "", "Path to ark binary (default: auto-detect).")
	fs.StringVar(arkPathOpt, "p", "", "Path to ark binary (default: auto-detect).")

	// --root
	rootDirOpt := fs.String("root", currentDir, "Root directory to serve.")
	fs.StringVar(rootDirOpt, "r", currentDir, "Root directory to serve.")

	// --name
	nameOpt := fs.String("name", "ark", "MCP server name in settings.json.")
	fs.StringVar(nameOpt, "n", "ark", "MCP server name in settings.json.")

	// --global
	globalFlagOpt := fs.Bool("global", false, "Write to ~/.claude/settings.json instead of .mcp.json.")
	fs.BoolVar(globalFlagOpt, "g", false, "Write to ~/.claude/settings.json instead of .mcp.json.")

	// --force
	forceFlagOpt := fs.Bool("force", false, "Overwrite existing MCP server entry.")
	fs.BoolVar(forceFlagOpt, "f", false, "Overwrite existing MCP server entry.")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark mcp-init --help")
	}

	if err := fs.Parse(args); err != nil {
		return optLength, nil, err
	}

	result := &MCPInitOption{
		ArkPath:    *arkPathOpt,
		RootDir:    *rootDirOpt,
		ServerName: *nameOpt,
		GlobalFlag: *globalFlagOpt,
		ForceFlag:  *forceFlagOpt,
		HelpFlag:   *helpFlagOpt,
		FlagSet:    fs,
	}

	OverRideHelp(fs)

	return optLength, result, nil
}
