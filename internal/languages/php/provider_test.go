package php

import (
	"context"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// extractDrafts runs Extract and returns all symbols plus a by-qualified lookup.
func extractDrafts(t *testing.T, src string) ([]language.SymbolDraft, map[string]language.SymbolDraft) {
	t.Helper()
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("test.php"), []byte(src))
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	byQual := make(map[string]language.SymbolDraft, len(ext.Symbols))
	for _, d := range ext.Symbols {
		byQual[d.Qualified] = d
	}
	return ext.Symbols, byQual
}

// extractImports runs Extract and returns the imports in source order.
func extractImports(t *testing.T, src string) []language.ImportDraft {
	t.Helper()
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("test.php"), []byte(src))
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	return ext.Imports
}

func TestPHPProvider_Identity(t *testing.T) {
	p := NewProvider()
	if p.Language() != "php" {
		t.Errorf("Language() = %q, want php", p.Language())
	}
	if exts := p.Extensions(); len(exts) != 1 || exts[0] != ".php" {
		t.Errorf("Extensions() = %v, want [.php]", exts)
	}
	if p.CacheVersion() == "" {
		t.Error("CacheVersion() is empty")
	}
}

// ---- PHP-2: top-level ----

func TestPHPProvider_TopLevelKinds(t *testing.T) {
	_, m := extractDrafts(t, `<?php
namespace App\Service;

class UserService {}
interface Repository {}
trait LogsActivity {}
enum Status {
    case Active;
}
function helper(): void {}
const MAX = 100;
`)
	want := map[string]symbol.SymbolKind{
		"App\\Service":               symbol.KindNamespace,
		"App\\Service\\UserService":  symbol.KindClass,
		"App\\Service\\Repository":   symbol.KindInterface,
		"App\\Service\\LogsActivity": symbol.KindTrait,
		"App\\Service\\Status":       symbol.KindEnum,
		"App\\Service\\helper":       symbol.KindFunction,
		"App\\Service\\MAX":          symbol.KindConstant,
	}
	for q, k := range want {
		d, ok := m[q]
		if !ok {
			t.Errorf("symbol %q not extracted", q)
			continue
		}
		if d.Kind != k {
			t.Errorf("%q: kind=%q want %q", q, d.Kind, k)
		}
		if !d.Exported {
			t.Errorf("%q: top-level symbol should be Exported", q)
		}
	}
}

func TestPHPProvider_GlobalAndBracketedNamespace(t *testing.T) {
	_, m := extractDrafts(t, `<?php
namespace App\Service {
    class UserService {}
}
namespace App\Other {
    trait LogsActivity {}
}
`)
	for q := range map[string]bool{"App\\Service\\UserService": true, "App\\Other\\LogsActivity": true} {
		if _, ok := m[q]; !ok {
			t.Errorf("expected qualified symbol %q", q)
		}
	}

	_, g := extractDrafts(t, `<?php
class Plain {}
const PLAIN = 1;
`)
	if _, ok := g["Plain"]; !ok {
		t.Error("global class Plain should have bare qualified")
	}
	if _, ok := g["PLAIN"]; !ok {
		t.Error("global const PLAIN should have bare qualified")
	}
}

// ---- PHP-3: members + containment ----

const memberSrc = `<?php
namespace App\Service;

class UserService
{
    private Repository $repository;
    private string $first, $second;
    public const DEFAULT_LIMIT = 100;
    private const A = 1, B = 2;

    public function __construct(
        private Logger $logger,
        protected readonly Config $config,
        Plain $plain,
    ) {}

    public function find(int $id): User {}
    protected static function make(): self {}
    private function secret(): void {}
}
`

