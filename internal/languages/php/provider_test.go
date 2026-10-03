package php

import (
	"context"
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

func TestPHPProvider_Safety(t *testing.T) {
	inputs := map[string]string{
		"empty":          ``,
		"only_tag":       `<?php`,
		"no_tag":         `plain text no php`,
		"broken_member":  `<?php class Broken { public function m( {  private $x; `,
		"partial_prop":   `<?php class C { private `,
		"bad_const":      `<?php class C { const = ; public function ok() {} }`,
		"unclosed_class": `<?php class C {`,
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
