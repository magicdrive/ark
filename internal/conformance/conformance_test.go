package conformance_test

import (
	"context"
	"testing"

	"github.com/magicdrive/ark/internal/conformance"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/css"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/languages/html"
	"github.com/magicdrive/ark/internal/languages/java"
	"github.com/magicdrive/ark/internal/languages/javascript"
	"github.com/magicdrive/ark/internal/languages/php"
	"github.com/magicdrive/ark/internal/languages/python"
	"github.com/magicdrive/ark/internal/languages/rust"
	"github.com/magicdrive/ark/internal/languages/terraform"
	"github.com/magicdrive/ark/internal/languages/typescript"
	"github.com/magicdrive/ark/internal/languages/zig"
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
		{
			name:     "rust",
			provider: rust.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "src/lib.rs", Source: []byte("pub mod a;\npub fn greet() {}\npub const C: i32 = 1;\npub static V: i32 = 2;\n")},
				{Name: "types_impls", File: "src/model.rs", Source: []byte("use std::fmt;\npub struct User { pub id: u64 }\npub enum Role { Admin, Member(u32) }\npub trait Named { fn name(&self) -> String; }\nimpl User { pub fn new(id: u64) -> Self { Self { id } } }\nimpl Named for User { fn name(&self) -> String { String::new() } }\nimpl fmt::Display for User { fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result { write!(f, \"{}\", self.id) } }\n")},
				{Name: "calls", File: "src/run.rs", Source: []byte("use crate::model::User;\nmacro_rules! twice { ($e:expr) => { $e; $e } }\nfn run() { let u = User::new(1); u.name(); twice!(crate::greet()); super::greet(); }\n")},
				{Name: "modules", File: "crates/util/src/a/mod.rs", Source: []byte("mod b { pub(super) fn f() {} }\nuse self::b::f as g;\nuse super::*;\n#[cfg(test)]\nmod tests { use super::*; #[test] fn t() { g(); } }\n")},
			},
			broken:  conformance.Case{Name: "broken", File: "src/broken.rs", Source: []byte("pub fn broken( {\n}\npub fn good() {}\n")},
			dynamic: conformance.Case{Name: "dynamic", File: "src/dyn.rs", Source: []byte("pub fn run(m: &[fn()], k: usize) { m[k](); }\n")},
		},
		{
			name:     "java",
			provider: java.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "src/main/java/p/A.java", Source: []byte("package p;\n\nimport java.util.List;\n\npublic class A {\n    private final List<String> names;\n    public A(List<String> names) { this.names = names; }\n    public int size() { return names.size(); }\n}\n")},
				{Name: "types", File: "src/main/java/p/T.java", Source: []byte("package p;\ninterface I { default String n() { return \"\"; } }\nenum E implements I { A, B { public String n() { return \"b\"; } } }\nrecord R(int x) implements I { R { } }\n@interface Ann { String value() default \"\"; }\n")},
				{Name: "calls", File: "src/main/java/p/C.java", Source: []byte("package p;\nimport static java.lang.Math.max;\nclass C extends A {\n    C() { super(null); }\n    int m(Object o) {\n        Runnable r = () -> size();\n        if (o instanceof R rec) { return rec.x(); }\n        new Object() { void x() { m(null); } };\n        return max(size(), E.A.ordinal());\n    }\n}\n")},
			},
			broken: conformance.Case{Name: "broken", File: "src/main/java/p/B.java", Source: []byte("package p;\nclass B { void broken( { } }\nclass Good { void ok() {} }\n")},
			// Java has no call syntax whose callee is computed at run time
			// (reflection is an ordinary call of Method.invoke); the honest
			// count is 0.
			dynamic: conformance.Case{Name: "dynamic", File: "src/main/java/p/D.java", Source: []byte("package p;\nclass D { Object run(java.lang.reflect.Method m) throws Exception { return m.invoke(null); } }\n")},
		},
		{
			name:     "zig",
			provider: zig.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "src/main.zig", Source: []byte("const std = @import(\"std\");\nconst math = @import(\"math.zig\");\npub fn main() void {\n    std.debug.print(\"{}\\n\", .{math.add(1, 2)});\n}\n")},
				{Name: "containers", File: "src/model.zig", Source: []byte("pub const User = struct {\n    const Self = @This();\n    id: u64,\n    role: Role = .member,\n    pub fn init(id: u64) Self { return .{ .id = id }; }\n    pub fn name(self: *const Self) []const u8 { _ = self; return \"\"; }\n};\npub const Role = enum { admin, member };\npub const Shape = union(enum) { circle: f32, square: f32 };\nconst Err = error{ NotFound, Denied };\nconst Handle = opaque {};\n")},
				{Name: "calls", File: "src/run.zig", Source: []byte("const model = @import(\"model.zig\");\nfn List(comptime T: type) type {\n    return struct { items: []T, pub fn len(self: @This()) usize { return self.items.len; } };\n}\npub fn run() !void {\n    var u = model.User.init(1);\n    _ = u.name();\n    const l: List(u8) = .{ .items = &.{} };\n    _ = l.len();\n    for (0..3) |i| { if (i > 1) break; }\n    const r = blk: { break :blk 1; };\n    _ = r;\n}\ntest \"run\" { try run(); }\n")},
			},
			broken:  conformance.Case{Name: "broken", File: "src/broken.zig", Source: []byte("pub fn broken( {\n}\npub fn good() void {}\n")},
			dynamic: conformance.Case{Name: "dynamic", File: "src/dyn.zig", Source: []byte("pub fn run(m: []const *const fn () void, k: usize) void { m[k](); }\n")},
		},
		{
			name:     "html",
			provider: html.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "index.html", Source: []byte("<!doctype html>\n<title>T</title>\n<h1 id=top-heading>Hi</h1>\n<a href=\"#top-heading\">up</a>\n")},
				{Name: "form", File: "docs/form.html", Source: []byte("<form id=f><label for=n>N</label><input id=n aria-describedby=\"h1 h2\" list=l></form><p id=h1></p><p id=h2></p><datalist id=l></datalist><button form=f>go</button>\n")},
				{Name: "links_svg", File: "docs/links.htm", Source: []byte("<a href=\"../index.html#top-heading\">i</a><iframe src=form.html></iframe><svg><defs><linearGradient id=g></linearGradient></defs><rect fill=\"url(#g)\"/><use href=\"#g\"/></svg><template><p id=t></p></template>\n")},
			},
			// A tag cut off by the end of the file is dropped (the standard's
			// recovery); the declaration before it is kept.
			broken:  conformance.Case{Name: "broken", File: "broken.html", Source: []byte("<p id=good>x</p>\n<div id=\"broken")},
			dynamic: conformance.Case{Name: "dynamic", File: "dyn.html", Source: []byte("<p id=a onclick=\"go()\"><label for=a>x</label>\n")},
		},
		{
			name:     "css",
			provider: css.NewProvider(),
			valid: []conformance.Case{
				{Name: "basic", File: "main.css", Source: []byte("@import \"base.css\";\n@keyframes spin { to { transform: rotate(1turn) } }\n.a { animation: 1s spin; color: var(--x, red); }\n")},
				{Name: "fonts_props", File: "styles/fonts.css", Source: []byte("@font-face { font-family: \"Inter\"; src: url(inter.woff2); }\n@property --a { syntax: '*'; inherits: false; }\n:root { --a: 1; }\n.t { font-family: Inter, sans-serif; }\n")},
				{Name: "modules", File: "c/card.module.css", Source: []byte(".card { composes: base from \"./base.module.css\"; }\n.title { composes: card; &:hover { color: red } }\n:global(.dark) .card { color: white }\n")},
			},
			// A url() broken by a space drops its declaration; the rule after it stays.
			broken:  conformance.Case{Name: "broken", File: "broken.css", Source: []byte(".a { background: url(bad url); }\n@keyframes good { }\n")},
			dynamic: conformance.Case{Name: "dynamic", File: "dyn.css", Source: []byte(".a { animation-name: var(--name); }\n")},
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
