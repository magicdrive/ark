# 多言語 Fidelity Baseline と Web 言語アーキテクチャ監査

監査日 2026-10-13、対象 HEAD `bab9b1e`（v6.4.2）。Production code は変更していない
（`docs/language-support.md` に、監査で確かめた既知の誤りを追記した）。

> この文書群は、Web Semantic Fidelity の実装**前**の時点の監査である。ここに書いた現状（例: CommonJS は Unsupported）と
> 設計案は、その時点のもの。現在の挙動は `docs/language-support.md`・`docs/resolution-model.md` に、実装の決定と検証は
> `docs/development/web-semantic-fidelity/` にある。評価の定義（`evaluation.md`）は、今も採点のハーネスの基準である。

| 文書 | 内容 |
|---|---|
| [audit.md](audit.md) | 言語アーキテクチャ、PHP の Baseline、Python と JavaScript / TypeScript の監査と実測 |
| [evaluation.md](evaluation.md) | 評価フレームワーク: Oracle の共通形式、指標、分母の規律、コーパス manifest、再現手順、ゲート |

道具:

- `internal/index/fidelity_baseline_test.go` — 本番と同じ経路で全参照を解決し、
  分布と参照ごとの結果を出す。Oracle を渡すと採点し、誤りの一覧と site ごとの判定を
  書き出す。opt-in（CI では skip）。
- `internal/index/fidelity_score_test.go` — 採点の規則を固定する単体テスト（CI で動く）。
- `docs/scripts/fidelity-baseline.sh` — 上を 1 コーパスに対して実行する。
- `docs/scripts/oracle/python-pyright.py` — Python の Oracle（pyright）。
- `docs/scripts/oracle/javascript-tsc.js` — JavaScript の Oracle（TypeScript の checker）。

## 主な発見

