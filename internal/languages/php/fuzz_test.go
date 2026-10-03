package php

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtractPHP verifies the PHP provider never panics, never emits
// out-of-bounds byte ranges, and never produces invalid locations on arbitrary
// input. Valid PHP seeds let the fuzzer explore structured mutations
// (deletion/insertion/truncation) of real source.
func FuzzExtractPHP(f *testing.F) {
	seeds := []string{
		"",
		"<?php",
		"<?php namespace App; class User {}",
		"<?php\nnamespace App\\Service;\nuse App\\Model\\User;\nclass S extends B implements I { use T; public function f(User $u): ?User { return $u; } }\n",
		"<?php enum Status: string { case A = 'a'; public function x(): void {} }",
		"<?php trait T { protected int $n; public function m() { $this->n(); } }",
		"<?php function f(A|B&C $x, ...$rest): never {}",
		"<?php $x = new $cls(); $o->$m(); User::{$k}();",
		"<?php class Broken { public function m( {",
		"not php at all",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	p := NewProvider()
	f.Fuzz(func(t *testing.T, src []byte) {
		ext, err := p.Extract(context.Background(), source.FileID("fuzz.php"), src)
		if err != nil {
			return // honest error is fine; must not panic
		}
		n := uint32(len(src))
		for _, s := range ext.Symbols {
			if s.Name == "" || s.Qualified == "" {
				t.Fatalf("empty name/qualified: %+v", s)
			}
			if s.StartByte > s.EndByte || s.EndByte > n {
				t.Fatalf("symbol out-of-bounds range [%d,%d] len=%d: %q", s.StartByte, s.EndByte, n, s.Name)
			}
			if s.Location.Range.Start.Line == 0 {
				t.Fatalf("symbol has 0 (non-1-based) line: %q", s.Name)
			}
		}
		for _, r := range ext.References {
			if r.Name == "" {
				t.Fatalf("empty reference name")
			}
		}
	})
}
