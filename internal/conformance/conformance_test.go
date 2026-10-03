package conformance_test

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/conformance"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/languages/javascript"
	"github.com/magicdrive/ark/internal/languages/python"
	"github.com/magicdrive/ark/internal/languages/typescript"
	"github.com/magicdrive/ark/internal/source"
)

type providerFixture struct {
	name     string
	provider language.Provider
	valid    []conformance.Case
	broken   conformance.Case // a recoverable broken source (good decl after a broken one)
}

func fixtures() []providerFixture {
	return []providerFixture{
		{
			name:     "go",
			provider: golang.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "basic.go", Source: []byte("package app\nfunc Greet() {}\nconst C = 1\nvar V = 2\n")},
				{Name: "types_methods", File: "types.go", Source: []byte("package app\nimport \"fmt\"\ntype User struct{ ID int }\nfunc (u *User) Save() error { fmt.Println(u); return nil }\n")},
				{Name: "calls", File: "calls.go", Source: []byte("package app\nfunc run() { Greet(); u := User{}; u.Save() }\n")},
			},
			broken: conformance.Case{Name: "broken", File: "broken.go", Source: []byte("package app\nfunc Broken( {\nfunc Good() {}\n")},
		},
		{
			name:     "typescript",
			provider: typescript.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "basic.ts", Source: []byte("export function greet() {}\nexport const C = 1;\n")},
				{Name: "class_iface", File: "cls.ts", Source: []byte("import { X } from \"./x\";\nexport interface I { a: number }\nexport class User implements I { a = 0; save(): void {} }\n")},
				{Name: "calls", File: "calls.ts", Source: []byte("function run() { greet(); const u = new User(); u.save(); }\n")},
			},
			broken: conformance.Case{Name: "broken", File: "broken.ts", Source: []byte("export class B {\n  m( {\n}\nexport function good() {}\n")},
		},
		{
			name:     "tsx",
			provider: typescript.NewTSXProvider(),
			valid: []conformance.Case{
				{Name: "component", File: "c.tsx", Source: []byte("import { useState } from \"react\";\nexport function App() { const [n, setN] = useState(0); return <div onClick={() => setN(n+1)}>{n}</div>; }\n")},
				{Name: "arrow", File: "a.tsx", Source: []byte("export const T = () => <h1>hi</h1>;\n")},
			},
			broken: conformance.Case{Name: "broken", File: "broken.tsx", Source: []byte("export function B( {\n return <div>;\n}\nexport function Good() { return <i/>; }\n")},
		},
		{
			name:     "javascript",
			provider: javascript.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "basic.js", Source: []byte("export function greet() {}\nexport const C = 1;\n")},
				{Name: "class", File: "cls.js", Source: []byte("import { X } from \"./x.js\";\nexport class User { constructor() { this.id = 0; } save() {} }\n")},
				{Name: "calls", File: "calls.js", Source: []byte("function run() { greet(); const u = new User(); u.save(); }\n")},
			},
			broken: conformance.Case{Name: "broken", File: "broken.js", Source: []byte("export function broken( {\nexport function good() {}\n")},
		},
		{
			name:     "python",
			provider: python.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "basic.py", Source: []byte("C = 1\ndef greet():\n    return None\n")},
				{Name: "class", File: "cls.py", Source: []byte("import os\nclass User:\n    def __init__(self):\n        self.id = 0\n    def save(self):\n        return os.getpid()\n")},
				{Name: "calls", File: "calls.py", Source: []byte("def run():\n    greet()\n    u = User()\n    u.save()\n")},
			},
			broken: conformance.Case{Name: "broken", File: "broken.py", Source: []byte("def broken(:\n    pass\ndef good():\n    pass\n")},
		},
	}
}

// TestProviderContract applies the required (currently-satisfied) contract to
// every provider. These invariants MUST hold; a failure is a real regression.
func TestProviderContract(t *testing.T) {
	for _, f := range fixtures() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			conformance.RunContract(t, f.provider, f.valid)
		})
	}
}

// TestQualityCandidates is a NON-FAILING audit. It records, per provider, the
// current status of aspirational quality requirements that PR 1 intentionally
// does NOT fix (see IMPROVEMENTS.md). It never calls t.Error/t.Fatal.
func TestQualityCandidates(t *testing.T) {
	for _, f := range fixtures() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			// Q1/Q2: broken-source partial extraction & diagnostics.
			ext, err := f.provider.Extract(context.Background(), source.FileID(f.broken.File), f.broken.Source)
			if err != nil {
				t.Logf("CANDIDATE broken-source: Extract returned error (%v)", err)
			}
			t.Logf("CANDIDATE Q1 partial-extraction: broken source yielded %d symbol(s) [target: recover the trailing valid declaration]", len(ext.Symbols))
			t.Logf("CANDIDATE Q2 diagnostics: broken source yielded %d diagnostic(s) [target: >=1 diagnostic]", len(ext.Diagnostics))

			// Q3/Q4: nested-member extraction over the valid corpus. Count
			// symbols that look like members of a container (method receiver
			// set, or Parent set) and how many populate SymbolDraft.Parent.
			var totalSyms, memberSyms, parentSet int
			for _, c := range f.valid {
				vext, _ := f.provider.Extract(context.Background(), source.FileID(c.File), c.Source)
				for _, s := range vext.Symbols {
					totalSyms++
					if s.Receiver != "" || s.Parent != "" {
						memberSyms++
					}
					if s.Parent != "" {
						parentSet++
					}
				}
			}
			t.Logf("CANDIDATE Q3 member-symbols: %d/%d valid-corpus symbol(s) are container members (receiver/parent) [target: class/object methods extracted as symbols]", memberSyms, totalSyms)
			t.Logf("CANDIDATE Q4 parent-field: %d/%d valid-corpus symbol(s) populate SymbolDraft.Parent", parentSet, totalSyms)
		})
	}
}
