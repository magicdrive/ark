package model_test

import (
	"testing"

	"github.com/magicdrive/enma/internal/model"
)

func TestOnOffSwitch_Set(t *testing.T) {
	tests := []struct {
		input       string
		expectError bool
		expected    model.OnOffSwitch
	}{
		{"on", false, model.OnOffSwitch("on")},
		{"off", false, model.OnOffSwitch("off")},
		{"ON", false, model.OnOffSwitch("on")},
		{"OFF", false, model.OnOffSwitch("off")},
		{"yes", false, model.OnOffSwitch("on")},
		{"YES", false, model.OnOffSwitch("on")},
		{"no", false, model.OnOffSwitch("off")},
		{"NO", false, model.OnOffSwitch("off")},
		{"y", false, model.OnOffSwitch("on")},
		{"n", false, model.OnOffSwitch("off")},
		{"aaaaaaaaa", true, ""},
		{"ON|OFF", true, ""},
		{"ON|OFF", true, ""},
		{"", true, ""},
	}

	for _, tt := range tests {
		var s model.OnOffSwitch
		err := s.Set(tt.input)
		if (err != nil) != tt.expectError {
			t.Errorf("Set(%q) error = %v, want error: %v", tt.input, err, tt.expectError)
		}
		if !tt.expectError && s != tt.expected {
			t.Errorf("Set(%q) = %v, want %v", tt.input, s, tt.expected)
		}
	}
}

func TestOnOffSwitch_String(t *testing.T) {
	vals := []string{"on", "off", "unknown"}
	for _, v := range vals {
		s := model.OnOffSwitch(v)
		if s.String() != v {
			t.Errorf("String() = %q, want %q", s.String(), v)
		}
	}
}
