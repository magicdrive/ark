# Ark v6.5.0 — release notes (draft)

> Draft for the maintainer. Every number below was measured on the corpora
> named in `docs/development/web-semantic-fidelity/commonjs-boundary/fidelity.md`
> against independent TypeScript-checker oracles; nothing here is a general
> guarantee or a proof of correctness.

## Overview

- **HTML** (new, support level `graph`): ids and their references (IDREF
  attributes, fragments) within a document as the browser builds it, links
  to documents, stylesheets and scripts by relative path, the declarations of
  inline styles and scripts, and links to images, fonts and media. See
  [Language support](../../docs/language-support.md#html).
- **CSS** (new, support level `graph`): `@keyframes`, `@font-face`,
  `@property`, custom properties and CSS Modules classes with their
  references within a stylesheet, `@import` by relative path, the CSS
  Modules classes JavaScript and TypeScript imports use, and `url()` of
  images and fonts. See [Language support](../../docs/language-support.md#css).
- **JavaScript** (improved): scope-aware resolution with ES module and
  CommonJS bindings, class members and inheritance, and reads of values.
  A name a local declaration shadows is never resolved to a top-level one.
- **Assets** (new): images, fonts and media that web documents link to are
  `asset` symbols.

## Compatibility

**References through a CommonJS export are `candidate`, never `exact` or
`strong`.** A CommonJS exports object is a plain object that code may change
at run time, so Ark no longer claims that the value an importer reads
(`require("./m").parse()`, an `import` of a CommonJS module, a re-export of
one, a class extended through one) is the declaration the module exports.
The candidate still names that declaration. As a result these calls are no
longer graph edges: `get_callers`, `get_callees` and `analyze_change_impact`
list them among `candidates` and count them as `unattributed`. The response
keys are unchanged. Lexical references (also inside a CommonJS module) and
ESM / TypeScript exports keep their confidence. See
[Resolution model](../../docs/resolution-model.md).

On the measured corpora the false `exact` / `strong` count stays 0, and every
target that lost `exact` / `strong` is still offered as a candidate. The
share of references resolved `exact` / `strong` drops where code goes
through CommonJS (semver 75.7% → 51.1%; tsc-built rxjs `dist/cjs` 35.9% →
23.4%), and is unchanged on ESM and TypeScript sources.

**New languages in tool parameters**: `language` / `lang` accept `html` and
`css`; `kind` accepts the HTML, CSS, JavaScript `script` and `asset` kinds
([MCP tools](../../docs/mcp-tools.md)).

Extraction caches of earlier versions are not reused for JavaScript and
TypeScript (provider versions `js-12+ts-16` and `ts-16`); HTML and CSS are
new providers; other languages' caches stay valid.

## Known limits

- CommonJS: a reference through a CommonJS export is a candidate, also when
  no code changes the exports object. A `require()` of an ES module reached
  through a namespace receiver (`const ns = require("./esm.js"); ns.f()`) is
  a candidate as well.
- No absolute URLs, selector matching, `tsconfig` paths or type inference.
  See [Known gaps](../../docs/language-support.md#known-gaps).
