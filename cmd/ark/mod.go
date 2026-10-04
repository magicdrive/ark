package ark

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
	"github.com/magicdrive/ark/internal/mcp"
	"github.com/magicdrive/ark/internal/setup"
	"github.com/magicdrive/ark/internal/skill"
	"github.com/magicdrive/ark/internal/syntax"
)

func Execute(version string) {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "mcp-server":
			runMCPServer(version)
			return
		case "mcp-init":
			runMCPInitCommand()
			return
		case "setup":
			runSetupCommand()
			return
		case "syntax":
			runSyntaxCommand()
			return
		case "symbol":
			runSymbolCommand()
			return
		case "skill":
			runSkillCommand()
			return
		}
	}
	runDefaultCommand(version)
}

func resolveArkPath(optPath string) string {
	if optPath != "" {
		return optPath
	}
	return "ark"
}

func runSetupCommand() {
	_, opt, err := commandline.SetupOptParse(os.Args[2:])
	if err != nil {
		log.Fatalf("Fatal Error: %v\n", err)
	}
	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	// Resolve the target client. `ark setup` with no client stays a Claude Code
	// alias during v4.x, with a deprecation warning (plan §6).
	rawClient := opt.Client
	if rawClient == "" {
		fmt.Fprintln(os.Stderr, setup.DeprecatedNoClientWarning)
		fmt.Fprintln(os.Stderr)
		rawClient = string(setup.ClientClaude)
	}
	client, err := setup.ParseClientID(rawClient)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	res, err := setup.Run(setup.Options{
		Client:  client,
		Name:    opt.Name,
		ArkPath: opt.ArkPath,
		RootDir: opt.RootDir,
		Global:  opt.GlobalFlag,
		Force:   opt.ForceFlag,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
	}
	fmt.Print(res.Report())
}

func runMCPInitCommand() {
	_, opt, err := commandline.MCPInitOptParse(os.Args[2:])
	if err != nil {
		log.Fatalf("Fatal Error: %v\n", err)
	}
	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	arkPath := resolveArkPath(opt.ArkPath)

	rootDir := opt.RootDir
	if abs, err := filepath.Abs(rootDir); err == nil {
		rootDir = abs
	}

	if err := mcp.RunMCPInit(&mcp.MCPInitOptions{
		ArkPath:    arkPath,
		RootDir:    rootDir,
		ServerName: opt.ServerName,
		Global:     opt.GlobalFlag,
		Force:      opt.ForceFlag,
	}); err != nil {
		log.Fatalf("Error: %v\n", err)
	}

	if cwd, err := os.Getwd(); err == nil {
		mcp.SuggestCLAUDEMd(cwd)
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
	_, opt, err := commandline.SyntaxOptParse(os.Args[2:])
	if err != nil {
		log.Fatalf("Fatal Error: %v\n", err)
	}

	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	if opt.FilePath == "" {
		fmt.Println("Error: file path is required")
		opt.FlagSet.Usage()
		os.Exit(1)
	}

	opts := syntax.SyntaxOptions{
		FilePath: opt.FilePath,
		Lang:     opt.Lang,
		Format:   opt.Format,
	}

	if err := syntax.RunSyntaxCommand(opts); err != nil {
		log.Fatalf("Error: %v\n", err)
	}
}

func runSymbolCommand() {
	_, opt, err := commandline.SymbolOptParse(os.Args[2:])
	if err != nil {
		log.Fatalf("Fatal Error: %v\n", err)
	}

	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	if opt.FilePath == "" {
		fmt.Println("Error: file path is required")
		opt.FlagSet.Usage()
		os.Exit(1)
	}

	opts := syntax.SymbolOptions{
		FilePath: opt.FilePath,
		Lang:     opt.Lang,
		Format:   opt.Format,
	}

	if err := syntax.RunSymbolCommand(opts); err != nil {
		log.Fatalf("Error: %v\n", err)
	}
}

func runSkillCommand() {
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
	runSkillAuto()
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

func runSkillAuto() {
	_, opt, err := commandline.SkillAutoOptParse(os.Args[2:])
	if err != nil {
		log.Fatalf("Fatal Error: %v\n", err)
	}

	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Fatal Error: cannot determine current directory: %v\n", err)
	}
	result, err := skill.NewResolver(cwd).Resolve()
	if err != nil {
		log.Fatalf("Error: %v\n", err)
	}
	fmt.Println(skill.PrintDetectionSummary(result.Detection))

	name := opt.Name
	output := opt.Output

	switch result.Mode {
	case skill.ModeAlreadyExists:
		fmt.Printf("\nArk skill exists: %s\nUse: ark skill update\n", result.ExistingArk.Name)
		if !opt.ForceFlag {
			return
		}
		fmt.Println("\nForce regenerating skill...")
		existing := result.ExistingArk
		forceName := name
		if forceName == "" {
			forceName = existing.Name
		}
		forceOutput := output
		if forceOutput == "" {
			forceOutput = existing.Path
		}
		switch existing.SkillType {
		case skill.SkillTypeRepository:
			analysis, _ := skill.NewAnalyzer(cwd).Analyze()
			if err := skill.GenerateRepository(skill.RepositoryOptions{Name: forceName, Output: forceOutput, Archive: opt.ArchiveFlag, Analysis: analysis}); err != nil {
				log.Fatalf("Error: %v\n", err)
			}
		case skill.SkillTypeExplorer:
			if err := skill.GenerateExplorer(skill.ExplorerOptions{Name: forceName, Output: forceOutput, Archive: opt.ArchiveFlag}); err != nil {
				log.Fatalf("Error: %v\n", err)
			}
		}
		fmt.Printf("Overwritten: %s\n", forceOutput)
		return
	case skill.ModeRepository:
		fmt.Println("\nGenerating Repository Skill...")
		if name == "" {
			name = result.SuggestedName
		}
		if output == "" {
			output = result.SuggestedPath
		}
		analysis, _ := skill.NewAnalyzer(cwd).Analyze()
		if err := skill.GenerateRepository(skill.RepositoryOptions{Name: name, Output: output, Archive: opt.ArchiveFlag, Analysis: analysis}); err != nil {
			log.Fatalf("Error: %v\n", err)
		}
		fmt.Printf("Created: %s\n", output)
	case skill.ModeExplorer:
		fmt.Println("\nGenerating Explorer Skill...")
		if name == "" {
			name = result.SuggestedName
		}
		if output == "" {
			output = result.SuggestedPath
		}
		if err := skill.GenerateExplorer(skill.ExplorerOptions{Name: name, Output: output, Archive: opt.ArchiveFlag}); err != nil {
			log.Fatalf("Error: %v\n", err)
		}
		fmt.Printf("Created: %s\nExisting skills unchanged.\n", output)
	}
}

func runSkillInit() {
	_, opt, err := commandline.SkillInitOptParse(os.Args[3:])
	if err != nil {
		log.Fatalf("Fatal Error: %v\n", err)
	}

	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	cwd, _ := os.Getwd()
	result, err := skill.NewResolver(cwd).ResolveForCommand("init")
	if err != nil {
		log.Fatalf("Error: %v\n", err)
	}
	if result.Mode == skill.ModeAlreadyExists {
		fmt.Printf("Repository Skill exists: %s\n", result.ExistingArk.Name)
		return
	}
	output := opt.Output
	if output == "" {
		output = result.SuggestedPath
	}
	analysis, _ := skill.NewAnalyzer(cwd).Analyze()
	if err := skill.GenerateRepository(skill.RepositoryOptions{Name: opt.Name, Output: output, Archive: opt.ArchiveFlag, Analysis: analysis}); err != nil {
		log.Fatalf("Error: %v\n", err)
	}
	fmt.Printf("Created: %s\n", output)
}

func runSkillAddExplorer() {
	_, opt, err := commandline.SkillAddExplorerOptParse(os.Args[3:])
	if err != nil {
		log.Fatalf("Fatal Error: %v\n", err)
	}

	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	cwd, _ := os.Getwd()
	result, err := skill.NewResolver(cwd).ResolveForCommand("add-explorer")
	if err != nil {
		log.Fatalf("Error: %v\n", err)
	}
	if result.Mode == skill.ModeAlreadyExists {
		fmt.Printf("Explorer Skill exists: %s\n", result.ExistingArk.Name)
		return
	}
	output := opt.Output
	if output == "" {
		output = result.SuggestedPath
	}
	if err := skill.GenerateExplorer(skill.ExplorerOptions{Name: opt.Name, Output: output, Archive: opt.ArchiveFlag}); err != nil {
		log.Fatalf("Error: %v\n", err)
	}
	fmt.Printf("Created: %s\n", output)
}

func runSkillUpdate() {
	_, opt, err := commandline.SkillUpdateOptParse(os.Args[3:])
	if err != nil {
		log.Fatalf("Fatal Error: %v\n", err)
	}

	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	cwd, _ := os.Getwd()
	updater := skill.NewUpdater(cwd)

	if opt.DryRunFlag {
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

	results, err := updater.Update(opt.ForceFlag)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Print(skill.FormatResults(results))
}

func runSkillInspect() {
	_, opt, err := commandline.SkillInspectOptParse(os.Args[3:])
	if err != nil {
		log.Fatalf("Fatal Error: %v\n", err)
	}

	if opt.HelpFlag {
		opt.FlagSet.Usage()
		os.Exit(0)
	}

	cwd, _ := os.Getwd()
	result, err := skill.NewResolver(cwd).Resolve()
	if err != nil {
		log.Fatalf("Error: %v\n", err)
	}
	fmt.Println("=== Skills ===")
	fmt.Println(skill.PrintDetectionSummary(result.Detection))
	fmt.Printf("Mode: %s\n", result.Mode)

	analysis, _ := skill.NewAnalyzer(cwd).Analyze()
	if analysis != nil {
		fmt.Println("\n=== Analysis ===")
		fmt.Printf("Project: %s\nLanguage: %s\n", analysis.ProjectName, analysis.PrimaryLang)
	}
}
