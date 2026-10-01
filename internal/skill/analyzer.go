package skill

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RepoAnalysis holds the analysis results of a repository
type RepoAnalysis struct {
	RootDir       string
	Languages     []LanguageInfo
	PrimaryLang   string
	BuildCommands []BuildCommand
	HasTests      bool
	Modules       []ModuleInfo
	ProjectName   string
}

// LanguageInfo represents detected language information
type LanguageInfo struct {
	Name       string
	Extension  string
	FileCount  int
	Percentage float64
}

// BuildCommand represents a detected build/test command
type BuildCommand struct {
	Type    string // "build", "test", "lint", "format"
	Command string
	Source  string // e.g., "Makefile", "package.json"
}

// ModuleInfo represents a detected module/package
type ModuleInfo struct {
	Name string
	Path string
	Type string // "package", "module", "directory"
}

// Analyzer handles repository analysis
type Analyzer struct {
	rootDir string
}

// NewAnalyzer creates a new repository analyzer
func NewAnalyzer(rootDir string) *Analyzer {
	return &Analyzer{rootDir: rootDir}
}

// Analyze performs full repository analysis
func (a *Analyzer) Analyze() (*RepoAnalysis, error) {
	analysis := &RepoAnalysis{
		RootDir:     a.rootDir,
		ProjectName: filepath.Base(a.rootDir),
	}
	langMap := a.detectLanguages()
	analysis.Languages = a.sortLanguages(langMap)
	if len(analysis.Languages) > 0 {
		analysis.PrimaryLang = analysis.Languages[0].Name
	}
	analysis.BuildCommands = a.detectBuildCommands()
	analysis.Modules = a.detectModules()
	analysis.HasTests = a.hasTests()
	return analysis, nil
}

func (a *Analyzer) detectLanguages() map[string]int {
	langCount := make(map[string]int)
	filepath.Walk(a.rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(a.rootDir, path)
		if shouldSkipPath(rel) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if lang := extToLanguage(ext); lang != "" {
			langCount[lang]++
		}
		return nil
	})
	return langCount
}

func (a *Analyzer) sortLanguages(langMap map[string]int) []LanguageInfo {
	var total int
	for _, count := range langMap {
		total += count
	}
	var langs []LanguageInfo
	for name, count := range langMap {
		langs = append(langs, LanguageInfo{
			Name: name, Extension: languageToExt(name), FileCount: count,
			Percentage: float64(count) / float64(total) * 100,
		})
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i].FileCount > langs[j].FileCount })
	return langs
}
