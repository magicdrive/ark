# 多言語 Fidelity 監査: 現状と Baseline

対象: HEAD `bab9b1e`（v6.4.2）、2026-10-13。測定方法は [evaluation.md](evaluation.md)。
ここに書く数は、記録したコマンドで再現できるものだけ。推測は「推測」と書く。

## A. 言語アーキテクチャ

### A.1 共通部分

| 層 | 場所 | 役割 |
|---|---|---|
| Provider の契約 | `internal/language/provider.go` | `Extraction`（`SymbolDraft`、`ReferenceDraft`、`ImportDraft`、`Bindings`、`Exports`、`TypeFacts`、`Diagnostics`）と、解決の方式を決めるフラグ（`ModuleScoped`、`PackageScoped`、`IdentityOnly`） |
| 登録 | `internal/languages/languages.go` `specList()` | 唯一の構成ルート。文法と SupportLevel を一括登録 |
| Parser | `internal/tsparse` | gotreesitter v0.55.1 の上の 3 段 fallback（production → candidate → forest）と `ParserPool`。forest probe は Go と Rust だけ |
| Resolver | `internal/resolver` | 言語中立。`resolveReference`（`resolver.go`）の順: Dynamic → R0（`NameQualified` / `ReceiverTypeQualified`、`identity.go`、継承は `inheritance.go`）→ `IdentityOnly` → `PackageScoped`（`packagescope.go`、Go の `TypeFacts` / `ReceiverChain` もここ）→ 名前による段（同じコンテナ → 同じファイル → binding（`modules.go`）→ legacy import の部分一致 → receiver 名 → 同じディレクトリ → リポジトリで一意 / 候補集合） |
| Confidence | `resolver/confidence.go`、`result.go` | `ConfidenceCap` は下げる方向だけ。graph の edge になるのは `HasUniqueTarget`（候補 1 件かつ Strong 以上）だけ |
| Index | `internal/index` | `NewFileIndex`、`uniqueReferences`、`dropSpeculative`、edge と completeness |

### A.2 言語別

| 項目 | Go | TS / TSX | JavaScript | Python | PHP | Terraform | Rust |
|---|---|---|---|---|---|---|---|
| SupportLevel | context_quality_certified | context_quality_certified | references | references | graph | graph | graph |
| Provider の規模（実装 / テストの行数） | 2,722 / 5,001 | 1,881 / 2,643 | 293 / 112 | 274 / 128 | 2,231 / 3,889 | 1,405 / 2,721 | 2,459 / 2,516 |
| CacheVersion | go-9 | ts-6 | 4 | 4 | php-14 | terraform-7 | rust-4 |
| Symbol | func / method / type / const / var | トップレベル + class のメンバー | トップレベルの function / class / let / const のみ | トップレベルの def / class のみ | namespace / class / interface / trait / enum / function / const + メンバー | resource / module / variable / output など | item / impl / method / field / macro など |
| Reference | call / construction / type_use / value_reference / write | call / construction / type_use / heritage / JSX | call / construction のみ | call のみ | call / construction / read / type_use / heritage / uses_trait | value_reference / depends_on | call / type_use / value / import / macro |
| 解決 | PackageScoped + TypeFacts / ReceiverChain | ModuleScoped + Binding / Export、ReceiverType | **名前による段だけ** | **名前による段だけ** | R0（クラス）+ 継承 lookup。関数は名前による段 | IdentityOnly + R0 | IdentityOnly + R0 + alias |
| 型の証拠 | TypeFacts / ReceiverType / ReceiverChain | ReceiverType | なし | なし | ReceiverType（typed param、nullable、一度の `new`、ctor 注入） | なし | なし |
| cap / Dynamic / Speculative | cap（shadow）、Speculative | なし | なし | なし | cap（`static::`）、Dynamic | なし | cap、Dynamic |
| 独立 Oracle | go/types（参照単位） | tsc（参照の有無だけ。宣言の identity は比べない） | なし（今回作成） | なし（今回作成） | **なし** | なし | rustc save-analysis |

根拠: 各 `internal/languages/<lang>/provider.go`、`docs/language-support.md`、
`internal/conformance/IMPROVEMENTS.md`。

### A.3 付随して見つかったこと

