// Package manifest reads the meaning of two TOML manifests from the tree
// tomldoc built: a Cargo manifest (Cargo.toml) and a Python project's
// pyproject.toml. It states what the file's own text fixes, and evaluates
// nothing: no cfg() expression, no version requirement, no environment
// marker, no glob, no build script; nothing is fetched or run.
//
// Identities are the TOML layer's JSON Pointers ("<file>#/package"), so a
// declaration of meaning is the key or table it is written as, with
// another kind:
//
//   - Cargo: the package is its [package] table (kind package, named by
//     its name); a dependency is its entry in [dependencies],
//     [dev-dependencies], [build-dependencies] (and their legacy
//     underscore spellings), [target.<cfg>.*-dependencies] and
//     [workspace.dependencies] (kind dependency, named by its key — the name
//     the code uses; `package = "..."` names the crate).
//   - pyproject: the project is its [project] table (kind package, named by
//     its name); a requirement of project.dependencies,
//     project.optional-dependencies.<extra>, dependency-groups.<group>
//     (PEP 735) and build-system.requires is a dependency named by its
//     PEP 508 name, at "<array>/<normalized name>" ("#<n>" for the n-th of
//     one name in one array): an array element has no key of its own.
//
// Relations, all evidence of the file alone:
//
//   - depends_on: the package its dependencies (Exact: the file's own
//     structure); a dependency what it names — with `path`, the package of
//     the manifest in that directory ("<dir>/Cargo.toml#/package", in the
//     repository: Cargo reads exactly that file; a path leaving the
//     repository is outside it), with `workspace = true` the workspace's
//     entry of its key (in this file when it is the workspace root, in
//     package.workspace's manifest when that names it; else Unresolved: the
//     root is the nearest ancestor manifest with [workspace], which one file
//     cannot see), else a crate of a registry or a git repository, or a
//     Python distribution — outside the repository ("cargo:crates.io/<crate>",
//     "cargo:registry:<name>/<crate>", "cargo:git:<url>#<crate>",
//     "pypi:<name>"; the two ecosystems never share an identity);
//   - contains: the workspace the package of each literal member directory
//     (a glob member is Unresolved: Ark expands no glob);
//   - references: a Cargo feature the features and dependencies it enables
//     ("x", "dep:x", "x/f", "x?/f"); several dependencies of one name (in
//     [dependencies] and a [target] table) leave it a Candidate.
package manifest

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/magicdrive/ark/internal/languages/internal/declname"
	"github.com/magicdrive/ark/internal/languages/toml/tomldoc"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// Problem codes.
const (
	CodeInvalidRequirement = "pyproject_invalid_requirement"
)

// Decl is a declaration the TOML layer does not state: a requirement of an
// array. Parent is the node (an array value) it is an element of.
type Decl struct {
	Name, Qualified string
	Kind            symbol.SymbolKind
	Span            tomldoc.Span
	Parent          *tomldoc.Node
	Signature       string
}

// Override restates the declaration of a node of the TOML layer.
type Override struct {
	Kind            symbol.SymbolKind
	Name, Signature string
}

// Ref is a reference the file states. Local marks a NameQualified of this
// file (a declaration the provider may have left out); InRepository one
// that can only be the repository's; Cap bounds its confidence.
type Ref struct {
	Name, Kind, Container, NameQualified, TargetKinds, Cap string
	Span                                                   tomldoc.Span
	InRepository, Local                                    bool
}

// Result is what a manifest states.
type Result struct {
	Overrides map[*tomldoc.Node]Override
	Decls     []Decl
	Refs      []Ref
	Problems  []tomldoc.Problem
}

// Identity names the identity the TOML layer gives a node ("" for none).
type Identity func(*tomldoc.Node) string

// Read reads file as a manifest; ok is false for a file that is none.
func Read(file string, d *tomldoc.Doc, id Identity) (Result, bool) {
	slash := strings.ReplaceAll(file, "\\", "/")
	sep := "/"
	if strings.Contains(file, "\\") && !strings.Contains(file, "/") {
		sep = "\\" // the index's file IDs separate with the OS's separator (Windows)
	}
	r := &reader{file: file, dir: path.Dir(slash), sep: sep, d: d, id: id, out: Result{Overrides: map[*tomldoc.Node]Override{}}}
	switch path.Base(slash) {
	case "Cargo.toml":
		r.cargo()
	case "pyproject.toml":
		r.pyproject()
	default:
		return Result{}, false
	}
	return r.out, true
}

