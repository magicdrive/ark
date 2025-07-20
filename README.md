
# Ark

> Yet another alternate \[directory | repository\] text generator tool

**ark** recursively scans a directory and produces a clean, human‑readable dump of the tree and file contents. Perfect for

* 📚 sharing codebases with LLMs
* 🧪 static‑analysis pipelines
* 🗂️ snapshotting source trees

It supports **plaintext**, **markdown**, **XML**, and **arklite** outputs, full UTF‑8 handling (with optional skip), and extensive filtering.

---

## 🚀 Quick Start

### 1 · Install

### 1. Installation

```bash
go install github.com/magicdrive/ark@latest
```

Using Homebrew:

```bash
brew install magicdrive/tap/ark
```

Or download a pre-built binary from [Releases](https://github.com/magicdrive/ark/releases).

---

### 2 · Generate an output file

```bash
ark <dirname>                # creates ark_output.txt in the cwd
```

---

## 🧰 Basic Usage

```text
ark [OPTIONS] <dirname>
ark mcp-server [OPTIONS]
```

---
<!--
## 📂 Sub‑commands

| Command      | Description                    |
|--------------|--------------------------------|
| `mcp-server` | Run Ark as an HTTP MCP server. |


---
-->

## ⚙️ General Options

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--help` | `-h` | Show help and exit | – |
| `--version` | `-v` | Show version | – |
| `--output-filename <file>` | `-o` | Name of the output file | `ark_output.txt` |
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

## 🛰  mcp‑server Options

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--root <dir>` | `-r` | Serve directory root | `$PWD` |
| `--type <stdio|http>` | `-t` | HTTP listen port | `stdio` |
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

## 📎 See Also

* Project home — <https://github.com/magicdrive/ark>

## Author

© 2025 Hiroshi IKEGAMI

## License

Released under the [MIT License](LICENSE)
