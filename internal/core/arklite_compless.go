package core

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicdrive/ark/internal/model"
	"github.com/magicdrive/ark/internal/textbank"
)

func Compless(path string, format model.OutputFormat) error {

	/*-----------*/
	/* Read file */
	/*-----------*/

	data, err := os.ReadFile(path)
	if err != nil {
		return err
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

	/*------------*/
	/* Write file */
	/*------------*/

	outFile, err := os.Create(path)
	if err != nil {
		return err
	}
	defer outFile.Close()

	writer := bufio.NewWriter(outFile)
	defer writer.Flush()
	expantionFilename := strings.TrimSuffix(path, ".arklite")

	abspath, _ := filepath.Abs(path)
	projectName := filepath.Base(abspath)
	fmt.Fprintf(writer, textbank.ArkliteComplessHeaderTemplate, projectName, abspath, format.String())
	writer.WriteString("## Directory Tree (JSON)\n")
	fmt.Fprintf(writer, `{ "type": "file", "name": "%s" }`, expantionFilename)
	writer.WriteString("\n")
	writer.WriteString("\n")
	writer.WriteString("## File Dump\n")
	writer.WriteString("@")
	writer.WriteString(expantionFilename)
	writer.WriteByte('\n')
	writer.Write(compact.Bytes())
	writer.WriteByte('\n')

	return err
}