1. **最大の品質問題は「名前だけの Strong / Exact」で、言語をまたいで共通している。**
   Python と JavaScript の名前による解決は、引数・ローカル変数による shadowing、外部の
   名前、属性の連鎖の receiver を無視して、同じファイル・同じディレクトリ・リポジトリで
   一意な宣言に結ぶ。独立した Oracle で数えた誤った Strong / Exact（Oracle が判定できない
   site を人手で分類した誤りを含む）:

   | 言語 | コーパス | 誤り | Strong precision（自動分） | Resolution recall | Coverage |
   |---|---|---|---|---|---|
   | Python | requests 2.32.3 | 191 | 78.0% | 81.9% | 15.2% |
   | Python | flask 3.0.3 | 56 | 92.6% | 52.6% | 12.0% |
   | Python | click 8.1.7 | 55 | 95.8% | 63.5% | 20.2% |
   | Python | attrs 24.2.0 | 61 以上（推定約 68） | 92.1% | 67.3% | 14.4% |
   | JavaScript | lodash-es 4.17.21 | 38 | 96.5% | 44.7% | 23.4% |
   | JavaScript | express 4.21.2 | 1 | 97.4% | 55.1% | 3.9% |
   | JavaScript | preact 10.26.4（`dist/` 除く） | 13 | 94.6% | 29.3% | 18.8% |
   | JavaScript | three 0.170.0（`src`） | 18 | 99.6% | 44.4% | 14.6% |

   Resolution recall は「Ark が宣言をシンボルとして表せる site」（Coverage の割合）の上の
   値。同じ形の誤りが、認定済みの TypeScript（shadowing による誤った Exact）と PHP の関数
   （`\json_encode()` / `\json_decode()` が `GuzzleHttp\` の同名関数に Strong。guzzle で
   4 件すべて誤り）にもある。
2. **PHP は設計の見本だが、数値の基準ではない。** 独立した Oracle・golden・実コーパスが
   無く、過去の「PHP 100点」「61→95」は計測に基づかない（再現できない）。PHP 相当を
   「設計上の到達点」として定義し、数値は言語共通の指標で置いた（[audit.md](audit.md) §B.4）。
3. **Python と JavaScript は独立 Oracle で測れるようになった。** pyright 1.1.414（依存を
   固定した Python 3.12 の venv）と TypeScript 5.6.3 の checker。両方とも 2 回の実行で
   出力が一致し、Ark 側の結果も一致した。
4. **binding なしで誤りだけを止めると Recall が壊滅する。** ModuleScoped にするだけでは、
   Python の Recall の上限が 1 割前後、lodash の正解の 99.6% が Candidate に落ちる。
   Soundness の回復と import の binding は同じ段階で出す。
5. **HTML の Parser（gotreesitter）は、ERROR も診断も出さずに木を途中で切ることがある。**
   既存の言語では約 6,500 ファイルで 0 件。HTML の前に言語共通の検出を入れ、Parser は
   HTML5 準拠の x/net/html の tokenizer を推奨する（新規依存なので保守者の判断が必要）。
6. **Web 横断は「パスの証拠」と「ありうる一致」に分ける。** 名前空間の分離の例外は
   パスの証拠（リソースリンク、CSS Modules の binding）による edge と Candidate だけに限り、
   selector と id の文字列一致はグラフの外に置いて Strong にしない。

## ロードマップの状況（2026-10-15）

監査で立てたロードマップ（Gap Matrix と段階 M0〜M6・W0〜W5）の詳細な文書と、実装前の HTML / CSS の設計案は、
役目を終えたので公開ツリーから外した。コミット `508e402`（ブランチ `archive/web-semantic-fidelity-ledger`）に残っている。
実装した段階の決定は `docs/development/web-semantic-fidelity/decisions.md` にある。

| 段階 | 状況 |
|---|---|
| M0 評価基盤 | 済（`internal/index/fidelity_baseline_test.go`、`docs/scripts/oracle/`） |
| M3 ES のスコープと ESM の binding（JS と TS） | 済 |
| M5 JS のクラスと CommonJS | 済（CommonJS の export 値は Candidate が上限。D39） |
| W1 HTML、W2 CSS、W3 リソースリンク、W5 埋め込み領域、W4 の CSS Modules | 済。HTML のパーサーは x/net/html、CSS は CSS Syntax Level 3 の自前の実装を採った（D19・D24）。gotreesitter の html の無言の切り捨て（W0 の動機）は、HTML で使わないので前提が変わった |
| W4 の「ありうる一致」の層（HTML の id ↔ JS の `getElementById`、class ↔ セレクタ） | 未実装（D28）。名前による関係は名前空間をまたがない |
| M1 PHP の関数の名前空間解決と Tier 1 Oracle（nikic/PHP-Parser の NameResolver） | 未着手（`docs/language-support.md` の PHP の「Known false strong」） |
| M2 Python の Soundness の回復と import の binding | 済（スコープ、ModuleScoped、相対・絶対 import の binding と export、関数内の import。4 コーパスで False Strong 0。`docs/language-support.md` の Python の節） |
| M4 Python のメンバーと receiver、M6 Python の継承 | 済（メソッド・属性のシンボル、`self` / `cls` / `super()`、識別できる基底を辿るメンバー探索。MRO の衝突は Candidate）。ローカル変数の型推論は未実装 |

未着手の段階の要点:
- 誤りを止める変更（ModuleScoped、スコープ）と import の binding は、同じリリースで出す（上の発見 4）。Python は M2・M4・M6 を一度に入れた。
## 独立レビュー

3 人のレビュー（A: Python の意味論、B: JavaScript / TypeScript の意味論、C: アーキテクチャと
評価方法）と、修正を確かめる再レビューの指摘を反映した。重大な指摘は 2 件で、どちらも
修正後に計測をやり直した（再レビューで重大な問題は残っていないことを確認。再レビューの
中程度の指摘 2 件 — `C.X = 1` の代入の扱いと、`report.scored` の representable 列 — も
修正して再計測した）:

- alias import の正しい解決が誤りと数えられる（A・B・C 共通）→ Oracle が宣言自身の名前を
  出し、それで照合する。`TestFidelityScore` で固定。
- pyright の Python 環境が暗黙で、依存が無いために誤りが「判定不能」に逃げる（A）→
  依存を固定した venv を Oracle に渡す。判定不能な Strong は人手で分類する運用にした。
