package textenc

import (
	"bytes"
	"fmt"
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

// multilingual is text in many scripts: Japanese, Western European, Korean,
// Chinese, Arabic, Hebrew, Devanagari, Greek, Cyrillic, emoji and a
// combining mark.
const multilingual = "日本語の文章です。\n漢字、ひらがな、カタカナ。\nEnglish text.\n" +
	"Français : été, naïve.\nDeutsch: Grüße.\nEspañol: mañana, corazón.\nPortuguês: ação.\n" +
	"한국어 문장입니다.\n中文简体测试。\n中文繁體測試。\nالعربية: مرحبا بالعالم.\nעברית: שלום עולם.\n" +
	"हिन्दी: नमस्ते दुनिया।\nΕλληνικά: Καλημέρα.\nРусский: Привет.\nEmoji: 🦊 🌍 🚀\nCombining: é\n"

// legacy are texts with the encodings that represent them, and the encoding
// Decode must name.
var legacy = []struct {
	name, text string
	enc        encoding.Encoding
	want       string
}{
	{"Shift_JIS", "日本語の文章です。\n漢字、ひらがな、カタカナ。\nEnglish text.\n", japanese.ShiftJIS, "shift_jis"},
	{"CP932 extensions", "機種依存文字：①②③ ㈱ ～ ∥ ￢ 髙\nこれは設定ファイルです。\n", japanese.ShiftJIS, "shift_jis"},
	{"EUC-JP", "日本語の文章です。\n漢字、ひらがな、カタカナ。\nEnglish text.\n", japanese.EUCJP, "euc-jp"},
	{"ISO-2022-JP", "日本語の文章です。\nメールの本文です。\n", japanese.ISO2022JP, "iso-2022-jp"},
	{"GB18030", "中文简体测试。\n这是一个文件，用来测试编码。\nEnglish text.\n", simplifiedchinese.GB18030, "gb18030"},
	{"Big5", "中文繁體測試。\n這是一個檔案，用來測試編碼。\nEnglish text.\n", traditionalchinese.Big5, "big5"},
	{"EUC-KR", "한국어 문장입니다.\n이 파일은 인코딩을 시험합니다.\nEnglish text.\n", korean.EUCKR, "euc-kr"},
	{"Windows-1252", "Français : été, naïve.\nDeutsch: Grüße.\nEspañol: mañana, corazón.\nPortuguês: ação.\nEuro € “quotes” — dash\n", charmap.Windows1252, "windows-1252"},
	{"ISO-8859-1", "Français : été, naïve.\nDeutsch: Grüße.\nCopyright © 2026, ½\n", charmap.ISO8859_1, "windows-1252"},
}

func encode(t *testing.T, e encoding.Encoding, s string) []byte {
	t.Helper()
	b, err := e.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatalf("encoding %q: %v", s, err)
	}
	return b
}

// Each file's text is recovered exactly, whatever the encoding it is in.
func TestDecode_RecoversTheText(t *testing.T) {
	for _, c := range legacy {
		t.Run(c.name, func(t *testing.T) {
			r := Decode(encode(t, c.enc, c.text), "")
			if r.Kind != Text || r.Encoding != c.want || r.Text != c.text {
				t.Fatalf("got %v %q %q (%s), want %s", r.Kind, r.Encoding, r.Text, r.Reason, c.want)
			}
		})
	}
	for name, data := range map[string][]byte{
		"UTF-8":     []byte(multilingual),
		"UTF-8 BOM": append([]byte{0xEF, 0xBB, 0xBF}, multilingual...),
		"UTF-16LE":  encode(t, xunicode.UTF16(xunicode.LittleEndian, xunicode.UseBOM), multilingual),
		"UTF-16BE":  encode(t, xunicode.UTF16(xunicode.BigEndian, xunicode.UseBOM), multilingual),
	} {
		t.Run(name, func(t *testing.T) {
			r := Decode(data, "")
			if r.Kind != Text || r.Text != multilingual {
				t.Fatalf("got %v %q (%s): %q", r.Kind, r.Encoding, r.Reason, r.Text)
			}
		})
	}
}

