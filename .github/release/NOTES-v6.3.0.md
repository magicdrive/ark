# Ark v6.3.0 — release notes (draft)

> Draft for the maintainer. Every number below was measured during the
> v6.3.0 work (macOS x86_64, Go 1.27.2) on the corpora named; nothing here is
> a general performance guarantee or a proof of correctness.

## Overview

- **Rust** (new, support level `graph`): declarations, associated items,
  `use` and re-exports, `macro_rules!`, trait implementations, resolved by
  Rust's own path rules within a crate. Ambiguity stays visible (candidates,
  unresolved references) rather than guessed. See
  [Language support](../../docs/language-support.md#rust).
- **Parsing is faster and its cost predictable** (improved, all languages):
  a file the parser rejects no longer spends tens of seconds in a fallback
  route that then fails (Go and Rust), and small files parse about ten times
  faster.
- **`get_callers` says what a caller is** (changed, all languages — see
  [Compatibility](#compatibility)).

## Compatibility

**MCP `get_callers` reverse-edge kinds** change for every language. A
reverse edge used to be `called_by` for any reference that was not a value
reference or a Terraform dependency; it now reports what the reference is:

| Reference | Before | Now |
|---|---|---|
| call, construction | `called_by` | `called_by` |
| trait / interface implementation | `called_by` | `implemented_by` |
| inheritance (`extends`) | `called_by` | `extended_by` |
| trait use (PHP `use T;`) | `called_by` | `trait_used_by` |
| type use | `called_by` | `type_used_by` |
| import | `called_by` | `imported_by` |
| value reference, Terraform `depends_on` | `referenced_by`, `depended_on_by` | unchanged |

Consumers that treat every `get_callers` edge as a call, or match the
string `called_by`, should review this. The edges themselves — which symbols
are callers — are unchanged; when one symbol both calls and, say, names
another as a type, its edge reports `called_by`.

Extraction caches of earlier versions are not reused for Rust (a new
provider); other languages' caches stay valid.

## Rust

- **Identity:** a declaration is named by its path in its crate
  (`crate::storage::Repository::new`); an inherent method and a trait
  implementation's method are different symbols
  (`<crate::storage::Repository as crate::storage::Store>::save`); each crate
  of a workspace — another package, a binary, a test, an example — is its own
  identity space. The crate comes from Cargo's default layout; `Cargo.toml`
  is not read.
- **Not resolved, by design:** method calls on values (`v.m()`), paths into
  another crate of the workspace, names a macro, a derive or an attribute
  macro may generate, glob re-exports through another file; a `use` whose
  reading depends on the edition is a candidate.
- **Measured against the compiler** (rustc 1.58 save-analysis, 71 crates.io
  packages without dependencies, build scripts or procedural macros): of the
  references Ark resolved to one declaration that the compiler also
  recorded, 12,926, none disagreed with the compiler; references the
  compiler records in no configuration it compiled were not counted. Recall
  of the compiler's references Ark observes (method calls apart): 79.1%.
  Procedure: `docs/scripts/rust-oracle.sh` (opt-in, not in CI).
- **256 crates.io packages** (4,810 files): indexed deterministically, no
  edge across crates, 48 s in all.

## Parsing

- The forest fallback route compared its alternatives across every comment
  before them — cubic in a run of comments after an ambiguous prefix (Rust:
  an attribute and doc comments), 37 s on a 44 KB file, spent in full before
  failing. For Go and Rust a probe on a copy with the comments blanked now
  decides first whether the route can succeed; it never returns a tree of
  its own. quick-error 1.2.3: 38,968 ms → 430 ms cold; serde 1.0.104:
  19,987 ms → 1,919 ms ([Performance](../../docs/performance.md#rust)).
- The production route reuses parsers (gotreesitter's ParserPool):
  building one cost more than parsing a small file.
- Parse results are unchanged: 18,103 files of Go, Rust, TypeScript, TSX,
  JavaScript, Python, PHP and Terraform give the same route and tree as
  before (`TestProbe_Corpus`, opt-in).