func TestPHPProvider_Members_QualifiedParentReceiver(t *testing.T) {
	_, m := extractDrafts(t, memberSrc)

	const cls = "App\\Service\\UserService"
	cases := []struct {
		qualified string
		kind      symbol.SymbolKind
		exported  bool
	}{
		{cls + ".repository", symbol.KindProperty, false},
		{cls + ".first", symbol.KindProperty, false},
		{cls + ".second", symbol.KindProperty, false},
		{cls + ".DEFAULT_LIMIT", symbol.KindConstant, true},
		{cls + ".A", symbol.KindConstant, false},
		{cls + ".B", symbol.KindConstant, false},
		{cls + ".__construct", symbol.KindConstructor, true},
		{cls + ".logger", symbol.KindProperty, false}, // promoted
		{cls + ".config", symbol.KindProperty, false}, // promoted
		{cls + ".find", symbol.KindMethod, true},
		{cls + ".make", symbol.KindMethod, false},   // protected
		{cls + ".secret", symbol.KindMethod, false}, // private
	}
	for _, c := range cases {
		d, ok := m[c.qualified]
		if !ok {
			t.Errorf("member %q not extracted", c.qualified)
			continue
		}
		if d.Kind != c.kind {
			t.Errorf("%q: kind=%q want %q", c.qualified, d.Kind, c.kind)
		}
		if d.Parent != cls {
			t.Errorf("%q: Parent=%q want %q", c.qualified, d.Parent, cls)
		}
		if d.Receiver != "UserService" {
			t.Errorf("%q: Receiver=%q want UserService", c.qualified, d.Receiver)
		}
		if d.Exported != c.exported {
			t.Errorf("%q: Exported=%v want %v", c.qualified, d.Exported, c.exported)
		}
	}
}

func TestPHPProvider_NormalParamNotProperty(t *testing.T) {
	_, m := extractDrafts(t, memberSrc)
	if _, ok := m["App\\Service\\UserService.plain"]; ok {
		t.Error("normal constructor parameter 'plain' must not become a property")
	}
}

func TestPHPProvider_NoDuplicateSymbols(t *testing.T) {
	syms, _ := extractDrafts(t, memberSrc)
	seen := map[string]int{}
	for _, d := range syms {
		seen[string(d.Kind)+"|"+d.Qualified]++
	}
	for k, n := range seen {
		if n != 1 {
			t.Errorf("duplicate symbol %q appears %d times", k, n)
		}
	}
}

func TestPHPProvider_EnumMembers(t *testing.T) {
	_, m := extractDrafts(t, `<?php
enum Status: string {
    case Active = 'active';
    case Disabled = 'disabled';
    const DEFAULT = 'x';
    public function label(): string {}
}
`)
	for q, k := range map[string]symbol.SymbolKind{
		"Status.Active":   symbol.KindConstant,
		"Status.Disabled": symbol.KindConstant,
		"Status.DEFAULT":  symbol.KindConstant,
		"Status.label":    symbol.KindMethod,
	} {
		d, ok := m[q]
		if !ok {
			t.Errorf("enum member %q not extracted", q)
			continue
		}
		if d.Kind != k {
			t.Errorf("%q: kind=%q want %q", q, d.Kind, k)
		}
		if d.Parent != "Status" {
			t.Errorf("%q: Parent=%q want Status", q, d.Parent)
		}
	}
	// Enum cases are publicly accessible.
	if !m["Status.Active"].Exported {
		t.Error("enum case should be Exported")
	}
}

func TestPHPProvider_InterfaceAndTraitMembers(t *testing.T) {
	_, m := extractDrafts(t, `<?php
interface Repository {
    public function find(int $id): ?User;
}
trait LogsActivity {
    protected string $log;
    public function record(): void {}
}
`)
	if d, ok := m["Repository.find"]; !ok || d.Kind != symbol.KindMethod {
		t.Errorf("interface method Repository.find missing/wrong: %+v", d)
	}
	if d, ok := m["LogsActivity.log"]; !ok || d.Kind != symbol.KindProperty || d.Exported {
		t.Errorf("trait property LogsActivity.log missing/wrong: %+v", d)
	}
	if d, ok := m["LogsActivity.record"]; !ok || d.Kind != symbol.KindMethod || !d.Exported {
		t.Errorf("trait method LogsActivity.record missing/wrong: %+v", d)
	}
}

