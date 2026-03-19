package chardetect

import (
	"io"
	"os"
)

// Detector is the character encoding detector with configurable options.
type Detector struct {
	// MinConfidence is the minimum confidence threshold (0.0 to 1.0).
	// Results below this threshold will return Unknown encoding.
	MinConfidence float64

	// SampleSize is the number of bytes to analyze.
	// Larger values provide better accuracy but slower performance.
	// If 0 or negative, the entire input is analyzed.
	SampleSize int
}

// defaultSampleSize is the default number of bytes to sample.
const defaultSampleSize = 8192 // 8KB

// NewDetector creates a new Detector with default settings.
func NewDetector() *Detector {
	return &Detector{
		MinConfidence: 0.0, // No minimum by default
		SampleSize:    defaultSampleSize,
	}
}

// WithMinConfidence sets the minimum confidence threshold and returns the detector.
func (d *Detector) WithMinConfidence(confidence float64) *Detector {
	d.MinConfidence = confidence
	return d
}

// WithSampleSize sets the sample size and returns the detector.
func (d *Detector) WithSampleSize(size int) *Detector {
	d.SampleSize = size
	return d
}

// Detect detects the character encoding of the given data.
func (d *Detector) Detect(data []byte) *Result {
	if len(data) == 0 {
		return &Result{
			Encoding:   Unknown,
			Confidence: 0.0,
		}
	}

	s := newScorer(data, d.SampleSize)

	// Step 1: Check for BOM (instant identification)
	if encoding, found := s.detectBOM(); found {
		return &Result{
			Encoding:   encoding,
			Confidence: 1.0,
			Language:   "ja",
		}
	}

	// Step 2: Handle ASCII-only case early
	if s.isASCII() {
		return &Result{
			Encoding:   ASCII,
			Confidence: 1.0,
			Language:   "en",
		}
	}

	// Step 3: Score each encoding (reuse same scorer for efficiency)
	scores := make(map[Encoding]float64)

	// ISO-2022-JP has distinctive escape sequences
	scores[ISO2022JP] = s.scoreISO2022JP()
	if scores[ISO2022JP] >= 0.85 {
		// Early return for ISO-2022-JP (high confidence)
		return &Result{
			Encoding:   ISO2022JP,
			Confidence: scores[ISO2022JP],
			Language:   "ja",
		}
	}

	// Score other encodings (reusing the same scorer instance)
	scores[UTF8] = s.scoreUTF8()
	scores[ShiftJIS] = s.scoreShiftJIS()
	scores[EUCJP] = s.scoreEUCJP()

	// CP932 is essentially Shift-JIS with extensions
	// Use Shift-JIS score with slight adjustment
	scores[CP932] = scores[ShiftJIS] * 0.95

	// Step 4: Find the highest score
	var bestEncoding Encoding = Unknown
	var bestScore float64 = 0.0

	for enc, score := range scores {
		if score > bestScore {
			bestScore = score
			bestEncoding = enc
		}
	}

	// Step 5: Apply minimum confidence threshold
	if bestScore < d.MinConfidence {
		return &Result{
			Encoding:   Unknown,
			Confidence: bestScore,
		}
	}

	// Determine language
	language := ""
	if bestEncoding == ShiftJIS || bestEncoding == EUCJP ||
		bestEncoding == ISO2022JP || bestEncoding == CP932 {
		language = "ja"
	} else if bestEncoding == UTF8 && bestScore > 0.7 {
		language = "ja" // Likely Japanese UTF-8
	}

	return &Result{
		Encoding:   bestEncoding,
		Confidence: bestScore,
		Language:   language,
	}
}

// Detect detects the character encoding using default settings.
// This is a convenience function equivalent to NewDetector().Detect(data).
func Detect(data []byte) *Result {
	return NewDetector().Detect(data)
}

// DetectReader detects the character encoding from an io.Reader.
// It reads up to the configured SampleSize bytes from the reader.
func DetectReader(r io.Reader) (*Result, error) {
	return NewDetector().DetectReader(r)
}

// DetectReader detects the character encoding from an io.Reader.
func (d *Detector) DetectReader(r io.Reader) (*Result, error) {
	sampleSize := d.SampleSize
	if sampleSize <= 0 {
		sampleSize = defaultSampleSize
	}

	buf := make([]byte, sampleSize)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}

	return d.Detect(buf[:n]), nil
}

// DetectFile detects the character encoding of a file.
// This is a convenience function that opens the file and calls DetectReader.
func DetectFile(path string) (*Result, error) {
	return NewDetector().DetectFile(path)
}

// DetectFile detects the character encoding of a file.
func (d *Detector) DetectFile(path string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return d.DetectReader(f)
}
