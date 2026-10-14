# 多言語 Fidelity 評価フレームワーク

監査（2026-10-13、HEAD `bab9b1e` = v6.4.2）で定めた、PHP・Python・JavaScript
（および今後の HTML・CSS）を同じ物差しで測るための評価方法。測定値は
[README.md](README.md) と [audit.md](audit.md) にある。

## 1. 原則

- **Soundness > Semantic Recall > Performance。** 品質ゲートの第一は誤った
  Strong / Exact / OutsideRepository が 0 であること。Recall の改善はその後。
- **Ark 自身の分布は正しさを示さない。** Exact が何割あっても、独立した
  Oracle と突き合わせるまでは精度とは呼ばない。
- **Oracle が判断できないものは成功にも失敗にも数えない。** 判断できない site の
  上の Strong は別の列に置き、**人手で分類する**（自動の採点は下限しか示さない）。
  多すぎるときは READY にせず、Oracle の範囲を広げる。
- **言語ごとの数を直接比べない。** 比べるのは同じ定義の割合（Level 1）と、
  言語固有の意味論の到達度（Level 2）だけ。
- **Coverage と Accuracy を分ける。** Recall は「Ark が表せる site」の上の値で、
  表せる割合（Coverage）と一緒に報告する。

## 2. 道具

| 物 | 役割 |
|---|---|
| `internal/index/fidelity_baseline_test.go`（`TestFidelityBaseline`） | 本番と同じ経路（`index.NewFileIndex` → `resolver.NewInRoot`。参照 ID の重複は最初の 1 件を残す）で全参照を解決し、言語別の分布と参照ごとの `.sites` を出す。Oracle を渡すと採点し、誤りの一覧（`.false`）と site ごとの判定（`.scored`）を出す。opt-in（`ARK_FIDELITY_ROOT` が無いと skip）、CI では動かない |
| `internal/index/fidelity_score_test.go`（`TestFidelityScore`） | 採点の規則（shadowing、同名の関数の中のローカル、1 参照 1 site、ambiguous、via、alias、型宣言ファイル、重複行）を小さな fixture で固定する。CI で動く |
| `docs/scripts/fidelity-baseline.sh` | `TestFidelityBaseline` を 1 コーパスに対して実行し、`report.{txt,json,sites,false,scored}` を書く |
| `docs/scripts/oracle/python-pyright.py` | Python の Oracle: 標準ライブラリの `ast` で参照位置を独立に列挙し、pyright の言語サーバー（`textDocument/definition`）で宣言を得る |
| `docs/scripts/oracle/javascript-tsc.js` | JavaScript の Oracle: TypeScript の構文木で参照位置を列挙し、checker（`allowJs`、`checkJs` なし）で宣言を得る |

Go と Rust には既存の Oracle がある（`docs/scripts/go-oracle.sh`、
`docs/scripts/rust-oracle.sh`）。PHP には無い（§7）。

## 3. Oracle の共通形式（`oracleSite`、JSON Lines）

1 行 1 site。参照する識別子そのものの範囲を持つ。

```json
{"lang":"python","file":"pkg/a.py","name":"h",
 "start_line":8,"start_col":5,"end_line":8,"end_col":6,
 "class":"declaration","decl":{"file":"pkg/util.py","line":1,"col":5,"name":"helper"}}
```

- 位置は 1 起点。列は **バイト単位**（アダプタが UTF-16 から変換する。BOM は
  数え、行は `\n` だけで区切る — Ark と同じ）。`end_col` は最後のバイトの次。
