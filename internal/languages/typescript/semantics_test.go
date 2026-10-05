package typescript

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func extractAs(t *testing.T, p *Provider, file, src string) language.Extraction {
	t.Helper()
	ext, err := p.Extract(context.Background(), source.FileID(file), []byte(src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", file, err)
	}
	if errs := language.ValidateModuleBindings(ext); len(errs) > 0 {
		t.Fatalf("module binding contract violated: %v", errs)
	}
	return ext
}

func extractTS(t *testing.T, src string) language.Extraction {
	return extractAs(t, NewProvider(), "src/a.ts", src)
}

func extractTSX(t *testing.T, src string) language.Extraction {
	return extractAs(t, NewTSXProvider(), "src/a.tsx", src)
}

func symsByQualified(ext language.Extraction) map[string]language.SymbolDraft {
	m := map[string]language.SymbolDraft{}
	for _, s := range ext.Symbols {
		m[s.Qualified+"|"+string(s.Kind)] = s
	}
	return m
}

func findSym(t *testing.T, ext language.Extraction, qualified string, kind symbol.SymbolKind) language.SymbolDraft {
	t.Helper()
	s, ok := symsByQualified(ext)[qualified+"|"+string(kind)]
	if !ok {
		var have []string
		for _, s := range ext.Symbols {
			have = append(have, s.Qualified+"("+string(s.Kind)+")")
		}
		t.Fatalf("symbol %s (%s) not extracted; have %v", qualified, kind, have)
	}
	return s
}

// --- TS-1: members + containment ---------------------------------------------

const memberSrc = `
export abstract class Base { abstract name(): string; protected log(m: string) {} }
export class UserService extends Base {
  private count = 0;
  #hidden = 1;
  static registry = new Map();
  constructor(private readonly repo: Repo, public name2: string, plain: number) { super(); }
  name(): string { return "x"; }
  create(n: string): User { return new User(n); }
  static from(r: Repo): UserService { return new UserService(r); }
  get total(): number { return this.count; }
  set total(v: number) { this.count = v; }
  handle = (a: Repo) => a.save();
  #secret() {}
  static async *gen() {}
  over(a: string): void;
  over(a: number): void;
  over(a: any) {}
}
export class OrderService { save() {} create() {} }
export class UserRepo { save() {} }
export interface Service { name(): string; readonly id: string; run?: () => void }
`

func TestMembers_SymbolsAndContainment(t *testing.T) {
	ext := extractTS(t, memberSrc)
	cases := []struct {
		qualified string
		kind      symbol.SymbolKind
		exported  bool
	}{
		{"Base", symbol.KindClass, true},
		{"Base.name", symbol.KindMethod, true},
		{"Base.log", symbol.KindMethod, false}, // protected
		{"UserService", symbol.KindClass, true},
		{"UserService.count", symbol.KindProperty, false},
		{"UserService.#hidden", symbol.KindProperty, false},
		{"UserService.registry", symbol.KindProperty, true},
		{"UserService.constructor", symbol.KindConstructor, true},
		{"UserService.repo", symbol.KindProperty, false}, // private parameter property
		{"UserService.name2", symbol.KindProperty, true}, // public parameter property
		{"UserService.name", symbol.KindMethod, true},
		{"UserService.create", symbol.KindMethod, true},
		{"UserService.from", symbol.KindMethod, true}, // static
		{"UserService.total", symbol.KindProperty, true},
		{"UserService.handle", symbol.KindMethod, true}, // arrow-function field
		{"UserService.#secret", symbol.KindMethod, false},
		{"UserService.gen", symbol.KindMethod, true}, // static async generator
		{"UserService.over", symbol.KindMethod, true},
		{"OrderService.save", symbol.KindMethod, true},
		{"UserRepo.save", symbol.KindMethod, true},
		{"Service", symbol.KindInterface, true},
		{"Service.name", symbol.KindMethod, true},
		{"Service.id", symbol.KindProperty, true},
		{"Service.run", symbol.KindProperty, true},
	}
	for _, c := range cases {
		s := findSym(t, ext, c.qualified, c.kind)
		if s.Exported != c.exported {
			t.Errorf("%s Exported = %v, want %v", c.qualified, s.Exported, c.exported)
		}
		if i := strings.LastIndex(c.qualified, "."); i > 0 {
			class := c.qualified[:i]
			if s.Parent != class || s.Receiver != class {
				t.Errorf("%s Parent=%q Receiver=%q, want %q", c.qualified, s.Parent, s.Receiver, class)
			}
			if s.Name != c.qualified[i+1:] {
				t.Errorf("%s Name = %q", c.qualified, s.Name)
			}
		} else if s.Parent != "" || s.Receiver != "" {
			t.Errorf("%s top level must have no Parent/Receiver", c.qualified)
		}
	}
	// A plain (non-property) constructor parameter is not a member.
	if _, ok := symsByQualified(ext)["UserService.plain|property"]; ok {
		t.Error("plain constructor parameter must not become a property")
	}
}

// Duplicate member names across unrelated classes are distinct symbols.
func TestMembers_DuplicateNamesAcrossClassesAreDistinct(t *testing.T) {
	ext := extractTS(t, memberSrc)
	a := findSym(t, ext, "OrderService.save", symbol.KindMethod)
	b := findSym(t, ext, "UserRepo.save", symbol.KindMethod)
	idA := symbol.NewSymbolID("typescript", "src/a.ts", a.Kind, a.Qualified)
	idB := symbol.NewSymbolID("typescript", "src/a.ts", b.Kind, b.Qualified)
	if idA == idB {
		t.Fatal("same-name members of different classes collide in SymbolID")
	}
	n := 0
	for _, s := range ext.Symbols {
		if s.Qualified == "UserService.name" || s.Qualified == "UserService.over" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("overloads/duplicates must collapse to one symbol per identity; got %d", n)
	}
}

// Get/set pair = ONE KindProperty symbol spanning both when adjacent; first
// accessor is the representative span when separated.
func TestMembers_GetterSetter(t *testing.T) {
	adjacent := `class C { get total(): number { return 1 }
  set total(v: number) {} }`
	ext := extractTS(t, adjacent)
	n := 0
	var span language.SymbolDraft
	for _, s := range ext.Symbols {
		if s.Qualified == "C.total" {
			n++
			span = s
		}
	}
	if n != 1 || span.Kind != symbol.KindProperty {
		t.Fatalf("want exactly one C.total property, got %d (%s)", n, span.Kind)
	}
	if span.Location.Range.End.Line != 2 {
		t.Errorf("adjacent accessors must span both: end line %d, want 2", span.Location.Range.End.Line)
	}

	separated := `class C { get total(): number { return 1 }
  other() {}
  set total(v: number) {} }`
	ext = extractTS(t, separated)
	n = 0
	for _, s := range ext.Symbols {
		if s.Qualified == "C.total" {
			n++
			span = s
		}
	}
	if n != 1 || span.Location.Range.End.Line != 1 {
		t.Errorf("separated accessors: want 1 symbol spanning the first accessor only, got n=%d end=%d", n, span.Location.Range.End.Line)
	}
}

func TestMembers_TopLevelKinds(t *testing.T) {
	ext := extractTS(t, `
export function f() {}
export const K = 1;
let v = 2;
export const arrow = (a: number) => a;
export const fe = function () {};
const { a, b } = obj;
export type T = string;
export enum E { A }
declare function ambient(): void;
function over(a: string): void;
function over(a: any) {}
`)
	want := map[string]symbol.SymbolKind{
		"f": symbol.KindFunction, "K": symbol.KindConstant, "v": symbol.KindVariable,
		"arrow": symbol.KindFunction, "fe": symbol.KindFunction,
		"T": symbol.KindTypeAlias, "E": symbol.KindEnum, "over": symbol.KindFunction,
	}
	got := map[string]symbol.SymbolKind{}
	for _, s := range ext.Symbols {
		got[s.Qualified] = s.Kind
	}
	for q, k := range want {
		if got[q] != k {
			t.Errorf("%s kind = %q, want %q", q, got[q], k)
		}
	}
	for _, no := range []string{"a", "b", "ambient"} {
		if _, ok := got[no]; ok {
			t.Errorf("%s must not be a symbol (destructuring / ambient)", no)
		}
	}
}

// --- TS-2: references ---------------------------------------------------------

type refRow struct {
	name, kind, container, recv, recvType string
}

func refRows(ext language.Extraction) []refRow {
	var out []refRow
	for _, r := range ext.References {
		out = append(out, refRow{r.Name, r.Kind, r.Container, r.ReceiverExpr, r.ReceiverType})
	}
	return out
}

func hasRef(rows []refRow, want refRow) bool {
	for _, r := range rows {
		if r == want {
			return true
		}
	}
	return false
}

func TestRefs_ContainersAndReceivers(t *testing.T) {
	ext := extractTS(t, `
import { User as DomainUser, Repo } from "./m";
import * as ns from "./n";
class Svc extends Base implements Contract, Other<DomainUser> {
  private repo: Repo = new Repo();
  create(r: Repo): User {
    this.helper();
    this.repo.save();
    r.find();
    User.create();
    ns.make();
    new ns.Thing();
    const x = new Local();
    x.run();
    unknown.run();
    return new User();
  }
}
interface Child extends Parent {}
function top() { helper(); const m = new Map<string, Foo>(); }
`)
	rows := refRows(ext)
	want := []refRow{
		{"Base", "inheritance", "Svc", "", ""},
		{"Contract", "implementation", "Svc", "", ""},
		{"Other", "implementation", "Svc", "", ""},
		{"DomainUser", "type_use", "Svc", "", ""}, // generic type argument
		{"Repo", "construction", "Svc.repo", "", ""},
		{"Repo", "type_use", "Svc.create", "", ""},
		{"User", "type_use", "Svc.create", "", ""},
		{"helper", "call", "Svc.create", "this", "Svc"},
		{"save", "call", "Svc.create", "this.repo", "Repo"},
		{"find", "call", "Svc.create", "r", "Repo"},
		{"create", "call", "Svc.create", "User", ""}, // static: verbatim receiver, no type
		{"make", "call", "Svc.create", "ns", ""},
		{"Thing", "construction", "Svc.create", "ns", ""},
		{"Local", "construction", "Svc.create", "", ""},
		{"run", "call", "Svc.create", "x", "Local"},  // const x = new Local()
		{"run", "call", "Svc.create", "unknown", ""}, // no evidence
		{"User", "construction", "Svc.create", "", ""},
		{"Parent", "inheritance", "Child", "", ""},
		{"helper", "call", "top", "", ""},
		{"Map", "construction", "top", "", ""},
		{"Foo", "type_use", "top", "", ""},
	}
	for _, w := range want {
		if !hasRef(rows, w) {
			t.Errorf("missing reference %+v\nhave:\n%s", w, dumpRows(rows))
		}
	}
}

func dumpRows(rows []refRow) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "  %+v\n", r)
	}
	return b.String()
}

