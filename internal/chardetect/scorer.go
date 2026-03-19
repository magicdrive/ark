package chardetect

import (
	"bytes"
	"unicode/utf8"
)

// scorer contains scoring logic for character encoding detection.
type scorer struct {
	data       []byte
	sampleSize int
}

// newScorer creates a new scorer for the given data.
func newScorer(data []byte, sampleSize int) *scorer {
	if sampleSize <= 0 || sampleSize > len(data) {
		sampleSize = len(data)
	}
	return &scorer{
		data:       data[:sampleSize],
		sampleSize: sampleSize,
	}
}

// detectBOM checks for Byte Order Mark and returns encoding if found.
func (s *scorer) detectBOM() (Encoding, bool) {
	if len(s.data) < 2 {
		return Unknown, false
	}

	// Check UTF-8 BOM
	if len(s.data) >= 3 && bytes.HasPrefix(s.data, bomUTF8) {
		return UTF8, true
	}

	// UTF-16 BOMs (not Japanese, but good to detect)
	if bytes.HasPrefix(s.data, bomUTF16LE) || bytes.HasPrefix(s.data, bomUTF16BE) {
		return UTF8, true // Treat as UTF-8 for simplicity
	}

	return Unknown, false
}

// scoreUTF8 scores the data as UTF-8.
func (s *scorer) scoreUTF8() float64 {
	if len(s.data) == 0 {
		return 0.0
	}

	// Check if valid UTF-8
	if !utf8.Valid(s.data) {
		return 0.0
	}

	// All ASCII is technically valid UTF-8
	if s.isASCII() {
		return 0.6 // Medium-low confidence for pure ASCII
	}

	// Count multi-byte sequences
	multiByteCount := 0
	totalRunes := 0

	// Use local variable to avoid modifying s.data
	data := s.data
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			return 0.0
		}
		if size > 1 {
			multiByteCount++
		}
		totalRunes++
		data = data[size:]
	}

	if totalRunes == 0 {
		return 0.0
	}

	// Higher ratio of multi-byte chars = higher confidence it's UTF-8
	ratio := float64(multiByteCount) / float64(totalRunes)

	// Score based on multi-byte ratio
	if ratio > 0.3 {
		return 0.95
	} else if ratio > 0.1 {
		return 0.85
	} else if ratio > 0.0 {
		return 0.75
	}

	return 0.6
}

// scoreShiftJIS scores the data as Shift-JIS.
func (s *scorer) scoreShiftJIS() float64 {
	if len(s.data) == 0 {
		return 0.0
	}

	validSequences := 0
	invalidSequences := 0
	totalBytes := len(s.data)
	i := 0

	for i < totalBytes {
		b := s.data[i]

		// ASCII range
		if b <= asciiHigh {
			i++
			continue
		}

		// Check Shift-JIS lead byte
		if (b >= sjisLead1Low && b <= sjisLead1High) ||
			(b >= sjisLead2Low && b <= sjisLead2High) {

			if i+1 >= totalBytes {
				invalidSequences++
				break
			}

			trail := s.data[i+1]
			// Check Shift-JIS trail byte
			if (trail >= sjisTrail1Low && trail <= sjisTrail1High) ||
				(trail >= sjisTrail2Low && trail <= sjisTrail2High) {
				validSequences++
				i += 2
			} else {
				invalidSequences++
				i++
			}
		} else {
			invalidSequences++
			i++
		}
	}

	if validSequences == 0 {
		return 0.0
	}

	// Calculate score based on valid/invalid ratio
	totalSequences := validSequences + invalidSequences
	if totalSequences == 0 {
		return 0.0
	}

	ratio := float64(validSequences) / float64(totalSequences)

	// High ratio = high confidence
	if ratio > 0.95 && validSequences > 10 {
		return 0.95
	} else if ratio > 0.90 && validSequences > 5 {
		return 0.90
	} else if ratio > 0.80 {
		return 0.80
	} else if ratio > 0.70 {
		return 0.70
	} else if ratio > 0.50 {
		return 0.50
	}

	return ratio * 0.5
}

