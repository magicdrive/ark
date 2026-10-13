# Language support

Ark analyzes eight languages. They are not supported equally: each language
has a **support level**, the highest stage its tests certify, and every
language has constructs Ark deliberately leaves unresolved.

`get_language_support` reports the levels at run time; `ark symbol <file>`
shows what Ark extracts from one file.

## Support levels

Levels build on each other:

| Level | What the tests certify |
|---|---|
| `parse` | The Tree-sitter grammar parses the language |
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
| PHP | `.php` | `graph` | ✓ | ✓ | ✓ | ✓ | ✓ | |
| Terraform | `.tf`, `.tfvars` | `graph` | ✓ | ✓ | ✓ | ✓ | ✓ | |
| Rust | `.rs` | `graph` | ✓ | ✓ | ✓ | ✓ | ✓ | |
| JavaScript | `.js`, `.mjs`, `.cjs`, `.jsx` | `references` | ✓ | ✓ | ✓ | | | |
| Python | `.py`, `.pyw` | `references` | ✓ | ✓ | ✓ | | | |

An unchecked cell means *not certified*, not *disabled*. Graph tools still
answer for JavaScript and Python, and `get_context` still works for PHP,
Terraform and Rust, but those results are not covered by the tests the level names.
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
| **Certified** | relative-import resolution, aliases, default / namespace / type-only imports, barrels, members under proven receiver types, `this` / static members, `extends` / `implements`, JSX components |
| **Unresolved by design** | external packages (`react`, `node:fs`), path aliases (`@/foo`, tsconfig `paths`), receivers without a proven type (`repo.save()` with an unannotated `repo`), inherited members, declaration merging, computed access (`a[k]()`), `.js`-suffixed specifiers when both `.ts` and `.tsx` exist, anonymous default exports |
| **Not implemented** | return-type propagation and type inference, control-flow narrowing, overload resolution, `.d.ts` / `.mts` / `package.json` resolution, `tsconfig` interpretation, `namespace` bodies, enum members, destructured declarations, framework semantics (React, Next.js, NestJS, Angular), decorator and DI inference |

TypeScript module resolution is not compiler-equivalent: Ark ranks only the
repository-local, configuration-independent subset above. Same-named static
and instance members of one class share one symbol.

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
(Laravel, Symfony). Function and constant names are resolved conservatively.

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
| **Supported** | the blocks above; same-module cross-file resolution; local module outputs (`module.x["k"].out`, `module.x[*].out`); module input arguments; `depends_on`; provider configurations and aliases; check-scoped data sources; override files (their references count, they declare nothing); broken files, as far as Tree-sitter's error recovery allows, with an error diagnostic |
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

## JavaScript

Level `references`.

**Extracted:** top-level function declarations, class declarations, and
`const` / `let` declarations (including exported ones); calls, `new`
expressions and imports.

**Not extracted:** class methods and other class members, functions nested
in other functions, object-literal methods.

**Resolution (not certified):** JavaScript emits no import bindings; an import
is matched by path, and other names by the shared name rules within
JavaScript. A JavaScript file and a TypeScript file never resolve each other's
names, even through an `import` — this is a known gap, not a design choice.

## Python

Level `references`.

**Extracted:** top-level function and class definitions (including decorated
ones); calls and imports (`import`, `from … import`, aliases).

**Not extracted:** methods and other class members, nested functions.

**Resolution (not certified):** imports are matched by path; there are no
import bindings, so `from .util import helper` resolves `helper` by the name
rules (for example `strong` when it is the only `helper` in the directory).

## Known gaps

The open conformance gaps of each provider are tracked in
[`internal/conformance/IMPROVEMENTS.md`](../internal/conformance/IMPROVEMENTS.md).
