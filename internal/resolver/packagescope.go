package resolver

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
	"github.com/magicdrive/ark/internal/testfiles"
)

// Package scoping (language.Extraction.PackageScoped).
//
// In a package-scoped file a name is resolved by the language's scoping,
// stated by the provider, never by its spelling elsewhere:
//
//   - A name without a receiver denotes a declaration of the file's own
//     package — its file (Exact), its directory and Package (Strong) — or of
//     a package the file imports with alias "." (Exact). Nothing else: a
//     declaration in another directory is not visible by an unqualified
//     name, however unique the name is in the repository. A name declared in
//     none of them (a local variable, a builtin, an external dot-imported
//     declaration) is Unresolved.
//   - A receiver naming an import denotes the imported package: the name is
//     looked up among that package's declarations only (Exact), and an
//     import path that names no repository package is OutsideRepository —
//     never a same-named declaration of the importing package, its file or
//     the rest of the repository.
//   - An import path names the repository directory D for which the path is
//     <prefix>/D, where <prefix> is an import-path prefix of the repository:
//     one that two or more distinct import paths of the repository resolve
//     under, or — when it is the repository's only candidate — one whose
//     last element is the repository root directory's name (a module
//     github.com/u/repo checked out as repo/). The prefix itself names the
//     root directory; importing it counts as one of the two paths when its
//     last element is the name of a package of the root directory (Go names
//     a package after its import path), so a module checked out under
//     another name (repo@v1.2.3/, a renamed clone) still has a prefix. A
//     single coincidence (an external path that happens to end in a
//     repository directory's name) establishes no prefix. A prefix without
//     a host (no dot in its first element: encoding, log) may be the
//     standard library's, whose paths can coincide with a repository's
//     directories in twos (encoding/json, encoding/xml) or name the root
//     package (log): only the root directory's own name establishes it.
//     Of several directories the longest wins (the import path names it
//     fully).
//   - An import path that names no repository directory is outside the
//     repository (OutsideRepository) only when nothing in the repository
//     could be it: when it ends in a repository directory or its last
//     element names a package of the root directory, an unestablished
//     prefix is missing evidence, not proof — the name stays Unresolved
//     without that claim.
//   - A directory may hold several packages (a library beside a generator
//     behind a build tag): an import refers to the one whose name the
//     reference uses (the implicit name of an import is its package's name);
//     with an explicit alias over several packages the target stays
//     ambiguous (Candidate).
//   - Only a package's non-test files are importable (internal/testfiles),
//     and only its Exported declarations are reachable from another package
//     (by pkg.Name or a dot import); a non-test file never sees its
//     package's test files.
//
// A receiver that is a variable keeps the existing rules (R2 with a proven
// type, R4 otherwise), and providers cap names they know to be local. A
// receiver without a proven type but with a ReceiverChain is resolved
// through the declarations the chain names (typefacts.go) when the chain
// proves its type, and as an untyped value receiver (R4) otherwise — never
// as an import or a type its name happens to spell. A Speculative
// reference (a selector that may name a field) is resolved through its
// evidence only: without a result it is Unresolved, never a name match.

// EvidencePackageScope: a package-scoped reference resolved, or found
// unresolvable, by package scoping.
const EvidencePackageScope EvidenceKind = "package_scope"

type packageScope struct {
	once sync.Once
	// packages: directory → the Package names of its non-test files.
	packages map[string]map[string]bool
	// dirOf: import path → repository directory ("" when external).
	dirOf map[string]string
	// maybeRepo: import paths dirOf maps to no directory that could still
	// name one (not proven external).
	maybeRepo map[string]bool
}

func (r *Resolver) pkgScope() *packageScope {
	ps := &r.packages
	ps.once.Do(func() {
		ps.packages = map[string]map[string]bool{}
		var importPaths []string
		seen := map[string]bool{}
		for i := range r.files {
			fi := &r.files[i]
			if !fi.PackageScoped {
				continue
			}
			if !testfiles.IsTestFile(string(fi.FileID)) {
				dir := filepath.ToSlash(filepath.Dir(string(fi.FileID)))
				if ps.packages[dir] == nil {
					ps.packages[dir] = map[string]bool{}
				}
				ps.packages[dir][fi.Package] = true
			}
			for _, imp := range fi.Imports {
				if !seen[imp.Path] {
					seen[imp.Path] = true
					importPaths = append(importPaths, imp.Path)
				}
			}
		}
		sort.Strings(importPaths)
		ps.dirOf, ps.maybeRepo = mapImportPaths(importPaths, ps.packages, r.rootName)
	})
	return ps
}

