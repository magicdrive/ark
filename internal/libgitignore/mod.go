package libgitignore

import (
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