func TestPHPProvider_Modifiers(t *testing.T) {
	_, m := extractDrafts(t, `<?php
final class F { final public function a(): void {} }
abstract class A { abstract protected function h(): void; }
readonly class R { public function x(): void {} }
`)
	for q := range map[string]bool{"F": true, "A": true, "R": true, "F.a": true, "A.h": true, "R.x": true} {
		if _, ok := m[q]; !ok {
			t.Errorf("modifier case: symbol %q not extracted", q)
		}
	}
	if m["A.h"].Exported {
		t.Error("abstract protected method A.h should not be Exported")
	}
}

// ---- Negative: container safety ----

func TestPHPProvider_ContainerSafety(t *testing.T) {
	syms, m := extractDrafts(t, `<?php
class Outer {
    public function m(): void {
        $fn = function() { return 1; };
        $anon = new class {
            public function innerAnon(): void {}
        };
        $arrow = fn($x) => $x + 1;
        function nestedFn() {}
    }
    private int $kept = 0;
}
`)
	// The anonymous class method must NOT be attributed to Outer.
	if _, ok := m["Outer.innerAnon"]; ok {
		t.Error("anonymous class method must not be attributed to Outer")
	}
	// Closures / arrow functions / nested functions inside a method body must
	// not be extracted as members.
	for _, d := range syms {
		if d.Parent == "Outer" && d.Name != "m" && d.Name != "kept" {
			t.Errorf("unexpected member of Outer: %+v", d)
		}
		if d.Name == "innerAnon" || d.Name == "nestedFn" {
			t.Errorf("body-nested symbol leaked: %+v", d)
		}
	}
	// Legitimate members are still present.
	if _, ok := m["Outer.m"]; !ok {
		t.Error("Outer.m method missing")
	}
	if _, ok := m["Outer.kept"]; !ok {
		t.Error("Outer.kept property missing")
	}
}

func TestPHPProvider_DestructorNotConstructor(t *testing.T) {
	_, m := extractDrafts(t, `<?php
class C {
    public function __construct() {}
    public function __destruct() {}
    public function __toString(): string {}
}
`)
	if m["C.__construct"].Kind != symbol.KindConstructor {
		t.Error("__construct should be KindConstructor")
	}
	if m["C.__destruct"].Kind != symbol.KindMethod {
		t.Error("__destruct should be KindMethod, not Constructor")
	}
	if m["C.__toString"].Kind != symbol.KindMethod {
		t.Error("__toString should be KindMethod")
	}
}

// ---- Quality ----

func TestPHPProvider_ByteAndLocation(t *testing.T) {
	src := []byte(memberSrc)
	p := NewProvider()
	ext, err := p.Extract(context.Background(), source.FileID("test.php"), src)
	if err != nil {
		t.Fatal(err)
	}
	n := uint32(len(src))
	for _, d := range ext.Symbols {
		if d.Name == "" || d.Qualified == "" {
			t.Errorf("empty Name/Qualified: %+v", d)
		}
		if d.Kind == "" || d.Kind == symbol.KindUnknown {
			t.Errorf("%q: invalid kind %q", d.Qualified, d.Kind)
		}
		if d.Location.File == "" {
			t.Errorf("%q: empty Location.File", d.Qualified)
		}
		if d.StartByte > d.EndByte || d.EndByte > n {
			t.Errorf("%q: bad byte range [%d,%d] len=%d", d.Qualified, d.StartByte, d.EndByte, n)
		}
		if d.Location.Range.Start.Line == 0 || d.Location.Range.Start.Column == 0 {
			t.Errorf("%q: location must be 1-based: %+v", d.Qualified, d.Location.Range.Start)
		}
	}
}

