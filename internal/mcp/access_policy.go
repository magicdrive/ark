package mcp

import (
	"errors"
	"fmt"
	"github.com/magicdrive/ark/internal/common"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/magicdrive/ark/internal/fsroot"
	"github.com/magicdrive/ark/internal/libgitignore"
)

// File access policy: .arkignore.
//
// What the MCP server may read is decided by one policy: the rules of every
// .arkignore file under the server root, plus the files named by
// --additionally-ignorerule, interpreted by the matcher the repository dump
// uses (libgitignore) — the dump's rule with .gitignore handling off. A path
// is excluded when it, or a directory above it, matches. .gitignore plays no
// part in the policy.
//
// The policy is enforced where files are reached, not where results are
// written: the path gate refuses an excluded path (or one that resolves
// through a symlink to an excluded file) as if it did not exist; file walks
// (listing, search, statistics, trees, symbol and reference search) never
// enter an excluded directory or read an excluded file; and the repository
// index is built without excluded files, so no symbol, reference, edge,
// context, map entry or diagnostic of an excluded file exists to be returned.
// The extraction cache is never consulted for an excluded file.
//
// The rules are read for every request, so an .arkignore change — an edit, a
// new, removed or moved file, an edited additional rule file — applies to the
// next request. Each request reads every rule file at most once (its
// libgitignore.IgnoreReader), so all its decisions use one version of each.
// A path named by the client is decided by the rule files that can apply to
// it (the root's, those of the directories above it and its own), read
// without walking the repository; a walk decides by the whole repository's
// rules, read by walking it. Both decide every path the same way. The whole
// repository's rule is compiled only when the rule files' fingerprint (paths
// and SHA-256 of the contents) changed, from the very bytes fingerprinted.
// An index whose file set a change alters is rebuilt, because its source
// fingerprint is taken over the same, policy-filtered walk.
//
// Symlinks that lead outside the root: refused by default — the path gate
// refuses a path that resolves outside the root, and walks skip a symlink
// entry whose target lies outside it, so its content is neither read nor
// indexed. With mcp-server --allow-external-symlinks on (the operator's
// choice; no request can change it), a symlink in the repository may be read
// through: a file link by walks and by name, a directory link by naming a
// path below it (walks do not descend into directory links, as before). The
// .arkignore rules still apply — to the path in the repository the link is
// reached by, and to the target when it lies inside the root. A path outside
// the root that no symlink in the repository leads to is never reachable.
//
// Names are matched as the file system lists them. On a case-insensitive
// file system (macOS by default, Windows) "SECRET.TXT" reaches the file
// "secret.txt", and the rules are case-sensitive: a path named by the client
// is therefore decided — and handed to the tool — by its canonical form, each
// component renamed to the name its directory lists for the object reached
// (fsroot, fsCanonicalPath), and so is the target of a symlink a walk meets.
// Walks name entries as listed. A path whose canonical form cannot be
// established (the tree changed during the lookup, two hard links fit the
// name, a name the file system does not take as a plain path) is refused.
//
// The policy is independent of secret masking: turning masking off never
// widens it. If the rules cannot be read, every path but the root is refused.

// errPolicyUnavailable marks a refusal because the .arkignore rules could not
// be read.
var errPolicyUnavailable = errors.New("the .arkignore rules could not be read, so file access is refused")

// accessPolicy is one request's snapshot of the policy.
type accessPolicy struct {
	rule          *libgitignore.GitIgnore
	err           error    // non-nil: the rules could not be read; exclude everything
	roots         []string // the server root as given and, if different, symlink-resolved
	allowExternal bool     // --allow-external-symlinks on
}

// policyKey carries a request's policy snapshot in its context.
type policyKey struct{}

// accessPolicy returns the policy: on a request's handler, the request's
// snapshot (taken on first use); otherwise the current policy.
func (h *ToolsHandler) accessPolicy() accessPolicy {
	if h.req == nil {
		return h.readAccessPolicy()
	}
	h.req.policyOnce.Do(func() { h.req.policy = h.readAccessPolicy() })
	return h.req.policy
}

// readAccessPolicy reads the current policy.
func (h *ToolsHandler) readAccessPolicy() accessPolicy {
	rule, err := h.sourceRule(libgitignore.ArkSource)
	return h.newPolicy(rule, err)
}

