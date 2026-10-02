package golang

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtract verifies that the Go language provider never panics on arbitrary input.
//
// Property: Extract must return (result, error) for any byte sequence —
// never panic, never hang on inputs that terminate.
func FuzzExtract(f *testing.F) {
	// Seed: valid Go, empty, broken syntax, unicode, binary.
	f.Add([]byte("package main\nfunc main() {}\n"))
	f.Add([]byte(""))
	f.Add([]byte("package main\nfunc Broken( {"))
	f.Add([]byte("package p\ntype T struct{}\nfunc (t T) Method() {}\n"))
	f.Add([]byte("not go source at all"))
	f.Add([]byte{0xff, 0xfe, 0x00})
	f.Add([]byte("package p\nfunc F() {\n// comment\n}\n"))

	p := NewProvider()

	f.Fuzz(func(t *testing.T, src []byte) {
		// Must never panic.
		_, _ = p.Extract(context.Background(), source.FileID("fuzz.go"), src)
	})
}
