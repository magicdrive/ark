package libgitignore

import (
	"crypto/sha256"
	"errors"
	"path/filepath"
)

// CapturedFile is an ignore file a caller read itself — through a pinned,
// descriptor-anchored tree — instead of IgnoreReader reading it by path: the
// directory it is in (absolute, as the root is spelled), its name
// (".gitignore" or ".arkignore") and its bytes, or why it could not be read.
type CapturedFile struct {
	Dir  string
	Name string
	Data []byte
	Err  error
}

// IgnoreFilesFromCaptured builds the IgnoreFiles of root from captured files
// (in the walk order of their directories) and the additional rule files,
// which are read here as IgnoreReader reads them. The result compiles,
// matches and fingerprints exactly as IgnoreReader.All's would for the same
// bytes.
func IgnoreFilesFromCaptured(root string, captured []CapturedFile, additionallyFileList []string) (*IgnoreFiles, error) {
	absRoot := ToAbsDir(root)
	f := &IgnoreFiles{root: absRoot}
	index := map[string]int{}
	for _, c := range captured {
		file := &ignoreFile{path: filepath.Join(c.Dir, c.Name)}
		if c.Err != nil {
			file.err = c.Err
		} else {
			file.sum = sha256.Sum256(c.Data)
			file.lines, file.err = scanLines(c.Data)
		}
		i, ok := index[c.Dir]
		if !ok {
			i = len(f.dirs)
			index[c.Dir] = i
			f.dirs = append(f.dirs, ignoreDir{dir: c.Dir})
		}
		switch c.Name {
		case ".gitignore":
			f.dirs[i].git = file
		case ".arkignore":
			f.dirs[i].ark = file
		default:
			return nil, errors.New("libgitignore: captured file is not an ignore file: " + c.Name)
		}
	}
	extra, err := NewIgnoreReader(absRoot, additionallyFileList).extraFiles()
	if err != nil {
		return nil, err
	}
	f.extra = extra
	f.fingerprint = f.digest()
	return f, nil
}

// WithCaptured returns the IgnoreFiles of the same root and additional rule
// files as f — read once, by the call that made f — with captured as its
// directories' files instead (in the walk order of their directories). A
// caller that captures directories as it needs them compiles each decision's
// rules without reading the additional rule files again.
func (f *IgnoreFiles) WithCaptured(captured []CapturedFile) (*IgnoreFiles, error) {
	g, err := IgnoreFilesFromCaptured(f.root, captured, nil)
	if err != nil {
		return nil, err
	}
	g.extra = f.extra
	g.fingerprint = g.digest()
	return g, nil
}
