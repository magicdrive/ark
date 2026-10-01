package commandline

import (
	"flag"
	"fmt"
	"os"
)

type SkillAutoOption struct {
	Name        string
	Output      string
	ArchiveFlag bool
	ForceFlag   bool
	HelpFlag    bool
	FlagSet     *flag.FlagSet
}

type SkillInitOption struct {
	Name        string
	Output      string
	ArchiveFlag bool
	HelpFlag    bool
	FlagSet     *flag.FlagSet
}

type SkillAddExplorerOption struct {
	Name        string
	Output      string
	ArchiveFlag bool
	HelpFlag    bool
	FlagSet     *flag.FlagSet
}

type SkillUpdateOption struct {
	ForceFlag  bool
	DryRunFlag bool
	HelpFlag   bool
	FlagSet    *flag.FlagSet
}

type SkillInspectOption struct {
	HelpFlag bool
	FlagSet  *flag.FlagSet
}

func SkillAutoOptParse(args []string) (int, *SkillAutoOption, error) {
	optLength := len(args)

	fs := flag.NewFlagSet("skill", flag.ExitOnError)

	// --name
	nameOpt := fs.String("name", "", "Specify skill name.")

	// --output
	outputOpt := fs.String("output", "", "Specify output directory.")

	// --archive
	archiveFlagOpt := fs.Bool("archive", false, "Specify flag to create ZIP archive.")

	// --force
	forceFlagOpt := fs.Bool("force", false, "Specify flag to overwrite existing skill.")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark skill --help")
	}

	if err := fs.Parse(args); err != nil {
		return optLength, nil, err
	}

	result := &SkillAutoOption{
		Name:        *nameOpt,
		Output:      *outputOpt,
		ArchiveFlag: *archiveFlagOpt,
		ForceFlag:   *forceFlagOpt,
		HelpFlag:    *helpFlagOpt,
		FlagSet:     fs,
	}

	OverRideHelp(fs)

	if err := result.Normalize(); err != nil {
		return optLength, nil, err
	}

	return optLength, result, nil
}

func (cr *SkillAutoOption) Normalize() error {
	return nil
}

func SkillInitOptParse(args []string) (int, *SkillInitOption, error) {
	optLength := len(args)

	fs := flag.NewFlagSet("skill-init", flag.ExitOnError)

	// --name
	nameOpt := fs.String("name", "repository-development", "Specify skill name.")

	// --output
	outputOpt := fs.String("output", "", "Specify output directory.")

	// --archive
	archiveFlagOpt := fs.Bool("archive", false, "Specify flag to create ZIP archive.")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark skill init --help")
	}

	if err := fs.Parse(args); err != nil {
		return optLength, nil, err
	}

	result := &SkillInitOption{
		Name:        *nameOpt,
		Output:      *outputOpt,
		ArchiveFlag: *archiveFlagOpt,
		HelpFlag:    *helpFlagOpt,
		FlagSet:     fs,
	}

	OverRideHelp(fs)

	if err := result.Normalize(); err != nil {
		return optLength, nil, err
	}

	return optLength, result, nil
}

func (cr *SkillInitOption) Normalize() error {
	return nil
}

func SkillAddExplorerOptParse(args []string) (int, *SkillAddExplorerOption, error) {
	optLength := len(args)

	fs := flag.NewFlagSet("skill-add-explorer", flag.ExitOnError)

	// --name
	nameOpt := fs.String("name", "ark-code-explorer", "Specify skill name.")

	// --output
	outputOpt := fs.String("output", "", "Specify output directory.")

	// --archive
	archiveFlagOpt := fs.Bool("archive", false, "Specify flag to create ZIP archive.")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark skill add-explorer --help")
	}

	if err := fs.Parse(args); err != nil {
		return optLength, nil, err
	}

	result := &SkillAddExplorerOption{
		Name:        *nameOpt,
		Output:      *outputOpt,
		ArchiveFlag: *archiveFlagOpt,
		HelpFlag:    *helpFlagOpt,
		FlagSet:     fs,
	}

	OverRideHelp(fs)

	if err := result.Normalize(); err != nil {
		return optLength, nil, err
	}

	return optLength, result, nil
}

func (cr *SkillAddExplorerOption) Normalize() error {
	return nil
}

func SkillUpdateOptParse(args []string) (int, *SkillUpdateOption, error) {
	optLength := len(args)

	fs := flag.NewFlagSet("skill-update", flag.ExitOnError)

	// --force
	forceFlagOpt := fs.Bool("force", false, "Specify flag to force update even if user modifications detected.")

	// --dry-run
	dryRunFlagOpt := fs.Bool("dry-run", false, "Specify flag to show what would be updated without making changes.")

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark skill update --help")
	}

	if err := fs.Parse(args); err != nil {
		return optLength, nil, err
	}

	result := &SkillUpdateOption{
		ForceFlag:  *forceFlagOpt,
		DryRunFlag: *dryRunFlagOpt,
		HelpFlag:   *helpFlagOpt,
		FlagSet:    fs,
	}

	OverRideHelp(fs)

	if err := result.Normalize(); err != nil {
		return optLength, nil, err
	}

	return optLength, result, nil
}

func (cr *SkillUpdateOption) Normalize() error {
	return nil
}

func SkillInspectOptParse(args []string) (int, *SkillInspectOption, error) {
	optLength := len(args)

	fs := flag.NewFlagSet("skill-inspect", flag.ExitOnError)

	// --help
	helpFlagOpt := fs.Bool("help", false, "Show help message.")
	fs.BoolVar(helpFlagOpt, "h", false, "Show help message.")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "\nHelpOption:")
		fmt.Fprintln(os.Stderr, "    ark skill inspect --help")
	}

	if err := fs.Parse(args); err != nil {
		return optLength, nil, err
	}

	result := &SkillInspectOption{
		HelpFlag: *helpFlagOpt,
		FlagSet:  fs,
	}

	OverRideHelp(fs)

	if err := result.Normalize(); err != nil {
		return optLength, nil, err
	}

	return optLength, result, nil
}

func (cr *SkillInspectOption) Normalize() error {
	return nil
}
