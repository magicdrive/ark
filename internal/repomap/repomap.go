package repomap

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// DetailLevel controls how much information appears in the map.
type DetailLevel string

const (
	DetailMinimal DetailLevel = "minimal" // package paths only
	DetailNormal  DetailLevel = "normal"  // top-ranked symbols + dependencies
	DetailVerbose DetailLevel = "verbose" // all exported symbols + dependencies
)

// Options configures map generation.
type Options struct {
	DetailLevel DetailLevel
	MaxSymbols  int // symbols per package (default 10)
	MaxPackages int // maximum packages (default 50)
}

func (o *Options) withDefaults() Options {
	out := *o
	if out.DetailLevel == "" {
		out.DetailLevel = DetailNormal
	}
	if out.MaxSymbols <= 0 {
		out.MaxSymbols = 10
	}
	if out.MaxPackages <= 0 {
		out.MaxPackages = 50
	}
	return out
}

// SymbolEntry is a ranked symbol for display.
type SymbolEntry struct {
	Name          string
	Qualified     string
	Kind          symbol.SymbolKind
	Exported      bool
	InboundEdges  int
	OutboundEdges int
	score         int
}

// PackageEntry represents one logical package/directory.
type PackageEntry struct {
	Path        string
	Language    string
	Symbols     []SymbolEntry
	IsEntry     bool
	IsTest      bool
	IsGenerated bool
	IsVendor    bool
	fileCount   int
}

// DependencyEdge is an import relationship between packages.
type DependencyEdge struct {
	From string
	To   string
}

// RepositoryMap is a compact logical view of a repository.
type RepositoryMap struct {
	RootPath     string
	Packages     []PackageEntry
	Dependencies []DependencyEdge
	Languages    map[string]int // language → file count
	TotalFiles   int
	TotalSymbols int
	Skipped      []string
}