// Unproven receivers must carry NO ReceiverType.
func TestRefs_ReceiverTypeUnproven(t *testing.T) {
	cases := map[string]string{
		"untyped param":    `function f(r) { r.go(); }`,
		"union param":      `function f(r: A | B) { r.go(); }`,
		"qualified type":   `function f(r: ns.A) { r.go(); }`,
		"builtin param":    `function f(r: string) { r.go(); }`,
		"type parameter":   `function f<T>(r: T) { r.go(); }`,
		"class tparam":     `class C<T> { m(r: T) { r.go(); } }`,
		"let new":          `function f() { let r = new A(); r.go(); }`,
		"call init":        `function f() { const r = make(); r.go(); }`,
		"shadow in inner":  `function f(r: A) { if (x) { const r = other(); } r.go(); }`,
		"catch shadow":     `function f(r: A) { try {} catch (r) { r.go(); } }`,
		"destructure":      `function f(r: A) { const { r } = o; r.go(); }`,
		"for-of shadow":    `function f(r: A) { for (const r of xs) { r.go(); } }`,
		"use before decl":  `function f() { r.go(); const r = new A(); }`,
		"this in function": `class C { m() { const g = function () { this.go(); }; } }`,
		"this in object":   `class C { m() { const o = { n() { this.go(); } }; } }`,
		"deep chain":       `class C { r: A = new A(); m() { this.r.x.go(); } }`,
		"static field":     `class C { static r: A = new A(); m() { this.r.go(); } }`,
		"untyped field":    `class C { r = make(); m() { this.r.go(); } }`,
		"this outside":     `function f() { this.go(); }`,
		"cast":             `function f(r: any) { (r as A).go(); }`,
	}
	for name, src := range cases {
		for _, r := range refRows(extractTS(t, src)) {
			if r.name == "go" && r.recvType != "" {
				t.Errorf("%s: got ReceiverType %q, want none", name, r.recvType)
			}
		}
	}
}

