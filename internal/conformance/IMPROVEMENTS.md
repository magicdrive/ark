# Provider Conformance — Improvement Candidates

This file records **quality requirements that are NOT yet satisfied by all
providers**. They are intentionally *not* asserted as failing tests in the
conformance suite, because PR 1 (Language Registry Consolidation + Conformance
Suite + Baseline Fixtures) has a strict **"existing behavior unchanged"**
contract. Fixing any of these is behavior-changing and belongs to a later PR.

The non-failing audit `TestQualityCandidates` in this package reports the
current status of each candidate at runtime (`go test -run TestQualityCandidates -v`).

Status measured against providers: go, typescript, tsx, javascript, python, php.

| ID | Candidate | go | typescript | tsx | javascript | python | php |
|----|-----------|----|-----------|----|-----------|--------|-----|
| Q1 | Partial extraction from broken source | partial | none | none | none | partial | partial |
| Q2 | Diagnostic emitted on broken source | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ |
| Q3 | Class/container member methods extracted as symbols | ✓ (receiver) | **✓** | **✓** | ✗ | ✗ | **✓** |
| Q4 | `SymbolDraft.Parent` populated for nested symbols | ✗ | **✓** | **✓** | ✗ | ✗ | **✓** |
| Q5 | MCP index/relations handle `.tsx` (registry-wide) | n/a | n/a | ✅ CLOSED | n/a | n/a | n/a |
| Q6 | `IncludeTests` recognises the language's test files | ✓ | ✓ | ✓ | partial (`.js` only) | ✓ | ✅ CLOSED (Phase 6) |

### Q3 / Q4 — PHP status (PHP-3)

PHP **satisfies Q3 and Q4**: it extracts class/interface/trait/enum members
(methods, constructors, properties, class constants, enum cases, constructor-
promoted properties) as symbols with `Receiver` set to the declaring type, and
populates `SymbolDraft.Parent` with the container's qualified name so the index
builds `symbol.ParentQualified`. This is verified end-to-end through the real
Repository Index (`internal/languages/php` integration tests). Q3/Q4 remain
open for JS/Python — their status is unchanged and PHP did not alter
their providers.

TypeScript and TSX now **also satisfy Q3 and Q4** (TypeScript Intelligence
project): class / interface members are first-class symbols with
`Qualified = Class.member`, `Parent = Class`, `Receiver = Class`; a get/set
pair is one `KindProperty` symbol. Q1/Q2 remain open for TS/TSX.

### Q6 — test-file detection: CLOSED for PHP (Phase 6)

Resolved by a language-neutral test classifier, `internal/testfiles`, the
single authority on "is this file a test?" for the Context Engine
(`IncludeTests`), impact analysis, the repository map, structural search
(`excludeTests`) and the skill analyzer — no consumer keeps naming rules of its
own (cross-consumer contract tests in each package, over the shared paths of
`internal/testfiles/testfilestest`). Conventions, by file name and directory
only:

- Go `*_test.go`; TypeScript/TSX `*.test.ts(x)`, `*.spec.ts(x)`; JavaScript
  `*.test.js`, `*.spec.js`; Python `test_*.py`, `*_test.py`;
- PHP `FooTest.php` (PHPUnit; a class named `Test` is not a test) and any file
  under a `tests/`, `Tests/` or `test/` directory.

Fixture data under a `testdata/` directory is a separate question,
`testfiles.IsTestData`, asked by the consumers that leave fixtures out
(impact, repository map, search).

PHP now takes part in the `IncludeTests` contract: a test caller is left out
of the Context unless `IncludeTests` is set, and stays a caller in the graph
and its completeness (`TestContext_IncludeTestsPHP`,
`TestTestCallers_SameContractAcrossLanguages`). The earlier freeze
`TestContext_IncludeTestsNoOpForPHP` was replaced.

Still open: JavaScript test files with the `.jsx`, `.mjs` and `.cjs`
extensions, and conventions of particular runners (e.g. Deno's
`foo_test.ts`), are not recognised.

## Q1 — Partial extraction from broken source

Plan §5 (Provider Conformance Suite → Broken source) and §14/§24 require that a
provider "takes the symbols it can" from syntactically broken source.

Current behavior: for a source where a broken declaration is followed by a
valid one (`func Broken( {` then `func Good() {}`):

- **go**: recovers 1 symbol but drops the trailing `Good` (Tree-sitter error
  node swallows the remainder).
- **python**: recovers 2 symbols (including the trailing valid `good`).
- **typescript / tsx / javascript**: recover **0** symbols.

Target: every provider recovers the trailing valid declaration.

## Q2 — Diagnostics on broken source

