package integration

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/traditionalchinese"
	xunicode "golang.org/x/text/encoding/unicode"
)

// searchHit is one line of search_in_files output: path:line:text.
type searchHit struct {
	path string
	line int
	text string
}

func searchHits(t *testing.T, out string) []searchHit {
	t.Helper()
	if !utf8.ValidString(out) {
		t.Fatalf("search output is not valid UTF-8: %q", out)
	}
	if out == "" {
		return nil
	}
	var hits []searchHit
	for _, l := range strings.Split(out, "\n") {
		p, rest, ok1 := strings.Cut(l, ":")
		n, text, ok2 := strings.Cut(rest, ":")
		line, err := strconv.Atoi(n)
		if !ok1 || !ok2 || err != nil {
			t.Fatalf("malformed search line %q", l)
		}
		hits = append(hits, searchHit{p, line, text})
	}
	return hits
}

// checkHitsAreLines fails for a hit that is not the line of its file's text
// at its number: a search never reports what a file does not say.
func checkHitsAreLines(t *testing.T, hits []searchHit, texts map[string]string) {
	t.Helper()
	for _, h := range hits {
		text, ok := texts[h.path]
		if !ok {
			t.Errorf("hit in %s, which has no text to search: %+v", h.path, h)
			continue
		}
		lines := strings.Split(text, "\n")
		if h.line < 1 || h.line > len(lines) || lines[h.line-1] != h.text {
			t.Errorf("hit %s:%d:%q is not line %d of the file", h.path, h.line, h.text, h.line)
		}
	}
}

func hasHit(hits []searchHit, path string, line int, text string) bool {
	for _, h := range hits {
		if h == (searchHit{path, line, text}) {
			return true
		}
	}
	return false
}

func mixedTexts() map[string]string {
	texts := map[string]string{}
	for p, f := range mixedRepository {
		texts[p] = f.text
	}
	return texts
}

// search_in_files searches every file of a mixed-encoding repository in its
// text, as get_file_content returns it: every line of every file is found
// under its own number, in a script of any language, and nothing is reported
// that a file does not say — the binary is never searched.
func TestSearch_MixedEncodingRepositoryThroughMCP(t *testing.T) {
	bin := buildArk(t)
	root := writeMixedRepository(t)
	c := startServer(t, root, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	texts := mixedTexts()
	search := func(args map[string]any) []searchHit {
		t.Helper()
		if _, ok := args["path"]; !ok {
			args["path"] = "."
		}
		if _, ok := args["maxResults"]; !ok {
			args["maxResults"] = 10000
		}
		out, isErr := c.tool("search_in_files", args)
		if isErr {
			t.Fatalf("search_in_files %v: %s", args, out)
		}
		hits := searchHits(t, out)
		checkHitsAreLines(t, hits, texts)
		return hits
	}

	for _, p := range mixedPaths() {
		for i, line := range strings.Split(texts[p], "\n") {
			if line == "" {
				continue
			}
			if hits := search(map[string]any{"query": line}); !hasHit(hits, p, i+1, line) {
				t.Errorf("%s line %d %q not found", p, i+1, line)
			}
		}
	}

	// Words of each script, each in the files that hold it.
	for _, tc := range []struct {
		query string
		want  []string // path:line
	}{
		{"設定ファイル", []string{"legacy/config.txt:1"}},          // Shift_JIS
		{"①②③", []string{"legacy/cp932.txt:1"}},              // CP932
		{"EUC-JP で保存", []string{"legacy/manual.txt:3"}},      // EUC-JP
		{"メールの本文", []string{"legacy/mail.txt:1"}},            // ISO-2022-JP
		{"用来测试编码", []string{"docs/chinese.txt:2"}},           // GB18030
		{"用來測試編碼", []string{"docs/chinese-big5.txt:2"}},      // Big5
		{"인코딩을 시험", []string{"docs/korean.txt:2"}},           // EUC-KR
		{"Euro € “quotes”", []string{"docs/european.txt:4"}}, // Windows-1252
		{"Copyright ©", []string{"docs/latin1.txt:2"}},       // ISO-8859-1
		{"Привет", []string{"README.md:17", "docs/unicode-utf16be.txt:15", "docs/unicode-utf16le.txt:15", "docs/unicode-utf8-bom.txt:15"}},
		{"plain ASCII", []string{"ascii.txt:1"}},
	} {
		hits := search(map[string]any{"query": tc.query})
		var got []string
		for _, h := range hits {
			got = append(got, h.path+":"+strconv.Itoa(h.line))
		}
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%q: got %v, want %v", tc.query, got, tc.want)
		}
	}

	// The byte order mark is not text: line 1 starts with its first word.
	hits := search(map[string]any{"query": "^日本語の文章", "isRegex": true})
	for _, p := range []string{"docs/unicode-utf8-bom.txt", "docs/unicode-utf16le.txt", "docs/unicode-utf16be.txt"} {
		if !hasHit(hits, p, 1, "日本語の文章です。") {
			t.Errorf("^ does not match line 1 of %s: %v", p, hits)
		}
	}
	// A regex spans a multibyte character as one: . is one character.
	if hits := search(map[string]any{"query": "^Emoji: .{1} .{1} .{1}$", "isRegex": true}); len(hits) != 4 {
		t.Errorf("one character per emoji: %v", hits)
	}
	// The binary's bytes are never searched.
	if hits := search(map[string]any{"query": "PNG"}); len(hits) != 0 {
		t.Errorf("binary searched: %v", hits)
	}
	// maxResults counts the lines found, whatever the encodings.
	if hits := search(map[string]any{"query": "日本語の文章です。", "maxResults": 3}); len(hits) != 3 {
		t.Errorf("maxResults 3: %d hits", len(hits))
	}
	// The same search gives the same answer.
	a, _ := c.tool("search_in_files", map[string]any{"path": ".", "query": "文", "maxResults": 10000})
	b, _ := c.tool("search_in_files", map[string]any{"path": ".", "query": "文", "maxResults": 10000})
	if a != b || a == "" {
		t.Error("search is not deterministic")
	}
	// Filters apply as before.
	if hits := search(map[string]any{"query": "日本語の文章です。", "excludeDir": "legacy,docs"}); len(hits) != 1 || hits[0].path != "README.md" {
		t.Errorf("excludeDir: %v", hits)
	}
}

