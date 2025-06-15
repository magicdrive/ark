# [BETA] ark

> Yet another alternate \[directory | repository] represent text generator tool

---

## 🚀 Overview

**ark** is a powerful CLI tool designed to recursively scan a directory and generate a readable and well-formatted text representation of its structure and contents. Ideal for:

* 📚 Sharing codebases with LLMs
* 🧪 Static analysis workflows
* 🗂️ Snapshotting source trees with clean formatting

Supports both **plaintext** and **markdown** outputs, full UTF-8 support with optional skip behavior, and extensive filtering options.

---

## 🧰 Usage

```sh
ark [OPTIONS] <dirname>
```

### 🔸 Description

Yet another alternate \[directory|repository] represent text generator tool.

### 🔸 Arguments

| Argument        | Description                             |
| --------------- | --------------------------------------- |
| `<dirname>`     | The target directory to scan            |
| `<byte_string>` | Byte size string (e.g. 1G, 10M, 100k)   |
| `<extension>`   | File extension name (e.g. go, ts, html) |
| `<regexp>`      | Regular expression string (Go syntax)   |

---

## ⚙️ Options

| Option                                 | Alias           | Description                                          |
| -------------------------------------- | --------------- | ---------------------------------------------------- |
| `--help`                               | `-h`            | Show help message and exit                           |
| `--version`                            | `-v`            | Show version                                         |
| `--output-filename <filename>`         | `-o`            | Specify output file name (default: `ark_output.txt`) |
| `--scan-buffer <byte>`                 | `-b`            | Line scan buffer size (default: `10M`)               |
| `--output-format <'txt'\|'md'>`        | `-o`            | Format of the output file (default: `txt`)           |
| `--additionally-ignorerule <filepath>` | `-a`            | Additional `.gitignore`-like rules                   |
| `--with-line-number <'on'\|'off'>`     | `-n`            | Show line numbers (default: `on`)                    |
| `--ignore-dotfile <'on'\|'off'>`       | `-d`            | Ignore dotfiles (default: `on`)                      |
| `--pattern-regex <regexp>`             | `-x`            | File match pattern                                   |
| `--include-ext <ext>`                  | `-i`            | Include file extensions (comma separated)            |
| `--exclude-dir-regex <regexp>`         | `-g`            | Exclude directories matching regex                   |
| `--exclude-file-regex <regexp>`        | `-G`            | Exclude files matching regex                         |
| `--exclude-ext <ext>`                  | `-e`            | Exclude file extensions (comma separated)            |
| `--exclude-dir <dir>`                  | `-E`            | Exclude specific directory names                     |
| `--skip-non-utf8`                      | `-s`            | Skip files that are not UTF-8 encoded                |

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

### Markdown (`--output-format md`)

```````
# Project Tree

```
example\_project
├── main.go
└── sub
└── sub.txt
```

---

# File: sub/sub.txt
```txt
hello world
```

```````
---

## 🧩 Integrations

- 🐚 Shell completions for **bash** and **zsh** included!
- 🔧 Easily embeddable in scripts, CI pipelines, or documentation generators

```sh
source completions/ark-completion.sh  # bash or zsh
````

---

## 📎 See Also

* Project URL: [https://github.com/magicdrive/ark](https://github.com/magicdrive/ark)
* `ark` documentation: [README.md](https://github.com/magicdrive/ark/README.md)

