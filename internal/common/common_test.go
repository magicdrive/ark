package common_test

import (
	"os"
	"testing"

	"github.com/magicdrive/ark/internal/common"
)

func TestMergeAllowFileList(t *testing.T) {
	a := map[string]bool{"foo": true, "bar": false}
	b := map[string]bool{"bar": true, "baz": true}
	got := common.MergeAllowFileList(a, b)
	want := map[string]bool{"foo": true, "bar": true, "baz": true}

	if len(got) != len(want) {
		t.Fatalf("wrong length: got %d, want %d", len(got), len(want))
	}
	for k := range want {
		if !got[k] {
			t.Errorf("missing or false: %q", k)
		}
	}
}

func TestCommaSeparated2StringList(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"foo", []string{"foo"}},
		{"foo,bar", []string{"foo", "bar"}},
		{"foo, bar,foo", []string{"foo", "bar"}},
		{"   foo  ,  bar  ", []string{"foo", "bar"}},
		{"foo,,bar", []string{"foo", "bar"}},
	}
	for _, tt := range tests {
		got := common.CommaSeparated2StringList(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("input %q: got len=%d, want %d", tt.in, len(got), len(tt.want))
			continue
		}
		for i, v := range tt.want {
			if got[i] != v {
				t.Errorf("input %q: got[%d]=%q, want %q", tt.in, i, got[i], v)
			}
		}
	}
}

func TestTrimDotSlash(t *testing.T) {
	tests := []struct{ in, want string }{
		{"./foo/bar", "foo/bar"},
		{"foo/bar", "foo/bar"},
		{"././baz", "./baz"},
		{"", ""},
	}
	for _, tt := range tests {
		got := common.TrimDotSlash(tt.in)
		if got != tt.want {
			t.Errorf("TrimDotSlash(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGetCurrentDir(t *testing.T) {
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd failed: %v", err)
	}
	got := common.GetCurrentDir()
	if got != want {
		t.Errorf("GetCurrentDir = %q, want %q", got, want)
	}
}

