package typescript

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtractTypeScript checks the extraction invariants on arbitrary input:
// no panic, byte ranges inside the source, a valid module-binding contract
// (candidates are clean root-relative paths that never escape the root), no
// ReceiverType without a receiver, and deterministic output.
func FuzzExtractTypeScript(f *testing.F) {
	for _, s := range []string{
		"", "import { a as b } from './x'; export * from '../y'; export { b };",
		"class C extends B implements I { constructor(private r: R) {} get t() { return 1 } set t(v) {} static s() {} #p = 1; }",
		"export default class K {}", "export { default as D } from './d.js'", "import * as ns from '../../../etc/passwd'",
		"const f = <T,>(a: T) => a; f?.(1)", "<div>", "class {", "import {", "export * as from",
		"function f(r: R) { const { r } = o; r.go(); }", "enum E { A = 1 }", "namespace N { export class In {} }",
	} {
		f.Add(s, "src/a.ts")
		f.Add(s, "a.tsx")
	}
	for _, name := range []string{"basic.ts", "broken.ts"} {
		if b, err := os.ReadFile(filepath.Join("testdata", name)); err == nil {
			f.Add(string(b), "src/"+name)
		}
	}
	f.Fuzz(func(t *testing.T, src, file string) {
		p := NewProvider()
		if filepath.Ext(file) == ".tsx" {
			p = NewTSXProvider()
		}
		ext, err := p.Extract(context.Background(), source.FileID(file), []byte(src))
		if err != nil {
			t.Fatalf("Extract error: %v", err)
		}
		n := uint32(len(src))
		for _, s := range ext.Symbols {
			if s.StartByte > s.EndByte || s.EndByte > n || s.Name == "" || s.Qualified == "" {
				t.Fatalf("bad symbol %+v (len=%d)", s, n)
			}
		}
		for _, r := range ext.References {
			if r.Name == "" || r.Kind == "" {
				t.Fatalf("bad reference %+v", r)
			}
			if r.ReceiverType != "" && r.ReceiverExpr == "" {
				t.Fatalf("ReceiverType without a receiver: %+v", r)
			}
		}
		if errs := language.ValidateModuleBindings(ext); len(errs) > 0 {
			t.Fatalf("module binding contract violated: %v", errs)
		}
		again, _ := p.Extract(context.Background(), source.FileID(file), []byte(src))
		if !reflect.DeepEqual(ext, again) {
			t.Fatal("non-deterministic extraction")
		}
	})
}

// FuzzModuleSpec checks path normalisation: candidates are always clean,
// slash-separated, root-relative and sorted, whatever the specifier.
func FuzzModuleSpec(f *testing.F) {
	for _, s := range []string{
		"./a", "../a", "../../../x", "./a/../../b", ".", "..", "./", "a/b", "@/x", "/abs", "./a.ts", "./a.js",
		"./a b", "./\x00", "./a\\b", ".//a", "./a/./b", "node:fs",
	} {
		f.Add("src/app/a.ts", s)
		f.Add("a.ts", s)
	}
	f.Fuzz(func(t *testing.T, importer, spec string) {
		m := moduleSpec(source.FileID(importer), spec)
		if m.Specifier != spec {
			t.Fatalf("specifier not preserved: %q vs %q", m.Specifier, spec)
		}
		ex := language.Extraction{Bindings: []language.BindingDraft{{
			Local: "x", Kind: language.BindingNamespace, Module: m, Location: source.Location{File: "f"},
		}}}
		if spec == "" {
			return // an empty specifier is rejected earlier by the extractor
		}
		if errs := language.ValidateModuleBindings(ex); len(errs) > 0 {
			t.Fatalf("importer=%q spec=%q: %v (candidates %v)", importer, spec, errs, fmt.Sprint(m.Candidates))
		}
	})
}

// FuzzTypeArgParser: the recovery's type grammar terminates on any input,
// never reads past it and reports references inside what it consumed.
func FuzzTypeArgParser(f *testing.F) {
	for _, s := range []string{"<A>", "<a.b<C[]>, 'x', -1>", "<{ a: A }>", "</* x", "<\xff>", "<\"\\x4\">"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p := &typeArgParser{src: []byte(s)}
		ok := p.typeArgs()
		if p.pos < 0 || p.pos > len(s) {
			t.Fatalf("position %d outside %d bytes", p.pos, len(s))
		}
		for _, r := range p.refs {
			if !ok {
				break
			}
			if int(r.start) > p.pos || int(r.end) > p.pos || r.start > r.end || !strings.HasSuffix(s[r.start:r.end], r.name) {
				t.Fatalf("reference %+v outside the consumed text %q", r, s[:p.pos])
			}
		}
	})
}
