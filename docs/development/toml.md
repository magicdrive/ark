# TOML, Cargo and pyproject support: design record and evaluation

This page records why TOML support is built the way it is, what was
measured, and how. The rules themselves are in the headers of
`internal/languages/toml/provider.go`,
`internal/languages/toml/tomldoc/tomldoc.go` and
`internal/languages/toml/manifest/manifest.go`.

## Parser

TOML 1.0 is the reference. Both libraries below read TOML 1.1 as well, and
so does Ark: what differs — `\e` and `\xHH` escapes, times without seconds,
inline tables over several lines or with a trailing comma — a TOML 1.0
parser rejects, and Ark reads without a diagnostic. Unicode bare keys
(a TOML 1.1 draft, not in 1.1.0) are rejected by both, and by Ark.

Candidates, measured on 2026-10-31 (Go 1.27.3, darwin/amd64):

| | BurntSushi/toml v1.6.0 (already a dependency) | pelletier/go-toml/v2 v2.4.3 |
|---|---|---|
| License, maintenance | MIT; v1.6.0 | MIT, no dependencies; v2.4.3 of 2026-07-05; govulncheck: nothing |
| Positions | `ParseError.Position` (line, column, byte offset, length) of an error only; `MetaData` has the keys in order but no position (`keyInfo.pos` is unexported) | `unstable.Parser`: every key part and key/value with its byte range (`Node.Raw`), string elements too; table headers and arrays have none |
| toml-test (the suite BurntSushi/toml ships, commit b54f9ffc of 2025-12-16), invalid files of TOML 1.0 | accepts 17 (e.g. `[a.b.c]` then `[a] b.c.t = 1`, `a.b = 1` then `[a]`, `a = {b = {}, b.c = 1}`, an offset minute of 60) | its decoder rejects all |
| Time | linear in tables; quadratic in the nesting of values (2000 levels: 0.73 s) and in the parts of a dotted key | the parser linear; the decoder quadratic in tables (32000 tables: 3.1 s) and in the keys of one inline table (32000: 1.9 s) |

**Chosen.** go-toml/v2's `unstable.Parser` for the document — it is the only
one that gives positions —, with a table builder of Ark's own on top, and
BurntSushi/toml's decoder as the last check (impossible dates, invalid
escapes and code points). The builder does what neither library returns:

- the tree TOML's table rules define (a header, a table a header implies,
  dotted keys, an inline table, an array of tables), with a problem per
  duplicate key and per redefined table (decoders stop at the first) — and
  strict where BurntSushi/toml is lenient (the 17 files above);
