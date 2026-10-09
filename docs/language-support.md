# Language support

Ark analyzes seven languages. They are not supported equally: each language
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
| JavaScript | `.js`, `.mjs`, `.cjs`, `.jsx` | `references` | ✓ | ✓ | ✓ | | | |
| Python | `.py`, `.pyw` | `references` | ✓ | ✓ | ✓ | | | |

An unchecked cell means *not certified*, not *disabled*. Graph tools still
answer for JavaScript and Python, and `get_context` still works for PHP and
Terraform, but those results are not covered by the tests the level names.
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

**Extracted:** functions, methods (with receiver type), structs, interfaces,
named types, package-level constants and variables; calls, constructions
(`T{}`, `pkg.T{}`), type uses, imports (including aliases and dot imports).
Type aliases (`type A = B`) and function-local declarations are not symbols.

**Resolution:** Go package scoping
([details](resolution-model.md#go-package-scope)). Package-qualified
functions, types and constants resolve `exact` through the import path;
same-package names resolve `exact` (same file) or `strong`. A local
declaration that shadows a name is respected.

A method call resolves when the receiver's type is written down locally:

- the method receiver (`func (s *T) M()`);
- an explicitly typed parameter (`func f(r *Repo)`);
- a single declaration in the function body with a written type
  (`x := &T{}`, `x := T{}`, `var x T`);
- one field step on such a variable, when the struct is declared in the same
  file with an explicitly typed field (`s.repo.Save()`).

Only unqualified type names count.

**Stays `candidate` or unresolved (never a wrong edge):**

- method calls through a field of a struct declared in another file, or of a
  package-qualified type (`h.svc.Place()` where `svc *orders.Service`);
- method calls on a function's return value (`NewService().Place()`) or on
  `var s pkg.T`;
- interface dispatch and promoted (embedded) methods;
- methods of named non-struct types (`type ids map[string]int`);
- declarations duplicated across build-tagged files (build tags are not
  evaluated);
- a function passed as a value (`ids: symbol.NewDeclarationID`) is not a call.

Go module paths are inferred from import paths, not read from `go.mod`. With
only one internal import path, imports map into the repository only if the
repository directory's name equals the module path's last element.

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

Ark never runs `terraform`, downloads modules or providers, or reads
`.terraform/`.

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
