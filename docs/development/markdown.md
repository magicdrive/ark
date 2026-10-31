# Markdown support: design record and evaluation

This page records why Markdown support is built the way it is, what was
measured, and how. The rules themselves are in the headers of
`internal/languages/markdown/provider.go` (what a file states),
`parse.go` (what goldmark reads), `slug.go` (GitHub's anchors), `dest.go`
(what a link destination names) and `guard.go` (the cost guard).

## Parser

- **Library.** github.com/yuin/goldmark v1.8.6 (2026-09-03; v1.8.7 of
  2026-10-30 was one day old and not taken), MIT, pure Go, no dependencies
  of its own. It states compliance with CommonMark 0.31.2: its `TestSpec`
  runs the 652 examples of the specification, and all 652 pass (run on a
  copy of the module with the repository's toolchain, go1.27.3). It is the
  parser Hugo and many Go tools use, maintained since 2019 with releases
  through 2026. `govulncheck` reports nothing for it.
- **Extensions**, as GitHub renders them: tables, strikethrough, extended
  autolinks (`www.x`, bare `https://x`), task lists (goldmark's `GFM`), and
  footnotes — without them `[^1]: note` is a link reference definition and
  `[^1]` a link to `note`. GitHub's other additions are not read: alerts
  (`> [!NOTE]`, a block quote to goldmark), math, mentions and issue
  references (text), emoji shortcodes (text; GitHub's anchor rule removes
  the emoji either way).
- **Positions.** goldmark keeps every block's lines and every text's bytes
  as offsets into the source (`text.Segment`), so positions are the file's
  bytes as stored — CRLF, a BOM and invalid UTF-8 included — and Ark's
  1-based line and byte column follow from a line table. goldmark records
  where an inline node starts only: the provider wraps goldmark's link
  parser (`linkEnds`), and where the reader stands after a link or an image
  is where it ends. A setext heading ends with its underline, a fenced code
  block with its closing fence, both found from the lines goldmark keeps.
- **Not chosen.** The Tree-sitter Markdown grammars bundled by gotreesitter
  come as two grammars (blocks, and inline content to run per block), and
  a grammar gives no CommonMark conformance to measure against the
  specification's examples; Ark would own the link reference definitions,
  the reference links and every position fix. blackfriday is not
  CommonMark-compliant (its own README, and goldmark's comparison);
  gomarkdown/markdown is a fork of it.

## Model

- **Symbols.** The file is a `document` (Qualified = its FileID, a target
  of path evidence: `symbol.DeclaresFile`). Each heading — ATX or setext,
  H1 to H6, also in a block quote or a list item — is a `section`, named by
  its text content and identified by GitHub's anchor, `<file>#<anchor>`. A
  section spans its heading and what follows up to the next heading of its
  level or a higher one (trailing blank lines excluded); a heading inside a
  container block spans itself (its container ends what follows it). Each
  fenced code block with a language is a `code_block` named by the info
  string's first word, `<container>::<language>` (repeats are told apart
  by the index's ordinal); each raw HTML `id` (and `<a name>`) is an
  `anchor`, `<file>#<id>`, since a fragment names it as it names a heading.
  Paragraphs, lists, tables and blocks without a language are no symbols:
  they would be noise in every listing, and nothing links to them.
- **Anchors.** `slug.go` follows github-slugger (the rule GitHub's renderer
  and its link checkers share): lower case by JavaScript's full case
  mapping (`İ` is `i̇`, a final `Σ` is `ς`), every character but a letter,
  a mark, a decimal digit, a letter number, `_`, `-` or a space removed,
  each space `-`, repeats `-1`, `-2`, … skipping an anchor already taken.
  The text content is what a browser reads from the rendered heading:
  inline code as written, a link's text, an image nothing, an HTML tag
  nothing, a soft line break `\n` (removed by the rule: a two-line setext
  heading `A` / `b` is `#ab`). `TestSlug_GitHubRule` pins github-slugger's
  own fixtures, Unicode headings, symbols, emoji and repeats.
- **Relations.** A document contains its top sections, a section its
  subsections (`reference.KindContains`, named by the child's identity, as
  YAML's). Links — inline, reference (full, collapsed, shortcut; goldmark
  resolves them to their definition, and an undefined one is text, as
  CommonMark says), images, autolinks — are references of the innermost
  section around them:
  - `#x`: an `id_reference` to `<this file>#x`, a section or an anchor;
  - a relative path: path evidence (`resource_link`, `FileIdentity`) to the
    file's own declaration, whatever its language — a Markdown, HTML or YAML
    document, a stylesheet, a script, an asset (whose node the index makes
    for the link); `b.md#x` an `id_reference` to that Markdown file's anchor
    (a fragment of an HTML file is not stated: that is the HTML name space);
  - `/a.md`: from the repository's root, as GitHub reads it, capped at
    Strong (the index's root may be another directory);
  - a path that leaves the root: a `resource_link` without identity,
    unresolved;
  - `https://host/p`, `//host/p`: a `resource_link` outside the repository,
    named `scheme://host/path` only — credentials, the query and the
    fragment, where a token may be, are no part of a name;
  - `mailto:`, `javascript:`, `data:` and other schemes, an escaped slash,
    a backslash, an invalid escape: nothing.

  Every identity is in the repository (`IdentityInRepository`): a missing
  file or anchor is Unresolved, never outside the repository and never
  resolved to something else. Every file is `IdentityOnly`: a section
  `Usage` of one file is no section of another, nor any code's `Usage`.
- **Names are data.** Every name, qualified name and reference name
  satisfies `declname.IsName` (one line, at most 256 bytes, no control or
  format character): a heading whose text is none is named by its anchor,
  and declares nothing when that is none either; a link whose name would be
  none states nothing.
- **Code blocks are text.** Their content is never run, and not read as
  another language: a provider reads one file as its own language, and an
  embedded reading (as HTML does for inline scripts) would put the block's
  declarations in that language's name space — a snippet's `func main` would
  become a repository declaration, and a repository-unique name a
  Candidate. Nothing in a code block, or a code span, is a link or a
  heading.
- **Front matter.** A file's YAML front matter (a first line `---`, up to a
  line `---` or `...`) is read as blank lines: GitHub renders it as a table,
  while CommonMark would make it a thematic break and a setext heading of
  its last key. An unclosed `---` is Markdown.
- **`.github`.** The provider reads Markdown under `.github`
  (`language.HiddenDirReader`): templates and release notes there are
  documents the others link to (Ark's own docs link
  `.github/release/NOTES-v6.0.0.md`). Every Markdown file is IdentityOnly,
  so reading them changes no other file's names.

## The cost guard

goldmark cannot be interrupted, and some inputs no author writes take it
time that grows with their square (v1.8.6, measured on 16 KiB and 64 KiB,
then 256 KiB; ×4 the input taking ×13–×33 the time):

| Input | 64 KiB | |
|---|---|---|
| `> > > …` on one line (also `> - `, `1. > `, `* - + `) | 1.0–2.8 s | block parsing |
| `*a*a…`, `_a_a…` (unmatched emphasis) | 0.4–2.1 s | inline |
| `~~a~~a…`, ``` ``a``a… ``` | 0.1–0.4 s | inline |
| `[a]([a](…` | 0.9–1.9 s | inline (link destinations) |
| 64000 link reference definitions in one paragraph (450 KB) | 9.8 s | block (definitions) |
| `[x]` on each of 16000 lines | 0.4 s | inline |

A time limit would make the result depend on the machine, so the guard
(`guard.go`) bounds the input from its text alone, before goldmark reads
it, rewriting bytes in place (never moving one, so every position stays the
file's):

- on one line, the block quote or list item marker after the 32nd is read
  as `\`: the rest of the line is a paragraph's text (its links still
  links);
- in a paragraph, of the lines that start with `[`, every 1000th is read as
  a blank line (a file with more than 1000 consecutive definitions loses
  one in a thousand);
- a block-only parse (no inline parsing, no definitions: linear) finds the
  blocks whose inline content goldmark reads; a block costs its delimiters
  (`*_~`` ` ``[]()<`) times its length; when the file's blocks cost more
  than 2×10⁸ — the definitions then read, since they are no inline content
  — the costliest blocks' delimiters are read as spaces until they do not.

Each rewrite is reported (`markdown_degenerate`, a warning, once per kind,
at the first). The budgets lie far above written documents: over the 5636
Markdown files of the Go module cache (below), the guard rewrote nothing,
the costliest file (aws-sdk-go-v2's CHANGELOG.md, 1.3 MB) costs 6.5×10⁶ —
a thirtieth of the budget — and the costliest block 2.4×10⁶. With the
guard, every shape above, at 256 KiB and 1 MiB, takes time linear in its
size (`TestPerformance_Linear`, ×3.4–×4.3 for ×4 the input), the slowest
1 MiB shape 0.33 s.

Other bounds: a file over 4 MiB declares itself only
(`markdown_too_large`); a file states at most 5000 declarations and 20000
references (`markdown_truncated`). A file that is no UTF-8 text is reported
(`markdown_invalid_utf8`, an error: the one way a Markdown file is broken)
and read all the same.

## Evaluation

- **Self-check** (`TestSelf_ArkDocuments`, opt-in, `ARK_MD_SELF`): Ark's
  own `README.md`, `ARCHITECTURE.md` and `docs/**/*.md` (this page
  included) — 36 documents, 476 headings, 158 local links of which 155 resolve (98.1%), every one of
  the 96 anchor links; 43 external links. The 3 unresolved are no broken
  links: `LICENSE` (no provider declares a file without an extension), the
  directory `.github/release`, and `docs/scripts/ark-mcp-timing.py` (the
  Python provider declares no file of its own).
- **Corpus** (`TestCorpus_Measure`, opt-in, `ARK_MD_CORPUS`): the Go module
  cache, 5636 files, 33.5 MB — 47424 sections (128 fewer than before front
  matter was skipped), 49778 links, 13878 code blocks; no failure; 1.8 s
  single-threaded (19.1 MB/s, the guard's
  block-only parse included); the slowest file aws-sdk-go-v2's CHANGELOG.md
  (1.3 MB) 78 ms.
- **Fuzzing.** `FuzzExtractMarkdown` (ranges within the file and equal to
  their bytes', names, parents and containers declared, unique identities,
  determinism, for a slash and a backslash FileID).
- **Mutation testing.** 32 mutants of the decisions above (the anchor
  rule, destinations, identity and confidence, spans and containers, code
  blocks, HTML ids, footnotes, front matter, UTF-8, the guard's bounds and
  order, Windows separators): all killed. One survived its first run (an
  anchor a heading took before its repeat — "a-1", "a", "a" — is skipped);
  `TestSlug_Duplicates` now pins it.

## Remaining work

- A node for every repository file (not only assets), so that a link to a
  Go file, a `LICENSE` or a directory resolves (now Unresolved).
- A fragment of an HTML document from Markdown (`page.html#id`): the HTML
  name space's identity, which a qualified identity of another language
  cannot name.
- GitHub alerts (`> [!NOTE]` is a block quote), MDX.
- Links in raw HTML (`<a href>`, `<img src>`): only `id` and `<a name>` are
  read.