// Valid UTF-8 is never decoded again — at any length, wherever a sample
// would cut a character (the cause of the dump's mojibake: a 1 KB sample
// cut a character, and the file was read as Shift_JIS).
func TestDecode_UTF8IsNeverDecodedAgain(t *testing.T) {
	line := "これは日本語の長い文書です。漢字とひらがなとカタカナを含みます。\n"
	for pad := 0; pad < 3; pad++ {
		s := strings.Repeat("x", pad) + "\n" + strings.Repeat(line, 200)
		if r := Decode([]byte(s), ""); r.Kind != Text || r.Encoding != "utf-8" || r.Text != s {
			t.Errorf("pad %d: got %v %q", pad, r.Kind, r.Encoding)
		}
	}
	for _, s := range []string{"ascii only\n", "一", "é", "🦊", "\x1b[31mred\x1b[0m log line\n"} {
		if r := Decode([]byte(s), ""); r.Kind != Text || r.Encoding != "utf-8" || r.Text != s {
			t.Errorf("%q: got %v %q", s, r.Kind, r.Encoding)
		}
	}
}

// Across languages, encodings and lengths, a reading is right or declined,
// never wrong — Decode does not name an encoding the text was not in — and
// text with enough to tell (a sentence, or code with one) is read.
func TestDecode_NeverMisreads(t *testing.T) {
	type sample struct {
		text     string
		mustRead bool // enough non-ASCII text to tell its encoding
	}
	texts := map[string][]sample{
		"ja": {{"吾輩は猫である。名前はまだ無い。どこで生れたかとんと見当がつかぬ。", true}, {"エラー：ファイルが見つかりません", true},
			{"// 初期化処理\n", false}, {"テスト", false}, {"日本語", false}},
		"ko":  {{"대한민국은 민주공화국이다. 모든 권력은 국민으로부터 나온다.", true}, {"// 사용자 정보를 가져온다\n", false}, {"안녕하세요", false}, {"한국어", false}},
		"zhs": {{"这个程序读取配置文件，并把结果写到标准输出。", true}, {"// 获取用户信息\n", false}, {"你好世界", false}, {"中文", false}},
		"zht": {{"這個程式讀取設定檔，並把結果寫到標準輸出。", true}, {"// 取得使用者資訊\n", false}, {"你好世界", false}, {"中文", false}},
		"west": {{"Les élèves étudient à l’école. Où est la bibliothèque ? Ça dépend.", true}, {"Die Straße ist schön. Grüße aus München.", true},
			{"¿Dónde está la estación?", false}, {"café", false}, {"naïve", false}, {"© 2026", false}},
	}
	encs := map[string][]encoding.Encoding{
		"ja": {japanese.ShiftJIS, japanese.EUCJP, japanese.ISO2022JP}, "ko": {korean.EUCKR},
		"zhs": {simplifiedchinese.GB18030}, "zht": {traditionalchinese.Big5},
		"west": {charmap.Windows1252, charmap.ISO8859_1},
	}
	for lang, ts := range texts {
		for _, e := range encs[lang] {
			for _, s := range ts {
				for _, form := range []string{s.text, "package main\n\nfunc main() {}\n" + s.text + "\n", strings.Repeat(s.text+"\n", 20)} {
					b, err := e.NewEncoder().Bytes([]byte(form))
					if err != nil {
						continue // not representable (’ in ISO-8859-1)
					}
					r := Decode(b, "")
					switch {
					case r.Kind == Text && r.Text == form:
					case r.Kind == Undetermined && !s.mustRead:
					case r.Kind == Undetermined:
						t.Errorf("%s %v %q: declined (%s)", lang, e, form, r.Reason)
					default:
						t.Errorf("%s %v %q: read as %q (%v): %q", lang, e, form, r.Encoding, r.Kind, r.Text)
					}
				}
			}
		}
	}
}