// hostedPath reports whether an import path's first element has a dot — a
// module path with a host (example.com/m); a path without one is reserved
// for the standard library, or a local module named without a host.
func hostedPath(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return strings.Contains(first, ".")
}

// mapImportPaths maps import paths to repository directories (see the
// package-scoping rules above). maybeRepo holds the paths it maps to none
// that could still name a repository package.
func mapImportPaths(importPaths []string, packages map[string]map[string]bool, rootName string) (dirOf map[string]string, maybeRepo map[string]bool) {
	type match struct{ prefix, dir string }
	matches := map[string][]match{}
	support := map[string]map[string]bool{} // prefix → import paths under it
	for _, p := range importPaths {
		if packages["."][path.Base(p)] {
			// p may be the prefix itself, naming the root package.
			if support[p] == nil {
				support[p] = map[string]bool{}
			}
			support[p][p] = true
		}
		for dir := range packages {
			if dir == "." || !strings.HasSuffix(p, "/"+dir) {
				continue
			}
			prefix := strings.TrimSuffix(p, "/"+dir)
			matches[p] = append(matches[p], match{prefix, dir})
			if support[prefix] == nil {
				support[prefix] = map[string]bool{}
			}
			support[prefix][p] = true
		}
	}
	accepted := func(prefix string) bool {
		named := rootName != "" && path.Base(prefix) == rootName
		if !hostedPath(prefix) {
			// A path without a host (no dot in its first element) is the
			// standard library's as much as a module's: encoding/json and
			// encoding/xml share "encoding" by coincidence. Only the root
			// directory's own name establishes it.
			return named
		}
		if len(support[prefix]) >= 2 {
			return true
		}
		return len(support) == 1 && named
	}
	out := map[string]string{}
	maybe := map[string]bool{}
	for _, p := range importPaths {
		best := ""
		for _, m := range matches[p] {
			if accepted(m.prefix) && len(m.dir) > len(best) {
				best = m.dir
			}
		}
		if best == "" && accepted(p) && packages["."] != nil {
			best = "." // the import path is the repository prefix itself
		}
		out[p] = best
		if best == "" && (len(matches[p]) > 0 || packages["."][path.Base(p)]) {
			maybe[p] = true
		}
	}
	return out, maybe
}

// resolvePackageScoped resolves a reference of a package-scoped file.
func (r *Resolver) resolvePackageScoped(res Resolution, ref reference.Reference, fi FileIndex) Resolution {
	if ref.ReceiverExpr == "" {
		return r.resolvePackageName(res, ref, fi)
	}
	if ref.ReceiverType != "" {
		if pkg, name, ok := strings.Cut(ref.ReceiverType, "."); ok {
			return r.resolveViaImportedReceiverType(res, ref, fi, pkg, name)
		}
		return r.resolveViaReceiverType(res, ref, fi)
	}
	if ref.ReceiverChain != "" {
		if out, ok := r.resolveViaReceiverChain(res, ref, fi); ok {
			return out
		}
		if !ref.Speculative {
			// The chain describes a value (a local, a package variable, a
			// call's result): proving no type for it, it is an untyped
			// receiver — never read as an import or a type of its name.
			return capUntypedReceiver(r.resolveByName(res, ref, fi), ref)
		}
	}
	if ref.Speculative {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceReceiverChain, Detail: fmt.Sprintf("%s.%s: the receiver's evidence names no declaration", ref.ReceiverExpr, ref.Name)}}
		return res
	}
	if ref.ConfidenceCap == ConfidenceCandidate.String() {
		// The provider caps a receiver it found a local declaration shadows
		// (a variable named like an import or a type: var atomic T;
		// atomic.Add(...)): it is a value, so neither the import nor the type
		// is evidence — and an import outside the repository proves nothing
		// about it (no OutsideRepository).
		return capUntypedReceiver(r.resolveByName(res, ref, fi), ref)
	}
	if imp, ok := r.importNamed(ref.ReceiverExpr, fi); ok {
		return r.resolveImportedName(res, ref, fi, imp)
	}
	if types := r.packageTypes(ref.ReceiverExpr, fi); len(types) > 0 {
		// T.M: a method of a type of this package.
		return r.memberResolution(res, ref, types, ConfidenceExact, EvidencePackageScope,
			fmt.Sprintf("receiver %q is a type of package %s", ref.ReceiverExpr, fi.Package))
	}
	// A variable or expression receiver without type evidence (R4).
	return capUntypedReceiver(r.resolveByName(res, ref, fi), ref)
}

