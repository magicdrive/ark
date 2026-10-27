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
6. **build.zig read, never run.** Only literal module declarations
   (`addModule`, `addImport` of a module a constant of the scope creates,
   `.imports`) map a named module, as an alias capped at Strong
   (`AliasDraft.ConfidenceCap`): a dependency's module of the name may be
   the one imported. `std`, `builtin`, `root` are never mapped; a path
   through them is observed by its last member only.

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
| Symbols (graph nodes) | 18,526 | 35,408 |
| References | 86,824 | 232,890 |
| — exact / strong | 60,326 / 4,656 | 138,367 / 2,991 |
| — candidate | 22 | 247 |
| — unresolved | 18,816 | 80,371 |
| Speculative, no declaration (dropped) | 3,004 | 10,914 |
| Imports resolved | 1,136 | 3,010 |
| Imports unresolved: named modules, ZON | 327 | 1,310 |
| Imports unresolved: missing file | 0 | 2 |
| Graph edges (exact / strong) | 40,753 (37,371 / 3,382) | 89,785 (88,675 / 1,110) |
| — calls / references / uses_type / imports | 14,375 / 15,282 / 9,961 / 1,135 | 30,552 / 37,048 / 19,180 / 3,005 |

The unresolved imports by name are `std`, `builtin` and named modules no
build.zig maps literally (TigerBeetle's `vsr` passes through a struct
field; Ghostty's GTK and other dependencies are fetched). The two missing
files are generated at build time or differ in case (`config.zig` for
`Config.zig`).

**Precision.** 230 graph edges drawn at random (seed 1; all edges, strong
edges only, and call edges only, of both repositories) were checked by
hand against the source: the reference's line and, where the target is
reached through a type, import or re-export, the declarations on the way
(for example `self.completed.push` through the field type
`QueueType(Completion)` of another file to `QueueType::()::push`, or
`stream.deinit()` through `Terminal.vtStream`'s result type `Stream`, a
re-export of `stream.Stream(Handler)`). No false `exact` or `strong` edge
was found. This is a sample of about 0.2 % of the edges, not a proof; no
independent oracle (a Zig compiler's semantic analysis) was run.

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

- Field types of another file's structs (`x.field.method()` across files):
  a field identity is `T.field`, which no member path computes.
- Named modules a build passes through struct fields or options, and
  dependencies' modules (`b.dependency(...)`, `build.zig.zon`).
- An independent oracle: comparing against the Zig compiler's own name
  resolution (e.g. `zig ast-check` output or a semantic dump) on a corpus,
  as Rust and Java have.