- `class`:

  | class | 意味 | 正解として使うか |
  |---|---|---|
  | `declaration` | `decl` が唯一の宣言（コーパス内の実装ファイル）。`decl.name` は宣言自身の名前（alias import の `h` が指す宣言は `helper`）。`via` は alias が経由する宣言（import の束縛、`const x = require(...)`） | 使う |
  | `ambiguous` | `decls` のどれか（何か所かで代入される名前、条件付きの定義） | 使う（どの宣言でもない Strong は誤り） |
  | `external` | 宣言がすべてコーパスの外（標準ライブラリ、依存）。JS では解決できない bare specifier の import から来る名前も含む | 使う |
  | `declaration_file` | コーパス内の型宣言ファイル（`.d.ts`、`.pyi`）だけに束縛される。実行時に代入される値は決まらない | 使わない（Strong は人手で分類） |
  | `module` | コーパス内のモジュールを指す名前（Python の `pkg.m.f` の `pkg` と `m`）。Ark はモジュールをシンボルにしない | 使わない（Strong は人手で分類） |
  | `dynamic` | 実行時にしか決まらない | 使わない |
  | `unknown` | Oracle が決められない | 使わない（Strong は人手で分類） |

- 宣言の位置は宣言の **名前** の位置。

**照合規則**:

1. site は、終端が一致する同名の Ark 参照に対応する。
2. 1 で対応しなかった site は、まだどの site にも対応していない参照のうち、
   始点が一致するもの、無ければ site を含む最も内側のもの（`new C(...)` のように
   式全体を範囲とする参照）に対応する。1 つの Ark 参照は 1 つの site にだけ
   対応する（`copy.copy()` の呼び出しは属性の `copy` の site のもので、receiver の
   `copy` の site のものではない）。
3. 宣言は、`decl.name`（無ければ site の名前）と同じ名前で、**宣言の行から始まり**、
   その位置を範囲に含む Ark シンボルのうち最も内側のものに対応する
   （関数 `result` の中のローカル `result` は関数 `result` ではない）。

既知の限界: 関数の 1 行目にある、関数と同名の引数は関数に対応してしまう
（`function iter(iter)`。シンボルが名前の範囲を持たないため。監査のコーパスでは 0 件）。
デコレータの行から範囲が始まるシンボルは宣言に対応せず、表現できない site になる
（再レビューで Python のデコレータ、TS の decorator、`export default class`、複数行の
`export const` は対応することを確認した）。匿名の `export default function () {}` は
名前が無いので表現できない。

## 4. 指標（Level 1: 全言語共通）

`TestFidelityBaseline` が `ORACLE {...}` として出す数。

| 列 | 定義 |
|---|---|
| `sites` | Oracle の site の総数（重複行は 1 件） |
| `declaration` / `ambiguous` / `external` / `declaration_file` / `module` / `dynamic` / `unknown` | `class` 別の内訳 |
| `representable` | `declaration` のうち、宣言が Ark のシンボルであるもの。**解決の分母** |
| `not_extracted` | `representable` のうち、Ark が参照を出していないもの（抽出漏れ） |
| `recovered` | `representable` のうち、Ark が唯一の Strong / Exact でその宣言に解決したもの |
| `exact_tp` / `exact_fp` | Ark が Exact で正しい / 別の宣言（**False Exact**） |
| `strong_tp` / `strong_fp` | 同じく Strong（**False Strong**） |
| `via_stop` | Ark の Strong / Exact が、alias の経由する宣言（import の束縛、`require` の変数）で止まっている。誤りでも正解でもない |
| `local_fp` | Oracle の宣言が Ark のシンボルでない（引数、ローカル変数による shadowing）のに、Ark が Strong / Exact を付けた（**False Strong / Exact**） |
| `external_fp` | Oracle は外部と言うのに、Ark がコーパス内のシンボルに Strong / Exact を付けた（**False Strong / Exact**） |
| `ambiguous_fp` | `ambiguous` の site で、どの宣言でもない Strong / Exact（**False Strong / Exact**） |
| `candidate_hit` / `candidate_miss` | Ark が Candidate で、正解が候補に含まれる / 含まれない |
| `unresolved` | `representable` で Ark が Unresolved |
| `false_outside` | `representable` で Ark が OutsideRepository（**False Outside**） |
| `local_outside` | 宣言がコーパス内にある（Ark のシンボルではない）のに、Ark が OutsideRepository（**False Outside**。例: export した object literal のメンバー） |
| `unknown_strong` | `unknown` / `dynamic` / `module` の site の Strong / Exact。自動では数えない（人手で分類） |
| `declaration_file_strong` | `declaration_file` の site の Strong / Exact。同上 |
| `unmatched_strong` | import 以外の Ark の Strong / Exact で、どの site にも対応しないもの |

