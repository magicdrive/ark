# Ark v6.11.0 — TOML, XML and Markdown

Ark v6.11.0 adds **TOML** (Cargo and pyproject manifests), **XML** (Maven POMs) and **Markdown** to the languages it indexes, so an agent can explore a repository's configuration, dependencies and documentation through the same graph tools as its code — the tables and dependencies of a Cargo workspace, the modules and dependencies of a Maven build, the sections of a README and the links between documents — without reading the files in full. It also fixes two secret-masking issues reported after v6.10.0.

As always, Ark resolves what a file's own text establishes and preserves uncertainty where it cannot: no version, `cfg()`, marker or Maven property is evaluated, nothing is fetched (no package registry, DTD, entity or URL), and a link to a file or heading that does not exist is reported unresolved.

## Highlights

### TOML, Cargo and pyproject

- TOML files (`.toml`) are indexed: the file, its tables and keys by JSON
  Pointer (`config/app.toml#/server/tls`; an array-of-tables element by its
  `name`, `Cargo.toml#/bin/cli`), with `contains` relations, and
  diagnostics for syntax errors, duplicate keys and redefined tables.
- Cargo manifests: the package (kind `package`), its dependencies (kind
  `dependency`) of every table, target ones included, with `depends_on`; a
  path dependency reaches the package of the repository's manifest it names
  (exact); `workspace = true` reaches the workspace's entry (exact where
  the root is this manifest or the one `package.workspace` names, else the
  ancestor manifest's that declares it, strong), so changing a workspace
  dependency reaches the members that inherit it; a workspace contains its
  literal members; features reference what they enable. crates.io,
  registries and git are outside the repository; glob members stay
  unresolved.
- pyproject.toml: the project (kind `package`), and every PEP 508
  requirement of its dependencies, optional dependencies, dependency
  groups and build system (kind `dependency`), outside the repository on
  PyPI.
- New dependency: github.com/pelletier/go-toml/v2 v2.4.3 (MIT, no
  dependencies) for positions; BurntSushi/toml checks dates and escapes.

### XML and Maven POM

- New language: XML (`.xml`), graph support. Every XML file's elements are
  declared by namespace-aware XPath-like paths
  (`pom.xml#/project/dependencies/dependency[2]`), with their attributes,
  namespace declarations and IDs (`xml:id`; `id` attributes, capped at
  Candidate), `contains` relations, fragment `href="#x"` references and
  XInclude / `xml-stylesheet` links.
- Maven POMs: the project (`groupId:artifactId`, inheriting the parent's
  groupId and version), its parent (`extends`, Strong: by `relativePath`
  and coordinates, else by coordinates), modules (`links`, Exact) and
  dependencies, managed dependencies, plugins and extensions (`depends_on`
  a project of the repository by coordinates, Strong, or outside the
  repository). Nothing is fetched.
- Safe by construction: no DTD, external entity or URL is ever read, no
  entity is expanded (billion-laughs and XXE documents are read as
  written and reported), and size, nesting and node bounds are diagnostics.
- XML names never reach other languages' declarations, and indexing a
  Maven project's XML changes no Java result.

### Markdown

Markdown documents (`.md`, `.markdown`, also under `.github`) are indexed:
README files, design documents, runbooks and ADRs become part of the graph.

- **Structure.** Each document and each heading (ATX and setext, H1–H6) as a
  `section`, identified by the anchor GitHub gives it (`README.md#usage`,
  repeats `#usage-1`), with `contains` relations; fenced code blocks by
  language (`code_block`); raw HTML ids (`anchor`).
