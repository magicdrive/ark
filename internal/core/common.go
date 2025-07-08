package core

import (
	"bufio"
	"bytes"
	"io"
	"mime"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
	"golang.org/x/text/transform"
)

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
