package secretpolicy

import (
	"fmt"

	"github.com/magicdrive/ark/internal/fsroot"
)

// Load reads the configuration at the root of tree and compiles it. The
// file is read from the root's own directory handle without following
// symlinks (fsroot.Tree.ReadChain): a configuration is a regular file at the
// root, listed under exactly its name. A symlink, a directory or a device
// under either name, both names present, or a file that cannot be read is
// an error. With neither name present the policy is the recommended one.
//
// Masking never decides access: what Load reads is independent of the
// .arkignore access policy, and a configuration changes nothing about which
// files can be read.
func Load(tree *fsroot.Tree, opt Options) (*Loaded, error) {
	name, data, err := Read(tree)
	if err != nil {
		return nil, err
	}
	if name == "" {
		return None(opt), nil
	}
	return Build(name, data, opt)
}

// Read reads the configuration file at the root of tree, as Load does: its
// name and bytes, or "" and nil when there is none.
func Read(tree *fsroot.Tree) (name string, data []byte, err error) {
	type found struct {
		name string
		data []byte
	}
	var present []found
	for _, name := range []string{FileName, AltFileName} {
		chain, err := tree.ReadChain(".", name, nil)
		if err != nil {
			return "", nil, &Error{File: name, Msg: fmt.Sprintf("cannot be read: %v", err)}
		}
		f := chain[0].File
		switch {
		case !f.Present:
			continue
		case f.Link:
			return "", nil, &Error{File: name, Msg: "is a symbolic link: the configuration must be a regular file at the repository root"}
		case f.NotRegular:
			return "", nil, &Error{File: name, Msg: "is not a regular file"}
		case f.Err != nil:
			return "", nil, &Error{File: name, Msg: fmt.Sprintf("cannot be read: %v", f.Err)}
		}
		present = append(present, found{name, f.Data})
	}
	switch len(present) {
	case 0:
		return "", nil, nil
	case 2:
		return "", nil, &Error{File: FileName, Msg: fmt.Sprintf("both %s and %s exist: keep one", FileName, AltFileName)}
	}
	return present[0].name, present[0].data, nil
}
