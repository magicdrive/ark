package libgitignore

import (
	"bufio"
	"os"
	"strings"
)

func ReadFilesAsLines(workingDir string, files []string) ([]string, error) {
	var lines []string
	normalizedWorkingDir := ensureTrailingSlash(workingDir)
	for _, path := range files {
		loadPath := normalizedWorkingDir + removeLeadingSlash(path)
		f, err := os.Open(loadPath)
		if err != nil {
			//return nil, err
			continue
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return lines, nil
}

func AppendIgnoreFileList(gi *GitIgnore, workingDir string, fileList []string) (*GitIgnore, error) {
	var fileLine []string
	var err error
	fileLine, err = ReadFilesAsLines(workingDir, fileList)

	if err != nil {
		return nil, err
	}
	return AppendIgnoreLines(gi, fileLine...)
}

func NewGitignore(workingDir string, fileList []string) (*GitIgnore, error) {
	var fileLine []string
	var err error
	fileLine, err = ReadFilesAsLines(workingDir, fileList)

	if err != nil {
		return nil, err
	}
	return CompileIgnoreLines(fileLine, workingDir, 1)
}

func ensureTrailingSlash(path string) string {
	if path == "" {
		return path
	}
	if strings.HasSuffix(path, "/") {
		return path
	}
	return path + "/"
}

func removeLeadingSlash(path string) string {
	if strings.HasPrefix(path, "/") {
		return path[1:]
	}
	return path
}