// pathPolicy returns a policy that decides rel (relative to the root) and the
// directories above it exactly as accessPolicy does, built from only the rule
// files that can apply to them (libgitignore.IgnoreReader.For) — through the
// request's reader, so it sees the same version of each file as the rest of
// the request. Naming one file thus costs a few file reads, not a walk of the
// repository. A directory above rel that cannot be examined refuses rel.
func (h *ToolsHandler) pathPolicy(rel string) accessPolicy {
	files, err := h.ignoreReader().For(filepath.ToSlash(rel))
	var rule *libgitignore.GitIgnore
	if err == nil {
		rule, err = files.CompileSource(libgitignore.ArkSource)
	}
	return h.newPolicy(rule, err)
}

// newPolicy is the policy of a compiled rule (or of the error that left it
// nil).
func (h *ToolsHandler) newPolicy(rule *libgitignore.GitIgnore, err error) accessPolicy {
	root, absErr := filepath.Abs(h.rootDir)
	if absErr != nil {
		root = h.rootDir
	}
	if rule == nil && err == nil {
		err = fmt.Errorf("no rule was built")
	}
	if err != nil {
		if _, statErr := os.Stat(root); errors.Is(statErr, fs.ErrNotExist) {
			// A root that does not exist holds no file to read or protect.
			rule, err = libgitignore.NewGitIgnore(), nil
		}
	}
	p := accessPolicy{rule: rule, err: err, allowExternal: h.opt != nil && h.opt.AllowExternalSymlinks}
	p.roots = []string{root}
	if real, realErr := filepath.EvalSymlinks(root); realErr == nil && real != root {
		p.roots = append(p.roots, real)
	}
	return p
}

// inside reports whether an absolute path lies in the root.
func (p accessPolicy) inside(path string) bool {
	for _, root := range p.roots {
		if _, ok := relInside(root, filepath.Clean(path)); ok {
			return true
		}
	}
	return false
}

// excludes reports whether path — absolute, or relative to the server root —
// is excluded. A path outside the root is not this policy's to decide (the
// path gate refuses it).
func (p accessPolicy) excludes(path string) bool {
	if !filepath.IsAbs(path) {
		return p.excludesRel(filepath.Clean(path))
	}
	for _, root := range p.roots {
		if rel, ok := relInside(root, filepath.Clean(path)); ok {
			return p.excludesRel(rel)
		}
	}
	return false
}

// excludesOwn is excludes without the parent directories: for a walk entry,
// whose parents the walk has already admitted.
func (p accessPolicy) excludesOwn(path string) bool {
	for _, root := range p.roots {
		if rel, ok := relInside(root, filepath.Clean(path)); ok {
			if rel == "." {
				return false
			}
			return p.err != nil || p.rule.MatchesRel(filepath.ToSlash(rel))
		}
	}
	return false
}

// excludesRel reports whether rel (relative to the root) or one of its parent
// directories matches the rules.
func (p accessPolicy) excludesRel(rel string) bool {
	if rel == "." || rel == "" {
		return false
	}
	if p.err != nil {
		return true
	}
	rel = filepath.ToSlash(rel)
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' && p.rule.MatchesRel(rel[:i]) {
			return true
		}
	}
	return p.rule.MatchesRel(rel)
}

// empty reports whether the policy excludes nothing (no rule, no error), so
// walks need no per-entry work.
func (p accessPolicy) empty() bool {
	return p.err == nil && (p.rule == nil || len(p.rule.Patterns()) == 0)
}

// errNotCanonical marks a refusal because a path's canonical form could not
// be established.
var errNotCanonical = errors.New("the path could not be verified against the file system")

// fsCanonicalPath returns rel (relative to the root, as the path gate computed
// it) as the file system names it: canonical, each component as its
// directory lists it, and real, the symlink-free path inside the root it
// leads to ("" when outside the root or unknown). A path that does not
// resolve (missing, dangling, looping, below a file, outside the root when
// that is refused) is returned as is with real "": the gate's own checks
// decide it, and nothing can be read through it — unless the path API still
// reaches a file by it (Windows drops trailing dots and spaces from names),
// which is refused. Any other failure is an error: the path is refused.
func fsCanonicalPath(root, rel string, allowExternal bool) (canonical, real string, err error) {
	return canonicalBy(root, rel, func(rel string) (string, string, error) {
		return fsroot.Canonicalize(root, rel, fsroot.Options{AllowExternalSymlinks: allowExternal})
	})
}

