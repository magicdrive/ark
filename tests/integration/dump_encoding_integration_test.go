package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	xunicode "golang.org/x/text/encoding/unicode"
)

const allScripts = "日本語の文章です。\n漢字、ひらがな、カタカナ。\nEnglish text.\n" +
	"Français : été, naïve.\nDeutsch: Grüße.\nEspañol: mañana, corazón.\nPortuguês: ação.\n" +
	"한국어 문장입니다.\n中文简体测试。\n中文繁體測試。\nالعربية: مرحبا بالعالم.\nעברית: שלום עולם.\n" +
	"हिन्दी: नमस्ते दुनिया।\nΕλληνικά: Καλημέρα.\nРусский: Привет.\nEmoji: 🦊 🌍 🚀\nCombining: e\u0301\n"

// mixedFile is a file of the mixed-encoding repository: its text and the
// encoding it is saved in (nil: UTF-8).
type mixedFile struct {
	text string
	enc  encoding.Encoding
	bom  []byte
}

var mixedRepository = map[string]mixedFile{
	"README.md":                  {"# 混在リポジトリ\n\n" + allScripts, nil, nil},
	"docs/unicode-utf8-bom.txt":  {allScripts, nil, []byte{0xEF, 0xBB, 0xBF}},
	"docs/unicode-utf16le.txt":   {allScripts, xunicode.UTF16(xunicode.LittleEndian, xunicode.UseBOM), nil},
	"docs/unicode-utf16be.txt":   {allScripts, xunicode.UTF16(xunicode.BigEndian, xunicode.UseBOM), nil},
	"legacy/config.txt":          {"設定ファイル：値＝１\n日本語の文章です。\n漢字、ひらがな、カタカナ。\n", japanese.ShiftJIS, nil},
	"legacy/cp932.txt":           {"機種依存文字：①②③ ㈱ ～ ∥ ￢\nこれは CP932 のファイルです。\n", japanese.ShiftJIS, nil},
	"legacy/manual.txt":          {"マニュアル：第一章\n日本語の文章です。\nEUC-JP で保存しています。\n", japanese.EUCJP, nil},
	"legacy/mail.txt":            {"メールの本文です。\n日本語の文章です。\n", japanese.ISO2022JP, nil},
	"docs/chinese.txt":           {"中文简体测试。\n这是一个文件，用来测试编码。\n", simplifiedchinese.GB18030, nil},
	"docs/chinese-big5.txt":      {"中文繁體測試。\n這是一個檔案，用來測試編碼。\n", traditionalchinese.Big5, nil},
	"docs/korean.txt":            {"한국어 문장입니다.\n이 파일은 인코딩을 시험합니다.\n", korean.EUCKR, nil},
	"docs/european.txt":          {"Français : été, naïve.\nDeutsch: Grüße.\nEspañol: mañana, corazón.\nEuro € “quotes”\n", charmap.Windows1252, nil},
	"docs/latin1.txt":            {"Português: ação.\nCopyright © 2026, ½\n", charmap.ISO8859_1, nil},
	"src/main.go":                {"package main\n\n// 日本語のコメント 🦊\nfunc main() { println(\"こんにちは\") }\n", nil, nil},
	"ascii.txt":                  {"plain ASCII only\n", nil, nil},
	"docs/long-utf8-japanese.md": {strings.Repeat("これは日本語の長い文書です。漢字とひらがなとカタカナを含みます。\n", 200), nil, nil},
}

func writeMixedRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel string, b []byte) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for rel, f := range mixedRepository {
		b := []byte(f.text)
		if f.enc != nil {
			var err error
			if b, err = f.enc.NewEncoder().Bytes(b); err != nil {
				t.Fatalf("%s: %v", rel, err)
			}
		}
		write(rel, append(append([]byte{}, f.bom...), b...))
	}
	// Binary, left out as always.
	write("bin/blob.bin", []byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0xFF, 0xFE, 0x00})
	return root
}

