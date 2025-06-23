package libgitignore

import (
	"os"
	"path/filepath"
)

// GenerateIntegratedGitIgnore collects all .gitignore under root recursively
func GenerateIntegratedGitIgnore(allowGitignore bool, root string, additionallyFileList []string) (*GitIgnore, error) {
	gi := NewGitIgnore()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			gitignore := filepath.Join(path, ".gitignore")
			arkignore := filepath.Join(path, ".arkignore")
			if _, err := os.Stat(gitignore); allowGitignore && err == nil {
				_, err := AppendIgnoreFileWithDir(gi, gitignore, path)
				if err != nil {
					return err
				}
			} else if _, err := os.Stat(arkignore); err == nil {
				_, err := AppendIgnoreFileWithDir(gi, gitignore, path)
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
			AppendIgnoreFileWithDir(gi, ignoreFilePath, root)
		}
	}

	gi.Root = root
	return gi, nil
}
