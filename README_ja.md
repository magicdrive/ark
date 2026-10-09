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

Homebrew を使う場合:

```bash
brew install magicdrive/tap/ark
```

または [Releases](https://github.com/magicdrive/ark/releases) からビルド済みバイナリをダウンロードできます。

Go ツールチェーンを使う場合は `main` からインストールします:

```bash
go install github.com/magicdrive/ark@main
```

> `go install github.com/magicdrive/ark@latest` では現在のリリースは**インストールされません**。モジュールパスに
> メジャーバージョンの接尾辞がないため、Go のモジュールプロキシは `@latest` を旧 v1 系に解決し、`@v5.x` の
> タグは `go install` で指定できません。`@main` からインストールしたバイナリの `ark --version` は疑似バージョン
> （`v0.0.0-<日付>-<コミット>`）を表示します。

---

### 2. Generate a codebase dump

```bash
ark <ディレクトリ名>    # カレントディレクトリに ark-output.txt を生成
```

---

### 3. 使っている coding agent 向けに Ark をセットアップ

coding agent を選び、プロジェクトルートで `ark setup` を一度だけ実行します:

```bash
cd /your/project

ark setup claude   # Claude Code
# または
ark setup cursor   # Cursor
# または
ark setup codex    # Codex
# または
ark setup cline    # Cline CLI
# または
ark setup copilot-vscode  # VS Code の GitHub Copilot、Chat / Agent mode（このリポジトリ）
# または
ark setup copilot-cli  # GitHub Copilot CLI（このリポジトリ）
```

これはクライアントの設定に Ark MCP サーバーを登録します。**Claude Code** の場合は
加えてプロジェクト skill / `/<name>` スラッシュコマンドも生成します。

その後 **クライアントを再起動（またはリロード）** し、プロンプトに従い Ark MCP サーバーを承認してください。

#### Agent 対応マトリクス

Ark が `setup` 経路を実際にテストしているクライアントのみを Supported として掲載しています。

| クライアント | セットアップコマンド | 書き込む設定 |
|--------------|---------------------|--------------|
| Claude Code  | `ark setup claude`  | `.mcp.json`（project）/ `~/.claude/settings.json`（`--global`） |
| Cursor       | `ark setup cursor`  | `.cursor/mcp.json`（project）/ `~/.cursor/mcp.json`（`--global`） |
| Codex        | `ark setup codex`   | Codex のユーザー設定（公式 `codex` CLI 経由） |
| Cline        | `ark setup cline`   | `~/.cline/mcp.json`（Cline **CLI**。下記注記参照） |
| GitHub Copilot (VS Code) | `ark setup copilot-vscode` | `.vscode/mcp.json`（プロジェクトのみ。下記注記参照） |
| GitHub Copilot CLI | `ark setup copilot-cli` | `.github/mcp.json`（プロジェクトのみ。下記注記参照） |

> **Cline の対象範囲:** v4.1 がサポートするのは **Cline CLI** の設定 `~/.cline/mcp.json` のみです。
> Cline の VS Code / Cursor / Windsurf 拡張が使う MCP 設定
> (`.../globalStorage/.../cline_mcp_settings.json`) は **対象外** です — Ark は OS/エディタ固有の
> ストレージパスを探索しません。IDE 拡張向けは手動で設定してください。

> **Copilot (VS Code) の対象範囲:** `ark setup copilot-vscode` が設定するのは、現在のリポジトリの
> **VS Code 上の GitHub Copilot Chat / Agent mode** のみです（`.vscode/mcp.json`、トップレベルは
> `servers`）。`--global` は未対応です（VS Code のユーザー設定パスが公式に文書化されていないため）。
> Copilot CLI、GitHub ホストの Copilot エージェント、GitHub のリポジトリ設定は**設定しません**。
> Claude Code が使うポータブルな `.mcp.json` にも一切触れません。Copilot CLI の設定は
> 現時点では `ark setup copilot-vscode` の管理対象外です（下記の `copilot-cli` が担当します）。
>
> 生成される `--root` は**リポジトリの絶対パス**です（VS Code の公式ドキュメントは、
> `.vscode/mcp.json` の `args` でのワークスペース変数の展開も、multi-root ワークスペースでの意味も
> 保証していません）。そのためこのファイルはマシン固有です。そのままコミット・共有せず、各開発者が
> ローカルで `ark setup copilot-vscode` を実行してください。`command` は、`--ark-path` を指定しない限り
> `ark`（`PATH` で解決）です。

> **Copilot CLI の対象範囲:** `ark setup copilot-cli`（`copilot-vscode` とは別の client）は、現在のリポジトリの
> **GitHub Copilot CLI** のみを設定します: `.github/mcp.json`（`mcpServers`、`local` entry）。
> `--global` は未対応で（`~/.copilot/mcp-config.json` は管理しません）、`copilot mcp` コマンドも
> 使いません。Copilot CLI は、同名のサーバーについて `.mcp.json` を `.github/mcp.json` より
> **優先**します。そのため `<root>/.mcp.json` に `mcpServers.ark` が既にある場合、setup は
> **拒否**されます（`--force` でも同様で、何も変更されません）。より近い入れ子の `.mcp.json` に
> `ark` がある場合も優先されますが、Ark は setup 時にそれを検出できません。`copilot-cli` の**後**に
> `ark setup claude` を実行した場合も、root の `.mcp.json` に作られる `ark` entry が優先されます。
> `--root` は上記と同様に絶対パスなので、ファイルをコミットせず、各開発者がローカルで
> `ark setup copilot-cli` を実行してください。

セットアップ後は MCP ツールを直接呼び出せます（`mcp__ark__find_symbol`、
`mcp__ark__get_symbols` など）。Claude Code では生成された `/<name>` スラッシュコマンドも使えます。

> **`--force` は「Ark のエントリを置換する」であって「あなたの設定を上書きする」ではありません。**
> `--force` が置き換えるのは Ark 自身の MCP エントリだけです。無関係な MCP サーバーの削除、
> 未知フィールドの破棄、壊れた設定の修復、他のクライアント設定の上書きは一切行いません。

> **Tip:** `setup` は Ark MCP を agent に接続し、`instruction` は agent に Ark MCP の効果的な使い方を伝えます。
> Claude Code に伝えるには、`ark instruction claude` を実行し、出力をプロジェクトの `CLAUDE.md` に追加してください
> （[`ark instruction`](#-instruction--agent-向け利用指示) 参照。`codex`、`cursor`、`cline`、`copilot-vscode`、
> `copilot-cli` にも対応しています）。Claude 向けのすぐ使えるテンプレートは
> [`misc/CLAUDE.md.example`](misc/CLAUDE.md.example) にあります。

---

## 🧰 Basic Usage

```text
ark [オプション] <ディレクトリ>
ark setup <client> [オプション]
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
| `setup <client>` | サポートする coding agent 向けに Ark を設定（claude, cursor, codex, cline, copilot-vscode, copilot-cli） |
| `mcp-server` | Ark を MCP サーバーとして起動 (stdio または HTTP) |
| `mcp-init` | `.mcp.json` に Ark MCP 設定を追加 |
| `syntax` | Tree-sitter を使ってファイルを解析し AST を出力 |
| `symbol` | ファイルからシンボル（関数・型など）を抽出 |
| `skill` | Claude Code / OpenAI 向けの Ark MCP スキルを生成 |
| `instruction <target>` | agent 向けの Ark MCP 利用指示を出力（target: `claude`、`codex`、`cursor`、`cline`、`copilot-vscode`、`copilot-cli`） |

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

## ⚡ setup — ワンコマンドの Agent セットアップ

`ark setup <client>` はあらゆるプロジェクトへの Ark 統合を最速で行う方法です。
対象クライアントの設定に Ark MCP サーバーを登録します（Claude Code ではさらに
プロジェクト skill / スラッシュコマンドを生成します）。

```bash
cd /your/project
ark setup cursor
ark setup claude --name my-project   # --name は Claude の skill 名のみに影響
ark setup codex --global
```

| Option | Alias | Description | Default |
|--------|-------|-------------|---------|
| `<client>` | – | 対象 agent: `claude`, `cursor`, `codex`, `cline`, `copilot-vscode`（VS Code の GitHub Copilot）または `copilot-cli`（GitHub Copilot CLI）。どちらもプロジェクトのみ | – |
| `--name <name>` | `-n` | Claude の skill/スラッシュコマンド名（**Claude のみ**） | ディレクトリ名 |
| `--ark-path <path>` | `-p` | `ark` バイナリのパス（セットアップ時に検証） | 自動検出（`PATH` 上の `ark`） |
| `--root <dir>` | `-r` | 提供するリポジトリルート | `$PWD` |
| `--global` | `-g` | クライアントの**ユーザーレベル** MCP 設定を使用 | project スコープ |
| `--force` | `-f` | 競合時に **Ark 所有の**エントリを置換 | – |

### 安全性の契約（Safety contract）

`ark setup` は *退屈なほど安全にインストールできる* よう設計されています:

- **冪等（Idempotent）** — 何度実行しても、設定済みなら以降は変更しません
  （等価な設定なら no-op。ファイルの書き直しすら行いません）。
- **競合安全（Conflict-safe）** — 既存の Ark エントリが要求と異なる場合はセットアップを失敗させ、
  `--force` での再実行を促します。黙って上書きしません。
- **保持（Preserving）** — 無関係な MCP サーバーや未知フィールドは常に保持します。
- **修復しない** — 壊れた/パース不能な設定は報告するだけで、上書きしません（`--force` でも）。
- **アトミック（Atomic）** — 設定ファイルは一時ファイル + rename で置換し、
  同時変更（lost-update）検出と write 後の検証（verify-after-write）を行います。

`ark setup`（クライアント指定なし）は v4.x の間 `ark setup claude` の **非推奨**エイリアスとして
維持され、警告を表示します。

### 手動設定（Manual configuration）

`ark setup` を実行できない場合（管理対象マシン、読み取り専用設定、特殊なインストール）は、
Ark MCP サーバーを手動で追加してください。コマンドは常に `ark mcp-server --root <path>` です。

**Claude Code** — `.mcp.json`（project）または `~/.claude/settings.json`（global）:

```json
{
  "mcpServers": {
    "ark": { "type": "stdio", "command": "ark",
             "args": ["mcp-server", "--root", "${CLAUDE_PROJECT_DIR:-.}/"], "env": {} }
  }
}
```

Claude Code は `CLAUDE_PROJECT_DIR` をサーバーの環境にしか設定しないため、この引数は `./`
に展開され、Ark は Claude Code が起動したディレクトリ（プロジェクト）を基準に解決します。
別のディレクトリを固定する場合やグローバル設定では、絶対パスの `--root` を使ってください。
`CLAUDE_PROJECT_DIR` が提供ルートと異なる場合、Ark は警告をログに出します。

**Cursor** — `.cursor/mcp.json`（project）または `~/.cursor/mcp.json`（global）:

```json
{
  "mcpServers": {
    "ark": { "command": "ark", "args": ["mcp-server", "--root", "/abs/path/to/repo"], "env": {} }
  }
}
```

**Cline CLI** — `~/.cline/mcp.json`: Cursor と同じ形式。

**GitHub Copilot (VS Code)** — `.vscode/mcp.json`: サーバーは `servers` の下に置き、`"type": "stdio"` を付けます:

```json
{
  "servers": {
    "ark": { "type": "stdio", "command": "ark", "args": ["mcp-server", "--root", "/abs/path/to/repo"], "env": {} }
  }
}
```

**Codex** — 公式 CLI で登録: `codex mcp add ark -- ark mcp-server --root /abs/path/to/repo`。

### トラブルシューティング

| 症状 | 対処 |
|------|------|
| `ark command ... not found on PATH` | `ark` を `PATH` に通すか、`--ark-path /full/path/to/ark` を指定。 |
| クライアントが Ark を検出しない | クライアントを完全に再起動/リロードして MCP 設定を再読込。 |
| `found a different Ark MCP configuration` | 要求が既存と異なる。`--force` で再実行。 |
| `cannot parse <file>` | 設定が壊れている。手動で修正（Ark は壊れたファイルに触れません）。 |
| `the Codex CLI (codex) was not found` | Codex（`codex`）をインストールするか手動設定（上記参照）。 |
| `root directory does not exist` | 存在する `--root` を指定（Ark は事前に検証します）。 |
| `path ... is outside the server root` | 提供ルート内のパス（ルート相対または絶対パス）を指定。メッセージ中のルートを確認してください。相対の `--root` はサーバー起動時のディレクトリ基準です。 |
| Permission denied | 設定ファイル/ディレクトリが書き込み不可。権限を修正するか `--global` を使用。 |

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
| `--root <dir>` | `-r` | 提供するディレクトリのルート。相対パスは起動時のディレクトリを基準に解決され、存在しない・ディレクトリでないルートではサーバーは起動しない | `$PWD` |
| `--type <stdio\|http>` | `-t` | トランスポート（`stdio`、または `--http-port` 上の `http`。エンドポイントは `/mcp`） | `stdio` |
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
| `--no-cache` | – | 永続的な抽出キャッシュを無効化（無効化しない場合は `<root>/.ark/index` に保存。`.gitignore` に `.ark/` を追加してください） | – |

---

## 🔍 syntax Options

| Option | Description | Default |
|--------|-------------|---------|
| `--lang <language>` | 言語指定 (go, typescript, tsx, javascript, python, php, terraform) | 自動検出 |
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
| `--lang <language>` | 言語指定 (go, typescript, tsx, javascript, python, php, terraform) | 自動検出 |
| `--format <text\|json>` | 出力フォーマット | `text` |
| `-h, --help` | ヘルプを表示 | – |

```bash
ark symbol main.go                  # Go ファイルからシンボルを抽出
ark symbol app.ts --format json     # TypeScript のシンボルを JSON 出力
ark symbol script.py --lang python
```

---

## 📜 instruction — agent 向け利用指示

`ark instruction <target>` は、coding agent が Ark MCP を効果的に使うための短い Markdown 指示（どの質問にどの
ツールを選ぶか、ファイル全体の読み込みが適切な場面、不確かな結果の扱い）を出力します。指示の本文はどの target
でも同じで、違うのは見せ方だけです。`claude` だけは、tool 名を Claude Code が実際に見せる形
（`mcp__ark__<tool>`）へ変換します。これは、model に見える MCP tool 名が公式に文書化されている唯一の agent
だからです。他の target は、指示本文の素の tool 名をそのまま出力します。存在しない命名規則を Ark が勝手に
作ることはありません。

| Target | Agent / surface | 推奨する貼り付け先 |
|--------|------------------|------------------|
| `claude` | Claude Code | `CLAUDE.md` |
| `codex` | OpenAI Codex（CLI / IDE拡張 / cloud） | `AGENTS.md` |
| `cursor` | Cursor | `AGENTS.md` |
| `cline` | Cline | `.clinerules/ark.md` |
| `copilot-vscode` | GitHub Copilot in VS Code | `.github/copilot-instructions.md` |
| `copilot-cli` | GitHub Copilot CLI | `.github/copilot-instructions.md` |

「推奨する貼り付け先」は、各 agent の公式ドキュメントがrepository-localなinstructionを探す場所です。
その agent が対応する唯一の仕組みという意味ではなく、Ark がそこへ書き込むわけでもありません。

```bash
ark instruction claude                      # 標準出力へ
ark instruction codex > ark-instruction.md
ark instruction codex >> AGENTS.md          # 既存の Ark セクションと重複しないよう、先に AGENTS.md を確認
ark instruction cursor >> AGENTS.md
mkdir -p .clinerules && ark instruction cline > .clinerules/ark.md
ark instruction copilot-vscode >> .github/copilot-instructions.md
ark instruction copilot-cli >> .github/copilot-instructions.md
```

出力するだけです。Ark がこれらのファイルを作成・編集することはなく、標準出力には指示以外を出しません。
未対応の target（例えば `agents` や `copilot` — どちらも target ではありません。下記参照）は、対応 target
の一覧付きでエラーになります。`ark setup claude` も setup 後に同じ Claude 向け指示を表示します。
`ark skill`（再利用可能な skill / スラッシュコマンド生成）は別の機能です。

instruction fileは、agentのmodelに渡るcontextであり、強制力のあるpolicyの境界ではありません。他のprompt
テキストと同様に扱ってください。

`ark instruction <target>` と `ark setup <client>` は現在同じ6つの agent 名を使っていますが、別の目的を持つ
別々のregistryです。`setup` は Ark の MCP サーバーを client に接続し、`instruction` はその使い方を agent に
教えます。どちらか一方だけが増減することもあります。

**`agents` や `copilot` を target にしない理由:** `AGENTS.md` は複数の target が偶然共有している貼り付け先で
あり、agentの identity ではありません。実際 Claude Code は、`CLAUDE.md` があると `AGENTS.md` を確実には読み
ません。また `copilot` だけでは、同じファイルを読むが setup 上は別の surface である `copilot-vscode` と
`copilot-cli` のどちらかを特定できません。`ark instruction` は常に agent 名を指定し、ファイル形式では指定し
ません。

---

## 🎯 skill Command

Ark の skill は、**Ark MCP を効果的に使うための、タスク指向のガイダンス**を提供します: どの目的にどの
ツール（repository map、symbol context、graph relations、change impact、検索）を使うか、ツールの組み合わせ方、
探索をやめる判断、曖昧・不確かな結果の扱い。target先の agent に向けて同じ短い常設ガイダンスを出力する
[`ark instruction`](#-instruction--agent-向け利用指示) より詳しい内容ですが、どちらも同じ利用モデルを教えます。

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
- Ark MCP を使うためのタスク指向ガイダンス（目的 → ツール、不確かさの扱い）
- 既存プロジェクトスキルと併用可能

### Generated Files

スキルには安全な更新のための YAML フロントマターが含まれます:
- `SKILL.md` — `ark-managed: true` メタデータ付きのスキルドキュメント
- `agents/openai.yaml` — OpenAI/Cline エージェント設定
- `agents/claude-code.md` — Claude Code カスタムエージェント（`mcp__ark__*` ツール名を使用）
- `references/conventions.md` — プロジェクト規約（Repository Skill のみ）

`ark skill update` が更新するのは `SKILL.md` と `agents/openai.yaml` で、`agents/claude-code.md` とインストール済みスラッシュコマンドは、スキルの生成時に書き込まれます。

`agents/claude-code.md` は 20 種類の Ark MCP ツールを使用する Claude Code スラッシュコマンドとして `.claude/commands/` にも自動インストールされます。

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
# Bash / Zsh（両対応の単一スクリプト）
source misc/completions/ark-completion.sh
# Fish
mkdir -p ~/.config/fish/completions
cp misc/completions/fish/ark.fish ~/.config/fish/completions/
```

補完はサブコマンド、すべてのフラグ、有限値を取るフラグの値（`--lang`、`--format`、
`--type`、`on`/`off` など）、`ark setup <client>`（`claude` / `cursor` / `codex` /
`cline` / `copilot-vscode` / `copilot-cli`）を対象とします。シェル別の単体ファイルは `misc/completions/{bash,zsh,fish}/`
にあります。補完ファイルが CLI・setup client レジストリ・言語レジストリとずれると、
テスト（`internal/completion`）が失敗します。

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
| TypeScript |   ✓   |    ✓    |     ✓      |     ✓      |   ✓   |    ✓    |
| TSX        |   ✓   |    ✓    |     ✓      |     ✓      |   ✓   |    ✓    |
| JavaScript |   ✓   |    ✓    |     ✓      |            |       |         |
| Python     |   ✓   |    ✓    |     ✓      |            |       |         |
| PHP        |   ✓   |    ✓    |     ✓      |     ✓      |   ✓   |         |
| Terraform  |   ✓   |    ✓    |     ✓      |     ✓      |   ✓   |         |

チェックマークは、canonical Language Registry がその言語の **認定サポートレベル**
として表明している段階を示します（`get_language_support` が実行時に報告）。PHP の
`get_context` 経路は実装済みで専用の context-quality テストもありますが、関数・定数名や
framework の dispatch など resolver の精度を意図的に保守的に保っているため、
PHP は **Graph** レベルで表明しています。過大表明を避けるため Context 列は未チェック
のままにしています。Terraform の `get_context` は依存グラフ上で動作しますが、まだ
context-quality ベンチマークの対象ではないため、同じく **Graph** レベルで表明しています。

#### TypeScript / TSX — 静的コードインテリジェンス

Ark は **シンボル**（class / interface / type alias / enum / 関数 / `const` コンポーネント、
および class・interface の **メンバ** — constructor、instance / static / abstract メソッド、
プロパティ、コンストラクタ引数プロパティ、アロー関数フィールド。getter/setter の組は
1 つのプロパティ）を安定した containment 付きで、**module binding**（named / alias /
default / namespace / type-only の import）、**export テーブル**（ローカル export、別名、
default、named re-export、`export *`、`export * as ns`、barrel チェーン）、**参照**
（呼び出し、`this.m()`、static / namespace 経由の呼び出し、construction、型参照、JSX
コンポーネント）、**typed relation**（`extends` / `implements`）を静的に抽出し、typed
symbol graph と agent 向け context を構築します。

解決は根拠ベースで保守的です。リポジトリ内の相対 import（`./user`、`../domain/user`、
`./user.ts`、`./user/index`）は、alias と barrel チェーン（深さ・状態数に上限、循環安全）
を辿って **定義側のシンボル** に決定的に解決されます。メンバ呼び出しは、レシーバの型が
構造的に証明できる場合（`this`、明示的な型注釈、`const x = new T()`、型付きフィールド /
コンストラクタ引数プロパティ）のみ解決します。型注釈のないレシーバ（`repo.save()`）は、
リポジトリ内に `save` が 1 つしかなくても `Candidate` / `Unresolved` のままです。
intrinsic な JSX 要素（`<div />`）はリポジトリ参照になりません。

| | 状態 |
|---|---|
| **認定済み**（graph の adversarial fixture、recall 1.00 かつ false Exact・捏造 edge ゼロの context-quality scenario、MCP、cache、fuzz、決定性で検証） | 相対 import 解決、alias、default / namespace / type-only import、barrel、型が証明されたレシーバでのメンバ解決、`this` / static メンバ、`extends` / `implements`、JSX コンポーネント参照 |
| **意図的に未解決**（推測せず honest な `Candidate` / `Unresolved`） | 外部パッケージ（`zod`、`react`、`node:fs`）、path alias（`@/foo`、tsconfig `paths`）、型が証明できない変数レシーバ、継承メンバの探索、宣言マージ（interface + class の同名）、computed / 動的アクセス（`a[k]()`）、`.ts` と `.tsx` が両方ある場合の `.js` 付き specifier、名前のない default export |
| **未実装** | 戻り値型の伝播・型推論（`const u = repo.find()`）、制御フローによる narrowing、コンパイラ同等の overload 解決、`.d.ts` / `.mts` / `package.json` 解決、`tsconfig` の解釈、`namespace` 本体、enum メンバ、分割代入の宣言、framework セマンティクス（React / Next / Nest / Angular）、decorator / DI 推論 |

**module 解決はコンパイラ同等ではありません。** Ark は、サポート対象として明示した、
リポジトリ内・設定非依存の字句的な部分集合の範囲でのみ、決定的な優先度を付与します
（`./user` → `user.ts`、`user.tsx`、`user/index.ts`、`user/index.tsx` の順。`.js` / `.jsx`
による置換は意図的に順位付けせず、`.ts` と `.tsx` が両方ある場合は曖昧のままです）。
Ark は `tsconfig`・`moduleResolution`・`moduleSuffixes`・`package.json` を解釈しません。
これらの設定に依存して解決されるプロジェクトでは、Ark の字句的な部分集合と異なる解決に
なる場合があります。

同一 class 内の同名 static / instance メンバは 1 つのシンボルを共有し、離れた getter/setter
は最初の accessor を範囲とします。`import` / `export` を持たないファイルはスクリプト（グロー
バル）として扱い、従来の近接ルールのままです。Ark は **純粋な静的解析**のみを行い、
リポジトリのコード・Node・npm / yarn / pnpm / bun・`tsc`・`tsserver`・package script・
リポジトリ設定を一切実行しません。

#### PHP — 静的コードインテリジェンス

Ark は PHP の **シンボル**（namespace / class / interface / trait / enum /
function / 定数 / method / constructor / property / class 定数 / enum case /
promoted property）、**import**（plain / alias / grouped / function / const の
`use`）、**参照**（function / static / instance / `$this` 呼び出し、construction、
class 定数 read、型参照）、**typed relation**（`extends` / `implements` / trait
`use`）を静的に抽出し、typed symbol graph と agent 向け context を構築します。
動的・曖昧な構文については **不確実性をそのまま保持**します。

**既知の制限（設計上の意図）**：動的呼び出し・動的生成（`$obj->$m()`、`new $c()`）は
推測しません。レシーバの型は宣言（型付き引数、constructor injection された property）
からのみ得て、代入からの型推論は行いません。Composer / PSR-4 / autoload 解決は
ありません。class 名はファイル自身の `namespace` / `use` / `use … as` / 完全修飾
構文から確定し、リポジトリ内の宣言と完全一致で照合します（リポジトリに宣言のない
class、例えば vendor の class は `Unresolved` のままで、同名のリポジトリ内 class には
決して結び付けません。複数宣言されている名前は `Candidate` です）。継承・trait の
メンバ（`self::` / `static::` / `parent::` を含む）は、リポジトリ内で宣言された型だけを
たどって構造的に解決します（自身の宣言 → trait → 直近の親 → interface）。参加者が
リポジトリ外または曖昧なとき、trait adaptation（`insteadof` / `as`）がそのメンバを
指すとき、親の private メンバのときは、捏造せず honest な `Candidate` / `Unresolved`
で止まります。関数・定数名は保守的です。framework（Laravel/Symfony 等）セマンティクスは
扱いません。Ark は **純粋な静的解析**のみを行い、リポジトリのコード・Composer・PHP
ツールを一切実行しません。

#### Terraform — 静的な構成インテリジェンス

Ark は Terraform 構成（`.tf`）の宣言 — `resource` / `data` / `ephemeral` /
`module` / `variable` / `locals`（local ごとに 1 シンボル）/ `output` /
`provider`（`alias` 付き）/ `check`（スコープ付き `data` を含む）/ `terraform`
ブロック — と、それらの間の **依存関係** を静的に抽出します。依存関係は式中の静的な
アドレス（`aws_vpc.main.id`、`data.aws_ami.ubuntu.id`、`var.region`、`local.name`、
`module.net`、`resource.TYPE.NAME`。template、heredoc、条件式、関数呼び出し、`for`
式、splat、index、`dynamic` ブロック内を含む）、`depends_on`、`provider` /
`providers` メタ引数です。これらは `references` / `depends_on` の graph edge になり
（`get_callees`。`get_callers` では `referenced_by` / `depended_on_by`）、呼び出しとして
扱われることはありません。`.tfvars` の代入は、名前の示す variable への write として
記録します。HCL の解析には Ark の pure-Go runtime に含まれる Tree-sitter HCL grammar
を使います（CGO 不要、新規依存なし）。

**module のスコープはディレクトリです。** シンボル名は Terraform アドレス
（`aws_vpc.main`）、qualified name は module ディレクトリを前置したもの
（`modules/network/aws_vpc.main`）です。参照は、それが書かれた module の中だけで、
そのディレクトリの全 `.tf` ファイルを横断して解決します。アドレスの宣言が 1 つなら
`Exact`、2 つなら `Candidate`（edge なし）、そこに宣言がなければ — 別の module に
同じアドレスがあっても — `Unresolved` です。`module.NAME.OUTPUT` は、module の
`source` がローカルパス（`./`、`../`）のとき子 module の `output "OUTPUT"` に解決
します。registry・Git などリモート module の output は `outsideRepository` として報告
します。Terraform の名前は identity でのみ照合し、名前の類似で Terraform 同士や他言語の
シンボルと結び付けることはありません。名前は HCL の識別子規則に従い Unicode 文字
（`aws_vpc.日本`）も扱います。Terraform と同じく正規化はせず、書かれたとおりに比較します。

| | 状態 |
|---|---|
| **対応** | 上記ブロック、同一 module のファイル横断解決、ローカル module の output（`module.x["k"].out`、`module.x[*].out` を含む）、module の input 引数（呼び出しから子の `variable` への edge。共有 module の variable の変更は、それを渡すすべての呼び出しに届く）、明示的 `depends_on`、provider 構成と alias、check スコープの data source、override ファイル（`override.tf`、`*_override.tf`：参照は計上し、宣言はしない）、壊れたファイル（Tree-sitter の回復が及ぶ範囲で後続の宣言を回復し — 閉じていない括弧はファイル末尾まで飲み込むことがある — error 診断を出す） |
| **設計上 参照ではない** | `count.*`、`each.*`、`self.*`、`path.*`、`terraform.*`、`for` / template `for` の変数、`dynamic` の iterator、bare な object key、関数名（provider 定義関数を含む）、`lifecycle.ignore_changes`、variable の型制約 |
| **意図的に unresolved / outside** | リモート module の output（`outsideRepository`）、module に `provider "x"` ブロックがない場合の `provider = x`（暗黙・継承の構成）、`.tf.json` にだけ宣言されたアドレス、symlink されたディレクトリ経由でのみ到達する module、override ファイルが `source` を置き換える module 呼び出し（どちらが有効かは merge 順で決まるため `Candidate`）、variable から計算される source |
| **未実装** | module 引数から子への値の流れ（および呼び出しが省略した input）、`.tf.json` / `.tfvars.json`（JSON 構文）、汎用 `.hcl`（Packer・Nomad・Terragrunt・Terraform test ファイルは Terraform として索引しない）、`moved` / `import` / `removed` ブロック（観測しない）、resource type の暗黙の default provider、`.tfvars` がどの module に渡るか、`terraform_remote_state` などの state 間データ、Terraform Cloud workspace |

Ark は **純粋な静的解析**のみを行い、`terraform` を実行せず、module や provider を
ダウンロードせず、`.terraform/` を読みません。

### 🤖 LLM-Optimized Workflow

Ark は **21 種類の MCP ツール**でコードインテリジェンススタック全体をカバーします:

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
| `search_context` | 名前の一部（`auth`、`getUser`）からシンボルを発見。順位付き候補を最大 `limit` 件（既定 5）返し、上位 `contextLimit` 件（既定 1）にコンテキストを付ける。すべて応答全体のトークン予算内。順位は名前の近さであり正しさの保証ではない |
| `find_references` | シンボルの全参照箇所を検索 |
| `get_relations` | ファイル間のインポート・依存関係を探索 |
| `get_callers` | あるシンボルを呼び出しているシンボルを検索 |
| `get_callees` | あるシンボルが呼び出しているシンボルを検索 |
| `get_repository_map` | LLM 向けのコンパクトなリポジトリマップ |
| `analyze_change_impact` | シンボル変更の影響範囲を推定 |
| `search_code` | 種別・名前・型使用などによる構造検索 |
| `get_language_support` | 対応言語とサポートレベルの一覧 |
| `get_diagnostics` | Ark が完全には解析できなかったファイル（parser が受理しなかった範囲、読めないファイル）。絞り込み・ページング対応 |

#### Ark の回答の読み方：診断・unresolved・完全性

- **診断（diagnostic）** は、Ark がファイルの一部（`parse_error`：parser が受理しなかった範囲で、書かれたとおりには解析されない。grammar が正しいコードを受理しないこともあるため、ソース自体は正しい可能性がある）またはファイル全体（読み込み不可・provider の失敗：ファイルはスキップ）を解析できなかったことを示します。Ark の解析についての情報であり、コンパイラの判定ではありません。`get_diagnostics` で一覧でき、graph 系ツール（`get_callers`、`get_callees`、`get_relations`、`get_context`、`search_context`、`analyze_change_impact`）は **index に診断があるときだけ** 診断の要約（`indexDiagnostics`。impact の JSON では `index_diagnostics`、テキスト出力では末尾の 1 行）を付けます。そのとき「caller なし」という答えは、それらのファイル内のコードを取りこぼしている可能性があります。
- **unresolved な参照** は別物です。Ark は構文を読めたが参照先を知らない（外部・組み込み・未宣言・実行時計算の名前）ことを示し、graph 系ツールの `unresolved` / `outsideRepository` で数えられます。診断にはなりません。
- **`unattributed: 0`** は「Ark が *観測した* 参照に、欠けている edge の可能性があるものはない」という意味です。観測していないものは数えられません。**診断 0 件かつ `unresolved: 0` でも、すべての依存関係を把握したことにはなりません** — どの provider も扱わない形式のファイル（`get_language_support` を参照。例：Terraform の `.tf.json`）は調べられず、provider がすべての構文を観測するとも限りません。
- **宣言はそれぞれ独立したシンボルです。** 1 つのファイルが同じ名前を 2 度宣言しても（Go の複数の `init`、二重定義された関数）、それぞれが固有の `symbolId`・ソース・caller・callee を持ちます。万一、異なる 2 つの宣言が同じ 64 ビットの `symbolId` を持った場合（ハッシュ衝突。確率は極めて低い）、Ark は両者を統合せずに index の構築を拒否します。index を使うツールは両方の宣言を示した `SymbolID collision` エラーを返します。複数の宣言が共有する名前は対象ツール（`get_context`、`get_callers`、`get_callees`、`get_relations`、`analyze_change_impact`）にとって曖昧で、候補一覧に各宣言の `symbolId` が表示されます。これらのツールは `symbolId` を受け取って 1 つを選択できます。
- ツールエラー（`isError`）はツール自体の失敗で、診断ではありません。
- Ark の parser（pure-Go の Tree-sitter runtime である gotreesitter）は、正しいコードの一部を受理できないことがあります。主経路で失敗したときは別の経路で再解析し、エラーなく解析できた場合だけその木を使います。それでも失敗した範囲は `parse_error` のままです。また、曖昧な構文（Go の `f[T](x)`、TypeScript の `f<T>(x)`）は、エラーなく解析できた木でも言語の解釈と異なる場合があります。Ark の Go / TypeScript 解析はそこで言語自身の規則に従い、判定できない呼び出しは除外するか Candidate にとどめ、確実なものとしては扱いません。

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
* アーキテクチャと設計上の不変条件 — [ARCHITECTURE.md](ARCHITECTURE.md)（英語）

## Author

© 2025 - 2026 Hiroshi IKEGAMI

## License

[MIT License](LICENSE) のもとで公開されています。
