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
  string's first word, `<container>::<language>`; the n-th block of a
  language in one container is `<container>::<language>[n]` (the first
  keeps its name, as XML's `[n]` steps do); each raw HTML `id` (and `<a name>`) is an
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
  - `/a.md`, `/a.md#x`: from the repository's root, as GitHub reads it,
    capped at Strong (the index's root may be another directory), the
    fragment's `id_reference` as the path's `resource_link`;
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
- **Secrets.** The index shows a declaration's name and qualified name as
  they are, and the MCP server masks names by a token's form only: a
  heading `## password = hunter2…` would be named so, its anchor
  `#password--hunter2…`, and a heading's anchor lowers a token's case
  (`AKIA…` is `akia…`, which no rule matches). So a heading whose text —
  before the anchor rule, and its name — or an HTML id whose value holds
  what Ark's recommended secret policy masks in a text
  (`secrets.Recommended().Find`, the content mode: assignments of a
  secret's name, token formats, PEM blocks) declares no section or anchor:
  its text is the document's, a link in it the section's around it, and a
  fragment that names it is unresolved. The policy errs on the side of
  masking: a heading `Token: rotate` (an assignment's shape) is no section
  either. Reference names are not checked: a fragment is the file's text
  as written, and a file's text is masked where it is shown.
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
time (or memory) that grows faster than they do (v1.8.6; the first rows
measured on 16 KiB and 64 KiB, then 256 KiB, ×4 the input taking ×13–×33
the time; the last five by the v6.11 audit, in the `ark symbol` CLI):

| Input | Size | Time | |
|---|---|---|---|
| `> > > …` on one line (also `> - `, `1. > `, `* - + `) | 64 KiB | 1.0–2.8 s | block parsing |
| `*a*a…`, `_a_a…` (unmatched emphasis) | 64 KiB | 0.4–2.1 s | inline |
| `~~a~~a…`, ``` ``a``a… ``` | 64 KiB | 0.1–0.4 s | inline |
| `[a]([a](…` | 64 KiB | 0.9–1.9 s | inline (link destinations) |
| 64000 link reference definitions in one paragraph | 450 KB | 9.8 s | block (definitions) |
| `[x]` on each of 16000 lines | 64 KiB | 0.4 s | inline |
| a table of N columns whose N rows have one cell (`\|a\|a…`, `\|-\|-…`, `a`…), N = 4000 / 8000 | 24 / 48 KB | 5.8 s, 3.4 GB / 32 s, 15.9 GB | goldmark adds every missing cell |
| 999 definitions, each title 200 lines | 414 KB | 55 s | definitions × the paragraph's lines |
| `- a` indented 2i columns (tabs), i up to 4000 | 4 MB | 84 s | depth³ |
| `[^a]: ` 32000 times on one line | 192 KB | 1.2 s (×4 for ×2) | footnote definitions nested on one line |
| `\xef\xbb\xbf# Title` | | | the first heading is text |

The cost is goldmark's own: a definition's label and title are found
line by line from the paragraph's end (`text.blockReader.Value`), and each
definition taken out moves the lines after it; every open container is
asked to continue on each line, its indentation computed from the line's
start, and the parser keeps a record per line and container until a blank
line (`parser.parseBlocks`); a table row that lacks cells gets as many as
the delimiter row has columns, the header row too.

A time limit would make the result depend on the machine, so the guard
(`guard.go`) bounds the input from its text alone, before goldmark reads
it, rewriting bytes in place (never moving one, so every position stays the
file's):

- on one line, the block quote, list item or footnote definition marker
  (`[^label]:`, up to the first `]:`) after the 32nd, or past column 128 (a tab to the next multiple of 4, a marker's bytes
  counted), is read as `\`: the rest of the line is a paragraph's text
  (its links still links). Containers nest no deeper than their markers'
  columns allow — a nested list item is indented to its parent's content,
  a block quote's content follows its `>` — so the column bounds the
  nesting a document builds line by line (at most 64 lists of 2 columns,
  32 of 4), with spaces, tabs and block quotes alike. 128 columns lie far
  past a written outline; a lower bound would not pay: goldmark's time per
  byte is then about the same (each line is shorter, and there are more),
  4 MiB of 64 nested lists repeated taking 4.7 s (and 32 levels, the bound
  at 64 columns, 3.8 s) — a flat list of 4 MiB (`- a` on every line)
  takes 3.2 s and 1.2 GB;
- in a paragraph, a line is read as a blank line when it is the 1000th
  that starts with `[` since the paragraph's start or the last such
  rewrite (a file with more than 1000 consecutive definitions loses one in
  a thousand), or — when the first line since then starts with `[`, the
  one way goldmark reads definitions there — the 2000th line since then (a
  definition whose title spans many lines; 1000 definitions with their
  titles on the next line still fit). goldmark's reading of the document
  then reads two paragraphs; the guard's own parse makes the lines after
  it a paragraph of its own (`definitionSplitter`, a paragraph transformer
  that runs before the definitions');
- a table's delimiter row with more than 128 columns, or whose table would
  make goldmark add more missing cells (to its header and its rows) than
  what is left of the file's 131072, is no delimiter row: its first byte
  that is no space is read as `\`, and the table is a paragraph.
  `tableGuard` wraps goldmark's table transformer, in the guard's parse
  and in goldmark's reading of the document alike: it reads the lines
  goldmark's transformer will — after the paragraph's definitions are
  taken out — as goldmark reads them (`isTableDelim`, `parseDelimiter`,
  `parseRow`; `TestTableGuard_ReadsAsGoldmark` compares the two on 1540
  cases), so no cell is made before the bound applies, in the guard's own
  parse either. A paragraph's rows are read once: after one of its tables
  is rewritten for its cells, every later one is. A table's cells that a
  row has are bounded by its bytes, so the cells goldmark makes are then
  linear in the file;
- one parse without inline content (linear: its definitions and tables
  bounded as above) finds the blocks whose inline content goldmark reads;
  a block costs its delimiters (`*_~`` ` ``[]()<`) times its length; when
  the file's blocks cost more than 2×10⁸ — the definitions taken out, since
  they are no inline content — the costliest blocks' delimiters are read as
  spaces until they do not.

Before them, a byte order mark that starts the file is read as three line
breaks: blank lines before the first block, which CommonMark ignores, so
the first heading is one and no line gains indentation (as spaces, a
`  - a` after it would be an indented code block); front matter after it
is skipped as without it.

Each rewrite is reported (`markdown_degenerate`, a warning, once per kind,
at the first). The budgets lie far above written documents: over the 5636
Markdown files of the Go module cache (below), the guard rewrote nothing,
the costliest file (aws-sdk-go-v2's CHANGELOG.md, 1.3 MB) costs 6.5×10⁶ —
a thirtieth of the budget — and the costliest block 2.4×10⁶. With the
guard, every shape above, at 256 KiB and 1 MiB, takes time linear in its
size (`TestPerformance_Linear`: ×2.4–×4.2 for ×4 the input, the slowest
1 MiB shape — lists nested by tabs — 0.83 s), and every shape at the
bound of 4 MiB takes 0.05–4.7 s (table below).

| 4 MiB input | Time | Max RSS |
|---|---|---|
| a table of 699050 columns, rows of one cell (`table_wide`) | 0.52 s | 231 MB |
| 128 columns, 2 M rows of one cell (`table128_short_rows`) | 1.49 s | 814 MB |
| 128 columns, full rows (a dense table, no rewrite) | 1.76 s | 1055 MB |
| 128-column tables after a definition, repeated | 0.97 s | 591 MB |
| definitions, each title 2000 lines (`defs_title_long`) | 1.61 s | 974 MB |
| definitions on every line | 0.53 s | 255 MB |
| `[^a]: ` on one line / 21 a line / nested by indentation | 0.03 / 0.55 / 0.47 s | 23 / 176 / 123 MB |
| `- a` indented 2i columns by tabs / spaces (`list_tab_deep`) | 3.30 / 1.67 s | 80 / 45 MB |
| 64 nested lists (the bound) repeated / then indented lines | 4.64 / 4.52 s | 1395 / 891 MB |
| block quotes and lists, tabs and spaces mixed | 0.05 s | 34 MB |
| for comparison: `- a` on every line (a flat list) | 3.18 s | 1053 MB |
| for comparison: prose with links and code spans | 0.59 s | 208 MB |

(`ark symbol <file>`, darwin/amd64, the audit's inputs at their sizes all
in 3.2 s at most: `table4000.md`, 24 KB, from 6.56 s and 5.0 GB to
0.02 s and 17 MB; `listtab4000.md`, 4 MB, from 84 s to 3.14 s.)

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
  The v6.11 audit's fixes (the table, definition and indentation bounds,
  the byte order mark, secrets in headings and ids, root fragments capped
  at Strong, code blocks' ordinals): 12 mutants, one of each decision —
  all killed; two survived their first run (a header row with fewer
  cells than the delimiter row, whose table the budget then missed, and
  the header row's own missing cells), which `TestGuard_TablePadding` now
  pins.
- **CommonMark.** goldmark's reading of the document is assembled from
  GFM's extensions with tables behind `tableGuard`: on the 652 examples of
  the specification (goldmark's `_test/spec.json`) and five more (tables,
  footnotes, a byte order mark), its HTML is the same as that of
  `extension.GFM` and `extension.Footnote` but for front matter (two
  examples, by design) and the byte order mark; goldmark's own `TestSpec`
  passes all 652.

## Remaining work

- A node for every repository file (not only assets), so that a link to a
  Go file, a `LICENSE` or a directory resolves (now Unresolved).
- A fragment of an HTML document from Markdown (`page.html#id`): the HTML
  name space's identity, which a qualified identity of another language
  cannot name.
- GitHub alerts (`> [!NOTE]` is a block quote), MDX.
- Links in raw HTML (`<a href>`, `<img src>`): only `id` and `<a name>` are
  read.