// Build constructs a RepositoryMap from an existing RepositoryIndex.
func Build(idx *index.RepositoryIndex, root string, opts Options) *RepositoryMap {
	opts = opts.withDefaults()

	// Pre-compute inbound/outbound edge counts per symbol.
	inbound := make(map[symbol.SymbolID]int)
	outbound := make(map[symbol.SymbolID]int)
	for _, file := range idx.Files() {
		for _, sym := range idx.SymbolsByFile(file) {
			for range idx.GetCallers(sym.ID) {
				inbound[sym.ID]++
			}
			for range idx.GetCallees(sym.ID) {
				outbound[sym.ID]++
			}
		}
	}

	// Group files by directory (= package).
	type pkgKey = string
	pkgFiles := make(map[pkgKey][]source.FileID)
	pkgLang := make(map[pkgKey]string)
	for _, fid := range idx.Files() {
		dir := filepath.Dir(string(fid))
		if dir == "." {
			dir = ""
		}
		pkgFiles[dir] = append(pkgFiles[dir], fid)
		if lang := fileLang(idx, fid); lang != "" && pkgLang[dir] == "" {
			pkgLang[dir] = lang
		}
	}

	stats := idx.Stats()
	rm := &RepositoryMap{
		RootPath:     root,
		Languages:    stats.Languages,
		TotalFiles:   stats.Files,
		TotalSymbols: stats.Symbols,
	}

	// Sort package paths for determinism.
	var pkgPaths []string
	for p := range pkgFiles {
		pkgPaths = append(pkgPaths, p)
	}
	sort.Strings(pkgPaths)

	// Dependency edges: collect from import references.
	depSet := make(map[[2]string]bool)

	for _, pkg := range pkgPaths {
		files := pkgFiles[pkg]
		entry := PackageEntry{
			Path:      pkg,
			Language:  pkgLang[pkg],
			fileCount: len(files),
		}

		entry.IsVendor = strings.Contains(pkg, "vendor/") || strings.HasPrefix(pkg, "vendor")
		entry.IsTest = isTestPackage(pkg, files)
		if !entry.IsVendor {
			entry.IsGenerated = isGeneratedPackage(root, files)
		}
		entry.IsEntry = isEntryPoint(pkg, idx, files)

		// Collect symbols for this package.
		var symEntries []SymbolEntry
		for _, fid := range files {
			isTestFile := isTestFileID(string(fid))
			for _, sym := range idx.SymbolsByFile(fid) {
				if opts.DetailLevel != DetailVerbose && !sym.Exported {
					continue
				}
				// Skip test helper symbols from test files in non-test packages.
				if isTestFile && !entry.IsTest && opts.DetailLevel != DetailVerbose {
					continue
				}
				se := SymbolEntry{
					Name:          sym.Name,
					Qualified:     sym.Qualified,
					Kind:          sym.Kind,
					Exported:      sym.Exported,
					InboundEdges:  inbound[sym.ID],
					OutboundEdges: outbound[sym.ID],
				}
				se.score = scoreSymbol(se, entry)
				symEntries = append(symEntries, se)
			}
		}

		// Rank and trim.
		sort.Slice(symEntries, func(i, j int) bool {
			if symEntries[i].score != symEntries[j].score {
				return symEntries[i].score > symEntries[j].score
			}
			return symEntries[i].Name < symEntries[j].Name
		})
		if len(symEntries) > opts.MaxSymbols {
			symEntries = symEntries[:opts.MaxSymbols]
		}
		entry.Symbols = symEntries

		// Collect import-based dependency edges.
		for _, fid := range files {
			for _, ref := range idx.ReferencesByFile(fid) {
				if ref.Kind != "import" {
					continue
				}
				// ref.Name is the import path; derive the package dir.
				toPkg := importPathToRelDir(root, string(ref.Name))
				if toPkg != "" && toPkg != pkg {
					key := [2]string{pkg, toPkg}
					depSet[key] = true
				}
			}
		}

		rm.Packages = append(rm.Packages, entry)
	}

	// Trim to MaxPackages by entry-point / non-generated first.
	sort.SliceStable(rm.Packages, func(i, j int) bool {
		pi, pj := rm.Packages[i], rm.Packages[j]
		si := packageScore(pi)
		sj := packageScore(pj)
		if si != sj {
			return si > sj
		}
		return pi.Path < pj.Path
	})
	if len(rm.Packages) > opts.MaxPackages {
		for _, p := range rm.Packages[opts.MaxPackages:] {
			rm.Skipped = append(rm.Skipped, p.Path)
		}
		rm.Packages = rm.Packages[:opts.MaxPackages]
	}

	// Collect dependency edges between surviving packages.
	pkgSet := make(map[string]bool)
	for _, p := range rm.Packages {
		pkgSet[p.Path] = true
	}
	for key := range depSet {
		if pkgSet[key[0]] && pkgSet[key[1]] {
			rm.Dependencies = append(rm.Dependencies, DependencyEdge{From: key[0], To: key[1]})
		}
	}
	sort.Slice(rm.Dependencies, func(i, j int) bool {
		if rm.Dependencies[i].From != rm.Dependencies[j].From {
			return rm.Dependencies[i].From < rm.Dependencies[j].From
		}
		return rm.Dependencies[i].To < rm.Dependencies[j].To
	})

	return rm
}

// Format returns a human-readable text representation.
func (m *RepositoryMap) Format() string {
	var sb strings.Builder

	// Header.
	langSummary := formatLangSummary(m.Languages)
	fmt.Fprintf(&sb, "%s (%s, %d files, %d symbols)\n",
		filepath.Base(m.RootPath), langSummary, m.TotalFiles, m.TotalSymbols)

	for _, pkg := range m.Packages {
		sb.WriteString("\n")
		label := pkg.Path
		if label == "" {
			label = "."
		}
		tags := ""
		if pkg.IsEntry {
			tags += " [entry]"
		}
		if pkg.IsGenerated {
			tags += " [generated]"
		}
		if pkg.IsVendor {
			tags += " [vendor]"
		}
		if pkg.IsTest {
			tags += " [test]"
		}
		fmt.Fprintf(&sb, "%s%s\n", label, tags)
		for _, sym := range pkg.Symbols {
			fmt.Fprintf(&sb, "  %s\n", sym.Name)
		}
	}

	if len(m.Dependencies) > 0 {
		sb.WriteString("\nDependencies:\n")
		for _, dep := range m.Dependencies {
			from := dep.From
			if from == "" {
				from = "."
			}
			to := dep.To
			if to == "" {
				to = "."
			}
			fmt.Fprintf(&sb, "  %s → %s\n", from, to)
		}
	}

	return sb.String()
}