type reader struct {
	file, dir, sep string
	d              *tomldoc.Doc
	id             Identity
	out            Result
}

// filePath spells a slash-separated repository path as the index's file
// IDs are spelled.
func (r *reader) filePath(p string) string { return strings.ReplaceAll(p, "/", r.sep) }

// repoPath joins a manifest-relative path to the manifest's directory: ok
// is false for one that leaves the repository or is absolute.
func (r *reader) repoPath(rel string) (string, bool) {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") || len(rel) > 1 && rel[1] == ':' {
		return "", false
	}
	p := path.Clean(r.dir + "/" + rel)
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", false
	}
	return p, true
}

// manifestIdentity is the identity of the package of the Cargo manifest
// in the repository directory dir.
func (r *reader) manifestIdentity(dir, name, pointer string) string {
	f := name
	if dir != "." {
		f = dir + "/" + name
	}
	return r.filePath(f) + "#" + pointer
}

func (r *reader) ref(x Ref) {
	if x.Container == "" || !declname.IsName(x.Name) {
		return
	}
	r.out.Refs = append(r.out.Refs, x)
}

// ---- Cargo ----

var depTables = []string{"dependencies", "dev-dependencies", "dev_dependencies", "build-dependencies", "build_dependencies"}

type dep struct {
	n      *tomldoc.Node
	normal bool // of [dependencies] or [target.<cfg>.dependencies]: one a feature may enable
}

func (r *reader) cargo() {
	root := r.d.Root
	pkg := root.Get("package")
	pkgID := ""
	if name, ok := pkg.Get("name").String(); ok && pkg.Kind == tomldoc.Table && declname.IsName(name) {
		pkgID = r.id(pkg)
		sig := "package " + name
		if v, ok := pkg.Get("version").String(); ok && declname.IsName(v) {
			sig += " " + v
		}
		r.out.Overrides[pkg] = Override{Kind: symbol.KindPackage, Name: name, Signature: sig}
	}
	ws := root.Get("workspace")
	if ws != nil && ws.Kind != tomldoc.Table {
		ws = nil
	}
	// The workspace root whose [workspace.dependencies] `workspace = true`
	// inherits from: this manifest, or the one package.workspace names.
	wsDeps := ""
	switch rel, ok := pkg.Get("workspace").String(); {
	case ws != nil:
		wsDeps = r.manifestIdentity(r.dir, "Cargo.toml", "/workspace/dependencies")
	case ok:
		if p, ok := r.repoPath(rel); ok {
			wsDeps = r.manifestIdentity(p, "Cargo.toml", "/workspace/dependencies")
		}
	}

	var deps []dep
	collect := func(t *tomldoc.Node, owner string) {
		for _, name := range depTables {
			tbl := t.Get(name)
			if tbl == nil || tbl.Kind != tomldoc.Table {
				continue
			}
			for _, e := range tbl.Children {
				if r.dependency(e, owner, wsDeps) {
					deps = append(deps, dep{e, name == "dependencies"})
				}
			}
		}
	}
	collect(root, pkgID)
	if tg := root.Get("target"); tg != nil && tg.Kind == tomldoc.Table {
		for _, cfg := range tg.Children {
			if cfg.Kind == tomldoc.Table {
				collect(cfg, pkgID)
			}
		}
	}
	if ws != nil {
		if tbl := ws.Get("dependencies"); tbl != nil && tbl.Kind == tomldoc.Table {
			for _, e := range tbl.Children {
				r.dependency(e, "", "")
			}
		}
		r.members(ws)
	}
	r.features(root.Get("features"), deps)
}

