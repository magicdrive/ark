package chardetect_test

import (
	"fmt"
	"log"

	"github.com/magicdrive/ark/internal/chardetect"
)

func ExampleDetect() {
	// Sample Japanese text in UTF-8
	data := []byte("こんにちは、世界！")

	result := chardetect.Detect(data)
	fmt.Printf("Encoding: %s\n", result.Encoding)
	fmt.Printf("Confidence: %.2f\n", result.Confidence)
	// Output:
	// Encoding: UTF-8
	// Confidence: 0.95
}

func ExampleDetectFile() {
	result, err := chardetect.DetectFile("testdata/utf8.txt")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Detected encoding: %s\n", result.Encoding)
	if result.Confidence > 0.8 {
		fmt.Println("High confidence detection")
	}
}

func ExampleNewDetector() {
	// Create a detector with custom settings
	detector := chardetect.NewDetector().
		WithMinConfidence(0.8).
		WithSampleSize(4096)

	data := []byte("日本語のテキスト")
	result := detector.Detect(data)

	if result.Encoding == chardetect.Unknown {
		fmt.Println("Could not detect encoding with high confidence")
	} else {
		fmt.Printf("Detected: %s (%.2f confidence)\n",
			result.Encoding, result.Confidence)
	}
}

func ExampleDetector_Detect() {
	detector := chardetect.NewDetector()

	// UTF-8 text
	utf8Data := []byte("こんにちは")
	result := detector.Detect(utf8Data)
	fmt.Printf("UTF-8: %s\n", result.Encoding)

	// The same detector can be reused
	asciiData := []byte("Hello, World!")
	result = detector.Detect(asciiData)
	fmt.Printf("ASCII: %s\n", result.Encoding)
}

func ExampleDetector_WithMinConfidence() {
	detector := chardetect.NewDetector().WithMinConfidence(0.9)

	data := []byte("あいうえお")
	result := detector.Detect(data)

	if result.Encoding == chardetect.Unknown {
		fmt.Printf("Confidence %.2f is below threshold 0.9\n", result.Confidence)
	} else {
		fmt.Printf("High confidence: %s\n", result.Encoding)
	}
}

func ExampleDetector_WithSampleSize() {
	// Only analyze the first 1KB for faster detection
	detector := chardetect.NewDetector().WithSampleSize(1024)

	// Large file data
	largeData := make([]byte, 100000)
	copy(largeData, []byte("日本語のテキスト"))

	result := detector.Detect(largeData)
	fmt.Printf("Detected from 1KB sample: %s\n", result.Encoding)
}

func ExampleEncoding_IsValid() {
	validEncoding := chardetect.UTF8
	invalidEncoding := chardetect.Unknown

	fmt.Printf("UTF-8 is valid: %t\n", validEncoding.IsValid())
	fmt.Printf("Unknown is valid: %t\n", invalidEncoding.IsValid())
	// Output:
	// UTF-8 is valid: true
	// Unknown is valid: false
}

func ExampleResult() {
	data := []byte("吾輩は猫である")
	result := chardetect.Detect(data)

	fmt.Printf("Encoding: %s\n", result.Encoding)
	fmt.Printf("Confidence: %.2f\n", result.Confidence)
	fmt.Printf("Language: %s\n", result.Language)

	// Check if detection was successful
	if result.Encoding.IsValid() && result.Confidence > 0.7 {
		fmt.Println("Reliable detection")
	}
}