// Words of an application — messages, labels — in string literals of ASCII
// code, a few to a file: a short literal is declined rather than read as
// another encoding (EUC-JP 完了 read as GB18030 窗位, Shift_JIS 確認 as
// Windows-1252 Šm”F). Bytes that are also valid UTF-8 read as UTF-8, by the
// order of the rules; they are not counted here.
func TestDecode_WordsInCodeAreNotMisread(t *testing.T) {
	words := map[string][]string{
		"ja":   {"完了", "確認", "却下", "起動", "納品", "終了", "暗号", "登録", "削除", "承認", "請求書", "顧客情報", "在庫数", "出荷日", "取消", "保存", "検索結果", "エラー", "ログイン", "設定ファイル", "停止中", "処理済み", "未処理", "担当者"},
		"zhs":  {"编辑", "新增", "删除", "确认", "完成", "用户", "订单", "库存", "登录", "密码", "错误", "成功", "失败", "查询", "导出", "审批", "客户信息", "配置文件", "处理中"},
		"zht":  {"刪除", "新增", "編輯", "確認", "完成", "使用者", "訂單", "庫存", "登入", "密碼", "錯誤", "成功", "失敗", "查詢", "匯出", "審核", "客戶資料", "設定檔", "處理中"},
		"ko":   {"목록", "삭제", "수정", "확인", "완료", "사용자", "주문", "재고", "로그인", "비밀번호", "오류", "성공", "실패", "검색", "내보내기", "승인", "고객 정보", "설정 파일", "처리 중"},
		"west": {"résumé", "naïve", "Größe", "Straße", "año", "coração", "ação", "café", "élève", "Übersicht", "señor", "français"},
	}
	encs := map[string][]encoding.Encoding{
		"ja": {japanese.ShiftJIS, japanese.EUCJP}, "zhs": {simplifiedchinese.GB18030},
		"zht": {traditionalchinese.Big5}, "ko": {korean.EUCKR}, "west": {charmap.Windows1252},
	}
	read := 0
	for lang, ws := range words {
		for _, e := range encs[lang] {
			for k := 1; k <= 10; k++ {
				for off := range ws {
					var b strings.Builder
					b.WriteString("public class App {\n")
					for i := 0; i < k; i++ {
						fmt.Fprintf(&b, "    static final String M%d = \"%s\";\n", i, ws[(off+i)%len(ws)])
					}
					b.WriteString("}\n")
					src := b.String()
					data := encode(t, e, src)
					if utf8.Valid(data) {
						continue
					}
					switch r := Decode(data, ""); {
					case r.Kind == Text && r.Text == src:
						read++
					case r.Kind == Undetermined:
					default:
						t.Errorf("%s %v, %d literals: read as %s: %q", lang, e, k, r.Encoding, strings.Split(r.Text, "\n")[1])
					}
				}
			}
		}
	}
	if read == 0 {
		t.Fatal("nothing read")
	}
}

// Text of single-byte encodings other than Windows-1252 — Central European,
// Turkish, Baltic — is not read as Windows-1252: its letters are symbols or
// Icelandic letters there.
func TestDecode_OtherSingleByteEncodingsAreNotWestern(t *testing.T) {
	for _, c := range []struct {
		enc  encoding.Encoding
		text string
	}{
		{charmap.Windows1250, "Zażółć gęślą jaźń. Proszę zmienić ustawienia.\n"},
		{charmap.ISO8859_2, "Zażółć gęślą jaźń. Proszę zmienić ustawienia.\n"},
		{charmap.Windows1254, "Ayarları değiştirin. Şimdi başlayın, lütfen.\n"},
		{charmap.ISO8859_9, "Ayarları değiştirin. Şimdi başlayın, lütfen.\n"},
		{charmap.Windows1257, "Šis failas yra užkoduotas. Ačiū už pagalbą.\n"},
	} {
		if r := Decode(encode(t, c.enc, c.text), ""); r.Kind == Text {
			t.Errorf("%v %q read as %s: %q", c.enc, c.text, r.Encoding, r.Text)
		}
	}
}