// canonicalBy is fsCanonicalPath with the resolution given: a tree pinned
// for the call, or the request's (requestCanonical).
func canonicalBy(root, rel string, resolve func(rel string) (canonical, real string, err error)) (canonical, real string, err error) {
	if rel == "." || rel == "" {
		return ".", ".", nil
	}
	canonical, real, err = resolve(filepath.ToSlash(rel))
	switch {
	case err == nil:
		return filepath.FromSlash(canonical), filepath.FromSlash(real), nil
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fsroot.ErrNotDir):
		if _, statErr := os.Stat(filepath.Join(root, rel)); statErr == nil {
			return "", "", fmt.Errorf("%q names a file only by the path API's own rewriting: %w", rel, err)
		}
		return rel, "", nil
	case errors.Is(err, fsroot.ErrOutsideRoot), errors.Is(err, fsroot.ErrSymlinkLoop):
		return rel, "", nil
	}
	return "", "", err
}

// excludesCanonical reports whether the walk entry path (a symlink) leads,
// as the file system names it, to an excluded file inside the root — the
// link's own text may spell the target in another case, or reach the root by
// another spelling of its path. A target that cannot be verified is
// excluded; one that cannot be reached is not (nothing is read through it).
func (p accessPolicy) excludesCanonical(path string) bool {
	for _, root := range p.roots {
		rel, ok := relInside(root, filepath.Clean(path))
		if !ok {
			continue
		}
		_, real, err := fsCanonicalPath(root, rel, p.allowExternal)
		if err != nil {
			return true
		}
		return real != "" && p.excludesRel(real)
	}
	return false
}

// excludesEntry reports whether a walk entry is excluded: by its own path or,
// for a symlink, by where it leads — outside the root unless external
// symlinks are allowed, or to an excluded file inside it, which a link must
// not expose under another name. A walk never enters an excluded directory
// and starts at a path the gate admitted, so the entry's parents are known
// not to be excluded: only the entry itself is matched.
func (p accessPolicy) excludesEntry(path string, symlink bool) bool {
	if !p.empty() && p.excludesOwn(path) {
		return true
	}
	if !symlink {
		return false
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false // dangling or looping: nothing can be read through it
	}
	if !p.inside(real) {
		if !p.allowExternal {
			return true
		}
		// Outside by its spelling; inside, perhaps, by identity.
		return !p.empty() && p.excludesCanonical(path)
	}
	return !p.empty() && (p.excludes(real) || p.excludesCanonical(path))
}

// excludesWalked is excludesEntry for a path whose entry type is unknown
// (Option.AccessExclude): it inspects the path itself.
func (p accessPolicy) excludesWalked(path string) bool {
	fi, err := os.Lstat(path)
	return p.excludesEntry(path, err == nil && fi.Mode()&fs.ModeSymlink != 0)
}

// indexExclude adapts the policy to index.Exclude.
func (p accessPolicy) indexExclude(path string, d fs.DirEntry) bool {
	return p.excludesEntry(path, d.Type()&fs.ModeSymlink != 0)
}

// excludedPathError is the error for a path the policy excludes: the same
// words as for a path that does not exist, so a refusal reveals nothing about
// the file. A policy that could not be read says so.
func (p accessPolicy) excludedPathError(path string) error {
	if p.err != nil {
		return fmt.Errorf("path %q: %w: %v", path, errPolicyUnavailable, p.err)
	}
	return fmt.Errorf("path %q does not exist", path)
}

// walkFrom walks start like filepath.Walk, entering it when it is the server
// root given through a symlink (common.WalkRoot).
func (h *ToolsHandler) walkFrom(start string, fn filepath.WalkFunc) error {
	if common.SamePath(start, h.rootDir) {
		return common.WalkRoot(start, fn)
	}
	return filepath.Walk(start, fn)
}
