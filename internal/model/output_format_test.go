package model_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/model"
)

func TestOutputFormat_Set_Valid(t *testing.T) {
	cases := []struct {
		input    string
		expected model.OutputFormat
	}{
		{"markdown", model.Markdown},
		{"Markdown", model.Markdown},
		{"md", model.Markdown},
		{"mark_down", model.Markdown},
		{"plaintext", model.PlainText},
		{"PlainText", model.PlainText},
		{"plain_text", model.PlainText},
		{"txt", model.PlainText},
	}

	for _, c := range cases {
		var out model.OutputFormat
		err := out.Set(c.input)
		if err != nil {
			t.Errorf("Set(%q) returned error: %v", c.input, err)
		}
		if out != c.expected {
			t.Errorf("Set(%q) = %q; want %q", c.input, out, c.expected)
		}
	}
}

func TestOutputFormat_Set_Invalid(t *testing.T) {
	invalidInputs := []string{"pdf", "docx", "", "textile"}
	for _, input := range invalidInputs {
		var out model.OutputFormat
		err := out.Set(input)
		if err == nil {
			t.Errorf("Set(%q) expected error but got nil", input)
		}
	}
}

func TestOutputFormat_String(t *testing.T) {
	cases := []struct {
		input    model.OutputFormat
		expected string
	}{
		{model.Markdown, "markdown"},
		{model.PlainText, "plaintext"},
	}
	for _, c := range cases {
		if c.input.String() != c.expected {
			t.Errorf("String() = %q; want %q", c.input.String(), c.expected)
		}
	}
}

func TestExt2OutputFormat(t *testing.T) {
	cases := []struct {
		ext      string
		expected string
	}{
		{"md", model.Markdown},
		{"Markdown", model.Markdown},
		{"txt", model.PlainText},
		{"unknown", model.PlainText}, // fallback
	}
	for _, c := range cases {
		got := model.Ext2OutputFormat(c.ext)
		if got != c.expected {
			t.Errorf("Ext2OutputFormat(%q) = %q; want %q", c.ext, got, c.expected)
		}
	}
}
