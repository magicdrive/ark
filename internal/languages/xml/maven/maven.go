// Package maven reads a Maven project object model (pom.xml) from an
// xmldoc.Document: the meaning layer of Ark's XML provider. It reads one
// file and fetches nothing: no parent POM, no artifact, no repository.
//
// A file is a POM when its root element is <project> in a Maven POM
// namespace (4.0.0, 4.1.0), or in no namespace in a file named pom.xml.
// It states:
//
//   - the project (symbol.KindPackage) "<FileID>::<groupId>:<artifactId>",
//     named "groupId:artifactId", its version in the signature. A groupId
//     or version the file does not write is its <parent>'s, as Maven
//     inherits them; what another file would have to supply is unknown, and
//     a project without both coordinates is "<FileID>::project". The
//     project also states an alias "maven:<groupId>:<artifactId>" of its
//     identity, at most Strong: coordinates name a project of the
//     repository only as far as nothing else builds the same ones (another
//     version, a repository the build reads).
//   - its parent: an inheritance reference. Maven takes the POM at
//     <relativePath> (default ../pom.xml; a directory means its pom.xml) if
//     its coordinates are the parent's, else the artifact from a
//     repository. So the reference names "<that file>::<groupId>:<artifactId>"
//     — the path and the coordinates both — and the file states a fallback
//     alias of that identity to "maven:<groupId>:<artifactId>": a POM at the
//     path with other coordinates, or none, leaves the coordinates to find
//     the parent among the repository's projects, else outside it. An empty
//     <relativePath/> (and a path leaving the repository) names the
//     coordinates only. At most Strong: the version is not compared.
//   - its modules (<modules><module>, Maven 4 <subprojects><subproject>, in
//     profiles too): a resource_link with path evidence (FileIdentity) to
//     the module's POM file (<module>/pom.xml, or the .xml file it names),
//     Exact; a module missing from the repository is Unresolved.
//   - its dependencies, managed dependencies, plugins, managed plugins and
//     build extensions (symbol.KindDependency)
//     "<FileID>::<section>::<groupId>:<artifactId>" (the n-th repeat
//     "#n"; a profile's "<FileID>::profile:<id>::<section>::…"). The
//     project depends_on each dependency, plugin and extension of its
//     build, and contains the managed ones and a profile's; each that is a
//     dependency of the build (a managed one only for scope import) has a
//     depends_on reference to "maven:<groupId>:<artifactId>": a project of
//     the repository with those coordinates (Strong, through its alias), or
//     outside the repository. A coordinate with a property the file does
//     not define (${…}), or an entity left unexpanded, is a dynamic name.
package maven

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/magicdrive/ark/internal/languages/xml/xmldoc"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// The POM namespaces.
const (
	NamespacePOM4  = "http://maven.apache.org/POM/4.0.0"
	NamespacePOM41 = "http://maven.apache.org/POM/4.1.0"
)

// Problem codes.
const (
	CodeMissingCoordinate   = "maven_missing_coordinate"
	CodeDuplicateDependency = "maven_duplicate_dependency"
)

// CoordinatePrefix starts the identity of a project's coordinates.
const CoordinatePrefix = "maven:"

// Symbol is a declaration the POM states.
type Symbol struct {
	Name, Qualified string
	Kind            symbol.SymbolKind
	Span            xmldoc.Span
	Parent          string
	Signature       string
}

// Ref is a reference the POM states. NameQualified is the target's
// identity; FileIdentity marks path evidence, InRepository an identity only
// the repository can declare.
type Ref struct {
	Name, Kind, Container string
	Span                  xmldoc.Span
	NameQualified         string
	FileIdentity          bool
	InRepository          bool
	Dynamic               bool
	Cap, TargetKinds      string
}

// Alias is an identity the POM declares to name another.
type Alias struct {
	Qualified, Target string
	Span              xmldoc.Span
	Cap               string
	Fallback          bool
}

// Result is what Extract states.
type Result struct {
	Project  string // the project's identity
	Symbols  []Symbol
	Refs     []Ref
	Aliases  []Alias
	Problems []xmldoc.Problem
}

// IsPOM reports whether the document is a Maven POM.
func IsPOM(file string, d *xmldoc.Document) bool {
	r := d.Root
	if r == nil || r.Name.Local != "project" {
		return false
	}
	switch r.Name.URI {
	case NamespacePOM4, NamespacePOM41:
		return true
	case "":
		return path.Base(slash(file)) == "pom.xml"
	}
	return false
}

