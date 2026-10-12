package rust

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/magicdrive/ark/internal/source"
)

// layout is where a file stands in its crate, derived lexically from its
// repository-relative path by Cargo's default target layout — no file is
// read, Cargo.toml included:
//
//	PKG/src/lib.rs, PKG/src/main.rs         the package's crate root
//	PKG/src/a.rs, PKG/src/a/mod.rs          module a of that crate
//	PKG/src/a/b.rs, PKG/src/a/b/mod.rs      module a::b
//	PKG/src/bin/x.rs                        the root of binary crate x
//	PKG/src/bin/x/main.rs                   the root of binary crate x
//	PKG/src/bin/x/a.rs (a/mod.rs)           module a of binary crate x
//	PKG/tests/x.rs, examples/x.rs, benches/x.rs, and x/main.rs there
//	                                        the root of that target's crate
//	PKG/build.rs                            the root of the build script
//
// A package's library and binary roots (lib.rs, main.rs) share one crate
// key: their module files are one directory, and a module declared by both
// is one set of declarations. Their own top-level items then share
// identities — two declarations of one name are ambiguous, never merged.
//
// Anything else has no layout (ok false): a file under two directories
// named src (a crate nested in another's sources, or a module named src),
// a module file of a single-file binary or test (its directory is shared
// with the other targets), a module of tests/ or examples/ shared by
// several targets (tests/common/mod.rs), a file outside any src/, tests/,
// examples/ or benches/ directory, or one a `#[path]` attribute or a
// Cargo.toml `[lib] path` places elsewhere (which this file cannot tell):
// its declarations are reached by no identity a path computes, and its
// references carry no identity.
type layout struct {
	crate   string   // the crate key, e.g. "crate", "crates/app/crate"
	modPath []string // the file's module path below the crate root
	ok      bool
}

// fileLayout returns file's layout.
func fileLayout(file source.FileID) layout {
	comps := strings.Split(path.Clean(filepath.ToSlash(string(file))), "/")
	if len(comps) == 0 || !strings.HasSuffix(comps[len(comps)-1], ".rs") {
		return layout{}
	}
	srcAt := -1
	for i, c := range comps[:len(comps)-1] {
		if c == "src" {
			if srcAt >= 0 {
				return layout{} // two src directories: which is the crate's?
			}
			srcAt = i
		}
	}
	if srcAt >= 0 {
		return srcLayout(comps[:srcAt], comps[srcAt+1:])
	}
	// tests/, examples/, benches/: the last such directory is the package's.
	for i := len(comps) - 2; i >= 0; i-- {
		switch comps[i] {
		case "tests", "examples", "benches":
			return targetDirLayout(comps[:i], comps[i:])
		}
	}
	if len(comps) >= 1 && comps[len(comps)-1] == "build.rs" {
		return layout{crate: crateKey(comps[:len(comps)-1], "build.rs"), ok: true}
	}
	return layout{}
}

// srcLayout lays out a file below PKG/src (rest: the components after src).
func srcLayout(pkg, rest []string) layout {
	if len(rest) >= 2 && rest[0] == "bin" {
		if len(rest) == 2 {
			// src/bin/x.rs: a single-file binary crate.
			return layout{crate: crateKey(pkg, "src/bin/"+rest[1]), ok: true}
		}
		// src/bin/x/...: the binary crate x rooted at src/bin/x/main.rs.
		return modulesBelow(crateKey(pkg, "src/bin/"+rest[1]+"/main.rs"), rest[2:], "main.rs")
	}
	return modulesBelow(crateKey(pkg, ""), rest, "lib.rs", "main.rs")
}

// targetDirLayout lays out a file below PKG/tests, examples or benches
// (rest starts with that directory's name).
func targetDirLayout(pkg, rest []string) layout {
	switch {
	case len(rest) == 2:
		return layout{crate: crateKey(pkg, rest[0]+"/"+rest[1]), ok: true}
	case len(rest) == 3 && rest[2] == "main.rs":
		return layout{crate: crateKey(pkg, rest[0]+"/"+rest[1]+"/main.rs"), ok: true}
	}
	// A module of a target directory: shared by several targets, or of a
	// target whose root this file cannot see.
	return layout{}
}

// modulesBelow lays out the file at rel below a crate's root directory:
// one of roots is the crate root; a mod.rs is its directory's module; any
// other file is the module of its own name.
func modulesBelow(crate string, rel []string, roots ...string) layout {
	last := rel[len(rel)-1]
	if len(rel) == 1 {
		for _, r := range roots {
			if last == r {
				return layout{crate: crate, ok: true}
			}
		}
	}
	mods := append([]string(nil), rel[:len(rel)-1]...)
	if last != "mod.rs" {
		mods = append(mods, strings.TrimSuffix(last, ".rs"))
	}
	for _, m := range mods {
		if !isIdentifier(m) {
			return layout{} // no module can be named so
		}
	}
	return layout{crate: crate, modPath: mods, ok: true}
}

// crateKey names the crate of a package directory's target: "crate" for
// the library and main binary of the repository root's package,
// "PKG/crate" for another package's, and the target's root path followed by
// "/crate" for the other targets ("src/bin/x.rs/crate", "tests/it.rs/crate").
// A path never contains "::", so a key is never confused with a module path.
func crateKey(pkg []string, target string) string {
	dir := strings.Join(pkg, "/")
	switch {
	case target == "" && dir == "":
		return "crate"
	case target == "":
		return dir + "/crate"
	case dir == "":
		return target + "/crate"
	}
	return dir + "/" + target + "/crate"
}

// isIdentifier reports whether s is a Rust identifier (ASCII, as module
// file names are; a raw identifier's r# is not part of a file name).
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return s != "_"
}

// modulePath composes a module's identity from its crate key and path.
func modulePath(crate string, mods []string) string {
	if len(mods) == 0 {
		return crate
	}
	return crate + "::" + strings.Join(mods, "::")
}
