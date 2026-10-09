# Ark

**Code intelligence engine for AI coding agents.**

[日本語](README_ja.md) · [Getting started](docs/getting-started.md) ·
[MCP tools](docs/mcp-tools.md) · [Resolution model](docs/resolution-model.md) ·
[Architecture](ARCHITECTURE.md)

Ark parses a repository, extracts its declarations and references, resolves
what each reference points to, and serves the resulting graph to coding
agents over the [Model Context Protocol](https://modelcontextprotocol.io).
An agent can ask who calls a function, what a change may affect, or which
code it needs to read before editing — and get an answer that states how
certain it is.

Ark is a single static binary. It analyzes Go, TypeScript, TSX, PHP,
Terraform, JavaScript and Python without running any code from the
repository.

## Why Ark

An agent working in a large repository usually navigates by text search and
by reading whole files. That has costs that grow with the repository:

- **Context is spent on irrelevant code.** Finding one function means reading
  the files around every match.
- **Names are ambiguous.** `Save` may be declared in five packages; a text
  match cannot tell which one a call reaches.
- **Relationships are guessed.** A function with the same name in another
  package, or another language, looks like a caller.
- **Unknowns look like answers.** "No other callers found" may mean there are
  none, or that the search could not see them.

Ark addresses these with structure and evidence:

| Problem | What Ark provides |
|---|---|
| Too much text | Declarations, call relationships and token-budgeted context for one symbol, instead of whole files |
| Ambiguous names | Every declaration has its own `symbolId`; ambiguous queries return the list of declarations, never a silent pick |
| Guessed relationships | References resolved by each language's scoping rules: imports, packages, receivers, namespaces. Names never match across languages |
| Hidden unknowns | Every answer carries a confidence level and counts what Ark saw but could not resolve |

## How it works

```text
repository files
      │
      ▼
language providers   Tree-sitter parsing, one file at a time:
      │              declarations, references, imports, language evidence
      ▼
index builder        symbols with stable IDs, containment
      │
      ▼
resolver             evidence → candidates + confidence (exact / strong / candidate)
      │
      ▼
graph                edges only from a unique exact or strong resolution;
      │              everything else counted, never dropped
      ▼
context · impact · repository map · search
      │
      ▼
MCP server  ──────►  coding agent
```

The index is built on the first request and reused while the source files
are unchanged; extraction results are cached on disk between sessions.
Details: [ARCHITECTURE.md](ARCHITECTURE.md),
[Operations](docs/operations.md).

## Quick start

**1. Install**

```bash
brew install magicdrive/tap/ark
```

Or download a binary from [Releases](https://github.com/magicdrive/ark/releases),
or run `go install github.com/magicdrive/ark@main` (not `@latest`, which
resolves to the legacy v1 line — see
[Getting started](docs/getting-started.md#go-toolchain)).

**2. Connect your agent** — in the repository root:

```bash
ark setup claude          # Claude Code
ark setup cursor          # Cursor
ark setup codex           # Codex
ark setup cline           # Cline CLI
ark setup copilot-vscode  # GitHub Copilot in VS Code
ark setup copilot-cli     # GitHub Copilot CLI
```

`setup` adds Ark's entry to the client's MCP configuration and leaves every
other entry untouched. Restart the client and approve the server.

**3. Tell the agent how to use it** (optional, recommended):

```bash
ark instruction claude >> CLAUDE.md   # or codex / cursor / cline / copilot-vscode / copilot-cli
```

**4. Ask** — "Give me an overview of this repository", "Who calls
`PlaceOrder`?", "What could break if I change `store.Save`?".

Full guide, including manual configuration:
[Getting started](docs/getting-started.md).

## Example: before changing a function

A five-file Go service: `api` calls `orders.Place`, which calls `store.Save`.
The responses below are Ark's actual output, shortened where marked `…`.

**What depends on `store.Save`?** — `analyze_change_impact`

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

**What does `Checkout` call?** — `get_callees`

```json
{
  "symbol": "Checkout",
  "edges": [
    { "from": "Handler.Checkout", "to": "Place", "kind": "calls", "confidence": "exact",
      "evidence": "orders.Place declared in imported package example.com/shop/orders (orders)" }
  ],
  "unattributed": 1,
  "candidates": [
    { "symbol": "Log.Record", "file": "audit/audit.go", "kind": "call", "confidence": "candidate",
      "evidence": "only symbol named \"Record\" in repository", "references": 1 }
  ],
  "unresolved": 0,
  "outsideRepository": 0
}
```

`Checkout` also calls `h.audit.Record(…)` through a struct field whose type
Ark does not track. Ark does not claim that edge: it reports `Log.Record` as a
candidate and counts the reference as unattributed, so the agent knows the
edge list is not the whole story.

**What do I need to read to change `Place`?** — `get_context`

```text
### orders/orders.go:10-15
Symbol: Place
Reason: target
Confidence: exact

func Place(id string, total int) error {
	if err := validate(total); err != nil {
		return err
	}
	return store.Save(store.Order{ID: id, Total: total})
}

### store/store.go:12-15
Symbol: Save
Reason: direct callee
Confidence: exact
…
--- stats: 6/6 items, ~143 tokens (budget 600), unattributed: 0 callers, 0 callees; unresolved callees: 0, outside repository: 0 ---
```

The target, its callees and its callers: six declarations, about 140
estimated tokens, instead of the three files they live in.

## Capabilities

Ark's MCP server provides 21 tools. Reference with parameters and examples:
[MCP tools](docs/mcp-tools.md).

| Task | Tools |
|---|---|
| Orient in a repository | `get_repository_map`, `get_directory_tree`, `get_project_stats` |
| Find symbols | `search_context` (partial names), `find_symbol` (regex), `search_code` (kind, calls, type usage), `get_symbols`, `get_symbol` |
| Trace relationships | `get_callers`, `get_callees`, `get_relations`, `find_references` |
| Assess a change | `analyze_change_impact` |
| Retrieve focused context | `get_context`, `search_context` |
| Read and search files | `get_file_content`, `get_files_arklite`, `search_in_files`, `list_files`, `get_file_info` |
| Inspect the analysis | `get_diagnostics`, `get_language_support` |

## Reliability and confidence

Every resolved reference has a confidence level. The level describes the
evidence, not a probability.

| Level | Meaning | Becomes a graph edge |
|---|---|---|
| `exact` | One target, proven by the language's scoping as Ark models it: same file, an import, a qualified name, a declared type | yes |
| `strong` | One target by weaker evidence, such as the only declaration of the name in the package | yes |
| `candidate` | One or more plausible targets | **no** |
| unresolved | No target in the repository (a builtin, an external package, a computed name) | no |

What this means in practice:

- **Treat `candidate` as a lead, not a dependency.** Callers, callees, impact
  and context are built from edges only.
- **An empty list is not proof of absence.** Every graph answer reports
  `unattributed` (references that may be missing edges), `unresolved` and
  `outsideRepository` counts, and `indexDiagnostics` when files could not be
  fully analyzed.
- **Ark does not guess between declarations.** Ambiguous names return the
  list of declarations with their `symbolId`.
- **Names never cross languages.** A Python `run()` is never a call to a Go
  `run`; TSX and TypeScript share names because TSX is TypeScript.
- **Go follows Go's scoping.** Unqualified names resolve within their
  package, `pkg.Name` through its import, and locally shadowed names are
  never resolved elsewhere.

Ark's evidence is static. It is not a compiler and does not infer types:
method calls whose receiver type is not written down locally stay
`candidate`. Full model: [Resolution model](docs/resolution-model.md).

## Language support

Support differs by language. Each language has a certified level — the
highest stage Ark's tests cover:

| Language | Level | Notes |
|---|---|---|
| Go | context-quality certified | Package scoping; method calls need a locally written receiver type |
| TypeScript, TSX | context-quality certified | Relative imports, barrels, typed receivers; no `tsconfig` paths or type inference |
| PHP | graph | Namespaces, `use`, inheritance and traits; no framework or autoload semantics |
| Terraform | graph | Module-scoped addresses, local module outputs; `.tf.json` not read |
| JavaScript | references | Top-level functions, classes, `const`/`let`; class methods not extracted |
| Python | references | Top-level functions and classes; methods not extracted |

Graph tools answer for every language, but results above a language's
certified level are not covered by tests. Details and per-language
limitations: [Language support](docs/language-support.md).

## Performance

Measured with the MCP server on public repositories (median of five runs;
[method and environment](docs/performance.md)):

| Repository | Files indexed | First request | Later requests | After restart (cache) | Peak memory |
|---|---:|---:|---:|---:|---:|
| ky (TypeScript) | 34 | 0.14 s | 2 ms | 21 ms | 50 MB |
| express (JavaScript) | 152 | 0.40 s | 10 ms | 82 ms | 56 MB |
| Ark (Go) | 600 | 3.8 s | 41 ms | 0.33 s | 246 MB |
| golang.org/x/tools (Go) | 1,875 | 18.5 s | 138 ms | 1.7 s | 959 MB |

Ark makes no general claim about token or time savings for agents; those
depend on the agent, model and task. Token budgets in Ark's responses are
estimates (`len(text)/4`), not model token counts.

## Security and privacy

- Ark reads files under the served root and never executes repository code,
  build tools or package managers.
- Ark opens no outbound network connections. The optional HTTP transport
  listens on `localhost` only and has no authentication.
- Tool paths are confined to the root. Symlinks leading out of it are
  neither read nor indexed unless the operator starts the server with
  `--allow-external-symlinks on` ([Symlink policy](SECURITY.md#symlink-policy)).
- Ark returns source code to the MCP client; whether it is sent to a model
  provider is decided by the client. Secrets that Ark's pattern rules detect
  (cloud and service tokens, private keys, `password = …` assignments) are
  masked in MCP responses by default (`--mask-secrets off` disables it, with
  a warning); masking is not exhaustive, and paths and symbol names are never
  masked.
- Files excluded by `.arkignore` are invisible to the MCP server: no tool
  reads, lists, searches or indexes them, whatever the masking setting.
- The server caches extraction results in `<root>/.ark/index`; add `.ark/` to
  `.gitignore`.

Details and known limitations: [SECURITY.md](SECURITY.md).

## Limitations

- No type inference, return-type propagation or control-flow analysis.
- Module resolution is not compiler-equivalent: `tsconfig` paths, Composer /
  PSR-4 autoloading and `package.json` are not interpreted.
- No framework semantics (dependency-injection containers, routing,
  decorators).
- No cross-language references, even through an explicit import (a
  JavaScript file importing a TypeScript file).
- JavaScript and Python class methods are not extracted.
- A file the parser cannot fully read is analyzed only where it parses; the
  rest is reported by `get_diagnostics`.

## Also in the box

- **Repository dump** — `ark <dir>` writes a directory's tree and file
  contents to one text, Markdown, XML or compact *arklite* file, with
  `.gitignore` handling and secret masking.
- **`ark symbol` / `ark syntax`** — print one file's declarations or syntax
  tree.
- **`ark skill`** — generate task guidance for agents that use skills.

See the [CLI reference](docs/cli.md).

## Documentation

| Document | Contents |
|---|---|
| [Getting started](docs/getting-started.md) | Installation, agent setup, first session, upgrades |
| [MCP tools](docs/mcp-tools.md) | Every tool's parameters, output and caveats |
| [Resolution model](docs/resolution-model.md) | Confidence, evidence, name spaces, how to read answers |
| [Language support](docs/language-support.md) | Levels and per-language capabilities and limits |
| [CLI reference](docs/cli.md) | Every command and option |
| [Performance](docs/performance.md) | Measurements and how to reproduce them |
| [Operations](docs/operations.md) | Index lifecycle, cache, resources, failure modes |
| [Troubleshooting](docs/troubleshooting.md) | Common problems and messages |
| [Architecture](ARCHITECTURE.md) | Design invariants for contributors |
| [Security](SECURITY.md) | Threat model and data boundary |

## Contributing

Bug reports and pull requests are welcome on
[GitHub](https://github.com/magicdrive/ark/issues). Before changing the
analysis, read [ARCHITECTURE.md](ARCHITECTURE.md): it lists the invariants a
plausible-looking change can break and the tests that enforce them. A change
is verified as described in its section 8 (`make lint` runs most checks
locally).

## License

[MIT](LICENSE) © 2025–2026 Hiroshi IKEGAMI