// dependency states the entry e of a dependency table: a dependency, the
// owner package's depends_on it, and its own depends_on what it names.
func (r *reader) dependency(e *tomldoc.Node, owner, wsDeps string) bool {
	if e.Kind == tomldoc.ArrayOfTables || e.Kind == tomldoc.Value && e.VKind != tomldoc.String {
		return false
	}
	q := r.id(e)
	if q == "" || !declname.IsName(e.Key) {
		return false // an entry named by no one-line name ("") stays a key
	}
	crate := e.Key
	if p, ok := e.Get("package").String(); ok && declname.IsName(p) {
		crate = p
	}
	at := e.KeySpan
	x := Ref{Name: crate, Kind: string(reference.KindExplicitDependency), Container: q, Span: at}
	source := "crates.io"
	pathVal, hasPath := e.Get("path").String()
	gitVal, hasGit := e.Get("git").String()
	regVal, hasReg := e.Get("registry").String()
	switch {
	case e.Get("workspace").IsTrue():
		source = "workspace"
		x.Name = e.Key // the workspace's entry of the same key
		x.InRepository, x.TargetKinds = true, string(symbol.KindDependency)
		if wsDeps != "" {
			x.NameQualified = wsDeps + "/" + pointer(e.Key)
			x.Local = strings.HasPrefix(wsDeps, r.file+"#")
		} else {
			// The root is the nearest ancestor manifest with [workspace]:
			// in the repository, but which one file cannot see. An
			// identity no declaration carries leaves it Unresolved —
			// neither outside the repository nor any namesake guessed.
			x.NameQualified = "cargo:workspace-root/dependencies/" + pointer(e.Key)
		}
	case hasPath:
		source = "path " + pathVal
		if p, ok := r.repoPath(pathVal); ok {
			x.NameQualified, x.InRepository = r.manifestIdentity(p, "Cargo.toml", "/package"), true
		} else {
			x.NameQualified = "cargo:path:" + crate // outside the repository
		}
		x.TargetKinds = string(symbol.KindPackage)
	case hasGit:
		source = "git"
		x.NameQualified = "cargo:git:" + withoutUserinfo(gitVal) + "#" + crate
	case hasReg:
		source = "registry " + regVal
		x.NameQualified = "cargo:registry:" + regVal + "/" + crate
	default:
		x.NameQualified = "cargo:crates.io/" + crate
	}
	if !declname.IsName(x.NameQualified) {
		x.NameQualified = "cargo:" + strings.SplitN(source, " ", 2)[0] + "/" + crate
	}
	if !declname.IsName(source) {
		source = strings.SplitN(source, " ", 2)[0]
	}
	sig := "dependency " + source
	if crate != e.Key {
		sig = "dependency " + crate + " " + source
	}
	r.out.Overrides[e] = Override{Kind: symbol.KindDependency, Name: e.Key, Signature: sig}
	r.ref(x)
	if owner != "" {
		r.ref(Ref{Name: e.Key, Kind: string(reference.KindExplicitDependency), Container: owner, Span: at,
			NameQualified: q, InRepository: true, Local: true, TargetKinds: string(symbol.KindDependency)})
	}
	return true
}

// members states the workspace's literal members as the packages it
// contains.
func (r *reader) members(ws *tomldoc.Node) {
	m := ws.Get("members")
	if m == nil || m.VKind != tomldoc.Array {
		return
	}
	// exclude names no literal member out: Cargo applies it to what a glob
	// or a path dependency would add, never to a member written as is.
	wsID := r.id(ws)
	for _, s := range m.Strs {
		x := Ref{Name: s.Value, Kind: string(reference.KindContains), Container: wsID, Span: s.Span, TargetKinds: string(symbol.KindPackage)}
		switch p, ok := r.repoPath(s.Value); {
		case strings.ContainsAny(s.Value, "*?["):
			// A glob names the directories it matches: Ark expands none,
			// so the member is unresolved — never one of them guessed.
			x.NameQualified, x.InRepository = r.manifestIdentity(r.dir, "Cargo.toml", "/workspace/members/"+pointer(s.Value)), true
		case ok:
			x.NameQualified, x.InRepository = r.manifestIdentity(p, "Cargo.toml", "/package"), true
		default:
			x.NameQualified = "cargo:path:" + s.Value
		}
		if declname.IsName(x.NameQualified) {
			r.ref(x)
		}
	}
}

// features states what each feature enables.
func (r *reader) features(f *tomldoc.Node, deps []dep) {
	if f == nil || f.Kind != tomldoc.Table {
		return
	}
	byName := map[string][]*tomldoc.Node{}
	for _, d := range deps {
		if d.normal {
			byName[d.n.Key] = append(byName[d.n.Key], d.n)
		}
	}
	for _, feat := range f.Children {
		from := r.id(feat)
		if from == "" || feat.VKind != tomldoc.Array {
			continue
		}
		for _, s := range feat.Strs {
			x := Ref{Name: s.Value, Kind: string(reference.KindValueReference), Container: from, Span: s.Span, InRepository: true, Local: true}
			v := s.Value
			var target []*tomldoc.Node
			switch {
			case strings.HasPrefix(v, "dep:"):
				target = byName[v[len("dep:"):]]
				x.TargetKinds = string(symbol.KindDependency)
			case strings.Contains(v, "/"):
				name, _, _ := strings.Cut(v, "/")
				target = byName[strings.TrimSuffix(name, "?")]
				x.TargetKinds = string(symbol.KindDependency)
			case f.Get(v) != nil:
				target = []*tomldoc.Node{f.Get(v)}
				x.TargetKinds = string(symbol.KindKey)
			default:
				target = byName[v] // an optional dependency's implicit feature
				x.TargetKinds = string(symbol.KindDependency)
			}
			if len(target) > 0 {
				x.NameQualified = r.id(target[0])
				if len(target) > 1 {
					x.Cap = "candidate"
				}
			}
			if x.NameQualified == "" {
				x.InRepository, x.Local = false, false
			}
			r.ref(x)
		}
	}
}

