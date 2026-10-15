# CSS Parser の再評価（Hardening Phase A4・D）

## A4. 選定の経緯の確認（Phase 5、decisions D24）

Phase 5 は 3 つを比べ、CSS Syntax Level 3 の自前の実装（`internal/languages/css` の `tokenizer.go`・
`reader.go`）を採った。比較のハーネスは `docs/scripts/css-parser-eval/`（`run.sh`）で、Phase 9 の監査 B が
白紙の作業領域で再実行し、件数がすべて一致した。

| 観点 | 自前（CSS Syntax 3） | tdewolff/parse/v2/css | gotreesitter（tree-sitter-css） |
|---|---|---|---|
| 公開テスト（SimonSapin/css-parsing-tests、149 件） | 149 合格 | 125 合格（落ちた 24 件のうち 16 件が現行の仕様からのずれ: エスケープを含む識別子、NUL、改行を含む文字列、`url()` の各形） | （トークンの水準では比較せず） |
| 実 CSS 967 ファイルのトークン | tdewolff と境界・種類が一致（既知の 3 種類の違いを除く） | — | — |
| 宣言の数 | 494,564 | — | 494,564（一致） |
| エラーの報告 | 規則の回復のとおり | — | 436/967 ファイル（45%）でエラー（文法が知らない構文への過剰な報告） |
| 時間 | 基準 | — | 約 9 倍 |
| 範囲 | 元のバイトの上で動く（前処理の影響を受けない） | 値の復号は利用側の仕事 | — |
| 依存 | なし | 新しい依存 | 既存 |

- tdewolff の既知の違い（仕様と違うのは tdewolff の側）: `url("x")` を 1 つの URL トークンにする
  （仕様では function と string）、`--x` を独自の種類にする、unicode-range（2014 年の CR の表現、
  現行の仕様に無い）。
- 安全性: 一度読みの再帰下降、括弧の対応の表、入れ子 256 段の上限、最悪の入力 8 種が線形の時間
  （`run.sh` とテストで固定）、fuzz（Phase 6）。
- 未決のまま残っていたこと（Phase 9 の監査 B の注記）: Provider の tokenizer はハーネスの移植なので、
  CSS の `TestHarnessAgreement` は独立の照合ではない。**独立の証拠は公開テストと tdewolff との比較**。

## Phase D の方針（事前に結論を置かない）

D24 の比較はトークンの水準と宣言の数が中心で、規則の構造（at-rule と prelude、入れ子の規則、宣言の
範囲、回復）を独立の実装と照合していない。Phase D で次を足し、維持か置き換えかを証拠で決める。

1. 規則の水準の差分テスト: 両方の結果を意味の比較ができる正規化（規則の種類と名前、prelude の
   トークン列、宣言の名前と値のトークン列、入れ子の深さ、回復で捨てた範囲）にして比べる。
2. 仕様の行列（`@media`・`@container`・範囲のクエリ・`@supports`・`@layer`・`@property`・`@keyframes`・
   `@font-face`・入れ子・カスタムプロパティ・`var()`・`calc()`・`url()`・エスケープ・非 ASCII・文字列・
   コメント・壊れた構文・EOF・深い入れ子・予期しない区切り）を、仕様の手順から期待を書いた表で照合する。
3. fuzz（不変条件: panic なし、停止、範囲が入力の中、決定的、規則の数と宣言の範囲が整合）を
   長めに回し、時間と件数を記録する。

## D1. 規則の水準の差分テスト（tdewolff の css.Parser と）

`docs/scripts/css-parser-eval/rulecmp.go`（`csseval rules`。`run.sh` が作る作業領域で、Phase 5 と同じ
967 ファイル: npm の 15 パッケージと GOMODCACHE の CSS）。両方の出力を、深さ付きの事象の列（at-rule の名前と
ブロックの有無、qualified rule、宣言の名前）に正規化して、ファイルごとに比べる。比べる自前の側は
ハーネスの walker（Provider の読み取り部はその移植で、`TestHarnessAgreement` が同じ構造を出すことを
確かめている）、独立の側は tdewolff/parse/v2 v2.8.16 の `css.Parser`。

**結果: 967 ファイルのうち 939 が完全に一致（自前の側の事象 679,566）。違いは 28 ファイル。**

| 種類（自前 / tdewolff の最初の違い） | ファイル | 原因 | 仕様に合うのは |
|---|---:|---|---|
| `rule{` / `@container{`、`rule{` / `rule{` | 11 | tdewolff は `@container` のブロックの中の規則を規則として読まない（`@container` を出し直す） | 自前（`@container` のブロックは規則のリスト） |
| `decl` / （終わり）、`rule{` / （終わり） | 11 | tdewolff が途中で読むのをやめる: 規則の中の入れ子の `@media`（CSS Nesting）で "unexpected ending in qualified rule"（7）、CSS に混じった Hugo のテンプレート `{{/* … */}}` で "unexpected token '{'"（3）など | 自前（仕様の回復は最後まで読む。入れ子の at-rule は CSS Nesting） |
| `decl initial-value` / `rule{` | 3 | tdewolff は `@property` のブロックの記述子を宣言として出さない | 自前（`@property` のブロックは宣言のリスト） |
| `rule{` / `decl *zoom` | 3 | IE 向けの `*zoom: 1`。tdewolff は宣言として許容する | 自前（`*` は識別子でなく、宣言として読めない部分は捨てる。現行のブラウザも無視する） |

