package commandline

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// InstructionOption is the parsed `ark instruction <target>` command line.
type InstructionOption struct {
	// Target is the raw positional instruction target (e.g. "claude"). It is
	// validated against the instruction target registry by the caller; the
	// parser only turns argv into fields.
	Target   string
	HelpFlag bool
	FlagSet  *flag.FlagSet
}

func InstructionOptParse(args []string) (int, *InstructionOption, error) {
	optLength := len(args)

	fs := flag.NewFlagSet("instruction", flag.ExitOnError)

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark instruction --help")
	}

	// The target is the first operand and may precede the flags, as for setup.
	target := ""
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target = args[0]
		rest = args[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return optLength, nil, err
	}
	if fs.NArg() > 0 {
		return optLength, nil, fmt.Errorf("unexpected argument %q; usage: ark instruction <target>", fs.Arg(0))
	}

	result := &InstructionOption{Target: target, HelpFlag: *helpFlagOpt, FlagSet: fs}
	OverRideHelp(fs)
	return optLength, result, nil
}
