package skill

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/magicdrive/ark/internal/testfiles"
)

func (a *Analyzer) detectBuildCommands() []BuildCommand {
	var commands []BuildCommand
	if fileExists(filepath.Join(a.rootDir, "go.mod")) {
		commands = append(commands,
			BuildCommand{Type: "build", Command: "go build ./...", Source: "go.mod"},
			BuildCommand{Type: "test", Command: "go test ./...", Source: "go.mod"},
		)
	}
	if fileExists(filepath.Join(a.rootDir, "Makefile")) {
		for _, t := range a.parseMakefileTargets() {
			commands = append(commands, BuildCommand{Type: t.Type, Command: "make " + t.Name, Source: "Makefile"})
		}
	}
	if fileExists(filepath.Join(a.rootDir, "package.json")) {
		commands = append(commands,
			BuildCommand{Type: "build", Command: "npm run build", Source: "package.json"},
			BuildCommand{Type: "test", Command: "npm test", Source: "package.json"},
		)
	}
	if fileExists(filepath.Join(a.rootDir, "Cargo.toml")) {
		commands = append(commands,
			BuildCommand{Type: "build", Command: "cargo build", Source: "Cargo.toml"},
			BuildCommand{Type: "test", Command: "cargo test", Source: "Cargo.toml"},
		)
	}
	if fileExists(filepath.Join(a.rootDir, "pyproject.toml")) || fileExists(filepath.Join(a.rootDir, "setup.py")) {
		commands = append(commands, BuildCommand{Type: "test", Command: "pytest", Source: "python"})
	}
	return commands
}

type makeTarget struct{ Name, Type string }

func (a *Analyzer) parseMakefileTargets() []makeTarget {
	content, err := os.ReadFile(filepath.Join(a.rootDir, "Makefile"))
	if err != nil {
		return nil
	}
	var targets []makeTarget
	known := map[string]string{"build": "build", "test": "test", "lint": "lint", "format": "format", "fmt": "format"}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, ".") {
			target := strings.TrimSuffix(line, ":")
			if typ, ok := known[target]; ok {
				targets = append(targets, makeTarget{target, typ})
			}
		}
	}
	return targets
}

func (a *Analyzer) detectModules() []ModuleInfo {
	var modules []ModuleInfo
	if fileExists(filepath.Join(a.rootDir, "go.mod")) {
		for _, dir := range []string{"cmd", "internal", "pkg"} {
			d := filepath.Join(a.rootDir, dir)
			if dirExists(d) {
				entries, _ := os.ReadDir(d)
				for _, e := range entries {
					if e.IsDir() {
						typ := "package"
						if dir == "cmd" {
							typ = "command"
						}
						modules = append(modules, ModuleInfo{Name: e.Name(), Path: dir + "/" + e.Name(), Type: typ})
					}
				}
			}
		}
	}
	if fileExists(filepath.Join(a.rootDir, "package.json")) {
		if dirExists(filepath.Join(a.rootDir, "src")) {
			entries, _ := os.ReadDir(filepath.Join(a.rootDir, "src"))
			for _, e := range entries {
				if e.IsDir() {
					modules = append(modules, ModuleInfo{Name: e.Name(), Path: "src/" + e.Name(), Type: "module"})
				}
			}
		}
	}
	return modules
}

func (a *Analyzer) hasTests() bool {
	hasTest := false
	filepath.Walk(a.rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(a.rootDir, path)
		if err != nil {
			return nil
		}
		if testfiles.IsTestFile(rel) {
			hasTest = true
			return filepath.SkipAll
		}
		return nil
	})
	return hasTest
}

func shouldSkipPath(rel string) bool {
	for _, p := range strings.Split(rel, string(filepath.Separator)) {
		if strings.HasPrefix(p, ".") || p == "vendor" || p == "node_modules" {
			return true
		}
	}
	return false
}

func extToLanguage(ext string) string {
	m := map[string]string{
		".go": "Go", ".js": "JavaScript", ".ts": "TypeScript", ".py": "Python",
		".rs": "Rust", ".java": "Java", ".rb": "Ruby", ".c": "C", ".h": "C",
		".cpp": "C++", ".cc": "C++", ".cs": "C#", ".php": "PHP", ".swift": "Swift",
		".kt": "Kotlin", ".scala": "Scala", ".sh": "Shell", ".bash": "Shell",
	}
	return m[ext]
}

func languageToExt(lang string) string {
	m := map[string]string{"Go": ".go", "JavaScript": ".js", "TypeScript": ".ts", "Python": ".py", "Rust": ".rs", "Java": ".java"}
	return m[lang]
}
