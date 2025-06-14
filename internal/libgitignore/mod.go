package libgitignore

func GenerateIntegratedGitIgnore(workingDir string, gitignorePath string, additionallyFileList []string) *GitIgnore {
	var gi *GitIgnore

	if gitignorePath != "" {
		gi, _ = CompileIgnoreFile(gitignorePath)
	} else {
		gi = NewPlainIgnoreRule()
	}

	if res, err := AppendIgnoreFileList(gi, workingDir, additionallyFileList); err == nil {
		gi = res
	}
	return gi
}