// resolveViaImportedReceiverType resolves a member of a receiver declared as
// pkg.Name — a type of the package the file imports as pkg (R2 through the
// import binding): Exact among that type's members. A type of a package
// outside the repository has its members outside too (OutsideRepository);
// an import that may name a repository package unproven, a package without
// that exported type, or a type without that member, is Unresolved.
func (r *Resolver) resolveViaImportedReceiverType(res Resolution, ref reference.Reference, fi FileIndex, pkg, name string) Resolution {
	detail := fmt.Sprintf("receiver %q declared as %q", ref.ReceiverExpr, ref.ReceiverType)
	imp, ok := r.importNamed(pkg, fi)
	if !ok {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceReceiverType, Detail: detail + "; no import named " + pkg}}
		return res
	}
	if imp.dir == "" {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceReceiverType, Detail: detail + "; " + imp.path + " is not a repository package"}}
		res.OutsideRepository = !r.pkgScope().maybeRepo[imp.path]
		return res
	}
	var types []symbol.Symbol
	for _, s := range r.importableDecls(imp.dir, imp.pkg, name, fi) {
		if ownsMembers(s.Kind) {
			types = append(types, s)
		}
	}
	if len(types) == 0 {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceReceiverType, Detail: fmt.Sprintf("%s; package %s (%s) declares no type %q", detail, imp.path, imp.dir, name)}}
		return res
	}
	conf := ConfidenceExact
	if len(types) > 1 {
		conf = ConfidenceCandidate
	}
	return r.memberResolution(res, ref, types, conf, EvidenceReceiverType, detail)
}

// resolvePackageName resolves a receiverless name by package scoping.
func (r *Resolver) resolvePackageName(res Resolution, ref reference.Reference, fi FileIndex) Resolution {
	// A receiverless name never denotes a member, not even one its
	// container encloses (an interface's method beside a type of that name:
	// type Provider interface { Language() Language }).
	if c := withoutMembers(r.sameFileContainerMatch(ref, fi)); len(c) > 0 {
		return r.pickBest(res, c, ConfidenceExact, EvidenceSameLexicalScope,
			fmt.Sprintf("symbol %q defined in same container %q", ref.Name, ref.Container))
	}
	if c := withoutMembers(r.sameFileMatch(ref, fi)); len(c) > 0 {
		return r.pickBest(res, c, ConfidenceExact, EvidenceSameFile,
			fmt.Sprintf("symbol %q defined in same file %s", ref.Name, fi.FileID))
	}
	if c := r.packageMembers(ref.Name, fi); len(c) > 0 {
		return r.pickBest(res, c, ConfidenceStrong, EvidenceSamePackage,
			fmt.Sprintf("symbol %q found in same package as %s", ref.Name, fi.FileID))
	}
	var dotted []symbol.Symbol
	var from []string
	for _, imp := range fi.Imports {
		if imp.Alias != "." {
			continue
		}
		dir := r.pkgScope().dirOf[imp.Path]
		if dir == "" {
			continue
		}
		if c := r.importableDecls(dir, "", ref.Name, fi); len(c) > 0 {
			dotted = append(dotted, c...)
			from = append(from, imp.Path)
		}
	}
	if len(dotted) > 0 {
		return r.pickBest(res, dotted, ConfidenceExact, EvidenceExplicitImport,
			fmt.Sprintf("symbol %q declared in dot-imported package %s", ref.Name, strings.Join(from, ", ")))
	}
	res.Evidence = []ResolutionEvidence{{Kind: EvidencePackageScope, Detail: fmt.Sprintf(
		"%q is declared neither in package %s nor in a dot-imported repository package; package scoping excludes the rest of the repository", ref.Name, fi.Package)}}
	return res
}