func TestPHPProvider_Deterministic(t *testing.T) {
	p := NewProvider()
	a, _ := p.Extract(context.Background(), source.FileID("test.php"), []byte(memberSrc))
	b, _ := p.Extract(context.Background(), source.FileID("test.php"), []byte(memberSrc))
	if len(a.Symbols) != len(b.Symbols) {
		t.Fatalf("non-deterministic count: %d vs %d", len(a.Symbols), len(b.Symbols))
	}
	for i := range a.Symbols {
		if a.Symbols[i].Qualified != b.Symbols[i].Qualified || a.Symbols[i].Kind != b.Symbols[i].Kind {
			t.Errorf("non-deterministic at %d: %+v vs %+v", i, a.Symbols[i], b.Symbols[i])
		}
	}
}

// ---- PHP-4: namespace imports / use ----

func TestPHPProvider_Imports_Forms(t *testing.T) {
	imports := extractImports(t, `<?php
use App\Model\User;
use App\Model\Repository as Repo;
use App\Model\User, App\Model\Account;
use App\Model\{Thing, Account as DomainAccount};
use function App\Util\helper;
use function App\Util\helper as appHelper;
use const App\Config\DEFAULT_LIMIT;
use const App\Config\DEFAULT_LIMIT as LIMIT;
`)
	type pa struct{ path, alias string }
	want := []pa{
		{"App\\Model\\User", ""},
		{"App\\Model\\Repository", "Repo"},
		{"App\\Model\\User", ""},
		{"App\\Model\\Account", ""},
		{"App\\Model\\Thing", ""},
		{"App\\Model\\Account", "DomainAccount"},
		{"App\\Util\\helper", ""},
		{"App\\Util\\helper", "appHelper"},
		{"App\\Config\\DEFAULT_LIMIT", ""},
		{"App\\Config\\DEFAULT_LIMIT", "LIMIT"},
	}
	if len(imports) != len(want) {
		t.Fatalf("got %d imports, want %d: %+v", len(imports), len(want), imports)
	}
	for i, w := range want {
		if imports[i].Path != w.path || imports[i].Alias != w.alias {
			t.Errorf("import[%d] = {Path:%q Alias:%q}, want {Path:%q Alias:%q}",
				i, imports[i].Path, imports[i].Alias, w.path, w.alias)
		}
		if imports[i].Location.File == "" {
			t.Errorf("import[%d] has empty Location.File", i)
		}
	}
}

func TestPHPProvider_Imports_MixedGrouped(t *testing.T) {
	imports := extractImports(t, `<?php
use App\Foo\{Bar, function baz, const QUX};
`)
	// Kind markers are dropped; targets/aliases are preserved and flattened.
	want := []string{"App\\Foo\\Bar", "App\\Foo\\baz", "App\\Foo\\QUX"}
	if len(imports) != len(want) {
		t.Fatalf("got %d imports, want %d: %+v", len(imports), len(want), imports)
	}
	for i, w := range want {
		if imports[i].Path != w {
			t.Errorf("import[%d].Path = %q, want %q", i, imports[i].Path, w)
		}
	}
}

func TestPHPProvider_Imports_LeadingBackslash(t *testing.T) {
	imports := extractImports(t, `<?php
use \App\Model\User;
`)
	if len(imports) != 1 || imports[0].Path != "App\\Model\\User" {
		t.Fatalf("leading backslash not normalized: %+v", imports)
	}
}

func TestPHPProvider_Imports_NamespaceContextNotApplied(t *testing.T) {
	// `use` targets are fully qualified; current namespace must NOT be prepended.
	imports := extractImports(t, `<?php
namespace App\Service;
use Domain\Model\User;
`)
	if len(imports) != 1 || imports[0].Path != "Domain\\Model\\User" {
		t.Fatalf("namespace context wrongly applied to import: %+v", imports)
	}
}

