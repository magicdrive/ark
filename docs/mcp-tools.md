# MCP tool reference

Ark's MCP server (`ark mcp-server`) exposes 21 tools and two resource
schemes. This page documents every tool's purpose, parameters, output and
caveats. The parameter tables are generated from the server's `tools/list`
response; a test (`TestDocs_MCPToolReferenceMatchesServer`) fails when they
drift.

- [Conventions](#conventions)
- [Tools by task](#tools-by-task)
- [Discovery](#discovery): `get_repository_map`, `search_context`, `find_symbol`, `search_code`, `get_symbols`, `get_symbol`
- [Relationships](#relationships): `get_callers`, `get_callees`, `get_relations`, `find_references`, `analyze_change_impact`
- [Context](#context): `get_context`
- [Files](#files): `get_file_content`, `get_files_arklite`, `list_files`, `search_in_files`, `get_directory_tree`, `get_file_info`, `get_project_stats`
- [Analysis status](#analysis-status): `get_diagnostics`, `get_language_support`
- [Resources](#resources)

The examples are real responses from a five-file Go repository
(`example.com/shop`: `api` → `orders` → `store`, plus `audit`), shortened
where marked `…`.

## Conventions

**Paths.** A `path` argument is relative to the server root or absolute
inside it. A path outside the root is refused, and so is one whose
symlink-resolved form leaves it unless the server runs with
`--allow-external-symlinks on` ([Symlink policy](../SECURITY.md#symlink-policy)):

```text
path "../outside.txt" is outside the server root "/work/shop"; use a path inside the repository
```

**Index.** Graph tools (`get_callers`, `get_callees`, `get_relations`,
`get_context`, `search_context`, `analyze_change_impact`,
`get_repository_map`, `get_diagnostics`) work on an index of the `path`
directory: the first call builds it, later calls reuse it after checking that
no source file changed ([Operations](operations.md#index-lifecycle)). A
subdirectory `path` indexes only that subtree.

**Target symbols.** Target tools take `symbol` as a name or qualified name
(`Place`, `Handler.Checkout`; a namespace prefix is optional). When several
declarations match, the call fails with the list of declarations and their
`symbolId`; pass `symbolId` or `filePattern` to select one. Ark never picks
one for you. A name that is some declaration's exact qualified name selects
that declaration (`index.html` is the document at the root, not
`sub/index.html`, whose qualified name is its path); with `filePattern`, the
lookup keeps to the matching files.

**Confidence and completeness.** Edges carry `confidence` and `evidence`;
`candidates` are possibilities, not edges; `unattributed`, `unresolved` and
`outsideRepository` count what did not become an edge. See
[Resolution model](resolution-model.md#reading-a-graph-answer).
`indexDiagnostics` appears only when some files could not be fully analyzed.

**Errors.** A tool that cannot answer returns an MCP result with
`isError: true` and a message. An error is never a diagnostic, and an empty
result is never an error.

**Tokens.** `maxTokens` budgets use Ark's estimate, `len(text)/4`, not a
model's tokenizer.

**Secret masking.** By default every tool result, resource and error
leaving the server has the secrets Ark's masking rules detect replaced by
`*****MASKED*****` — source snippets, search lines, receiver expressions,
messages. File paths, symbol names, IDs and Ark's own enumerations are never
altered, so they can be passed back in later calls; text results of
`list_files`, `get_repository_map` and `search_code` consist of such
identifiers and are not masked. `ark mcp-server --mask-secrets off` turns
masking off for the server and logs a warning; a request cannot change it
(the `maskSecrets` argument of `get_file_content` and `get_files_arklite` is
accepted and has no effect). A value that only names another value is kept
as written: a GitHub Actions context reference (`${{ secrets.NAME }}`), and in
the `get_file_content` or `get_symbol` result of a `.tf` file a Terraform
reference (`var.db_password`, `random_password.x.result`); an expression with
anything else in it is masked whole, as is anything written after a kept
reference on its line, and every line of a heredoc or YAML block value
(`<<EOT`, `|`). A value is masked whole however it is written: all its words
(`password: correct horse battery`), under a quoted key (`"api_key": "…"`,
`'password' => '…'`), past a quote that does not end it. Masking is
pattern-based and does not
find every secret. See [SECURITY.md](../SECURITY.md#secret-masking).

The rules are Ark's recommended default, adjustable per repository with
`.ark-secrets-rules.toml` at its root ([Secret masking
rules](secret-rules.md)): added rules apply to every tool and resource; the
entries that disable built-in rules apply only when the server is started
with `--secret-rule-overrides on`, and otherwise are left out and logged
once. The file is read for every request and compiled when it changes, so
an edit applies to the next call. If it cannot be compiled, every result
with repository text in it is replaced by an error naming the file and the
problem (identifier-only results still go out) until it is fixed.

**Files excluded by `.arkignore`.** No tool or resource returns anything from
a file the repository's `.arkignore` files exclude: a path naming one (or a
symlink leading to one) is reported as not existing, and listings, searches,
the index, the graph, context, the repository map and diagnostics leave it
out. This holds with masking off. See
[SECURITY.md](../SECURITY.md#file-access-policy-arkignore).

## Tools by task

| Task | Tool |
|---|---|
| Orient in an unfamiliar repository | `get_repository_map` |
| Find a symbol from part of its name | `search_context` |
| Find declarations by regular expression | `find_symbol` |
| Find symbols by kind, calls or type usage | `search_code` |
| List a file's declarations / read one declaration | `get_symbols`, `get_symbol` |
| Who calls this? What does it call? | `get_callers`, `get_callees`, `get_relations` |
| Every syntactic use of a name | `find_references` |
| What might break if this changes? | `analyze_change_impact` |
| The code needed to understand or modify a symbol | `get_context` |
| Read or search files | `get_file_content`, `get_files_arklite`, `search_in_files`, `list_files` |
| What could Ark not analyze? | `get_diagnostics`, `get_language_support` |

## Discovery

### `get_repository_map`

A ranked overview of the packages (directories) of the repository, their
key declarations and the dependencies between them. Start here in an
unfamiliar repository.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | Repository root path |
| `detail` | string (`minimal`, `normal`, `verbose`) |  | `normal` | Level of detail: minimal (packages only), normal (top symbols), verbose (all exported symbols) |
| `format` | string (`text`, `json`) |  | `text` | Output format |
| `maxPackages` | integer |  | `50` | Maximum number of packages to include |
| `maxSymbols` | integer |  | `10` | Maximum symbols per package |

Example:

```text
shop (go, 5 files, 11 symbols)

store
  Order
  Save

orders
  Place
…
Dependencies:
  api → audit
  api → orders
  orders → store
```

Packages and their symbols are ordered by a deterministic score that favors
entry points, exported declarations and declarations other code depends on.
The text names every package of the map — one that lists nothing too — so
both ends of every dependency line are in it. `format: "json"`
adds each symbol's edge counts and lists the packages left out by
`maxPackages`.

A Terraform module (a directory) lists what it builds rather than its
interface: managed and ephemeral resources, module calls and data sources
first — one resource of each type before a second of any — then outputs,
providers and input variables; locals (and `terraform` blocks) only at
`detail: "verbose"`. A module call whose `source` is a local directory in the
repository is a dependency on that module; a registry, Git or other remote
source, a directory outside the repository, a `source` that is no literal and
a call an override file may replace are none.

```text
infra (terraform, 12 files, 654 symbols)

modules/app
  aws_lb_target_group.web
  aws_lb.web
  aws_ecs_service.web
  …
envs/prod
  module.network
  module.app
  …
Dependencies:
  envs/prod → modules/app
  envs/prod → modules/network
```

To count resources by type or module, use [`search_code`](#search_code) with
`groupBy`.

### `search_context`

Finds symbols from a partial or approximate identifier (`place`, `getUser`,
`user_profile`, `Service.create`) and returns context for the top candidates,
all within one token budget. Use it when you do not know the exact name.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `query` | string | yes |  | Identifier, qualified name or part of one (plain text, not a regex; at most 256 bytes) |
| `contextLimit` | integer |  | `1` | How many candidates, from rank 1 down, get context (0-20, at most limit). 0 = metadata only. Rank is not correctness: the others are still returned. Range 0–20. |
| `includeContext` | boolean |  | `true` | Attach context for the top candidates within the budget. false returns candidate metadata only (faster) |
| `limit` | integer |  | `5` | Maximum number of candidates to return (1-20). Range 1–20. |
| `maxTokens` | integer |  | `4000` | Estimated token budget for the whole response (len(text)/4 approximation, as get_context). Range 1–100000. |
| `path` | string |  |  | Only search symbols in this file or directory (relative to the server root, or absolute inside it). Default: the whole server root |

Example:

```json
{"query":"place","totalMatches":2,"returnedMatches":2,"truncated":false,"contextLimit":0,
 "results":[
  {"rank":1,"matchType":"exact_case_insensitive","symbol":{"id":"c58fa84d28632b1a","name":"Place",
   "qualifiedName":"Place","kind":"function","language":"go","path":"orders/orders.go","startLine":10,"endLine":15},
   "context":{"status":"not_requested"}},
  {"rank":2,"matchType":"word_boundary","symbol":{"id":"f42af8d9d71ea6dc","name":"TestPlace", …},
   "context":{"status":"not_requested"}}]}
```

The rank is name similarity (`matchType`: `exact`, `exact_case_insensitive`,
`prefix`, `word_boundary`, `qualified`, `substring`), not evidence of what
code refers to; same-named candidates stay separate. `maxTokens` bounds the
whole response: context that does not fit is marked `omitted_budget`. To get
another candidate's context, call `get_context` with its `qualifiedName`,
`filePattern` set to its `path` and `symbolId` set to its `id`.

### `find_symbol`

Searches declarations by name pattern (a regular expression) across the
files of a directory, parsing them directly (no index).

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `pattern` | string | yes |  | Symbol name pattern to search for (supports regex) |
| `includeExt` | string |  |  | Include only these extensions (comma-separated, e.g., 'go,ts,py') |
| `kind` | string |  |  | Filter by symbol kind (function, method, class, interface, type, struct, constant, variable) |
| `maxResults` | integer |  | `50` | Maximum number of results. When more symbols match, the first maxResults (in path order) are returned with truncated: true |
| `path` | string |  |  | Directory path to search in (default: current directory) |

Example:

```json
{"query":"Save","matches":[{"path":"store/store.go","symbol":{"name":"Save","kind":"function",
 "startLine":12,"endLine":15, …,"exported":true}}],
 "stats":{"filesScanned":5,"filesSkipped":1,"parseErrors":0},"truncated":false}
```

Results are in path order; when more than `maxResults` match, `truncated` is
`true`.

### `search_code`

Finds symbols by structural predicates, combined with AND: kind, name
substring, exported status, a call to a name, use of a type, file pattern,
language. It lists at most `maxResults` matches; the first line always states
the total. With `groupBy` it counts the matches instead. With `format: json`
each match carries the symbol's fields; a JavaScript or TypeScript class
member also its `Placement` (`static` or `instance`), a field other
languages omit.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | Repository root or directory path |
| `callsName` | string |  |  | Only symbols that contain a call to a function/method whose name contains this string |
| `excludeGenerated` | boolean |  | `false` | Exclude generated/vendor files |
| `excludeTest` | boolean |  | `true` | Exclude test files and testdata directories (default true) |
| `exported` | boolean |  |  | Filter by exported/public status |
| `filePattern` | string |  |  | File path substring filter (case-insensitive) |
| `format` | string (`text`, `json`) |  | `text` |  |
| `groupBy` | string |  |  | Count the matches instead of listing them, by one or two comma-separated keys: `kind`, `language`, `directory`, `file`, `resourceType` |
| `kind` | string |  |  | Symbol kind filter: function, method, class, struct, interface, type, constant, variable, …; Terraform: `resource` (managed and ephemeral), `data_source`, `module` (a module call), `output`, `variable` (`var.*` and `local.*`), `configuration` (provider, terraform, check); Rust: `impl` (an impl block), `macro` (a `macro_rules!` macro); HTML: `document` (a file), `element` (an element with an id); CSS: `stylesheet`, `keyframes`, `font_face`, `property_registration`, `custom_property`, `css_class` (a CSS Modules class); JavaScript: `script` (a file); `asset` (an image, a font or a media file web documents link to) |
| `language` | string |  |  | Language filter: go, typescript, tsx, javascript, python, php, terraform, rust, java, html, css |
| `maxResults` | integer |  | `50` | Maximum number of matches listed (the total is always reported); with `groupBy`, of group lines (at most 1000) |
| `namePattern` | string |  |  | Name substring filter (case-insensitive) |
| `usesType` | string |  |  | Only symbols that reference a type whose name contains this string |

Example:

```text
Found 1 symbol(s) matching kind=function, calls~"Save"

orders/orders.go:10  function  Place [exported]
```

`callsName` and `usesType` match references by name substring; they do not
require a resolved edge. Test files are excluded unless `excludeTest: false`.

`groupBy` counts every match of the query from the index (nothing is
re-parsed), largest group first, and lists at most `maxResults` group lines
(subgroups included), stating how many it leaves out. `resourceType` is a
Terraform resource's type as declared, with data sources as `data.TYPE` and
ephemeral resources as `ephemeral.TYPE`; other symbols count as `(none)`.
`directory` is the file's directory — for Terraform, the module. A
`namePattern` such as `aws_rds` selects types by name, which is no
classification by cloud service.

```text
577 symbols matching kind=resource, counted by resourceType (50 of 60 groups shown; raise maxResults or narrow the query)

    18  aws_rds_cluster
    17  aws_acm_certificate
    …
```

`kind: "resource", namePattern: "aws_ecs", groupBy: "directory"` answers where
the ECS resources are; `groupBy: "resourceType,directory"` nests the modules
under each type. In JSON: `{"totalCount", "groupBy", "groups": [{"name",
"count", "groups"}], "totalGroups", "omittedGroups"}`.

### `get_symbols`

Lists the declarations of one file, parsed with Tree-sitter (an HTML file
with golang.org/x/net/html: its document, the elements with an id and the
declarations of its inline styles and scripts; a CSS
file as CSS Syntax Level 3: its stylesheet, @keyframes, @font-face,
@property, custom properties and CSS Modules classes). For a large
file, list its declarations here and read the ones you need with
[`get_symbol`](#get_symbol) rather than the whole file. Code in comments
declares nothing: a commented-out declaration — a commented-out Terraform
resource, say — is no symbol (an empty file lists `"symbols": null`); find
it with [`search_in_files`](#search_in_files), e.g. the regex
`^\s*(#|//)\s*resource "` (a `/* … */` block shows as plain lines).

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | File path to analyze |
| `lang` | string |  |  | Language override (go, typescript, tsx, javascript, python, php, terraform, rust, java, html, css). Auto-detected if not specified. |

Example:

```json
{"path":"api/handler.go","language":"go","symbols":[
 {"name":"Handler","kind":"struct","startLine":9,"endLine":9, …,"exported":true},
 {"name":"Checkout","kind":"method","startLine":12,"endLine":15, …,"receiver":"Handler","exported":true},
 {"name":"Retry","kind":"method","startLine":18,"endLine":20, …,"receiver":"Handler","exported":true}]}
```

### `get_symbol`

Returns one declaration of a file, with its source.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `name` | string | yes |  | Symbol name to find |
| `path` | string | yes |  | File path containing the symbol |
| `includeSource` | boolean |  | `true` | Include the source code of the symbol |

Example:

```json
{"name":"Save","kind":"function","language":"go","path":"store/store.go","startLine":12,"endLine":15,
 "exported":true,"source":"func Save(o Order) error {\n\torders[o.ID] = o\n\treturn nil\n}", …}
```

## Relationships

### `get_callers`

Lists the symbols that call (construct, use) the target, from the graph.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | Directory to index (repository root or a subdirectory) |
| `symbol` | string | yes |  | Symbol name or qualified name (e.g. greet, UserService.Create, LoginScreenPolicy::showsSsoButton; a namespace prefix is optional) |
| `filePattern` | string |  |  | File path substring to disambiguate when multiple packages define the same symbol (e.g. internal/resolver) |
| `maxDepth` | integer |  | `1` | Maximum transitive depth (1 = direct callers only, 0 = unlimited) |
| `maxResults` | integer |  | `50` | Maximum number of edges to return |
| `symbolId` | string |  |  | Optional SymbolID selecting one declaration when several share the name (from search_context or an ambiguity listing). It must belong to `symbol`, and IDs depend on the indexed path: search_context's IDs are for path "." |

Example:

```json
{"symbol":"Place","edges":[
  {"from":"Place","to":"Handler.Checkout","kind":"called_by","confidence":"exact",
   "evidence":"orders.Place declared in imported package example.com/shop/orders (orders)"},
  {"from":"Place","to":"Handler.Retry","kind":"called_by","confidence":"exact", …},
  {"from":"Place","to":"TestPlace","kind":"called_by","confidence":"strong",
   "evidence":"symbol \"Place\" found in same package as orders/orders_test.go"}],
 "unattributed":0}
```

In a reverse edge, `to` is the caller. `unattributed: 0` means no observed
reference may be a missing caller; otherwise `candidates` samples the possible
callers (at most 10; `candidatesTotal` and `candidateRelationsTotal` count
all). Each reverse edge reports what the reference is: `called_by` for a
call or a construction, and otherwise `referenced_by` (a value; Terraform
dependents; an HTML element's id), `depended_on_by` (Terraform `depends_on`),
`linked_from` (an HTML document or a stylesheet linking it), `implemented_by`,
`extended_by`, `trait_used_by`, `type_used_by` or `imported_by` — an
implementation, a type use or an import is never shown as a call.
`maxDepth` above 1 follows callers transitively.

### `get_callees`

Lists what the target calls, constructs or uses, and accounts for every
observed reference it contains. For an HTML document the callees are what it
refers to: `references` (an element by its id; a declaration of its inline
styles), `links` (another document, a stylesheet, a script) and `calls` (a
function or class of its inline scripts, from their top level); for a CSS
stylesheet `references` (an animation name, a font
family, a composed class) and `links` (an `@import`).

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | Directory to index (repository root or a subdirectory) |
| `symbol` | string | yes |  | Symbol name or qualified name to look up |
| `filePattern` | string |  |  | File path substring to disambiguate when multiple packages define the same symbol (e.g. internal/resolver) |
| `maxDepth` | integer |  | `1` | Maximum transitive depth (1 = direct callees only, 0 = unlimited) |
| `maxResults` | integer |  | `50` | Maximum number of edges to return |
| `symbolId` | string |  |  | Optional SymbolID selecting one declaration when several share the name (from search_context or an ambiguity listing). It must belong to `symbol`, and IDs depend on the indexed path: search_context's IDs are for path "." |

Example:

```json
{"symbol":"Checkout","edges":[
  {"from":"Handler.Checkout","to":"Place","kind":"calls","confidence":"exact", …}],
 "unattributed":1,
 "candidates":[{"symbol":"Log.Record","file":"audit/audit.go","kind":"call","confidence":"candidate",
   "evidence":"only symbol named \"Record\" in repository","references":1}],
 "candidatesTotal":1,"candidateRelationsTotal":1,"unresolved":0,"outsideRepository":0}
```

Each observed reference is exactly one of: an edge, unattributed, unresolved
(no repository symbol can be its target) or `outsideRepository`.
`unresolvedReferences` lists the references with no candidate (first 10, with
a `reason`).

### `get_relations`

Both directions at once — callers and callees, constructions, type uses,
reads and writes — with reference kind, confidence and evidence.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | Directory to index (repository root or a subdirectory) |
| `symbol` | string | yes |  | Symbol name or qualified name (e.g. UserService.Create; a namespace prefix is optional) |
| `filePattern` | string |  |  | Narrow an ambiguous symbol name by file-path substring |
| `maxResults` | integer |  | `50` | Maximum number of relations to return |
| `symbolId` | string |  |  | Optional SymbolID selecting one declaration when several share the name (from search_context or an ambiguity listing). It must belong to `symbol`, and IDs depend on the indexed path: search_context's IDs are for path "." |

Example:

```json
{"symbol":"Place","relations":[
  {"direction":"called_by","qualified":"Handler.Checkout","file":"api/handler.go","kind":"call","confidence":"exact", …},
  {"direction":"calls","qualified":"Order","file":"store/store.go","kind":"construction","confidence":"exact", …},
  {"direction":"calls","qualified":"validate","file":"orders/orders.go","kind":"call","confidence":"exact", …}],
 "unattributed":0,"unresolved":0,"outsideRepository":0}
```

Candidate relations are sampled (at most 10 per direction) with totals.

### `find_references`

Lists every syntactic reference with a given name — calls, type uses,
imports, constructions, reads, writes — whether or not it resolves.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `name` | string | yes |  | Symbol name to search for (exact match) |
| `path` | string | yes |  | File or directory path to search in |
| `kind` | string |  |  | Filter by reference kind: call, type_use, import, construction, read, write, value_reference, depends_on, id_reference, resource_link, unknown |
| `maxResults` | integer |  | `100` | Maximum number of results |

Example:

```json
{"path":".","name":"Place","results":[
  {"id":"9c1e40a7f3b26d58","name":"Place","kind":"call","language":"go","file":"api/handler.go","startLine":14,"startColumn":9,
   "container":"Handler.Checkout","receiverExpr":"orders","isCall":true}, …],
 "count":3}
```

This is a name match, not resolution: references to a different declaration
with the same name are included. Use `get_callers` for resolved callers.

A selector that may be a field read rather than a method (Go `x.f` not
called) is not among these results: it is a reference only once the index
resolves it to a declaration, and `find_references` lists what extraction
states, without resolving.

Each result has an `id` (16 hex digits) telling the listed references
apart: derived from the reference's language, file (its path relative to
the server root, in the OS's path form), kind, name and span, it is the same
for the same file, OS path form and Ark version, and may change when the
file or Ark's extraction does. It is not a `symbolId` — no tool takes it as
input — and not meant to be stored across Ark versions.

### `analyze_change_impact`

Estimates what a change to the target may affect: direct dependents and
dependencies, tests, and transitive dependents, each with its confidence.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | Repository root or directory path |
| `symbol` | string | yes |  | Symbol name or qualified name to analyze (e.g. 'Save' or 'Repository.Save') |
| `filePattern` | string |  |  | Narrow an ambiguous symbol name by file-path substring |
| `format` | string (`text`, `json`) |  | `text` |  |
| `maxDepth` | integer |  | `3` | Maximum transitive depth to follow |
| `symbolId` | string |  |  | Optional SymbolID selecting one declaration when several share the name (from search_context or an ambiguity listing). It must belong to `symbol`, and IDs depend on the indexed path: search_context's IDs are for path "." |

Example:

```text
Impact analysis: Save

Target:
  Save  store/store.go:12

Direct dependents (callers):
  Place                                    orders/orders.go:10  [exact]
…
Tests:
  TestPlace                                orders/orders_test.go:5  [strong]

Transitive dependents:
  Handler.Retry                            api/handler.go:18  [exact]
  Handler.Checkout                         api/handler.go:12  [exact]

Possible dependents (low confidence):
  (none)

Unattributed references: 0
```

Impact follows graph edges only. A transitive entry carries the weakest
confidence on its path. Candidates appear separately as possible dependents
(`possible_dependent` in JSON) and are not guaranteed impacts.

## Context

### `get_context`

Returns the source an agent needs to understand or modify a symbol: the
target, then its callees and callers ranked by relevance, within a token
budget.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | Repository root or directory path to index |
| `symbol` | string | yes |  | Symbol name or qualified name to get context for (e.g. 'Create' or 'UserService.Create') |
| `filePattern` | string |  |  | Narrow an ambiguous symbol name by file-path substring |
| `format` | string (`text`, `json`) |  | `text` | Output format |
| `includeTests` | boolean |  | `false` | Include test symbols in context |
| `maxDepth` | integer |  | `2` | Graph traversal depth for collecting related symbols |
| `maxTokens` | integer |  | `8000` | Estimated token budget (len(text)/4 approximation); target is always included |
| `symbolId` | string |  |  | Optional SymbolID selecting one declaration when several share the name (from search_context or an ambiguity listing). It must belong to `symbol`, and IDs depend on the indexed path: search_context's IDs are for path "." |

Example:

```text
### orders/orders.go:10-15
Symbol: Place
Reason: target
Confidence: exact

func Place(id string, total int) error { … }

### store/store.go:12-15
Symbol: Save
Reason: direct callee
Confidence: exact
…
--- stats: 6/6 items, ~143 tokens (budget 600), unattributed: 0 callers, 0 callees; unresolved callees: 0, outside repository: 0 ---
```

Items come only from graph edges; candidates appear only in the
`unattributed` counts. The target is always included, even beyond
`maxTokens` (`TargetTruncated`). Test symbols are left out unless
`includeTests`. `format: "json"` adds each item's score breakdown.

## Files

### `get_file_content`

Reads one file, whole. For a large file, list its declarations with
[`get_symbols`](#get_symbols) and read the ones you need with
[`get_symbol`](#get_symbol) first; read the whole file only when you need
it.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | File path to read |
| `deleteComments` | boolean |  | `false` | Remove code comments |
| `maskSecrets` | boolean |  | `true` | Accepted for compatibility. Masking follows the server's --mask-secrets setting; a call cannot turn it off |
| `withLineNumbers` | boolean |  | `true` | Include line numbers |

Detected secrets are masked unless the server runs with `--mask-secrets off` ([Conventions](#conventions)). `withLineNumbers` currently has no effect: the content is returned without line numbers.

### `get_files_arklite`

Reads several files in the compact arklite format (one line per file).

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `paths` | array of string | yes |  | Array of file paths to include |
| `deleteComments` | boolean |  | `false` | Remove code comments |
| `maskSecrets` | boolean |  | `true` | Accepted for compatibility. Masking follows the server's --mask-secrets setting; a call cannot turn it off |
| `maxFiles` | integer |  | `10` | Maximum number of files to process |

The header and file entries show absolute paths.

### `list_files`

Lists files under a directory with the same filters as the repository
dump.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | Directory path to scan |
| `allowGitignore` | boolean |  | `true` | Respect .gitignore rules |
| `excludeDir` | string |  |  | Exclude these directories (comma-separated) |
| `excludeDirRegex` | string |  |  | Exclude directories matching this regex pattern |
| `excludeExt` | string |  |  | Exclude these extensions (comma-separated) |
| `excludeFileRegex` | string |  |  | Exclude files matching this regex pattern |
| `ignoreDotfiles` | boolean |  | `false` | Ignore dotfiles |
| `includeExt` | string |  |  | Include only these extensions (comma-separated) |
| `patternRegex` | string |  |  | Include files matching this regex pattern |
| `skipNonUTF8` | boolean |  | `false` | Skip non-UTF8 files |

The server's filter options are the defaults; arguments override them per
request.

### `search_in_files`

Searches file contents for text or a regular expression.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | Directory path to search in |
| `query` | string | yes |  | Search query |
| `allowGitignore` | boolean |  | `true` | Respect .gitignore rules |
| `excludeDir` | string |  |  | Exclude these directories (comma-separated) |
| `excludeExt` | string |  |  | Exclude these extensions (comma-separated) |
| `ignoreDotfiles` | boolean |  | `false` | Ignore dotfiles |
| `includeExt` | string |  |  | Include only these extensions (comma-separated) |
| `isRegex` | boolean |  | `false` | Treat query as regex |
| `maxResults` | integer |  | `100` | Maximum number of results |

Each result is `path:line:text`, one per matching line however often it
matches (`maxResults` counts lines): the line number counts from 1, the line
is whole (a CRLF line keeps its `\r`), and a query never spans lines. A file
is searched in its text as [`get_file_content`](#get_file_content) shows it —
decoded from its own encoding, its byte order mark dropped
([Encodings](cli.md#encodings)) — so text in Shift_JIS or UTF-16 is found as
the text it is. Binary files are not searched, nor are files whose encoding
cannot be told or that are not in the encoding the server's `--file-encoding`
names for them; under `--skip-non-utf8`, only files whose bytes are valid
UTF-8 are (7-bit ISO-2022-JP among them, searched decoded).

Example:

```text
api/handler.go:14:	return orders.Place(id, total)
api/handler.go:19:	return orders.Place(id, total)
```

### `get_directory_tree`

Returns the directory tree as JSON.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | Directory path to scan |
| `excludeDirs` | string |  |  | Comma-separated directories to omit: a name (matches that path component anywhere, e.g. vendor) or a path relative to the tree root (e.g. storage/framework) |
| `maxDepth` | integer |  |  | List directories at most this deep (1 = the path's direct children); deeper directories are listed without contents and marked truncated. 0 or absent = unlimited |

Bound large repositories with `maxDepth` and `excludeDirs`.

### `get_file_info`

Returns a file's size, language, extension and modification time.
`language` is the language Ark analyzes the file as
([`get_language_support`](#get_language_support): `terraform` for `.tf` and
`.tfvars`, `javascript` for `.jsx`), otherwise a highlighting tag from the
file name (`yaml`, `json` — Terraform's `.tf.json` included —, …), or empty.

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | File path to analyze |

### `get_project_stats`

Counts files by extension (`extensionStats`), by language or highlighting
tag as [`get_file_info`](#get_file_info) reports it (`languageStats`), and by
the languages Ark analyzes (`supportedLanguageStats`: the files of
[`get_language_support`](#get_language_support)'s languages).

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | Project directory path |
| `allowGitignore` | boolean |  | `true` | Respect .gitignore rules |
| `ignoreDotfiles` | boolean |  | `false` | Ignore dotfiles |

## Analysis status

### `get_diagnostics`

Lists what Ark could not analyze while indexing: unreadable files, provider
failures and `parse_error` regions the parser rejected. A
`reference_id_collision` warning names a reference left out of the index
because another reference already has its ID (a provider stating one
reference twice, or a hash collision).

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | yes |  | Directory to index (repository root or a subdirectory) |
| `filePattern` | string |  |  | Only diagnostics of files whose path contains this substring |
| `maxResults` | integer |  | `100` | Maximum number of diagnostics to return |
| `offset` | integer |  | `0` | Number of matching diagnostics to skip (for paging) |
| `severity` | string (`error`, `warning`) |  |  | Only diagnostics of this severity |

Example:

```json
{"path":".","diagnostics":[],"total":0,"truncated":false,
 "summary":{"filesIndexed":5,"filesSkipped":0,"filesWithDiagnostics":0,"errors":0,"warnings":0}}
```

Diagnostics describe Ark's analysis, not compiler errors: the parser rejects
some valid code. No diagnostics does not mean the analysis is complete: only
files of supported languages are examined. `summary` counts all diagnostics;
`total` counts those matching the filters.

### `get_language_support`

Lists the supported languages, their extensions and support levels.

No parameters.

Example:

```json
[{"language":"go","level":"context_quality_certified","extensions":[".go"]}, …,
 {"language":"terraform","level":"graph","extensions":[".tf",".tfvars"]},
 {"language":"rust","level":"graph","extensions":[".rs"]},
 {"language":"html","level":"graph","extensions":[".html",".htm"]},
 {"language":"css","level":"graph","extensions":[".css"]}]
```

See [Language support](language-support.md).

## Resources

`resources/list` advertises two URI schemes, subject to the same path rules
as the tools:

| URI | Returns |
|---|---|
| `file://<path>` | The content of a file |
| `directory://<path>` | The listing of a directory |
