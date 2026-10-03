# Ark

> ディレクトリ・リポジトリのテキスト出力ツール — コードインテリジェンス MCP ツール付き

**ark** はディレクトリを再帰的にスキャンし、ツリー構造とファイル内容を人間が読みやすい形式で出力します。また Tree-sitter によるシンボル抽出や MCP (Model Context Protocol) サーバー機能も備えています。以下のような用途に最適です：

* 📚 コードベースを LLM に共有する
* 🧪 静的解析パイプライン
* 🗂️ ソースツリーのスナップショット
* 🔍 **コードインテリジェンス** — シンボル抽出・定義ジャンプ
* 🛰️ **MCP サーバー** — AI エージェントにコードベースのコンテキストを提供

**プレーンテキスト**・**Markdown**・**XML**・**arklite** 形式の出力に対応し、UTF-8 の完全サポート（スキップオプションあり）、豊富なフィルタリング機能、**Tree-sitter** によるコード解析を提供します。

---

## 🚀 Quick Start

### 1. Install

```bash
go install github.com/magicdrive/ark@latest
```

Homebrew を使う場合:

```bash
brew install magicdrive/tap/ark
```

または [Releases](https://github.com/magicdrive/ark/releases) からビルド済みバイナリをダウンロードできます。

---

### 2. Generate a codebase dump

```bash
ark <ディレクトリ名>    # カレントディレクトリに ark-output.txt を生成
```

---

### 3. Set up Ark MCP for Claude Code

プロジェクトルートで一度だけ実行します:

```bash
cd /your/project
ark setup --name my-project
```

このコマンド一つで:
- `.mcp.json` を書き込み — Ark MCP サーバーを登録
- `.claude/commands/my-project.md` をインストール — `/my-project` をスラッシュコマンドとして有効化

その後 **Claude Code を再起動** して、プロンプトに従い MCP サーバーを承認してください。

準備ができたら以下が使えます:
- **`/my-project`** — Ark MCP ツールでコードベースを探索するスラッシュコマンド
- **MCP ツール直接呼び出し** — `mcp__ark__find_symbol`、`mcp__ark__get_symbols` など

```
/my-project         ← 19 種類の Ark MCP ツールを持つ探索アシスタントを起動
```

> **Tip:** プロジェクトルートに `CLAUDE.md` を置くと、Claude Code が自動的に Ark MCP ツールを使うよう誘導できます。すぐ使えるテンプレートを [`misc/CLAUDE.md.example`](misc/CLAUDE.md.example) に用意しています。

---

## 🧰 Basic Usage

```text
ark [オプション] <ディレクトリ>
ark setup [オプション]
ark mcp-server [オプション]
ark mcp-init [オプション]
ark syntax <ファイル> [オプション]
ark symbol <ファイル> [オプション]
ark skill [オプション]
```

---

## 📂 Sub‑commands

| Command | Description |
|---------|-------------|
| `setup` | ワンステップセットアップ: MCP 設定 + Claude Code スラッシュコマンドの生成 |
| `mcp-server` | Ark を MCP サーバーとして起動 (stdio または HTTP) |
| `mcp-init` | `.mcp.json` に Ark MCP 設定を追加 |
| `syntax` | Tree-sitter を使ってファイルを解析し AST を出力 |
| `symbol` | ファイルからシンボル（関数・型など）を抽出 |
| `skill` | Claude Code / OpenAI 向けの Ark MCP スキルを生成 |

---

## ⚙️ General Options

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--help` | `-h` | ヘルプを表示して終了 | – |
| `--version` | `-v` | バージョンを表示 | – |
| `--output-filename <file>` | `-o` | 出力ファイル名 | `ark-output.txt` |
| `--scan-buffer <size>` | `-b` | 読み込みバッファサイズ (`10M`, `500K` など) | `10M` |
| `--output-format <fmt>` | `-f` | `txt`, `md`, `xml`, `arklite` | `txt` |
| `--mask-secrets <on/off>` | `-m` | シークレットを検出してマスク | `on` |
| `--allow-gitignore <on/off>` | `-a` | `.gitignore` ルールを適用 | `on` |
| `--additionally-ignorerule <file>` | `-A` | 追加の無視ルールファイル | – |
| `--with-line-number <on/off>` | `-n` | 行番号を付与 | `on` |
| `--ignore-dotfile <on/off>` | `-d` | ドットファイルをスキップ | `off` |
| `--pattern-regex <regexp>` | `-x` | 正規表現にマッチするパスのみ含める | – |
| `--include-ext <exts>` | `-i` | 拡張子でフィルタ（例: `go,ts,html`） | – |
| `--exclude-dir-regex <regexp>` | `-g` | 正規表現にマッチするディレクトリを除外 | – |
| `--exclude-file-regex <regexp>` | `-G` | 正規表現にマッチするファイルを除外 | – |
| `--exclude-ext <exts>` | `-e` | 拡張子で除外 | – |
| `--exclude-dir <names>` | `-E` | ディレクトリ名で除外 | – |
| `--compless` | `-c` | **arklite** 形式で圧縮出力 | – |
| `--skip-non-utf8` | `-s` | 非 UTF-8 ファイルを無視 | – |
| `--silent` | `-S` | ログ・進捗表示を抑制 | – |
| `--delete-comments` | `-D` | 言語に応じてコメントを除去 | – |

---

## ⚡ setup — One-step Claude Code Setup

`ark setup` はあらゆるプロジェクトへの Ark 統合を最速で行う方法です。`mcp-init` と `skill` を 1 コマンドで完結させます:

```bash
cd /your/project
ark setup --name my-project
```

以下の 3 つを一度に実行します:
1. `.mcp.json` を書き込み — プロジェクトの Ark MCP サーバーを登録
2. `skills/my-project/` を生成 — スキルドキュメント一式を作成
3. `.claude/commands/my-project.md` をインストール — Claude Code スラッシュコマンドとして有効化

その後 Claude Code を再起動して MCP サーバーを承認すれば、すぐに使えます。

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--name <name>` | `-n` | プロジェクト名（スキルとスラッシュコマンドに使用） | ディレクトリ名 |
| `--ark-path <path>` | `-p` | ark バイナリのパス | 自動検出 |
| `--root <dir>` | `-r` | 提供するルートディレクトリ | `$PWD` |
| `--global` | `-g` | MCP 設定を `~/.claude/settings.json` に書き込む | `.mcp.json` |
| `--force` | `-f` | 既存エントリを上書き | – |

---

## 🔧 mcp‑init Options

`ark mcp-init` はカレントプロジェクトの `.mcp.json` に ark MCP サーバーエントリを追加（または更新）します。手動設定なしで Claude Code の ark ツールを使えるようになります。

```bash
# カレントプロジェクトに Ark MCP を追加
ark mcp-init

# グローバルな Claude Code 設定に追加 (~/.claude/settings.json)
ark mcp-init --global

# ルートディレクトリを指定
ark mcp-init --root /path/to/project

# 既存エントリを上書き
ark mcp-init --force
```

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--ark-path <path>` | `-p` | ark バイナリのパス | 自動検出 |
| `--root <dir>` | `-r` | 提供するルートディレクトリ | `$PWD` |
| `--name <name>` | `-n` | MCP サーバー名 | `ark` |
| `--global` | `-g` | `~/.claude/settings.json` に書き込む | `.mcp.json`（プロジェクトローカル） |
| `--force` | `-f` | 既存エントリを上書き | – |

---

## 🛰  mcp‑server Options

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `--root <dir>` | `-r` | 提供するディレクトリのルート | `$PWD` |
| `--type <stdio\|http>` | `-t` | サーバータイプ | `stdio` |
| `--http-port <port>` | `-p` | HTTP リスンポート | `8522` |
| `--scan-buffer <size>` | `-b` | 読み込みバッファサイズ | `10M` |
| `--mask-secrets <on/off>` | `-m` | シークレットを検出してマスク | `on` |
| `--allow-gitignore <on/off>` | `-a` | `.gitignore` ルールを適用 | `on` |
| `--additionally-ignorerule <file>` | `-A` | 追加の無視ルールファイル | – |
| `--ignore-dotfile <on/off>` | `-d` | ドットファイルをスキップ | `off` |
| `--pattern-regex <regexp>` | `-x` | パスフィルタ正規表現 | – |
| `--include-ext <exts>` | `-i` | 拡張子フィルタ | – |
| `--exclude-dir-regex <regexp>` | `-g` | ディレクトリ除外正規表現 | – |
| `--exclude-file-regex <regexp>` | `-G` | ファイル除外正規表現 | – |
| `--exclude-ext <exts>` | `-e` | 拡張子で除外 | – |
| `--exclude-dir <names>` | `-E` | ディレクトリ名で除外 | – |
| `--skip-non-utf8` | `-s` | 非 UTF-8 ファイルを無視 | – |
| `--delete-comments` | `-D` | コメントを除去 | – |
| `--no-cache` | – | 永続インデックスキャッシュを無効化 | – |

---

## 🔍 syntax Options

| Option | Description | Default |
|--------|-------------|---------|
| `--lang <language>` | 言語指定 (go, typescript, tsx, javascript, python, php) | 自動検出 |
| `--format <text\|json>` | 出力フォーマット | `text` |
| `-h, --help` | ヘルプを表示 | – |

```bash
ark syntax main.go                  # Go ファイルを解析
ark syntax app.ts --format json     # TypeScript を解析して JSON 出力
ark syntax script.py --lang python
```

---

## 🏷️ symbol Options

| Option | Description | Default |
|--------|-------------|---------|
| `--lang <language>` | 言語指定 (go, typescript, tsx, javascript, python, php) | 自動検出 |
| `--format <text\|json>` | 出力フォーマット | `text` |
| `-h, --help` | ヘルプを表示 | – |

```bash
ark symbol main.go                  # Go ファイルからシンボルを抽出
ark symbol app.ts --format json     # TypeScript のシンボルを JSON 出力
ark symbol script.py --lang python
```

---

## 🎯 skill Command

### Subcommands

| Subcommand | Description |
|------------|-------------|
| `skill` | 自動モード — 既存スキルを検出して適切なタイプを生成 |
| `skill init` | Repository Skill を生成（リポジトリ固有の完全なスキル） |
| `skill add-explorer` | 既存スキルの補助として Explorer Skill を追加 |
| `skill update` | Ark 管理スキルを更新（ユーザーファイルは保持） |
| `skill inspect` | 検出されたスキルとリポジトリ解析を表示 |

### Options

| Option | Description | Default |
|--------|-------------|---------|
| `--name <name>` | スキル名 | 自動決定 |
| `--output <dirname>` | 出力ディレクトリ | `skills/<name>` |
| `--archive` | ZIP アーカイブを作成 | – |
| `--force` | 既存スキルを上書き | – |
| `-h, --help` | ヘルプを表示 | – |

### Update Options

| Option | Description |
|--------|-------------|
| `--force` | ユーザー変更があっても強制更新 |
| `--dry-run` | 変更せずに更新内容のプレビューを表示 |

```bash
ark skill                               # 自動モード
ark skill init                          # Repository Skill を生成
ark skill add-explorer                  # Explorer Skill を追加
ark skill update                        # Ark 管理スキルを更新
ark skill update --dry-run              # 更新プレビュー
ark skill inspect                       # 検出されたスキルを表示
```

### Skill Types

**Repository Skill** (`skill init`): プロジェクト固有の完全なスキル
- go.mod、package.json、Makefile などから検出したビルド・テストコマンド
- プロジェクト言語の解析結果
- コーディング規約のリファレンスファイル

**Explorer Skill** (`skill add-explorer`): コードナビゲーション用の軽量スキル
- Ark MCP ツールの使い方ガイド
- 既存プロジェクトスキルと併用可能

### Generated Files

スキルには安全な更新のための YAML フロントマターが含まれます:
- `SKILL.md` — `ark-managed: true` メタデータ付きのスキルドキュメント
- `agents/openai.yaml` — OpenAI/Cline エージェント設定
- `agents/claude-code.md` — Claude Code カスタムエージェント（`mcp__ark__*` ツール名を使用）
- `references/conventions.md` — プロジェクト規約（Repository Skill のみ）

`agents/claude-code.md` は 19 種類の Ark MCP ツールを使用する Claude Code スラッシュコマンドとして `.claude/commands/` にも自動インストールされます。

---

## 📝 Arguments

| Argument | Description |
|----------|-------------|
| `<dirname>` | スキャン対象のディレクトリ |
| `<byte-string>` | サイズ指定文字列 (`10M`, `100K` など) |
| `<extension>` | ファイル拡張子 (`go`, `ts`, `html`) |
| `<regexp>` | Go の `regexp` 構文パターン |

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

Arklite は LLM のトークン効率を高めるために設計された、ファイルごとに 1 行で表現するコンパクト形式です:

1. 自然言語ヘッダー（プロジェクト名 + パス）
2. JSON ディレクトリツリー
3. ファイルダンプ（`@path` + 改行を `␤` で表現したコンテンツ）

---

## 🗂 Example `.arkignore`

```gitignore
# VCS
.git/
.hg/
.svn/

# IDE / エディタ
.idea/
.vscode/
*.code-workspace
*.sublime-*
```

---

## 🧩 Shell Completions

```sh
# Bash / Zsh
source completions/ark-completion.sh
# Fish
funcsave ark
```

---

## ✨ Why Ark?

### 🎯 Symbol-First Code Exploration

ファイル全体をダンプする代わりに、**必要なものだけ抽出**できます:

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

**メリット:**
- 📉 **トークン削減** — ファイル全体を読む必要がない
- 🎯 **精度向上** — 目的の定義に直接ジャンプ
- 🔍 **発見性** — `[exported]` マーカーで API の公開面が一目でわかる

### 🚀 Pure Go + Tree-sitter = Best of Both Worlds

- **CGO 不要** — どこでもクロスコンパイル可能、シングル静的バイナリ
- **本物の解析** — 正規表現ハックではなく、AST ベースのシンボル抽出
- **マルチ言語対応** — Go、TypeScript、TSX、JavaScript、Python、PHP（対応言語は増加中！）

### 🌐 言語サポート

Ark は実際にテスト済みの能力だけを表明します。レベルは積み上げ式です：
Parse → Symbols → References → Resolution → Graph → Context。

| 言語       | Parse | Symbols | References | Resolution | Graph | Context |
|------------|:-----:|:-------:|:----------:|:----------:|:-----:|:-------:|
| Go         |   ✓   |    ✓    |     ✓      |     ✓      |   ✓   |    ✓    |
| TypeScript |   ✓   |    ✓    |     ✓      |            |       |         |
| TSX        |   ✓   |    ✓    |     ✓      |            |       |         |
| JavaScript |   ✓   |    ✓    |     ✓      |            |       |         |
| Python     |   ✓   |    ✓    |     ✓      |            |       |         |
| PHP        |   ✓   |    ✓    |     ✓      |     ✓      |   ✓   |         |

チェックマークは、canonical Language Registry がその言語の **認定サポートレベル**
として表明している段階を示します（`get_language_support` が実行時に報告）。PHP の
`get_context` 経路は実装済みで専用の context-quality テストもありますが、namespace/
import・継承/trait メンバ解決など resolver の精度を意図的に保守的に保っているため、
PHP は **Graph** レベルで表明しています。過大表明を避けるため Context 列は未チェック
のままにしています。

#### PHP — 静的コードインテリジェンス

Ark は PHP の **シンボル**（namespace / class / interface / trait / enum /
function / 定数 / method / constructor / property / class 定数 / enum case /
promoted property）、**import**（plain / alias / grouped / function / const の
`use`）、**参照**（function / static / instance / `$this` 呼び出し、construction、
class 定数 read、型参照）、**typed relation**（`extends` / `implements` / trait
`use`）を静的に抽出し、typed symbol graph と agent 向け context を構築します。
動的・曖昧な構文については **不確実性をそのまま保持**します。

**既知の制限（設計上の意図）**：動的呼び出し・動的生成（`$obj->$m()`、`new $c()`）は
推測しません。変数レシーバの型推論は行いません。Composer / PSR-4 / autoload 解決は
ありません。`use` エイリアスや継承・trait メンバ解決は保守的（捏造せず honest な
`Candidate` / `Unresolved`）です。framework（Laravel/Symfony 等）セマンティクスは
扱いません。Ark は **純粋な静的解析**のみを行い、リポジトリのコード・Composer・PHP
ツールを一切実行しません。

### 🤖 LLM-Optimized Workflow

Ark は **19 種類の MCP ツール**でコードインテリジェンススタック全体をカバーします:

| ツール | 説明 |
|--------|------|
| `get_directory_tree` | プロジェクトレイアウトを把握 |
| `get_symbols` | ファイル内の関数・型を一覧表示 |
| `find_symbol` | リポジトリ全体からシンボルを検索 |
| `get_symbol` | 特定の関数・型のソースコードを取得 |
| `search_in_files` | 全文検索・正規表現検索 |
| `list_files` | フィルタ対応のファイル一覧 |
| `get_file_content` | ファイル全体を読み込む |
| `get_file_info` | ファイルのメタデータ（サイズ・行数・言語） |
| `get_project_stats` | 言語別ファイル数などの統計 |
| `get_files_arklite` | 複数ファイルを圧縮形式で取得 |
| `get_context` | シンボルに対してトークン予算付きのコンテキストを取得（ターゲットは常に含まれる） |
| `find_references` | シンボルの全参照箇所を検索 |
| `get_relations` | ファイル間のインポート・依存関係を探索 |
| `get_callers` | あるシンボルを呼び出しているシンボルを検索 |
| `get_callees` | あるシンボルが呼び出しているシンボルを検索 |
| `get_repository_map` | LLM 向けのコンパクトなリポジトリマップ |
| `analyze_change_impact` | シンボル変更の影響範囲を推定 |
| `search_code` | 種別・名前・型使用などによる構造検索 |
| `get_language_support` | 対応言語とサポートレベルの一覧 |

コアとなる階層的探索パターン:

```
get_directory_tree   →   プロジェクト構造を把握
        ↓
    find_symbol      →   「Foo はどこ？」を探す
        ↓
    get_symbols      →   ファイルの内容を一覧表示
        ↓
    get_symbol       →   ソースコードを正確に抽出
        ↓
    get_context      →   安全な修正のためのトークン予算付きコンテキスト
```

このアプローチにより、ファイル全体を読む場合と比べて**トークン使用量を大幅に削減**しながら、完全なコンテキスト認識を維持できます。

### 📦 Instant Skill Generation

```bash
$ ark skill --name my-project-explorer
✅ Created my-project-explorer/SKILL.md
✅ Created my-project-explorer/agents/openai.yaml
```

ChatGPT や Cline があなたのコードベースを効率的に探索する方法を学ぶために必要なファイル一式を、1 コマンドで生成できます。

---

## 📎 See Also

* プロジェクトホーム — <https://github.com/magicdrive/ark>

## Author

© 2025 - 2026 Hiroshi IKEGAMI

## License

[MIT License](LICENSE) のもとで公開されています。