言語別の分布には `import_strong`（import 参照の Strong / Exact。Oracle は import 文を
site にしないので採点外）も出す。

導出する値:

- **False Strong 合計** = `exact_fp + strong_fp + local_fp + external_fp + ambiguous_fp`（ゲート: 0）
- **False Outside 合計** = `false_outside + local_outside`（ゲート: 0）
- **Resolution recall** = `recovered / representable`
- **Strong precision** = `(exact_tp + strong_tp) / (exact_tp + strong_tp + False Strong 合計)`
- **Extraction recall** = `1 - not_extracted / representable`
- **Coverage** = `representable / declaration`（Ark が宣言をシンボルとして表せる割合）

自動の False Strong 合計は **下限** である。`unknown_strong` と
`declaration_file_strong` を人手で分類した誤りを足したものが、Soundness ゲートで
使う値。

## 5. Level 2: 言語固有の意味論

共通の分母とは別に、合成 fixture の **期待表**（site ごとの正解と、Strong に
してはならない理由）で測る。各項目は「正しく解く」「保守的に Candidate /
Unresolved にとどめる」「誤る」のどれかで採点し、「誤る」は 0 でなければならない。

| 言語 | 項目 |
|---|---|
| PHP | namespace と use（クラス・関数・定数）、関数の namespace → global フォールバック、trait と adaptation、継承、`self` / `static` / `parent`、promoted / 注入 property、動的呼び出し |
| Python | LEGB と shadowing（引数、内包表記、walrus、except、match、for、with）、**関数のどこかで束縛した名前は代入より前も含めて関数全体でローカル**、**クラス本体はメソッドを囲むスコープではない**、モジュールレベルの再束縛や import が同じファイルの def を隠す、`del`、`global` / `nonlocal`、import の全形（相対、alias、`__init__` の再エクスポート、star（外部・未知のモジュールからの star を含む）、条件付き）、メソッドと `self` / `cls`、継承と MRO、`super()`、デコレータ、getattr / setattr、importlib |
| JavaScript | var / let / const と hoisting、TDZ、shadowing（引数、ローカル、ネスト関数、catch、分割代入）、ESM（named / default / namespace / 再エクスポート / alias）、CommonJS（require / module.exports / exports.f）、bare specifier、class・this・super・static・private、prototype、computed access、`import()` |

Python と JavaScript の fixture は監査の作業領域にある。実装ミッションでは、
これを `internal/languages/<lang>/testdata` に置き、期待表をテストにする。

## 6. 分母の規律（Series 1 / Series 2）

Go Phase 2 と同じ方式で、変更前後の比較を固定する。キーは **Oracle の site**
（`file:start_line:start_col:name`）で、Ark の参照の範囲ではない（Ark の範囲は版に
よって変わる）。

- 変更前と変更後で、同じ Oracle の JSON Lines に対して `report.scored` を出す
  （1 行 1 site、タブ区切り: キー、class、`representable` か `-`、Ark の判定の列）。
- **Series 1（固定分母）**: 両方の `report.scored` で 3 列目が `representable` の site。
  Recall の改善はこの集合の上でだけ主張する（判定 `tp` が回復、`not_extracted` が
  抽出漏れ、`via` は経由点で止まった Strong）。
- **Series 2（新規分）**: 変更後にだけ `representable` になった site（新しい
  シンボルで表せるようになったもの）。別に報告し、False Strong ゲートは Series 2 にも
  適用する。
- Oracle とその環境（版、Python の版と依存）、コーパスの版は固定して記録する。
  採点の定義（照合規則、class の基準）を変えたら、変更前の値を同じ定義で取り直す。
