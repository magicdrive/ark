package libgitignore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
	"path/filepath"
)

func GenerateIntegratedGitIgnore(allowGitignore bool, root string, additionallyFileList []string) (*GitIgnore, error) {
	absRoot := ToAbsDir(root)
	gi := NewGitIgnore()
	gi.Root = absRoot

	err := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Repository metadata (.git) and Ark's own cache (.ark) hold no
			// ignore files that apply to repository content.
			if path != absRoot && (d.Name() == ".git" || d.Name() == ".ark") {
				return filepath.SkipDir
			}
			gitignore := filepath.Join(path, ".gitignore")
			arkignore := filepath.Join(path, ".arkignore")
			if _, err := os.Stat(gitignore); allowGitignore && err == nil {
				_, err := AppendIgnoreFileWithDir(gi, gitignore, path)
				if err != nil {
					return err
				}
			} else if _, err := os.Stat(arkignore); err == nil {
				_, err := AppendIgnoreFileWithDir(gi, arkignore, path)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(additionallyFileList) != 0 {
		for _, ignoreFilePath := range additionallyFileList {
			_, err := AppendIgnoreFileWithDir(gi, ignoreFilePath, absRoot)
			if err != nil {
				return nil, err
			}
		}
	}

	return gi, nil
}

// IgnoreFilesFingerprint identifies the ignore rules GenerateIntegratedGitIgnore
// would build for root: the path and content of every .gitignore / .arkignore
// it would read (same walk) and of each additional rule file. Equal
// fingerprints mean an equal rule set.
func IgnoreFilesFingerprint(root string, additionallyFileList []string) (string, error) {
	absRoot := ToAbsDir(root)
	h := sha256.New()
	err := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != absRoot && (d.Name() == ".git" || d.Name() == ".ark") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == ".gitignore" || d.Name() == ".arkignore" {
			writeFileDigest(h, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	for _, f := range additionallyFileList {
		writeFileDigest(h, f)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeFileDigest(h hash.Hash, path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(h, "%s unreadable\n", path)
		return
	}
	fmt.Fprintf(h, "%s %x\n", path, sha256.Sum256(b))
}
