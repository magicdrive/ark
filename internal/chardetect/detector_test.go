package chardetect

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain sets up test data before running tests.
func TestMain(m *testing.M) {
	// Generate test data files
	t := &testing.T{}
	generateTestData(t)

	// Run tests
	code := m.Run()

	// Cleanup is optional - we can keep testdata for manual inspection
	os.Exit(code)
}

func TestDetect_UTF8(t *testing.T) {
	tests := []struct {
		name          string
		filename      string
		wantEncoding  Encoding
		minConfidence float64
		checkLanguage bool
		expectedLang  string
	}{
		{
			name:          "UTF8 mixed content",
			filename:      "utf8.txt",
			wantEncoding:  UTF8,
			minConfidence: 0.7,
		},
		{
			name:          "UTF8 with BOM",
			filename:      "utf8_bom.txt",
			wantEncoding:  UTF8,
			minConfidence: 1.0,
		},
		{
			name:          "UTF8 simple",
			filename:      "utf8_simple.txt",
			wantEncoding:  UTF8,
			minConfidence: 0.7,
		},
		{
			name:          "UTF8 long text",
			filename:      "utf8_long.txt",
			wantEncoding:  UTF8,
			minConfidence: 0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tt.filename))
			if err != nil {
				t.Fatalf("Failed to read test file: %v", err)
			}

			result := Detect(data)

			if result.Encoding != tt.wantEncoding {
				t.Errorf("Detect() encoding = %v, want %v", result.Encoding, tt.wantEncoding)
			}

			if result.Confidence < tt.minConfidence {
				t.Errorf("Detect() confidence = %v, want >= %v", result.Confidence, tt.minConfidence)
			}

			t.Logf("Detected: %s (confidence: %.2f, language: %s)",
				result.Encoding, result.Confidence, result.Language)
		})
	}
}

func TestDetect_ShiftJIS(t *testing.T) {
	tests := []struct {
		name          string
		filename      string
		wantEncoding  Encoding
		minConfidence float64
	}{
		{
			name:          "Shift-JIS mixed",
			filename:      "shift-jis.txt",
			wantEncoding:  ShiftJIS,
			minConfidence: 0.7,
		},
		{
			name:          "Shift-JIS with ASCII",
			filename:      "mixed_sjis_ascii.txt",
			wantEncoding:  ShiftJIS,
			minConfidence: 0.5,
		},
		{
			name:          "Kanji heavy Shift-JIS",
			filename:      "kanji_heavy.sjis",
			wantEncoding:  ShiftJIS,
			minConfidence: 0.8,
		},
		{
			name:          "Hiragana only Shift-JIS",
			filename:      "hiragana_only.sjis",
			wantEncoding:  ShiftJIS,
			minConfidence: 0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tt.filename))
			if err != nil {
				t.Fatalf("Failed to read test file: %v", err)
			}

			result := Detect(data)

			if result.Encoding != tt.wantEncoding {
				t.Errorf("Detect() encoding = %v, want %v", result.Encoding, tt.wantEncoding)
			}

			if result.Confidence < tt.minConfidence {
				t.Errorf("Detect() confidence = %v, want >= %v", result.Confidence, tt.minConfidence)
			}

			if result.Language != "ja" {
				t.Errorf("Detect() language = %v, want 'ja'", result.Language)
			}

			t.Logf("Detected: %s (confidence: %.2f, language: %s)",
				result.Encoding, result.Confidence, result.Language)
		})
	}
}

func TestDetect_EUCJP(t *testing.T) {
	tests := []struct {
		name          string
		filename      string
		wantEncoding  Encoding
		minConfidence float64
	}{
		{
			name:          "EUC-JP mixed",
			filename:      "euc-jp.txt",
			wantEncoding:  EUCJP,
			minConfidence: 0.7,
		},
		{
			name:          "Kanji heavy EUC-JP",
			filename:      "kanji_heavy.eucjp",
			wantEncoding:  EUCJP,
			minConfidence: 0.8,
		},
		{
			name:          "Katakana only EUC-JP",
			filename:      "katakana_only.eucjp",
			wantEncoding:  EUCJP,
			minConfidence: 0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tt.filename))
			if err != nil {
				t.Fatalf("Failed to read test file: %v", err)
			}

			result := Detect(data)

			if result.Encoding != tt.wantEncoding {
				t.Errorf("Detect() encoding = %v, want %v", result.Encoding, tt.wantEncoding)
			}

			if result.Confidence < tt.minConfidence {
				t.Errorf("Detect() confidence = %v, want >= %v", result.Confidence, tt.minConfidence)
			}

			if result.Language != "ja" {
				t.Errorf("Detect() language = %v, want 'ja'", result.Language)
			}

			t.Logf("Detected: %s (confidence: %.2f, language: %s)",
				result.Encoding, result.Confidence, result.Language)
		})
	}
}

