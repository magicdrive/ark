package ark

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
	"github.com/magicdrive/ark/internal/mcp"
	"github.com/magicdrive/ark/internal/skill"
	"github.com/magicdrive/ark/internal/syntax"
)

func Execute(version string) {
	if len(os.Args) < 2 {
		fmt.Println("Usage: ark <command> [options]")
		fmt.Println("\nCommands:")
		fmt.Println("  mcp-server  Start MCP server")
		fmt.Println("  syntax      Parse file and output AST")
		fmt.Println("  symbol      Extract symbols from file")
		fmt.Println("  skill       Generate Cline/ChatGPT Skill")
		fmt.Println("  <dir>       Dump directory tree (default)")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "mcp-server":
		runMCPServer(version)
	case "syntax":
		runSyntaxCommand()
	case "symbol":
		runSymbolCommand()
	case "skill":
		runSkillCommand()
	default:
		runDefaultCommand(version)
	}
}

func runMCPServer(version string) {
	_, opt, err := commandline.ServerOptParse(version, os.Args[2:])
	if err != nil {
		log.Fatalf("Fatal Error: %v\n", err)
	}
	if opt.GeneralOption.HelpFlag {
		opt.GeneralOption.FlagSet.Usage()
		os.Exit(0)
	}
	mcp.RunMCPServe(opt.RootDir, opt)
}

func runSyntaxCommand() {
	fs := flag.NewFlagSet("syntax", flag.ExitOnError)
	lang := fs.String("lang", "", "Language (go, typescript, tsx, javascript, python)")
	format := fs.String("format", "text", "Output format (text, json)")
	help := fs.Bool("help", false, "Show help")
	fs.BoolVar(help, "h", false, "Show help")

	fs.Usage = func() {
		fmt.Println("Usage: ark syntax <file> [options]")
		fmt.Println("\nParse a file and output the AST using Tree-sitter.")
		fmt.Println("\nOptions:")
		fs.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  ark syntax main.go")
		fmt.Println("  ark syntax app.ts --format json")
		fmt.Println("  ark syntax script.py --lang python")
	}

	if err := fs.Parse(os.Args[2:]); err != nil {
		log.Fatal(err)
	}

	if *help {
		fs.Usage()
		os.Exit(0)
	}

	if fs.NArg() < 1 {
		fmt.Println("Error: file path is required")
		fs.Usage()
		os.Exit(1)
	}

	opts := syntax.SyntaxOptions{
		FilePath: fs.Arg(0),
		Lang:     *lang,
		Format:   *format,
	}

	if err := syntax.RunSyntaxCommand(opts); err != nil {
		log.Fatalf("Error: %v\n", err)
	}
}

func runSymbolCommand() {
	fs := flag.NewFlagSet("symbol", flag.ExitOnError)
	lang := fs.String("lang", "", "Language (go, typescript, tsx, javascript, python)")
	format := fs.String("format", "text", "Output format (text, json)")
	help := fs.Bool("help", false, "Show help")
	fs.BoolVar(help, "h", false, "Show help")

	fs.Usage = func() {
		fmt.Println("Usage: ark symbol <file> [options]")
		fmt.Println("\nExtract symbols (functions, types, classes, etc.) from a file.")
		fmt.Println("\nOptions:")
		fs.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  ark symbol main.go")
		fmt.Println("  ark symbol app.ts --format json")
		fmt.Println("  ark symbol script.py --lang python")
	}

	if err := fs.Parse(os.Args[2:]); err != nil {
		log.Fatal(err)
	}

	if *help {
		fs.Usage()
		os.Exit(0)
	}

	if fs.NArg() < 1 {
		fmt.Println("Error: file path is required")
		fs.Usage()
		os.Exit(1)
	}

	opts := syntax.SymbolOptions{
		FilePath: fs.Arg(0),
		Lang:     *lang,
		Format:   *format,
	}

	if err := syntax.RunSymbolCommand(opts); err != nil {
		log.Fatalf("Error: %v\n", err)
	}
}

func runSkillCommand() {
	// Check for subcommands
	if len(os.Args) > 2 {
		switch os.Args[2] {
		case "init":
			runSkillInit()
			return
		case "add-explorer":
			runSkillAddExplorer()
			return
		case "update":
			runSkillUpdate()
			return
		case "inspect":
			runSkillInspect()
			return
		}
	}

	// Auto mode
	fs := flag.NewFlagSet("skill", flag.ExitOnError)
	name := fs.String("name", "", "Skill name")
	output := fs.String("output", "", "Output directory")
	archive := fs.Bool("archive", false, "Create ZIP archive")
	force := fs.Bool("force", false, "Overwrite existing")
	help := fs.Bool("help", false, "Show help")
	fs.BoolVar(help, "h", false, "Show help")

	fs.Usage = func() {
		fmt.Println("Usage: ark skill [subcommand] [options]")
		fmt.Println("\nSubcommands: init, add-explorer, update, inspect")
		fmt.Println("\nOptions:")
		fs.PrintDefaults()
	}

	fs.Parse(os.Args[2:])
	if *help {
		fs.Usage()
		os.Exit(0)
	}

	runSkillAuto(*name, *output, *archive, *force)
}

