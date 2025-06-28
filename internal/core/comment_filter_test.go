package core

import (
	"bytes"
	"testing"
)

func TestStripComments(t *testing.T) {
	tests := []struct {
		name     string
		lang     string
		input    string
		expected string
	}{
		{
			name:     "Go line comments",
			lang:     "go",
			input:    "// line comment\nvar x = 1\n// another comment",
			expected: "var x = 1",
		},
		{
			name:     "Go block comments",
			lang:     "go",
			input:    "/* block comment */\nvar y = 2\n/* another\nblock */",
			expected: "var y = 2",
		},
		{
			name:     "Python hash comment",
			lang:     "python",
			input:    "# comment\nvalue = 42",
			expected: "value = 42",
		},
		{
			name:     "HTML block comment",
			lang:     "html",
			input:    "<!-- HTML comment -->\n<p>Hello</p>",
			expected: "<p>Hello</p>",
		},
		{
			name:     "Vim double quote comment",
			lang:     "vim",
			input:    "\" this is a comment\nlet g:foo = 1",
			expected: "let g:foo = 1",
		},
		{
			name:     "Default mixed comment",
			lang:     "unknown",
			input:    "# comment\ncode line\n// another\n<!-- html -->",
			expected: "# comment\ncode line\n// another\n<!-- html -->",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := getCommentDelimiters(tt.lang)
			output := stripComments([]byte(tt.input), pattern)
			if !bytes.Equal(bytes.TrimSpace(output), []byte(tt.expected)) {
				t.Errorf("expected %q, got %q", tt.expected, output)
			}
		})
	}
}