type extractor struct {
	file  string
	src   []byte
	sep   string
	dir   string // the file's directory, slash-separated ("" for the root)
	ns    string
	root  *xmldoc.Node
	props map[string]string
	res   Result
}

// Extract reads the POM; false when the document is none.
func Extract(file string, d *xmldoc.Document) (Result, bool) {
	if !IsPOM(file, d) {
		return Result{}, false
	}
	x := &extractor{file: file, src: d.Src, sep: separator(file), ns: d.Root.Name.URI, root: d.Root, props: map[string]string{}}
	x.dir = path.Dir(slash(file))
	if x.dir == "." {
		x.dir = ""
	}
	x.run()
	return x.res, true
}

func (x *extractor) child(n *xmldoc.Node, local string) *xmldoc.Node { return n.Child(x.ns, local) }

// text is a leaf element's own text, trimmed ("" for none or an element
// with children or with more text than xmldoc keeps).
func text(n *xmldoc.Node) string {
	if n == nil || len(n.Children) > 0 || n.TextCut {
		return ""
	}
	return strings.TrimSpace(n.Text)
}

// textSpan is the bytes of a leaf element's trimmed text (the element's
// when it has none).
func textSpan(src []byte, n *xmldoc.Node) xmldoc.Span {
	s, e := n.TextSpan.Start, n.TextSpan.End
	for s < e && isSpace(src[s]) {
		s++
	}
	for e > s && isSpace(src[e-1]) {
		e--
	}
	if s == e {
		return n.Span
	}
	return xmldoc.Span{Start: s, End: e}
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func (x *extractor) run() {
	r := x.root
	parent := x.child(r, "parent")
	pg, pa, pv := text(x.child(parent, "groupId")), text(x.child(parent, "artifactId")), text(x.child(parent, "version"))
	g, a, v := text(x.child(r, "groupId")), text(x.child(r, "artifactId")), text(x.child(r, "version"))
	if g == "" {
		g = pg
	}
	if v == "" {
		v = pv
	}
	builtin := map[string]string{
		"project.groupId": g, "pom.groupId": g, "groupId": g,
		"project.artifactId": a, "pom.artifactId": a, "artifactId": a,
		"project.version": v, "pom.version": v, "version": v,
		"project.parent.groupId": pg, "parent.groupId": pg,
		"project.parent.artifactId": pa, "project.parent.version": pv, "parent.version": pv,
	}
	if props := x.child(r, "properties"); props != nil {
		for _, c := range props.Children {
			if t := text(c); t != "" && !strings.Contains(t, "${") {
				x.props[c.Name.Local] = t
			}
		}
	}
	for k, val := range builtin {
		if val != "" && !strings.Contains(val, "${") {
			x.props[k] = val
		}
	}
	g, a, v = x.expand(g), x.expand(a), x.expand(v)

	proj := Symbol{Name: "project", Qualified: x.file + "::project", Kind: symbol.KindPackage, Span: r.Span}
	if coordinate(g) && coordinate(a) {
		proj.Name = g + ":" + a
		proj.Qualified = x.file + "::" + proj.Name
		x.res.Aliases = append(x.res.Aliases, Alias{Qualified: CoordinatePrefix + proj.Name, Target: proj.Qualified, Span: r.Span, Cap: "strong"})
	}
	proj.Signature = "project " + proj.Name
	if v != "" {
		proj.Signature += ":" + v
	}
	if p := text(x.child(r, "packaging")); p != "" {
		proj.Signature += " (" + p + ")"
	}
	x.res.Project = proj.Qualified
	x.res.Symbols = append(x.res.Symbols, proj)

	if parent != nil {
		x.parent(parent, pg, pa)
	}
	x.modules(r)
	x.sections(r, "", true)
	if profiles := x.child(r, "profiles"); profiles != nil {
		for i, p := range profiles.ChildrenNamed(x.ns, "profile") {
			id := text(x.child(p, "id"))
			if !coordinate(id) {
				id = strconv.Itoa(i + 1)
			}
			x.modules(p)
			x.sections(p, "profile:"+id+"::", false)
		}
	}
}

// expand replaces the properties the file defines (${name}); what is left
// unexpanded keeps its ${…}.
func (x *extractor) expand(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		j := strings.Index(s[i:], "${")
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+j])
		k := strings.IndexByte(s[i+j:], '}')
		if k < 0 {
			b.WriteString(s[i+j:])
			break
		}
		name := s[i+j+2 : i+j+k]
		if val, ok := x.props[name]; ok {
			b.WriteString(val)
		} else {
			b.WriteString(s[i+j : i+j+k+1])
		}
		i += j + k + 1
	}
	return b.String()
}

