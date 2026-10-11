// Package perffixture generates the synthetic repositories Ark's performance
// benchmarks run on: deterministic (the same spec writes the same bytes),
// self-contained (no secret, no file of the machine it runs on), and shaped
// to separate what costs what — size, depth, nested ignore rules, large
// files. Tests and benchmarks only.
package perffixture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Spec describes a repository.
type Spec struct {
	Name         string
	Fanout       int  // subdirectories of each directory above the leaves
	Depth        int  // directory levels below the root
	FilesPerDir  int  // Go source files in every directory (the root too)
	FuncsPerFile int  // functions per source file
	FuncBody     int  // extra statement lines per function (file size)
	NestedRules  bool // a .arkignore in every directory, excluding some files
	TextFiles    int  // non-source text files per directory
}

// Specs are the repositories the benchmarks measure.
var Specs = []Spec{
	{Name: "small", Fanout: 4, Depth: 1, FilesPerDir: 20, FuncsPerFile: 8},
	{Name: "medium-shallow", Fanout: 40, Depth: 1, FilesPerDir: 25, FuncsPerFile: 8, TextFiles: 2},
	{Name: "medium-deep", Fanout: 2, Depth: 8, FilesPerDir: 2, FuncsPerFile: 8, TextFiles: 1},
	{Name: "medium-nested-rules", Fanout: 40, Depth: 1, FilesPerDir: 25, FuncsPerFile: 8, TextFiles: 2, NestedRules: true},
	{Name: "large", Fanout: 20, Depth: 2, FilesPerDir: 12, FuncsPerFile: 8, TextFiles: 1},
	{Name: "bigfiles", Fanout: 2, Depth: 1, FilesPerDir: 6, FuncsPerFile: 400, FuncBody: 20},
}

// ByName returns the spec named name.
func ByName(name string) (Spec, bool) {
	for _, s := range Specs {
		if s.Name == name {
			return s, true
		}
	}
	return Spec{}, false
}

// Stats describes a written repository.
type Stats struct {
	Files, Dirs, RuleFiles, Rules, SourceFiles int
	Bytes                                      int64
}

func (s Stats) String() string {
	return fmt.Sprintf("files=%d (sources %d) dirs=%d bytes=%d ruleFiles=%d rules=%d",
		s.Files, s.SourceFiles, s.Dirs, s.Bytes, s.RuleFiles, s.Rules)
}

// Write writes the repository of spec at root (created) and describes it.
func Write(root string, spec Spec) (Stats, error) {
	var st Stats
	put := func(rel, body string) error {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		st.Files++
		st.Bytes += int64(len(body))
		return os.WriteFile(p, []byte(body), 0o644)
	}
	if err := put("go.mod", "module example.com/bench\n\ngo 1.22\n"); err != nil {
		return st, err
	}
	if err := put(".arkignore", "*.log\nbuild/\n"); err != nil {
		return st, err
	}
	st.RuleFiles, st.Rules = 1, 2
	var dirs []string
	var walk func(rel string, depth int)
	walk = func(rel string, depth int) {
		dirs = append(dirs, rel)
		if depth == spec.Depth {
			return
		}
		for i := 0; i < spec.Fanout; i++ {
			walk(joinRel(rel, fmt.Sprintf("d%02d", i)), depth+1)
		}
	}
	walk("", 0)
	for di, dir := range dirs {
		st.Dirs++
		pkg := fmt.Sprintf("p%d", di)
		for f := 0; f < spec.FilesPerDir; f++ {
			if err := put(joinRel(dir, fmt.Sprintf("f%03d.go", f)), source(pkg, di, f, spec)); err != nil {
				return st, err
			}
			st.SourceFiles++
		}
		for f := 0; f < spec.TextFiles; f++ {
			if err := put(joinRel(dir, fmt.Sprintf("notes%d.txt", f)), strings.Repeat(fmt.Sprintf("notes %d %d: Helper and Shared are described here.\n", di, f), 20)); err != nil {
				return st, err
			}
		}
		if err := put(joinRel(dir, "run.log"), "excluded by the root rule\n"); err != nil {
			return st, err
		}
		if spec.NestedRules && dir != "" {
			rules := []string{"secret_*.go", "*.tmp", "/generated/", "!keep_secret.go", "cache/", "*.bak", "vendor_copy/", "/local.go", "**/fixtures/*.json", "tmp*"}
			if err := put(joinRel(dir, ".arkignore"), strings.Join(rules, "\n")+"\n"); err != nil {
				return st, err
			}
			st.RuleFiles++
			st.Rules += len(rules)
			for _, name := range []string{"secret_a.go", "keep_secret.go", "local.go", "x.tmp"} {
				if err := put(joinRel(dir, name), source(pkg, di, 900+len(name), Spec{FuncsPerFile: 2})); err != nil {
					return st, err
				}
			}
		}
	}
	return st, nil
}

func joinRel(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// source is a Go file of package pkg: functions calling each other, a
// shared Helper and Shared, so relation tools have work to do.
func source(pkg string, dir, file int, spec Spec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	if file == 0 {
		b.WriteString("// Helper is called from every file of the package.\nfunc Helper(n int) int { return n + 1 }\n\n")
		b.WriteString("// Shared is a type every file uses.\ntype Shared struct{ N int }\n\n// Value returns the value.\nfunc (s *Shared) Value() int { return Helper(s.N) }\n\n")
	}
	for i := 0; i < spec.FuncsPerFile; i++ {
		fmt.Fprintf(&b, "// F%d_%d_%d is function %d of file %d.\nfunc F%d_%d_%d(x int) int {\n", dir, file, i, i, file, dir, file, i)
		for j := 0; j < spec.FuncBody; j++ {
			fmt.Fprintf(&b, "\tx = x*%d + %d // step %d\n", j+3, j, j)
		}
		if i > 0 {
			fmt.Fprintf(&b, "\tx = F%d_%d_%d(x)\n", dir, file, i-1)
		}
		b.WriteString("\ts := &Shared{N: x}\n\treturn Helper(s.Value())\n}\n\n")
	}
	return b.String()
}