tdewolff が自前より多くの事象を出したのは `*zoom` の 3 ファイルだけで、少なかったのは 25 ファイル（途中で
止まる 10 ファイルを含む）。**自前の側が仕様と違う違いは見つからなかった。**

## D2. 仕様の行列（`internal/languages/css/spec_matrix_test.go`）

`@media`・`@container`・範囲のクエリ・`@supports`・`@layer`（文とブロック）・`@property`・`@keyframes`・
`@font-face`・CSS Nesting（`&`、子孫の規則、入れ子の `@media`、ident で始まる入れ子の規則）・カスタム
プロパティ（`{}` を含む値、空の値）・`var()`・`calc()`・`url()` の各形・エスケープを含む識別子・非 ASCII の
識別子・文字列のエスケープ・コメント・`!important`（空白と大文字）・壊れた宣言・EOF（文字列・コメント・
ブロック）・予期しない `}`・CDO / CDC・深い入れ子（300 段、上限 256 で `too_deep`）の 27 ケース。実装の
出力を観察したうえで、各ケースを CSS Syntax 3（現行の Editor's Draft）の手順（stylesheet・block の内容・
qualified rule・at-rule・宣言の消費）と 1 件ずつ照合した。照合で仕様と食い違ったものは無く、その出力を
期待として固定した（非自明なケースの理由はテストのコメント）。

注記: 構文の水準で正しい規則でも、セレクタとして無効な prelude（`} d { … }` の `} d`）はブラウザが捨てる。
Ark はセレクタの文法を検証しないので、そのような規則の中の宣言の参照（`animation-name` など）も出す
（構文の水準の判断。既知の制限）。

## D3. fuzz

| 対象 | 時間 | 実行数 | 新しい入力（coverage） | 失敗 |
|---|---|---:|---:|---:|
| `FuzzExtractCSS`（Provider の抽出 2 種 + 読み取り部） | 5 分 | 15,698,261 | 171（合計 1,063） | 0 |
| `FuzzExtractHTML`（埋め込みの CSS / JS を含む） | 5 分 | 1,564,871 | 211（合計 1,126） | 0 |

不変条件: panic しない、決定的（同じ入力の 2 回の抽出が一致）、Symbol・参照・項目の範囲が入力の中、
入れ子の深さが上限未満、**閉じていないブロックがあれば `eof_in_block` を報告する**（Hardening で追加:
不完全な構文を完全と判定しない）。停止しない入力は `go test -fuzz` の時間の上限で検出される（0 件）。
制約: それぞれ 5 分（CI の fuzz smoke は短時間）。資源の `url()` の種を足した。

## 性能（967 ファイル、30.1MB、5 回の最良値、同じマシン）

| 処理 | 時間 |
|---|---|
| tdewolff の `css.Parser`（構文解析のみ） | 0.24 秒 |
| 自前の tokenizer | 0.62 秒 |
| 自前の読み取り部（`read`） | 1.58 秒 → **1.09 秒**（トークン列の容量を入力の 1/4 で確保。`growslice` が時間の 1/3 だった） |
| Provider の抽出の全体（`Extract`、CSS Modules を含む） | 1.92 秒 → **1.39 秒**（約 22MB/秒） |

tdewolff は約 4.5 倍速い。自前の tokenizer は識別子の値を復号した文字列として作る（エスケープを解いた
名前が Symbol の名前になる）ので、その分の費用がある。

## D4. 維持か置き換えか

**判断: 自前の実装を維持する。** 置き換えの計画は作らない。

| 観点 | 自前 | tdewolff/parse/v2/css |
|---|---|---|
| 仕様への適合（トークン） | 公開テスト 149/149 | 125/149（16 件は現行の仕様からのずれ） |
| 仕様への適合（規則） | 967 ファイルの差分のすべてで、仕様に合うのは自前の側。仕様の行列 27 ケースが一致 | `@container` の中の規則、CSS Nesting の入れ子の at-rule、`@property` の記述子を読めない。10 ファイルで途中で止まる |
| 位置の精度 | 元のバイトの上で動く（前処理の影響なし）。範囲は fuzz とテストで固定 | `url("x")` を 1 トークンにするなど、範囲の対応に補正が要る |
| 回復 | 仕様の回復で最後まで読み、失われる内容を診断にする | 途中で止まる（エラーで終わる）入力がある |
| 性能 | 22MB/秒（実用上の問題なし: CSS はリポジトリの小さな部分） | 約 4.5 倍速い |
| 依存 | なし | 新しい依存（さらに値の復号は自前で書く必要） |
| 保守 | tokenizer と読み取り部（テスト・fuzz・差分のハーネス・仕様の行列つき） | 上流の対応を待つ必要（CSS Nesting、`@container`） |
| Ark の抽出との適合 | 項目（at-rule・規則・宣言・問題）を直接出す | イベントの列から組み直す |

「ライブラリを統一したい」という理由での置き換えにはあたらない。性能の差は、仕様の適合と位置の精度の
差に比べて小さい。

### 保守の方針

- 差分のハーネス（`docs/scripts/css-parser-eval/run.sh`: トークンと規則の水準の tdewolff との比較、公開
  テスト、gotreesitter との宣言の数）を、CSS の仕様の変更や読み取り部の変更のたびに再実行する。
- 仕様の行列（`spec_matrix_test.go`）に、CSS Syntax の Editor's Draft の変更（例: 非 ASCII の識別子の範囲
  #7129）を足す。
- fuzz の不変条件を保つ（CI の fuzz smoke）。
