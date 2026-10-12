package core

import (
	"fmt"

	"github.com/magicdrive/ark/internal/accesspolicy"
	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/fsroot"
	"github.com/magicdrive/ark/internal/safewalk"
	"github.com/magicdrive/ark/internal/secretpolicy"
)

// attachFileAccess makes the dump reach its files through a pinned tree
// under a policy snapshot (internal/safewalk): the target is pinned once,
// its ignore files are captured once (.arkignore, the additional rule files
// and, with .gitignore handling on, .gitignore), and every listing, walk and
// read is decided by them — a file read is the file decided, never a path
// opened again. Symlinks are followed as the dump always followed them,
// outside the target too. With masking on it also loads the target's
// secret rules configuration (opt.SecretPolicy). It returns the function
// that releases the access.
func attachFileAccess(opt *commandline.Option) (func(), error) {
	if opt.FS != nil {
		return func() {}, nil
	}
	tree, err := fsroot.Pin(opt.TargetDirname, fsroot.Options{AllowExternalSymlinks: true})
	if err != nil {
		return nil, fmt.Errorf("a directory not found: %s: %w", opt.TargetDirname, err)
	}
	// The masking policy: the target's secret rules configuration, read
	// once from the pinned root, applied to every file. A configuration that
	// cannot be read or compiled fails the dump.
	ownPolicy := false
	if opt.MaskSecretsFlag.Bool() && opt.SecretPolicy == nil {
		loaded, err := secretpolicy.Load(tree, secretpolicy.Options{AllowWeakening: true})
		if err != nil {
			tree.Close()
			return nil, fmt.Errorf("secret rules: %w", err)
		}
		opt.SecretPolicy, ownPolicy = loaded.Policy, true
	}
	dropPolicy := func() {
		if ownPolicy {
			opt.SecretPolicy = nil
		}
	}
	snap, err := accesspolicy.Build(tree, accesspolicy.Options{AdditionalRuleFiles: opt.AdditionallyIgnoreRuleFilenameList})
	if err != nil {
		dropPolicy()
		tree.Close()
		return nil, fmt.Errorf("ignore rules: %w", err)
	}
	if err := snap.Err(); err != nil {
		dropPolicy()
		snap.Close()
		tree.Close()
		return nil, fmt.Errorf("ignore rules: %w", err)
	}
	rules, err := snap.RuleSet(opt.AllowGitignoreFlag.Bool())
	if err != nil {
		dropPolicy()
		snap.Close()
		tree.Close()
		return nil, fmt.Errorf("ignore rules: %w", err)
	}
	w := safewalk.New(snap, opt.TargetDirname)
	if rules.Git != nil {
		w.WithRule(rules.Git)
	}
	opt.FS = w
	return func() {
		opt.FS = nil
		dropPolicy()
		snap.Close()
		tree.Close()
	}, nil
}
