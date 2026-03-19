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

	"github.com/magicdrive/ark/internal/chardetect"
	"golang.org/x/text/encoding/japanese"
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

	// Peek at the first 8KB to detect encoding
	peek, err := buf.Peek(8192)
	if err != nil && err != io.EOF {
		// If we can't peek 8KB, try smaller size
		peek, err = buf.Peek(1024)
		if err != nil && err != io.EOF {
			return nil, err
		}
	}

	// Detect character encoding using our chardetect package
	result := chardetect.Detect(peek)

	// If already UTF-8 or ASCII, return as-is
	if result.Encoding == chardetect.UTF8 || result.Encoding == chardetect.ASCII {
		return buf, nil
	}

	// Select appropriate decoder based on detected encoding
	var decoder transform.Transformer

	switch result.Encoding {
	case chardetect.ShiftJIS, chardetect.CP932:
		decoder = japanese.ShiftJIS.NewDecoder()
	case chardetect.EUCJP:
		decoder = japanese.EUCJP.NewDecoder()
	case chardetect.ISO2022JP:
		decoder = japanese.ISO2022JP.NewDecoder()
	default:
		// Unknown or unhandled encoding - return as-is
		// This is safer than failing, as the caller can decide how to handle it
		return buf, nil
	}

	return transform.NewReader(buf, decoder), nil
}

func DeleteComments(data []byte, fpath string) []byte {
	lang := detectLanguageTag(fpath)
	pattern := getCommentDelimiters(lang)
	result := stripComments(data, pattern)
	return result

}