func TestPHPProvider_Imports_BracketedAndMultipleNamespaces(t *testing.T) {
	imports := extractImports(t, `<?php
namespace A {
    use X\One;
}
namespace B {
    use Y\Two;
}
`)
	want := []string{"X\\One", "Y\\Two"}
	if len(imports) != len(want) {
		t.Fatalf("got %d imports, want %d: %+v", len(imports), len(want), imports)
	}
	for i, w := range want {
		if imports[i].Path != w {
			t.Errorf("import[%d].Path = %q, want %q", i, imports[i].Path, w)
		}
	}
}

func TestPHPProvider_Imports_SourceOrderPreserved(t *testing.T) {
	imports := extractImports(t, `<?php
use App\B;
use App\A;
use App\C;
`)
	want := []string{"App\\B", "App\\A", "App\\C"}
	for i, w := range want {
		if imports[i].Path != w {
			t.Errorf("import[%d].Path = %q, want %q (source order must be preserved)", i, imports[i].Path, w)
		}
	}
}

func TestPHPProvider_Imports_DuplicatesNotDeduped(t *testing.T) {
	imports := extractImports(t, `<?php
use App\Model\User;
use App\Model\User as U2;
`)
	if len(imports) != 2 {
		t.Fatalf("duplicate textual names with different aliases must not be deduped: %+v", imports)
	}
}

// Negative: trait `use` inside a class body must NOT become an import.
func TestPHPProvider_TraitUseNotImport(t *testing.T) {
	imports := extractImports(t, `<?php
namespace App;
use App\Logging\Logger;
class UserService {
    use LogsActivity;
    use AnotherTrait;
    public function m(): void {}
}
`)
	if len(imports) != 1 || imports[0].Path != "App\\Logging\\Logger" {
		t.Fatalf("trait use must not be an import; got %+v", imports)
	}
}

// Regression: PHP-3 symbol extraction is unaffected by import handling.
func TestPHPProvider_Imports_SymbolRegression(t *testing.T) {
	_, m := extractDrafts(t, `<?php
namespace App\Service;
use App\Model\User;
class UserService {
    use LogsActivity;
    private User $user;
    public function find(int $id): User {}
}
`)
	for q, k := range map[string]symbol.SymbolKind{
		"App\\Service\\UserService":      symbol.KindClass,
		"App\\Service\\UserService.user": symbol.KindProperty,
		"App\\Service\\UserService.find": symbol.KindMethod,
	} {
		if d, ok := m[q]; !ok || d.Kind != k {
			t.Errorf("symbol regression: %q missing or wrong kind (%+v)", q, d)
		}
	}
}

func TestPHPProvider_Imports_Deterministic(t *testing.T) {
	src := `<?php
use App\Model\{User, Account as A};
use function App\f;
`
	a := extractImports(t, src)
	b := extractImports(t, src)
	if len(a) != len(b) {
		t.Fatalf("non-deterministic import count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Path != b[i].Path || a[i].Alias != b[i].Alias {
			t.Errorf("non-deterministic import at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
}

// ---- PHP-4 focused: 4 high-risk concerns ----

func importPaths(ds []language.ImportDraft) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Path
	}
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Concern 1: grouped-use prefix joining must not break on "\" presence/absence
// (no doubled "\\" between segments, no missing separator).
func TestPHPImports_GroupedPrefixJoin(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{`<?php
use App\Model\{User, Account};`, []string{"App\\Model\\User", "App\\Model\\Account"}},
		{`<?php
use App\{Thing};`, []string{"App\\Thing"}},
		{`<?php
use App\{Sub\Thing, Other};`, []string{"App\\Sub\\Thing", "App\\Other"}},
		{`<?php
use Deeply\Nested\Ns\{A, B\C};`, []string{"Deeply\\Nested\\Ns\\A", "Deeply\\Nested\\Ns\\B\\C"}},
	}
	for _, c := range cases {
		got := importPaths(extractImports(t, c.src))
		if !eqStrings(got, c.want) {
			t.Errorf("src %q: paths = %v, want %v", c.src, got, c.want)
		}
		for _, p := range got {
			if strings.Contains(p, "\\\\") {
				t.Errorf("path %q contains a doubled separator", p)
			}
			if strings.HasPrefix(p, "\\") {
				t.Errorf("path %q has a leading separator (should be normalized)", p)
			}
		}
	}
}

// Invalid PHP (leading "\" before a grouped prefix) must not panic.
func TestPHPImports_GroupedLeadingBackslashNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on invalid grouped use: %v", r)
		}
	}()
	_ = extractImports(t, `<?php
use \App\Model\{User, Account};
`)
}

