# Provider Conformance — Open Gaps

This file lists provider quality requirements that are **not yet met by every
provider**. They are deliberately not asserted by `RunContract`, which asserts
only what every provider already satisfies, so a failure there is always a
regression.

Rules for this file:

- **Status is measured, not written down.** Run
  `go test ./internal/conformance -run TestQualityCandidates -v`; the audit
  (`TestQualityCandidates`) reports each gap per provider. This file keeps no
  per-provider status table — a hand-maintained one went stale.
- **Open gaps only.** When a gap closes for every provider, promote it into
  `RunContract` as a failing assertion and delete its entry here. Git keeps
  the history.
- Durable design decisions and invariants belong in `ARCHITECTURE.md`;
  per-language user-facing limits in `README.md` ("Language Support").
- Closing a gap changes behavior: it may change golden snapshots and must not
  weaken any invariant in `ARCHITECTURE.md` (no false Strong/Exact, no
  fabricated edge). Bump the provider's `CacheVersion`.

## Q1 — Partial extraction from broken source

A provider should take the symbols it can from syntactically broken source.
Probe (per language): a broken declaration followed by a valid one, e.g.
`func Broken( {` then `func Good() {}`.

Target: every provider recovers the trailing valid declaration. Today the
Tree-sitter error node swallows the remainder for several grammars.

## Q2 — Diagnostics on broken source

No provider emits a `language.Diagnostic` when the parse tree contains error
nodes: breakage is silent, and the extraction simply has fewer symbols.

Target: at least one `SeverityWarning` diagnostic when the tree has errors.
"Unknown is not empty" applies here too — silent degradation looks like a
complete answer.

## Q3 — Member symbols (JavaScript, Python)

JavaScript and Python do not extract class/object members as symbols, so
member-level references have nothing to resolve to (TypeScript/TSX, PHP and
Go do). Target: members as symbols with `Qualified = Class.member`,
`Parent = Class`, `Receiver = Class`, as TypeScript and PHP do.

## Q4 — `SymbolDraft.Parent` (JavaScript, Python; Go by design)

`Parent` (the enclosing symbol's qualified name) is what the resolver uses as
lexical containment proof. JavaScript and Python leave it empty (follows from
Q3). Go has no lexical member containment: its methods attach by
`Receiver` within the declaring type's directory, which the resolver treats as
weaker evidence (capped at Strong, `resolver.membersOf`). That is the intended
Go model, not a gap.

## Q6 — Test-file recognition (remaining)

`internal/testfiles` is the single authority on test files. Not yet
recognised: JavaScript tests with `.jsx`, `.mjs` and `.cjs` extensions (the
JavaScript provider indexes these files), and runner-specific conventions such
as Deno's `foo_test.ts`.