// One-character literals are words too: a CJK character read as
// Windows-1252 is two accented letters standing alone (表 as ±í), never
// Western text.
func TestDecode_OneCharacterLiteralsAreNotWestern(t *testing.T) {
	for _, c := range []struct {
		enc   encoding.Encoding
		chars []string
	}{
		{japanese.EUCJP, []string{"表", "裏", "正", "副", "甲", "乙", "可", "否", "未"}},
		{japanese.ShiftJIS, []string{"可", "否", "未", "表", "裏"}},
		{simplifiedchinese.GB18030, []string{"上", "下", "左", "右", "是", "否"}},
		{korean.EUCKR, []string{"남", "북", "대", "소", "상", "하"}},
		{traditionalchinese.Big5, []string{"上", "下", "左", "右", "是", "否"}},
	} {
		for k := 1; k <= 12; k++ {
			var b strings.Builder
			for i := 0; i < k; i++ {
				fmt.Fprintf(&b, "    case %d: return \"%s\";\n", i, c.chars[i%len(c.chars)])
			}
			src := b.String()
			data := encode(t, c.enc, src)
			if utf8.Valid(data) {
				continue // read as UTF-8 by the order of the rules (EUC-JP 表 is UTF-8 ɽ)
			}
			r := Decode(data, "")
			if r.Kind == Text && r.Text != src {
				t.Errorf("%v, %d literals: read as %s: %q", c.enc, k, r.Encoding, r.Text)
			}
		}
	}
}

// A file larger than the scoring sample is decided by its non-ASCII lines,
// wherever they are, and decoded whole.
func TestDecode_LargeFileIsDecidedByItsTextAndDecodedWhole(t *testing.T) {
	ascii := strings.Repeat("INSERT INTO t VALUES (1, 'plain ascii row');\n", 2000) // ~90 KB
	ja := strings.Repeat("日本語の文章です。漢字、ひらがな、カタカナ。\n", 2000)                          // ~90 KB in Shift_JIS
	for name, s := range map[string]string{"ASCII first": ascii + ja, "text first": ja + ascii, "text between": ascii + ja + ascii} {
		r := Decode(encode(t, japanese.ShiftJIS, s), "")
		if r.Kind != Text || r.Encoding != "shift_jis" || r.Text != s {
			t.Errorf("%s: %v %q (%d of %d bytes) %s", name, r.Kind, r.Encoding, len(r.Text), len(s), r.Reason)
		}
	}
}

// UTF-16 after a byte order mark that is no text — private-use and
// unassigned characters — is binary.
func TestDecode_UTF16OfNoTextIsBinary(t *testing.T) {
	var b strings.Builder
	for r := rune(0xE000); r < 0xE100; r++ {
		b.WriteRune(r)
	}
	data := encode(t, xunicode.UTF16(xunicode.LittleEndian, xunicode.UseBOM), b.String())
	if r := Decode(data, ""); r.Kind != Binary {
		t.Errorf("private-use UTF-16: %v", r.Kind)
	}
}

// Turkish text with only ı and ş (Ý and Þ in Windows-1252, Icelandic
// letters) is not Western.
func TestDecode_TurkishLettersAreNotWestern(t *testing.T) {
	s := "Kısa şiir. Işık. Şimdi kış.\n"
	if r := Decode(encode(t, charmap.Windows1254, s), ""); r.Kind == Text {
		t.Errorf("read as %s: %q", r.Encoding, r.Text)
	}
}

// Western typography around words reads as Western text.
func TestDecode_WesternTypography(t *testing.T) {
	for _, s := range []string{
		"¿Dónde está la estación? ¡Atención! Hoy hace 25°C en la ciudad.\n",
		"3ª edição, nº 42, 1º andar. A informação está disponível.\n",
		"Die Fläche ist 12 m² groß, das Volumen 3 m³. Grüße.\n",
	} {
		if r := Decode(encode(t, charmap.Windows1252, s), ""); r.Kind != Text || r.Text != s {
			t.Errorf("%q: %v %q (%s)", s, r.Kind, r.Encoding, r.Reason)
		}
	}
}

