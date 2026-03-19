/*
Package chardetect provides character encoding detection for text files,
with a focus on Japanese encodings.

It supports UTF-8, Shift-JIS, EUC-JP, ISO-2022-JP, and CP932 (Windows-31J)
with high accuracy through statistical analysis and pattern matching.

# Supported Encodings

  - UTF-8 (with and without BOM)
  - Shift-JIS (Shift_JIS)
  - EUC-JP (Extended Unix Code for Japanese)
  - ISO-2022-JP (Japanese email/network encoding)
  - CP932 (Windows-31J, Microsoft's extension of Shift-JIS)
  - ASCII (7-bit ASCII)

# Basic Usage

The simplest way to detect encoding:

	data, _ := os.ReadFile("file.txt")
	result := chardetect.Detect(data)
	fmt.Printf("Encoding: %s (confidence: %.2f)\n",
		result.Encoding, result.Confidence)

# Advanced Usage

Create a detector with custom settings:

	detector := chardetect.NewDetector().
		WithMinConfidence(0.8).
		WithSampleSize(8192)

	result := detector.Detect(data)
	if result.Confidence >= 0.8 {
		fmt.Printf("High confidence: %s\n", result.Encoding)
	}

# Detection from Files

Detect encoding directly from a file:

	result, err := chardetect.DetectFile("legacy.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Detected: %s\n", result.Encoding)

# Detection Algorithm

The detection process uses multiple strategies:

 1. BOM (Byte Order Mark) detection for instant identification
 2. Escape sequence detection for ISO-2022-JP
 3. Byte pattern analysis for Shift-JIS and EUC-JP
 4. Statistical scoring for final determination

This multi-stage approach ensures high accuracy even with small samples
or ambiguous content.

# Performance

The detector is optimized for speed and minimal memory allocation:

  - Early termination when high confidence is reached
  - Efficient byte scanning without regex
  - No heap allocations in hot paths
  - Concurrent-safe detector instances

# Thread Safety

All detector instances are safe for concurrent use.
The package-level Detect* functions are also safe for concurrent calls.
*/
package chardetect