// Where a line is and what it says, in every encoding: a line number counts
// the file's line feeds, a line is reported whole (with the carriage return
// of a CRLF line, as for UTF-8), one hit per line however often it matches,
// a match at either end of a line is found, and a query never spans lines.
func TestSearch_LinePositionsInEveryEncoding(t *testing.T) {
	bin := buildArk(t)
	root := t.TempDir()
	const text = "先頭の行 start\r\n2行目は🦊と🌍を含む\r\n\r\n行末に来る語 end\r\n反復 反復 反復\r\nabc\r\n"
	files := map[string]encoding.Encoding{
		"utf8.txt":    nil,
		"sjis.txt":    japanese.ShiftJIS,
		"eucjp.txt":   japanese.EUCJP,
		"utf16le.txt": xunicode.UTF16(xunicode.LittleEndian, xunicode.UseBOM),
		"utf16be.txt": xunicode.UTF16(xunicode.BigEndian, xunicode.UseBOM),
	}
	texts := map[string]string{}
	for name, enc := range files {
		body := text
		if enc == japanese.ShiftJIS || enc == japanese.EUCJP {
			body = strings.ReplaceAll(strings.ReplaceAll(text, "🦊", "狐"), "🌍", "地球") // no emoji in JIS
		}
		b := []byte(body)
		if enc != nil {
			var err error
			if b, err = enc.NewEncoder().Bytes(b); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
		texts[name] = body
	}
	c := startServer(t, root, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	search := func(query string, isRegex bool) []searchHit {
		t.Helper()
		out, isErr := c.tool("search_in_files", map[string]any{"path": ".", "query": query, "isRegex": isRegex})
		if isErr {
			t.Fatalf("search_in_files %q: %s", query, out)
		}
		hits := searchHits(t, out)
		checkHitsAreLines(t, hits, texts)
		return hits
	}
	for name := range files {
		for _, tc := range []struct {
			query   string
			isRegex bool
			line    int
		}{
			{"先頭", false, 1},
			{"^先頭の行", true, 1},
			{"end\r", false, 4},
			{"end\\r$", true, 4},
			{"行目は", false, 2},
			{"反復", false, 5},
			{"^abc\\r$", true, 6},
		} {
			hits := search(tc.query, tc.isRegex)
			n := 0
			for _, h := range hits {
				if h.path == name {
					n++
					if h.line != tc.line {
						t.Errorf("%s %q: line %d, want %d", name, tc.query, h.line, tc.line)
					}
				}
			}
			if n != 1 {
				t.Errorf("%s %q: %d hits, want 1", name, tc.query, n)
			}
		}
		if !hasHit(search("^$|^\\r$", true), name, 3, "\r") {
			t.Errorf("%s: the empty line 3 is not found", name)
		}
	}
	if hits := search("🦊と🌍", false); len(hits) != 3 || !hasHit(hits, "utf16le.txt", 2, "2行目は🦊と🌍を含む\r") {
		t.Errorf("emoji: %v", hits)
	}
	// A query never spans lines, in any encoding (the search is by line).
	if hits := search("start\r\n2行目", false); len(hits) != 0 {
		t.Errorf("a query spanning lines matched: %v", hits)
	}
	if hits := search("start(.|\\s)*2行目", true); len(hits) != 0 {
		t.Errorf("a regex spanning lines matched: %v", hits)
	}
}

// A file whose encoding cannot be told is not searched — its bytes taken as
// text would match what it does not say — and --file-encoding decides it. A
// file not in the encoding named for it is skipped, the search goes on;
// --skip-non-utf8 and the ignore rules leave files out as before.
func TestSearch_UndeterminedSkippedAndNamedEncodings(t *testing.T) {
	bin := buildArk(t)
	root := t.TempDir()
	ambiguous, err := traditionalchinese.Big5.NewEncoder().Bytes([]byte("中文\n")) // EUC-JP reads it as kana
	if err != nil {
		t.Fatal(err)
	}
	sjis, err := japanese.ShiftJIS.NewEncoder().Bytes([]byte("設定ファイル：値＝１\n日本語の文章です。漢字、ひらがな、カタカナ。\n"))
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{
		"short.txt":        ambiguous,
		"sub/short.txt":    ambiguous,
		"ok.txt":           []byte("日本語 中文\n"),
		"ignored/sjis.txt": sjis,
		"kept/sjis.txt":    sjis,
		".gitignore":       []byte("ignored/\n"),
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(c *rpcClient, args map[string]any) string {
		t.Helper()
		args["path"] = "."
		out, isErr := c.tool("search_in_files", args)
		if isErr || !utf8.ValidString(out) || strings.ContainsRune(out, utf8.RuneError) {
			t.Fatalf("search_in_files %v: %q (error %v)", args, out, isErr)
		}
		return out
	}

	c := startServer(t, root, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	if out := run(c, map[string]any{"query": "中文"}); out != "ok.txt:1:日本語 中文" {
		t.Errorf("undetermined files searched: %q", out)
	}
	if out := run(c, map[string]any{"query": "設定"}); out != "kept/sjis.txt:1:設定ファイル：値＝１" {
		t.Errorf("gitignored or Shift_JIS: %q", out)
	}
	if out := run(c, map[string]any{"query": "設定", "allowGitignore": false}); out != "ignored/sjis.txt:1:設定ファイル：値＝１\nkept/sjis.txt:1:設定ファイル：値＝１" {
		t.Errorf("allowGitignore false: %q", out)
	}
	if out := run(c, map[string]any{"query": "設定", "skipNonUTF8": true}); out != "" {
		t.Errorf("skipNonUTF8 searched Shift_JIS: %q", out)
	}

	// The server's rules, relative to its root: sub/** is Big5, so is
	// short.txt; ok.txt is named Shift_JIS, which it is not — it is skipped,
	// and the search goes on.
	c = startServer(t, root, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache", "--file-encoding", "ok.txt=shift_jis,sub/**=big5,short.txt=big5")
	c.handshake()
	if out := run(c, map[string]any{"query": "中文"}); out != "short.txt:1:中文\nsub/short.txt:1:中文" {
		t.Errorf("named encodings: %q", out)
	}

	// --skip-non-utf8 on the server leaves out every file that is not UTF-8.
	c = startServer(t, root, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache", "--skip-non-utf8")
	c.handshake()
	if out := run(c, map[string]any{"query": "設定"}); out != "" {
		t.Errorf("--skip-non-utf8 searched Shift_JIS: %q", out)
	}
	if out := run(c, map[string]any{"query": "中文"}); out != "ok.txt:1:日本語 中文" {
		t.Errorf("--skip-non-utf8 UTF-8: %q", out)
	}
}