func mixedPaths() []string {
	var ps []string
	for p := range mixedRepository {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return ps
}

func dump(t *testing.T, bin, root, format string, extra ...string) (string, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "dump."+format)
	cmd := exec.Command(bin, append(append([]string{"--silent", "--output-format", format, "--output-filename", out}, extra...), ".")...)
	cmd.Dir = root
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if b, err := cmd.Output(); err != nil {
		t.Fatalf("ark %s: %v\n%s\n%s", format, err, b, stderr.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(b) {
		t.Fatalf("%s dump is not valid UTF-8", format)
	}
	return string(b), stderr.String()
}

// A repository whose files are in different encodings dumps, in every
// format, as UTF-8 in which every file's text is the original text — each
// file decoded on its own, none decoded twice, the binary left out.
func TestDump_MixedEncodingRepository(t *testing.T) {
	bin := buildArk(t)
	root := writeMixedRepository(t)

	txt, warnings := dump(t, bin, root, "txt")
	if warnings != "" {
		t.Errorf("warnings: %s", warnings)
	}
	parts := regexp.MustCompile(`\n=== (.+?) ===\n`).Split(txt, -1)
	names := regexp.MustCompile(`\n=== (.+?) ===\n`).FindAllStringSubmatch(txt, -1)
	var order []string
	for i, m := range names {
		order = append(order, m[1])
		want, ok := mixedRepository[m[1]]
		if !ok {
			t.Errorf("txt: unexpected file %s", m[1])
			continue
		}
		if parts[i+1] != want.text {
			t.Errorf("txt: %s is not its original text:\n got %q\nwant %q", m[1], parts[i+1], want.text)
		}
	}
	if want := mixedPaths(); strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("txt: files %v, want %v (every file, in order, once)", order, want)
	}

	md, _ := dump(t, bin, root, "md")
	xml, _ := dump(t, bin, root, "xml")
	arklite, _ := dump(t, bin, root, "arklite")
	for _, p := range mixedPaths() {
		text := mixedRepository[p].text
		if n := strings.Count(md, "# File: "+p+"\n"); n != 1 {
			t.Errorf("md: %s appears %d times", p, n)
		}
		if !strings.Contains(md, text) {
			t.Errorf("md: the text of %s is not there as it is", p)
		}
		if !strings.Contains(xml, "<![CDATA[\n"+text) {
			t.Errorf("xml: the text of %s is not there as it is", p)
		}
		for _, line := range strings.Split(text, "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "//") && !strings.Contains(arklite, line) {
				t.Errorf("arklite: line %q of %s is not there", line, p)
			}
		}
	}
	for _, f := range []string{txt, md, xml, arklite} {
		if strings.ContainsRune(f, utf8.RuneError) || strings.ContainsRune(f, '\uFEFF') || strings.Contains(f, "縺") {
			t.Error("a dump has a replacement character, a byte order mark or mojibake")
		}
	}
}

