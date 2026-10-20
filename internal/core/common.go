package core

import (
	"bytes"
	"io"
	"mime"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/magicdrive/ark/internal/textenc"
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

// ConvertToUTF8 returns r's content as UTF-8 text, decoded from its encoding
// (textenc.Decode: a byte order mark, UTF-8, ISO-2022-JP, or a legacy
// encoding told from the bytes). Content that is not text in an encoding it
// can tell is returned as it is.
func ConvertToUTF8(r io.Reader) (io.Reader, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if res := textenc.Decode(data, ""); res.Kind == textenc.Text {
		return strings.NewReader(res.Text), nil
	}
	return bytes.NewReader(data), nil
}

func DeleteComments(data []byte, fpath string) []byte {
	lang := detectLanguageTag(fpath)
	pattern := getCommentDelimiters(lang)
	result := stripComments(data, pattern)
	return result

}