- インストール済みの MCP サーバーが `get_language_support` に rust を出さなかったのは、
  rust 追加（f34d1f6）より前に起動した古いプロセスのためで、HEAD は rust を
  graph として登録している（`languages.go`、`TestOneRegistrationInvariant_Rust`）。
- `internal/languages/php/provider.go` の package コメントが「imports / references /
  resolution は未実装」のまま古い。

## B. PHP Baseline

### B.1 意味論の対応状況

「Fully」は unit テストと監査の fixture で確かめた範囲の分類で、実コーパスでの
精度ではない（PHP には Oracle が無い）。

| 項目 | 分類 | 根拠 |
|---|---|---|
| Namespace、use（クラス） | Fully | `names.go`、`references.go`。`TestQualifiedNames_*`、`TestQualifiedIdentity_*` |
| use function / use const | Partially | ImportDraft は出すが解決に使わない（`names.go` に「out of scope」と明記） |
| Class / Interface / Trait（宣言と関係） | Fully | `TestPHPRef_Relations`、`TestPHP_TypedRelationEdges` |
| Trait adaptation（insteadof / as） | Conservatively unresolved | `resolver/inheritance.go` が lookup を止める。`TestPHPRef_TraitAdaptationKeepsBaseRelation` |
| 継承 | Fully（リポジトリ内） | `inheritance.go`（own → trait → parent → interface）。`TestInheritance_*` |
| メソッド解決（instance） | Partially | 型の証拠がある receiver だけ Exact、無ければ Candidate |
| static メソッド、`self::` / `parent::` | Fully | 継承元に Exact |
| `static::` | Partially | final でなければ Strong に cap。`new static`、trait 内の `self::` / `static::` は Unresolved |
| Property | Partially | 宣言は抽出、**読み書きの参照は抽出しない** |
| Function | **Partially（偽 Strong あり）** | 呼び出しが名前空間を無視した名前の段で解決される（下記） |
| Constant | Partially | クラス定数は Exact。グローバル定数の参照と `define()` は抽出しない |
| Constructor / promoted property | Fully | `TestPHPProvider_DestructorNotConstructor` ほか |
| Anonymous class | Unsupported | Symbol なし。本体の参照も失われる |
| Closure / arrow function | Partially | 本体は外側のメソッドに帰属、型の証拠は無効化 |
| 型宣言、戻り値型、nullable | Fully | `TestPHPRef_Types` |
| Union / Intersection | Fully（type_use）/ Conservatively unresolved（receiver） | |
| 動的呼び出し | Conservatively unresolved | `Dynamic=true`。`TestDynamicCalls_*` |
| Cross-file | Fully（クラス）/ Partially（関数） | |

### B.2 評価基盤

| 種類 | 有無 |
|---|---|
| unit | あり（122 関数、サブテスト込み 310 件 PASS） |
| golden | **なし**（`internal/golden/testdata` に php が無い） |
| 合成コーパス | あり（`corpus_test.go`、10 ファイル） |
| 実コーパス | **リポジトリに無い** |
| 参照単位の Oracle | **なし**（`php` も phpstan も手元に無い） |
| ゲート | あり（`TestQualifiedIdentityGates`: edge 数の固定、負例、25 回の決定性） |
| mutation | リポジトリに無い |
| fuzz | あり（`FuzzExtractPHP`） |
| context quality | あり（11 シナリオ、recall 1.00 / irrelevant 0.00） |

**「PHP 100点」は再現できない。** git 履歴、リポジトリ、記録のどこにも、その数を出す
計測は無い。100% と言える数は context quality の 11 シナリオ（作者が用意した小さな
fixture で必要な文脈が揃うか）だけで、解決の精度や Recall を測るものではない。
「clinic.admin 61→95」も、手元に無いリポジトリへの主観的な採点だった。

### B.3 実測（同じ道具: `fidelity-baseline.sh <corpus> <out> php`）

| | guzzle 7.9.2 | slim 4.12.0 |
|---|---|---|
| ファイル / Symbol / 参照 | 41 / 429 / 1,458 | 72 / 636 / 1,330 |
| Exact / Strong / Candidate / Unresolved | 309 / 4 / 66 / 1,079 | 410 / 13 / 46 / 861 |
| OutsideRepository / Dynamic | 296 / 50 | 238 / 19 |
| 抽出 / 解決の時間 | 288 ms / 2 ms | 177 ms / 5 ms |

