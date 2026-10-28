# Resolution model

Ark answers questions such as "who calls `Place`?" from a graph it builds
statically. This page explains how a reference in source becomes — or does
not become — an edge in that graph, what each confidence level promises, and
how to read the counts every graph answer carries.

The short version:

- Every resolved reference carries a **confidence**: `exact`, `strong`,
  `candidate` or unresolved.
- **Only a unique `exact` or `strong` resolution becomes a graph edge.**
  Callers, callees, change impact and context are built from edges.
- A `candidate` is a possibility Ark reports, never a fact it builds on.
- An empty edge list is not proof of absence. The `unattributed`,
  `unresolved` and `outsideRepository` counts say what Ark saw but could not
  turn into an edge.

The design rules behind this, and the tests that enforce them, are in
[ARCHITECTURE.md](../ARCHITECTURE.md) §2–3.

## From source to graph

1. A **language provider** parses one file (Tree-sitter grammars on a pure-Go
   runtime; HTML with golang.org/x/net/html; CSS with Ark's own CSS Syntax
   Level 3) and extracts declarations (symbols), references (calls,
   constructions, type uses, reads, writes, imports) and the evidence the
   language's own rules prove: import bindings, a receiver's declared type, a
   package name, a qualified identity. A provider never reads other files.
2. The **index builder** turns every declaration into a symbol with a stable
   `symbolId` and assigns each reference to the declaration that lexically
   contains it.
3. The **resolver**, which knows no language, grades each reference against
   the declarations of the repository and records its candidates, a
   confidence and the evidence it used.
