package core

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/secrets"
)

func WriteAllFiles(treeStr string, root string, outputPath string, allowedFileListMap map[string]bool, opt *commandline.Option) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	writer := bufio.NewWriter(outFile)
	defer writer.Flush()

	var abspath string

	abspath, err = filepath.Abs(root)
	if err != nil {
		abspath = root
	}
	projectName := filepath.Base(abspath)

	writer.WriteString(PrependDescriptionWithFormat(projectName, root, opt.OutputFormat))

	if opt.OutputFormat == "markdown" {
		writer.WriteString("# Project Tree\n\n```\n" + root + "\n" + treeStr + "\n```\n")
	} else {
		writer.WriteString(root + "\n" + treeStr + "\n")
	}

	err = filepath.WalkDir(root, func(fpath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if IsUnderGitDir(fpath) {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if _, ok := allowedFileListMap[fpath]; !ok {
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

		decodedBytes, err := io.ReadAll(decoded)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", fpath, err)
		}

		if opt.DeleteCommentsFlag {
			lang := detectLanguageTag(fpath)
			pattern := getCommentDelimiters(lang)
			decodedBytes = stripComments(decodedBytes, pattern)
		}

		var content = string(decodedBytes)

		if opt.MaskSecretsFlag.Bool() {
			content = secrets.MaskAll(content)
		}

		if opt.OutputFormat == "markdown" {
			writer.WriteString("\n---\n\n")
			fmt.Fprintf(writer, "# File: %s\n", fpath)
			fmt.Fprintf(writer, "```%s\n", detectLanguageTag(fpath))
		} else {
			fmt.Fprintf(writer, "\n=== %s ===\n", fpath)
		}

		scanner := bufio.NewScanner(strings.NewReader(content))
		maxCapacity, _ := opt.ScanBuffer.Bytes()
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, maxCapacity)

		lineNumber := 1
		for scanner.Scan() {
			line := scanner.Text()
			if opt.WithLineNumberFlag.Bool() && opt.OutputFormat != "markdown" {
				fmt.Fprintf(writer, "%6d: %s\n", lineNumber, line)
			} else {
				writer.WriteString(line + "\n")
			}
			lineNumber++
		}
		if err := scanner.Err(); err != nil {
			return err
		}

		if opt.OutputFormat == "markdown" {
			writer.WriteString("```\n")
		}
		return nil
	})

	return err
}