// ISO-2022-JP of symbols, full-width letters or rare kanji is still
// ISO-2022-JP: its escapes and JIS pairs decide it.
func TestDecode_ISO2022JPOfAnyJISText(t *testing.T) {
	for _, s := range []string{"①②③　※注意　■□◆◇　〒100-0001\n", "ＡＢＣ１２３　ｘｙｚ\n", "鬱蒼薔薇檸檬憂鬱顰蹙\n"} {
		if r := Decode(encode(t, japanese.ISO2022JP, s), ""); r.Kind != Text || r.Encoding != "iso-2022-jp" || r.Text != s {
			t.Errorf("%q: %v %q %q", s, r.Kind, r.Encoding, r.Text)
		}
	}
}

// A JIS character of a byte out of 0x21–0x7E is no character: the bytes are
// not ISO-2022-JP (the decoder would make one of the second byte, a line
// feed among others), whether guessed or named. A run of JIS characters
// ending at a line feed still is.
func TestDecode_ISO2022JPMalformedPairsAreNotJIS(t *testing.T) {
	for _, s := range []string{"\x1b$B0\n", "\x1b$B0\nabc\n", "\x1b$B$3$s0 \x1b(B\n", "\x1b$B$3\x7f$s\x1b(B\n", "\x1b$B$3\t\x1b(B\n", "\x1b$B$3$\r\n\x1b(B\n"} {
		if r := Decode([]byte(s), ""); r.Kind == Text && r.Encoding != "utf-8" {
			t.Errorf("%q read as %s: %q", s, r.Encoding, r.Text)
		}
		if r := Decode([]byte(s), "iso-2022-jp"); r.Kind != Invalid {
			t.Errorf("%q named ISO-2022-JP: %v %q", s, r.Kind, r.Text)
		}
	}
	if r := Decode([]byte("\x1b$B$3$s\n$K$A$O\x1b(B\n"), "iso-2022-jp"); r.Kind != Text || r.Text != "こん\n$K$A$O\n" {
		t.Errorf("a run ending at a line feed: %v %q", r.Kind, r.Text)
	}
}

// A NUL makes a file binary whatever encoding a rule names for it (but
// UTF-16, which holds NUL bytes as text).
func TestDecode_ExplicitEncodingDoesNotMakeBinaryText(t *testing.T) {
	bin := []byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0xFF}
	if r := Decode(bin, "euc-jp"); r.Kind != Binary {
		t.Errorf("binary named EUC-JP: %v", r.Kind)
	}
	if r := Decode(encode(t, xunicode.UTF16(xunicode.LittleEndian, xunicode.IgnoreBOM), "ab"), "utf-16le"); r.Kind != Text || r.Text != "ab" {
		t.Errorf("UTF-16LE named: %v %q", r.Kind, r.Text)
	}
}

// The documented limits, pinned: UTF-16 without a byte order mark is binary
// (NUL bytes); high bytes no encoding reads as plausible text are declined,
// never guessed; and a few legacy bytes that also are valid UTF-8 read as
// UTF-8 (Shift_JIS 閾値 is UTF-8 臒l) — the rules put valid UTF-8 first, and
// --file-encoding decides such a file.
func TestDecode_DocumentedLimits(t *testing.T) {
	noBOM := encode(t, xunicode.UTF16(xunicode.LittleEndian, xunicode.IgnoreBOM), "plain text without a byte order mark\n")
	if r := Decode(noBOM, ""); r.Kind != Binary {
		t.Errorf("UTF-16 without a byte order mark: %v", r.Kind)
	}
	noise := bytes.Repeat([]byte{0x9F, 0xE0, 0x81, 0xFD, 0xC1, 0x8E, 'x', ' '}, 40)
	if r := Decode(noise, ""); r.Kind == Text {
		t.Errorf("noise read as %s: %q", r.Encoding, r.Text)
	}
	sjis := encode(t, japanese.ShiftJIS, "閾値")
	if r := Decode(sjis, ""); r.Kind != Text || r.Encoding != "utf-8" || r.Text != string(sjis) {
		t.Errorf("Shift_JIS 閾値: %v %q %q, want it read as UTF-8 (the documented limit)", r.Kind, r.Encoding, r.Text)
	}
	if r := Decode(sjis, "shift_jis"); r.Kind != Text || r.Text != "閾値" {
		t.Errorf("Shift_JIS 閾値 named: %v %q", r.Kind, r.Text)
	}
}

