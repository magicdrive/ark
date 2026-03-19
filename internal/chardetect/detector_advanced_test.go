package chardetect

import (
	"sync"
	"testing"
)

// TestDetect_Concurrent tests that detection is safe for concurrent use
func TestDetect_Concurrent(t *testing.T) {
	testData := []struct {
		name     string
		filename string
		expected Encoding
	}{
		{"UTF-8", "testdata/utf8.txt", UTF8},
		{"Shift-JIS", "testdata/shift-jis.txt", ShiftJIS},
		{"EUC-JP", "testdata/euc-jp.txt", EUCJP},
		// Note: ISO-2022-JP may be detected as ASCII if the content is very short
		// or doesn't contain escape sequences
	}

	// Create a detector once
	detector := NewDetector()

	var wg sync.WaitGroup
	errChan := make(chan error, len(testData)*10)

	// Run 10 goroutines for each test case
	for _, tt := range testData {
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(filename string, expected Encoding) {
				defer wg.Done()

				result, err := detector.DetectFile(filename)
				if err != nil {
					errChan <- err
					return
				}

				if result.Encoding != expected {
					t.Errorf("Concurrent detection failed: got %v, want %v",
						result.Encoding, expected)
				}
			}(tt.filename, tt.expected)
		}
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		t.Errorf("Concurrent detection error: %v", err)
	}
}

// TestDetect_VeryShortData tests detection with very short data
func TestDetect_VeryShortData(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected Encoding
	}{
		{
			name:     "Empty",
			data:     []byte{},
			expected: Unknown,
		},
		{
			name:     "1 byte ASCII",
			data:     []byte("a"),
			expected: ASCII,
		},
		{
			name:     "2 bytes ASCII",
			data:     []byte("ab"),
			expected: ASCII,
		},
		{
			name:     "3 bytes UTF-8 (one char)",
			data:     []byte("あ"),
			expected: UTF8,
		},
		{
			name:     "6 bytes UTF-8 (two chars)",
			data:     []byte("あい"),
			expected: UTF8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Detect(tt.data)

			if result.Encoding != tt.expected {
				t.Errorf("Detect() encoding = %v, want %v",
					result.Encoding, tt.expected)
			}

			t.Logf("Short data: %d bytes -> %s (confidence: %.2f)",
				len(tt.data), result.Encoding, result.Confidence)
		})
	}
}

// TestDetect_CorruptedSequences tests handling of corrupted byte sequences
func TestDetect_CorruptedSequences(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		// We don't strictly check the result, just that it doesn't panic
		shouldNotPanic bool
	}{
		{
			name:           "Incomplete Shift-JIS sequence at end",
			data:           []byte{0x41, 0x42, 0x43, 0x82}, // ABC + incomplete Shift-JIS
			shouldNotPanic: true,
		},
		{
			name:           "Invalid Shift-JIS trail byte",
			data:           []byte{0x82, 0x00, 0x82, 0xA0}, // Invalid then valid
			shouldNotPanic: true,
		},
		{
			name:           "Incomplete EUC-JP sequence",
			data:           []byte{0xA4, 0xA2, 0xA4}, // あ + incomplete
			shouldNotPanic: true,
		},
		{
			name:           "Invalid UTF-8 sequence",
			data:           []byte{0xC0, 0x80, 0xE0, 0x80}, // Invalid UTF-8
			shouldNotPanic: true,
		},
		{
			name:           "Mixed valid and invalid bytes",
			data:           []byte{0x41, 0xFF, 0x42, 0xFE, 0x43}, // ASCII mixed with invalid
			shouldNotPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Detect() panicked on corrupted data: %v", r)
				}
			}()

			result := Detect(tt.data)

			// Should return some result without panicking
			t.Logf("Corrupted data detected as: %s (confidence: %.2f)",
				result.Encoding, result.Confidence)
		})
	}
}

// TestDetect_BoundaryBytes tests edge cases at encoding boundaries
func TestDetect_BoundaryBytes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		desc string
	}{
		{
			name: "Shift-JIS boundary (0x81-0x9F)",
			data: []byte{0x81, 0x40, 0x9F, 0xFC}, // First and last valid lead bytes
			desc: "Testing Shift-JIS lead byte boundaries",
		},
		{
			name: "EUC-JP boundary (0xA1-0xFE)",
			data: []byte{0xA1, 0xA1, 0xFE, 0xFE}, // Boundary bytes
			desc: "Testing EUC-JP byte boundaries",
		},
		{
			name: "ASCII boundary (0x00-0x7F)",
			data: []byte{0x00, 0x20, 0x7F}, // NULL, space, DEL
			desc: "Testing ASCII boundaries",
		},
		{
			name: "High bytes (0x80-0xFF)",
			data: []byte{0x80, 0xA0, 0xC0, 0xE0, 0xFF},
			desc: "Testing various high bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Detect(tt.data)

			t.Logf("%s: %s (confidence: %.2f)",
				tt.desc, result.Encoding, result.Confidence)

			// Should not panic and should return some result
			if result.Encoding == "" {
				t.Error("Detect() returned empty encoding")
			}
		})
	}
}

// TestDetector_ScorerReuse tests that scorer reuse doesn't cause issues
func TestDetector_ScorerReuse(t *testing.T) {
	detector := NewDetector()

	// Detect the same data multiple times with the same detector
	data := []byte("こんにちは、世界！")

	results := make([]*Result, 5)
	for i := 0; i < 5; i++ {
		results[i] = detector.Detect(data)
	}

	// All results should be identical
	for i := 1; i < len(results); i++ {
		if results[i].Encoding != results[0].Encoding {
			t.Errorf("Reuse test: result[%d].Encoding = %v, want %v",
				i, results[i].Encoding, results[0].Encoding)
		}
		if results[i].Confidence != results[0].Confidence {
			t.Errorf("Reuse test: result[%d].Confidence = %v, want %v",
				i, results[i].Confidence, results[0].Confidence)
		}
	}
}

// TestDetect_LargeFile tests detection on larger files
func TestDetect_LargeFile(t *testing.T) {
	// Generate a large text
	largeText := make([]byte, 0, 1024*1024) // 1MB
	baseText := []byte("日本語のテキストです。This is Japanese text.\n")

	for len(largeText) < cap(largeText) {
		largeText = append(largeText, baseText...)
	}

	tests := []struct {
		name       string
		sampleSize int
	}{
		{"Default 8KB", 8192},
		{"Small 1KB", 1024},
		{"Large 64KB", 65536},
		{"Very large 512KB", 524288},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewDetector().WithSampleSize(tt.sampleSize)
			result := detector.Detect(largeText)

			if result.Encoding == Unknown {
				t.Errorf("Failed to detect large file with sample size %d",
					tt.sampleSize)
			}

			t.Logf("Large file (%d bytes) with sample %d: %s (confidence: %.2f)",
				len(largeText), tt.sampleSize, result.Encoding, result.Confidence)
		})
	}
}

// BenchmarkDetector_ScorerReuse benchmarks the improved scorer reuse
func BenchmarkDetector_ScorerReuse(b *testing.B) {
	data := []byte("こんにちは、世界！日本語のテキストです。")
	detector := NewDetector()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = detector.Detect(data)
	}
}