- where a table header and an array begin and end (the parser gives the key
  parts only; the brackets are found by scanning the header's line), and a
  table's bytes: from its header to the end of its last key/value, widened
  to the hull of what it holds (`[a]` holds `[a.b]`) — so a table's span
  encloses its children's, as the index requires of a parent.

It parses nothing itself. go-toml's decoder is not used: it is quadratic in
the number of tables. BurntSushi/toml's decoder is run only on a document
whose values nest at most 64 deep and whose keys have at most 8 parts
(`toml_not_checked` otherwise: dates, escapes and code points are then not
checked).

## Model

- **Identities.** Tables and keys are JSON Pointers, as YAML's keys:
  `<file>#/a/b` (Name `/a/b`; `~` and `/` in a key are `~0` and `~1`). An
  element of an array of tables is named by its `name` key (Cargo's
  `[[bin]]`, `[[test]]`, `[[example]]`): `/bin/cli`, the n-th element of
  one name `/bin/cli#<n>`; one without a `name` (or with a `#` in it) by its
  place among the unnamed ones, `/bin/#1`. Inserting a named element
  renames no other, and an element moved among named ones keeps its
  identity. A declaration of meaning is the table or key it is written as,
  with another kind and name: a Cargo package is `<manifest>#/package` (kind
  `package`, named by its name), a dependency `<manifest>#/dependencies/serde`
  (kind `dependency`, named by its key). A PEP 508 requirement is an array
  element, which has no key: `<file>#/project/dependencies/<normalized
  name>`, the n-th of one name in one array `...#<n>`.
- **Path evidence without reading another file.** A path dependency
  `path = "../core"` names the package of `crates/core/Cargo.toml`, whose
  identity is `crates/core/Cargo.toml#/package` — computed from the path
  alone, so the provider never reads the other manifest. It is an
  `IdentityInRepository` reference: Exact when that manifest declares a
  package, Unresolved when it does not (a virtual manifest, no file).
  `FileIdentity` is not used: its target is a file's own declaration
  (a document), not a package. A path leaving the repository (or absolute,
  or with `\`) is outside it. Identities from paths are spelled with the
  file IDs' separator (Windows).
- **What one file cannot fix stays open.** `workspace = true` names the
  workspace root's entry: the manifest itself when it has `[workspace]`, or
  the one `package.workspace` names; otherwise the root is the nearest
  ancestor manifest with `[workspace]`, which one file cannot see — the
  reference carries an identity no declaration has
  (`cargo:workspace-root/dependencies/<key>`, `IdentityInRepository`):
  Unresolved, never outside the repository, never a namesake. A glob
  member (`crates/*`) is Unresolved the same way; `exclude` removes no
  literal member (Cargo applies it to globs and path dependencies).
  Feature values (`"dep:x"`, `"x/f"`, `"x?/f"`, `"x"`) reference the
  dependency or feature they name; two dependencies of one key (in
  `[dependencies]` and a `[target]` table) leave a Candidate.
- **Two ecosystems, two name spaces.** Every file is `IdentityOnly`: no
  name match ever reaches a TOML declaration. Outside the repository a
  crate is `cargo:crates.io/<crate>`, `cargo:registry:<name>/<crate>` or
  `cargo:git:<url>#<crate>` (credentials, query and fragment removed from
  the URL), a Python distribution `pypi:<normalized name>`.
- **Relations.** `contains` (file → top tables and keys, table → entries,
  array of tables → elements, requirement array → requirements, workspace
  → member packages), `depends_on` (package → its dependencies, normal,
  dev, build and target ones and a pyproject's requirements, build-system
  ones included; dependency → what it names), `references` (feature → what
  it enables).
- **Bounds.** 4 MiB per file; declarations at most 6 parts deep and 5000
  per file, the shallower first (so a manifest's requirements are kept
  before deep `[tool]` keys), 20000 references, 1000 diagnostics; a key of
  more than 128 parts is reported and not read. A reference to a
  declaration of the file the bound left out is left out too.
- **Names are data.** A key that is no one-line name (`declname.IsName`)
  declares nothing, nor does what it holds; values never become names,
  except a package's name, a dependency's key and a requirement's
  distribution name, all checked. Signatures carry a value's TOML type, a
  table's header, a dependency's source.

## Evaluation

- **toml-test.** `TestTOMLTestSuite` (opt-in, `ARK_TOML_TEST_DIR`, e.g.
  BurntSushi/toml's `internal/toml-test/tests`): all 262 valid files read
  without an error and with the decoder's tree (keys, tables, arrays of
  tables, string values); all 466 invalid files of TOML 1.0 (the 1.1
  relaxations aside) report an error.
- **Real files.** `TestRealRepositories` (opt-in, `ARK_TOML_REAL_REPOS`)
  checks every TOML file of a repository against both decoders (an error
  exactly where either rejects) and every manifest's package and
  dependencies against BurntSushi/toml's decoding; every edge Exact. On
  2026-10-31: Ark (12 files: `goreg.toml`, the Rust fixtures' manifests),
  BurntSushi/toml v1.6.0 (748 files, 474 rejected), wazero v1.9.0 (4
  Cargo manifests), chroma v2.27.0 (a pyproject.toml), revive v1.17.1,
  kjak v1.2.0, gitleaks v1.25.1: all agree.
- **Corpus.** `TestCorpus` (opt-in, `ARK_TOML_CORPUS`) over the Go module
  cache: 2560 files, 718 KB, 20443 declarations in 0.20 s; the slowest
  file 12.6 ms (gitleaks.toml); no failure, every range within its file.
- **Worst cases** of 4 MiB: 300000 tables 1.6 s; one inline table of
  400000 keys 0.8 s; arrays of tables 0.9 s; values nested 64 deep on every
  line 0.9 s; a distinct dotted key of 8 parts (the most still checked) on
  every line 1.9 s. `TestExtract_Linear` keeps every shape within 40x the
  time for 16x the input.
- **Fuzzing.** `FuzzExtractTOML` (as a Cargo manifest, also with Windows
  file IDs, a pyproject.toml and data) and `FuzzParse` (against both
  decoders: no error ⇒ go-toml accepts; both accept ⇒ no error), 60–90 s
  each: one finding (a dependency with the empty key named ""), fixed.
- **Mutation testing.** 20 mutants of the document layer, the provider and
  the manifests, all killed; the first round's two survivors became tests
  (shallow-first bound for manifest declarations, references to left-out
  declarations), the third showed that `exclude` must not remove literal
  members.

## Remaining work

- The workspace root of `workspace = true` when no file names it: the
  resolver's `Lookup` could state the ancestor manifests in order, if a
  kind class admitted dependencies.
- Poetry (`[tool.poetry.dependencies]`), uv workspaces
  (`[tool.uv.workspace]`), `[patch]` and `[replace]`, `default-members`,
  Python path requirements (`name @ file:...`).
- `.cargo/config.toml` (a hidden directory the index does not enter).
