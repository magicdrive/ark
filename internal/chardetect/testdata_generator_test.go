package chardetect

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// Test content in various complexities
const (
	// Basic Japanese text
	simpleJapanese = "こんにちは、世界！"

	// Classic literature (Natsume Soseki)
	literaryJapanese = "吾輩は猫である。名前はまだ無い。どこで生れたかとんと見当がつかぬ。"

	// Mixed content
	mixedContent = `# タイトル
これはテストファイルです。
This is a test file.
日本語と英語が混在しています。
Numbers: 123, 456, 789
記号: ！@#$%^&*()
漢字、ひらがな、カタカナ、全て含む。`

	// Kanji-heavy content
	kanjiHeavy = "日本国憲法前文：日本国民は、正当に選挙された国会における代表者を通じて行動し、われらとわれらの子孫のために、諸国民との協和による成果と、わが国全土にわたつて自由のもたらす恵沢を確保し、政府の行為によつて再び戦争の惨禍が起ることのないやうにすることを決意し、ここに主権が国民に存することを宣言し、この憲法を確定する。"

	// Hiragana only
	hiraganaOnly = "これはひらがなのみのぶんしょうです。すべてひらがなでかかれています。"

	// Katakana only
	katakanaOnly = "コレハカタカナノミノブンショウデス。スベテカタカナデカカレテイマス。"

	// ASCII only
	asciiOnly = "This is a pure ASCII text file.\nIt contains no Japanese characters.\nOnly English letters, numbers, and symbols."

	// Long text (repeated for large file)
	longTextUnit = "日本語のテキストが続きます。This is followed by English text. 繰り返しパターン。Repeating pattern. "
)

// generateTestData creates test files in various encodings.
// This is called from TestMain to set up test data.
func generateTestData(t *testing.T) {
	testdataDir := "testdata"

	// Create testdata directory if it doesn't exist
	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		t.Fatalf("Failed to create testdata directory: %v", err)
	}

	tests := []struct {
		filename string
		content  string
		encoding string // "utf8", "shift-jis", "euc-jp", "iso-2022-jp"
		withBOM  bool
	}{
		// UTF-8
		{"utf8.txt", mixedContent, "utf8", false},
		{"utf8_bom.txt", mixedContent, "utf8", true},
		{"utf8_simple.txt", simpleJapanese, "utf8", false},
		{"utf8_long.txt", generateLongText(), "utf8", false},

		// Shift-JIS
		{"shift-jis.txt", mixedContent, "shift-jis", false},
		{"mixed_sjis_ascii.txt", mixedContent, "shift-jis", false},
		{"kanji_heavy.sjis", kanjiHeavy, "shift-jis", false},
		{"hiragana_only.sjis", hiraganaOnly, "shift-jis", false},

		// EUC-JP
		{"euc-jp.txt", mixedContent, "euc-jp", false},
		{"kanji_heavy.eucjp", kanjiHeavy, "euc-jp", false},
		{"katakana_only.eucjp", katakanaOnly, "euc-jp", false},

		// ISO-2022-JP
		{"iso-2022-jp.txt", literaryJapanese, "iso-2022-jp", false},

		// ASCII
		{"ascii.txt", asciiOnly, "utf8", false},

		// Edge cases
		{"empty.txt", "", "utf8", false},
		{"short.txt", "Hi", "utf8", false},
	}

	for _, tt := range tests {
		path := filepath.Join(testdataDir, tt.filename)
		if err := writeEncodedFile(path, tt.content, tt.encoding, tt.withBOM); err != nil {
			t.Fatalf("Failed to create %s: %v", tt.filename, err)
		}
	}
}

// writeEncodedFile writes content to a file in the specified encoding.
func writeEncodedFile(path, content, encoding string, withBOM bool) error {
	var encoded []byte
	var err error

	switch encoding {
	case "utf8":
		encoded = []byte(content)
		if withBOM {
			encoded = append(bomUTF8, encoded...)
		}

	case "shift-jis":
		encoder := japanese.ShiftJIS.NewEncoder()
		encoded, _, err = transform.Bytes(encoder, []byte(content))
		if err != nil {
			return err
		}

	case "euc-jp":
		encoder := japanese.EUCJP.NewEncoder()
		encoded, _, err = transform.Bytes(encoder, []byte(content))
		if err != nil {
			return err
		}

	case "iso-2022-jp":
		encoder := japanese.ISO2022JP.NewEncoder()
		encoded, _, err = transform.Bytes(encoder, []byte(content))
		if err != nil {
			return err
		}

	default:
		encoded = []byte(content)
	}

	return os.WriteFile(path, encoded, 0644)
}

// generateLongText creates a long text for testing large files.
func generateLongText() string {
	// Generate ~10KB of text
	text := ""
	for i := 0; i < 100; i++ {
		text += longTextUnit
	}
	return text
}

// TestGenerateTestData is a helper test that can be run to regenerate test data.
func TestGenerateTestData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test data generation in short mode")
	}
	generateTestData(t)
}