- `unknown_strong`、`declaration_file_strong`、`unmatched_strong`、`import_strong` は
  毎回報告する。

## 7. Oracle の選定

| 言語 | Oracle | 版 | License | 独立性 | 用途 |
|---|---|---|---|---|---|
| Go | go/types | Go の版に従う | BSD | 高 | 既存（`go-oracle.sh`） |
| Rust | rustc save-analysis | 1.58.0（≤ 1.69） | MIT / Apache-2.0 | 高 | 既存（`rust-oracle.sh`） |
| Python | pyright（言語サーバー） | 1.1.414 | MIT | 高（Ark と無関係の型検査器） | 主 Oracle。補助に jedi 0.19.2（MIT）、スコープだけなら `symtable` + `ast`（Python 3.12） |
| JavaScript | TypeScript Compiler API（`allowJs`） | 5.6.3（CI の TS Oracle と同じ版） | Apache-2.0 | 高 | 主 Oracle。単一ファイルのスコープは eslint-scope 8.4.0（BSD-2-Clause）で相互検証できる |
| PHP | 未構築 | — | — | — | Tier 1: nikic/PHP-Parser の `NameResolver`（クラス・関数・定数の FQN と、関数の global フォールバック）。Tier 2: PHPStan の Collector（メソッド・プロパティの宣言クラス）。手元に `php` が無いため未試用 |
| HTML / CSS | 構造の Oracle | — | — | — | 実装した評価は `docs/scripts/html-parser-eval`・`css-parser-eval` |

注意:

- **pyright には依存を入れた Python を渡す。** アダプタの第 3 引数のインタプリタを
  pyright の `python.pythonPath` にする。依存が無いと、依存の名前に pyright は何も
  答えず、Ark がそれをリポジトリの同名の宣言に結ぶ誤りが `unknown` に逃げる
  （監査の初回の計測はこの状態だった）。インタプリタの版と `pip freeze` を結果と
  一緒に記録する。
- pyright は `.pyi` スタブを優先する。コーパス内のスタブだけに束縛された site は
  `declaration_file` にする。JavaScript のコーパス内の `.d.ts` も同じ。
- `unknown_strong` と `declaration_file_strong` の site は、`report.scored` から
  抜き出して人手で分類する。監査では preact の `options.unmount(...)` が同じファイルの
  関数 `unmount` に Exact になる誤りと、`options._catchError(...)`（debug / hooks が実行時に
  差し替える）の Strong を、この分類で見つけた。
- TypeScript の checker は代入や JSDoc から member の型を推論する。Ark の規則では
  証明できない推論があるので、Oracle が `declaration` と言う member の site で
  Ark が Unresolved なのは Recall の損失としてだけ数え、Ark の Strong を同じ推論で
  正当化してはならない。
- **生成物はコーパスに含めない。** バンドル・minify 済みのファイル（`dist/`、
  `build/`、`*.min.js`）は 1 文字のローカル名がトップレベルの名前を隠すので、
  名前による解決の誤りが桁違いに増える（監査では three の `package/` 全体で
  `local_fp` 7,585 件と `external_fp` 149 件、`package/src` だけでは 18 件）。生成物の
  誤りも Ark の欠陥だが、手書きのコードと同じ分母に入れない。

既知の Oracle の漏れ（実装ミッションで read / write を抽出する前に扱いを決める）:
Python は文字列の注釈、type comment、累算代入の対象、`del`、`global` 宣言、import 文を
site にしない。Ark は `.pyw` も対象にするが Oracle は `.py` だけ。JavaScript は
namespace import の名前と CommonJS の `module` / `exports` を `declaration` にする
（宣言の位置がファイルや関数の先頭になる。Ark は該当する参照を出さないので現在は無害）。

## 8. コーパス manifest

ダウンロードしたアーカイブの sha256 は、監査のマシンで計算した値。

### Python（PyPI の sdist）

