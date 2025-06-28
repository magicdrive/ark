package core

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/secrets"
	"github.com/magicdrive/ark/internal/textbank"
)

func WriteAllFilesAsXML(treeStr string, root string, outputPath string, allowedFileListMap map[string]bool, opt *commandline.Option) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()
	writer := bufio.NewWriter(outFile)
	defer writer.Flush()

	abspath, _ := filepath.Abs(root)
	projectName := filepath.Base(abspath)

	writer.WriteString(xml.Header)
	writer.WriteString("<ProjectDump>\n")
	fmt.Fprintf(writer, textbank.DescriptionTemplateXML, xmlEscape(projectName), xmlEscape(abspath))
	writer.WriteString("\n")
	writer.WriteString("<Tree>")
	writer.WriteString("\n")
	writer.WriteString("<![CDATA[")
	writer.WriteString("\n")
	writer.WriteString(treeStr)
	writer.WriteString("]]>")
	writer.WriteString("\n")
	writer.WriteString("</Tree>")
	writer.WriteString("\n")

	err = writeXMLDirectory(writer, root, allowedFileListMap, opt)
	if err != nil {
		return err
	}

	writer.WriteString("</ProjectDump>\n")
	return nil
}

func writeXMLDirectory(writer *bufio.Writer, dir string, allowedFileListMap map[string]bool, opt *commandline.Option) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() && !entries[j].IsDir() {
			return true
		}
		if !entries[i].IsDir() && entries[j].IsDir() {
			return false
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	for _, entry := range entries {
		name := entry.Name()
		fpath := filepath.Join(dir, name)

		if IsUnderGitDir(fpath) {
			continue
		}
		if allowedFileListMap != nil {
			if _, ok := allowedFileListMap[fpath]; !ok {
				continue
			}
		}
		if entry.IsDir() {
			fmt.Fprintf(writer, `<directory name="%s">`, xmlEscape(name))
			writer.WriteString("\n")
			err := writeXMLDirectory(writer, fpath, allowedFileListMap, opt)
			writer.WriteString("</directory>\n")
			if err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(fpath)
			if err != nil {
				return err
			}
			if isBinary(data) || isImage(fpath) {
				continue
			}
			decoded, err := convertToUTF8(bytes.NewReader(data))
			if err != nil {
				if opt.SkipNonUTF8Flag {
					continue
				}
				return fmt.Errorf("failed to convert %s: %w", fpath, err)
			}

			decodedBytes, err := io.ReadAll(decoded)
			if opt.DeleteCommentsFlag {
				lang := detectLanguageTag(fpath)
				pattern := getCommentDelimiters(lang)
				decodedBytes = stripComments(decodedBytes, pattern)
			}

			if err != nil {
				return fmt.Errorf("failed to read %s: %w", fpath, err)
			}
			content := string(decodedBytes)
			if opt.MaskSecretsFlag.Bool() {
				content = secrets.MaskAll(content)
			}
			lang := detectLanguageTag(fpath)
			fmt.Fprintf(writer, `<file name="%s" language="%s">`, xmlEscape(name), xmlEscape(lang))
			writer.WriteString("\n<![CDATA[\n")
			writer.WriteString(xmlEscapeForCDATA(content))
			writer.WriteString("\n]]>\n")
			writer.WriteString("</file>\n")
		}
	}
	return nil
}

func xmlEscape(s string) string {
	var buf strings.Builder
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

func xmlEscapeForCDATA(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
}
