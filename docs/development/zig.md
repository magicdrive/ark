# Zig support: design record and evaluation

This page records why Zig support is built the way it is, what was measured,
and how. The rules themselves are in `internal/languages/zig/provider.go`
(the header) and `ARCHITECTURE.md` §1; users' documentation is in
[language support](../language-support.md#zig).

## Version and parser

- **Language version.** Zig 0.15 to 0.17. Zig 0.17.0 (2026-10-01) is the
  current release; the name-resolution syntax Ark reads — `@import`,
  container declarations, `@This()`, `pub`, `test` — did not change between
  0.15 and 0.17 (the 0.16 and 0.17 language changes are to `switch`,
  packed types, `@Type`'s replacements, `@cImport`, `@bitCast` and
  vectors). `usingnamespace` was removed in 0.15: older code that uses it
  still parses, and a container with it is treated as one that may bring
  any name.
- **Parser.** [tree-sitter-grammars/tree-sitter-zig](https://github.com/tree-sitter-grammars/tree-sitter-zig)
  at `6479aa13f32f701c383083d8b28360ebd682fb7d` (2025-09-10, its latest
  commit; its corpus follows Zig 0.15.1), MIT, as bundled by gotreesitter
  v0.56.0 — the runtime every Ark provider already uses. Pure Go: no cgo,
  nothing to build per platform, no new dependency. maxxnino/tree-sitter-zig
  (the other candidate) is listed by gotreesitter's update log but not
  bundled, and is older.
- **Known grammar gaps** (reported as `parse_error`, region not analyzed):
  inline assembly clobbers written as a struct literal (0.15's
  `: .{ .rax = true }`), a type that is an `if` expression after a pointer
  prefix (`*if (c) A else B`). Two derivations are read by Zig's rules
  instead of the tree's: `?*const a.B` is derived `(?*const a).B` (the
  members continue the path, the prefixes make a type of it), and an empty
  container `struct {}` carries a field without text (no field). A name the
  parser inserts without text and without a MISSING mark (`const = ;`) is
  reported as a `parse_error`.

## Design decisions

1. **Identity from the path, like Rust.** A Zig file is a struct and
   `@import("x.zig")` is the file the path names from the importer's
   directory, so every identity is derived from the file's own path
   (`src/engine.zig::Engine::run`) and the provider reads no other file.
   Every file is `IdentityOnly`: no name match ever resolves a Zig
   reference.
2. **A constant naming a namespace is a declaration of its own**
   (`AliasDraft.Members`). `const Vec = @import("vec.zig").Vec;` is a
   declaration (find it, see who uses it) and an alias for the members
   below it; with a plain alias a reference to `Vec` would name both the
   constant and the struct (a Candidate, no edge).
3. **Comptime choices are alternatives.** `if` / `switch` between
   namespaces states one alias per branch; a member through it is a
   Candidate. Nothing is evaluated: no target, no build option is assumed.
4. **Generic type factories by their one return.** `fn List(comptime T:
   type) type { return struct {...}; }` declares the container `List::()`;
   `List(u32).append` is that declaration's `append` whatever the
   arguments. A factory with two returns is not followed.
5. **Receivers by declared types only.** A declared type, a construction,
   a function's written result type (`F::()`, an alias the declaring file
   states). A call through another file's type whose identity no
   declaration has stays Unresolved without claiming anything
   (`ReferenceDraft.TentativeIdentity`): it may be a field holding a
   function, and it must stay attributable to every method of its name, or
   `get_callers` would report a false "complete".
