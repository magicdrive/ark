package core

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/secrets"
	"github.com/magicdrive/ark/internal/textbank"
)

const newlineToken = "␤"

func WriteAllFilesAsArklite(treeStr, root, outputPath string, allowedFileListMap map[string]bool, opt *commandline.Option) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	writer := bufio.NewWriter(outFile)
	defer writer.Flush()

	abspath, _ := filepath.Abs(root)
	projectName := filepath.Base(abspath)

	fmt.Fprintf(writer, textbank.DescriptionTemplateArklite, projectName, abspath)
	writer.WriteString("## Directory Tree (JSON)\n")
	writer.WriteString(treeStr)
	writer.WriteString("\n")
	writer.WriteString("\n")
	writer.WriteString("## File Dump\n")

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {

		if err != nil || d.IsDir() {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if allowedFileListMap != nil {
			if _, ok := allowedFileListMap[path]; !ok {
				return nil
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			log.Println(err.Error())
			return err
		}
		if !utf8.Valid(data) {
			return nil // skip non-UTF-8 or binary
		}

		// Strip block and line comments
		lang := detectLanguageTag(path)
		pattern := getCommentDelimiters(lang)
		data = stripComments(data, pattern)

		if opt.MaskSecretsFlag.Bool() {
			content := secrets.MaskAll(string(data))
			data = []byte(content)
		}
		lines := bytes.Split(data, []byte("\n"))

		var compact bytes.Buffer
		for _, line := range lines {
			trim := bytes.TrimSpace(line)
			if len(trim) == 0 {
				continue
			}
			if compact.Len() > 0 {
				compact.WriteString(newlineToken)
			}
			compact.Write(trim)
		}

		writer.WriteString("@" + rel + "\n")
		writer.Write(compact.Bytes())
		writer.WriteByte('\n')

		return nil
	})

	return err
}
