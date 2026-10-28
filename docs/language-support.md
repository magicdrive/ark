# Language support

Ark analyzes twelve languages. They are not supported equally: each language
has a **support level**, the highest stage its tests certify, and every
language has constructs Ark deliberately leaves unresolved.

`get_language_support` reports the levels at run time; `ark symbol <file>`
shows what Ark extracts from one file.

## Support levels

Levels build on each other:

| Level | What the tests certify |
|---|---|
| `parse` | The parser parses the language (Tree-sitter; HTML: golang.org/x/net/html; CSS: Ark's CSS Syntax Level 3) |
| `symbols` | Declarations are extracted with correct names, kinds and ranges |
| `references` | Calls, constructions, type uses and imports are extracted |
| `resolution` | References are resolved with the confidence rules of [the resolution model](resolution-model.md) |
| `graph` | Graph tools (callers, callees, relations, impact) are tested on this language |
| `context_quality_certified` | `get_context` passes Ark's context-quality benchmark: expected items found, no fabricated edge, no false `exact` |

| Language | Extensions | Level | Parse | Symbols | References | Resolution | Graph | Context |
|---|---|---|:-:|:-:|:-:|:-:|:-:|:-:|
| Go | `.go` | `context_quality_certified` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| TypeScript | `.ts` | `context_quality_certified` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| TSX | `.tsx` | `context_quality_certified` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Java | `.java` | `context_quality_certified` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| PHP | `.php` | `graph` | ✓ | ✓ | ✓ | ✓ | ✓ | |
| Terraform | `.tf`, `.tfvars` | `graph` | ✓ | ✓ | ✓ | ✓ | ✓ | |
| Rust | `.rs` | `graph` | ✓ | ✓ | ✓ | ✓ | ✓ | |
| HTML | `.html`, `.htm` | `graph` | ✓ | ✓ | ✓ | ✓ | ✓ | |
| CSS | `.css` | `graph` | ✓ | ✓ | ✓ | ✓ | ✓ | |
| Python | `.py`, `.pyw` | `graph` | ✓ | ✓ | ✓ | ✓ | ✓ | |
| Zig | `.zig` | `graph` | ✓ | ✓ | ✓ | ✓ | ✓ | |
| JavaScript | `.js`, `.mjs`, `.cjs`, `.jsx` | `references` | ✓ | ✓ | ✓ | | | |

An unchecked cell means *not certified*, not *disabled*. Graph tools still
answer for JavaScript, and `get_context` still works for PHP, Terraform,
Rust, HTML, CSS, Python and Zig, but those results are not covered by the tests the level names.
Treat them as best-effort.

Every language is analyzed statically: Ark never runs the language's
compiler, interpreter, package manager or build tool.

All languages share these rules:

- Names resolve only within the referencing file's language; TSX shares
  TypeScript's names ([name spaces](resolution-model.md#name-spaces-names-never-cross-languages)).
- A file the parser cannot fully read gets a `parse_error` diagnostic; the
  rejected region is not analyzed as written (`get_diagnostics`). The parser
  rejects some valid code; Ark retries alternative parse routes and keeps a
  tree only if it parses cleanly.
- Directories whose name starts with `.`, and `vendor` and `node_modules`,
  are not indexed.

## Go

**Extracted:** functions, methods (with receiver type), structs, interfaces
and the methods they declare (`Reader.Read`), named types, type aliases
(`type A = B`), package-level constants and
variables (every name of a spec and of a grouped `var ( ... )`); calls,
constructions (`T{}`, `pkg.T{}`), type uses (parameter, result, field,
variable and conversion types, type arguments, embedded fields, `pkg.T`),
value references (constants, package variables, functions passed as values,
`pkg.Var`, method values `t.M`, method expressions `T.M`, `(*T).M`,
`pkg.T.M`), writes (`x = v`, `x += v`, `x++`,
`pkg.Var = v`; a field write `v.f = x` is recorded for `find_references`,
fields are no symbols), imports (including aliases and dot imports).
Function-local declarations are not symbols, and their uses are not recorded.

**Resolution:** Go package scoping
([details](resolution-model.md#go-package-scope)). Package-qualified
functions, types and constants resolve `exact` through the import path;
same-package names resolve `exact` (same file) or `strong`. A local
declaration or a type parameter that shadows a name is respected.

A method call, method value or method expression resolves to the method
the type checker selects when the receiver's type is known:

- written down locally — the method receiver (`func (s *T) M()`), a typed
  parameter (`func f(r *Repo)`, `func f(t *pkg.T)`, `func f(l List[int])`),
  `x := &T{}`, `var x pkg.T` — exact or strong;
- or derived through declarations (strong): a function's or method's
  declared result (`s := pkg.New(); s.Run()`, `NewService().Place()`,
  `a, err := Open()`), a struct field's declared type in any file
  (`s.repo.Save()`), an element of a typed slice or map (`for _, it := range
  items`, `m[k].Run()`), a type assertion (`v := x.(*T)`), `new(T)`, a
  package variable (`var Default = &Client{}`), a conversion, an alias.

Promoted methods (`o.M()` with `M` declared on an embedded `Inner`) resolve
to the embedded type's method; a call through an interface resolves to the
interface's method — its static target, never a guessed implementation.
Generic receivers (`Box[int]`) resolve to the generic declaration. Methods of
named non-struct types (`type ids map[string]int`) resolve like any other. A
type name a type parameter or a local type shadows is no receiver type.

**Stays `candidate` or unresolved (never a wrong edge):**

- a receiver typed by a type parameter (`func f[T Stringer](t T) { t.String() }`)
  or by an interface literal;
- a derivation through a declaration outside the repository
  (`http.NewRequest(...).Header`), a function-typed value, a channel receive,
  a type switch binding, or a map's key;
- a promoted method beside or below an embedded type outside the repository
  (`struct{ sync.Mutex; Inner }`), or ambiguous at its depth;
- declarations duplicated across build-tagged files, or several programs in
  one directory (build tags are not evaluated): the declared types of either
  are no evidence;
- field reads (fields are no symbols);
- a name whose kind the syntax leaves open (`others[i](1)` with `others`
  declared in another file of the package) keeps the parser's reading: its
  target is right, its edge kind may be `uses_type` for a variable.

**Measured against `go/types`** (every identifier the type checker resolves,
joined with Ark's references; golang.org/x/tools v0.36.0, golang.org/x/exp,
cobra v1.9.1, go-cmp v0.6.0 and Ark itself; procedure
`docs/scripts/go-oracle.sh`, opt-in, not in CI): of 92,553 references Ark
resolved to one declaration the type checker could verify, none disagreed
with it, and none was claimed outside the repository wrongly; 99.2% of the
uses of repository declarations the type checker records resolve to the
right one (calls 98%, method calls, method values and method expressions
87–96%, promoted methods 79%, interface methods 84%, type uses and values
above 99%). What the oracle cannot judge — a package whose dependencies are
not in the module cache, a file the host's build constraints exclude — is
counted apart.

Go module paths are inferred from import paths, not read from `go.mod`. An
import path maps into the repository under a prefix two import paths share;
the module path itself (importing the root package) counts as one of them,
and with only one internal import path the repository directory's name must
equal the module path's last element. A module path without a host
(`module myapp`) maps only when the repository directory is named like it:
such paths may be the standard library's (`encoding/json` beside a
repository directory `json/`). An import that may name a repository
package but is not mapped stays unresolved — it is not claimed to be outside
the repository.

## TypeScript and TSX

**Extracted:** classes, interfaces, type aliases, enums, functions, `const`
components, and every class or interface member — constructors, instance,
static and abstract methods, properties, constructor parameter properties,
arrow-function fields (a getter/setter pair is one property). Module bindings
(named, aliased, default, namespace and type-only imports), export tables
(local, aliased, default, re-exports, `export *`, `export * as ns`, barrel
chains), references (calls, `this.m()`, static and namespace-qualified calls,
construction, type references, JSX components) and `extends` / `implements`.
The classes of a CSS Modules file a relative import of it uses, as in
JavaScript (below).
A static member is reached only through the class name and an instance
member only through an instance (`this`, a typed or constructed variable),
as in JavaScript.

**Resolution:** relative imports (`./user`, `../domain/user`, `./user.ts`,
`./user/index`) resolve through aliases and barrel chains (bounded and
cycle-safe) to the defining declaration. `./user` is looked up as `user.ts`,
`user.tsx`, `user/index.ts`, `user/index.tsx`, in that order. A member call
resolves only when its receiver's type is proven: `this`, an explicit type
annotation, `const x = new T()`, or a typed field or constructor parameter
property. Intrinsic JSX elements (`<div />`) are never repository references.
Files without `import` / `export` are scripts and keep the proximity rules.

| | |
|---|---|
| **Certified** | relative-import resolution, aliases, default / namespace / type-only imports, barrels, members under proven receiver types, `this` / static members, `extends` / `implements`, JSX components; members inherited through an identified supertype and `super.m()` (since the web semantic fidelity work, outside the certification benchmark) |
| **Unresolved by design** | external packages (`react`, `node:fs`), path aliases (`@/foo`, tsconfig `paths`), receivers without a proven type (`repo.save()` with an unannotated `repo`), members inherited through a supertype that is not identified (`extends` a package's class, a mixin call), declaration merging, computed access (`a[k]()`), `.js`-suffixed specifiers when both `.ts` and `.tsx` exist, anonymous default exports |
| **Not implemented** | return-type propagation and type inference, control-flow narrowing, overload resolution, `.d.ts` / `.mts` / `package.json` resolution, `tsconfig` interpretation, `namespace` bodies, enum members, destructured declarations, framework semantics (React, Next.js, NestJS, Angular), decorator and DI inference |

TypeScript module resolution is not compiler-equivalent: Ark ranks only the
repository-local, configuration-independent subset above. A static and an
instance member of one name are two symbols, and a reference to the name is
ambiguous; in a static method `this` and `super` are no receiver type.

A name a parameter, a local declaration, a nested function or class, or a
catch or loop binding shadows is never resolved `strong` or `exact` to a
top-level declaration or import of that name (`function f(helper) {
helper() }`); a local type declaration shadows a type the same way. A member
call on an exported object literal (`export const Cache = { get() {} }`,
then `Cache.get()`) is unresolved, not outside the repository. TypeScript's
utility types (`Partial<T>`, `Exclude<T, U>`, …) are no receiver type.

A declaration whose `export` keyword stands on the line before it is not
read as exported: an import of it stays unresolved.

## PHP

**Extracted:** namespaces, classes, interfaces, traits, enums, functions,
constants, methods, constructors, properties, class constants, enum cases,
promoted properties; `use` imports (plain, aliased, grouped, function,
const); calls (function, static, instance, nullsafe, `$this`), construction
(including `new self` / `new parent`), class-constant reads, type references,
`Foo::class` strings; `extends`, `implements`, trait `use`.

**Resolution:** class names resolve by exact qualified identity from the
file's `namespace`, `use` and fully qualified syntax. A class not declared in
the repository (a vendor class) stays unresolved and is never matched to a
same-named repository class; a name declared twice is a `candidate`.
Inherited and trait members (`self::`, `static::`, `parent::`) resolve
structurally — own declaration, traits, nearest parent, interfaces — through
repository-declared types only, and stop at `candidate` / unresolved when a
participant is outside the repository or ambiguous, a trait adaptation
(`insteadof` / `as`) names the member, or the member is private to a
supertype.

A receiver's type comes only from a declaration (a typed parameter, a
constructor-injected property) or from one `$x = new T()` / `$c = T::class`
statement whose block contains the use; an assignment inside a branch, loop,
`try` or condition is not evidence after it.

**Unresolved by design:** dynamic calls and construction (`$obj->$m()`,
`$fn()`, `new $c()`, except `$c = Foo::class; new $c()` with `$c` never
reassigned), container lookups (`make(Foo::class)` is a type use of `Foo`,
never a call to it), Composer / PSR-4 autoloading, framework semantics
(Laravel, Symfony).

**Known false `strong`:** function calls are resolved by name, not by
namespace — `\json_encode()` (the global function) can resolve to a
repository function `json_encode` declared in a namespace. References to
global constants are not extracted.

## Terraform

**Extracted:** `resource`, `data`, `ephemeral`, `module`, `variable`,
`locals` (one symbol per local), `output`, `provider` (with `alias`), `check`
(with its scoped `data` sources) and `terraform` blocks, and the dependencies
between them: every static address in an expression (`aws_vpc.main.id`,
`data.aws_ami.ubuntu.id`, `var.region`, `local.name`, `module.net`, including
templates, heredocs, conditionals, function arguments, `for` expressions,
splats, indexes and `dynamic` blocks), `depends_on`, and `provider` /
`providers` meta-arguments. `.tfvars` assignments are writes of the variables
they name.

Dependencies form `references` and `depends_on` edges, never calls:
`get_callees` lists dependencies; `get_callers` lists dependents
(`referenced_by`, `depended_on_by`).

**Resolution:** a module is a directory. A symbol's name is its address
(`aws_vpc.main`); its qualified name adds the module directory
(`modules/network/aws_vpc.main`). A reference resolves only within its module,
across that directory's `.tf` files: `exact` when the address is declared
once, `candidate` when twice, unresolved otherwise — even if another module
declares the same address. `module.NAME.OUTPUT` resolves to the child
module's `output` when the module `source` is a local path; outputs of
registry, Git or other remote modules are `outsideRepository`. A module input
argument is an edge from the call to the child's `variable`.

| | |
|---|---|
| **Supported** | the blocks above; same-module cross-file resolution; local module outputs (`module.x["k"].out`, `module.x[*].out`); module input arguments; `depends_on`; provider configurations and aliases; check-scoped data sources; override files (their references count, they declare nothing); broken files, as far as Tree-sitter's error recovery allows, with an error diagnostic — a name the parser makes up to recover is not extracted — `module.m.` references `module.m` and no output — nor is a name its recovery joins from the next line (`x = var.` followed by `y = 1`) |
| **Not references by design** | `count.*`, `each.*`, `self.*`, `path.*`, `terraform.*`, `for` and `dynamic` iterators, object keys, function names, `lifecycle.ignore_changes`, type constraints |
| **Unresolved or outside** | remote module outputs, `provider = x` without a `provider "x"` block in the module, addresses declared only in `.tf.json`, modules reached only through a symlinked directory, a module whose `source` an override replaces, computed sources |
| **Not implemented** | value flow from a module argument into the child, `.tf.json` / `.tfvars.json`, other `.hcl` files (Packer, Nomad, Terragrunt, Terraform tests), `moved` / `import` / `removed` blocks, implicit default providers, which module a `.tfvars` file feeds, remote state, Terraform Cloud workspaces |

**What the graph covers.** Within a module, every static address resolves
to its declaration (above). Across a module call whose `source` is a local
directory, the call's outputs and input arguments resolve to the child's
`output` and `variable` declarations. Nothing crosses a state: a
`terraform_remote_state` data source is an ordinary data source (its
`outputs.X` is an attribute, not an edge to the other configuration), an ID
or ARN written in a data source's arguments is a string literal, and a
`backend` block is not read — so which stack builds the VPC another stack
reads is not in the graph, and Ark does not guess it from equal strings.

**Overview and counts.** The repository map lists, per module, what it builds
— resources (one of each type before a second of any), module calls, data
sources — before its outputs and variables, and shows module calls of a local
directory as dependencies between modules. `search_code` with `groupBy`
counts resources by type (`resourceType`, with data sources as `data.TYPE`),
module (`directory`) or kind ([MCP tools](mcp-tools.md#search_code)).

**Comments are not configuration.** A commented-out block (`#`, `//` or
`/* … */`) declares nothing, is in no count and no edge; find one with
`search_in_files` (e.g. `^\s*(#|//)\s*resource "`).

Ark never runs `terraform`, downloads modules or providers, or reads
`.terraform/`.

## Rust

**Extracted:** functions, `struct`, `union`, `enum` (with variants), `trait`
(with its methods, associated types and constants), `impl` blocks (kind
`impl`) and their methods, associated functions, types and constants, named
struct and union fields, `type` aliases, `const`, `static`, `mod` (inline
and file modules), `macro_rules!` macros (kind `macro`), `extern` block
functions, and items declared inside function bodies. Each declaration's
signature keeps its attributes, so a `#[cfg(...)]` alternative shows which
configuration it belongs to. References: calls, struct and tuple-struct
construction, type uses (generics, bounds, `where` clauses, patterns),
value references to constants, statics and enum variants, `use`
declarations, macro invocations, and `impl Trait for Type` as an
implementation of the trait.

**Identity.** A declaration's qualified name is its Rust path, prefixed by
its crate: `crate::storage::Repository::new`. The crate and module come from
the file's place in Cargo's default layout — `src/lib.rs` and `src/main.rs`
are the package's crate root, `src/a.rs` and `src/a/mod.rs` module `a`,
`src/bin/x.rs`, `tests/x.rs`, `examples/x.rs`, `benches/x.rs` and `build.rs`
crates of their own. The repository root's package is `crate`; another
package's is its directory (`crates/util/crate::text::upper`); a binary's
its root file (`src/bin/tool.rs/crate::main`). A trait implementation's
items are `<Type as Trait>::name`, distinct from the inherent `Type::name`;
fields are `Type.field`; an item in a function body is `f::{block}::name`; an
exported macro is `crate::name!`. Cargo.toml is not read: a file Cargo
places elsewhere (`[lib] path`, `#[path]`, a nested `src` directory, a module
of `tests/` shared by several tests) is named by its file path instead and
reached by no path.

**Resolution:** only paths Rust's own scoping fixes resolve — `exact` when
one declaration has the path, `candidate` when several (`#[cfg]`
alternatives, two impls of one generic type):

| | |
|---|---|
| **Supported** | `crate::`, `self::`, `super::` (repeated), `Self` in impls and trait default methods; items and `use` bindings of the enclosing block or module (Rust modules do not inherit their parent's names); `use` aliases, nested `use` lists, `self` in lists; re-exports and imports of other files of the crate (`pub use storage::Repository;` in `lib.rs` makes `crate::Repository` reach the struct, `use a::b as m;` makes `m::x` reach `a::b::x`); a glob of a module or of an enum of the same file when it is the scope's only glob (`use self::Kind::*;`) — from outside the module only its `pub` / `pub(crate)` names; paths through inline modules; `Type::assoc` of an inherent item; enum variants; `macro_rules!` by textual scope within the file, `#[macro_export]` macros by path; trait implementations (`implements` edges) |
| **Candidate** | `#[cfg]` alternatives of one item (and of an item and an import or alias of its name); a `use` path starting with a plain name outside the crate root (Rust 2015 reads it from the crate root, 2018 from the scope or another crate; the edition is in Cargo.toml) — and what a path through such an import reaches; a module and a function of one name (`mod parse; pub use parse::parse;` makes `crate::parse` both); a name both `src/lib.rs` and `src/main.rs` declare (they share the crate key `crate`) |
| **Unresolved by design** | method calls on values (`v.m()`, `self.m()`: type inference and trait resolution); names a local binding, closure parameter, pattern or generic parameter shadows — or a macro statement of the file may bind (`let $n = …` with a name the invocation supplies); a name a glob of another file, an item-level macro, a non-standard macro statement in the block, or a derive or attribute macro other than the compiler's (`#[derive(EnumIter)]`, `#[my_attr]`) may bring; a `#[cfg]` item in a scope with a glob; a private or `pub(super)` name a glob brings from outside its module; a name two `use` declarations bind; a `#[path]` module's items; trait items reached through a type (`Type::default()` without an inherent `default`); `<T as Trait>::f` and other qualified paths; computed callees (`handlers[k]()`, reported as dynamic) |
| **Not implemented** | paths into another crate of the repository (`my_lib::f` from a binary or test, path dependencies: the crate name is in Cargo.toml), glob re-exports through another file (`pub use self::types::*;`), Cargo workspaces and `[lib]` / `[[bin]]` paths, editions, features, procedural macros, derives, `macro_rules!` expansion, `include!`, build-script output |

**Cargo.** Ark recognizes Cargo's *default directory layout* — which file is
a crate root, which crate a file belongs to, a package's directory — and
nothing of Cargo's *configuration*: it does not read `Cargo.toml`, so it
does not know a workspace's members, a package's or crate's name, path
dependencies, `[lib]` / `[[bin]]` paths, the edition or features. Two crates
of a workspace are separate identity spaces (`crates/util/crate::helper` is
not `crate::helper`), and a reference from one to the other
(`util::helper()`) is unresolved, not wrong.

**Measured against the compiler.** On 71 crates.io packages without
dependencies, build scripts or procedural macros (rustc 1.58 save-analysis;
docs/scripts/rust-oracle.sh), every reference Ark resolved to one
declaration that the compiler also recorded resolved as the compiler did;
references the compiler did not record (code another configuration
compiles, bounds, `Self::X`) are not counted. This holds for that corpus
and configuration, not as a proof.

A file the parser rejects (for example Rust 2015 `try!(...)`, read as the
`try` keyword) is reported by `get_diagnostics`; its rejected region is not
analyzed. Parsing it costs a few hundred milliseconds more than a file the
parser reads ([Performance](performance.md#rust)).

Unit tests in `#[cfg(test)] mod tests { ... }` are ordinary items of their
file; files under a `tests/` directory and `tests.rs` modules are test files.

Ark never runs `cargo`, `rustc` or build scripts, and never reads
`Cargo.toml` or `Cargo.lock`. A `target/` directory is skipped only as
`.gitignore` excludes it (as `cargo new` writes it): code generated into
`target/` would otherwise be indexed like any source.

## HTML

**Extracted:** the document (kind `document`, one per file, named by the
file) and every element with an `id` (kind `element`, its start tag).
References: an element's id named by an IDREF attribute — `for` (`label`,
`output`), `form`, `list`, `headers`, `popovertarget`, `commandfor`,
`itemref` and the ARIA relationship attributes (`aria-labelledby`,
`aria-describedby`, `aria-controls`, `aria-owns`, `aria-flowto`,
`aria-activedescendant`, `aria-details`, `aria-errormessage`) — or by a
fragment (`<a href="#x">`, `<area>`, SVG `href` / `xlink:href="#x"`,
SVG `fill="url(#x)"` and the other paint, clip, mask, filter and marker
attributes) (kind `id_reference`); and a link to another HTML document of
the repository by a relative path (`<a href="docs/b.html">`, `<area>`,
`<iframe src>`, `<frame src>`; kind `resource_link`, or `id_reference` with a
fragment: `b.html#x`); and the stylesheets and scripts a page loads by a
relative path (`<link rel="stylesheet" href="a.css">` to the CSS stylesheet,
`<script src="a.js">` — no type, `module` or a JavaScript type — and
`<link rel="modulepreload">` to the JavaScript file's `script` symbol;
`resource_link`, Strong at most). These are the only relations between two
languages, by path, never by a name: an `id="run"` is not a JavaScript
`run()`.

**What the browser sees.** The file is parsed as the HTML standard does
(golang.org/x/net/html): an id or a reference counts only when the
standard's tree has it on the element its own tag builds — text inside
`<script>`, `<style>`, `<title>` or SVG `CDATA` that merely looks like a tag
is none, nor is a later duplicate attribute, an attribute merged away
(`<body>` written twice), or a tag the standard ignores. A tag cut off by the
end of the file is dropped, as the standard does, and reported as an error
diagnostic (`html_eof_in_tag`). Where Ark's parser cannot build the standard's tree to the
end (a `<template>` inside SVG `<foreignObject>`, `<desc>` or MathML text, or
a document whose last tag the parser ignores, such as a frameset's), it
states no id and no reference for the file and reports
`html_tree_unverified`.

**Identity.** An element's qualified name is `<file>#<id>`; inside a
`<template>`, whose content is a tree of its own, `<file>#<k> <id>` with `k`
the template's ordinal in the file. A document's is its path. References
reach a declaration only by this identity, never by a matching name in
another file.

**Resolution:**

| | |
|---|---|
| **Exact** | an id reference within a static document, to the one element declaring the id in the same tree (the document, or one template's content) |
| **Strong** | the same in a document whose DOM may change at run time (a `<script>` — also one only the browser builds, inside an SVG `<style>` —, an `on*` attribute, a `javascript:` URL, an `<iframe srcdoc>` or a frame, object or embed loading another document (of the same origin, it may change this one), template syntax such as `{{ }}`, `{% %}`, `<% %>`, `<?php`, a framework binding or control flow such as `[id]`, `*ngIf`, `@for (…)`, `@foreach(…)`, `{#each …}`, `v-for`, `x-data`, `th:each`, a server-side include `<!--#include …-->`); a link to another document, or to an id in one, and a page's stylesheet or script (which file a URL serves depends on the server) |
| **Candidate** | an id declared twice in one tree; a reference inside a `<template>` to an id of its content — the tree its content is cloned into is not known — unless the template is a declarative shadow root (`shadowrootmode`, on an element that can host one and has no earlier one below it, outside any other template); a hyperlink's `#x` always names the document's element |
| **Unresolved** | an id no element declares, a missing document (never outside the repository) |
| **Not stated** | `<a name>` and `<map name>` targets, `usemap`; an id the browser gives to more elements than Ark can see (an element inside an SVG `<style>` or `<title>`, a reconstructed formatting element) — it declares nothing then, so no reference resolves to it; `<iframe src="#x">`; run-time changes by SMIL (`<set attributeName="href">`); the form-associated custom elements' `form`; root-relative (`/x.html`) and absolute URLs, `//cdn/…`; directory links (`docs/`); links and fragments of a document with `<base href>`; percent-encoded and text fragments, `#top`; ids and references holding template syntax or whitespace; files that are neither a source nor an asset (below), and an asset attribute the browser does not fetch (`source src` in `picture`, `rel=prefetch`, `preload` of another destination, `a href` to an image); a stylesheet or script the browser does not fetch (a `<script>` type or `language` that is no JavaScript, a stylesheet `type` other than `text/css` — an empty one included —, a `disabled` stylesheet, a `modulepreload` of a destination that is no script); `.ts` and `.cjs` scripts; `style` attributes and event handler attributes (`onclick`); in an inline script, references with a receiver (`a.m()`, `this.m()`), imports and exports (CSS Modules included); a script that does not run — one the end of the file closes, a classic `nomodule` script, a classic script whose `for` / `event` is other than `window` / `onload`, one with `src`; an inline `<style>` or `<script>` inside a `<template>` (a declarative shadow root's included), in MathML, holding a NUL, or in SVG holding a character reference or a CDATA section (an SVG `<script>` with `href`, or of type `module`) |

**Graph.** An id reference forms a `references` edge from the document to the
element (`referenced_by` in reverse), a link a `links` edge to the other
document (`linked_from` in reverse). An unresolved reference to another
document's id is reported by the id's name (`x` of `b.html#x`).

**Assets.** Images, fonts, audio, video and text tracks (`.png .jpg .jpeg
.gif .webp .avif .svg .ico .bmp .apng .woff .woff2 .ttf .otf .eot .mp4 .webm
.ogg .ogv .oga .mp3 .wav .m4a .flac .vtt`) are nodes of the index (kind
`asset`, named by their path; their content is never read, and a symbolic
link is none). A document links to one (`links`, Strong at most) from `img
src` and `srcset`, `source src` (in `video` / `audio`) and `srcset` (in
`picture`), `video poster` / `src`, `audio src`, `track src`, `input
type=image src`, `link` `rel=icon` / `apple-touch-icon` or `rel=preload` with
`as` image, font, audio, video or track, and SVG `image` / `use` `href`; a
stylesheet from `url()`, `src()` and `image-set()` strings in a declaration
(`@font-face src` included). A missing file, or one named with another case,
is unresolved (never outside); root-relative, absolute and protocol-relative
URLs and a document with `<base href>` state no link. An asset has a node
only when something links to it, and its location is line 0 (it has no
text): tools print it as `path:0`.

**Inline style and script.** The text of a `<style>` (no `type`, or an empty
one or `text/css`) is read as CSS, and that of a `<script>` without `src`
whose type is JavaScript as JavaScript, each by its language's extractor;
their locations are the document's. A style element's declarations have the
identities a stylesheet of the document's path would (`page.html @keyframes
spin`, `page.html --x`), shared by the document's style elements, and resolve
as in a stylesheet (an `@import` links to the stylesheet, unless the document
has a `<base href>`). A classic script's top-level declarations are the
document's globals, `page.html init` (and `page.html Widget.run` for a
member); a module script's are its own, `page.html module#1 init` (the
document's first module script). A script's call or `new` of a name reaches
the document's declaration of that identity only — never another
document's, nor a JavaScript file's — `strong` at most (an external script
may declare the same global; scripts run in order); in a module script, its
own declaration first, then a global. An embedded declaration is not
exported. A text is read only when the standard's tree holds exactly that
text as the element's content.

`ark syntax` prints no syntax tree for HTML (there is no Tree-sitter grammar,
so `--lang html` always fails there); `ark symbol`, `get_symbols` and the
other tools work as for every language.

## CSS

**Extracted:** the stylesheet (kind `stylesheet`), `@keyframes` (kind
`keyframes`, an ident or string name), the family an `@font-face` declares
(kind `font_face`), `@property` registrations (kind
`property_registration`), custom property declarations (`--x: …`, kind
`custom_property`), and, in a CSS Modules file (`*.module.css`), its local
classes (kind `css_class`; not inside `:global(…)` or after `:global`).
References: animation names (`animation-name`, and the `animation`
shorthand when it holds exactly one name), font families (`font-family`;
generic families excluded), `var(--x)` (fallbacks and nested `var()`
included), `composes` (CSS Modules) and `@import` of a stylesheet by a
relative path. An `@import` counts only where the browser reads it: at the
top level, before every rule but `@charset`, `@layer` statements and other
`@import`s. An `@font-face` names a face only with a `src` (its last valid
`font-family` names it); `@keyframes`, `@font-face` and `@property` nested
in a style rule declare nothing (in `@media` or `@supports` they do).

**Reading.** Ark reads CSS as CSS Syntax Level 3 has it (its own
implementation; nested rules and custom properties as the current draft
has them): the byte order mark, newlines, NUL and invalid UTF-8 as the
standard decodes them, the browsers' recovery from errors. Where written
content is lost or cannot be read it reports an error diagnostic
(`css_bad_url`, `css_bad_string`, `css_rule_from_declaration` — a
declaration whose `;` is missing before a nested rule —,
`css_stray_in_prelude`, `css_encoding`); where nothing is lost a warning
(`css_invalid` — e.g. the `*zoom: 1` hack —, `css_eof_in_block`,
`css_eof_in_comment`, `css_eof_in_string`, `css_eof_in_url`,
`css_replacement_character`, `css_charset`, `css_too_deep` for blocks
nested past 256 levels).

**Identity.** A declaration's qualified name holds its file:
`styles/main.css @keyframes spin`, `styles/main.css @font-face inter var`
(families in lower case: they match case-insensitively),
`styles/main.css --brand`, `components/card.module.css .title`. A reference
reaches a declaration only of its own stylesheet (or the file a `composes
… from` or `@import` names): which stylesheets one page loads together is
not CSS's to say.

**Resolution:**

| | |
|---|---|
| **Exact** | `composes: name` of a class of the same CSS Modules file |
| **Strong** | an animation name or a font family declared once in the same stylesheet (another stylesheet of the page may define the name later and win); `composes: name from "./other.module.css"`; `@import` of a repository stylesheet (which file a URL serves depends on the server) |
| **Candidate** | `var(--x)` of a custom property the stylesheet declares (its value comes from the element tree's inheritance); a name declared twice in the stylesheet (two `@font-face` weights of one family) |
| **Unresolved** | a name the stylesheet does not declare — also one another stylesheet declares — and a missing `@import` target (never outside the repository) |
| **Not stated** | `url()` in a custom propertys value (resolved where it is substituted); an `@import` after another rule (a `@layer` statement after an `@import` included) or with a block; root-relative and absolute URLs; `composes: x from global`, `@value`; families in the `font` shorthand; names computed by `var()` or holding template syntax (`{{ }}`, `{% %}`, …); the classes of a stylesheet that is not a CSS Modules file (global names with no declaration site); selectors and the elements they match |

**Graph.** A name reference forms a `references` edge from the stylesheet
to the declaration (`referenced_by` in reverse), an `@import` a `links`
edge (`linked_from`). `var()` forms no edge (Candidate).

`ark syntax` prints no syntax tree for CSS (there is no Tree-sitter grammar,
so `--lang css` always fails there); `ark symbol`, `get_symbols` and the
other tools work as for every language.

## JavaScript

Level `references`.

JavaScript is extracted by the extractor TypeScript uses: the two languages
share declarations, module bindings and exports, class members, receiver
evidence and scopes. JavaScript keeps its own name space — a JavaScript file
and a TypeScript file never resolve each other's names, even through an
`import` (a known gap, not a design choice).

**Extracted:** the file itself (kind `script`, the target of an HTML
`<script src>`), top-level functions, classes, `const` / `let` / `var`
declarations and their exports; class members (methods, constructors,
fields, accessors); the members of a top-level `const` object
(`const api = { get() {}, n: 1 }`, and `api.m = function () {}` assigned
later) and a constructor function's prototype methods
(`F.prototype.m = function () {}`, `F.prototype = { m() {} }`); ES module
imports, exports and re-exports; the classes of a CSS Modules file used
through a relative import of it (`import s from "./a.module.css"` or
`import * as s` and then `s.btn`, `s?.btn`, `s["btn"]`; `import { btn }`
and then `btn`), which reach that file's class `.btn` by its path —
`strong` at most (a bundler setting may export a class under another name),
unresolved when the file declares no such class (TypeScript and TSX
alike; also `export default s.btn` and a destructuring of the class map,
`const { btn, "x-y": xy } = s`, at its keys); CommonJS `const x = require("./m")`,
`const { a, b: c } = require("./m")`, `const y = require("./m").y`, and
top-level `module.exports = x`, `module.exports = { a, b: c }`,
`exports.a = f`; calls, `new` and JSX components; reads of a value — a
function passed as an argument, a constant in an expression, the object of a
member access, `export default x`, `export { x }` — of a name the file
declares at its top level or binds by an import or a require, where no local
declaration hides it (`references` edges, as Go's value references).

**Resolution (not certified):** a file with `import` / `export` or CommonJS
is module-scoped: a name resolves through its own declarations and its
bindings, never by name similarity across the repository. `./x` is `x.js`;
without it, one of `x.jsx`, `x.mjs`, `x.cjs` or `x/index.*` (several existing
files are ambiguous). A file with neither (a classic script) reaches by a
free name another classic script's declaration, never a module's (exported
or not: a module's top level is its own scope). A CommonJS module that
replaces `module.exports` exports none of its `exports.a = …` properties, an
export another assignment overwrites (`module.exports = { a }` then
`module.exports.a = b`, `exports.a = function () {}` after `exports.a = a`)
is not stated, and a `var` / `let` require the file
reassigns binds nothing; `const u = require("./u")` of a module that does not
replace `module.exports` is its namespace (`u.a()`). A reference through a
CommonJS export — a require, an `import` of a CommonJS module, a re-export of
one — keeps the exported declaration as a `candidate`, never `strong` or
`exact`: the exports object is a plain object code may change at run time by
routes no static rule closes, so Ark records which declaration the source
exports but does not claim the value an importer reads is that declaration.
The module's own references to its declarations, and ESM and TypeScript
exports, keep their confidence. A name a parameter, a
local declaration, a nested function or class, or a catch or loop binding
shadows is never resolved `strong` or `exact` to a top-level declaration or
import of that name. A member call resolves under a proven receiver (`this`,
`const x = new T()`, a `const` object, a constructor function's `this`),
through the class's supertypes (`extends` an identifier its import or file
declares) and for `super.m()`; a static member is reached through the class
name, an instance member (a prototype's included) through an instance —
`Clock.now()` is never the instance member `now`, `new Clock().tick()` never
a static `tick`, and a constructor function's name never reaches its
prototype's members; a member of a value (an object, a prototype) is
`strong` at most — code may reassign it. A package import
(`require("express")`) is outside the repository — also when the package is
the repository itself (`import … from "preact"` inside preact): Ark does not
read `package.json`.

**Not modelled:** CSS Modules through `require` (with css-loader's default
`esModule`, it returns the module namespace, not the class map), `import()`,
`{ default as s }`, aliases (`@/a.module.css`) or a
converted name (`s.fooBar` for `.foo-bar`); an import with attributes
(`with { type: "css" }`) is a stylesheet object, no class map, and binds
none; a use of a class is a read of a value (an argument, an operand, an
initializer, a JSX expression, …), never a callee, `new`, an assignment's
target, `delete`, a parameter or a name in a type; a function's own properties (`F.m = …`), objects held by
`let` / `var` or with a declared type, members added at run time
(`Object.assign`, mixins), computed access (`a[k]()`), dynamic `import()`,
other CommonJS shapes (a require inside a function or branch).

## Python

**Extracted:** functions and classes at any depth — inside `if`, `try`,
`with`, `for` and `while` blocks too (a block is no scope in Python) —
methods (kind `method`; `@property` methods kind `property`), nested
functions and classes (`outer.inner`), module variables (kind `variable`)
and class attributes (kind `field`). Every plain or annotated assignment
statement of a name is a declaration of its own, and an attribute a method
assigns through its proven `self` (`self.x = …`) is a field of the class. A
`@typing.overload` signature and a property's `@x.setter` / `.getter` /
`.deleter` are no declarations of their own. References: calls (of a name,
of `a.b.f`, of a member of another expression), decorators (a call of the
decorator, whose container is the decorated declaration), base classes
(`extends` edges), type uses in annotations (string annotations are not
read), imports, and computed calls (`getattr(o, n)()`, `o[k]()`: dynamic).

**Scopes.** A name is looked up as Python looks it up (LEGB): a name bound
anywhere in a function is local to all of it, before its assignment too; a
class body is no scope of its methods; `global` and `nonlocal` declarations
redirect a binding; an assignment expression binds in the enclosing
function, not in its comprehension; a comprehension's first iterable belongs
to the enclosing scope; decorators, defaults, annotations and base classes
are read in the scope that defines the function or class, annotations of a
generic one (PEP 695) in its annotation scope, which sees the names of the
class body it is in. A module or class body runs top to bottom: a name it
reads before its first binding there is another binding (a builtin, the
module's). Every binding form counts: parameters, assignments, `for`, `with … as`, `except … as`,
`import`, `def`, `class`, `del`, comprehension targets, assignment
expressions, `match` captures and PEP 695 type parameters.

**Modules.** Imports are bindings (`import a.b`, `import a.b as m`,
`from m import f as g`, relative imports `from . import x`,
`from ..m import f`), resolved to the file the module name denotes: a module
`a.b` is `a/b.py` or the package `a/b/__init__.py`. A relative import is
read from the importing file's package directory. An absolute one is read
from the roots of `sys.path` as Ark models them: every ancestor directory of
the importing file that is **no package** (has no `__init__.py`) — the
repository root included — and `src/`. So a package's own `json.py` is never
what `import json` in that package means, while a script's sibling module is
what the script imports. A module's attributes are its declarations and the
names it imports (re-exports: `from .app import Flask` in `__init__.py`),
and a package's `__init__.py` that imports from a submodule
(`from .sub import x`) gives the package the attribute `sub`.
`from pkg import n` is the attribute `n` of `pkg` when `pkg` binds one —
even to a value Ark cannot prove (a loop variable, a fallback assignment, a
name a star import or `__getattr__` may supply: then it is unresolved) — and
otherwise the submodule `pkg.n`. An import in a function binds the name in
that function and the functions nested in it.
Ark reads no configuration (`pyproject.toml`, `setup.cfg`, `PYTHONPATH`,
`.pth` files): a module the repository does not have (the standard library,
a dependency) is unresolved — never `outside` either, since nothing proves
the repository has no such module.

**Resolution:**

| | |
|---|---|
| **Supported** (`exact`) | Functions, classes and variables of the module; a nested function called in its enclosing function or a function nested there; imports, aliases, relative imports, package re-exports, submodules (`pkg.sub.f()` after `import pkg` when `pkg/__init__.py` imports from `sub`), imports inside a function; `self.m()` and `cls.m()` in a method — the class's own member, else one a base declares, through the bases Ark identifies; `ClassName.m()`; a call of a class (construction); base classes; type uses |
| **Supported** (`strong`) | `super().m()` in a class with one plain base (under cooperative multiple inheritance, the class `super()` reaches is decided at run time) |
| **Decorators** | A decorated declaration is a symbol and keeps the edges of its body's calls, whatever the decorator. A reference to its name resolves to it (`exact`) only under decorators Ark knows, by their documented semantics — the same object (`abc.abstractmethod`, `typing.override` / `final` / `runtime_checkable` / `no_type_check`, `functools.wraps`, `functools.total_ordering`, `dataclasses.dataclass`, `enum.unique`) or a descriptor or wrapper that runs the body (`staticmethod`, `classmethod`, `property`, `functools.cache` / `lru_cache` / `cached_property`); `typing.overload` signatures are no declarations. They are known only in their documented forms — `functools.wraps(f)` called, `lru_cache` and `dataclass` applied or called with configuring arguments only (`lru_cache(maxsize=1)`, not `lru_cache(f)`), the others applied (`@abstractmethod(f)` decorates with `f`'s result), the descriptors (`property`, `classmethod`, `staticmethod`, `cached_property`) in a class body — recognized through the file's import of the module (a builtin only where nothing in the file rebinds it), and only where no star import and no assignment of the file (`functools.cache = …`) may rebind them; an accessor (`@x.setter`) only of a property `x` the class body declared before. Under any other decorator — one may return another function, class or object — a reference to the name, to a member of such a class, or found through such a class as a base is a `candidate`, never an edge; so is a stack with one unknown decorator in it |
| **Candidate** | A name defined or assigned more than once (`if` / `else` branches, a `try` / `except ImportError` fallback, a class rebound by `X = wrap(X)`); a module name bound both by a declaration and an import; a module and a package of one name, or one module under two roots; a member two bases declare (MRO conflicts are not resolved); a member that is both a method and an attribute (`self.handler = h` beside `def handler`, or set by a base's method: an instance's own attribute hides the method); a member the class body rebinds (an import, a loop, `del`, `global`) or the file assigns through the class (`C.helper = other`); a name a nested declaration of an enclosing function and another declaration share; a class defined twice; a member called on a value whose type no declaration proves (`user.save()` on a local, `self.repo.find()`, a parameter's methods: every member of the name is a candidate) |
| **Unresolved by design** | Names a local binding may denote (parameters, assignments, loops, `with`, `except`, comprehensions, `match` captures, rebinding through `global` / `nonlocal`, `del`); builtins (`open`, `print`); names a star import brings, and a module-level name a later star import may rebind; `self` in a method under an unknown decorator, in a method whose name the class body rebinds (`f = staticmethod(f)`), or in a class nested in a class body; `getattr` / `setattr`, `importlib` / `__import__`, attributes a metaclass or `__getattr__` creates |
| **Not implemented** | Re-exports through a star import (`from .core import *` in `__init__.py`); the types of local variables (`x = C(); x.m()`); value references (a function passed as a value) and attribute reads; string annotations; `.pyi` stubs (not indexed); Python 2 implicit relative imports; the configuration of `sys.path`; monkey patching from another file (`util.helper = other` elsewhere: an assignment is seen only in the file of the class it changes), `exec`, and writes through `globals()` / `vars()` / `setattr` — the name still resolves to the declaration it was bound to, Exact, though the program replaced it |

**Measured against pyright.** On requests 2.32.3, flask 3.0.3, click 8.1.7
and attrs 24.2.0 (pyright 1.1.414 with each package's dependencies installed;
`docs/scripts/fidelity-baseline.sh` and `docs/scripts/oracle/python-pyright.py`),
no reference Ark resolved to one declaration (Strong or Exact) contradicts
pyright, and none is reported outside the repository. The references pyright
names no declaration for — code it reads as unreachable on the measuring
machine (`if sys.platform == "win32":`), a stub-only package (attrs) — were
classified by hand: none of Ark's targets there is wrong. This holds for
those corpora and that configuration, not as a proof.

A directory is a package when its `__init__.py` is indexed: one `.arkignore`
excludes makes the directory a root of absolute imports to Ark. The
`sys.path` model is a model: a module run with `python -m pkg.mod` does
not have its own directory on `sys.path`, so in a directory without
`__init__.py` (a namespace package) a module named like a standard-library
one (`app/logging.py`) is what Ark resolves `import logging` there to. Known
decorators are recognized through the file's own imports (`functools`,
`typing`, …): a repository module of the same name at a root is taken for
the standard library's.

Ark never runs Python, a build backend or `setup.py`, and reads no
configuration file.

## Java

**Extracted:** classes, interfaces, enums, records (kind `class`) and
annotation types (kind `interface`) at any depth — member types, local
classes, anonymous classes (an enum constant's body is one) —, methods,
constructors (named `<init>`), fields, enum constants (kind `constant`),
record components (fields) and annotation elements (methods). A method's
and a constructor's symbol state its parameter types. References: calls,
constructor calls (`new`, `this(…)`, `super(…)`, an enum constant's
arguments), class instance creations, method references (`X::m`,
`X::new`), type uses (declarations, type arguments and bounds, casts,
`instanceof` and patterns, `throws`, `catch`, annotations, class literals,
`permits`, the outer names of a qualified name), `extends` / `implements`,
and field reads and writes.

**Identity.** A type is named by its canonical name, `com.acme.Outer.Inner`
(a type of the unnamed package by its simple name); a member by its type's
and its own, `com.acme.User.befriend`, a constructor `com.acme.User.<init>`.
A local class is `ENCLOSING.method.Local`, an anonymous class
`ENCLOSING.<anonymous>` (`#2`, `#3` for the next ones of the file): names no
other file can write. Overloads share their name — `get_callers` on one
lists them with a `symbolId` each, never picks one.

**Resolution.** Ark looks a name up as Java does (JLS 6.4, 6.5, 15.12) and
asks the repository what the file cannot see: the members a supertype
declares, the package's other files, the types an on-demand import brings.
A local variable, parameter or pattern binding is the file's own and no
reference; a type parameter is no type of the repository.

| | |
|---|---|
| **Supported** (`exact`) | A type of the file, a single-type import, a member type of an enclosing type or one it inherits, a type of the package in the reference's source root; members of a type and those it inherits — through `this`, `super`, a type name, a local variable, parameter or field with a declared type, a cast, `new T()`, `var x = new T()`, a type variable's one bound —; unqualified calls searched in the innermost enclosing type that has a method of the name (JLS 15.12.1); static imports of methods, fields and member types; overloads by argument count and by the argument types the file proves (literals, typed variables, casts, `new`, `null`, lambdas), phase by phase (strict, loose, variadic), the most specific of primitive ones — a reference-typed argument rules out primitive parameters only (`h(long)` / `h(String)`), never one reference type for another; only the methods and constructors the call may access (a private one within its top-level class, a package-private one within its package); an override of a method of no parameters; inherited overloads beside a subclass's own; private members are not inherited, package-private ones only within their package, an interface's static methods not at all, and a field hides a supertype's of the name |
| **Supported** (`strong`) | What is found past a scope the repository may not hold: another file of the package in another source root, an on-demand import (`import a.*`, `java.lang`), a type whose supertype is outside the repository (an own method of a class extending a library class: the library may declare the overload the call means), an anonymous class of a library interface; a declaration only another source root holds (`src/test` → `src/main`, another module); an override presumed from equal parameter type names |
| **Candidate** | Overloads the arguments do not tell apart — those that differ only in reference parameter types (`f(B)` / `f(C)`, `g(B)` / `g(String)`) whatever the arguments —, or that only a protected overload of another package (which the call may not access) would tell apart or would leave the later phases untried; a class declared in two source roots, neither of them the reference's (two flavors of a library seen from a third module); a method an outside supertype may implement beside an interface's declaration; an own method marked `@Override` whose overridden one is outside the repository; members of a type the parser could not fully read, and what is found past one |
| **Unresolved by design** | Receivers that are a method's result or another type's field (`a.b().c()`, `x.f.g()`); lambda parameters of inferred type; a `var` initialized otherwise; a switch label's enum constant (the selector's type decides it); annotation element names; anything in a region the parser rejected; the members `java.lang.Object`, `Enum` and `Record` give, an enum's `values()` / `valueOf`, a record's implicit accessors and canonical constructor, a class's default constructor (found, no declaration) |
| **Not implemented** | Return and field types (a call on a call's result); generic inference; argument types the file does not prove (another method's result); Maven, Gradle and module descriptors (`module-info.java`): dependencies, modules, generated sources; dynamic dispatch — a call names its static declaration, not the override that runs |

**Context.** `get_context` items are the graph's edges only, each with its
edge's confidence: the overload and the constructor a call names, the
supertypes a type extends and implements, a declared receiver's member (a
call through an interface is that interface's method, never an
implementation), and the callers of a declaration. What the graph leaves a
candidate — overloads its arguments do not tell apart, a receiver typed by a
method's result, a class two source roots declare — is no item, only an
unattributed count. Overloads share their qualified name: the ambiguity
listing gives each a `symbolId`, which selects it with the listed name. A
field and a method may share a name too (`User.name` and `User.name()`):
they are two items, told apart by their lines and kinds.

**Source roots.** A file's source root is where its package's path starts
(`src/main/java/` of `src/main/java/com/acme/User.java` in package
`com.acme`). A class two roots declare — the JRE and Android flavors of one
library, a test copy — is the reference's own root's when that root has it;
one only another root has is `strong`: the build may not compile them
together. Build files are not read.

**Measured against javac.** On spring-petclinic, Apache Commons Lang,
google-java-format, Gson, Guava and JUnit 5 (javac 21 with the projects'
main dependencies; `docs/scripts/oracle/java-javac.java`, the opt-in
`TestOracle_Javac`), 179,553 references resolved `exact` and 184,187
`strong` agree with javac but two `strong` ones (a call of a class
extending `junit.framework.TestCase` that javac binds to an overload
`TestCase` declares); references Ark puts outside the repository agree too.
Names javac could not attribute (a dependency it did not have, a file
javac 21 fails on) are not counted. This holds for those corpora and that
configuration, not as a proof.

The parser rejects a few valid constructs — a type annotation before a
variadic parameter's `...` (`Class<?> @Nullable... types`), Java 25 compact
source files —: such a region is reported by `get_diagnostics`. Rejected
outside a method body, it may hide an import, an `extends` clause or a
declaration: the file states no references, and every type it declares is
capped at `candidate` — a member found in it, or past it in a subtype's
lookup, is a candidate, and a member it does not show is not reported
outside the repository; inside a body, that body states no references.

Files named `Test*.java`, `*Test.java`, `*Tests.java`, `*TestCase.java` and
those under a `test/` or `tests/` directory are test files. Ark never runs
`javac`, Maven or Gradle and reads no build file.

## Zig

Zig 0.15 to 0.17 (the current release, 0.17.0), as the grammar
[tree-sitter-zig](https://github.com/tree-sitter-grammars/tree-sitter-zig)
parses it (its corpus follows Zig 0.15). Older code parses too, but
constructs Zig has removed are read conservatively: a container with
`usingnamespace` (removed in 0.15) may bring any name, so names it could
bring stay unresolved.

**Extracted:** each file itself (a Zig file is a struct; kind `module`),
functions (`fn`, `pub`, `export`, `extern`, `inline`), methods (functions of
a container), `const` and `var` declarations, `struct`, `union`, `enum`,
`opaque` and error sets (`error{...}`) with their fields, enum fields and
error names, containers declared in function bodies (a generic type
factory's returned struct), `test` declarations (`test "name"`, `test decl`).
A constant whose value is a type by its syntax (`*const T`, `?T`, `E || F`,
`@This()`) or a container of the file is a `type_alias`; an `@import` a
`module`. Function parameters, locals and captures are analyzed for scoping,
not extracted. References: calls, `T{...}` constructions, type uses, value
references to declarations, imports (`@import`), decl and enum literals
where a declared type fixes their result type (`const e: Engine = .init();`,
`.red`), `@This()` as a use of its container, and calls of computed callees
(`handlers[i]()`, reported as dynamic).

**Identity.** A declaration's qualified name is its file's
repository-relative path followed by the containers down to it:
`src/engine.zig::Engine::run`. A struct or union field is
`src/engine.zig::Engine.count` (no member access names a field); a
declaration in a function body is `f::name`; the one container a function
returns (`fn List(comptime T: type) type { return struct {...}; }`) is
`src/list.zig::List::()`, and an unnamed container elsewhere is
`...::{struct}`. The file itself is `src/engine.zig`, which a relative
`@import` of it names. Get a declaration by its Zig path: `Engine.run`.

**Resolution:** only what Zig's own rules and the files' declarations fix
resolves — `exact` when one declaration has the identity:

| | |
|---|---|
| **Supported** | names by lexical scope (a block's locals before their declaration, parameters, captures, the declarations of the enclosing containers, innermost first, through the function bodies a container is declared in); members of a container of the file, of a file a relative `@import` names (`@import("../x.zig")` from its directory), and of a constant naming either — an import, `@This()`, a path, a re-export of another file (`pub const Vec = @import("vec.zig").Vec;`) — through any chain of files; generic type factories (`List(u32).append`, `var l: List(u8)`, also of another file) whose one return is a container; method calls on a receiver whose type the file proves: a declared type (`v: T`, `*T`, `*const T`, a parameter `self: *Self`), a construction (`T{...}`), a call of a function whose written result type is a container (`T.init()` returning `Self`, through `try` / `catch` for an error union — also of another file), a field of a struct of the file; decl and enum literals of a declared type; named modules (`@import("stdx")`) by the import table of the importing file's module, as a closed `build.zig` states it (below) — `m.addImport("name", other)`, `.imports = &.{ .{ .name, .module } }` (also chosen by a comptime condition, or copied from another module's `import_table`), where a module is a `b.createModule` / `b.addModule` with a literal `.root_source_file = b.path("...")`, an artifact's `.root_module`, a dependency's `.module("name")` (the module a build of the repository registers under the name), or a constant, tuple element, function parameter (what the file's calls pass) or return naming one; a file may belong to a module rooted in its directory or above (Zig rejects an import out of the module's directory) — at **strong** at most (a fetched package's module of the name may be the one used); `@import("x.zig")` is the module the table binds under that name, else the file — while every `build.zig` of the repository is closed |
| **Candidate** | every import — named or relative, and every member through one — while a `build.zig` of the repository is open: it gives the builder or a module to code Ark does not read (a helper file, a dependency's build code, a function pointer), uses one in a way Ark does not model, or holds syntax Ark does not read; what it runs may bind any name in any module (a file's references to its own declarations are untouched); a namespace a comptime condition chooses (`pub const Impl = if (builtin.os.tag == .windows) win.Impl else posix.Impl;`, `switch` the same way): a member through it is either alternative — also when only one alternative declares it (the other may be `std`'s, a file outside the repository, or hold it as a field); a member of containers that share an identity (unnamed payload structs of one union, same-named containers of two blocks of a function); a name declared twice in one scope; a module name the modules a file may belong to bind to different files; a binding to a module the build's syntax does not name, or of a name it does not state (computed at build time) — then every import of the module's files is a Candidate, `@import("x.zig")` included; a file a module's table also binds under its relative path's name (Zig takes the module); a file whose path differs from the import's in case alone (a case-insensitive file system opens it) |
| **Unresolved by design** | `std`, `builtin`, `root` and anything through them (the compiler provides them; root's calls stay attributable to every declaration of their name); a named module no `build.zig` of the repository binds for the importing file's module — a binding in build code that is no `build.zig` (a helper file the build imports, such as Ghostty's `src/build/`), a fetched package's, one a function of another file makes: the import counts toward every file's importers and its calls toward every same-named declaration, never ruled out; a ZON import (`@import("build.zig.zon")`); method calls on a value whose type needs inference (an `anytype` or `comptime T: type` parameter, a capture, a `comptime var` holding a type, an optional, slice or array, a call of a function stating no container result type); a member another file's type may hold as a field holding a function (the call is observed, unresolved); a name a `usingnamespace` of a nearer container may bring; `@field`, `@call` targets and other computed names; a file a relative import names that does not exist |
| **Not implemented** | comptime evaluation (which branch a target compiles, `inline` loops, type construction with `@Type` / `@Struct`), reading build code beyond `build.zig` files (helper files they import, a dependency's build through `build.zig.zon`) and beyond the module values above (options, struct fields of values the file does not construct, loops), field accesses as references, the types of a container's fields declared in another file (`x.field.method()` through a field of another file's struct), struct-field aliases (`.{ .x = ... }` initializer names) |

**Measured on real projects.** On TigerBeetle and Ghostty (pinned commits;
see [the evaluation record](development/zig.md)), every graph edge of a
random sample checked by hand pointed at the declaration the reference
names; no false `exact` or `strong` edge was found in the sample. This is a
sample, not a proof.

A file the parser rejects (inline assembly clobbers written as a struct
literal, `*if (c) A else B` as a type) is reported by `get_diagnostics`;
its rejected region is not analyzed.

Tests written inline (`test "..." { ... }`) are ordinary declarations of
their file; files named `*_test.zig`, `test.zig` or `tests.zig` and files
under a `test/` or `tests/` directory are test files.

Ark never runs `zig`, `zig build`, `build.zig` or any build step, and never
reads `build.zig.zon`. What a `build.zig` binds is read from its syntax
(`addImport`, `addAnonymousImport`, `addOptions`, `.imports`,
`import_table`); an operation on a module it does not read leaves the
module's names unknown — their imports Candidates, counted as unresolved
besides. Its bindings ground a Strong (and the absence of one a relative
Exact) only when the build is **closed**: Ark follows every use of a value
that reaches the build graph — the builder and what is derived from it —
through the file's own functions, the std.Build methods Zig 0.17.0's source
shows keep every module's root, table and registration, containers, and
std functions that only read. A build that gives such a value to anything
else (a helper file, a dependency's build code, an unknown method, a
function pointer, a pointer cast), assigns one where Ark does not follow
it, or holds syntax Ark does not read is **open**: every import of the
repository is then a Candidate, counted as unresolved besides. See
[the design record](development/zig.md), decision 8. A `zig-cache/` or `.zig-cache/` directory is skipped
only as `.gitignore` excludes it (a dot directory always is).

## Known gaps

The open conformance gaps of each provider are tracked in
[`internal/conformance/IMPROVEMENTS.md`](../internal/conformance/IMPROVEMENTS.md).
