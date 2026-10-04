package commandline

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/magicdrive/ark/internal/common"
)

type SetupOption struct {
	// Client is the raw positional client token (e.g. "cursor"). It is empty
	// when the user ran `ark setup` with no client. Validation against the
	// supported-client registry happens in the setup layer, not here —
	// commandline only parses argv.
	Client     string
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

	// Extract the leading positional client token before flag parsing. Go's
	// flag package stops at the first non-flag argument, so pulling the client
	// out first lets `ark setup <client> [OPTIONS]` parse options reliably.
	client := ""
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		client = args[0]
		rest = args[1:]
	}

	// --name
	nameOpt := fs.String("name", "", "Claude skill/slash-command name (Claude only; default: current directory name).")
	fs.StringVar(nameOpt, "n", "", "Claude skill/slash-command name (Claude only; default: current directory name).")

	// --ark-path
	arkPathOpt := fs.String("ark-path", "", "Path to ark binary (default: auto-detect).")
	fs.StringVar(arkPathOpt, "p", "", "Path to ark binary (default: auto-detect).")

	// --root
	rootDirOpt := fs.String("root", currentDir, "Root directory to serve.")
	fs.StringVar(rootDirOpt, "r", currentDir, "Root directory to serve.")

	// --global
	globalFlagOpt := fs.Bool("global", false, "Configure Ark in the client's user-level MCP configuration.")
	fs.BoolVar(globalFlagOpt, "g", false, "Configure Ark in the client's user-level MCP configuration.")

	// --force
	forceFlagOpt := fs.Bool("force", false, "Replace an existing Ark-owned MCP entry (never touches other config).")
	fs.BoolVar(forceFlagOpt, "f", false, "Replace an existing Ark-owned MCP entry (never touches other config).")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark setup --help")
	}

	if err := fs.Parse(rest); err != nil {
		return optLength, nil, err
	}

	// Formal contract: `ark setup <client> [OPTIONS]` — the client is the first
	// token, before any flags. Flags-before-client is intentionally NOT
	// supported: Go's flag package stops at the first non-flag argument, so
	// `ark setup --global cursor --force` could not parse the trailing flag
	// reliably. Any leftover positional is therefore an explicit error rather
	// than a silently half-working alternate syntax.
	if fs.NArg() > 0 {
		return optLength, nil, fmt.Errorf(
			"unexpected argument %q; usage: ark setup <client> [OPTIONS] (the client must come first)", fs.Arg(0))
	}

	result := &SetupOption{
		Client:     client,
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
