package model_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/model"
)

func TestByteString_Set_Valid(t *testing.T) {
	validInputs := []string{
		"1024", "1K", "1KB", "1Ki", "1KIB",
		"1M", "1MB", "1Mi", "1MIB",
		"1G", "1GB", "1Gi", "1GIB",
		"1T", "1TB", "1Ti", "1TIB",
		"1P", "1PB", "1Pi", "1PIB",
		"1.5G", "2.25MB", "3.75kib",
	}

	for _, input := range validInputs {
		var b model.ByteString
		err := b.Set(input)
		if err != nil {
			t.Errorf("Set(%q) returned error: %v", input, err)
		}
	}
}

func TestByteString_Set_Invalid(t *testing.T) {
	invalidInputs := []string{
		"", "1X", "G1", "1..5GB", "1.5.5GB", "MB", "tenMB",
	}

	for _, input := range invalidInputs {
		var b model.ByteString
		err := b.Set(input)
		if err == nil {
			t.Errorf("Set(%q) expected error but got nil", input)
		}
	}
}

func TestByteString_Bytes(t *testing.T) {
	cases := []struct {
		input    string
		expected int64
	}{
		{"1", 1},
		{"1K", 1024},
		{"1M", 1024 * 1024},
		{"1G", 1024 * 1024 * 1024},
		{"1.5K", 1536},
		{"2.5M", int64(2.5 * 1024 * 1024)},
	}

	for _, c := range cases {
		var b model.ByteString
		if err := b.Set(c.input); err != nil {
			t.Fatalf("Set(%q) failed: %v", c.input, err)
		}
		got, err := b.Bytes()
		if err != nil {
			t.Errorf("Bytes() failed for %q: %v", c.input, err)
		}
		if got != c.expected {
			t.Errorf("Bytes() = %d for input %q; want %d", got, c.input, c.expected)
		}
	}
}

func TestByteString_Bytes_Invalid(t *testing.T) {
	var b model.ByteString
	b = "invalid"
	_, err := b.Bytes()
	if err == nil {
		t.Errorf("Bytes() expected error for invalid input but got nil")
	}
}

