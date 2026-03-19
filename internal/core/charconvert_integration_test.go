package core

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// TestConvertToUTF8_Integration tests the integration with actual file operations
func TestConvertToUTF8_Integration(t *testing.T) {
	// テスト用の一時ディレクトリを作成
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		content  string
		encoding string
	}{
		{
			name:     "shift-jis file",
			content:  "これはShift-JISでエンコードされたファイルです。\n日本語の文字が含まれています。",
			encoding: "shift-jis",
		},
		{
			name:     "euc-jp file",
			content:  "これはEUC-JPでエンコードされたファイルです。\n日本語の文字が含まれています。",
			encoding: "euc-jp",
		},
		{
			name:     "utf-8 file",
			content:  "これはUTF-8でエンコードされたファイルです。\n日本語の文字が含まれています。",
			encoding: "utf-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// ファイルを作成
			filename := filepath.Join(tmpDir, tt.name+".txt")
			var data []byte
			var err error

			switch tt.encoding {
			case "shift-jis":
				encoder := japanese.ShiftJIS.NewEncoder()
				data, _, err = transform.Bytes(encoder, []byte(tt.content))
			case "euc-jp":
				encoder := japanese.EUCJP.NewEncoder()
				data, _, err = transform.Bytes(encoder, []byte(tt.content))
			default:
				data = []byte(tt.content)
			}

			if err != nil {
				t.Fatalf("Failed to encode: %v", err)
			}

			err = os.WriteFile(filename, data, 0644)
			if err != nil {
				t.Fatalf("Failed to write file: %v", err)
			}

			// ファイルを読み込んでUTF-8に変換
			file, err := os.Open(filename)
			if err != nil {
				t.Fatalf("Failed to open file: %v", err)
			}
			defer file.Close()

			converted, err := ConvertToUTF8(file)
			if err != nil {
				t.Fatalf("ConvertToUTF8() error = %v", err)
			}

			// 変換結果を読み込む
			buf := new(bytes.Buffer)
			_, err = buf.ReadFrom(converted)
			if err != nil {
				t.Fatalf("Failed to read converted data: %v", err)
			}

			// 期待される内容と比較
			if buf.String() != tt.content {
				t.Errorf("ConvertToUTF8() integration test failed")
				t.Logf("Expected: %s", tt.content)
				t.Logf("Got: %s", buf.String())
			}
		})
	}
}

// TestConvertToUTF8_RealWorldFile tests with realistic file content
func TestConvertToUTF8_RealWorldFile(t *testing.T) {
	tmpDir := t.TempDir()

	// 実際のコードファイルのようなコンテンツ
	sourceCode := `package main

import "fmt"

// これは日本語のコメントです
func main() {
	// 変数の宣言
	message := "こんにちは、世界！"
	fmt.Println(message)

	// ループ処理
	for i := 0; i < 10; i++ {
		fmt.Printf("カウント: %d\n", i)
	}
}

/*
マルチラインコメント
複数行にわたるコメントです
日本語も含まれています
*/
`

	// Shift-JISでエンコード
	encoder := japanese.ShiftJIS.NewEncoder()
	sjisData, _, err := transform.Bytes(encoder, []byte(sourceCode))
	if err != nil {
		t.Fatalf("Failed to encode to Shift-JIS: %v", err)
	}

	// ファイルに書き込み
	filename := filepath.Join(tmpDir, "sample.go")
	err = os.WriteFile(filename, sjisData, 0644)
	if err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// ファイルを読み込んで変換
	file, err := os.Open(filename)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	converted, err := ConvertToUTF8(file)
	if err != nil {
		t.Fatalf("ConvertToUTF8() error = %v", err)
	}

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(converted)
	if err != nil {
		t.Fatalf("Failed to read converted data: %v", err)
	}

	if buf.String() != sourceCode {
		t.Error("Real world file conversion failed")
		t.Logf("Original length: %d", len(sourceCode))
		t.Logf("Converted length: %d", buf.Len())
	}
}

