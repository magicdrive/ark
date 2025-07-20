package ark

import (
	"fmt"
	"log"
	"os"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
	"github.com/magicdrive/ark/internal/mcp"
)

func Execute(version string) {
	if len(os.Args) > 2 && os.Args[1] == "mcp-server" {
		_, opt, err := commandline.ServerOptParse(version, os.Args[2:])
		if err != nil {
			log.Fatalf("Faital Error: %v\n", err)
		}
		mcp.RunMCPServe(opt.RootDir, opt)

	} else {
		_, opt, err := commandline.GeneralOptParse(os.Args[1:])
		if err != nil {
			log.Fatalf("Faital Error: %v\n", err)
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
