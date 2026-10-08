package php_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// Local type evidence (receiver_types.go) holds only where the evidence
// assignment dominates the use: a whole statement of a `{ ... }` block, for
// uses after it in that block. Everything else is no evidence — the call is
// then not an edge, and completeness still counts it (unattributed when
// repository candidates exist, unresolved otherwise). End to end through
// RepositoryIndex.

// flowTarget is what a fixture's evidence would prove, if it were evidence.
type flowTarget struct {
	to   string                  // qualified target
	kind reference.ReferenceKind // reference kind of the edge
}

var (
	repoSave     = flowTarget{`App\Repo.save`, reference.KindCall}
	fooMake      = flowTarget{`App\Foo.make`, reference.KindCall}
	fooConstruct = flowTarget{`App\Foo`, reference.KindConstruction}
)

// flowIndex indexes body as the body of App\f and returns f's symbol ID.
func flowIndex(t *testing.T, body string) (*index.RepositoryIndex, symbol.SymbolID) {
	t.Helper()
	idx := phpTreeIndex(t, map[string]string{
		// Two classes with save(): an untyped `$r->save()` is a Candidate.
		"Repo.php":  "<?php\nnamespace App;\nclass Repo { public function save() {} }\n",
		"Other.php": "<?php\nnamespace App;\nclass Other { public function save() {} }\n",
		"Foo.php":   "<?php\nnamespace App;\nclass Foo { public static function make() {} }\n",
		"F.php":     "<?php\nnamespace App;\nfunction f($c, $xs) {\n" + body + "\n}\n",
	})
	fs := idx.FindSymbolsByQualified(`App\f`)
	if len(fs) != 1 {
		t.Fatalf("App\\f: %d symbols", len(fs))
	}
	return idx, fs[0].ID
}

func edgeTo(idx *index.RepositoryIndex, from symbol.SymbolID, want flowTarget) (index.GraphEdge, bool) {
	for _, e := range idx.GetCallees(from) {
		if s, ok := idx.GetSymbol(e.To); ok && s.Qualified == want.to && e.RefKind == want.kind {
			return e, true
		}
	}
	return index.GraphEdge{}, false
}

func TestFlowSoundness_DominatingAssignmentsResolve(t *testing.T) {
	cases := []struct {
		name, body string
		want       flowTarget
	}{
		{"class-string statement", `$cls = Foo::class; return new $cls();`, fooConstruct},
		{"class-string static call", `$cls = Foo::class; $cls::make();`, fooMake},
		{"object statement", `$r = new Repo(); $r->save();`, repoSave},
		{"use in a nested block after it", `$r = new Repo(); if ($c) { $r->save(); }`, repoSave},
		{"assignment and use in the same if block", `if ($c) { $r = new Repo(); $r->save(); }`, repoSave},
		{"assignment and use in the same loop body", `foreach ($xs as $x) { $r = new Repo(); $r->save(); }`, repoSave},
		{"assignment and use in the same try block", `try { $r = new Repo(); $r->save(); } catch (\Exception $e) {}`, repoSave},
		{"compact only reads", `$r = new Repo(); $a = compact('r'); $r->save();`, repoSave},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, f := flowIndex(t, tc.body)
			e, ok := edgeTo(idx, f, tc.want)
			if !ok || e.Confidence.String() != "exact" {
				t.Fatalf("want an exact %s edge to %s, got %v (ok=%v)", tc.want.kind, tc.want.to, e.Confidence, ok)
			}
		})
	}
}