| 規模 | 名前 | 版 | License | sha256 | .py | 行数 | 依存（pyright 用 venv、Python 3.12.15） |
|---|---|---|---|---|---|---|---|
| 中 | requests | 2.32.3 | Apache-2.0 | `55365417734eb18255590a9ff9eb97e9e1da868d4ccd6402399eaf68af20a760` | 34 | 10,737 | urllib3 2.2.3、idna 3.10、charset-normalizer 3.4.0、certifi 2024.8.30、pytest 8.3.3、pytest-httpbin 2.1.0、PySocks 1.7.1 |
| 中 | flask | 3.0.3 | BSD-3-Clause | `ceb27b0af3823ea2737928a4d99d125a06175b8512c445cbd9a9ce200ef76842` | 82 | 17,565 | werkzeug 3.0.4、jinja2 3.1.4、markupsafe 2.1.5、itsdangerous 2.2.0、click 8.1.7、blinker 1.8.2、python-dotenv 1.0.1、asgiref 3.8.1、pytest 8.3.3 |
| 中 | click | 8.1.7 | BSD-3-Clause | `ca9853ad459e787e2192211578cc907e7594e294c7ccc834310722b41b9ca6de` | 71 | 17,551 | pytest 8.3.3 |
| 中 | attrs | 24.2.0 | MIT | `5cfb1b9148b5b086569baec03f20d7b6bf3bcacc9a42bebf87ffaaca362f6346` | 52（ほか `.pyi` 10） | 18,232 | pytest 8.3.3、hypothesis 6.112.1、zope.interface 7.0.3、cloudpickle 3.0.0、mypy 1.11.2 |
| 大 | Django | 4.2.16 | BSD-3-Clause | `6f1616c2786c408ce86ab7e10f792b8f15742f7b7b7460243929cb371e7f1dad` | 2,762 | 455,953 | 性能の計測のみ（Oracle は未実施） |

推移的な依存を含む完全な一覧は、venv の `pip freeze` で記録する（直接指定した版は
上の表。推移的な依存は取得時の最新になるので、実装ミッションでは lock する）。

取得: `https://pypi.org/pypi/<name>/<version>/json` の sdist の URL を curl で取り、
sha256 を確かめて展開する（ビルドもインストールもしない）。

### JavaScript（npm のパッケージ tarball）

| 規模 | 名前 | 版 | License | sha256 | 計測する範囲 |
|---|---|---|---|---|---|
| 中 | lodash-es | 4.17.21 | MIT | `777598ac703f02b403ef678cd11bce2150ad788f35c774ea7c9cc241a892cb7b` | tarball 全体（ESM、JS 644） |
| 中 | express | 4.21.2 | MIT | `fc43a91e7dc7affb53c6ad7123a4f35485ed3c45226ae7a3847b7738e783e008` | tarball 全体（CommonJS、JS 12） |
| 中 | preact | 10.26.4 | MIT | `3ee2f67884ecaad0652a3bb75d1b6734518dc276f0af653e9fb2d24bfc441c27` | `dist/` を除く（JS 49。tarball 全体は JS 80、TS 16） |
| 大 | three | 0.170.0 | MIT | `4a608a355dcaba72e0e5383cdc814303f5b6060b43c238cdf6932dceb699238d` | `package/src` のみ（ESM、JS 678。tarball 全体は JS 1,046） |
| 大 | jquery | 3.7.1 | MIT | `68a9f787516da47c680e09c187bcbac4536b6f85d90eb882844e12919e583f53` | AMD の src（未計測） |

取得: `npm pack <name>@<version>`（スクリプトは実行されない）で tarball を取り、
sha256 を確かめて展開する。範囲の作り方:

```sh
# preact: dist/ を除いたコピー
(cd preact && find . -path '*/dist' -prune -o -type f -print | cpio -pdm ../preact-nodist)
# three: package/src をコーパスのルートにする
corpus=three/package/src
```

### PHP