4. The **graph** keeps the unique `exact` / `strong` resolutions as edges and
   counts everything else. Context, impact and the repository map read only
   the graph and what providers state about declarations (the repository
   map: a declaration's role and the unit its member scope names, e.g. a
   Terraform module call's local child); they never look names up
   themselves.

## Confidence

Confidence is an ordered set of evidence classes, not a probability. It is
never averaged or promoted; caps only lower it.

| Confidence | What Ark knows | What it does not know | Edge |
|---|---|---|---|
| `exact` | The one target, by the language's scoping as Ark models it: same container or file, an import or module binding (not one through a CommonJS export: see below), a qualified identity, a member of a type proven by declaration. | Nothing within the model. It is static evidence, not compiler proof. | yes, if unique |
| `strong` | The one target, by weaker evidence: the only declaration of the name in the same directory or package, or in the repository (for languages without package scoping); a receiver-name match; a declaration a language's search finds past a scope that may hold the name outside the repository (Java). | Whether something outside the repository shadows it. | yes, if unique |
| `candidate` | One or more plausible targets. | Which one, or whether an unseen target exists. | **never** |
| unresolved | No candidate. | The target. For a builtin, an external package or a computed name this is the correct answer. | never |
| `outsideRepository` | A flag on unresolved: authoritative evidence (an import, a qualified name, a declared type) places the target outside the repository. Evidence that reaches the repository and finds nothing Ark represents (an import of a repository file without the export, a member of an exported object literal) is unresolved without this flag. | — | never |

Several viable targets are always `candidate`, never a guess: Ark does not
pick the first or the most likely.

**CommonJS exports.** Ark keeps three things apart:

- A *lexical binding* — a name to the declaration its scope binds
  (`const fn = original`), also inside a CommonJS module — is `exact` as
  above.
- A *static export mapping* — which declaration an export statement names
  (`module.exports = { parse }`, `exports.a = f`) — is recorded from the
  source. It says nothing about the value at run time.
- An *export value resolution* — a reference that reaches a declaration
  through a CommonJS export (`require("./m").parse()`, an `import` of a
  CommonJS module, a re-export of one, a class extended through one) — is
  `candidate` at most. The exports object is a plain object that code may
  change at run time by routes no static rule closes, so Ark does not claim
  the value an importer reads is the exported declaration. The candidate
  names that declaration: a usable reference, not a proof of run-time
  identity.

ESM and TypeScript exports are live bindings and keep the confidence their
evidence proves. No confidence claims anything about arbitrary run-time
behaviour (`eval`, reflection, monkey-patching).

### Evidence

Every resolution names the evidence it rests on. Graph answers show it in the
`evidence` field, for example:

```json
{ "from": "Handler.Checkout", "to": "Place", "kind": "calls", "confidence": "exact",
  "evidence": "orders.Place declared in imported package example.com/shop/orders (orders)" }
```

| Evidence kind | Meaning | Typical confidence |
|---|---|---|
| `same_lexical_scope` | Declared in the same container | exact |
| `same_file` | Declared in the same file | exact |
| `module_binding` | Bound by an import statement or a require to a declaration of a repository file (TypeScript, TSX, JavaScript, Python) | exact; candidate through a CommonJS export |
| `explicit_import` | Declared in an imported package (Go), or in a file under an imported path (JavaScript) | exact |
| `qualified_identity` | The language fixed the fully qualified name (PHP namespaces, Terraform addresses, Rust and Zig paths) | exact; strong through a module build.zig maps (Zig); candidate through a comptime choice between namespaces (Zig) |
| `receiver_type` | Member of the receiver's declared type | exact or strong |
| `receiver_chain` | Member of the type the receiver is built as, derived through declarations (Go: `x := NewServer(); x.Run()`) | strong |
| `promoted_member` | Member promoted through an embedded type (Go embedding) | strong (candidate beside an embedded type of unknown members) |
| `member_scope` | Member of a scope the declaration states (Terraform module outputs and inputs) | exact |
| `language_lookup` | Found by the language's own search as the provider states it — enclosing scopes, imports, the package, supertypes' members, overloads by argument count and by primitive versus reference argument types (Java; never one reference type for another) | exact; strong past a scope the repository may not hold (another file of the package outside the reference's source root, an on-demand import, a supertype outside the repository) or in another source root; candidate for overloads the arguments do not tell apart |
| `same_package` | Only declaration of the name in the file's package or directory | strong |
| `unique_repo_match` | Only declaration of the name in the repository, within the file's language | strong (candidate when capped) |
| `qualified_receiver` | Receiver name matches the declaring type's name | strong (candidate when capped) |
| `package_scope` | Go package scoping found no declaration | unresolved |
| `candidate_set` | Several declarations share the name | candidate |
| `untyped_receiver` | Cap: the receiver's type is not proven | lowers to candidate |
| `module_scope` | Cap: the file is a module and the name is neither declared nor imported | lowers to candidate |
| `confidence_cap` | Cap stated by the provider (for example, a shadowed name) | lowers |
| `target_kind` | The reference can denote only some kinds of declaration | narrows |
| `identity_only` | The file's names resolve only by qualified identity | unresolved without identity |
| `dynamic_name` | The name is computed at run time | unresolved |

## Reading a graph answer

Every observed reference inside a symbol is exactly one of: an **edge**,
**unattributed** (it may denote a repository symbol but is not a resolved
edge), **unresolved** (no repository symbol can be its target) or
**outsideRepository** (proven external).

```json
{
  "symbol": "Checkout",
  "edges": [ { "to": "Place", "confidence": "exact", "...": "..." } ],
  "unattributed": 1,
  "candidates": [
    { "symbol": "Log.Record", "file": "audit/audit.go", "confidence": "candidate",
      "evidence": "only symbol named \"Record\" in repository", "references": 1 }
  ],
  "unresolved": 0,
  "outsideRepository": 0
}
```

Here `Checkout` calls `orders.Place` (an `exact` edge) and
`h.audit.Record(…)`. The second call goes through a struct field whose type
Ark does not propagate, so `Log.Record` is only a candidate and is counted as
unattributed: the edge list alone would miss it.

| Field | Meaning |
|---|---|
| `unattributed: 0` | No reference Ark observed may be a missing repository edge. |
| `unattributed > 0` | Some references may involve repository symbols but are not edges; `candidates` samples them (at most 10, with totals). |
| `unresolved` | References no repository symbol can be the target of: builtins, external packages, computed names. |
| `indexDiagnostics` | Present only when some files could not be fully analyzed; references in that code are in no count. |

`unattributed: 0` with no diagnostics still does not prove completeness:
files of unsupported formats are not examined, and a provider may not observe
every construct (for example, a function passed as a value is not a call).

## Name spaces: names never cross languages

Name-based rules consider only declarations of the referencing file's
language. A provider may declare its language a dialect of another; TSX is a
dialect of TypeScript, so `.ts` and `.tsx` files share one name space. No
other languages share names.

```python
# py/app.py
def run():
    GoOnly()        # unresolved — the Go function GoOnly is never a candidate
```

Explicit evidence names its targets itself. A TSX file importing a TypeScript
module resolves through the binding:

```json
{ "from": "View", "to": "main", "kind": "calls", "confidence": "exact",
  "evidence": "\"main\" bound by import (web/view.tsx ← \"./app\" → web/app.ts exports \"main\")" }
```

TypeScript module resolution lists only `.ts` / `.tsx` files and JavaScript
module resolution only JavaScript files, so TypeScript and JavaScript files
do not resolve each other's names. The one relation between two languages
is path evidence, which names its target's file: an HTML page's
`<link rel="stylesheet">` and `<script src>`, a CSS Modules class that a
JavaScript or TypeScript import of the `.module.css` file uses.

## Go: package scope

Go files resolve by Go's scoping rules, not by spelling:

- An unqualified name is a declaration of the file's own package (same file:
  `exact`; another file of the package: `strong`) or of a dot-imported
  package.
- `pkg.Name` is looked up in the package the import path names: exported,
  non-test declarations only. An import of a package outside the repository
  is `outsideRepository` — but only when nothing in the repository could be
  it: an import path ending in a repository directory, or naming the root
  package, that no module prefix establishes stays unresolved without that
  claim.
- A name a local declaration or a type parameter shadows at its use is never
  resolved to a package-level declaration; a variable named like an import
  (`var atomic T; atomic.Add()`) is a variable, not the import.
- Besides calls and constructions, Go files record type uses (`func f(r
  *Repo)`, `var x pkg.T`, `[]T{}`, a type argument, an embedded field), value
  references (a constant, a package variable, a function passed as a value,
  `pkg.Var`, a method expression `T.M`) and writes (`x = v`, `x++`,
  `pkg.Var = v`, a field write `v.f = x`). Type uses form `uses_type` edges
  and value references `references` edges; writes form none.
- No repository-wide name rule applies: a builtin, a local function value or
  an external declaration is never "the only declaration with that name"
  elsewhere.

```go
// a/a.go
package a
type cancel struct{}            // unrelated

// c/c.go
package c
func Run() {
	cancel := func() {}
	cancel()                    // no edge to a.cancel
}
```

Import paths are mapped to repository directories by suffix, under a module
prefix that at least two import paths share (the module path itself counts
when the root directory's package is named after its last element), or the
only candidate prefix that ends in the repository directory's name. A prefix
without a host (`encoding` of `encoding/json`, `log`) may be the standard
library's: only the repository directory's own name establishes it.

A method's receiver is typed by declarations, never by guessing:

- A receiver whose type is written down locally (a parameter, the method
  receiver, `x := &T{}`) is a member of that type.
- Otherwise its type is derived from what the code names: a function's or
  method's declared result (`x := NewServer(); x.Run()`, `pkg.Open().Close()`),
  a struct field's declared type (`s.repo.Find()`, in any file), an element
  of a slice or map (`for _, it := range items`, `m[k].Run()`), a type
  assertion or `new(T)`, a package variable's type or initializer. Each step
  must name exactly one declaration of the repository: a name declared in
  build-tagged variants, a declaration outside the repository or a result
  typed by a type parameter leaves the receiver untyped (`candidate`). These
  resolutions are at most `strong` (`receiver_chain`).
- A method promoted through embedding (`type Outer struct{ Inner }`,
  `o.M()` with `M` declared on `Inner`) resolves to `Inner.M` — the method
  the type checker selects: the shallowest embedding depth wins, two at that
  depth are ambiguous (`candidate`), a field hides a method, and an embedded
  type outside the repository above that depth leaves the call unresolved
  (`promoted_member`).
- A call through an interface (`r.Read()` with `r Reader`) resolves to the
  interface's method — its static target. The concrete method run-time
  dispatch picks is not guessed.
- Method values (`f := t.Run`) and method expressions (`T.Run`, `(*T).Run`,
  `pkg.T.Run`, `T[int].Run`) resolve the same way; a field read (`t.name`)
  is no reference.

See [Language support](language-support.md#go).

## Ambiguity, identity and `symbolId`

Every declaration is its own symbol with a 64-bit `symbolId`, even when one
file declares a name twice. Target tools (`get_context`, `get_callers`,
`get_callees`, `get_relations`, `analyze_change_impact`) refuse an ambiguous
name and list the declarations:

```text
Ambiguous symbol "init" — 2 matches found. Narrow with filePattern, retry with one of the qualified names below, or pass its symbolId to select one declaration:
  init  (function)  main.go:3  symbolId=3820bb024b6ef92d
  init  (function)  main.go:5  symbolId=c1473e6252724f29
```

Pass `symbolId` (or `filePattern`) to select one. A `symbolId` depends on the
indexed `path`; IDs from `search_context` are valid with `path: "."`.

Should two distinct declarations ever produce the same `symbolId`, Ark does
not build the index (index tools answer `SymbolID collision`) rather than
merge them.

## What changed in v6.0.0

v6.0.0 removed classes of wrong edges that earlier versions produced. Each
case below now resolves as shown; v5.0.1 produced a `strong` edge.

| Case | v5.0.1 | v6.0.0 |
|---|---|---|
| A Go call to a name declared once, in another package (`cancel()` above) | `strong` edge to the other package's declaration | no edge |
| A Go call to a builtin (`append`) when the repository declares an `append` | `strong` edge | no edge |
| A Python, JavaScript or PHP call to a name declared only in another language | `strong` edge to that language's declaration | unresolved |
| A Go method call on a value of unknown type | candidates from every language | candidates from Go only |

Measured against an independent `go/types` oracle on three Go repositories,
Strong/Exact resolutions the type checker contradicts went from 4,221 to 0;
see the [v6.0.0 release notes](../.github/release/NOTES-v6.0.0.md) for the
method and numbers.

## What Ark does not do

- Type inference or control- and data-flow analysis. (Go receivers are typed
  only through declarations' written types: a declared result, field or
  variable type — never from uses, assignments or generic instantiation.)
- Compiler-equivalent module resolution: `tsconfig` paths, Composer / PSR-4
  autoloading and `package.json` are not interpreted.
- Framework semantics (dependency-injection containers, routing, decorators).
- Execute any code, build tool or package manager from the repository.

Per-language limits: [Language support](language-support.md).
