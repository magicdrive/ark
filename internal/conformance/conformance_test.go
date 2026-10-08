package conformance_test

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/conformance"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/languages/javascript"
	"github.com/magicdrive/ark/internal/languages/php"
	"github.com/magicdrive/ark/internal/languages/python"
	"github.com/magicdrive/ark/internal/languages/terraform"
	"github.com/magicdrive/ark/internal/languages/typescript"
	"github.com/magicdrive/ark/internal/source"
)

type providerFixture struct {
	name     string
	provider language.Provider
	valid    []conformance.Case
	broken   conformance.Case // a recoverable broken source (good decl after a broken one)
	dynamic  conformance.Case // one call whose callee name is computed at run time (Q7)
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
			broken:  conformance.Case{Name: "broken", File: "broken.go", Source: []byte("package app\nfunc Broken( {\nfunc Good() {}\n")},
			dynamic: conformance.Case{Name: "dynamic", File: "dyn.go", Source: []byte("package app\nfunc run(m map[string]func()) { m[\"k\"]() }\n")},
		},
		{
			name:     "typescript",
			provider: typescript.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "basic.ts", Source: []byte("export function greet() {}\nexport const C = 1;\n")},
				{Name: "class_iface", File: "cls.ts", Source: []byte("import { X } from \"./x\";\nexport interface I { a: number }\nexport class User implements I { a = 0; save(): void {} }\n")},
				{Name: "calls", File: "calls.ts", Source: []byte("function run() { greet(); const u = new User(); u.save(); }\n")},
				{Name: "modules", File: "src/mod.ts", Source: []byte("import D, { A as B, type T } from \"./a\";\nimport * as ns from \"../up\";\nexport { B as C } from \"./b.js\";\nexport * from \"zod\";\nexport * as q from \"../../../escape\";\nexport default class K extends D { constructor(private r: B) { super(); } get v() { return 1 } set v(x) {} static s() { ns.go(); } }\n")},
			},
			broken:  conformance.Case{Name: "broken", File: "broken.ts", Source: []byte("export class B {\n  m( {\n}\nexport function good() {}\n")},
			dynamic: conformance.Case{Name: "dynamic", File: "dyn.ts", Source: []byte("export function run(o: any, k: string) { o[k](); }\n")},
		},
		{
			name:     "tsx",
			provider: typescript.NewTSXProvider(),
			valid: []conformance.Case{
				{Name: "component", File: "c.tsx", Source: []byte("import { useState } from \"react\";\nexport function App() { const [n, setN] = useState(0); return <div onClick={() => setN(n+1)}>{n}</div>; }\n")},
				{Name: "arrow", File: "a.tsx", Source: []byte("export const T = () => <h1>hi</h1>;\n")},
				{Name: "components", File: "src/p.tsx", Source: []byte("import { Card } from \"./card\";\nimport * as UI from \"./ui\";\nexport const P = () => <div><Card /><UI.Button /><span /></div>;\n")},
			},
			broken:  conformance.Case{Name: "broken", File: "broken.tsx", Source: []byte("export function B( {\n return <div>;\n}\nexport function Good() { return <i/>; }\n")},
			dynamic: conformance.Case{Name: "dynamic", File: "dyn.tsx", Source: []byte("export function run(o: any, k: string) { o[k](); }\n")},
		},
		{
			name:     "javascript",
			provider: javascript.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "basic.js", Source: []byte("export function greet() {}\nexport const C = 1;\n")},
				{Name: "class", File: "cls.js", Source: []byte("import { X } from \"./x.js\";\nexport class User { constructor() { this.id = 0; } save() {} }\n")},
				{Name: "calls", File: "calls.js", Source: []byte("function run() { greet(); const u = new User(); u.save(); }\n")},
			},
			broken:  conformance.Case{Name: "broken", File: "broken.js", Source: []byte("export function broken( {\nexport function good() {}\n")},
			dynamic: conformance.Case{Name: "dynamic", File: "dyn.js", Source: []byte("export function run(o, k) { o[k](); }\n")},
		},
		{
			name:     "python",
			provider: python.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "basic.py", Source: []byte("C = 1\ndef greet():\n    return None\n")},
				{Name: "class", File: "cls.py", Source: []byte("import os\nclass User:\n    def __init__(self):\n        self.id = 0\n    def save(self):\n        return os.getpid()\n")},
				{Name: "calls", File: "calls.py", Source: []byte("def run():\n    greet()\n    u = User()\n    u.save()\n")},
			},
			broken:  conformance.Case{Name: "broken", File: "broken.py", Source: []byte("def broken(:\n    pass\ndef good():\n    pass\n")},
			dynamic: conformance.Case{Name: "dynamic", File: "dyn.py", Source: []byte("def run(o, n):\n    getattr(o, n)()\n")},
		},
		{
			name:     "php",
			provider: php.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "basic.php", Source: []byte("<?php\nfunction greet(): void {}\nconst C = 1;\n")},
				{Name: "class", File: "cls.php", Source: []byte("<?php\nnamespace App;\nuse App\\Model\\User;\nclass UserService {\n    private User $user;\n    public function find(int $id): User { return $this->user; }\n}\n")},
				{Name: "relations", File: "rel.php", Source: []byte("<?php\nclass Child extends Base implements Contract {\n    use LogsActivity;\n    public static function make(): self { return new self(); }\n}\n")},
			},
			broken:  conformance.Case{Name: "broken", File: "broken.php", Source: []byte("<?php\nclass Broken { public function m( {\nfunction good() {}\n")},
			dynamic: conformance.Case{Name: "dynamic", File: "dyn.php", Source: []byte("<?php\nfunction run($o, $m) { $o->$m(); }\n")},
		},
		{
			name:     "terraform",
			provider: terraform.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "main.tf", Source: []byte("variable \"region\" {}\nresource \"aws_vpc\" \"main\" { cidr_block = var.cidr }\noutput \"id\" { value = aws_vpc.main.id }\n")},
				{Name: "module", File: "envs/prod/main.tf", Source: []byte("module \"net\" {\n  source = \"../../modules/net\"\n  cidr   = local.cidr\n}\nlocals { cidr = \"10.0.0.0/16\" }\noutput \"vpc\" { value = module.net.vpc_id }\n")},
				{Name: "expressions", File: "x.tf", Source: []byte("resource \"a\" \"b\" {\n  count = 2\n  ids = [for s in data.c.d : s.id if s.ok]\n  t = \"${var.x}-${count.index}\"\n  depends_on = [a.c]\n  dynamic \"ingress\" {\n    for_each = var.rules\n    content { port = ingress.value }\n  }\n}\n")},
				{Name: "tfvars", File: "terraform.tfvars", Source: []byte("region = \"eu-west-1\"\n")},
			},
			broken: conformance.Case{Name: "broken", File: "broken.tf", Source: []byte("resource \"a\" \"broken\" {\n  x = var.y +\n}\nresource \"a\" \"good\" {}\n")},
			// Terraform has no call whose callee is computed at run time
			// (function names are static built-ins); the honest count is 0.
			dynamic: conformance.Case{Name: "dynamic", File: "dyn.tf", Source: []byte("locals { v = local.m[var.k] }\n")},
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
			conformance.CheckBrokenSourceDiagnosed(t, f.provider, f.broken)
		})
	}
}

