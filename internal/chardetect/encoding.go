package chardetect

// Encoding represents a character encoding type.
type Encoding string

// Supported character encodings.
const (
	// UTF8 represents UTF-8 encoding (with or without BOM).
	UTF8 Encoding = "UTF-8"

	// ShiftJIS represents Shift-JIS encoding (Japanese).
	ShiftJIS Encoding = "Shift-JIS"

	// EUCJP represents EUC-JP encoding (Extended Unix Code for Japanese).
	EUCJP Encoding = "EUC-JP"

	// ISO2022JP represents ISO-2022-JP encoding (Japanese email/network encoding).
	ISO2022JP Encoding = "ISO-2022-JP"

	// CP932 represents CP932/Windows-31J encoding (Microsoft's extension of Shift-JIS).
	CP932 Encoding = "CP932"

	// ASCII represents 7-bit ASCII encoding.
	ASCII Encoding = "ASCII"

	// Unknown represents an unidentified or unsupported encoding.
	Unknown Encoding = "Unknown"
)

// String returns the string representation of the encoding.
func (e Encoding) String() string {
	return string(e)
}

// IsValid returns true if the encoding is a known, supported encoding.
func (e Encoding) IsValid() bool {
	switch e {
	case UTF8, ShiftJIS, EUCJP, ISO2022JP, CP932, ASCII:
		return true
	default:
		return false
	}
}

// Result represents the result of character encoding detection.
type Result struct {
	// Encoding is the detected character encoding.
	Encoding Encoding

	// Confidence is the confidence level of the detection (0.0 to 1.0).
	// Higher values indicate higher confidence.
	// - 1.0: Absolute certainty (e.g., BOM detected)
	// - 0.9+: Very high confidence
	// - 0.7-0.9: High confidence
	// - 0.5-0.7: Medium confidence
	// - <0.5: Low confidence (may be unreliable)
	Confidence float64

	// Language is the detected language (e.g., "ja" for Japanese, "en" for English).
	// This may be empty if language detection is not applicable.
	Language string
}

// BOM (Byte Order Mark) signatures for various encodings.
var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// ISO-2022-JP escape sequences.
var (
	// Escape sequence to enter Kanji mode
	escSeqKanjiIn = [][]byte{
		{0x1B, 0x24, 0x42}, // ESC $ B
		{0x1B, 0x24, 0x40}, // ESC $ @
	}

	// Escape sequence to exit Kanji mode (return to ASCII)
	escSeqKanjiOut = [][]byte{
		{0x1B, 0x28, 0x42}, // ESC ( B
		{0x1B, 0x28, 0x4A}, // ESC ( J
	}
)

// Byte range constants for encoding detection.
const (
	// Shift-JIS first byte ranges
	sjisLead1Low  = 0x81
	sjisLead1High = 0x9F
	sjisLead2Low  = 0xE0
	sjisLead2High = 0xFC

	// Shift-JIS second byte ranges
	sjisTrail1Low  = 0x40
	sjisTrail1High = 0x7E
	sjisTrail2Low  = 0x80
	sjisTrail2High = 0xFC

	// EUC-JP byte ranges
	eucjpLow  = 0xA1
	eucjpHigh = 0xFE

	// Half-width katakana in EUC-JP
	eucjpKatakanaSS2 = 0x8E

	// JIS X 0212 supplementary kanji in EUC-JP
	eucjpKanjiSS3 = 0x8F

	// ASCII range
	asciiHigh = 0x7F
)