- **Links.** Same-document and cross-document anchor links resolve to the
  section (`references`, Exact); relative links resolve to the linked
  repository file's own declaration in any language — Markdown, HTML, YAML,
  CSS, JavaScript, images (`links`); reference-style links through their
  definitions. A missing file or anchor is reported unresolved, never
  outside the repository; web URLs are outside the repository and are named
  by their scheme and host only (a webhook's path is its secret).
- **Parser.** github.com/yuin/goldmark v1.8.6 (CommonMark 0.31.2: all 652
  specification examples pass; GFM tables, strikethrough, autolinks, task
  lists; footnotes). YAML front matter is skipped.
- **Bounds.** 4 MiB a file, 5000 declarations, 20000 references. A guard
  reads input whose parsing would cost more than linear time or memory as
  text and reports it (`markdown_degenerate`): tables over 128 columns or
  with too many missing cells, deep nesting, long runs of link reference
  definitions, unmatched delimiters. Every such shape of a 4 MiB file is
  read within about 5 seconds. Code blocks are never executed or read as
  code.
- **Secrets.** A heading or a raw HTML id whose text Ark's recommended
  secret rules match (`## password = …`) declares no section or anchor, as
  a name is shown as written.

All three are at the **graph** level (not `context_quality_certified`). `toml`, `xml` and `markdown` are accepted wherever a language is named (`--lang`, the `language` / `lang` arguments of MCP tools), and `search_code` accepts the kinds `table`, `dependency`, `attribute`, `section` and `code_block`.

```text
search_code   kind=package                     → Cargo and pyproject packages, Maven projects
get_callees   crates/app/Cargo.toml#/package   → its dependencies (depends_on)
search_code   kind=dependency filePattern=pom  → a POM's dependencies
search_code   kind=section filePattern=README  → a README's sections by their GitHub anchors
search_code   kind=code_block                  → the fenced code blocks by language
get_callers   docs/design.md                   → the documents and sections linking it
```

### Secret masking

- **A Slack token is masked whole.** Before, the `slack-token` rule stopped at the token's first `-` (and at 48 characters), so `xoxb-123456789012-1234567890123-<secret>` kept its secret section — since v6.1.0. The rule now also masks rotated (`xoxe.xoxb-…`), app-level (`xapp-1-…`), refresh (`xoxe-1-…`), legacy `xoxo-` and workflow (`xwfp-…`) tokens, only in a token's shape (`xapp-style` is left as written). A word glued to a token (`xoxb-1234567890-password=…`) is not taken into it, so what follows is masked as before; no text v6.1.0's rules hid is shown. The rule ID is unchanged; `ark secrets-scan` reports the whole token.
- **Paths in `get_context` and `analyze_change_impact` text are no longer partly masked.** A path that reads like an assignment (`auth/token-store.go`) was shown as `auth/token-*****MASKED*****` since v6.9.0. Paths and names in their text, and the candidates of an ambiguity error, are now masked as names are — a token in one is still masked — and the sources are masked as before.

## Changed

- **Indexing:** `.toml`, `.xml`, `.md` and `.markdown` files are now indexed; `.github`'s Markdown files too (its scripts still are not). Repositories with configuration and documentation show more symbols in `search_code`, `get_repository_map` and `get_project_stats`, and indexing takes longer (see Performance below).
- **Repository map:** a package keeps the language of its code when TOML, XML, Markdown or YAML files sit beside it.
- **Secret masking:** the recommended rules are v6.1.0's with one recorded amendment (the Slack token above): text without a Slack token masks byte for byte as before, and text with one masks at least what it masked before.

## Performance

Cold MCP index builds, alternating with v6.10.0 on one machine, two rounds each:

| Repository | v6.10.0 | v6.11.0 | Files indexed |
|---|---|---|---|
| Ark | 4.6–4.9 s | 4.9–5.5 s | 1293 → 1359 |
| docker/cli v29.7.2 | 2.1 s | 2.3–2.4 s | 709 → 1004 |
| docker/docker v26.1.3 | 9.3 s | 9.3 s | 2045 → 2122 |
| Hephaestus | 1.7 s | 1.7 s | 243 → 292 |

The cost follows the files newly indexed; peak memory stayed within the rounds' spread. Every new reader is linear in its input, also on degenerate input (one long line, deep nesting, many links, wide tables, repeated DOCTYPEs): each is tested at sizes four times apart.

## Compatibility

- No CLI flag, MCP tool, argument or response field was removed or renamed; languages, symbol kinds and the `slack-token` rule's matches are added.
- Results for existing languages are unchanged: their golden snapshots and the conformance contract are identical, except that a fixture's `Cargo.toml` is now indexed as TOML beside the Rust code.
- In the repository map, a directory holding only data and documentation files takes its first file's language, as before: one with a `README.md` and a `compose.yaml` (or `.github/ISSUE_TEMPLATE`) now reads `markdown` where it read `yaml`.
- The cache format is unchanged (schema 2); existing caches are reused.
- New dependencies: github.com/pelletier/go-toml/v2 v2.4.3 and github.com/yuin/goldmark v1.8.6 (both MIT, pure Go). XML uses Go's standard library.
- Built with Go 1.27.3 for the same eight platforms as before.

## Known Limitations

- TOML: no version requirement, `cfg()` expression, environment marker or glob is evaluated; Poetry and uv settings, `[patch]` and `[replace]` are plain keys; glob workspace members stay unresolved; a `workspace = true` dependency that several ancestor workspaces declare is a Candidate of each (one file cannot see which root is the nearest); `.cargo/config.toml` is not indexed (a hidden directory).
- XML: no DTD or schema is read, so IDREF attributes other than fragments are not references; only UTF-8 (and ASCII) documents are read; Maven properties from a parent or a profile, `${revision}` and Gradle builds are not resolved.
- Markdown: links are matched as written (`#Usage` is not `#usage`); links inside raw HTML are not read; a link to a file Ark does not index (a Go source, `LICENSE`, a directory) is unresolved like a missing one; a heading that reads like an assignment (`## Token: rotate`) is taken for a secret and declares no section.
- Data and markup files declare at most 5000 symbols each, and files over 4 MiB declare only themselves.

Details: [language support](https://github.com/magicdrive/ark/blob/v6.11.0/docs/language-support.md) and the design records for [TOML](https://github.com/magicdrive/ark/blob/v6.11.0/docs/development/toml.md), [XML](https://github.com/magicdrive/ark/blob/v6.11.0/docs/development/xml.md) and [Markdown](https://github.com/magicdrive/ark/blob/v6.11.0/docs/development/markdown.md).

## Installation / Upgrade

```sh
brew upgrade ark            # or: brew install magicdrive/tap/ark
# or, without Homebrew (macOS, Linux):
curl -fsSLO https://raw.githubusercontent.com/magicdrive/ark/main/install.sh
sh install.sh               # sh install.sh --upgrade to upgrade
```
