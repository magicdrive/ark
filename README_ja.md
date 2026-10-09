# Ark

**AI コーディングエージェントのためのコードインテリジェンスエンジン**

[English](README.md) · [導入ガイド](docs/getting-started.md) ·
[MCP ツール](docs/mcp-tools.md) · [解決モデル](docs/resolution-model.md) ·
[アーキテクチャ](ARCHITECTURE.md)

Ark はリポジトリを構文解析して宣言と参照を抽出し、各参照がどの宣言を指すかを解決したうえで、その結果のグラフを [Model Context Protocol](https://modelcontextprotocol.io)（MCP）でコーディングエージェントに提供します。エージェントは「この関数を呼んでいるのはどこか」「この変更は何に影響するか」「編集の前に読むべきコードはどれか」を問い合わせ、どこまで確かな答えなのかを添えた回答を受け取れます。

Ark は単一の静的バイナリです。Go、TypeScript、TSX、PHP、Terraform、JavaScript、Python を、リポジトリのコードを一切実行せずに解析します。

> 詳細なドキュメント（`docs/` 以下）は英語で提供しています。

## なぜ Ark か

大きなリポジトリで作業するエージェントは、たいていテキスト検索とファイル全体の読み込みでコードをたどります。この方法には、リポジトリが大きくなるほど増える代償があります。

- **関係のないコードでコンテキストを使い果たす。** 1 つの関数を探すために、検索に当たったファイルを丸ごと読むことになります。
- **名前があいまい。** `Save` が 5 つのパッケージで宣言されていても、文字列の一致ではどれが呼ばれているのか区別できません。
- **関係を推測してしまう。** 別のパッケージや別の言語にある同名の関数が、呼び出し元のように見えてしまいます。
- **分からないことが答えに見える。** 「ほかに呼び出し元は見つからない」が、本当に存在しないのか、検索から見えていないだけなのか区別できません。

Ark は、構造と根拠でこれらに対処します。

| 課題 | Ark が提供するもの |
|---|---|
| テキストが多すぎる | ファイル全体ではなく、1 つのシンボルの宣言、呼び出し関係、トークン予算内のコンテキスト |
| 名前があいまい | 宣言ごとに固有の `symbolId`。あいまいな問い合わせには、黙って 1 つを選ばず宣言の一覧を返す |
| 関係の推測 | import、パッケージ、レシーバー、名前空間など、各言語のスコープ規則による参照の解決。言語をまたいで名前が一致することはない |
| 隠れた不明点 | すべての回答に確信度を付け、検出したが解決できなかった参照の数を示す |

## 仕組み

```text
リポジトリのファイル
      │
      ▼
言語プロバイダー     Tree-sitter による 1 ファイル単位の構文解析:
      │              宣言、参照、import、言語ごとの根拠
      ▼
インデックス構築     安定した ID を持つシンボル、包含関係
      │
      ▼
リゾルバー           根拠 → 候補 + 確信度（exact / strong / candidate）
      │
      ▼
グラフ               辺になるのは一意の exact / strong の解決だけ。
      │              それ以外は捨てずに数として記録
      ▼
コンテキスト · 影響範囲 · リポジトリマップ · 検索
      │
      ▼
MCP サーバー  ──────►  コーディングエージェント
```

インデックスは最初の問い合わせで構築され、ソースファイルが変わらないかぎり再利用されます。抽出結果はセッションをまたいでディスクにキャッシュされます。詳細は [ARCHITECTURE.md](ARCHITECTURE.md) と [運用ガイド](docs/operations.md) を参照してください。

## クイックスタート

**1. インストール**

```bash
brew install magicdrive/tap/ark
```

[Releases](https://github.com/magicdrive/ark/releases) からバイナリを入手するか、`go install github.com/magicdrive/ark@main` でも導入できます（`@latest` は旧 v1 系に解決されるので使わないでください。理由は[導入ガイド](docs/getting-started.md#go-toolchain)を参照）。

**2. エージェントに接続する** — リポジトリのルートで実行します。

```bash
ark setup claude          # Claude Code
ark setup cursor          # Cursor
ark setup codex           # Codex
ark setup cline           # Cline CLI
ark setup copilot-vscode  # GitHub Copilot in VS Code
ark setup copilot-cli     # GitHub Copilot CLI
```

`setup` はクライアントの MCP 設定に Ark のエントリーを追加し、ほかのエントリーには一切触れません。クライアントを再起動し、サーバーを承認してください。

**3. 使い方をエージェントに伝える**（任意・推奨）

```bash
ark instruction claude >> CLAUDE.md   # codex / cursor / cline / copilot-vscode / copilot-cli も可
```

**4. 質問する** — 「このリポジトリの概要を教えて」「`PlaceOrder` を呼んでいるのはどこ?」「`store.Save` を変更すると何が壊れうる?」など。

手動での設定を含む詳しい手順は[導入ガイド](docs/getting-started.md)にあります。

## 例: 関数を変更する前に

5 つのファイルからなる Go のサービスで、`api` が `orders.Place` を呼び、`orders.Place` が `store.Save` を呼びます。以下は Ark の実際の出力です（`…` は省略箇所）。

**`store.Save` に依存しているのは何か** — `analyze_change_impact`

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

**`Checkout` は何を呼んでいるか** — `get_callees`

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

`Checkout` は構造体のフィールドを経由して `h.audit.Record(…)` も呼んでいますが、Ark はフィールドの型を追跡しません。そのため、この呼び出しを辺とは主張せず、`Log.Record` を候補として示し、参照を unattributed（帰属できない参照）として数えます。エージェントは、辺の一覧がすべてではないことを知ることができます。

**`Place` を変更するために何を読めばよいか** — `get_context`

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

対象の関数とその呼び出し先・呼び出し元、あわせて 6 つの宣言（推定約 140 トークン）が返ります。それらを含む 3 つのファイルを丸ごと読む必要はありません。

## 機能

Ark の MCP サーバーは 21 のツールを提供します。パラメーターと使用例は [MCP ツールリファレンス](docs/mcp-tools.md)を参照してください。

| 目的 | ツール |
|---|---|
| リポジトリの全体像をつかむ | `get_repository_map`、`get_directory_tree`、`get_project_stats` |
| シンボルを探す | `search_context`（名前の一部から）、`find_symbol`（正規表現）、`search_code`（種類・呼び出し・型の使用）、`get_symbols`、`get_symbol` |
| 関係をたどる | `get_callers`、`get_callees`、`get_relations`、`find_references` |
| 変更の影響を評価する | `analyze_change_impact` |
| 必要なコンテキストだけを取得する | `get_context`、`search_context` |
| ファイルを読む・検索する | `get_file_content`、`get_files_arklite`、`search_in_files`、`list_files`、`get_file_info` |
| 解析の状態を確認する | `get_diagnostics`、`get_language_support` |

## 信頼性と確信度

解決された参照には、それぞれ確信度が付きます。確信度は根拠の種類を表すもので、確率ではありません。

| 確信度 | 意味 | グラフの辺になるか |
|---|---|---|
| `exact` | Ark がモデル化した言語のスコープ規則で証明された唯一の対象（同じファイル、import、修飾名、宣言された型） | なる |
| `strong` | より弱い根拠による唯一の対象（パッケージ内でその名前を持つ唯一の宣言など） | なる |
| `candidate` | 1 つ以上のありうる対象 | **ならない** |
| 未解決 | リポジトリ内に対象がない（組み込み関数、外部パッケージ、実行時に決まる名前） | ならない |

実際の使い方では、次の点に注意してください。

- **`candidate` は手がかりであって、依存関係ではありません。** 呼び出し元・呼び出し先・影響範囲・コンテキストは、辺だけから作られます。
- **一覧が空でも、存在しないことの証明にはなりません。** グラフ系の回答には必ず、`unattributed`（辺になっていない可能性のある参照）、`unresolved`、`outsideRepository` の数が含まれます。ファイルを完全に解析できなかった場合は `indexDiagnostics` も付きます。
- **Ark は宣言のあいだで推測しません。** あいまいな名前には、`symbolId` 付きの宣言一覧を返します。
- **名前は言語の境界を越えません。** Python の `run()` が Go の `run` の呼び出しになることはありません。TSX は TypeScript なので、両者は名前を共有します。
- **Go は Go のスコープ規則に従います。** 修飾のない名前はそのパッケージの中で、`pkg.Name` はその import を通して解決され、ローカルで覆い隠された名前がほかの宣言に解決されることはありません。

Ark の根拠は静的なものです。コンパイラではなく、型推論も行いません。レシーバーの型がその場で書かれていないメソッド呼び出しは `candidate` のままです。詳しくは[解決モデル](docs/resolution-model.md)を参照してください。

## 言語サポート

言語ごとにサポートの範囲は異なります。各言語には、Ark のテストが認定している最高の段階（サポートレベル）があります。

| 言語 | レベル | 補足 |
|---|---|---|
| Go | context-quality certified | パッケージスコープ。メソッド呼び出しの解決には、レシーバーの型がその場で書かれている必要がある |
| TypeScript、TSX | context-quality certified | 相対 import、barrel、型の付いたレシーバー。`tsconfig` のパス設定と型推論には非対応 |
| PHP | graph | 名前空間、`use`、継承とトレイト。フレームワークやオートロードの意味論には非対応 |
| Terraform | graph | モジュール単位のアドレス、ローカルモジュールの出力。`.tf.json` は読まない |
| JavaScript | references | トップレベルの関数・クラス・`const`/`let`。クラスのメソッドは抽出しない |
| Python | references | トップレベルの関数とクラス。メソッドは抽出しない |

グラフ系のツールはすべての言語で動作しますが、認定レベルを超える結果はテストの対象外です。詳細と言語ごとの制限は[言語サポート](docs/language-support.md)を参照してください。

## 性能

公開リポジトリに対して MCP サーバーで計測した結果です（5 回の中央値。[計測方法と環境](docs/performance.md)）。

| リポジトリ | インデックスしたファイル数 | 最初の問い合わせ | 2 回目以降 | 再起動後（キャッシュあり） | 最大メモリ |
|---|---:|---:|---:|---:|---:|
| ky（TypeScript） | 34 | 0.14 秒 | 2 ms | 21 ms | 50 MB |
| express（JavaScript） | 152 | 0.40 秒 | 10 ms | 82 ms | 56 MB |
| Ark（Go） | 600 | 3.8 秒 | 41 ms | 0.33 秒 | 246 MB |
| golang.org/x/tools（Go） | 1,875 | 18.5 秒 | 138 ms | 1.7 秒 | 959 MB |

エージェントがどれだけトークンや時間を節約できるかについて、Ark は一般的な数値を示しません。それはエージェント、モデル、タスクによって変わるためです。Ark の応答に含まれるトークン予算は推定値（`len(text)/4`）であり、モデルのトークン数ではありません。

## セキュリティとプライバシー

- Ark は指定したルート以下のファイルを読むだけで、リポジトリのコード、ビルドツール、パッケージマネージャーを実行しません。
- Ark は外部へのネットワーク接続を行いません。オプションの HTTP トランスポートは `localhost` でのみ待ち受け、認証はありません。
- ツールに渡すパスはルートの内側に限られます。ルートの外へ向かうシンボリックリンクは、運用者がサーバーを `--allow-external-symlinks on` で起動しない限り、読み取りもインデックス化もされません（[Symlink policy](SECURITY.md#symlink-policy)）。
- Ark はソースコードを MCP クライアントに返します。それがモデルの提供元に送られるかどうかは、クライアントが決めます。Ark のパターン規則が検出する秘密情報（クラウドやサービスのトークン、秘密鍵、`password = …` のような代入）は、既定で MCP の応答からマスクされます（`--mask-secrets off` で無効にでき、その場合は警告が出ます）。マスクは完全ではなく、ファイルパスとシンボル名はマスクしません。
- `.arkignore` で除外したファイルは、MCP サーバーからは存在しないものとして扱われます。マスクの設定に関わらず、どのツールもそれを読み出し、一覧に出し、検索し、インデックスに含めることはありません。
- サーバーは抽出結果を `<root>/.ark/index` にキャッシュします。`.ark/` を `.gitignore` に追加してください。

詳細と既知の制限は [SECURITY.md](SECURITY.md) にあります。

## 制限事項

- 型推論、戻り値の型の伝搬、制御フロー解析は行いません。
- モジュール解決はコンパイラと同等ではありません。`tsconfig` のパス設定、Composer / PSR-4 のオートロード、`package.json` は解釈しません。
- フレームワークの意味論（DI コンテナ、ルーティング、デコレーター）は扱いません。
- 明示的な import があっても、言語をまたぐ参照は解決しません（JavaScript から TypeScript のファイルを import する場合など）。
- JavaScript と Python のクラスのメソッドは抽出しません。
- 構文解析器が完全には読めなかったファイルは、読めた部分だけを解析します。残りは `get_diagnostics` で報告されます。

## そのほかの機能

- **リポジトリのダンプ** — `ark <dir>` で、ディレクトリのツリーとファイルの内容を、テキスト、Markdown、XML、またはコンパクトな *arklite* 形式の 1 ファイルに書き出します。`.gitignore` の適用とシークレットのマスクに対応しています。
- **`ark symbol` / `ark syntax`** — 1 つのファイルの宣言や構文木を表示します。
- **`ark skill`** — skill に対応したエージェント向けに、作業ガイドを生成します。

詳しくは [CLI リファレンス](docs/cli.md)を参照してください。

## ドキュメント

| 文書 | 内容 |
|---|---|
| [導入ガイド](docs/getting-started.md) | インストール、エージェントの設定、最初の使い方、アップグレード |
| [MCP ツール](docs/mcp-tools.md) | 全ツールのパラメーター、出力、注意点 |
| [解決モデル](docs/resolution-model.md) | 確信度、根拠、名前空間、回答の読み方 |
| [言語サポート](docs/language-support.md) | サポートレベルと言語ごとの機能・制限 |
| [CLI リファレンス](docs/cli.md) | すべてのコマンドとオプション |
| [性能](docs/performance.md) | 計測結果と再現手順 |
| [運用ガイド](docs/operations.md) | インデックスのライフサイクル、キャッシュ、リソース、障害時の挙動 |
| [トラブルシューティング](docs/troubleshooting.md) | よくある問題とメッセージ |
| [アーキテクチャ](ARCHITECTURE.md) | コントリビューター向けの設計上の不変条件 |
| [セキュリティ](SECURITY.md) | 脅威モデルとデータの境界 |

## コントリビューション

バグ報告とプルリクエストは [GitHub](https://github.com/magicdrive/ark/issues) で受け付けています。解析に関わる変更の前に [ARCHITECTURE.md](ARCHITECTURE.md) を読んでください。一見もっともらしい変更が壊しうる不変条件と、それを守るテストが書かれています。変更の検証方法は同文書の第 8 節にあります（`make lint` で大半をローカル実行できます）。

## ライセンス

[MIT](LICENSE) © 2025–2026 Hiroshi IKEGAMI