// A closure parameter that shadows an outer variable poisons the OUTER
// evidence only; the closure's own annotated parameter is still proven.
func TestRefs_ReceiverTypeShadowByClosure(t *testing.T) {
	ext := extractTS(t, `function f(r: A) { g((r: B) => r.go()); r.go(); }`)
	var types []string
	for _, r := range refRows(ext) {
		if r.name == "go" {
			types = append(types, r.recvType)
		}
	}
	if len(types) != 2 || types[0] != "B" || types[1] != "" {
		t.Errorf("go ReceiverTypes = %q, want [B \"\"] (inner proven, outer poisoned)", types)
	}
}

// Proven receivers outside the main table.
func TestRefs_ReceiverTypeProven(t *testing.T) {
	cases := map[string]string{
		"param":            `function f(r: A) { r.go(); }`,
		"optional param":   `function f(r?: A) { r.go(); }`,
		"generic type":     `function f(r: A<string>) { r.go(); }`,
		"annotated const":  `function f() { const r: A = make(); r.go(); }`,
		"annotated let":    `function f() { let r: A = make(); r.go(); }`,
		"const new":        `function f() { const r = new A(); r.go(); }`,
		"closure inherits": `function f(r: A) { g(() => r.go()); }`,
		"arrow keeps this": `class A { m() { const g = () => this.go(); } }`,
		"param property":   `class C { constructor(private r: A) {} m() { this.r.go(); } }`,
		"field annotation": `class C { r: A; m() { this.r.go(); } }`,
		"field new":        `class C { r = new A(); m() { this.r.go(); } }`,
		"arrow function":   `const f = (r: A) => r.go();`,
		"non-null":         `function f(r: A) { r!.go(); }`,
		"optional chain":   `function f(r: A) { r?.go(); }`,
	}
	for name, src := range cases {
		found := false
		for _, r := range refRows(extractTS(t, src)) {
			if r.name == "go" {
				found = true
				if r.recvType != "A" {
					t.Errorf("%s: ReceiverType = %q, want A", name, r.recvType)
				}
			}
		}
		if !found {
			t.Errorf("%s: call not extracted", name)
		}
	}
}

