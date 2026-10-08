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

## Q8 — Parser fidelity

gotreesitter is a reimplementation of the Tree-sitter runtime. Measured
against the reference runtime with the same grammar commits (differential
testing over real corpora), its trees differ in two ways:

- **with an error the reference does not report** — a valid region is not
  analyzed and reported as `parse_error`. `tsparse` recovers the cases its
  candidate route parses; the rest stay visible as diagnostics (e.g. a PHP
  destructuring assignment: `TestKnownParserDefect_PHPDestructuring`).
- **without any error** — an ambiguous construct resolved differently (Go
  generic instantiation vs. index expression, TypeScript `as` / `satisfies`
  / `<`). Nothing reports these, so references from them may differ.

Target: trees identical to the reference runtime on valid source. Measure
declarations with `go test ./internal/languages/golang -run TestFidelity -v`
(and `ARK_GO_FIDELITY_ROOTS` for external corpora); whole-tree comparison
needs the reference runtime and is not part of the test suite.

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

## Q7 — Observing computed-name calls

A call whose callee name is computed at run time should be observed as a
`language.ReferenceDraft` with `Dynamic` set, so that completeness reports it
as an unresolved reference of its container instead of the call silently
not existing ("Unknown is not empty"). Probe (per language): one such call —
Go `m["k"]()`, TypeScript/TSX/JavaScript `o[k]()`, Python `getattr(o, n)()`,
PHP `$o->$m()`.

Target: exactly one Dynamic reference, with no name fabricated from the
expression. Only a Dynamic reference is safe here: a guessed name would be
matched against declarations.

Terraform has no such syntax: function names are static built-ins and a
computed index (`local.m[var.k]`) selects a value, not a declaration. Its
probe has no computed-name call, so 0 is its correct count. Q3/Q4 do not
apply to it either: Terraform declarations have no members (an attribute
such as `.id` is not a declaration), and a check-scoped data source is its
only symbol with `Parent`.
