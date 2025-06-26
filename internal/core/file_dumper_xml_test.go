package core_test

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
	"github.com/magicdrive/ark/internal/model"
)

type ProjectDump struct {
	XMLName xml.Name `xml:"ProjectDump"`
	Tree    string   `xml:"Tree"`
}

func TestWriteAllFilesAsXML_Basic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subdir", "sub.txt"), []byte("sub content"), 0644); err != nil {
		t.Fatal(err)
	}

	treeStr := "├── main.go\n└── subdir/\n    └── sub.txt\n"
	outFile := filepath.Join(dir, "out.xml")

	fileList := map[string]bool{
		filepath.Join(dir, "main.go"):           true,
		filepath.Join(dir, "subdir"):            true,
		filepath.Join(dir, "subdir", "sub.txt"): true,
	}

	opt := &commandline.Option{
		OutputFormat:       model.OutputFormat("xml"),
		WithLineNumberFlag: model.OnOffSwitch("off"),
		MaskSecretsFlag:    model.OnOffSwitch("off"),
		ScanBuffer:         model.ByteString("1M"),
		WorkingDir:         dir,
		IgnoreDotFileFlag:  model.OnOffSwitch("off"),
		AllowGitignoreFlag: model.OnOffSwitch("off"),
	}

	if err := core.WriteAllFilesAsXML(treeStr, dir, outFile, fileList, opt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("cannot read XML output: %v", err)
	}
	text := string(data)

	if !strings.HasPrefix(text, "<?xml") {
		t.Errorf("missing XML header")
	}
	if !strings.Contains(text, "<ProjectDump>") {
		t.Errorf("missing <ProjectDump>")
	}
	if !strings.Contains(text, treeStr) {
		t.Errorf("tree section missing or invalid")
	}
	if !strings.Contains(text, "package main") {
		t.Errorf("main.go content not present in XML output")
	}
	if !strings.Contains(text, "sub content") {
		t.Errorf("subdir/sub.txt content missing")
	}
	if !strings.Contains(text, `file name="main.go"`) {
		t.Errorf("main.go file tag missing")
	}
	if !strings.Contains(text, `file name="sub.txt"`) {
		t.Errorf("sub.txt file tag missing")
	}
}

func TestWriteAllFilesAsXML_CDATA_Escape(t *testing.T) {
	dir := t.TempDir()
	// ]]>
	testContent := "aaa ]]> bbb"
	file := filepath.Join(dir, "test.xml")
	if err := os.WriteFile(file, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}
	treeStr := "└── test.xml\n"
	outFile := filepath.Join(dir, "out.xml")

	fileList := map[string]bool{file: true}

	opt := &commandline.Option{
		OutputFormat:       model.OutputFormat("xml"),
		WithLineNumberFlag: model.OnOffSwitch("off"),
		MaskSecretsFlag:    model.OnOffSwitch("off"),
		ScanBuffer:         model.ByteString("1M"),
		WorkingDir:         dir,
		IgnoreDotFileFlag:  model.OnOffSwitch("off"),
		AllowGitignoreFlag: model.OnOffSwitch("off"),
	}

	if err := core.WriteAllFilesAsXML(treeStr, dir, outFile, fileList, opt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("cannot read output: %v", err)
	}
	text := string(data)

	if !strings.Contains(text, "aaa ]]]]><![CDATA[> bbb") {
		t.Errorf("CDATA escape not performed correctly: got\n%s", text)
	}
}