// Computed access, super and dynamic callees are not guessed at.
func TestRefs_NoFabrication(t *testing.T) {
	ext := extractTS(t, `
class C extends B {
  m(a: any) {
    a["x"]();
    super.go();
    (a.b)();
    a.b.c;
    new (getCtor())();
    this[k]();
  }
}`)
	for _, r := range refRows(ext) {
		// `getCtor()` is a genuine nested call; nothing else may be emitted.
		if (r.kind == "call" && r.name != "getCtor") || r.kind == "construction" {
			t.Errorf("unexpected reference %+v", r)
		}
	}
}

func TestRefs_TypeParametersAreNotReferences(t *testing.T) {
	ext := extractTS(t, `
function f<T extends Base, U = Def>(a: T, b: U): T { return a; }
class C<K> { m(k: K): K { return k; } }
interface I<V> { get(): V }
type Alias<W> = W[];
`)
	for _, r := range refRows(ext) {
		switch r.name {
		case "T", "U", "K", "V", "W":
			t.Errorf("type parameter %q emitted as a reference: %+v", r.name, r)
		}
	}
	rows := refRows(ext)
	for _, want := range []string{"Base", "Def"} {
		found := false
		for _, r := range rows {
			if r.name == want && r.kind == "type_use" {
				found = true
			}
		}
		if !found {
			t.Errorf("constraint/default %s must still be a type_use", want)
		}
	}
}

