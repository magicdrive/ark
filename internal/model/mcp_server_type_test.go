package model_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/model"
)

func TestMcpServerType_Set(t *testing.T) {
	tests := []struct {
		input       string
		expectError bool
		expected    model.McpSreverType
	}{
		{"http", false, model.McpSreverType("http")},
		{"stdio", false, model.McpSreverType("stdio")},
		{"aaaaaaaaa", true, ""},
		{"", true, ""},
	}

	for _, tt := range tests {
		var s model.McpSreverType
		err := s.Set(tt.input)
		if (err != nil) != tt.expectError {
			t.Errorf("Set(%q) error = %v, want error: %v", tt.input, err, tt.expectError)
		}
		if !tt.expectError && s != tt.expected {
			t.Errorf("Set(%q) = %v, want %v", tt.input, s, tt.expected)
		}
	}
}

func TestMcpServerType_String(t *testing.T) {
	vals := []string{"http", "stdio", "unknown"}
	for _, v := range vals {
		s := model.McpSreverType(v)
		if s.String() != v {
			t.Errorf("String() = %q, want %q", s.String(), v)
		}
	}
}
