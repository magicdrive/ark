package core

// IsMetadataDirName reports whether name is a repository metadata directory
// that file traversal never descends into: ".git" (version control) and ".ark"
// (Ark's own index cache, which would otherwise be listed, searched and parsed
// as if it were repository content).
func IsMetadataDirName(name string) bool {
	return name == ".git" || name == ".ark"
}
