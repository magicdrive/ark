package core

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
	"golang.org/x/text/transform"

	"github.com/magicdrive/ark/internal/commandline"
)

func ReadAndWriteAllFiles(treeStr string, root string, outputPath string, opt *commandline.Option) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	writer := bufio.NewWriter(outFile)
	defer writer.Flush()

	writer.WriteString(treeStr + "\n")

	err = filepath.WalkDir(root, func(fpath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if isHiddenFile(fpath) {
			return nil
		}

		data, err := os.ReadFile(fpath)
		if err != nil {
			return err
		}

		if isBinary(data) || isImage(fpath) {
			return nil
		}

		decoded, err := convertToUTF8(bytes.NewReader(data))
		if err != nil {
			if opt.SkipNonUTF8Flag {
				return nil
			}
			return fmt.Errorf("failed to convert %s: %w", fpath, err)
		}

		fmt.Fprintf(writer, "\n=== %s ===\n", fpath)

		scanner := bufio.NewScanner(decoded)
		lineNumber := 1
		for scanner.Scan() {
			line := scanner.Text()
			if opt.WithLineNumberFlag {
				fmt.Fprintf(writer, "%6d: %s\n", lineNumber, line)
			} else {
				writer.WriteString(line + "\n")
			}
			lineNumber++
		}
		return scanner.Err()
	})

	return err
}

func isBinary(data []byte) bool {
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

func isImage(filename string) bool {
	ext := strings.ToLower(path.Ext(filename))
	mimeType := mime.TypeByExtension(ext)
	return strings.HasPrefix(mimeType, "image/")
}

func convertToUTF8(r io.Reader) (io.Reader, error) {
	buf := bufio.NewReader(r)
	peek, err := buf.Peek(1024)
	if err != nil && err != io.EOF {
		return nil, err
	}
	encoding, _, _ := charset.DetermineEncoding(peek, "")
	return transform.NewReader(buf, encoding.NewDecoder()), nil
}