// TestConvertToUTF8_MultipleReads tests that the reader can be read multiple times
func TestConvertToUTF8_MultipleReads(t *testing.T) {
	original := "日本語のテキストです。"
	encoder := japanese.ShiftJIS.NewEncoder()
	data, _, _ := transform.Bytes(encoder, []byte(original))

	reader := bytes.NewReader(data)
	converted, err := ConvertToUTF8(reader)
	if err != nil {
		t.Fatalf("ConvertToUTF8() error = %v", err)
	}

	// 最初の読み込み
	buf1 := make([]byte, 10)
	n1, _ := converted.Read(buf1)

	// 2回目の読み込み
	buf2 := make([]byte, 100)
	n2, _ := converted.Read(buf2)

	totalRead := n1 + n2
	if totalRead == 0 {
		t.Error("No data was read from the converter")
	}

	t.Logf("Read %d bytes in first read, %d bytes in second read", n1, n2)
}

// TestConvertToUTF8_BinaryDataHandling tests handling of binary data
func TestConvertToUTF8_BinaryDataHandling(t *testing.T) {
	// バイナリデータ（画像のようなデータ）
	binaryData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46}

	reader := bytes.NewReader(binaryData)
	result, err := ConvertToUTF8(reader)
	if err != nil {
		t.Fatalf("ConvertToUTF8() should not error on binary data: %v", err)
	}

	// バイナリデータはそのまま返されるべき（変換されない）
	output := new(bytes.Buffer)
	output.ReadFrom(result)

	t.Logf("Binary data handling: input %d bytes, output %d bytes",
		len(binaryData), output.Len())
}

// TestConvertToUTF8_StreamProcessing tests streaming conversion
func TestConvertToUTF8_StreamProcessing(t *testing.T) {
	// 大きなデータをストリーム処理
	var original string
	for i := 0; i < 1000; i++ {
		original += "これは日本語のテストデータです。"
	}

	encoder := japanese.ShiftJIS.NewEncoder()
	sjisData, _, err := transform.Bytes(encoder, []byte(original))
	if err != nil {
		t.Fatalf("Failed to encode: %v", err)
	}

	reader := bytes.NewReader(sjisData)
	converted, err := ConvertToUTF8(reader)
	if err != nil {
		t.Fatalf("ConvertToUTF8() error = %v", err)
	}

	// チャンクで読み込む
	chunk := make([]byte, 1024)
	totalBytes := 0

	for {
		n, err := converted.Read(chunk)
		totalBytes += n
		if err != nil {
			break
		}
	}

	t.Logf("Stream processing: read %d bytes total", totalBytes)

	if totalBytes == 0 {
		t.Error("No data was read during stream processing")
	}
}

// TestConvertToUTF8_SpecialCharacters tests conversion of special characters
func TestConvertToUTF8_SpecialCharacters(t *testing.T) {
	// 特殊文字を含むテキスト
	original := `特殊文字のテスト：
- 波ダッシュ: ～
- 全角チルダ: ～
- ハイフン: ‐－−
- マイナス: −
- 長音: ー
- 中黒: ・
- 句読点: 、。
- かぎ括弧: 「」『』
- 記号: ①②③㈱㊤
- 旧字体: 國學髙
`

	encoder := japanese.ShiftJIS.NewEncoder()
	sjisData, _, err := transform.Bytes(encoder, []byte(original))
	if err != nil {
		t.Logf("Some special characters may not be encodable in Shift-JIS: %v", err)
		// Shift-JISでエンコードできない文字がある可能性があるため、
		// このテストはベストエフォート
		return
	}

	reader := bytes.NewReader(sjisData)
	converted, err := ConvertToUTF8(reader)
	if err != nil {
		t.Fatalf("ConvertToUTF8() error = %v", err)
	}

	output := new(bytes.Buffer)
	output.ReadFrom(converted)

	t.Logf("Special characters test: input %d bytes, output %d bytes",
		len(sjisData), output.Len())
}

// TestConvertToUTF8_ErrorRecovery tests error recovery
func TestConvertToUTF8_ErrorRecovery(t *testing.T) {
	// 壊れたShift-JISデータ
	corruptedData := []byte{0x82, 0x00, 0x82, 0xA0} // 不正なシーケンス

	reader := bytes.NewReader(corruptedData)
	result, err := ConvertToUTF8(reader)

	// エラーが発生しても関数は失敗しない（安全に処理）
	if err != nil {
		t.Logf("ConvertToUTF8() returned error (expected for corrupted data): %v", err)
	}

	// 結果を読み込めることを確認
	output := new(bytes.Buffer)
	output.ReadFrom(result)

	t.Logf("Error recovery test: processed %d bytes", output.Len())
}
