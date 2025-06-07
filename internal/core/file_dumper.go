package core

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type ReadOption struct {
	WithLineNumber bool
	ShowHeader     bool
	SkipNonUTF8    bool
}

func ReadAllFilesRecursively(root string, opt ReadOption) (string, error) {
	var builder strings.Builder

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// check UTF-8
		if opt.SkipNonUTF8 && !utf8.Valid(data) {
			return nil
		}

		// remove UTF-8 BOM
		data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

		if opt.ShowHeader {
			builder.WriteString(fmt.Sprintf("\n=== %s ===\n", path))
		}

		if opt.WithLineNumber {
			scanner := bufio.NewScanner(bytes.NewReader(data))
			lineNumber := 1
			for scanner.Scan() {
				builder.WriteString(fmt.Sprintf("%6d: %s\n", lineNumber, scanner.Text()))
				lineNumber++
			}
			if err := scanner.Err(); err != nil {
				return err
			}
		} else {
			builder.Write(data)
			if len(data) > 0 && data[len(data)-1] != '\n' {
				builder.WriteByte('\n')
			}
		}

		return nil
	})

	if err != nil {
		return "", err
	}
	return builder.String(), nil
}