// scoreEUCJP scores the data as EUC-JP.
func (s *scorer) scoreEUCJP() float64 {
	if len(s.data) == 0 {
		return 0.0
	}

	validSequences := 0
	invalidSequences := 0
	totalBytes := len(s.data)
	i := 0

	for i < totalBytes {
		b := s.data[i]

		// ASCII range
		if b <= asciiHigh {
			i++
			continue
		}

		// Half-width katakana (SS2)
		if b == eucjpKatakanaSS2 {
			if i+1 >= totalBytes {
				invalidSequences++
				break
			}
			trail := s.data[i+1]
			if trail >= 0xA1 && trail <= 0xDF {
				validSequences++
				i += 2
			} else {
				invalidSequences++
				i++
			}
			continue
		}

		// JIS X 0212 supplementary kanji (SS3)
		if b == eucjpKanjiSS3 {
			if i+2 >= totalBytes {
				invalidSequences++
				break
			}
			b2 := s.data[i+1]
			b3 := s.data[i+2]
			if (b2 >= eucjpLow && b2 <= eucjpHigh) &&
				(b3 >= eucjpLow && b3 <= eucjpHigh) {
				validSequences++
				i += 3
			} else {
				invalidSequences++
				i++
			}
			continue
		}

		// Standard EUC-JP two-byte sequence
		if b >= eucjpLow && b <= eucjpHigh {
			if i+1 >= totalBytes {
				invalidSequences++
				break
			}
			trail := s.data[i+1]
			if trail >= eucjpLow && trail <= eucjpHigh {
				validSequences++
				i += 2
			} else {
				invalidSequences++
				i++
			}
		} else {
			invalidSequences++
			i++
		}
	}

	if validSequences == 0 {
		return 0.0
	}

	totalSequences := validSequences + invalidSequences
	if totalSequences == 0 {
		return 0.0
	}

	ratio := float64(validSequences) / float64(totalSequences)

	// High ratio = high confidence
	if ratio > 0.95 && validSequences > 10 {
		return 0.95
	} else if ratio > 0.90 && validSequences > 5 {
		return 0.90
	} else if ratio > 0.80 {
		return 0.80
	} else if ratio > 0.70 {
		return 0.70
	} else if ratio > 0.50 {
		return 0.50
	}

	return ratio * 0.5
}

// scoreISO2022JP scores the data as ISO-2022-JP.
func (s *scorer) scoreISO2022JP() float64 {
	if len(s.data) == 0 {
		return 0.0
	}

	escapeCount := 0

	// Look for escape sequences
	for i := 0; i < len(s.data)-2; i++ {
		if s.data[i] != 0x1B {
			continue
		}

		// Check for Kanji-in sequences
		for _, seq := range escSeqKanjiIn {
			if i+len(seq) <= len(s.data) &&
				bytes.Equal(s.data[i:i+len(seq)], seq) {
				escapeCount++
				break
			}
		}

		// Check for Kanji-out sequences
		for _, seq := range escSeqKanjiOut {
			if i+len(seq) <= len(s.data) &&
				bytes.Equal(s.data[i:i+len(seq)], seq) {
				escapeCount++
				break
			}
		}
	}

	// ISO-2022-JP is characterized by escape sequences
	if escapeCount == 0 {
		return 0.0
	}

	// More escape sequences = higher confidence
	if escapeCount >= 4 {
		return 1.0
	} else if escapeCount >= 2 {
		return 0.95
	} else if escapeCount == 1 {
		return 0.85
	}

	return 0.0
}

// isASCII checks if data contains only ASCII characters.
func (s *scorer) isASCII() bool {
	for _, b := range s.data {
		if b > asciiHigh {
			return false
		}
	}
	return true
}