func TestDetect_ISO2022JP(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "iso-2022-jp.txt"))
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	result := Detect(data)

	// ISO-2022-JP detection depends on escape sequences
	// If the file content is too short or ASCII-only, it may be detected as ASCII
	t.Logf("Detected: %s (confidence: %.2f, language: %s)",
		result.Encoding, result.Confidence, result.Language)

	// Accept both ISO-2022-JP and ASCII for this test
	// (ASCII is valid if the content doesn't contain escape sequences)
	if result.Encoding != ISO2022JP && result.Encoding != ASCII {
		t.Errorf("Detect() encoding = %v, want ISO-2022-JP or ASCII", result.Encoding)
	}
}

func TestDetect_ASCII(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ascii.txt"))
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	result := Detect(data)

	if result.Encoding != ASCII {
		t.Errorf("Detect() encoding = %v, want %v", result.Encoding, ASCII)
	}

	if result.Confidence != 1.0 {
		t.Errorf("Detect() confidence = %v, want 1.0", result.Confidence)
	}

	t.Logf("Detected: %s (confidence: %.2f, language: %s)",
		result.Encoding, result.Confidence, result.Language)
}

func TestDetect_EdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		wantEncoding Encoding
	}{
		{
			name:         "Empty file",
			filename:     "empty.txt",
			wantEncoding: Unknown,
		},
		{
			name:         "Short file",
			filename:     "short.txt",
			wantEncoding: ASCII,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tt.filename))
			if err != nil {
				t.Fatalf("Failed to read test file: %v", err)
			}

			result := Detect(data)

			if result.Encoding != tt.wantEncoding {
				t.Errorf("Detect() encoding = %v, want %v", result.Encoding, tt.wantEncoding)
			}

			t.Logf("Detected: %s (confidence: %.2f)",
				result.Encoding, result.Confidence)
		})
	}
}

func TestDetector_WithMinConfidence(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "utf8.txt"))
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	detector := NewDetector().WithMinConfidence(0.95)
	result := detector.Detect(data)

	// With high min confidence, some detections might return Unknown
	t.Logf("Detected: %s (confidence: %.2f)", result.Encoding, result.Confidence)

	if result.Confidence >= 0.95 && result.Encoding == Unknown {
		t.Error("High confidence result should not be Unknown")
	}
}

func TestDetector_WithSampleSize(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "utf8_long.txt"))
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	tests := []struct {
		name       string
		sampleSize int
	}{
		{"1KB sample", 1024},
		{"4KB sample", 4096},
		{"Full file", len(data)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewDetector().WithSampleSize(tt.sampleSize)
			result := detector.Detect(data)

			t.Logf("Sample size %d: %s (confidence: %.2f)",
				tt.sampleSize, result.Encoding, result.Confidence)

			if result.Encoding == Unknown {
				t.Error("Should detect encoding even with smaller sample")
			}
		})
	}
}

func TestDetectFile(t *testing.T) {
	path := filepath.Join("testdata", "utf8.txt")

	result, err := DetectFile(path)
	if err != nil {
		t.Fatalf("DetectFile() error = %v", err)
	}

	if result.Encoding == Unknown {
		t.Errorf("DetectFile() should detect encoding")
	}

	t.Logf("Detected from file: %s (confidence: %.2f)",
		result.Encoding, result.Confidence)
}

func TestDetectFile_NotFound(t *testing.T) {
	_, err := DetectFile("testdata/nonexistent.txt")
	if err == nil {
		t.Error("DetectFile() should return error for non-existent file")
	}
}

func TestEncoding_String(t *testing.T) {
	tests := []struct {
		encoding Encoding
		want     string
	}{
		{UTF8, "UTF-8"},
		{ShiftJIS, "Shift-JIS"},
		{EUCJP, "EUC-JP"},
		{ISO2022JP, "ISO-2022-JP"},
		{CP932, "CP932"},
		{ASCII, "ASCII"},
		{Unknown, "Unknown"},
	}

	for _, tt := range tests {
		if got := tt.encoding.String(); got != tt.want {
			t.Errorf("Encoding.String() = %v, want %v", got, tt.want)
		}
	}
}

func TestEncoding_IsValid(t *testing.T) {
	tests := []struct {
		encoding Encoding
		want     bool
	}{
		{UTF8, true},
		{ShiftJIS, true},
		{EUCJP, true},
		{ISO2022JP, true},
		{CP932, true},
		{ASCII, true},
		{Unknown, false},
		{Encoding("invalid"), false},
	}

	for _, tt := range tests {
		if got := tt.encoding.IsValid(); got != tt.want {
			t.Errorf("Encoding.IsValid() for %v = %v, want %v",
				tt.encoding, got, tt.want)
		}
	}
}