// Bytes that are no text are binary, as the dump has always treated them.
func TestDecode_Binary(t *testing.T) {
	for name, b := range map[string][]byte{
		"NUL":                               {'a', 0x00, 'b'},
		"PNG":                               {0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00},
		"controls":                          bytes.Repeat([]byte{0x01, 0x02, 0x03, 'a'}, 10),
		"high and controls":                 bytes.Repeat([]byte{0xC3, 0x01, 0x8F, 0x02, 0x9D}, 20),
		"NUL in long text":                  append([]byte(strings.Repeat("text line\n", 100)), 0x00),
		"NUL after a UTF-8 byte order mark": append([]byte{0xEF, 0xBB, 0xBF}, "text\x00more text here\n"...),
		"binary after FF FE":                append([]byte{0xFF, 0xFE}, bytes.Repeat([]byte{0x00, 0xD8, 0x13, 0xE0, 0x01, 0x00, 0x7F, 0x00}, 16)...),
	} {
		if r := Decode(b, ""); r.Kind != Binary {
			t.Errorf("%s: %v %q, want binary", name, r.Kind, r.Encoding)
		}
	}
}

// A file whose encoding cannot be told is declined, never read with
// characters replaced; an explicit encoding decides it, and bytes not in the
// named encoding are refused.
func TestDecode_UndeterminedAndExplicit(t *testing.T) {
	big5 := encode(t, traditionalchinese.Big5, "中文") // also kana in EUC-JP
	if r := Decode(big5, ""); r.Kind != Undetermined || r.Text != "" {
		t.Fatalf("ambiguous bytes: %v %q %q", r.Kind, r.Encoding, r.Text)
	}
	if r := Decode(big5, "big5"); r.Kind != Text || r.Text != "中文" {
		t.Fatalf("explicit big5: %v %q", r.Kind, r.Text)
	}
	sjis := encode(t, japanese.ShiftJIS, "日本語")
	if r := Decode(sjis, "euc-kr"); r.Kind != Invalid || r.Text != "" {
		t.Fatalf("Shift_JIS bytes named EUC-KR: %v %q", r.Kind, r.Text)
	}
	if r := Decode([]byte{0xEF, 0xBB, 0xBF, 0xFF}, ""); r.Kind != Undetermined {
		t.Errorf("BOM then invalid UTF-8: %v", r.Kind)
	}
	if r := Decode([]byte{0xFF, 0xFE, 'a'}, ""); r.Kind != Undetermined {
		t.Errorf("UTF-16 BOM, odd length: %v", r.Kind)
	}
	if r := Decode([]byte("plain\n"), "shift_jis"); r.Kind != Text || r.Text != "plain\n" {
		t.Errorf("ASCII named Shift_JIS: %v %q", r.Kind, r.Text)
	}
	if r := Decode(append([]byte{0xEF, 0xBB, 0xBF}, "é\n"...), "utf-8"); r.Kind != Text || r.Text != "é\n" {
		t.Errorf("UTF-8 with a byte order mark named UTF-8: %v %q", r.Kind, r.Text)
	}
	// Too little text to tell: declined, not read as GB18030 (窗位).
	if r := Decode(encode(t, japanese.EUCJP, "x = \"完了\"\n"), ""); r.Kind != Undetermined {
		t.Errorf("EUC-JP 完了: %v %q %q", r.Kind, r.Encoding, r.Text)
	}
	// A stray escape to JIS with nothing after it is kept, not removed.
	if s := "line one \x1b$B\nline two of a plain file\n"; Decode([]byte(s), "").Text != s {
		t.Errorf("a stray escape was removed: %q", Decode([]byte(s), "").Text)
	}
	// An escape in a terminal log is not ISO-2022-JP.
	if log := "2026-10-20 12:00:00 INFO \x1b$Bstarting the server on port 8080 with 4 workers\x1b(B\n"; Decode([]byte(log), "").Text != log {
		t.Errorf("a log line with an escape was decoded: %q", Decode([]byte(log), "").Text)
	}
}

