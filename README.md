# Ark

> Yet another alternate \[directory | repository\] represent text generator tool

**ark** is a powerful CLI tool designed to recursively scan a directory and generate a readable and well-formatted text representation of its structure and contents. Ideal for:

* 📚 Sharing codebases with LLMs
* 🧪 Static analysis workflows
* 🗂️ Snapshotting source trees with clean formatting

Supports both **plaintext**, **markdown**, **xml**, and **arklite** outputs, full UTF-8 support with optional skip behavior, and extensive filtering options.

---

## 🚀 Quick Start

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

### 2. Generate ark_output.txt

```bash
ark <dirname>
```

---

## 🧰 Usage

```sh
ark [OPTIONS] <dirname>
```

### Arguments

| Argument        | Description                             |
| --------------- | --------------------------------------- |
| `<dirname>`     | The target directory to scan            |
| `<byte_string>` | Byte size string (e.g. 10M, 100k)   |
| `<extension>`   | File extension name (e.g. go, ts, html) |
| `<regexp>`      | Regular expression string (Go syntax)   |

---

## ⚙️ Options

| Option                                            | Alias           | Description                                                                       |
| ------------------------------------------------- | --------------- | ----------------------------------------------------------------------------------|
| `--help`                                          | `-h`            | Show help message and exit                                                        |
| `--version`                                       | `-v`            | Show version                                                                      |
| `--output-filename <filename>`                    | `-o`            | Output file name (default: `ark_output.txt`)                                      |
| `--scan-buffer <byte>`                            | `-b`            | Line scan buffer size (default: `10M`)                                            |
| `--output-format <'txt'\|'md'\|'xml'\|'arklite'>` | `-f`            | Output file format (default: `txt`)                                               |
| `--mask-secrets <'on'\|'off'>`                    | `-m`            | Detect secrets and mask them (default: `on`)                                      |
| `--allow-gitignore <'on'\|'off'>`                 | `-a`            | Enable `.gitignore` filter                                                        |
| `--additionally-ignorerule <filepath>`            | `-p`            | Additional `.gitignore`-like rules                                                |
| `--with-line-number <'on'\|'off'>`                | `-n`            | Show line numbers (default: `on`)                                                 |
| `--ignore-dotfile <'on'\|'off'>`                  | `-d`            | Ignore dotfiles (default: `on`)                                                   |
| `--pattern-regex <regexp>`                        | `-x`            | File match pattern                                                                |
| `--include-ext <ext>`                             | `-i`            | Include file extensions (comma separated)                                         |
| `--exclude-dir-regex <regexp>`                    | `-g`            | Exclude directories matching regex                                                |
| `--exclude-file-regex <regexp>`                   | `-G`            | Exclude files matching regex                                                      |
| `--exclude-ext <ext>`                             | `-e`            | Exclude file extensions (comma separated)                                         |
| `--exclude-dir <dir>`                             | `-E`            | Exclude specific directory names                                                  |
| `--skip-non-utf8`                                 | `-s`            | Skip non-UTF-8 files                                                              |
| `--silent`                                        | `-S`            | Suppress logs                                                                     |
| `--delete-comments`                               | `-D`            | Strip comments based on language detection                                        |

---

## 📦 Output Format Examples

### Plaintext (`--output-format txt`)

```
example_project
├── main.go
└── sub
    └── sub.txt

=== sub/sub.txt ===
hello world
```

---

### Markdown (`--output-format md`)

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

---

### XML (`--output-format xml`)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<ProjectDump>
  <Description>
    <ProjectName>example_project</ProjectName>
    <ProjectPath>/absolute/path/to/example_project</ProjectPath>
  </Description>
  <Tree>
    <![CDATA[
example_project
├── main.go
└── sub
    └── sub.txt
    ]]>
  </Tree>
  <Files>
    <File path="main.go">
      <![CDATA[
package main
func main() {
  println("hello")
}
      ]]>
    </File>
    <File path="sub/sub.txt">
      <![CDATA[
hello world
      ]]>
    </File>
  </Files>
</ProjectDump>
```

---

### Arklite (`--output-format arklite`)

```
# ArkLite Format: example_project (/absolute/path/to/example_project)

## Directory Tree (JSON)
{"name":"example_project","type":"directory","children":[{"name":"main.go","type":"file"},{"name":"sub","type":"directory","children":[{"name":"sub.txt","type":"file"}]}]}

## File Dump
@main.go
package main␤func main() {␤  println("hello")␤}
@sub/sub.txt
hello world
```

---


#### 🤔  What's Arklite?

Arklite format is a highly compact format tailored for LLM input efficiency. It consists of:

1. 📝 A natural language description (project name and absolute path)
2. 🗂 A JSON-based directory structure
3. 📄 A one-line-per-file representation of all text content (with comments stripped)

Each file is prefixed with `@<relative path>` followed by a newline-delimited (`␤`) single-line content.

#### Example

```
## Project: example_project
## Path: /absolute/path/to/example_project

## Directory Tree (JSON)
{"name":"example_project","type":"directory","children":[{"name":"main.go","type":"file"},{"name":"sub","type":"directory","children":[{"name":"sub.txt","type":"file"}]}]}

## File Dump
@main.go
package␤main␤func␤main(){␤fmt.Println("hello")}
@sub/sub.txt
hello␤world
```

This format is designed to be minimal and structured for high compression in LLM token space.


## 🗂 Example `.arkignore`

```
# =============================
# VCS / Version Control
# =============================
.git/
.hg/
.svn/

# =============================
# Editors / IDEs
# =============================
.idea/
.vscode/
*.code-workspace
*.sublime-project
*.sublime-workspace
```

---

## 🧩 Integrations

```sh
source completions/ark-completion.sh
```

---

## 📎 See Also

- Project: https://github.com/magicdrive/ark

## Author

(c) 2025 Hiroshi IKEGAMI

## License

This project is licensed under the [MIT License](https://github.com/magicdrive/ark/blob/main/LICENSE)
