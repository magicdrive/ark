package core

import (
	"io/fs"
	"path/filepath"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/secrets"
)

// maskContent masks one dumped file's content with the run's policy
// (opt.MaskPolicy), when masking is on. The file's syntax is decided as the
// MCP server decides it (secrets.SyntaxForFile) from the entry the walk
// listed: a regular file's name; a symlink is read as text of no known
// syntax, so a link named ".tf" never makes another file's value a kept
// reference.
func maskContent(opt *commandline.Option, path string, d fs.DirEntry, content string) string {
	if !opt.MaskSecretsFlag.Bool() {
		return content
	}
	syntax := secrets.SyntaxUnknown
	if d != nil && d.Type()&fs.ModeSymlink == 0 {
		syntax = secrets.SyntaxForFile(filepath.Base(path))
	}
	return opt.MaskPolicy().Mask(content, secrets.Mode{Syntax: syntax})
}