// TestQualityCandidates is a NON-FAILING audit. It records, per provider, the
// current status of the open quality gaps listed in IMPROVEMENTS.md — the
// authority for their per-provider status. It never calls t.Error/t.Fatal.
func TestQualityCandidates(t *testing.T) {
	for _, f := range fixtures() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			// Q1: broken-source partial extraction (Q2, diagnostics, is part of
			// the contract: CheckBrokenSourceDiagnosed).
			ext, err := f.provider.Extract(context.Background(), source.FileID(f.broken.File), f.broken.Source)
			if err != nil {
				t.Logf("CANDIDATE broken-source: Extract returned error (%v)", err)
			}
			t.Logf("CANDIDATE Q1 partial-extraction: broken source yielded %d symbol(s) [target: recover the trailing valid declaration]", len(ext.Symbols))

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

			// Q7: a call whose callee name is computed at run time is observed
			// (a Dynamic reference), not silently dropped.
			dext, _ := f.provider.Extract(context.Background(), source.FileID(f.dynamic.File), f.dynamic.Source)
			var dynamic int
			for _, r := range dext.References {
				if r.Dynamic {
					dynamic++
				}
			}
			t.Logf("CANDIDATE Q7 dynamic-calls: %d Dynamic reference(s) for one computed-name call [target: 1]", dynamic)
		})
	}
}