func TestLookup(t *testing.T) {
	for in, want := range map[string]string{
		"UTF-8": "utf-8", "sjis": "shift_jis", "CP932": "shift_jis", "Windows-31J": "shift_jis",
		"eucJP": "euc-jp", "ISO_2022_JP": "iso-2022-jp", "GBK": "gb18030", "big5": "big5",
		"EUC-KR": "euc-kr", "cp1252": "windows-1252", "latin1": "iso-8859-1", "UTF-16LE": "utf-16le",
	} {
		if got, ok := Lookup(in); !ok || got != want {
			t.Errorf("Lookup(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "ebcdic", "utf-32"} {
		if _, ok := Lookup(bad); ok {
			t.Errorf("Lookup(%q) accepted", bad)
		}
	}
	for _, n := range Names() {
		if c, ok := Lookup(n); !ok || c != n {
			t.Errorf("canonical name %q does not look up to itself (%q)", n, c)
		}
	}
}

// Decode never panics; text is valid UTF-8 with nothing replaced that the
// input did not have, and the same bytes always give the same result.
func FuzzDecode(f *testing.F) {
	f.Add([]byte(multilingual))
	for _, c := range legacy {
		b, _ := c.enc.NewEncoder().Bytes([]byte(c.text))
		f.Add(b)
	}
	f.Add([]byte{0xFF, 0xFE, 0x41, 0x00})
	f.Add([]byte{0x1B, '$', 'B', 0x46, 0x7C, 0x1B, '(', 'B'})
	f.Fuzz(func(t *testing.T, b []byte) {
		r := Decode(b, "")
		if r.Kind == Text {
			if !utf8.ValidString(r.Text) {
				t.Fatalf("invalid UTF-8 text from %q", b)
			}
			if strings.ContainsRune(r.Text, utf8.RuneError) && !(utf8.Valid(b) && bytes.ContainsRune(b, utf8.RuneError)) {
				t.Fatalf("U+FFFD in the text of %q (%s)", b, r.Encoding)
			}
		} else if r.Text != "" {
			t.Fatalf("text with kind %v", r.Kind)
		}
		if again := Decode(b, ""); again != r {
			t.Fatalf("not deterministic: %+v then %+v", r, again)
		}
		// A legacy reading is a decoding: text that encodes back (the
		// decoders read more than the encoders write — Big5's HKSCS, JIS
		// X 0212) reads as the same text in the same encoding.
		if r.Kind == Text && r.Encoding != "utf-8" && !strings.HasPrefix(r.Encoding, "utf-16") {
			if back, err := encodings[r.Encoding].NewEncoder().Bytes([]byte(r.Text)); err == nil {
				if again := Decode(back, r.Encoding); again.Kind != Text || again.Text != r.Text {
					t.Fatalf("%s text does not read back: %+v", r.Encoding, again)
				}
			}
		}
		// An explicit encoding gives its text or refuses; nothing replaced.
		for _, name := range Names() {
			e := Decode(b, name)
			if e.Kind == Text && (!utf8.ValidString(e.Text) || (strings.ContainsRune(e.Text, utf8.RuneError) && !bytes.ContainsRune(b, utf8.RuneError))) {
				t.Fatalf("explicit %s: bad text %q from %q", name, e.Text, b)
			}
		}
	})
}