// The MCP server reads the same files as the same text: get_file_content
// returns each file's original text, get_files_arklite all of them.
func TestDump_MixedEncodingRepositoryThroughMCP(t *testing.T) {
	bin := buildArk(t)
	root := writeMixedRepository(t)
	c := startServer(t, root, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	var paths []any
	for _, p := range mixedPaths() {
		paths = append(paths, p)
		text, isErr := c.tool("get_file_content", map[string]any{"path": p})
		if isErr || text != mixedRepository[p].text {
			t.Errorf("get_file_content %s:\n got %q (error %v)\nwant %q", p, text, isErr, mixedRepository[p].text)
		}
	}
	all, isErr := c.tool("get_files_arklite", map[string]any{"paths": paths, "maxFiles": len(paths)})
	if isErr {
		t.Fatalf("get_files_arklite: %s", all)
	}
	for _, p := range mixedPaths() {
		for _, line := range strings.Split(mixedRepository[p].text, "\n") {
			if line != "" && !strings.Contains(all, line) {
				t.Errorf("get_files_arklite: line %q of %s is not there", line, p)
			}
		}
	}
}

// A file whose encoding cannot be told is left out with a warning naming it
// — never dumped with characters replaced — and --file-encoding decides it.
func TestDump_UndeterminedEncodingIsAnnouncedAndCanBeNamed(t *testing.T) {
	bin := buildArk(t)
	root := t.TempDir()
	ambiguous, err := traditionalchinese.Big5.NewEncoder().Bytes([]byte("中文\n")) // EUC-JP reads it as kana
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "short.txt"), ambiguous, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("日本語\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	txt, warnings := dump(t, bin, root, "txt")
	if strings.Contains(txt, "=== short.txt ===") || !strings.Contains(txt, "=== ok.txt ===\n日本語\n") {
		t.Errorf("dump:\n%s", txt)
	}
	if !strings.Contains(warnings, "short.txt") || !strings.Contains(warnings, "--file-encoding") {
		t.Errorf("no warning for short.txt: %q", warnings)
	}
	txt, warnings = dump(t, bin, root, "txt", "--file-encoding", "short.txt=big5")
	if !strings.Contains(txt, "=== short.txt ===\n中文\n") || warnings != "" {
		t.Errorf("with --file-encoding: %q / %q", txt, warnings)
	}
	// Bytes not in the named encoding are refused, not replaced.
	_, warnings = dump(t, bin, root, "txt", "--file-encoding", "ok.txt=shift_jis")
	if !strings.Contains(warnings, "ok.txt") {
		t.Errorf("UTF-8 named Shift_JIS dumped without a warning: %q", warnings)
	}
	// A pattern may start with ./; one that matches no file is announced.
	txt, warnings = dump(t, bin, root, "txt", "--file-encoding", "./short.txt=big5,nothing/**=sjis")
	if !strings.Contains(txt, "=== short.txt ===\n中文\n") || !strings.Contains(warnings, "nothing/**=shift_jis matches no file") {
		t.Errorf("./ pattern and an unused rule: %q / %q", txt, warnings)
	}

	// The MCP server applies the same rules, relative to its root, and
	// refuses a file not in the encoding named for it.
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "short.txt"), ambiguous, 0o644); err != nil {
		t.Fatal(err)
	}
	// Only a path pattern names sub/short.txt: it is matched relative to the
	// server root.
	c := startServer(t, root, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache", "--file-encoding", "ok.txt=shift_jis,sub/**=big5")
	c.handshake()
	if text, isErr := c.tool("get_file_content", map[string]any{"path": "sub/short.txt"}); isErr || text != "中文\n" {
		t.Errorf("MCP sub/short.txt named Big5: %q (error %v)", text, isErr)
	}
	// Unlike the dump, the MCP server keeps its fallback for a file whose
	// encoding cannot be told: its bytes, no error (the JSON response
	// replaces what is not UTF-8 with U+FFFD).
	if text, isErr := c.tool("get_file_content", map[string]any{"path": "short.txt"}); isErr || !strings.ContainsRune(text, utf8.RuneError) {
		t.Errorf("MCP undetermined short.txt: %q (error %v), want its bytes as before", text, isErr)
	}
	if text, isErr := c.tool("get_file_content", map[string]any{"path": "ok.txt"}); !isErr || strings.ContainsRune(text, utf8.RuneError) {
		t.Errorf("MCP UTF-8 named Shift_JIS: %q (error %v), want an error", text, isErr)
	}
}

// --skip-non-utf8 still leaves out, silently, every file that is not UTF-8.
func TestDump_SkipNonUTF8LeavesOutLegacyEncodings(t *testing.T) {
	bin := buildArk(t)
	root := writeMixedRepository(t)
	txt, warnings := dump(t, bin, root, "txt", "--skip-non-utf8")
	if warnings != "" {
		t.Errorf("warnings: %s", warnings)
	}
	for p, f := range mixedRepository {
		utf8File := f.enc == nil
		if got := strings.Contains(txt, "\n=== "+p+" ===\n"); got != utf8File {
			t.Errorf("--skip-non-utf8: %s in the dump: %v, want %v", p, got, utf8File)
		}
	}
}
