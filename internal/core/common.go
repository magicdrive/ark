package core

import (
	"bufio"
	"bytes"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
	"golang.org/x/text/transform"

	"github.com/magicdrive/ark/internal/commandline"
)

func IsArkReadable(data []byte, path string, opt *commandline.Option) bool {
	absPath, _ := ToAbs(path)
	relPath, _ := ToRel(opt.TargetDirname, path)
	basename := filepath.Base(relPath)

	var result = true
	result = result && opt.IgnoreDotFileFlag.Bool() && IsHiddenFile(basename)
	result = result && IsUnderGitDir(absPath)
	result = result && IsBinary(data)
	result = result && IsImage(relPath)

	return result
}

func IsHiddenFile(name string) bool {
	return strings.HasPrefix(name, ".")
}

func IsUnderGitDir(path string) bool {
	absPath := filepath.Clean(path)
	parts := strings.Split(absPath, string(filepath.Separator))
	if slices.Contains(parts, ".git") {
		return true
	}
	return false
}

func IsBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.Contains(data, []byte{0x00}) {
		return true
	}
	if !utf8.Valid(data) {
		return true
	}
	controlCount := 0
	for _, b := range data {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
			controlCount++
		}
	}
	controlRatio := float64(controlCount) / float64(len(data))
	return controlRatio > 0.1
}

func IsImage(filename string) bool {
	ext := strings.ToLower(path.Ext(filename))
	mimeType := mime.TypeByExtension(ext)
	return strings.HasPrefix(mimeType, "image/")
}

func ConvertToUTF8(r io.Reader) (io.Reader, error) {
	buf := bufio.NewReader(r)
	peek, err := buf.Peek(1024)
	if err != nil && err != io.EOF {
		return nil, err
	}
	encoding, _, _ := charset.DetermineEncoding(peek, "")
	return transform.NewReader(buf, encoding.NewDecoder()), nil
}

func DeleteComments(data []byte, fpath string) []byte {
	lang := detectLanguageTag(fpath)
	pattern := getCommentDelimiters(lang)
	result := stripComments(data, pattern)
	return result

}

func ToAbs(p string) (string, error) {
	p = expandHome(p)
	p = os.ExpandEnv(p)    // $HOME, ${VAR}, etc.
	p = filepath.Clean(p)  // remove ./, ../, duplicate slashes
	return filepath.Abs(p) // make absolute & resolve symlinks where possible
}

func ToRel(baseDir, targetPath string) (string, error) {
	var err error

	if baseDir == "" {
		baseDir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}

	baseDir, err = ToAbs(baseDir)
	if err != nil {
		return "", err
	}

	targetPath, err = ToAbs(targetPath)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil {
		return "", err
	}
	return filepath.Clean(rel), nil
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