6. **Module environments: a name is what the importer's module binds.**
   Zig 0.17.0 resolves `@import("name")` — `@import("x.zig")` too — in the
   import table of the module the importing file is compiled in, before any
   file (checked with the compiler: a table entry `helper.zig` wins over
   the `helper.zig` beside the importer; two modules of one compilation
   bind `util` to two files and each sees its own), and a file can be in a
   module only if it lies in the module root's directory ("import of file
   outside module path"). So three identities are kept apart — a file
   (`src/vsr.zig`), a module (`{zigmod}src/vsr.zig`, every module a build
   creates with that root, their tables merged) and an importer's view
   (`{zigenv}src/cli.zig`, the tables of the modules rooted in its
   directories) — and the alias graph joins them: the file states its view
   → `{zigdir}<dir>` for its directories, a build.zig states `{zigdir}<dir>`
   → `{zigmod}<root>` and `{zigmod}<root>::name` → the bound module's root.
   A file several modules may hold sees all their bindings (a Candidate
   when they differ). A relative import's file states `{zigspec}<dir>::spec`
   for its importers' directories; a build states it for a binding named
   like a file. Unknowns stay unknown, as caps: a binding to a module the
   syntax does not name caps the name at Candidate; a binding of a name it
   does not state caps the module (every import of its files); a binding on
   a module the syntax does not name may be any module's (`{zigany}`).
   build.zig is read by a static evaluator (`buildEval`), never run: module
   values through constants, tuple elements, if-captures, the file's
   function parameters (every call of the file: its arguments), returns,
   artifacts' `.root_module`, dependencies' `.module("name")` (the module a
   build of the repository registers under the name, `{zigreg}name`),
   tables chosen by a comptime condition (all branches) or copied from
   another module's `import_table`. Its soundness does not rest on listing
   operations: every occurrence of a module or artifact it names is
   classified where it is written, and any it does not read leaves the
   module's names unknown; an assignment of a root, a root module or a
   table — whatever the receiver — or a dereferenced write of a module
   leaves every module's unknown. Four adversarial reviews with the
   compiler as oracle (`review3_test.go` to `review6_test.go`, 48 shapes)
   drove these rules; each found shapes the previous rules misread, so the
   evaluator's reading of build code is the part of Zig support most
   likely to hold another. A fifth (`review7_test.go`) found, beside build
   code, shapes of the shared resolution: a chain of aliases cut at
   `MaxAliasDepth` (now an open Candidate: what it names is unknown), a
   bound root the index does not hold (a vendored or skipped file: an open
   Candidate, through `AliasDraft.Fallback`), paths apart in Unicode
   normalization or full case folding (`source.FoldPath`), names with
   escapes and `@field` by a literal name in ordinary sources (decoded,
   observed).

   **Soundness first (the maintainers' decision, 2026-10-28).** Five
   reviews did not converge — build.zig is arbitrary Zig, and each round
   found another construct the evaluator misread — so what the evaluator
   reads now grounds a Strong or an Exact only for a build whose every way
   of reaching the graph Ark follows (decision 8); any other build binds
   anything. Results through a binding are capped at
   Strong: build code Ark does not read is assumed not to rebind a name,
   and a fetched package's module of the name may be the one used. `std`
   and `builtin` are never mapped; `root` is unresolved (its calls stay
   attributable).

   Alternatives weighed: keeping one repository-wide mapping per name (the
   first design) made a registration (`b.addModule("name")`, which names a
   module for dependents) a binding of every importer's — wrong where two
   modules bind the name apart (F7) or a module binds a name ending in
   `.zig` (F5); evaluating build.zig's control flow (a build interpreter)
   would reimplement the build runner; resolving through each file's
   relative-import reachability from module roots would need alias chains
   as long as import chains (bounded by `MaxAliasDepth`) — the directory
   rule over-approximates membership by the compiler's own constraint and
   costs one alias per directory.
7. **Unknown imports count toward every file.** An import of a named module
   no build.zig Ark reads binds (a helper file's binding, a fetched
   package's) may name any file: it is unresolved and
   `ReferenceDraft.MayBeAnyFile`, which completeness counts toward every
   file's declaration of the language — no file's importers read complete
   while such an import exists. Narrowing it to the files a build roots a
   module at would trust build code Ark does not read. A relative import
   whose path names no file but differs from one in case alone
   (`ReferenceDraft.PathCaseFold`) resolves to that file as a Candidate:
   macOS's default file system opens it, a case-sensitive one does not.

8. **Closed and open builds: Strong and Exact only on a proven graph.**
   A resolution is Exact or Strong only if every way it could resolve
   otherwise is excluded by what Ark proves; a way left unproven makes it
   a Candidate (or leaves it Unresolved). For an import that means:

   | | A relative import `@import("x.zig")` is Exact when | A named import `@import("name")` is Strong when |
   |---|---|---|
   | the file | it exists (or one apart in case only: a Candidate) | — |
   | the importer's modules | every module that may hold the importer is one a closed build makes (its root's directory holds the importer) | the same |
   | the table | no table of those binds `x.zig` | one module binds `name`, the same on every path |
   | other modules | no module rooted elsewhere can hold the importer | no other binding of the name in those tables |
   | unknown code | no build of the repository is open | the same |
   | the target | the path names one file | the bound root is one file the index holds |

   Strong, not Exact, for a binding: a fetched package's module of the name
   (a dependency's `b.dependency`) or a build run otherwise may stand for it.

   **Closed.** A build.zig is closed when Ark follows every way its code
   may reach the build graph. Zig 0.17.0's std keeps the graph in no global
   (neither `std.Build` nor the configurer has a container-level `var`):
   code reaches it only through a value it is given — the build function's
   `*std.Build` and what is derived from it (a module, an artifact, a step,
   a path, a dependency: each reaches the builder through `owner`). The
   world (`closed.go`) taints those values — a binding (a declaration, a
   parameter, a capture, resolved by Zig's scopes; a name Ark cannot
   resolve by the name) holding one, flow-insensitively, to a fixed point —
   and decides each use of one:

   | A graph value is | Closed when | Otherwise |
   |---|---|---|
   | given to a call | the callee is a function of the file (its parameters, by position, tainted and its body read), a method `buildEval` reads, a std.Build method Zig 0.17.0's source shows creates, roots, binds and registers no module (`neutralMethods`: their bodies call none of `Module.init`, `create`, `addImport`, `addAnonymousImport`, `addOptions`, a `createModule` or an `addModule`), a container's method (the container is tainted), a std function that only reads (`std.mem.eql`, ...) | open: another file's function (a build helper), a dependency's build code, an unknown method, any other std function, a function pointer |
   | assigned | to a name; to a field std states holds no module, root or table (`neutralFields`: settings such as `link_libc`) of a graph value | open: another field, a field of a value of the file's types, a dereference, an index, a container-level `var` |
   | given to a builtin | `@as`, `@call` (a call, above), one that returns no graph value (`@intFromPtr`, `@TypeOf`) | open: `@ptrCast`, `@fieldParentPtr`, `@field`, any other |
   | in module options | Zig 0.17.0's `Module.CreateOptions` and artifacts' options' fields | open: another field (0.11's `.dependencies`, `.source_file`) |

   Syntax outside the grammar's forms the world reads (`usingnamespace`,
   `asm`, `async`/`await`/`suspend`/`resume`, an error or a missing node),
   nesting past `maxDepth` and a taint that does not settle open the build
   too: what Ark does not read is never taken as harmless. std code given
   a graph value may call the methods of the file's types it is given by
   convention (`format`, `hash`, `lessThan`): their parameters are tainted
   then. Values holding no graph value (an option, a string, a target) are
   free; a field std states holds none (`allocator`, `build_root`, `io`) is
   one — unless a type of the file declares a field of the name.

   **Open.** An open build may create a module rooted at any file
   (`b.path("../x")`, a `cwd_relative` path, a dependency's path) and bind
   any name in any module: every module's names are unknown (`{zigany}`
   and `{zigspec}.` stand for what Ark does not see) — for every file of
   the repository, not the build's directory alone. Its modules' bindings
   Ark reads are still read (a Candidate among the unknown).

   **Relative imports have the importer's identity.** `@import("x.zig")`
   resolves through `{zigimp}<path>` — the alias `{zigimp}<path>` →
   `<path>` (the file) and → `{zigspec}<dir>::x.zig` for the importer's
   directories — and a member through it (`x.f`) through `{zigimp}<path>::f`.
   The file's own declarations and its references to them keep the file's
   identity: what a build may bind under `x.zig` concerns the importers,
   never the file's scopes (a file's call of its own function stays Exact
   in an open world).

   **Trust boundary.**

   | Case | Read as |
   |---|---|
   | one build.zig, its own functions | closed if every use above is |
   | a helper file (`@import("build/x.zig")`), its functions | open when given a graph value (not read) |
   | a dependency's build code (`@import("dep")`), its helpers | open when given a graph value |
   | a dependency the build fetches (`b.dependency`) | its modules are what its build registers: Strong at most; one no build of the repository registers is unknown |
   | build options, `builtin` | not evaluated: every branch is read (a Candidate when they bind apart; a branch never taken is read too) |
   | comptime choices | every branch, as options |
   | std.Build | Zig 0.17.0's source (`TestClosedTablesAgainstStd` checks the tables against a std tree); another version's API Ark does not list opens the build |
   | `build.zig.zon` | not read: its packages are dependencies (above) |
   | several build roots | each read alone; one open build opens every module |
   | a consumer outside the repository, a command line (`zig build-exe --dep`) | outside the boundary: the graph is the repository's builds' |
   | illegal behavior (Zig's undefined behavior) | outside the boundary |

   **Completeness.** An open build's unknowns are open (`AliasDraft.Open`):
   a resolution through one counts, besides its Candidates, as an
   unresolved reference — a relative import as an import of any file
   (`ReferenceDraft.MayBeAnyFile`). So while a build is open no file's
   importers read complete (`unattributed` > 0 wherever an import may
   name it), and a call through an import is attributable to every
   declaration of its name; a file's own references are not touched. With
   every build closed, a file's importer list is complete when every
   import that may name it resolved to an edge: `MayBeAnyFile` is kept
   (narrowing an unresolved named import to the roots of the closed
   builds' modules would miss a fetched package's module).

   Alternatives weighed: reading a build.zig when its every statement is
   of a form the evaluator lists (the option as first stated) — syntax
   coverage is not semantics: a statement of a listed form may still give
   the builder to code Ark does not read; bounding an open build to its
   directory — its paths may leave it; reading helper files (Ghostty's
   `src/build/`) — a later step (each helper read as the build is).

## Evaluation on real projects

Pinned commits, cloned shallow; nothing of them was built or run.

| Repository | Commit | Zig files | Zig source |
|---|---|---:|---:|
| [TigerBeetle](https://github.com/tigerbeetle/tigerbeetle) (Apache-2.0) | `0450c32b2c3c3ab6114f3402cc601341cc8192aa` (2026-10-14) | 248 | 6.4 MB |
| [Ghostty](https://github.com/ghostty-org/ghostty) (MIT, `minimum_zig_version = "0.17.0"`) | `83fb95624b0439ee660c2679b43b38c8b63769cd` (2026-10-27) | 814 | 13.7 MB |

Measured with the opt-in `TestCorpus` (`ARK_ZIG_CORPUS=<repo> go test
./internal/languages/zig -run TestCorpus -v`; `ARK_ZIG_SAMPLE=<n>` prints a
random edge sample, `ARK_ZIG_INCREMENTAL=1` the cache builds):

| | TigerBeetle | Ghostty |
|---|---:|---:|
| Files with a parse error | 0 | 6 (11 diagnostics) |
| Symbols (graph nodes) | 18,522 | 35,408 |
| References | 86,820 | 232,890 |
| — exact / strong | 60,272 / 5,894 | 137,902 / 26 |
| — candidate | 117 | 713 |
| — unresolved | 17,535 | 83,335 |
| Speculative, no declaration (dropped) | 3,002 | 10,914 |
| Imports resolved | 1,154 | 2,926 |
| Imports unresolved: named modules, ZON | 309 | 1,378 |
| Imports unresolved: missing file, or capped | 0 | 18 |
| Graph edges (exact / strong) | 41,670 (37,329 / 4,341) | 88,400 (88,375 / 25) |
| — calls / references / uses_type / imports | 14,604 / 15,831 / 10,082 / 1,153 | 29,980 / 36,475 / 19,024 / 2,921 |

(With the module environments of decision 6; the first measurements and
how they changed are below.) The unresolved imports by name are `std`,
`builtin`, `root` and named modules no build.zig of the repository binds
for the importing file's module (Ghostty's are bound in `src/build/*.zig`,
or are fetched packages: GTK and others). The two missing files are
generated at build time or differ in case (`config.zig` for `Config.zig`:
a Candidate now); the other 16 are `pkg/wuffs/src`'s relative imports, which
its build makes Candidates: it binds test images under names a comptime
loop computes (`std.fmt.comptimePrint`), names Ark does not read.

**Precision.** 230 graph edges drawn at random (seed 1; all edges, strong
edges only, and call edges only, of both repositories) were checked by
hand against the source: the reference's line and, where the target is
reached through a type, import or re-export, the declarations on the way
(for example `self.completed.push` through the field type
`QueueType(Completion)` of another file to `QueueType::()::push`, or
`stream.deinit()` through `Terminal.vtStream`'s result type `Stream`, a
re-export of `stream.Stream(Handler)`). No false `exact` or `strong` edge
was found in it. This is a sample of about 0.2 % of the edges, not a proof;
no independent oracle (a Zig compiler's semantic analysis) was run.

**Adversarial review.** An independent review searching for valid Zig that
Ark resolves wrongly found four shapes the sample had not drawn, each
reproduced and fixed with a test (`TestReview_NoExactPastUnprovenAlternatives`):
a comptime choice whose other alternative lies outside the repository
(`if (builtin.single_threaded) DummyMutex else std.Thread.Mutex`) or holds
the member as a field — a member found in one alternative alone is now a
Candidate (the alternatives' aliases are capped); containers sharing an
identity (unnamed payload structs of a union, same-named containers of two
blocks) — a member through one is now a Candidate; a `comptime var`
holding a type, which may be reassigned — no longer a namespace; quoted
names containing an identity separator — no name. On the two
repositories this turned 69 and 172 edges into Candidates. Twenty
soundness mutations, these four included, are each killed by the tests.

**Second audit.** A pre-merge audit repeated the measurement (the
counts above, the same commits) and checked a new sample: 144 edges drawn
by strata (seed `20261028`; exact calls, references, uses_type and
imports, strong, cross-file, through an alias, with a receiver; 72 of
each repository), each traced by hand through the declarations to the
target: no false `exact` or `strong` edge. Every strong edge rests on a
build.zig module mapping; the thirteen mappings behind them (TigerBeetle's
`stdx`, twelve Ghostty `pkg/` modules) were each checked against the
build.zig and build.zig.zon that declare them. A new adversarial review,
checking each shape with Zig 0.17.0 (a target's body made a
`@compileError`: the build fails exactly when the reference reaches it),
found what `TestCompleteness_MembersPastAnotherFilesMemberStayAttributable`
and `TestReview2_CompletenessAndNoFalseExact` now pin:

- calls the index ruled out as any repository declaration's caller —
  `get_callers` read complete while they exist: through a value of
  another file (`a.global.run()`, a field of it), a namespace constant of
  another file Ark cannot denote, a named module no build.zig maps
  literally (TigerBeetle's `vsr`: `src/vsr.zig::fatal` showed 8 callers
  and nothing unattributed; 67 calls are now unattributed), `root`, a
  re-export chain past the alias depth. A member past another file's
  member, or of a named module other than std and builtin, now names its
  target only if declared (`ReferenceDraft.TentativeIdentity`): resolved
  as before when declared (the graphs of both repositories are unchanged),
  attributable to every declaration of its name when not;
- an exact edge to one of two unnamed alternatives sharing an identity
  (`if (c) struct { run: *const fn () void } else struct { pub fn run }`):
  an alias to a shared identity is capped at Candidate (one Ghostty edge,
  inside such a struct, became one too);
- `@"u0"`, `@"f80"`, `@"c_char"` declarations taken for the unquoted
  primitive.

**Third round (module environments).** The second audit's remaining
blockers — F5, F7 and files' importers that read complete — share a root
cause: a build's registration of a module was read as every file's
binding, and a module's identity was a file's. With decisions 6 and 7 the
same measurement gives (TigerBeetle / Ghostty): graph edges 41,670 /
88,400, exact 37,329 / 88,375 (TigerBeetle's unchanged; Ghostty's
`pkg/wuffs/src`, 127 edges, Candidates: see above), strong 4,341 / 25
(3,355 / 1,110 before). TigerBeetle gains the `vsr` module, whose table the
evaluator now reads through `build_vsr_module`'s parameters and tuple
(+986 strong edges, none lost). Ghostty loses the strong edges from `src/`
into its `pkg/` modules (1,085, every one through the old repository-wide
mapping): `src/`'s modules and their tables are made in
`src/build/*.zig`, build code that is no build.zig — those imports are
unresolved now, attributable to every same-named declaration and every
file's importers. A new stratified sample of 149 edges (seed `20261029`,
the strong edges of TigerBeetle's new `vsr` stratum and all 25 of
Ghostty's included) was checked by hand: see the audit record.

**False negatives seen.** Method calls on values whose type needs
inference (a capture's, an `anytype` parameter's, a call result of a
function stating no container type), members through a field of another
file's struct, everything through `std`, and named modules a build passes
through struct fields or dependencies.

**Performance.** Same machine as [performance](../performance.md) (Intel
Xeon W-2140B, macOS 15.7.9, Go 1.27.2). In-process, Zig files only: index
build 3.1 s (TigerBeetle) and 8.4 s (Ghostty); with the extraction cache,
cold 1.9 s / 6.1 s, every file cached 0.75 s / 2.1 s, after editing one
file 0.75 s / 2.0 s, the graph identical in all three. The MCP server on
the whole repository (every language, `docs/scripts/ark-mcp-timing.py`,
three runs) is in [performance](../performance.md#zig).

## Remaining work

- Build code that is no `build.zig`: the helper files a build imports
  (Ghostty's `src/build/`) bind most of a large project's modules. Reading
  them needs the build root `b.path` is relative to, which a helper file
  does not state.
- A dependency's modules by its `build.zig.zon` entry (a local `.path`
  package's registration rather than any build's of the name).
- Field types of another file's structs (`x.field.method()` across files):
  a field identity is `T.field`, which no member path computes.
- An independent oracle: comparing against the Zig compiler's own name
  resolution (e.g. `zig ast-check` output or a semantic dump) on a corpus,
  as Rust and Java have.