func runDefaultCommand(version string) {
	_, opt, err := commandline.GeneralOptParse(os.Args[1:])
	if err != nil {
		log.Fatalf("Fatal Error: %v\n", err)
	}

	if opt.VersionFlag {
		fmt.Printf("ark version %s\n", version)
		os.Exit(0)
	}

	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	if opt.TargetDirname == "" {
		fmt.Println("Error: a directory name is required")
		os.Exit(1)
	} else if !DirExists(opt.TargetDirname) {
		fmt.Printf("Error: a directory not found: %s\n", opt.TargetDirname)
		os.Exit(1)
	}

	if err := core.Apply(opt); err != nil {
		log.Fatal(err)
	}
}

func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		return false
	}
	return info.IsDir()
}

func runSkillAuto(name, output string, archive, force bool) {
	cwd, _ := os.Getwd()
	result, err := skill.NewResolver(cwd).Resolve()
	if err != nil {
		log.Fatalf("Error: %v\n", err)
	}
	fmt.Println(skill.PrintDetectionSummary(result.Detection))

	switch result.Mode {
	case skill.ModeAlreadyExists:
		fmt.Printf("\nArk skill exists: %s\nUse: ark skill update\n", result.ExistingArk.Name)
		if !force {
			return
		}
	case skill.ModeRepository:
		fmt.Println("\nGenerating Repository Skill...")
		if name == "" {
			name = result.SuggestedName
		}
		if output == "" {
			output = result.SuggestedPath
		}
		analysis, _ := skill.NewAnalyzer(cwd).Analyze()
		skill.GenerateRepository(skill.RepositoryOptions{Name: name, Output: output, Archive: archive, Analysis: analysis})
		fmt.Printf("Created: %s\n", output)
	case skill.ModeExplorer:
		fmt.Println("\nGenerating Explorer Skill...")
		if name == "" {
			name = result.SuggestedName
		}
		if output == "" {
			output = result.SuggestedPath
		}
		skill.GenerateExplorer(skill.ExplorerOptions{Name: name, Output: output, Archive: archive})
		fmt.Printf("Created: %s\nExisting skills unchanged.\n", output)
	}
}

func runSkillInit() {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	name := fs.String("name", "repository-development", "Skill name")
	output := fs.String("output", "", "Output directory")
	archive := fs.Bool("archive", false, "Create ZIP archive")
	fs.Parse(os.Args[3:])

	cwd, _ := os.Getwd()
	result, _ := skill.NewResolver(cwd).ResolveForCommand("init")
	if result.Mode == skill.ModeAlreadyExists {
		fmt.Printf("Repository Skill exists: %s\n", result.ExistingArk.Name)
		return
	}
	if *output == "" {
		*output = result.SuggestedPath
	}
	analysis, _ := skill.NewAnalyzer(cwd).Analyze()
	skill.GenerateRepository(skill.RepositoryOptions{Name: *name, Output: *output, Archive: *archive, Analysis: analysis})
	fmt.Printf("Created: %s\n", *output)
}

func runSkillAddExplorer() {
	fs := flag.NewFlagSet("add-explorer", flag.ExitOnError)
	name := fs.String("name", "ark-code-explorer", "Skill name")
	output := fs.String("output", "", "Output directory")
	archive := fs.Bool("archive", false, "Create ZIP archive")
	fs.Parse(os.Args[3:])

	cwd, _ := os.Getwd()
	result, _ := skill.NewResolver(cwd).ResolveForCommand("add-explorer")
	if result.Mode == skill.ModeAlreadyExists {
		fmt.Printf("Explorer Skill exists: %s\n", result.ExistingArk.Name)
		return
	}
	if *output == "" {
		*output = result.SuggestedPath
	}
	skill.GenerateExplorer(skill.ExplorerOptions{Name: *name, Output: *output, Archive: *archive})
	fmt.Printf("Created: %s\n", *output)
}

func runSkillUpdate() {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	force := fs.Bool("force", false, "Force update even with conflicts")
	dryRun := fs.Bool("dry-run", false, "Show what would be updated")
	fs.Parse(os.Args[3:])

	cwd, _ := os.Getwd()
	updater := skill.NewUpdater(cwd)

	if *dryRun {
		conflicts := updater.CheckConflicts()
		arkSkills, _ := skill.NewDetector(cwd).FindArkSkills()
		fmt.Println("Skills to update:")
		for _, s := range arkSkills {
			modified := ""
			for _, c := range conflicts {
				if c == s.Name {
					modified = " (modified)"
				}
			}
			fmt.Printf("  %s (%s)%s\n", s.Name, s.SkillType, modified)
		}
		return
	}

	results, err := updater.Update(*force)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Print(skill.FormatResults(results))
}

func runSkillInspect() {
	cwd, _ := os.Getwd()
	result, _ := skill.NewResolver(cwd).Resolve()
	fmt.Println("=== Skills ===")
	fmt.Println(skill.PrintDetectionSummary(result.Detection))
	fmt.Printf("Mode: %s\n", result.Mode)

	analysis, _ := skill.NewAnalyzer(cwd).Analyze()
	if analysis != nil {
		fmt.Println("\n=== Analysis ===")
		fmt.Printf("Project: %s\nLanguage: %s\n", analysis.ProjectName, analysis.PrimaryLang)
	}
}