Plan §5 ("diagnosticを返せる") and §24. Currently **no provider** emits any
`language.Diagnostic` for broken source — breakage is silent. The extraction
simply contains fewer symbols with no signal that parsing degraded.

Target: emit at least one `Diagnostic` (e.g. `SeverityWarning`) when the parse
tree contains error nodes.

## Q3 — Member method symbol extraction

Measured via the valid corpus: only **go** extracts methods as symbols
(represented as a symbol with `Receiver` set, e.g. `User.Save`). For
`typescript`, `tsx`, `javascript`, and `python`, methods declared inside a
class/object are **not** extracted as symbols at all — only the top-level
class/interface/function/const symbols appear.

Consequence already visible in the Phase 0 baseline (`internal/golden`): the
Python index resolves almost no method-level edges because the method symbols
do not exist to resolve to.

Target: extract member methods as symbols with a stable qualified name
(`Class.method`) for TS/TSX/JS/Python, consistent with Go.

## Q4 — `SymbolDraft.Parent` population

No provider populates `SymbolDraft.Parent`. Nesting is currently encoded only
via `Qualified` (and `Receiver` for Go methods). The plan's contract item
"Parent refers to qualified semantic container" is therefore vacuously true
(nothing to check) rather than actively satisfied.

Target (design decision for a later PR): either populate `Parent` with the
enclosing symbol's qualified name, or formally document `Qualified` as the
single source of truth for nesting and drop `Parent`.

## Q5 — MCP index/relations do not handle `.tsx` — CLOSED

**Status: CLOSED.** `tsxCompatExclusion` and all related compatibility code have
been removed; `defaultProviders()` now returns the full canonical registry, so
indexing, `get_relations`, graph, and context all handle `.tsx` through the same
path as every other language — identical to `find_references`. Regression tests:
`TestIndexHandlesTSX` and `TestTSXUnifiedAcrossTools` (`internal/mcp`).

The original gap, for the record:

Discovered during Registry Consolidation (PR 1 / Phase 1). The MCP
`defaultProviders()` set — used by repository indexing and `get_relations` —
historically omitted the TSX provider, while `find_references` included it. So
`.tsx` files are visible to `find_references` but invisible to indexing and
relation queries.

The canonical `language.Registry` (in `internal/languages`) treats `tsx` as a
first-class language. To keep PR 1 strictly behavior-preserving, `tsx` is
filtered out at the MCP indexing layer via `tsxCompatExclusion`
(`internal/mcp/tools.go`) — an implementation-level shim, **not** a statement
about the language's capability or `SupportLevel`.

Resolution: `tsxCompatExclusion` was removed; index/relations use the full
registry, making `.tsx` handling consistent across all MCP tools. The PR-1
behavior lock (`TestTSXCompatBehavior`) was replaced by the regression tests
noted above.

## PHP (PHP-1 … PHP-8) — fixed vs. deferred

### Fixed / delivered
- Symbols, members, containment (Q3/Q4), imports/aliases/grouped use.
- References: function/static/instance/`$this` calls, construction, class-constant reads, type references.
- Typed relations: `extends` / `implements` / `uses_trait` → `EdgeExtends` / `EdgeImplements` / `EdgeUsesTrait` (D4), carried Provider → Graph.
- Graph Builder hardening: unknown `ReferenceKind` never fabricates an edge (`default: continue`); construction is an explicit call-like edge.
- Receiver-aware resolution (language-neutral): an explicit **type** receiver (`User::m()`) constrains member resolution so it can never fall back to an unrelated same-name member (removed a false Exact); variable/`$this`/dynamic receivers unchanged.
- Context: typed relation reason labels (`extends`/`implements`/`uses_trait`) via `EdgeKind`; type-dependency path hardened to exact-name + unique (no ambiguous/prefix fabrication).

### Deferred (precision / future architecture — not correctness bugs)
- **Resolver**: `ReferenceKind × SymbolKind` compatibility; no general type inference (receiver types come only from declarations: parameters, constructor-injected properties). Delivered since: qualified identity (namespace / `use` / alias aware), constructor-injection receiver evidence (Phase 4), and structural inherited-member and trait-member lookup — own → traits → nearest parent → interfaces, stopping at unknown participants (Phase 5).
- **Context**: unify the type-dependency path onto resolver-validated graph edges; reverse typed-relation context (`extended_by` / `implemented_by` / `trait_used_by`); relation-aware ranking weights (current scoring is sufficient on measured scenarios).
- **Test detection**: closed — Q6 above (language-neutral `internal/testfiles`).
- **Framework awareness** (Laravel/Symfony/Doctrine/PHPUnit): out of scope — a future Framework Evidence Provider, not PHP language support.

These are honest degradations (Candidate/Unresolved/omitted), never false Exact/Strong or fabricated edges.
