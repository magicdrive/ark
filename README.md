
# Ark

> Yet another alternate \[directory | repository\] text generator tool — with code intelligence MCP tools

**ark** recursively scans a directory and produces a clean, human‑readable dump of the tree and file contents. It also provides **code intelligence** features via Tree-sitter for symbol extraction and MCP (Model Context Protocol) server support. Perfect for

* 📚 sharing codebases with LLMs
* 🧪 static‑analysis pipelines
* 🗂️ snapshotting source trees
* 🔍 **code intelligence** — extract symbols, navigate definitions
* 🛰️ **MCP server** — serve codebase context to AI agents

It supports **plaintext**, **markdown**, **XML**, and **arklite** outputs, full UTF‑8 handling (with optional skip), extensive filtering, and **Tree-sitter powered** code analysis.

---

## 🚀 Quick Start

### 1. Install

```bash
go install github.com/magicdrive/ark@latest
```

Using Homebrew:

```bash
brew install magicdrive/tap/ark
```

Or download a pre-built binary from [Releases](https://github.com/magicdrive/ark/releases).

---

### 2. Generate a codebase dump

```bash
ark <dirname>                # creates ark-output.txt in the cwd
```

---

### 3. Set up Ark MCP for Claude Code

Run once in your project root:

```bash
cd /your/project
ark setup --name my-project
```

This single command:
- Writes `.mcp.json` — registers the Ark MCP server
- Installs `.claude/commands/my-project.md` — activates `/my-project` as a slash command

Then **restart Claude Code** and approve the MCP server when prompted.

After that, you can use:
- **`/my-project`** — slash command that explores your codebase using Ark MCP tools
- **MCP tools directly** — `mcp__ark__find_symbol`, `mcp__ark__get_symbols`, etc.

```
/my-project         ← loads exploration assistant with all 19 Ark MCP tools
```

> **Tip:** Add a `CLAUDE.md` to your project root to instruct Claude Code to use Ark MCP tools automatically. A ready-to-use template is available at [`misc/CLAUDE.md.example`](misc/CLAUDE.md.example).

---

## 🧰 Basic Usage

```text
ark [OPTIONS] <dirname>
ark setup [OPTIONS]
ark mcp-server [OPTIONS]
ark mcp-init [OPTIONS]
ark syntax <file> [OPTIONS]
ark symbol <file> [OPTIONS]
ark skill [OPTIONS]
```

---

## 📂 Sub‑commands

| Command      | Description                                      |
|--------------|--------------------------------------------------|
| `setup`      | One-step setup: MCP config + Claude Code slash command. |
| `mcp-server` | Run Ark as an MCP server (stdio or HTTP).        |
| `mcp-init`   | Add ark MCP config to `.mcp.json`.              |
| `syntax`     | Parse file and output AST using Tree-sitter.     |
| `symbol`     | Extract symbols (functions, types, etc.) from file. |
| `skill`      | Generate Ark MCP skill for Claude Code / OpenAI. |

---

## ⚙️ General Options

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--help` | `-h` | Show help and exit | – |
| `--version` | `-v` | Show version | – |
| `--output-filename <file>` | `-o` | Name of the output file | `ark-output.txt` |
| `--scan-buffer <size>` | `-b` | Read buffer size (`10M`, `500K`, …) | `10M` |
| `--output-format <fmt>` | `-f` | `txt`, `md`, `xml`, `arklite` | `txt` |
| `--mask-secrets <on/off>` | `-m` | Detect & mask secrets | `on` |
| `--allow-gitignore <on/off>` | `-a` | Obey `.gitignore` rules | `on` |
| `--additionally-ignorerule <file>` | `-A` | Extra ignore‑rule file | – |
| `--with-line-number <on/off>` | `-n` | Prepend line numbers | `on` |
| `--ignore-dotfile <on/off>` | `-d` | Skip dotfiles | `off` |
| `--pattern-regex <regexp>` | `-x` | Include paths matching regexp | – |
| `--include-ext <exts>` | `-i` | Include only ext(s) (`go,ts,html`) | – |
| `--exclude-dir-regex <regexp>` | `-g` | Exclude dirs matching regexp | – |
| `--exclude-file-regex <regexp>` | `-G` | Exclude files matching regexp | – |
| `--exclude-ext <exts>` | `-e` | Exclude ext(s) | – |
| `--exclude-dir <names>` | `-E` | Exclude dirs by name | – |
| `--compless` | `-c` | Compress result with **arklite** | – |
| `--skip-non-utf8` | `-s` | Ignore non‑UTF‑8 files | – |
| `--silent` | `-S` | Suppress logs / progress | – |
| `--delete-comments` | `-D` | Strip comments (language‑aware) | – |

---

## ⚡ setup — One-step Claude Code Setup

`ark setup` is the fastest way to integrate Ark into any project. It combines `mcp-init` and `skill` in a single command:

```bash
cd /your/project
ark setup --name my-project
```

This does three things at once:
1. Writes `.mcp.json` — registers the Ark MCP server for this project
2. Generates `skills/my-project/` — full skill documentation
3. Installs `.claude/commands/my-project.md` — activates `/my-project` as a Claude Code slash command

Then restart Claude Code to approve the MCP server, and you're ready.

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--name <name>` | `-n` | Project name (used for skill and slash command) | directory name |
| `--ark-path <path>` | `-p` | Path to ark binary | auto-detect |
| `--root <dir>` | `-r` | Root directory to serve | `$PWD` |
| `--global` | `-g` | Write MCP config to `~/.claude/settings.json` | `.mcp.json` |
| `--force` | `-f` | Overwrite existing entries | – |

---

## 🔧 mcp‑init Options

`ark mcp-init` adds (or updates) the ark MCP server entry in `.mcp.json` for the current project, so you can use ark tools in Claude Code without manual configuration.

```bash
# Add ark MCP to the current project
ark mcp-init

# Add to global Claude Code settings (~/.claude/settings.json)
ark mcp-init --global

# Specify a custom root directory
ark mcp-init --root /path/to/project

# Overwrite an existing entry
ark mcp-init --force
```

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--ark-path <path>` | `-p` | Path to ark binary | auto-detect |
| `--root <dir>` | `-r` | Root directory to serve | `$PWD` |
| `--name <name>` | `-n` | MCP server name | `ark` |
| `--global` | `-g` | Write to `~/.claude/settings.json` | `.mcp.json` (project-local) |
| `--force` | `-f` | Overwrite existing entry | – |

---

## 🛰  mcp‑server Options

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--root <dir>` | `-r` | Serve directory root | `$PWD` |
| `--type <stdio\|http>` | `-t` | HTTP listen port | `stdio` |
| `--http-port <port>` | `-p` | HTTP listen port | `8522` |
| `--scan-buffer <size>` | `-b` | Read buffer size (`10M`, `500K`, …) | `10M` |
| `--mask-secrets <on/off>` | `-m` | Detect & mask secrets | `on` |
| `--allow-gitignore <on/off>` | `-a` | Obey `.gitignore` rules | `on` |
| `--additionally-ignorerule <file>` | `-A` | Extra ignore‑rule file | – |
| `--ignore-dotfile <on/off>` | `-d` | Skip dotfiles | `off` |
| `--pattern-regex <regexp>` | `-x` | Include paths matching regexp | – |
| `--include-ext <exts>` | `-i` | Include only ext(s) (`go,ts,html`) | – |
| `--exclude-dir-regex <regexp>` | `-g` | Exclude dirs matching regexp | – |
| `--exclude-file-regex <regexp>` | `-G` | Exclude files matching regexp | – |
| `--exclude-ext <exts>` | `-e` | Exclude ext(s) | – |
| `--exclude-dir <names>` | `-E` | Exclude dirs by name | – |
| `--skip-non-utf8` | `-s` | Ignore non‑UTF‑8 files | – |
| `--delete-comments` | `-D` | Strip comments (language‑aware) | – |
| `--no-cache` | – | Disable persistent index cache | – |

---

## 🔍 syntax Options

| Option | Description | Default |
|--------|-------------|---------|
| `--lang <language>` | Language (go, typescript, tsx, javascript, python, php) | auto-detect |
| `--format <text\|json>` | Output format | `text` |
| `-h, --help` | Show help | – |

```bash
ark syntax main.go                # Parse Go file
ark syntax app.ts --format json   # Parse TypeScript, JSON output
ark syntax script.py --lang python
```

---

## 🏷️ symbol Options

| Option | Description | Default |
|--------|-------------|---------|
| `--lang <language>` | Language (go, typescript, tsx, javascript, python, php) | auto-detect |
| `--format <text\|json>` | Output format | `text` |
| `-h, --help` | Show help | – |

```bash
ark symbol main.go                # Extract symbols from Go file
ark symbol app.ts --format json   # Extract symbols, JSON output
ark symbol script.py --lang python
```

---

## 🎯 skill Command

### Subcommands

| Subcommand | Description |
|------------|-------------|
| `skill` | Auto mode - detect existing skills and generate appropriate type |
| `skill init` | Generate Repository Skill (full repo-specific skill) |
| `skill add-explorer` | Add Explorer Skill as companion to existing skills |
| `skill update` | Update Ark-managed skills (preserves user files) |
| `skill inspect` | Show detected skills and repository analysis |

### Options

| Option | Description | Default |
|--------|-------------|---------|
| `--name <name>` | Skill name | auto-determined |
| `--output <dirname>` | Output directory | `skills/<name>` |
| `--archive` | Create ZIP archive | – |
| `--force` | Overwrite existing skill | – |
| `-h, --help` | Show help | – |

### Update Options

| Option | Description |
|--------|-------------|
| `--force` | Force update even if user modifications detected |
| `--dry-run` | Show what would be updated without making changes |

```bash
ark skill                              # Auto mode
ark skill init                         # Generate Repository Skill
ark skill add-explorer                 # Add Explorer Skill
ark skill update                       # Update Ark-managed skills
ark skill update --dry-run             # Preview update
ark skill inspect                      # Show detected skills
```

### Skill Types

**Repository Skill** (`skill init`): Full project-specific skill including:
- Build/test commands detected from go.mod, package.json, Makefile, etc.
- Project language analysis
- Conventions reference file

**Explorer Skill** (`skill add-explorer`): Lightweight companion skill for code navigation:
- Ark MCP tool usage guide
- Works alongside existing project skills

### Generated Files

Skills include YAML frontmatter for safe updates:
- `SKILL.md` - Skill documentation with `ark-managed: true` metadata
- `agents/openai.yaml` - OpenAI/Cline agent configuration
- `agents/claude-code.md` - Claude Code custom agent (uses `mcp__ark__*` tool names)
- `references/conventions.md` - Project conventions (Repository Skill only)

The `agents/claude-code.md` file is also automatically installed to `.claude/commands/` as a Claude Code slash command that uses all 19 Ark MCP tools.

---

## 📝 Arguments

| Argument | Description |
|----------|-------------|
| `<dirname>` | Directory to scan |
| `<byte-string>` | Size string (`10M`, `100K`, …) |
| `<extension>` | File extension (`go`, `ts`, `html`) |
| `<regexp>` | Go `regexp` syntax pattern |

---

## 📦 Output Examples

<details>
<summary>Plaintext <code>(--output-format txt)</code></summary>

```text
example_project
├── main.go
└── sub
    └── sub.txt

=== sub/sub.txt ===
hello world
```
</details>

<details>
<summary>Markdown <code>(--output-format md)</code></summary>

````markdown
# Project Tree
```
example_project
├── main.go
└── sub
    └── sub.txt
```

---

# File: sub/sub.txt
```txt
hello world
```
````
</details>

<details>
<summary>XML <code>(--output-format xml)</code></summary>

```xml
<?xml version="1.0" encoding="UTF-8"?>
<ProjectDump>
  <Description>
    <ProjectName>example_project</ProjectName>
    <ProjectPath>/abs/path/example_project</ProjectPath>
  </Description>
  <Tree><![CDATA[
example_project
├── main.go
└── sub
    └── sub.txt
  ]]></Tree>
  <Files>
    <File path="main.go"><![CDATA[
package main
func main() { println("hello") }
    ]]></File>
    <File path="sub/sub.txt"><![CDATA[
hello world
    ]]></File>
  </Files>
</ProjectDump>
```
</details>

<details>
<summary>Arklite <code>(--output-format arklite)</code></summary>

```
# Arklite Format: example_project (/abs/path/example_project)

## Directory Tree (JSON)
{"name":"example_project","type":"directory","children":[{"name":"main.go","type":"file"},{"name":"sub","type":"directory","children":[{"name":"sub.txt","type":"file"}]}]}

## File Dump
@main.go
package main␤func main(){␤println("hello")␤}
@sub/sub.txt
hello world
```
</details>

---

## 🤔 What is Arklite?

Arklite is a compact single‑line‑per‑file format tuned for LLM token efficiency:

1. Natural‑language header (project + path)  
2. JSON directory tree  
3. File dump (`@path` + content with `␤` for newlines)

---

## 🗂 Example `.arkignore`

```gitignore
# VCS
.git/
.hg/
.svn/

# IDEs / editors
.idea/
.vscode/
*.code-workspace
*.sublime-*
```

---

## 🧩 Shell Completions

```sh
# Bash & Zsh
source completions/ark-completion.sh
# Fish
funcsave ark
```

---

## ✨ Why Ark?

### 🎯 Symbol-First Code Exploration

Instead of dumping entire files, **extract only what you need**:

```bash
$ ark symbol internal/mcp/tools.go
File: internal/mcp/tools.go (go)
Symbols: 11

  struct ToolsHandler [exported] (line 15-18)
  function NewToolsHandler [exported] (line 21-26)
  method ListTools (ToolsHandler) [exported] (line 29-251)
  method CallTool (ToolsHandler) [exported] (line 254-279)
  ...
```

**Benefits:**
- 📉 **Token-efficient** — No need to read entire files
- 🎯 **Precise** — Jump directly to the definition you need
- 🔍 **Discoverable** — `[exported]` markers show API surface at a glance

### 🚀 Pure Go + Tree-sitter = Best of Both Worlds

- **No CGO required** — Cross-compile anywhere, single static binary
- **Real parsing** — Not regex hacks, actual AST-based symbol extraction
- **Multi-language** — Go, TypeScript, TSX, JavaScript, Python, PHP (and growing!)

### 🌐 Language Support

Ark advertises only the capabilities it actually tests. Levels build up:
Parse → Symbols → References → Resolution → Graph → Context.

| Language   | Parse | Symbols | References | Resolution | Graph | Context |
|------------|:-----:|:-------:|:----------:|:----------:|:-----:|:-------:|
| Go         |   ✓   |    ✓    |     ✓      |     ✓      |   ✓   |    ✓    |
| TypeScript |   ✓   |    ✓    |     ✓      |            |       |         |
| TSX        |   ✓   |    ✓    |     ✓      |            |       |         |
| JavaScript |   ✓   |    ✓    |     ✓      |            |       |         |
| Python     |   ✓   |    ✓    |     ✓      |            |       |         |
| PHP        |   ✓   |    ✓    |     ✓      |     ✓      |   ✓   |         |

A checkmark means the canonical Language Registry advertises that level as the
language's certified support level; `get_language_support` reports it at runtime.
PHP's `get_context` path is implemented and covered by dedicated context-quality
tests, but PHP is advertised at **Graph** level because several resolver
precision areas (namespace/import, inherited and trait member resolution) remain
intentionally conservative — so the Context cell is left unchecked rather than
overstating certification.

#### PHP — static code intelligence

Ark statically extracts PHP **symbols** (namespaces, classes, interfaces,
traits, enums, functions, constants, methods, constructors, properties,
class constants, enum cases, promoted properties), **imports** (plain / aliased
/ grouped / function / const `use`), **references** (function / static /
instance / `$this` calls, construction, class-constant reads, type references),
and **typed relations** (`extends` / `implements` / trait `use`) into a typed
symbol graph and agent-oriented context — while **preserving uncertainty** for
dynamic or ambiguous constructs.

**Known limitations (by design):** dynamic calls / construction (`$obj->$m()`,
`new $c()`) are not guessed; variable receivers are not type-inferred; there is
no Composer / PSR-4 / autoload resolution; `use`-alias and inherited/trait member
resolution are intentionally conservative (honest `Candidate` / `Unresolved`
rather than a fabricated answer); no framework (Laravel/Symfony/…) semantics.
Ark performs **pure static analysis** and never executes repository code,
Composer, or any PHP tooling.

### 🤖 LLM-Optimized Workflow

Ark provides **19 MCP tools** covering the full code-intelligence stack:

| Tool | Description |
|------|-------------|
| `get_directory_tree` | Understand project layout |
| `get_symbols` | List functions/types in a file |
| `find_symbol` | Search for a symbol by name across the repo |
| `get_symbol` | Get source code of one specific function/type |
| `search_in_files` | Full-text or regex search across files |
| `list_files` | Filter-aware file listing |
| `get_file_content` | Read a whole file |
| `get_file_info` | File metadata (size, lines, language) |
| `get_project_stats` | Language breakdown, file counts |
| `get_files_arklite` | Multiple files in compressed format |
| `get_context` | Token-budgeted, relevance-ranked context for a symbol (target always included) |
| `find_references` | Find all usages of a symbol across the repo |
| `get_relations` | Explore import/dependency relations between files |
| `get_callers` | Find symbols that call a given symbol |
| `get_callees` | Find symbols called by a given symbol |
| `get_repository_map` | Compact logical map of the repo for LLM orientation |
| `analyze_change_impact` | Estimate impact of changing a symbol |
| `search_code` | Structural search by kind, name, type usage, etc. |
| `get_language_support` | List supported languages and their feature levels |

The core navigation pattern:

```
get_directory_tree   →   Understand project structure
        ↓
    find_symbol      →   Locate "where is Foo?"
        ↓
    get_symbols      →   List what's in a file
        ↓
    get_symbol       →   Extract exact source code
        ↓
    get_context      →   Token-budgeted context for safe modification
```

This approach **dramatically reduces token usage** compared to reading entire files, while maintaining full context awareness.

### 📦 Instant Skill Generation

```bash
$ ark skill --name my-project-explorer
✅ Created my-project-explorer/SKILL.md
✅ Created my-project-explorer/agents/openai.yaml
```

One command generates everything needed to teach ChatGPT or Cline how to efficiently explore your codebase.

---

## 📎 See Also

* Project home — <https://github.com/magicdrive/ark>

## Author

© 2025 - 2026Hiroshi IKEGAMI

## License

Released under the [MIT License](LICENSE)