// --- scoring ---

func scoreSymbol(se SymbolEntry, pkg PackageEntry) int {
	score := 0
	if se.Name == "main" || se.Name == "__main__" {
		score += 100
	}
	if se.Exported {
		score += 50
	}
	score += se.InboundEdges * 10
	score += se.OutboundEdges * 5
	if pkg.IsGenerated {
		score -= 20
	}
	if pkg.IsTest {
		score -= 10
	}
	return score
}

func packageScore(p PackageEntry) int {
	score := 0
	if p.IsEntry {
		score += 200
	}
	if p.IsVendor || p.IsGenerated {
		score -= 100
	}
	if p.IsTest {
		score -= 30
		// testdata directories are fixture-only; exclude from default map view.
		if strings.Contains(p.Path, "testdata") {
			score -= 1000
		}
	}
	score += len(p.Symbols) * 2
	return score
}

// --- helpers ---

func fileLang(idx *index.RepositoryIndex, fid source.FileID) string {
	syms := idx.SymbolsByFile(fid)
	if len(syms) > 0 {
		return syms[0].Language
	}
	return ""
}

func isTestFileID(fid string) bool {
	name := filepath.Base(fid)
	return strings.HasSuffix(name, "_test.go") ||
		strings.HasSuffix(name, "_test.ts") ||
		strings.HasSuffix(name, "_test.js") ||
		strings.HasPrefix(name, "test_")
}

func isTestPackage(pkgPath string, files []source.FileID) bool {
	// testdata directories are fixture data only — treat as test.
	if strings.Contains(pkgPath, "testdata") {
		return true
	}
	// Only mark as a test package when every file is a test file.
	// Regular packages that have test files alongside source are NOT test packages.
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		name := filepath.Base(string(f))
		if !strings.HasSuffix(name, "_test.go") &&
			!strings.HasSuffix(name, "_test.ts") &&
			!strings.HasSuffix(name, "_test.js") &&
			!strings.HasPrefix(name, "test_") {
			return false
		}
	}
	return true
}

func isGeneratedPackage(root string, files []source.FileID) bool {
	for _, fid := range files {
		full := filepath.Join(root, string(fid))
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		// Check first 512 bytes for generation markers.
		head := string(data)
		if len(head) > 512 {
			head = head[:512]
		}
		if strings.Contains(head, "Code generated") || strings.Contains(head, "DO NOT EDIT") {
			return true
		}
	}
	return false
}

func isEntryPoint(pkg string, idx *index.RepositoryIndex, files []source.FileID) bool {
	base := filepath.Base(pkg)
	if base == "main" || base == "cmd" {
		return true
	}
	// Check if any file has a "main" function.
	for _, fid := range files {
		for _, sym := range idx.SymbolsByFile(fid) {
			if sym.Name == "main" && sym.Kind == symbol.KindFunction {
				return true
			}
		}
	}
	return false
}

// importPathToRelDir converts an import path to a repo-relative directory, best-effort.
// For internal paths like "github.com/magicdrive/ark/internal/mcp" → "internal/mcp".
func importPathToRelDir(root, importPath string) string {
	// Try to find a matching subdirectory under root.
	// Split on "/" and walk from the last component backward.
	parts := strings.Split(importPath, "/")
	for i := 0; i < len(parts); i++ {
		candidate := strings.Join(parts[i:], string(filepath.Separator))
		if _, err := os.Stat(filepath.Join(root, candidate)); err == nil {
			return candidate
		}
	}
	return ""
}

func formatLangSummary(langs map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var pairs []kv
	for k, v := range langs {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	var parts []string
	for _, p := range pairs {
		parts = append(parts, p.k)
	}
	return strings.Join(parts, ", ")
}