// --- TS-3 / TS-4: bindings, exports, module candidates --------------------------

func bindingRows(ext language.Extraction) []string {
	var out []string
	for _, b := range ext.Bindings {
		out = append(out, fmt.Sprintf("%s=%s:%s:%s typeOnly=%v", b.Local, b.Kind, b.Imported, b.Module.Specifier, b.TypeOnly))
	}
	return out
}

func TestBindings(t *testing.T) {
	ext := extractTS(t, `
import D, { A, B as C, type T } from "./m";
import * as ns from "../n";
import type { X, Y as Z } from "./t";
import type W from "./w";
import E, * as all from "./e";
import "./side-effect";
import { z } from "zod";
`)
	want := []string{
		"D=named:default:./m typeOnly=false",
		"A=named:A:./m typeOnly=false",
		"C=named:B:./m typeOnly=false",
		"T=named:T:./m typeOnly=true",
		"ns=namespace::../n typeOnly=false",
		"X=named:X:./t typeOnly=true",
		"Z=named:Y:./t typeOnly=true",
		"W=named:default:./w typeOnly=true",
		"E=named:default:./e typeOnly=false",
		"all=namespace::./e typeOnly=false",
		"z=named:z:zod typeOnly=false",
	}
	got := bindingRows(ext)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("bindings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Legacy ImportDraft keeps one entry per statement (including side-effect).
	if len(ext.Imports) != 7 {
		t.Errorf("legacy imports = %d, want 7", len(ext.Imports))
	}
}

func exportRows(ext language.Extraction) []string {
	var out []string
	for _, e := range ext.Exports {
		mod := ""
		if e.Kind != language.ExportLocal {
			mod = "<-" + e.Module.Specifier
		}
		out = append(out, fmt.Sprintf("%s %q=%q%s except=%v typeOnly=%v", e.Kind, e.Exported, e.Local, mod, e.Except, e.TypeOnly))
	}
	return out
}

func TestExports(t *testing.T) {
	ext := extractTS(t, `
import { Imp } from "./imp";
class Hidden {}
export class User {}
export function make() {}
export const K = 1, L = 2;
export interface I {}
export type T = string;
export enum E { A }
export { Hidden as Shown, Imp };
export { default as Dflt, Named as Other } from "./src";
export type { TT } from "./types";
export { type U1, U2 } from "./u";
export * from "./all";
export * as ns from "./nsmod";
export default class Main {}
`)
	want := []string{
		`local "User"="User" except=[] typeOnly=false`,
		`local "make"="make" except=[] typeOnly=false`,
		`local "K"="K" except=[] typeOnly=false`,
		`local "L"="L" except=[] typeOnly=false`,
		`local "I"="I" except=[] typeOnly=true`,
		`local "T"="T" except=[] typeOnly=true`,
		`local "E"="E" except=[] typeOnly=false`,
		`local "Shown"="Hidden" except=[] typeOnly=false`,
		`local "Imp"="Imp" except=[] typeOnly=false`,
		`from "Dflt"="default"<-./src except=[] typeOnly=false`,
		`from "Other"="Named"<-./src except=[] typeOnly=false`,
		`from "TT"="TT"<-./types except=[] typeOnly=true`,
		`from "U1"="U1"<-./u except=[] typeOnly=true`,
		`from "U2"="U2"<-./u except=[] typeOnly=false`,
		`all ""=""<-./all except=[default] typeOnly=false`,
		`namespace "ns"=""<-./nsmod except=[] typeOnly=false`,
		`local "default"="Main" except=[] typeOnly=false`,
	}
	got := exportRows(ext)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("exports:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// `Hidden` is declared but only exported under an alias; `export default
	// class Main` exports "default", not "Main".
	for _, e := range ext.Exports {
		if e.Exported == "Hidden" || e.Exported == "Main" {
			t.Errorf("unexpected export name %q", e.Exported)
		}
	}
}

func TestExports_DefaultForms(t *testing.T) {
	ext := extractTS(t, `
function make() {}
export default make;
`)
	if got := exportRows(ext); len(got) != 1 || got[0] != `local "default"="make" except=[] typeOnly=false` {
		t.Errorf("export default identifier: %v", got)
	}
	ext = extractTS(t, `export default function make() {}`)
	if got := exportRows(ext); len(got) != 1 || got[0] != `local "default"="make" except=[] typeOnly=false` {
		t.Errorf("export default function: %v", got)
	}
	ext = extractTS(t, `export default () => 1;`)
	if len(ext.Exports) != 0 {
		t.Errorf("anonymous default export has no identity: %v", exportRows(ext))
	}
}

func TestModuleScoped(t *testing.T) {
	if !extractTS(t, `import { a } from "./a"; a();`).ModuleScoped {
		t.Error("file with import must be module-scoped")
	}
	if !extractTS(t, `export const a = 1;`).ModuleScoped {
		t.Error("file with export must be module-scoped")
	}
	if extractTS(t, `function f() {} f();`).ModuleScoped {
		t.Error("a script (no import/export) declares globals and must not be module-scoped")
	}
}

func candidates(spec string, importer string) string {
	m := moduleSpec(source.FileID(importer), spec)
	if m.Candidates == nil {
		return "external"
	}
	var parts []string
	for _, c := range m.Candidates {
		parts = append(parts, fmt.Sprintf("%s@%d", c.File, c.Priority))
	}
	return strings.Join(parts, " ")
}

func TestModuleSpecCandidates(t *testing.T) {
	cases := []struct{ spec, importer, want string }{
		{"./user", "src/a.ts", "src/user.ts@0 src/user.tsx@1 src/user/index.ts@2 src/user/index.tsx@3"},
		{"../domain/user", "src/app/a.ts", "src/domain/user.ts@0 src/domain/user.tsx@1 src/domain/user/index.ts@2 src/domain/user/index.tsx@3"},
		{"./user.ts", "src/a.ts", "src/user.ts@0"},
		{"./user.tsx", "src/a.tsx", "src/user.tsx@0"},
		{"./user/index", "src/a.ts", "src/user/index.ts@0 src/user/index.tsx@1 src/user/index/index.ts@2 src/user/index/index.tsx@3"},
		{"./user.service", "src/a.ts", "src/user.service.ts@0 src/user.service.tsx@1 src/user.service/index.ts@2 src/user.service/index.tsx@3"},
		// .js / .jsx substitution is NOT ranked: equal priority, ambiguity when both exist.
		{"./user.js", "src/a.ts", "src/user.ts@0 src/user.tsx@0"},
		{"./user.jsx", "src/a.ts", "src/user.ts@0 src/user.tsx@0"},
		{".", "src/a.ts", "src/index.ts@0 src/index.tsx@1"},
		{"./", "src/a.ts", "src/index.ts@0 src/index.tsx@1"},
		{"..", "src/app/a.ts", "src/index.ts@0 src/index.tsx@1"},
		{"./x", "a.ts", "x.ts@0 x.tsx@1 x/index.ts@2 x/index.tsx@3"},
		{".", "a.ts", "index.ts@0 index.tsx@1"},
		// not repository-resolvable
		{"zod", "src/a.ts", "external"},
		{"react", "src/a.tsx", "external"},
		{"@/foo", "src/a.ts", "external"},
		{"@scope/pkg/sub", "src/a.ts", "external"},
		{"node:fs", "src/a.ts", "external"},
		{"/abs/path", "src/a.ts", "external"},
		// root escape never yields candidates
		{"../../x", "src/a.ts", "external"},
		{"../x", "a.ts", "external"},
		{"./../../x", "src/a.ts", "external"},
		// backslashes / control characters are never interpreted
		{`./a\b`, "src/a.ts", "external"},
		{"./a\x00b", "src/a.ts", "external"},
		{"./a\nb", "src/a.ts", "external"},
	}
	for _, c := range cases {
		if got := candidates(c.spec, c.importer); got != c.want {
			t.Errorf("%q from %s:\n got %s\nwant %s", c.spec, c.importer, got, c.want)
		}
	}
}

// Priorities must only ever reflect the lexical subset Ark explicitly supports
// (.ts before .tsx before directory index) — not compiler-equivalent module
// resolution, which Ark does not interpret.
func TestModuleSpecPriorityOrderIsArkLexicalSubset(t *testing.T) {
	m := moduleSpec("src/a.ts", "./user")
	var order []string
	for _, c := range m.Candidates {
		order = append(order, string(c.File))
	}
	want := []string{"src/user.ts", "src/user.tsx", "src/user/index.ts", "src/user/index.tsx"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("candidate order %v, want %v", order, want)
	}
	if !sort.SliceIsSorted(m.Candidates, func(i, j int) bool { return m.Candidates[i].Priority < m.Candidates[j].Priority }) {
		t.Error("priorities must be non-decreasing")
	}
}

// --- TSX ------------------------------------------------------------------------

func TestTSX_ComponentsAndIntrinsics(t *testing.T) {
	ext := extractTSX(t, `
import { UserCard, Layout } from "./card";
import * as UI from "./ui";
export function Page() {
  return (
    <div className="p">
      <UserCard user={u} />
      <Layout.Header />
      <UI.Button>go</UI.Button>
      <span />
      <button onClick={() => save()} />
      <my-element />
    </div>
  );
}
export const Row = () => <UserCard />;
`)
	rows := refRows(ext)
	for _, w := range []refRow{
		{"UserCard", "call", "Page", "", ""},
		{"Header", "call", "Page", "Layout", ""},
		{"Button", "call", "Page", "UI", ""},
		{"save", "call", "Page", "", ""},
		{"UserCard", "call", "Row", "", ""},
	} {
		if !hasRef(rows, w) {
			t.Errorf("missing %+v\n%s", w, dumpRows(rows))
		}
	}
	for _, r := range rows {
		switch r.name {
		case "div", "span", "button", "my-element":
			t.Errorf("intrinsic element %q must not be a reference: %+v", r.name, r)
		}
	}
	findSym(t, ext, "Page", symbol.KindFunction)
	findSym(t, ext, "Row", symbol.KindFunction) // const component = function symbol
}

// --- hardening ------------------------------------------------------------------

func TestMalformedInputsDoNotPanic(t *testing.T) {
	inputs := []string{
		``, `import`, `import {`, `import { A as } from`, `import * from "x"`, `import x from`,
		`export`, `export {`, `export { A as } from "./x"`, `export * as from "./x"`,
		`export default`, `class`, `class {`, `class C {`, `class C extends {}`, `class C { get }`,
		`class C { constructor( }`, `class C { static async * }`, `interface`, `interface I {`,
		`function (`, `const = 1`, `const { = o`, `new`, `a.`, `a?.`, `<div`, `<Foo.`,
		"import x from \"\"", "export { a } from ''", "import * as from './x'",
		`class C { 1() {} 'a-b'() {} [k]() {} }`,
	}
	for _, p := range []*Provider{NewProvider(), NewTSXProvider()} {
		for _, src := range inputs {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s: panic on %q: %v", p.Language(), src, r)
					}
				}()
				ext, err := p.Extract(context.Background(), "x/a.ts", []byte(src))
				if err != nil {
					t.Errorf("%q: unexpected error %v", src, err)
				}
				if errs := language.ValidateModuleBindings(ext); len(errs) > 0 {
					t.Errorf("%q: module-binding contract violated: %v", src, errs)
				}
			}()
		}
	}
}

func TestExtractionIsDeterministic(t *testing.T) {
	src := memberSrc + `
import { a as b } from "./m"; export * from "./n"; export { b };
`
	render := func() string {
		ext := extractTS(t, src)
		return fmt.Sprintf("%v|%v|%v|%v|%v", ext.Symbols, ext.References, ext.Imports, ext.Bindings, ext.Exports)
	}
	want := render()
	for i := range 50 {
		if render() != want {
			t.Fatalf("run %d: non-deterministic extraction", i)
		}
	}
}
