package chardetect

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkDetect_UTF8(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "utf8.txt"))
	if err != nil {
		b.Fatalf("Failed to read test file: %v", err)
	}

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		_ = Detect(data)
	}
}

func BenchmarkDetect_ShiftJIS(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "shift-jis.txt"))
	if err != nil {
		b.Fatalf("Failed to read test file: %v", err)
	}

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		_ = Detect(data)
	}
}

func BenchmarkDetect_EUCJP(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "euc-jp.txt"))
	if err != nil {
		b.Fatalf("Failed to read test file: %v", err)
	}

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		_ = Detect(data)
	}
}

func BenchmarkDetect_ISO2022JP(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "iso-2022-jp.txt"))
	if err != nil {
		b.Fatalf("Failed to read test file: %v", err)
	}

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		_ = Detect(data)
	}
}

func BenchmarkDetect_LongText(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "utf8_long.txt"))
	if err != nil {
		b.Fatalf("Failed to read test file: %v", err)
	}

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		_ = Detect(data)
	}
}

func BenchmarkDetector_WithSampleSize1KB(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "utf8_long.txt"))
	if err != nil {
		b.Fatalf("Failed to read test file: %v", err)
	}

	detector := NewDetector().WithSampleSize(1024)

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		_ = detector.Detect(data)
	}
}

func BenchmarkDetector_WithSampleSize4KB(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "utf8_long.txt"))
	if err != nil {
		b.Fatalf("Failed to read test file: %v", err)
	}

	detector := NewDetector().WithSampleSize(4096)

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		_ = detector.Detect(data)
	}
}

func BenchmarkDetectFile(b *testing.B) {
	path := filepath.Join("testdata", "utf8.txt")

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		_, _ = DetectFile(path)
	}
}