// packageMembers returns the non-member declarations named name in the
// file's package (same directory and Package), other files only; a non-test
// file does not see test files.
func (r *Resolver) packageMembers(name string, fi FileIndex) []symbol.Symbol {
	inTest := testfiles.IsTestFile(string(fi.FileID))
	var out []symbol.Symbol
	for _, ds := range r.dirSymbols[filepath.Dir(string(fi.FileID))][nameKey{fi.nameSpace(), name}] {
		if ds.file == fi.FileID || isMember(*ds.sym) {
			continue
		}
		other := r.byFile[ds.file]
		if other == nil || !other.PackageScoped || other.Package != fi.Package {
			continue
		}
		if !inTest && testfiles.IsTestFile(string(ds.file)) {
			continue
		}
		out = append(out, *ds.sym)
	}
	return out
}

// packageTypes returns the type declarations named name in the file's own
// package that can own members (ownsMembers).
func (r *Resolver) packageTypes(name string, fi FileIndex) []symbol.Symbol {
	var out []symbol.Symbol
	for _, s := range withoutMembers(r.sameFileMatch(reference.Reference{Name: name}, fi)) {
		if ownsMembers(s.Kind) {
			out = append(out, s)
		}
	}
	for _, s := range r.packageMembers(name, fi) {
		if ownsMembers(s.Kind) {
			out = append(out, s)
		}
	}
	return out
}

// importableDecls returns the Exported non-member declarations named name of
// the non-test files of directory dir in package pkg ("" for any package of
// the directory).
func (r *Resolver) importableDecls(dir, pkg, name string, fi FileIndex) []symbol.Symbol {
	var out []symbol.Symbol
	for _, ds := range r.dirSymbols[filepath.FromSlash(dir)][nameKey{fi.nameSpace(), name}] {
		other := r.byFile[ds.file]
		if other == nil || !other.PackageScoped || isMember(*ds.sym) || !ds.sym.Exported || testfiles.IsTestFile(string(ds.file)) {
			continue
		}
		if pkg != "" && other.Package != pkg {
			continue
		}
		out = append(out, *ds.sym)
	}
	return out
}

// importNamed returns the file's import that the receiver expr names: its
// explicit alias, else — implicitly — the name of a package of the
// repository directory it maps to, else the last element of its path.
func (r *Resolver) importNamed(expr string, fi FileIndex) (importRef, bool) {
	ps := r.pkgScope()
	for _, imp := range fi.Imports {
		if imp.Alias == "." || imp.Alias == "_" {
			continue
		}
		dir := ps.dirOf[imp.Path]
		pkgs := ps.packages[dir]
		switch {
		case imp.Alias != "":
			if imp.Alias == expr {
				return importRef{path: imp.Path, dir: dir, pkg: onlyPackage(pkgs)}, true
			}
		case dir != "" && pkgs[expr]:
			return importRef{path: imp.Path, dir: dir, pkg: expr}, true
		case dir == "" && path.Base(imp.Path) == expr:
			return importRef{path: imp.Path}, true
		}
	}
	return importRef{}, false
}

// onlyPackage returns the one package name of a set, or "" for several.
func onlyPackage(pkgs map[string]bool) string {
	if len(pkgs) != 1 {
		return ""
	}
	for p := range pkgs {
		return p
	}
	return ""
}

type importRef struct{ path, dir, pkg string }

// resolveImportedName resolves pkg.Name through the file's import of pkg.
func (r *Resolver) resolveImportedName(res Resolution, ref reference.Reference, fi FileIndex, imp importRef) Resolution {
	if imp.dir == "" {
		if r.pkgScope().maybeRepo[imp.path] {
			res.Evidence = []ResolutionEvidence{{Kind: EvidenceExplicitImport, Detail: fmt.Sprintf("%q imports %s, which may name a repository package: no import-path prefix establishes which", ref.ReceiverExpr, imp.path)}}
			return res
		}
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceExplicitImport, Detail: fmt.Sprintf("%q imports %s, which is not a repository package", ref.ReceiverExpr, imp.path)}}
		res.OutsideRepository = true
		return res
	}
	c := r.importableDecls(imp.dir, imp.pkg, ref.Name, fi)
	if len(c) == 0 {
		res.Evidence = []ResolutionEvidence{{Kind: EvidenceExplicitImport, Detail: fmt.Sprintf("package %s (%s) declares no %q", imp.path, imp.dir, ref.Name)}}
		return res
	}
	return r.pickBest(res, c, ConfidenceExact, EvidenceExplicitImport,
		fmt.Sprintf("%s.%s declared in imported package %s (%s)", ref.ReceiverExpr, ref.Name, imp.path, imp.dir))
}