// Concern 2: mixed grouped use must not leak the function/const kind markers
// into Path.
func TestPHPImports_MixedGroupedNoKindInPath(t *testing.T) {
	got := importPaths(extractImports(t, `<?php
use App\Foo\{Bar, function baz, const QUX};
`))
	want := []string{"App\\Foo\\Bar", "App\\Foo\\baz", "App\\Foo\\QUX"}
	if !eqStrings(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	for _, p := range got {
		if strings.Contains(p, "function") || strings.Contains(p, "const") {
			t.Errorf("kind marker leaked into path %q", p)
		}
	}
}

// Concern 3: trait use (including multi-trait and adaptation blocks) must never
// become an import, regardless of surrounding namespace imports.
func TestPHPImports_TraitUseNeverImported(t *testing.T) {
	// Trait use only, no namespace import → zero imports.
	if got := extractImports(t, `<?php
class C {
    use A;
    use B, D;
    use E { E::foo as bar; }
}
`); len(got) != 0 {
		t.Errorf("trait-use-only file produced imports: %+v", got)
	}

	// Namespace import + trait uses → only the namespace import.
	got := importPaths(extractImports(t, `<?php
namespace App;
use App\Logging\Logger;
class UserService {
    use A;
    use B, D;
    use E { E::foo as bar; }
    public function m(): void {}
}
`))
	if !eqStrings(got, []string{"App\\Logging\\Logger"}) {
		t.Errorf("imports = %v, want only [App\\Logging\\Logger]", got)
	}
}

// Concern 4: source order is preserved and duplicates are NOT deduped, even
// when plain and grouped forms are interleaved.
func TestPHPImports_OrderAndDuplicates(t *testing.T) {
	got := extractImports(t, `<?php
use App\B;
use App\{A, C};
use App\B;
use App\B as Other;
`)
	wantPath := []string{"App\\B", "App\\A", "App\\C", "App\\B", "App\\B"}
	wantAlias := []string{"", "", "", "", "Other"}
	if len(got) != len(wantPath) {
		t.Fatalf("got %d imports, want %d: %+v", len(got), len(wantPath), got)
	}
	for i := range wantPath {
		if got[i].Path != wantPath[i] || got[i].Alias != wantAlias[i] {
			t.Errorf("import[%d] = {%q,%q}, want {%q,%q}", i, got[i].Path, got[i].Alias, wantPath[i], wantAlias[i])
		}
	}
}

func TestPHPProvider_Safety(t *testing.T) {
	inputs := map[string]string{
		"empty":          ``,
		"only_tag":       `<?php`,
		"no_tag":         `plain text no php`,
		"broken_member":  `<?php class Broken { public function m( {  private $x; `,
		"partial_prop":   `<?php class C { private `,
		"bad_const":      `<?php class C { const = ; public function ok() {} }`,
		"unclosed_class": `<?php class C {`,
		"broken_use":     `<?php use App\ ; use ; use App\Model\{ ;`,
		"use_no_tail":    `<?php use ;`,
		"group_empty":    `<?php use App\{};`,
	}
	p := NewProvider()
	for name, src := range inputs {
		src := src
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on %q: %v", name, r)
				}
			}()
			if _, err := p.Extract(context.Background(), source.FileID("x.php"), []byte(src)); err != nil {
				t.Errorf("unexpected error on %q: %v", name, err)
			}
		})
	}
}