- guzzle の Strong 4 件はすべて誤り: 先頭 `\` の付いたグローバル関数
  `\json_encode()` / `\json_decode()`（各 2 件）が `GuzzleHttp\json_encode` /
  `GuzzleHttp\json_decode` に結ばれる（`src/Cookie/SessionCookieJar.php:57,68`、
  `src/Utils.php:275,296`）。`docs/language-support.md` は「Function and constant
  names are resolved conservatively」と書いていたので、この監査で既知の誤りとして
  書き直した。原因は `php/references.go` の `emitFunctionCall` が
  名前空間と `\` を捨て、`NameQualified` なしで参照を出すこと（名前による段の
  same_package / unique_repo_match で Strong になる）。
- Oracle が無いので Precision / Recall は **測定不能**。コーパスの取得元と
  ハッシュも記録が無い（以前のセッションの作業領域にあったもの）。

### B.4 品質基準としての妥当性

PHP は「保守的な設計の見本」としては妥当（クラス名の lexical な Exact、継承 lookup
の停止規則、Dynamic と cap、負例ゲート）。しかし **数値で示せる基準ではない**
（Oracle・golden・実コーパスが無く、関数解決に偽 Strong がある）。そのため本監査は、
「PHP 相当」を **設計上の到達点** として定義する:

1. メンバー（メソッド・プロパティ）が Symbol で、Parent / Receiver を持つ
2. 名前の束縛（import / use）が言語の規則で Exact に解決される
3. receiver の型を、宣言・支配する代入から証明できる範囲で使う
4. 継承をたどる lookup があり、不明な参加者で止まる
5. 動的な参照を Dynamic として記録する
6. 負例ゲートと決定性のテストがある

そして数値の基準は言語共通の Level 1 指標（False Strong = 0、Recall）で置く。
PHP 自身も Tier 1 Oracle を作って同じ物差しで測り直す（ロードマップの M1。`README.md` の「ロードマップの状況」）。

## C. Python

### C.1 現状

- Provider（`internal/languages/python/provider.go`、274 行）は、モジュール直下の
  `def` / `class`（デコレータ付きを含む）と call と import のパスだけを出す。
  メソッド、ネストした関数、`if` / `try` の中の定義、type_use / read / 属性参照、
  Bindings / Exports、スコープ、型の証拠、cap、Dynamic は無い。
- 解決は名前による段だけ: 同じファイルで Exact、legacy import のパスの部分一致で
  Exact、同じディレクトリで Strong、リポジトリで一意なら Strong。

### C.2 確認した根本原因

| ID | 内容 | 位置 |
|---|---|---|
| PY-1 | receiver が単純な識別子でない呼び出し（`self.repo.save()`、`super().save()`、`a.b.c()`、`f().g()`）を、receiver の無い素の名前の呼び出しとして記録し、untyped receiver の cap を逃れる | `provider.go:232-258` |
| PY-2 | `from .util import helper` で、import した名前（`helper`）を module として記録する。`from .m import *` は何も記録しない | `provider.go:195-210` |
| PY-3 | スコープが無い。引数・ローカル・内包表記・walrus・except・match の束縛や builtins を無視し、同じファイル（Exact）や一意名（Strong）に解決する | resolver の名前の段 |
| PY-4 | import 名と同じ receiver（`json.loads`）は module receiver とみなされ cap されず、同じファイルや一意名の同名宣言に解決される | `resolver.go`、`receivers.go` |
| PY-5 | legacy の import 照合がパスを部分文字列で比べる（`helper` が `helpers_pkg/tool.py` に一致） | `resolver.go` の `importMatch` |
| PY-6 | メソッド内の参照の Container が、同名のトップレベル関数に結び付く。誤りの実体は caller 側の edge（`index/builder.go` の `containerSymbol` は候補が 1 件だと包含を確かめない）。ネストした関数やクラス本体の中の呼び出しは Container を失う。Oracle は呼び先しか見ないので **数値に現れない**（未測定） | `provider.go:138-143`、`index/builder.go:557-562` |
| PY-7 | import 参照そのものが名前で解決され Strong / Exact になる（attrs の `import attrs` 7 件が `src/attr/_make.py:attrs` に）。Container が無いので edge にはならないが、references の出力に Strong で出る。Oracle は import 文を site にしないので採点外（`import_strong`: requests 38、flask 88、click 90、attrs 31） | PY-2 と同じ |

### C.3 意味論の分類（fixture: 14 ファイル、pyright と jedi で正解を確認）

| 項目 | 分類 |
|---|---|
| LEGB / shadowing、comprehension、walrus、match、except の束縛 | **Unsupported（誤った Strong / Exact を出す）** |
| local / global / nonlocal、ネスト関数、closure | Unsupported |
| class body scope、メソッド、classmethod / staticmethod、property | Unsupported（メソッドが Symbol にならない） |
| import X / from import / alias | Partially（**外部名を同名のリポジトリ宣言に Strong にする**） |
| 相対 import | **誤り**（PY-2） |
| package / `__init__.py` の再エクスポート / star import / namespace package | Unsupported（一意名で偶然当たるだけ） |
| 条件付き import / 定義 | Unsupported（`if` の中の `def` は抽出されない） |
| 継承、多重継承、MRO、`super()` | **Unsupported（`super().save()` が無関係な関数に Strong）** |
| decorator | Partially（`@d(...)` は call、素の `@d` は記録しない） |
| descriptor、metaclass、Protocol、注釈 | Unsupported |
| getattr / setattr、monkey patch、実行時の属性注入 | Unsupported（外側の呼び出しを記録しない。属性の連鎖は PY-1 で cap を逃れる） |
| dynamic import（importlib） | **Unsupported（誤った Strong）**: `importlib.import_module("pkg.util").helper()` が `pkg/util.py:helper` に same_package で Strong（PY-1 と同じ形） |

### C.4 Baseline（pyright 1.1.414、依存を固定した Python 3.12.15 の venv）

手順は [evaluation.md](evaluation.md) §9。Coverage = 表現可能 / declaration
（モジュールを指す site は `module` として declaration から除く）。

| コーパス | 参照 | 表現可能（Coverage） | 回復 | Resolution recall | 自動の False Strong / Exact | Strong precision | `unknown_strong` / `declaration_file_strong` | 抽出漏れ | `import_strong` |
|---|---|---|---|---|---|---|---|---|---|
| requests 2.32.3 | 2,838 | 781（15.2%） | 640 | 81.9% | **181**（local 166、external 15） | 78.0% | 22 / 0 | 135 | 38 |
| flask 3.0.3 | 4,426 | 924（12.0%） | 486 | 52.6% | **39**（local 20、external 19） | 92.6% | 18 / 0 | 357 | 88 |
| click 8.1.7 | 4,689 | 1,780（20.2%） | 1,130 | 63.5% | **50**（exact 3、local 8、external 37、ambiguous 2） | 95.8% | 17 / 0 | 453 | 90 |
| attrs 24.2.0 | 4,356 | 1,036（14.4%） | 697 | 67.3% | **60**（local 60） | 92.1% | 217 / 88 | 299 | 31 |

- **自動の False Strong は下限。** `unknown_strong` / `declaration_file_strong` の人手分類は
  §C.4.1。False Outside は 0（Python の Provider は Outside を出さない）。
- **Recall は Ark が表せる site（declaration の 12〜20%）の上の値。** メソッド・ローカル・属性は
  Ark のシンボルでないので分母に入らない。
- 誤りの主因（Reviewer A の分類）: requests はテスト関数の引数 `httpbin`（150 件）と
  `httpbin_secure`（8 件）による shadowing（PY-3）、`requests.codes.get` や
  `self.lookup_dict.get` が `api.py:get` に結ばれる属性の連鎖（PY-1）。click は builtin の
  `open`（PY-3）、`pop` / `copy` / `isatty`（PY-1）、`click.edit` / `clear` / `pause` の
  Exact（PY-4）。flask は `current_app.x` と `super().loads`（PY-1）、`_json.x`（PY-4）、
  pytest の fixture 引数 `purge_module`。attrs はテスト関数の中で同名のクラスを繰り返し
  定義する shadowing。
- **最初の計測は pyright の Python 環境が暗黙（3.9.4、依存なし）だった**（Reviewer A の
  指摘）。依存を入れると flask の external の誤りが 9 → 19、requests の `unknown` が
  2,198 → 924 になった。数値はすべて依存を入れた後のもの。
- attrs の `declaration_file` 143 site は、実装を持たない `.pyi` だけに束縛された site。
  スタブと実装の組（`m.pyi` と `m.py`）は実装で採点する。
- 時間: 抽出 233〜312 ms、解決 13〜22 ms（各 30〜80 ファイル）。Django 4.2.16
  （2,761 ファイル、参照 172,370）は抽出 7.3 秒、解決 2.4 秒、全体 13 秒
  （`fidelity-baseline.sh <django> <out> python`、Oracle なし）。
- 決定性: 4 コーパスとも `report.sites` が 2 回の実行で一致。pyright の出力も
  requests で 2 回一致。

#### C.4.1 判定不能な Strong の人手分類

`unknown_strong` / `declaration_file_strong` の site を分類した（attrs は 305 件から
無作為に 40 件、seed 20261013）。分類は名前と receiver の形による規則（作業領域の
`classify.py`）で行い、各コーパスの代表例を原ソースで目視して確かめた。全件を
1 件ずつ読んだものではない。

| コーパス | 対象 | 誤り | 正しい | 判定不能 | 誤りを足した False Strong / Exact |
|---|---|---|---|---|---|
| requests | 22 | 10 | 12 | 0 | **191** |
| flask | 18 | 17 | 1 | 0 | **56** |
| click | 17 | 5 | 12 | 0 | **55** |
| attrs | 40（305 件の標本） | 1 | 39 | 0 | **61 以上**（標本の誤り 1 件。全体への推定は約 8 件で、約 68） |

- 誤りの多くは PY-1（属性の連鎖で receiver が落ちる）: `resp.cookies.get(...)` が
  `api.py:get` に（正しくはリポジトリ内の `RequestsCookieJar.get`）、flask の
  `app.config.update(...)` 13 件がサンプルアプリの view 関数 `blog.py:update` に、
  `app.json.dumps(...)` がモジュール関数 `json/__init__.py:dumps` に（正しくは
  `DefaultJSONProvider.dumps`）、click の `_local.stack.pop()` がテストの `pop` に。
- 外部の名前（PY-4）: flask の `docs/conf.py` の `get_version`（`pallets_sphinx_themes` から
  import しているのに `cli.py:get_version` に）、click の `webbrowser.open(url)` がサンプルの
  `open` に。
- attrs の `attrs.frozen` が `setters.frozen` に（正しくは `_next_gen.py` の
  `frozen = partial(...)`。変数の束縛を Ark が宣言として抽出しない）。click の
  `click.get_binary_stdin()` は `click/__init__.py` が公開しない名前で、正しい解決先は無い。
- 正しいもの: `requests.auth.HTTPDigestAuth(...)` などモジュール名を receiver にした呼び出しや、
  from-import した名前の呼び出しは、ほぼ正しかった。

### C.5 Oracle 候補

| 候補 | 試用 | 宣言の identity | 参照単位 | cross-file | 動的・MRO | 出力 | License | 判定 |
|---|---|---|---|---|---|---|---|---|
| pyright 1.1.414 | した | ○ | ○ | ○（相対、alias、namespace、再エクスポート） | `super`・MRO・match ○、getattr は空 | LSP の JSON | MIT | **主 Oracle** |
| basedpyright 1.40.2 | した | pyright と同じ | 同 | 同 | 同 | 同 | MIT | 代替（リポジトリ内の解決は pyright と完全一致） |
| jedi 0.19.2 | した | ○ | ○ | ○ | `super`・MRO ○。match は実行する Python の版に依存 | Python API | MIT | 補助。候補の並びが非決定的（ソートが必要） |
| ast + symtable | した | ×（スコープの分類だけ） | 名前とスコープ | × | — | Python API | PSF | Series 1（スコープ）の補助。3.12 が必要 |
| libcst 1.8.6 | した | 修飾名の文字列だけ | ○ | 修飾名 | MRO なし | Python API | MIT | 不採用 |
| mypy 1.19.1（dmypy inspect） | 一部 | 一部の位置で取れた | 列の規約に癖 | ○ | 未検証 | テキスト | MIT | 不採用 |
| pytype、rope | していない | — | — | — | — | — | — | 未確認 |

pyright を選んだ理由: 決定的で、MRO・`super`・match・alias・相対 import・
namespace package・条件付き定義（複数の宣言を返す）を正しく扱い、同梱の typeshed
で標準ライブラリや外部ライブラリを外部と判別できる。Python の実行時の振る舞いを
決めるものではないので、`getattr` などには答えない（`unknown` に数える）。

### C.6 Go Phase 2 の手法の再利用

- 再利用できる: `ConfidenceCap` と untyped receiver の cap、`Speculative`
  （`x.f` が属性かもしれない場合）、shadow の cap（Go の `scopes.go` の考え方）、
  一度だけの代入に限った `ReceiverChain`、関数の戻り値やクラス属性の注釈による
  `TypeFacts`（ただし注釈は実行時に強制されないので上限 Strong）、Oracle の
  Series 1 / 2。
- 再利用できない: `PackageScoped`（ディレクトリ = パッケージの前提）、go/types の
  Oracle、「変数の型が変わらない」前提、embedding による昇格（Python は MRO）、
  interface メソッドを静的な呼び先とする扱い（Protocol は構造的な型）、
  大文字による export の規則。

## D. JavaScript / TypeScript

### D.1 現状

- JavaScript の Provider（`internal/languages/javascript/provider.go`、293 行）は、
  トップレベルの function / class / let / const（とその export 形）、call と new、
  `import` 文のパスだけを出す。`var`、class のメンバー、ネスト関数、object literal、
  `export … from`、`require`、`import()`、Bindings / Exports、スコープ、Dynamic は無い。
  `ModuleScoped=false` なので、名前による段が全部効く。
- TypeScript の Provider は `moduleSpec`（`modules.go`）で Bindings / Exports /
  ExportAll / 名前空間 import を出し、`ModuleScoped`、class メンバー、
  ReceiverType（注釈、`this`、`new T()`、`this.field`）を持つ。

| 観点 | JavaScript | TypeScript / TSX |
|---|---|---|
| Parser | tsparse + tree-sitter-javascript | 同じ tsparse（`generic_call.go` で誤パースを補正） |
| Scope | なし | `scope{container, thisClass, props, vars, tparams}`。shadowing は receiver 型の無効化にしか使わない |
| Module | パスの部分一致（legacy） | `moduleSpec` の候補 + Bindings / Exports、ModuleScoped |
| Symbol identity | トップレベルのみ | `Class.member`、Parent / Receiver |
| Reference | call / new のみ（範囲は callee 全体、`new` は式全体） | call / new / type_use / heritage / JSX（範囲はプロパティの名前） |
| 型の証拠 | なし | 注釈、`this`、`new T()` |
| Tests | 4 件 | semantics / graph / context / fuzz / gates / corpus / compiler fidelity |
| Oracle | なし（今回作成） | tsc（`.github/ts-oracle`）。参照の **有無** を比べる parser fidelity で、宣言の identity は比べない |
| 名前空間 | JS と TS は別。JS↔TS の参照は解決されない（docs の known gap） | |

### D.2 意味論の分類（fixture: JS 11 ファイルと TS の双子）

| 項目 | 分類 |
|---|---|
| トップレベルの function / class / let / const、hoisting | Fully |
| `var` | Unsupported |
| shadowing（引数、ローカル、ネスト関数、catch） | **Unsupported（誤った Strong / Exact）** |
| 分割代入、default / rest 引数 | Conservatively unresolved |
| ESM named / default import | Partially（同じディレクトリに同名があると誤った Strong） |
| import alias、namespace import | Conservatively unresolved / Candidate |
| 再エクスポート | Unsupported |
| bare specifier | **誤り**（`import * as lib from 'lib'; lib.helper()` が `lib/helpers.js` に Exact（legacy の import 照合の部分一致）、`import util from 'util'; util.format()` が `lib/format.js` に Strong） |
| CommonJS（require / module.exports） | Unsupported（名前の一致だけで Strong） |
| dynamic `import()` | Candidate（誤った target） |
| class のメンバー、extends、super、`this.m()`、static、prototype、object literal | Conservatively unresolved（メンバーが Symbol でない） |
| JSX のコンポーネント | Unsupported（`.jsx` で参照 0） |
| computed access（`o[k]()`） | Conservatively unresolved（参照を出さず、Dynamic の印も無い） |
| `Object.assign` による mixin、Proxy、eval | Conservatively unresolved / Unknown |
| receiver 名が import パスの末尾と一致（`options.unmount()`） | **誤った Exact** |

**TypeScript（認定済み）でも見つかった問題**（TS 5.6.3 で照合。再レビューの fixture を含む）:

- `export` と `function f()` を別の行に書くと、`f()` の呼び出しが OutsideRepository に
  なる（**誤った Outside**）。

- 引数やローカル宣言による shadowing が、import binding や same_file を根拠に
  **誤った Exact** になる。docs にも `IMPROVEMENTS.md` にも記載が無い。
- object literal を export した定数を module binding の receiver として呼ぶと
  （`Cache.get()`）、OutsideRepository になる（**誤った Outside**。JS を TS の
  Provider にかけた実験で three に 42 件）。

### D.3 Baseline（TypeScript 5.6.3 の checker、`allowJs`）

| コーパス | 参照 | 表現可能（Coverage） | 回復 | Resolution recall | 自動の False Strong / Exact | Strong precision | `unknown_strong` / `declaration_file_strong` | 抽出漏れ |
|---|---|---|---|---|---|---|---|---|
| lodash-es 4.17.21 | 3,432 | 2,324（23.4%） | 1,038 | 44.7% | **38**（local） | 96.5% | 0 / 0 | 1,286 |
| express 4.21.2 | 659 | 69（3.9%） | 38 | 55.1% | **1**（local） | 97.4% | 0 / 0 | 31 |
| preact 10.26.4（`dist/` 除く） | 651 | 598（18.8%） | 175 | 29.3% | **10**（local） | 94.6% | 21 / 5 | 410 |
| three 0.170.0（`package/src`） | 20,689 | 10,428（14.6%） | 4,627 | 44.4% | **18**（local） | 99.6% | 0 / 0 | 5,611 |

- 自動で数えた誤りはすべて shadowing（lodash の `iteratee` など）。JS 調査担当が別の
  方法で数えた数（lodash 38、three 18）と一致する。ただし lodash の 1 件
  （`lodash.default.js` の `var mixin`）は、モジュールレベルの `var` を Ark が抽出しない
  ことによる。False Outside は 0（JS の Provider は Outside を出さない）。
- 判定不能な Strong（preact）の人手分類は §D.3.1。`options.unmount(...)` が同じ
  ファイルの関数 `unmount` に Exact、`options._catchError(...)` の Strong 4 件（debug /
  hooks が実行時に差し替える）は、Oracle が `.d.ts` のプロパティに束縛するので自動では
  数えられない。
- 抽出漏れの多くは、Ark が call と new しか参照にしないため（`export default x`、
  shorthand `{ x }`、読み出しは site だが Ark の参照ではない）。Resolution recall は
  「Oracle が参照と見なす site」の上の値で、call だけの値ではない。
- express は CommonJS なので、表現可能な site が少ない（require の束縛を Ark が
  モデル化していない）。
- 生成物を含めると誤りが桁違いに増える（three の `package/` 全体で local 7,585 と
  external 149、preact の `dist/` を含めると 148）。minify されたバンドルは 1 文字の
  ローカル名がトップレベル名を隠すため。
- Oracle の改訂（レビュー後）: alias の宣言名、class / function 宣言の後の static の
  代入を宣言 1 つとして扱う、解決できない bare specifier の import を external に、
  コーパス自身のパッケージ名の import は external にしない、BOM と U+2028 の位置の補正、
  `C.X = 1` のような後付けの代入（TypeScript は識別子 `C` を宣言として返す）を除く、
  shorthand / `export default` を site に、JSX の閉じタグを site から除く。
- 時間: three/src（678 ファイル）で抽出 1.3 秒、解決 41 ms。Oracle の生成は約 20 秒。
- 決定性: 4 コーパスとも `report.sites` が 2 回の実行で一致。Oracle の出力も lodash と
  three/src で 2 回一致。

#### D.3.1 判定不能な Strong の人手分類

preact の `unknown_strong` / `declaration_file_strong` 26 件: 誤り 3、正しい 19、判定不能 4。
誤りを足した False Strong / Exact は **13**。

- 誤り: `compat/src/portals.js` の `render(...)` 2 件（`import { render } from 'preact'` なのに、
  同じパッケージの `compat/src/render.js:render` に same_package で Strong。正しくは
  `src/render.js:render`）、`src/diff/index.js:636` の `options.unmount(vnode)`（同じファイルの
  関数 `unmount` に Exact。実際は hooks / compat が代入するフック）。
- 判定不能: `options._catchError(...)` 4 件（初期値は Ark の解決先と同じだが、debug /
  compat が実行時に差し替える）。
- 正しい: `'preact'`、`'preact/hooks'`、`'preact/compat'` の自己参照 import を通した
  `toChildArray`、`createElement`、`useState` など。

### D.4 JS / TS の共通化の判断

- **共通化すべき（Soundness が上がる。ES の構文と意味が同じ）**: lexical scope と
  shadowing の解析（両方に欠けている）、ESM の import / export / 再エクスポートの
  抽出（`moduleSpec` の候補拡張子だけ言語ごと）、class メンバーの抽出と
  `this` / `new T()` の証拠、call / new / JSX の参照の形。
- **分離すべき**: 型注釈の証拠（TS のみ）、JSDoc（JS のみ。`checkJs` なしでは強制
  されないので cap 付き）、CommonJS の意味論、script と module の判定、
  `generic_call` の補正（TS のみ）、文法（TS の文法は `.js` の JSX を読めない）、
  名前空間（JS を TS の Dialect にしない。JS↔TS は明示的な binding で扱う）。
- **TS の Provider を JS にそのまま使う案は採らない。** 実験（JS を `.ts` に改名して
  TS の Provider にかけた）では lodash の誤りが 38 → 0 になったが、TS 自身の欠陥
  （shadowing、object literal の誤った Outside）を持ち込む。
- 推奨: `internal/languages/internal/ecmascript`（仮）に scope / module / class の
  抽出を置き、`typed` のフラグと拡張子表で JS と TS を分ける。重複を減らすことだけを
  目的にした共通化はしない。shadowing の修正は TS にも同時に効く。

### D.5 Oracle 候補

| 候補 | 版 / License | 参照ごとの宣言 | cross-file | CommonJS | 判定 |
|---|---|---|---|---|---|
| TypeScript Compiler API（`allowJs`、`getSymbolAtLocation` → alias を解く → declarations） | 5.6.3 / Apache-2.0（CI の TS Oracle と同じ版） | ○ | ○ | ○ | **主 Oracle**。three/src の 10 万 site で約 20 秒、2 回の出力が一致 |
| TS Language Service | 同 | ○（同じ checker） | ○ | ○ | 未試用（Compiler API で足りる） |
| eslint-scope 8.4.0 + espree 10.4.0 | BSD-2-Clause | ○（単一ファイル） | × | × | Series 1（スコープ）の相互検証 |
| Babel（`scope.getBinding`）7.28.0 | MIT | ○（単一ファイル） | × | × | eslint-scope と同等 |
| oxc-parser 0.72.0 | MIT | ×（semantic の API が無い） | — | — | 不採用 |
| SWC | — | — | — | — | 未試用 |

注意: checker は代入や JSDoc から member の型を推論する。Ark が構文で証明できない
推論を含むので、member の site で Ark が Unresolved なのは Recall の損失として
だけ数え、Ark の Strong を同じ推論で正当化しない。

## E. 横断して見えたこと

1. **最大の品質問題は全言語に共通する「名前だけの Strong / Exact」。** Python と
   JavaScript は抽出が浅いことより、名前による段（同じファイルで Exact、同じ
   ディレクトリ・一意名で Strong）が shadowing と外部名を無視することが問題。
   PHP の関数、認定済みの TypeScript の shadowing も同じ形。
2. **Oracle で測れる状態になった。** Python と JavaScript は、独立した Oracle で
   誤りを 1 件ずつ列挙できる（`report.false`）。PHP は Oracle が無いままなので、
   基準としては Tier 1 Oracle を先に作る必要がある。
3. **HTML の Parser に、ERROR も診断も出さずに木を途中で切る問題がある**
   （実装前の設計案。アーカイブ、`README.md`）。既存の言語では、実コーパス約
   6,500 ファイル（Python 3,008、JS 1,659、Go 1,768、PHP 79、TS 53、Rust 17）で
   同じ現象は 0 件だった（「エラーがあるのに ERROR / MISSING ノードが無い」0、
   「エラーノードなしで木が入力より短い」0）。
