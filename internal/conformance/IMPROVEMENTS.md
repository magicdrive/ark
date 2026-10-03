# Provider Conformance — Improvement Candidates

This file records **quality requirements that are NOT yet satisfied by all
providers**. They are intentionally *not* asserted as failing tests in the
conformance suite, because PR 1 (Language Registry Consolidation + Conformance
Suite + Baseline Fixtures) has a strict **"existing behavior unchanged"**
contract. Fixing any of these is behavior-changing and belongs to a later PR.

The non-failing audit `TestQualityCandidates` in this package reports the
current status of each candidate at runtime (`go test -run TestQualityCandidates -v`).

Status measured on 2026-10-03 against providers: go, typescript, tsx,
javascript, python.

| ID | Candidate | go | typescript | tsx | javascript | python |
|----|-----------|----|-----------|----|-----------|--------|
| Q1 | Partial extraction from broken source | partial (1) | none (0) | none (0) | none (0) | partial (2) |
| Q2 | Diagnostic emitted on broken source | ✗ | ✗ | ✗ | ✗ | ✗ |
| Q3 | Class/container member methods extracted as symbols | ✓ (receiver) | ✗ | ✗ | ✗ | ✗ |
| Q4 | `SymbolDraft.Parent` populated for nested symbols | ✗ | ✗ | ✗ | ✗ | ✗ |
| Q5 | MCP index/relations handle `.tsx` (registry-wide) | n/a | n/a | ✅ CLOSED | n/a | n/a |

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