func TestFlowSoundness_NonDominatingAssignmentsAreNoEvidence(t *testing.T) {
	cases := []struct {
		name, body string
		want       flowTarget
	}{
		{"if: class-string", `if ($c) { $cls = Foo::class; } return new $cls();`, fooConstruct},
		{"if: class-string static call", `if ($c) { $cls = Foo::class; } $cls::make();`, fooMake},
		{"if: object", `if ($c) { $r = new Repo(); } $r->save();`, repoSave},
		{"unbraced if", `if ($c) $r = new Repo(); $r->save();`, repoSave},
		{"else", `if ($c) { } else { $r = new Repo(); } $r->save();`, repoSave},
		{"elseif", `if ($c) { } elseif ($xs) { $r = new Repo(); } $r->save();`, repoSave},
		{"switch case", `switch ($c) { case 1: $r = new Repo(); $r->save(); break; }`, repoSave},
		{"switch, use after", `switch ($c) { case 1: $r = new Repo(); break; } $r->save();`, repoSave},
		{"match arm", `$r = match ($c) { 1 => new Repo(), default => null }; $r->save();`, repoSave},
		{"foreach", `foreach ($xs as $x) { $r = new Repo(); } $r->save();`, repoSave},
		{"for", `for ($i = 0; $i < $c; $i++) { $r = new Repo(); } $r->save();`, repoSave},
		{"while", `while ($c) { $r = new Repo(); } $r->save();`, repoSave},
		{"do-while", `do { $r = new Repo(); } while ($c); $r->save();`, repoSave},
		{"try", `try { $r = new Repo(); } catch (\Exception $e) { } $r->save();`, repoSave},
		{"catch", `try { } catch (\Exception $e) { $r = new Repo(); } $r->save();`, repoSave},
		{"finally", `try { } finally { $r = new Repo(); } $r->save();`, repoSave},
		{"short-circuit", `$c && ($r = new Repo()); $r->save();`, repoSave},
		{"ternary operand", `$c ? ($r = new Repo()) : null; $r->save();`, repoSave},
		{"nested in an expression", `$k = ($r = new Repo()); $r->save();`, repoSave},
		{"reassigned", `$r = new Repo(); $r = new Other(); $r->save();`, repoSave},
		{"reassigned class-string", `$cls = Foo::class; $cls = $c; new $cls();`, fooConstruct},
		{"closure assignment", `$f = function () { $r = new Repo(); }; $r->save();`, repoSave},
		{"closure, class-string", `$f = function () { $cls = Foo::class; }; new $cls();`, fooConstruct},
		{"use before the assignment", `$r->save(); $r = new Repo();`, repoSave},
		{"loop back edge", `while ($c) { $r->save(); $r = new Repo(); }`, repoSave},
		{"same name in a nested scope", `$r = new Repo(); $f = function () { $r = new Other(); $r->save(); }; $r->save();`, repoSave},
		{"goto", `goto L; $r = new Repo(); L: $r->save();`, repoSave},
		{"new static", `$r = new static(); $r->save();`, repoSave},
		// Bound outside the function body, or by the engine.
		{"superglobal", `$_SESSION = new Repo(); $_SESSION->save();`, repoSave},
		{"GLOBALS", `$GLOBALS = new Repo(); $GLOBALS->save();`, repoSave},
		{"http_response_header", `$http_response_header = new Repo(); file_get_contents('x'); $http_response_header->save();`, repoSave},
		// Scope features, whatever their spelling.
		{"EXTRACT", `$r = new Repo(); EXTRACT($c); $r->save();`, repoSave},
		{"qualified extract", `$r = new Repo(); \extract($c); $r->save();`, repoSave},
		{"parse_str", `$r = new Repo(); Parse_Str($c); $r->save();`, repoSave},
		{"string assert", `$r = new Repo(); assert('$r = new Other()'); $r->save();`, repoSave},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, f := flowIndex(t, tc.body)
			if e, ok := edgeTo(idx, f, tc.want); ok {
				t.Fatalf("no evidence must give no edge, got %s %s edge to %s", e.Confidence, tc.want.kind, tc.want.to)
			}
			// The reference did not disappear, and is not passed off as
			// known: a call with repository candidates is unattributed, a
			// construction of a computed class unresolved — never outside
			// the repository.
			_, out := idx.Unattributed(f)
			un := idx.UnresolvedOutgoing(f)
			if tc.want.kind == reference.KindConstruction {
				if un.Unresolved == 0 {
					t.Fatalf("the construction without evidence is not counted as unresolved")
				}
			} else if out == 0 {
				t.Fatalf("the call without evidence is not counted as unattributed")
			}
			for _, r := range un.References {
				if r.Reason == index.UnresolvedOutside {
					t.Fatalf("a reference without evidence is reported outside the repository: %+v", r)
				}
			}
		})
	}
}

// Constructor property evidence assumes the property holds the injected type
// once the constructor ran. When it has not (an early return or throw, a
// subclass constructor not calling this one) the property holds its default:
// only a default that cannot dispatch a call keeps the evidence.
func TestFlowSoundness_CtorPropertyDefaults(t *testing.T) {
	mk := func(decl, ctor string) string {
		return "<?php\nnamespace App;\nclass C\n{\n    " + decl + "\n    public function __construct(Repo $r, $flag = false) { " + ctor + " }\n    public function run() { return $this->p->save(); }\n}\n"
	}
	cases := []struct {
		name, decl, ctor string
		want             string // edge confidence to Repo.save, "" = none
	}{
		{"no default", "private $p;", "$this->p = $r;", "exact"},
		{"null default", "private $p = null;", "$this->p = $r;", "exact"},
		{"scalar default", "private $p = 0, $q = 'x';", "$this->p = $r;", "exact"},
		{"negative default", "private $p = -1;", "$this->p = $r;", "exact"},
		{"array default", "private $p = [];", "$this->p = $r;", "exact"},
		{"early return, null default", "private $p;", "if ($flag) { return; } $this->p = $r;", "exact"},
		{"enum-case default", "private $p = Status::Active;", "$this->p = $r;", ""},
		{"constant default", "private $p = DEFAULT_REPO;", "$this->p = $r;", ""},
		{"conditional assignment", "private $p;", "if ($flag) { $this->p = $r; }", ""},
		{"new static", "private $p;", "$this->p = new static();", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx := phpTreeIndex(t, map[string]string{
				"C.php":     mk(tc.decl, tc.ctor),
				"Repo.php":  "<?php\nnamespace App;\nclass Repo { public function save() {} }\n",
				"Other.php": "<?php\nnamespace App;\nclass Other { public function save() {} }\n",
			})
			if got := callerEdge(t, idx, `App\Repo.save`, `App\C.run`); got != tc.want {
				t.Fatalf("edge confidence = %q, want %q", got, tc.want)
			}
			if tc.want == "" {
				run := idx.FindSymbolsByQualified(`App\C.run`)
				if _, out := idx.Unattributed(run[0].ID); out == 0 {
					t.Fatalf("the call without evidence is not counted as unattributed")
				}
			}
		})
	}
}

// new self / new parent are lexical facts, unaffected by the flow rules.
func TestFlowSoundness_RelativeConstructionUnchanged(t *testing.T) {
	idx := phpTreeIndex(t, map[string]string{
		"P.php": "<?php\nnamespace App;\nclass P {}\n",
		"C.php": "<?php\nnamespace App;\nclass C extends P\n{\n    public function m($c) { if ($c) { return new self(); } return new parent(); }\n}\n",
	})
	for _, target := range []string{`App\C`, `App\P`} {
		if got := callerEdge(t, idx, target, `App\C.m`); got != "exact" {
			t.Errorf("construction of %s: %q, want exact", target, got)
		}
	}
}