// pointer is the JSON Pointer token of a key.
func pointer(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

// withoutUserinfo removes the credentials a URL may carry, in its
// authority, query or fragment.
func withoutUserinfo(u string) string {
	u, _, _ = strings.Cut(u, "#")
	u, _, _ = strings.Cut(u, "?")
	scheme, rest, ok := strings.Cut(u, "://")
	if !ok {
		// scp-like: [user[:password]@]host:path
		if i := strings.LastIndex(u, "@"); i >= 0 && !strings.Contains(u[:i], "/") {
			return u[i+1:]
		}
		return u
	}
	auth, tail, hasTail := strings.Cut(rest, "/")
	if i := strings.LastIndex(auth, "@"); i >= 0 {
		auth = auth[i+1:]
	}
	if hasTail {
		return scheme + "://" + auth + "/" + tail
	}
	return scheme + "://" + auth
}

// ---- pyproject ----

// pep508Name is the name a PEP 508 requirement starts with.
var pep508Name = regexp.MustCompile(`^[ \t]*([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)`)

var pep503 = regexp.MustCompile(`[-_.]+`)

// normalize is a distribution name's PEP 503 normalized form.
func normalize(name string) string { return strings.ToLower(pep503.ReplaceAllString(name, "-")) }

func (r *reader) pyproject() {
	root := r.d.Root
	proj := root.Get("project")
	projID := ""
	if name, ok := proj.Get("name").String(); ok && proj.Kind == tomldoc.Table && declname.IsName(name) {
		projID = r.id(proj)
		sig := "project " + name
		if v, ok := proj.Get("version").String(); ok && declname.IsName(v) {
			sig += " " + v
		}
		r.out.Overrides[proj] = Override{Kind: symbol.KindPackage, Name: name, Signature: sig}
	}
	r.requirements(proj.Get("dependencies"), projID)
	if opt := proj.Get("optional-dependencies"); opt != nil && opt.Kind == tomldoc.Table {
		for _, g := range opt.Children {
			r.requirements(g, projID)
		}
	}
	if groups := root.Get("dependency-groups"); groups != nil && groups.Kind == tomldoc.Table {
		for _, g := range groups.Children {
			r.requirements(g, projID)
		}
	}
	r.requirements(root.Get("build-system").Get("requires"), projID)
}

// requirements states the PEP 508 requirements of the array a.
func (r *reader) requirements(a *tomldoc.Node, owner string) {
	if a == nil || a.Kind != tomldoc.Value || a.VKind != tomldoc.Array {
		return
	}
	at := r.id(a)
	if at == "" {
		return
	}
	seen := map[string]int{}
	for _, s := range a.Strs {
		m := pep508Name.FindStringSubmatch(s.Value)
		if m == nil || !declname.IsName(m[1]) {
			r.out.Problems = append(r.out.Problems, tomldoc.Problem{Code: CodeInvalidRequirement, Start: s.Span.Start, End: s.Span.End,
				Message: "a requirement that does not start with a distribution name (PEP 508) declares nothing"})
			continue
		}
		name, norm := m[1], normalize(m[1])
		seen[norm]++
		q := at + "/" + pointer(norm)
		if n := seen[norm]; n > 1 {
			q += "#" + strconv.Itoa(n)
		}
		if !declname.IsName(q) {
			continue
		}
		r.out.Decls = append(r.out.Decls, Decl{Name: name, Qualified: q, Kind: symbol.KindDependency, Span: s.Span, Parent: a,
			Signature: fmt.Sprintf("dependency pypi %s", norm)})
		r.ref(Ref{Name: name, Kind: string(reference.KindExplicitDependency), Container: q, Span: s.Span, NameQualified: "pypi:" + norm})
		if owner != "" {
			r.ref(Ref{Name: name, Kind: string(reference.KindExplicitDependency), Container: owner, Span: s.Span,
				NameQualified: q, InRepository: true, Local: true, TargetKinds: string(symbol.KindDependency)})
		}
	}
}
