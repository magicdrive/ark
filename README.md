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
Terraform, Rust, Java, Zig, JavaScript and Python without running any code
from the repository.

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
language providers   Tree-sitter parsing (HTML: x/net/html; CSS: CSS Syntax 3), one file at a time:
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

Or, on macOS and Linux without Homebrew, the install script (into `~/.local/bin`; see
[Installation](#installation) for its options and checks, the archives and building from
source):

```bash
curl -fsSLO https://raw.githubusercontent.com/magicdrive/ark/main/install.sh
sh install.sh
```

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
| Java | context-quality certified | Java's own name lookup — imports, the package, nested and inherited member types, inherited members and overloads by argument count and by primitive versus reference argument types (not between two reference types), typed variables, `this` / `super`; no return or field types followed, no build files |
| PHP | graph | Namespaces, `use`, inheritance and traits; no framework or autoload semantics |
| Terraform | graph | Module-scoped addresses, local module outputs; `.tf.json` not read |
| Rust | graph | Paths within a crate (`crate`/`self`/`super`, `use`, inherent vs trait impls); no method calls on values, other crates of the workspace or macro expansion |
| HTML | graph | Ids and their references (IDREF attributes, fragments) within a document as the browser builds it, links to documents, stylesheets and scripts by relative path, the declarations of inline styles and scripts, and links to images, fonts and media; no absolute URLs |
| CSS | graph | `@keyframes`, `@font-face`, `@property`, custom properties and CSS Modules classes with their references within a stylesheet, `@import` by relative path, CSS Modules classes JavaScript and TypeScript imports use, `url()` of images and fonts; no selector matching |
| Python | graph | Scopes (LEGB), imports and relative imports, package re-exports, `self` / `cls` / `super()`, inheritance; no type inference for locals, star imports or dynamic attributes |
| Zig | graph | Lexical scopes, relative `@import`, containers and `@This()`, re-exports, generic type factories, receivers of declared types and declared result types, named modules by the importing module's import table as a build.zig states it — strong only when every build.zig of the repository is closed (Ark can follow all of its code: no dependency, build helper file or ZON import, nothing it does not model) and the binding is the only one; otherwise every import is a candidate. No comptime evaluation, `std`, or modules only other build code binds; see [Zig](docs/language-support.md#zig) |
| JavaScript | references | Declarations, class members, ES modules and CommonJS (TypeScript's extractor), scope-aware; not certified for graph or context quality |

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
  masked. The rules are a recommended default a repository can adjust in
  `.ark-secrets-rules.toml` — the MCP server applies the disabling of a rule
  only with its operator's `--secret-rule-overrides on` — and
  `ark secrets-scan` shows what they mask, and why, without the values
  ([Secret masking rules](docs/secret-rules.md)).
- Files excluded by `.arkignore` are invisible to the MCP server: no tool
  reads, lists, searches or indexes them, whatever the masking setting.
- The server caches extraction results in `<root>/.ark/index`; add `.ark/` to
  `.gitignore`.

Details and known limitations: [SECURITY.md](SECURITY.md).

## Limitations

- No type inference, return-type propagation or control-flow analysis.
- Module resolution is not compiler-equivalent: `tsconfig` paths, Composer /
  PSR-4 autoloading, `package.json`, `Cargo.toml` and Python's `sys.path`
  configuration are not interpreted.
- Macros are not expanded (Rust `macro_rules!`, procedural macros, derives).
- No framework semantics (dependency-injection containers, routing,
  decorators).
- No cross-language references by name, even through an explicit import (a
  JavaScript file importing a TypeScript file); the only relations between
  languages are by path (an HTML page's stylesheets and scripts, CSS Modules
  classes a JavaScript or TypeScript import uses).
- Python names a star import brings, `getattr` / `setattr`, monkey patching
  and `importlib` are not resolved; a method call on a local variable's value
  is not either.
- A file the parser cannot fully read is analyzed only where it parses; the
  rest is reported by `get_diagnostics`.

## Also in the box

- **Repository dump** — `ark <dir>` writes a directory's tree and file
  contents to one text, Markdown, XML or compact *arklite* file, with
  `.gitignore` handling and secret masking.
- **`ark secrets-scan`** — report what secret masking masks in a repository,
  file, line and rule, without the values; list the rules; suggest
  `.arkignore` entries.
- **`ark symbol` / `ark syntax`** — print one file's declarations or syntax
  tree (not masked).
- **`ark skill`** — generate task guidance for agents that use skills.

See the [CLI reference](docs/cli.md).

## Installation

| Way | Platforms | Update |
|---|---|---|
| Homebrew: `brew install magicdrive/tap/ark` | macOS, Linux | `brew upgrade ark` |
| The install script (below) | macOS, Linux (amd64, arm64) | `sh install.sh --upgrade` |
| An archive from [Releases](https://github.com/magicdrive/ark/releases), checked against `ark_checksums.txt` | macOS (amd64, arm64), Linux and Windows (386, amd64, arm64) | download the new one |
| From source: `git clone https://github.com/magicdrive/ark.git && cd ark && make build` (Go of `go.mod`; the binary is `build/ark`) | any | pull and build again |

### Install script (macOS and Linux, amd64 and arm64)

Download the script, read it if you like, then run it with `sh`:

```sh
curl -fsSLO https://raw.githubusercontent.com/magicdrive/ark/main/install.sh
sh install.sh                                  # the latest release, into ~/.local/bin
"$HOME/.local/bin/ark" --version             # prints: ark version vX.Y.Z
```

```sh
sh install.sh                                  # install the latest release
sh install.sh --version v6.7.2                 # install a given release (older ones too)
sh install.sh --upgrade                        # switch to the latest stable release
sh install.sh --upgrade --version v6.7.2       # switch to a given release (also a downgrade)
sh install.sh --dir "$HOME/.local/bin"         # choose the directory (this is the default)
sh install.sh --help
```

- **Where**: `$HOME/.local/bin/ark` by default; `--dir DIR` installs `DIR/ark` and creates
  `DIR` if needed (spaces in `DIR` are fine). It never uses `sudo` and never changes `PATH` or
  shell start-up files: if the script says the directory is not in `PATH`, add it, e.g.
  `export PATH="$HOME/.local/bin:$PATH"` in `~/.profile`. For a system directory such as
  `/usr/local/bin`, run it as a user who can write there (it does not ask for `sudo` itself).
- **Versions**: without `--version` it installs the latest stable release (pre-releases are not
  picked). `--version` takes a release tag, `v6.7.2` or `6.7.2`, newer or older than the
  installed one; a version that does not exist is an error, never another version. `--upgrade`
  is the same run, named for what you mean. Running it again with the installed version changes
  nothing. Every ark release can be installed (v0.0.2 and later).
- **Updating**: run `sh install.sh --upgrade` again (with the same `--dir` if you used one).
- **Checks**: the archive must match the SHA-256 listed in the release's checksum file
  (`ark_checksums.txt`); a missing or malformed checksum file stops the install. Only the
  `ark` binary is taken out of the archive, after its entries have been checked (no absolute
  paths, `..`, links or devices). The new binary must run and report the version asked for
  before it replaces the old one in a single rename: a failed or interrupted run leaves the
  installed `ark` as it was.
- **Security**: every request starts at github.com, over https, and redirects (GitHub sends the
  downloads to its asset host) are followed only to https. The checksum file comes from the same GitHub release as the archive, so it catches a
  broken or altered download, but not a release that was itself replaced on GitHub. For a
  stronger check, verify the archive's build provenance with the
  [GitHub CLI](https://cli.github.com/) before you trust it:
  `gh attestation verify ark_<version>_<os>_<arch>.tar.gz -R magicdrive/ark`
  (releases from v6.7.1 on have these attestations).
  The command above runs the script from the `main` branch; to run a fixed script, download it
  from a release tag instead (`https://raw.githubusercontent.com/magicdrive/ark/<tag>/install.sh`;
  the tags from v6.9.0 on have it). Piping it straight into `sh` (`curl ... | sh`) also works, but then you
  run what you have not seen.
- **Homebrew**: the script refuses Homebrew's directories (its prefix, e.g. `/opt/homebrew`, or
  `/usr/local` on an Intel Mac) and does not replace a symbolic link such as one Homebrew made.
  Keep one way of installing: with Homebrew, upgrade with `brew upgrade ark`; the script warns
  if another `ark` comes first in `PATH`.
- **Uninstall**: delete the file: `rm "$HOME/.local/bin/ark"` (or `DIR/ark`).
- **Needs**: a POSIX `sh`, `curl` (or GNU Wget 1.17+; BusyBox wget is refused because it does
  not check certificates, and wget2 is not supported), `tar`, `gzip`, and `sha256sum`, `shasum`
  or `openssl`. A proxy set in `https_proxy` is used. `INSTALLER_TIMEOUT` (seconds, default 60)
  is how long it waits for a connection or more data.
- **Windows**: the script does not run on Windows (Git Bash, MSYS2 and Cygwin are refused). Use
  the `ark_<version>_windows_<arch>.tar.gz` of [Releases](https://github.com/magicdrive/ark/releases) (it holds `ark.exe`). Inside WSL, the Linux instructions apply.
- **Troubleshooting**: *not writable* — choose a directory of your own with `--dir`;
  *not in PATH* — see above; *does not run in DIR (noexec)* — choose a directory on another file
  system; *SHA-256 mismatch* or *rejected* — nothing was installed; try again later and report it
  if it persists; *release not found* — check the version on the
  [Releases page](https://github.com/magicdrive/ark/releases); *inside the Homebrew prefix* —
  use `brew upgrade ark`, or another `--dir`.
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