// coordinate reports whether s is a groupId or artifactId as Maven
// accepts one ([A-Za-z0-9_.-]+).
func coordinate(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

func (x *extractor) parent(n *xmldoc.Node, pg, pa string) {
	pg, pa = x.expand(pg), x.expand(pa)
	if !coordinate(pg) || !coordinate(pa) {
		x.problem(n.Span, fmt.Sprintf("the parent's groupId and artifactId are not both written as coordinates (%q, %q)", pg, pa))
		return
	}
	coords := pg + ":" + pa
	ref := Ref{Name: coords, Kind: string(reference.KindInheritance), Container: x.res.Project, Span: n.Span,
		NameQualified: CoordinatePrefix + coords, Cap: "strong", TargetKinds: string(symbol.KindPackage)}
	rel := "../pom.xml"
	if rp := x.child(n, "relativePath"); rp != nil {
		rel = text(rp)
		if rel != "" && !strings.HasSuffix(rel, ".xml") {
			rel = strings.TrimSuffix(rel, "/") + "/pom.xml"
		}
	}
	if p, ok := x.inRepository(rel); ok && rel != "" {
		ref.NameQualified = x.filePath(p) + "::" + coords
		x.res.Aliases = append(x.res.Aliases, Alias{Qualified: ref.NameQualified, Target: CoordinatePrefix + coords, Span: n.Span, Cap: "strong", Fallback: true})
	}
	x.res.Refs = append(x.res.Refs, ref)
}

// inRepository resolves a relative path written in the file to a
// repository path, slash-separated; false for one that leaves the
// repository or is no plain relative path.
func (x *extractor) inRepository(rel string) (string, bool) {
	p, ok := RelativePath(x.dir, rel)
	if !ok || p == ".." || strings.HasPrefix(p, "../") {
		return p, false
	}
	return p, true
}

// RelativePath joins a plain relative path (no scheme, drive, query,
// fragment, backslash, property or absolute path) to a slash-separated
// directory; false for any other text.
func RelativePath(dir, rel string) (string, bool) {
	if rel == "" || strings.ContainsAny(rel, ":?#\\%$") || strings.HasPrefix(rel, "/") || len(rel) > 1024 {
		return "", false
	}
	return path.Clean(path.Join(dir, rel)), true
}

func (x *extractor) modules(n *xmldoc.Node) {
	for _, list := range [][2]string{{"modules", "module"}, {"subprojects", "subproject"}} {
		for _, m := range x.child(n, list[0]).ChildrenNamed(x.ns, list[1]) {
			name := text(m)
			if name == "" {
				continue
			}
			sp := textSpan(x.src, m)
			ref := Ref{Name: name, Kind: string(reference.KindResourceLink), Container: x.res.Project, Span: sp,
				FileIdentity: true, TargetKinds: string(symbol.KindDocument)}
			if strings.Contains(name, "${") || strings.Contains(name, "&") {
				ref.Dynamic = true
				x.res.Refs = append(x.res.Refs, ref)
				continue
			}
			file := name
			if !strings.HasSuffix(file, ".xml") {
				file = strings.TrimSuffix(file, "/") + "/pom.xml"
			}
			p, ok := RelativePath(x.dir, file)
			if !ok {
				ref.Dynamic = true // a URL or other text no path of the repository
				x.res.Refs = append(x.res.Refs, ref)
				continue
			}
			ref.NameQualified = x.filePath(p)
			ref.InRepository = !(p == ".." || strings.HasPrefix(p, "../"))
			x.res.Refs = append(x.res.Refs, ref)
		}
	}
}

type section struct {
	name    string   // the identity's section
	path    []string // elements from the project (or profile)
	item    string
	depends bool // a dependency of the build (else managed)
	plugin  bool
}

var sections = []section{
	{"dependencies", []string{"dependencies"}, "dependency", true, false},
	{"dependencyManagement", []string{"dependencyManagement", "dependencies"}, "dependency", false, false},
	{"plugins", []string{"build", "plugins"}, "plugin", true, true},
	{"pluginManagement", []string{"build", "pluginManagement", "plugins"}, "plugin", false, true},
	{"extensions", []string{"build", "extensions"}, "extension", true, false},
}

func (x *extractor) sections(base *xmldoc.Node, prefix string, main bool) {
	for _, s := range sections {
		n := base
		for _, p := range s.path {
			n = x.child(n, p)
		}
		if n == nil {
			continue
		}
		seen := map[string]int{}
		dup := map[string]bool{}
		for _, item := range n.ChildrenNamed(x.ns, s.item) {
			x.dependency(item, s, prefix, main, seen, dup)
		}
	}
}

func (x *extractor) dependency(n *xmldoc.Node, s section, prefix string, main bool, seen map[string]int, dup map[string]bool) {
	rawG, rawA := text(x.child(n, "groupId")), text(x.child(n, "artifactId"))
	if rawG == "" && s.plugin {
		rawG = "org.apache.maven.plugins"
	}
	if rawA == "" || rawG == "" {
		x.problem(n.Span, fmt.Sprintf("a %s without a groupId and an artifactId names no artifact", s.item))
		return
	}
	g, a := x.expand(rawG), x.expand(rawA)
	v, scope := x.expand(text(x.child(n, "version"))), text(x.child(n, "scope"))
	typ, classifier := text(x.child(n, "type")), text(x.child(n, "classifier"))
	resolved := coordinate(g) && coordinate(a)
	name := g + ":" + a
	if !resolved {
		name = rawG + ":" + rawA
	}
	key := name + ":" + typ + ":" + classifier
	if dup[key] {
		x.res.Problems = append(x.res.Problems, xmldoc.Problem{Code: CodeDuplicateDependency, Start: n.Span.Start, End: n.Span.End,
			Message: fmt.Sprintf("the %s %s is declared twice in %s", s.item, name, s.name)})
	}
	dup[key] = true
	seen[name]++
	q := x.file + "::" + prefix + s.name + "::" + name
	if c := seen[name]; c > 1 {
		q += "#" + strconv.Itoa(c)
	}
	sig := s.item + " " + name
	if v != "" {
		sig += ":" + v
	}
	for _, extra := range []string{typ, classifier, scope} {
		if extra != "" {
			sig += " " + extra
		}
	}
	x.res.Symbols = append(x.res.Symbols, Symbol{Name: name, Qualified: q, Kind: symbol.KindDependency, Span: n.Span, Parent: x.res.Project, Signature: sig})
	kind := reference.KindContains
	if main && s.depends {
		kind = reference.KindExplicitDependency
	}
	x.res.Refs = append(x.res.Refs, Ref{Name: q, Kind: string(kind), Container: x.res.Project, Span: n.Span,
		NameQualified: q, InRepository: true, TargetKinds: string(symbol.KindDependency)})
	if !s.depends && scope != "import" {
		return
	}
	at := n.Span
	if an := x.child(n, "artifactId"); an != nil {
		at = textSpan(x.src, an)
	}
	ref := Ref{Name: name, Kind: string(reference.KindExplicitDependency), Container: q, Span: at,
		NameQualified: CoordinatePrefix + name, Cap: "strong", TargetKinds: string(symbol.KindPackage)}
	if !resolved {
		ref.NameQualified, ref.Cap, ref.TargetKinds, ref.Dynamic = "", "", "", true
	}
	x.res.Refs = append(x.res.Refs, ref)
}

func (x *extractor) problem(sp xmldoc.Span, msg string) {
	x.res.Problems = append(x.res.Problems, xmldoc.Problem{Code: CodeMissingCoordinate, Message: msg, Start: sp.Start, End: sp.End})
}

// filePath spells a slash-separated repository path as the index's file
// IDs are spelled.
func (x *extractor) filePath(p string) string { return strings.ReplaceAll(p, "/", x.sep) }

func slash(file string) string { return strings.ReplaceAll(file, "\\", "/") }

// separator is the separator of the index's file IDs, as the file's own ID
// shows it: the OS's (Windows: '\'). A file at the root shows none; its
// paths are the same either way except for those of subdirectories, which
// then use the OS's.
func separator(file string) string {
	switch {
	case strings.Contains(file, "/"):
		return "/"
	case strings.Contains(file, "\\"):
		return "\\"
	}
	return osSeparator
}

var osSeparator = string(filepath.Separator)
