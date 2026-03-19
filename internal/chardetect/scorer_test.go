package chardetect

import (
	"testing"
)

func TestScorer_detectBOM(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		wantEncoding Encoding
		wantFound    bool
	}{
		{
			name:         "UTF-8 BOM",
			data:         append(bomUTF8, []byte("test")...),
			wantEncoding: UTF8,
			wantFound:    true,
		},
		{
			name:         "No BOM",
			data:         []byte("test"),
			wantEncoding: Unknown,
			wantFound:    false,
		},
		{
			name:         "Empty data",
			data:         []byte{},
			wantEncoding: Unknown,
			wantFound:    false,
		},
		{
			name:         "Too short",
			data:         []byte{0xEF},
			wantEncoding: Unknown,
			wantFound:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScorer(tt.data, len(tt.data))
			gotEncoding, gotFound := s.detectBOM()

			if gotEncoding != tt.wantEncoding {
				t.Errorf("detectBOM() encoding = %v, want %v", gotEncoding, tt.wantEncoding)
			}
			if gotFound != tt.wantFound {
				t.Errorf("detectBOM() found = %v, want %v", gotFound, tt.wantFound)
			}
		})
	}
}

func TestScorer_isASCII(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "Pure ASCII",
			data: []byte("Hello, World!"),
			want: true,
		},
		{
			name: "With Japanese",
			data: []byte("Hello, 世界!"),
			want: false,
		},
		{
			name: "Empty",
			data: []byte{},
			want: true,
		},
		{
			name: "High byte",
			data: []byte{0x80},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScorer(tt.data, len(tt.data))
			if got := s.isASCII(); got != tt.want {
				t.Errorf("isASCII() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScorer_scoreUTF8(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		minScore float64
	}{
		{
			name:     "Valid UTF-8 Japanese",
			data:     []byte("こんにちは、世界！"),
			minScore: 0.8,
		},
		{
			name:     "ASCII only",
			data:     []byte("Hello, World!"),
			minScore: 0.5,
		},
		{
			name:     "Invalid UTF-8",
			data:     []byte{0xFF, 0xFE, 0xFD},
			minScore: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScorer(tt.data, len(tt.data))
			score := s.scoreUTF8()

			if score < tt.minScore {
				t.Errorf("scoreUTF8() = %v, want >= %v", score, tt.minScore)
			}
		})
	}
}

func TestScorer_scoreISO2022JP(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		minScore float64
	}{
		{
			name:     "With escape sequences",
			data:     []byte{0x1B, 0x24, 0x42, 'a', 'b', 0x1B, 0x28, 0x42},
			minScore: 0.85,
		},
		{
			name:     "No escape sequences",
			data:     []byte("regular text"),
			minScore: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScorer(tt.data, len(tt.data))
			score := s.scoreISO2022JP()

			if tt.minScore == 0.0 {
				if score != 0.0 {
					t.Errorf("scoreISO2022JP() = %v, want 0.0", score)
				}
			} else if score < tt.minScore {
				t.Errorf("scoreISO2022JP() = %v, want >= %v", score, tt.minScore)
			}
		})
	}
}

func TestNewScorer_SampleSize(t *testing.T) {
	data := make([]byte, 10000)
	for i := range data {
		data[i] = 'A'
	}

	tests := []struct {
		name       string
		sampleSize int
		wantSize   int
	}{
		{
			name:       "Normal sample",
			sampleSize: 1024,
			wantSize:   1024,
		},
		{
			name:       "Zero sample (use all)",
			sampleSize: 0,
			wantSize:   10000,
		},
		{
			name:       "Larger than data",
			sampleSize: 20000,
			wantSize:   10000,
		},
		{
			name:       "Negative (use all)",
			sampleSize: -1,
			wantSize:   10000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newScorer(data, tt.sampleSize)
			if len(s.data) != tt.wantSize {
				t.Errorf("newScorer() data length = %v, want %v", len(s.data), tt.wantSize)
			}
		})
	}
}
