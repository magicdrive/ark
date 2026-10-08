# Ark v5.0.0 — release notes (draft)

> Draft for the maintainer. Every statement below was measured during release
> validation; nothing here is a general performance or token guarantee.

## Highlights — trustworthy code intelligence

Ark's dependency answers (`get_callers`, `get_callees`, `analyze_change_impact`,
`find_references`, …) now distinguish what the analysis proves from what it
could not analyze, and several classes of wrong edges are gone.

- **Terraform / HCL support (new).** `.tf` and `.tfvars` files are indexed:
  resources, data sources, variables, outputs, locals, modules and providers,
  with references between them, local module calls resolved into the child
  module (including module arguments → the child's variables), and dynamic or
  remote modules reported instead of guessed.
- **Fewer false Exact / Strong edges.** Measured on real repositories:
  - Go: calling an element of a function slice or map (`handlers[i](x)`) was an
    Exact/Strong edge to a same-named function or to the variable; it is no
    edge now.
  - Go: methods on generic types (`func (s *Set[T]) Add`) were attributed to the
    wrong receiver (`T.Add`, `string.add`); they now get their real receiver.
  - TypeScript: type parameters of nested signatures, mapped-type keys
    (`[K in …]`), `infer` names and the `bigint` keyword were type references
    that could resolve to a same-named repository type (zod: 10 Exact edges to a
    function named `bigint`); they are not references now.
  - hashicorp/terraform: a parser failure had hidden 29 functions of one file;
    calls from other files resolved by name to same-named functions in a
    different package (Strong). Both are fixed.
- **More of the code is analyzed.** Ark's parser (gotreesitter) rejects some
  valid code; Ark now retries with its alternative parser routes and keeps a
  tree only if it parses cleanly. Generic calls the parser misreads (Go
  `F[T](x)`, `pkg.F[T](x)`, `T[X]{…}`; TypeScript `f<T>(x).m`) are read by the
  languages' own rules.
- **Analysis limits are visible.** New MCP tool `get_diagnostics` lists the
  regions Ark could not analyze (`parse_error`, unreadable files); graph tools
  carry an `indexDiagnostics` summary when the index has any. No diagnostics is
  not a completeness claim.

## Compatibility

- **CLI:** no flag was removed or changed; `ark syntax/symbol --lang` also
  accepts `terraform`. Exit codes and output formats are unchanged.
- **MCP:** same protocol version (2024-11-05). Tools: 19 → 20 (`get_diagnostics`
  added); no tool, required argument or input property was removed. New result
  fields are additive.
- **Setup:** `ark setup` for claude, cursor, codex, cline, copilot-vscode and
  copilot-cli behaves as in v4.4.x.
- **Results change meaning where they were wrong.** Edges listed above disappear;
  recovered declarations and generic calls add edges. Impact reports follow.
- **Cache:** the Go, TypeScript, PHP, JavaScript, Python and Terraform extractors
  changed their cache versions, so the first run after upgrading re-extracts
  every file. Old entries in `.ark/index` are never used again but are not
  deleted; removing `.ark/index` reclaims the space.

## Known limitations

- A TypeScript generic call whose type arguments contain function types,
  conditional types or computed keys is not recovered when the parser misreads
  it: the call is missing, never wrong.
- TypeScript types in interface bodies and overload signatures, and references
  inside string-named methods, are not observed.
- Terraform: `.tf.json`, value flow and some meta-arguments are not analyzed.
- See `internal/conformance/IMPROVEMENTS.md` for the full list.

## Validation

- Differential testing against the reference Tree-sitter runtime (9,100 files of
  14 public repositories) and against the languages' own front ends: Go
  (`go/ast` + `go/types`: Ark, hashicorp/terraform, cobra — no false call or
  construction reference) and the TypeScript compiler 5.6.3 (zod: no false
  reference; 1,000,094 generated type-argument cases: every one Ark accepts
  the compiler parses the same way).
- Before/after comparison of 14 repositories; every changed edge classified and
  every new Exact/Strong edge checked against the source.
- CI gates on every push and before every release: tests, race detector,
  staticcheck, fuzz smoke (8 targets) and the TypeScript compiler
  differential; mutation testing of the soundness rules.
- Release artifacts (macOS, Linux, Windows; amd64, arm64, 386) built with
  GoReleaser in snapshot mode, checksums and Homebrew formula verified, and
  the macOS amd64 binary exercised end to end (CLI, MCP JSON-RPC session,
  `ark setup` for every client in an isolated HOME, cache upgrade from v4.4.1).