監査で使った guzzle 7.9.2（CHANGELOG の先頭）と slim **4.12.0**（`Slim/App.php` の
`VERSION`）は、以前のセッションの作業領域にあったもので、取得元とハッシュの記録が
無い。実装ミッションの最初に Packagist の dist（zip）から取り直し、ここに版・
License・sha256 を記録する。

### HTML / CSS

監査の試用に使った実 HTML / CSS（Go のモジュールキャッシュにあった文書と
html5lib の木構築テスト）は、選び方の手順を残していない。W1 / W2 の最初に、
html5lib-tests（版を固定）と、版を固定した静的サイトのコーパスを manifest に載せる。

### 合成・否定・曖昧コーパス

- **小（合成）**: 言語ごとの fixture（§5 の Level 2 の期待表付き）。
- **否定**: Strong にしてはならない site だけを集めたもの（shadowing、外部名、
  bare specifier、動的アクセス、条件付き定義）。期待は「Strong / Exact が 0」。
- **曖昧**: 複数の宣言がありうるもの（条件付き定義、多重継承、同名の兄弟モジュール）。
  期待は Candidate 以下。
- **Cross-file / Cross-module**: 相対 import、パッケージの再エクスポート、
  CommonJS と ESM の混在。

## 9. 再現手順

```sh
# 1. コーパスを取得して展開（例: requests）
curl -LO <sdist URL> && shasum -a 256 requests-2.32.3.tar.gz && tar xzf requests-2.32.3.tar.gz

# 2. Python の Oracle: pyright 1.1.414 と、依存を固定した venv（§8）
npm --prefix /tmp/oracle install pyright@1.1.414
python3.12 -m venv /tmp/env-requests
/tmp/env-requests/bin/pip install --only-binary=:all: 'urllib3==2.2.3' 'idna==3.10' ...
/tmp/env-requests/bin/pip freeze > env-requests.freeze.txt
python3.12 -I docs/scripts/oracle/python-pyright.py \
  /tmp/oracle/node_modules/pyright/langserver.index.js requests-2.32.3 \
  /tmp/env-requests/bin/python > py-requests.jsonl

# JavaScript の Oracle: TypeScript 5.6.3
npm --prefix /tmp/oracle install typescript@5.6.3
node docs/scripts/oracle/javascript-tsc.js /tmp/oracle/node_modules/typescript lodash-es > js-lodash.jsonl

# 3. Ark を採点（out/requests/report.false に誤り、report.scored に site ごとの判定）
sh docs/scripts/fidelity-baseline.sh requests-2.32.3 out/requests python py-requests.jsonl

# 4. Series 1: 変更前（before/）と変更後（after/）の report.scored を site のキーで join し、
#    両方で 3 列目が representable の行（report.scored はバイト順に並んでいる）
LC_ALL=C join -t "$(printf '\t')" before/report.scored after/report.scored |
  awk -F'\t' '$3 == "representable" && $6 == "representable"'
```

Python の Oracle は `python3.12 -I` で動かす（`-I` は作業ディレクトリのファイルを
読み込まない。コーパスのコードは実行しない）。Oracle の出力は決定的であることを
確認済み（同じ入力で 2 回生成して一致。[audit.md](audit.md) の実測を参照）。

## 10. ゲート（実装ミッションの合格条件の雛形）

1. 全コーパスで False Strong 合計 = 0 と False Outside 合計 = 0。`unknown_strong` と
   `declaration_file_strong` は全件を人手で分類し、誤りは 0。
2. Series 1 で `recovered` が減らない（失った正解 0）。段階が明示した場合に限り、
   減少を site ごとの理由付きで許容する。
3. `unmatched_strong` と `import_strong` が変更前より増えない。増えたら理由を示す。
4. Level 2 の期待表で「誤る」が 0。
5. 決定性: 同じコーパスの 2 回の実行で `report.sites` と `report.scored` が一致。
6. 性能: index の時間とメモリの変化を、変更前と同じマシンで報告する。
