package php

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// Provider-side qualified identity (PHP-Q2): the FQN a written class name
// denotes, determined from the file alone (namespace, use, use-as, `\`-qualified
// and `namespace\` syntax). These tests inspect what the provider emits; the
// resolver's use of it is covered by qualified_identity_test.go.

func extractRefs(t *testing.T, src string) []language.ReferenceDraft {
	t.Helper()
	ext, err := NewProvider().Extract(context.Background(), source.FileID("t.php"), []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ext.References
}

// idents summarizes each reference as "kind name [recv=…] [name=FQN] [rtype=FQN]".
func idents(refs []language.ReferenceDraft) []string {
	var out []string
	for _, r := range refs {
		s := r.Kind + " " + r.Name
		if r.ReceiverExpr != "" {
			s += " recv=" + r.ReceiverExpr
		}
		if r.NameQualified != "" {
			s += " name=" + r.NameQualified
		}
		if r.ReceiverTypeQualified != "" {
			s += " rtype=" + r.ReceiverTypeQualified
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func wantIdents(t *testing.T, src string, want ...string) {
	t.Helper()
	got := idents(extractRefs(t, src))
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("references:\n got  %q\n want %q", got, want)
	}
}

func TestQualifiedNames_NamespaceLocal(t *testing.T) {
	wantIdents(t, `<?php
namespace App\Services;
class C {
    public function m() {
        Foo::make();
        new Foo();
    }
}`,
		`call make recv=Foo rtype=App\Services\Foo`,
		`construction Foo name=App\Services\Foo`,
	)
}

func TestQualifiedNames_GlobalNamespace(t *testing.T) {
	// No namespace: a bare class name is the global class of that name.
	wantIdents(t, `<?php
class C {
    public function m() { new Foo(); Foo::make(); }
}`,
		`construction Foo name=Foo`,
		`call make recv=Foo rtype=Foo`,
	)
}

func TestQualifiedNames_Use(t *testing.T) {
	wantIdents(t, `<?php
namespace App\Http;
use App\Services\Foo;
class C {
    public function m(Foo $f) { Foo::make(); new Foo(); $f->run(); }
}`,
		`type_use Foo name=App\Services\Foo`,
		`call make recv=Foo rtype=App\Services\Foo`,
		`construction Foo name=App\Services\Foo`,
		`call run recv=$f rtype=App\Services\Foo`,
	)
}

func TestQualifiedNames_UseAlias(t *testing.T) {
	// The alias is matched case-insensitively (PHP); the identity is the
	// import's FQN as written.
	wantIdents(t, `<?php
namespace App\Http;
use App\Services\Foo as Bar;
class C {
    public function m() { Bar::make(); BAR::make(); new bar(); }
}`,
		`call make recv=Bar rtype=App\Services\Foo`,
		`call make recv=BAR rtype=App\Services\Foo`,
		`construction bar name=App\Services\Foo`,
	)
}

func TestQualifiedNames_FullyQualified(t *testing.T) {
	// A leading `\` is not part of the canonical identity; the use table and
	// the current namespace do not apply.
	wantIdents(t, `<?php
namespace App\Http;
use Other\Foo;
class C {
    public function m(\App\Services\Foo $f) {
        \App\Services\Foo::make();
        new \App\Services\Foo();
        $f->run();
    }
}`,
		`type_use Foo name=App\Services\Foo`,
		`call make recv=Foo rtype=App\Services\Foo`,
		`construction Foo name=App\Services\Foo`,
		`call run recv=$f rtype=App\Services\Foo`,
	)
}

func TestQualifiedNames_RelativeNamespaceSyntax(t *testing.T) {
	wantIdents(t, `<?php
namespace App\Services;
class C {
    public function m() { namespace\Foo::make(); new namespace\Foo(); }
}`,
		`call make recv=Foo rtype=App\Services\Foo`,
		`construction Foo name=App\Services\Foo`,
	)
}

func TestQualifiedNames_QualifiedNameWithImportedPrefix(t *testing.T) {
	// `Sub\Foo` with `use App\Models as Sub` expands only the first segment;
	// without such an import it is relative to the current namespace.
	wantIdents(t, `<?php
namespace App\Http;
use App\Models as Sub;
class C {
    public function m() { Sub\Foo::make(); Other\Foo::make(); }
}`,
		`call make recv=Foo rtype=App\Models\Foo`,
		`call make recv=Foo rtype=App\Http\Other\Foo`,
	)
}

func TestQualifiedNames_GroupedUse(t *testing.T) {
	wantIdents(t, `<?php
namespace App\Http;
use App\Services\{Foo, Bar as Baz};
class C {
    public function m() { Foo::a(); Baz::b(); }
}`,
		`call a recv=Foo rtype=App\Services\Foo`,
		`call b recv=Baz rtype=App\Services\Bar`,
	)
}

// `use function` / `use const` import functions and constants, not classes: they
// must not shadow a class name, and are out of scope for qualified identity.
func TestQualifiedNames_FunctionAndConstImportsAreNotClassImports(t *testing.T) {
	wantIdents(t, `<?php
namespace App\Http;
use function Lib\Foo;
use const Lib\Bar;
use Lib\{function Baz, const QUX};
class C {
    public function m() { Foo::a(); new Bar(); Baz::b(); helper(); }
}`,
		`call a recv=Foo rtype=App\Http\Foo`,
		`construction Bar name=App\Http\Bar`,
		`call b recv=Baz rtype=App\Http\Baz`,
		`call helper`,
	)
}

func TestQualifiedNames_BracketedNamespacesHaveSeparateImportTables(t *testing.T) {
	wantIdents(t, `<?php
namespace A {
    use X\Foo;
    class C1 { public function f() { Foo::m(); } }
}
namespace B {
    use Y\Foo;
    class C2 { public function f() { Foo::m(); } }
}
namespace {
    class C3 { public function f() { Foo::m(); } }
}`,
		`call m recv=Foo rtype=X\Foo`,
		`call m recv=Foo rtype=Y\Foo`,
		`call m recv=Foo rtype=Foo`,
	)
}

func TestQualifiedNames_StatementNamespacesResetImports(t *testing.T) {
	wantIdents(t, `<?php
namespace A;
use X\Foo;
class C1 { public function f() { Foo::m(); } }

namespace B;
class C2 { public function f() { Foo::m(); } }
`,
		`call m recv=Foo rtype=X\Foo`,
		`call m recv=Foo rtype=B\Foo`, // A's import does not leak into B
	)
}

// A use applies to the code after it, in its own namespace only.
func TestQualifiedNames_ImportAppliesFromItsPosition(t *testing.T) {
	wantIdents(t, `<?php
namespace A;
class Before { public function f() { Foo::m(); } }
use X\Foo;
class After { public function f() { Foo::m(); } }
`,
		`call m recv=Foo rtype=A\Foo`,
		`call m recv=Foo rtype=X\Foo`,
	)
}

// Declarations and references derive names from one canonical form, so a
// declaration's Symbol.Qualified is exactly the identity a reference carries.
func TestQualifiedNames_MatchesDeclarationQualified(t *testing.T) {
	decl := `<?php
namespace App\Services;
class Foo { public static function make() {} }`
	ext, err := NewProvider().Extract(context.Background(), source.FileID("d.php"), []byte(decl))
	if err != nil {
		t.Fatal(err)
	}
	var qualified string
	for _, s := range ext.Symbols {
		if s.Name == "Foo" {
			qualified = s.Qualified
		}
	}
	for _, src := range []string{
		"<?php\nnamespace App\\Http;\nuse App\\Services\\Foo;\nclass C { function m() { Foo::make(); } }",
		"<?php\nnamespace App\\Http;\nuse App\\Services\\Foo as X;\nclass C { function m() { X::make(); } }",
		"<?php\nnamespace App\\Http;\nclass C { function m() { \\App\\Services\\Foo::make(); } }",
		"<?php\nnamespace App\\Services;\nclass C { function m() { Foo::make(); } }",
	} {
		refs := extractRefs(t, src)
		if len(refs) != 1 || refs[0].ReceiverTypeQualified != qualified {
			t.Errorf("%q: receiver identity %v, want declaration's %q", src, idents(refs), qualified)
		}
	}
}

// Relative class names are not lexical identities and carry none.
func TestQualifiedNames_RelativeScopesCarryNoIdentity(t *testing.T) {
	wantIdents(t, `<?php
namespace App;
class C extends P {
    public function m() { self::a(); static::b(); parent::c(); new static(); }
}`,
		`inheritance P name=App\P`,
		`call a`,
		`call b`,
		`call c`,
		`construction static`,
	)
}

// Members, functions and constants never get a qualified identity.
func TestQualifiedNames_OnlyTypesAreQualified(t *testing.T) {
	for _, r := range extractRefs(t, `<?php
namespace App;
use Lib\Foo;
class C {
    public function m() { helper(); Foo::K; Foo::make(); $x->y(); }
}`) {
		if r.Kind == "call" && r.ReceiverExpr == "" && (r.NameQualified != "" || r.ReceiverTypeQualified != "") {
			t.Errorf("function call %q must not carry qualified identity: %+v", r.Name, r)
		}
		if r.NameQualified != "" && r.ReceiverExpr != "" {
			t.Errorf("member %q must not carry NameQualified: %+v", r.Name, r)
		}
	}
}

// Type, relation and trait references all carry the identity of their class.
func TestQualifiedNames_TypeRelations(t *testing.T) {
	wantIdents(t, `<?php
namespace App\Http;
use App\Base;
use App\Contracts\{A, B as Bee};
use App\Concerns\Logs;
class C extends Base implements A, Bee {
    use Logs;
    private ?Base $p;
}`,
		`inheritance Base name=App\Base`,
		`implementation A name=App\Contracts\A`,
		`implementation Bee name=App\Contracts\B`,
		`uses_trait Logs name=App\Concerns\Logs`,
		`type_use Base name=App\Base`,
	)
}

// Typed receivers: parameter, promoted/typed property and `new` assignment.
func TestQualifiedNames_ReceiverTypeEvidence(t *testing.T) {
	wantIdents(t, `<?php
namespace App\Http;
use App\Services\Foo;
use App\Services\Bar as B;
class C {
    private Foo $foo;
    public function __construct(private B $bar) {}
    public function m(Foo $p) {
        $p->a();
        $this->foo->b();
        $this->bar->c();
        $n = new Foo();
        $n->d();
    }
}`,
		`type_use Foo name=App\Services\Foo`,
		`type_use B name=App\Services\Bar`,
		`type_use Foo name=App\Services\Foo`,
		`call a recv=$p rtype=App\Services\Foo`,
		`call b recv=$this->foo rtype=App\Services\Foo`,
		`call c recv=$this->bar rtype=App\Services\Bar`,
		`construction Foo name=App\Services\Foo`,
		`call d recv=$n rtype=App\Services\Foo`,
	)
}

func TestQualifiedNames_AliasClashHasNoIdentity(t *testing.T) {
	// Importing one alias twice is invalid PHP; no identity is guessed.
	for _, r := range extractRefs(t, `<?php
namespace App;
use X\Foo;
use Y\Foo;
class C { public function m() { Foo::a(); new Foo(); } }`) {
		if r.NameQualified != "" || r.ReceiverTypeQualified != "" {
			t.Errorf("ambiguous alias must carry no identity: %+v", r)
		}
	}
}

// The provider now emits qualified identity evidence, which changes its cached
// output: entries written as php-6 lack it and must not be reused.
func TestCacheVersionCoversQualifiedIdentity(t *testing.T) {
	if got := NewProvider().CacheVersion(); got != "php-7" {
		t.Errorf("CacheVersion = %q, want php-7 (php-6 entries carry no qualified identity)", got)
	}
}
