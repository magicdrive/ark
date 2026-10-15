# Verification

> Phase 0〜9 の検証の記録（履歴）。ここが参照する `baseline.md`・`acceptance-criteria.md`・進捗の台帳は
> アーカイブした（`README.md`）。Phase 3・5 のパーサーの比較は、`decisions.md` の D19・D24 の根拠である。

## Oracle とコーパスの版

| 物 | 版 |
|---|---|
| TypeScript（JS / TS の Oracle） | 5.6.3（`javascript-tsc.js`） |
| node | v22.17.0 |
| Go | go1.27.2 darwin/amd64 |
| コーパス | `baseline.md` の表 |

## Phase 0

- `go vet ./...`: 通過。`go test ./internal/index/ .`: 通過（監査成果物のコミット前に再確認）。
- `measure.sh p0-baseline`: `baseline.md` の表。JS の値は監査と一致。
- 採点上の不一致の分類:
  - rxjs `local_fp` 2 件（`internal/testing/SubscriptionLoggable.ts:10,19` の `now`）: Ark は
    `Scheduler.now`（static と instance を 1 つにした TS のシンボル）に Exact。Oracle の宣言は
    instance の `now`（40 行目）で、シンボルの開始行（static の宣言）と合わないため表現不能。
    Ark の解決先の表現の制限で、誤った解決ではない（TS の既知の制限）。
    **Phase 2 で訂正**: Ark のシンボルは static の宣言（26 行目）の位置で、解決先は static のメンバーだった。
    インスタンスの `this.scheduler.now()` を static の宣言に結ぶのは誤り（D14 で修正、Phase 2 の FS は 0）。

## Phase 1（JavaScript / TypeScript のスコープと ESM）

### 計測（`measure.sh p1-d`、固定の Oracle、Phase 0 と同じマシン）

| corpus | RSS MB | 抽出 ms | 解決 ms | 表現可能 | Coverage | 回復 | Recall | FS | FO | unknown_strong | decl_file_strong | Candidate hit | 抽出漏れ |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lodash | 60 | 250 | 7 | 3,560 | 35.8% | 1,235 | 34.7% | **0** | **0** | 0 | 0 | 0 | 2,325 |
| express | 31 | 52 | 1 | 229 | 12.9% | 78 | 34.1% | **0** | **0** | 0 | 0 | 0 | 151 |
| preact（dist 除く） | 32 | 73 | 1 | 605 | 19.0% | 188 | 31.1% | **0** | **0** | 1 | 0 | 0 | 417 |
| three/src | 203 | 1,357 | 80 | 14,056 | 19.7% | 6,707 | 47.7% | **0** | **0** | 0 | 0 | 897 | 5,625 |
| jsfix | 22 | 16 | 0 | 34 | 59.6% | 17 | 50.0% | **0** | **0** | 0 | 0 | 0 | 14 |
| jsfix2 | 21 | 14 | 0 | 1 | 20.0% | 0 | 0% | **0** | **0** | 0 | 0 | 1 | 0 |
| rxjs（TS） | 80 | 239 | 10 | 3,247 | 35.6% | 1,669 | 51.4% | 2※ | **0** | 0 | 0 | 224 | 1,258 |

※ rxjs の FS 2 件は Phase 0 で分類した採点上の不一致（`Scheduler.now`）。

- Baseline からの変化: JS の FS 38 / 1 / 10 / 18 → 0 / 0 / 0 / 0、rxjs の FO 13 → 0。
- Recall の分母（表現可能）が増えた（クラスのメンバーと `var` がシンボルになった）ので、Recall の率は
  旧値と比べない。比べるのは Series 1。

### Series 1（Baseline と変更後の両方で表現可能な site）

| corpus | Series 1 | 回復 Baseline → 変更後 | 失った正解 | 新たな正解 | Series 2（新しく表現可能） | その回復 |
|---|---|---|---|---|---|---|
| lodash | 2,324 | 1,038 → 1,038 | 0 | 0 | 1,236 | 197 |
| express | 69 | 38 → 38 | 0 | 0 | 160 | 40 |
| preact | 598 | 175 → 188 | 0 | 13 | 7 | 0 |
| three/src | 10,428 | 4,627 → 5,187 | 3 | 563 | 3,628 | 1,520 |
| jsfix | 29 | 7 → 14 | 0 | 7 | 5 | 3 |
| jsfix2 | 1 | 1 → 0 | 1 | 0 | 0 | 0 |
| rxjs | 3,247 | 1,669 → 1,669 | 0 | 0 | 0 | 0 |

失った正解（全件）:

- three `nodes/accessors/InstanceNode.js:55` `buffer(...)`: 同じメソッドの別のブロック（59 行目）で
  `const buffer` を宣言している。スコープの過大近似で Candidate（正解は候補に含まれる）。
- three `nodes/tsl/TSLCore.js:169` `nodeObject(...)`: 同じ関数の別のブロック（154 行目）で
  `let nodeObject` を宣言している。同上。
- three `nodes/procedural/Checker.js:4` `uv()`: デフォルト引数 `[coord = uv()]` の `uv` が、同じ
  アロー関数の本体の `const uv` によってローカルとみなされる（過大近似。Reviewer A の指摘で訂正。
  最初の記録の「`export *` の曖昧さ」は誤り）。
- jsfix2 `app/z.js:2` `render()`: module のファイルから classic script のグローバル関数を呼ぶ。
  module-scoped の cap で Candidate（script が読み込まれることは証明できない）。

### 判定不能な Strong の分類

- preact `compat/src/suspense-list.js:62` `suspended(...)`: `import { suspended } from './suspense.js'` が
  `suspense.js:235` の `export function suspended` に Exact。**正しい**（Oracle は隣の `suspense.d.ts` に
  解決して判定不能になったと推測）。

### 監査で人手分類した誤り（P1-3）

- preact `src/diff/index.js:636` `options.unmount(vnode)`: Exact → Unresolved。
- preact `compat/src/portals.js:23,60` `render(...)`: Strong → Unresolved（`'preact'` の bare
  specifier なので外部。D8 の既知の制限）。

### 決定性

- `measure.sh` を 2 回（p1-d、p1-d2）実行し、7 コーパスすべてで `report.sites` と `report.scored` が一致。

### テストと静的解析

| 検証 | 結果 |
|---|---|
| `gofmt -l` | 出力なし |
| `go vet ./...` | 通過 |
| staticcheck（CI と同じ範囲: cache, context, index, resolver, language, languages, mcp, symbol, reference, repomap, impact, search） | 通過。`./...` 全体では、今回触れていない既存のパッケージ（core, model, skill, spinner）に指摘がある |
| `go test ./...` | 通過 |
| `go test -race ./...` | 通過 |
| TS コンパイラ Oracle テスト（`ARK_TYPESCRIPT_MODULE` = 5.6.3、`ARK_TS_ORACLE_REQUIRED=1`、`ARK_TS_FIDELITY_ROOTS` は CI と同じ `internal/{golden,languages/typescript,mcp}/testdata`） | 63 件 PASS、SKIP 0（`verify-p1b/tsoracle.log`）。別に rxjs だけを対象に `TestFidelity_TypeScriptCompiler` を実行: 一致 2,937 件と偽の構築参照 2 件（`new WebSocketCtor!(...)`）で、Phase 0 のコード（`git archive b126265`）でも同じ（既存の差、回帰ではない） |
| Phase 1 の負例テスト（`TestECMAScriptScopes`、`TestECMAScriptCommonJS`、`TestTypeScriptTypeScopes`、`TestTypeScriptExportOnItsOwnLine`） | 通過（JS と TS の両方） |
| golden | JS の golden を更新（クラスのメンバー、名前の位置の範囲、binding による Exact の edge 3 本）。差分を確認済み |

### Mutation（`wsf/mut/mut2.py`）

22 種類すべて検出（テストが失敗）、元に戻したことを確認: localCap を外す、receiver の根の cap を外す、
ProgramLocals を外す、FunctionLocals を外す、値の receiver を Outside に戻す、dangling を無視、
型演算子の除外を外す、require の束縛を外す、module.exports の export を外す、`var` をローカルにしない、
相対の未索引 module を external に戻す、catch の引数、アローの単一引数、rest、デフォルト値、
入れ子のパターン、名前付き関数式の名前、関数内のクラス宣言の値の名前、`export default` の式、
関数式の名前による receiver の型の無効化、再代入された require の束縛、`module.exports` の置き換え時の
プロパティ export の除外。

### 採点の外にあるもの

- preact のパッケージの自己参照（`import … from 'preact'`、`'preact/hooks'`）による OutsideRepository が
  **37 件**ある（Baseline は 0 件）。Oracle はこれらを `unknown`（31 件）か site なし（6 件）に分類する
  ので、表の FO（0）には入らない。宣言はリポジトリの `package/src` にあるので、Outside の主張としては
  誤り。D8 の既知の制限（Provider は `package.json` を読めない）。

### 独立レビュー（Reviewer A: JS / TS）

重大な指摘なし。中 6 件・低 4 件。対応:

| 指摘 | Severity | 対応 |
|---|---|---|
| クラス式の名前がスコープに入らず誤った Exact | 中 | `ecmascript.body` でクラス式の名前を局所名にした（過大近似）。テストと Mutation M18 |
| `export default` の無名クラスの static block を見落とす | 中 | `export_statement` の `value` も走査。テストと M19 |
| 名前付き関数式・クラス式の名前が receiver の型を隠さない | 中 | `declCounts` で数える。テストと M20 |
| TS の CacheVersion が `ts-6` のまま | 中 | `ts-7` に上げた |
| preact の自己参照の Outside 37 件が採点外 | 中 | 上の「採点の外にあるもの」に明記。D8 |
| unit テストが Mutation 7 種を見逃す | 中 | 7 形のテストを追加。M12〜M17、M18 で検出を確認 |
| CommonJS: `module.exports =` と `exports.a =` の併用、再代入される require の束縛 | 低 | 置き換えがあるときプロパティ export を出さない、`var` / `let` で再代入される require は束縛にしない。テストと M21・M22 |
| `uv()` の喪失の原因の説明の誤り | 低 | 訂正 |
| TS コンパイラ Oracle のログ | 低 | CI と同じ範囲で実行したログに差し替え |
| TS の `declare module` / `namespace` の過大近似、`jsModuleSpec` の優先順位 | 低 | 優先順位を `x.js`（0）、その他と `index.*`（1）に変更。過大近似は D8 に記録 |

## Phase 2（JavaScript のクラス・メンバー・CommonJS）

### 計測（`measure.sh p2-g`、Oracle は `oracle2/`（`member` の印付き。印を除けば Phase 0 の Oracle と同一であることを確認））

| corpus | RSS MB | 抽出 ms | 解決 ms | 表現可能 | Coverage | 回復 | Recall | FS | FO | unknown_strong | decl_file_strong | Ark で表現可能なメンバー: 分母 / 回復 / Recall | Oracle 側のメンバー: 分母 / 回復 / Recall |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lodash | 52 | 246 | 7 | 3,591 | 36.1% | 1,242 | 34.6% | **0** | **0** | 0 | 0 | 31 / 7 / 22.6% | 449 / 7 / 1.6% |
| express | 30 | 50 | 1 | 243 | 13.7% | 80 | 32.9% | **0** | **0** | 0 | 0 | 14 / 2 / 14.3% | 162 / 2 / 1.2% |
| preact（dist 除く） | 32 | 71 | 1 | 617 | 19.4% | 188 | 30.5% | **0** | **0** | 1 | 0 | 12 / 0 / 0% | 85 / 0 / 0% |
| three/src | 208 | 1,305 | 68 | 14,715 | 20.6% | 7,394 | 50.2% | **0** | **0** | 0 | 0 | 4,283 / 2,203 / 51.4% | 9,632 / 2,234 / 23.2% |
| jsfix | 21 | 14 | 0 | 38 | 66.7% | 22 | 57.9% | **0** | **0** | 0 | 0 | 9 / 7 / 77.8% | 17 / 9 / 52.9% |
| jsfix2 | 21 | 12 | 0 | 1 | 20.0% | 0 | 0% | **0** | **0** | 0 | 0 | 0 / 0 / — | 0 / 0 / — |
| rxjs（TS） | 72 | 235 | 11 | 3,255 | 35.7% | 1,715 | 52.7% | **0** | **0** | 0 | 0 | 893 / 196 / 21.9% | 899 / 196 / 21.8% |

- rxjs の FS は 0（Phase 0〜2 の途中で「採点上の不一致」としていた `Scheduler.now` は、static の宣言を
  指す誤りだった。D14 で修正）。
- メンバーの Recall は 2 つの分母で出す: Ark で表現可能なメンバー（宣言が receiver を持つ Ark のシンボル）と、
  Oracle 側のメンバー（property access の名前の site。Ark に依存しない。Reviewer A の指摘）。
- 作業中の値: Phase 1 の直後（`p2-base`）の three のメンバー（Ark で表現可能）は 1,516 / 3,624（41.8%）。

### Series 1

| corpus | Phase 1 → Phase 2 の回復 | 失った正解 | 新たな正解 | Phase 0 → Phase 2 の回復 | 失った正解（Phase 0 から） |
|---|---|---|---|---|---|
| lodash | 1,235 → 1,235 | 0 | 0 | 1,038 → 1,038 | 0 |
| express | 78 → 78 | 0 | 0 | 38 → 38 | 0 |
| preact | 188 → 188 | 0 | 0 | 175 → 188 | 0 |
| three/src | 6,707 → 7,351 | 1 | 645 | 4,627 → 5,187 | 3（Phase 1 の 3 件） |
| jsfix | 17 → 20 | 0 | 3 | 7 → 15 | 0 |
| jsfix2 | 0 → 0 | 0 | 0 | 1 → 0 | 1（Phase 1 の 1 件） |
| rxjs | 1,669 → 1,715 | 0 | 46 | 1,669 → 1,715 | 0 |

- three の喪失 1 件: `animation/AnimationClip.js:181` `this.CreateFromMorphTargetSequence(...)`。static メソッドの
  中の `this`（クラス自身）は証拠にしない（D14）ので Candidate。健全な喪失。

### 判定不能な Strong の分類

- preact `src/diff/index.js:317,366,623,649` `options._catchError(...)`（`declaration_file`）: p2-f までは
  `options.js` の `const options = { _catchError }` の shorthand プロパティに Strong（宣言の水準では正しいが、
  実行時には `compat/src/suspense.js:6` と `debug/src/debug.js:78` が値を差し替える）。`options` は JSDoc の
  `@type` で型が宣言されているので、再レビュー（N-4）の修正で所有者にしなくなり、p2-g では 0 件。
- preact `compat/src/suspense-list.js:62` `suspended(...)`: Phase 1 と同じ（正しい）。

### テストと静的解析（`verify-p2b/`）

| 検証 | 結果 |
|---|---|
| `gofmt -l` / `go vet ./...` / staticcheck（CI と同じ範囲） | 通過 |
| `go test ./...` / `go test -race ./...` | 通過 |
| TS コンパイラ Oracle テスト（CI と同じ範囲） | 68 件 PASS、SKIP 0 |
| Phase 2 のテスト（`TestECMAScriptValueMembers`、`TestECMAScriptMemberNegatives`、`TestECMAScriptSuperShadowedInScript`、`TestTypeScriptDeclaredObjectTypeIsNoValueOwner`、`TestECMAScriptCommonJSNamespace`、`TestMembersShareNames_InheritedLookupSurvivesCache`） | 通過（JS と TS） |
| 既存テストの改訂 | `TestGraph_ThisMemberAndStaticReceiver`、`TestGraph_TSXComponents`、`TestRefs_NoFabrication`（D10・D11） |
| golden | ts_semantics: 継承の edge と `Layout.Header` の Strong の edge、そのシンボル |
| 決定性 | `measure.sh` を 2 回（p2-g、p2-g2）、7 コーパスすべて一致 |
| 最終の検証（`verify-p2c/`） | gofmt / vet / staticcheck / unit / race 通過、TS コンパイラ Oracle テスト 70 件 PASS |

### Mutation（`wsf/mut/mut3.py`）

27 種類すべて検出: 値の所有者、object のメンバー、後付けのメンバー、コンストラクタの `this`、値の Strong の
上限（2 箇所）、anyKind、JS の親の特定、`super`、CommonJS の名前空間、型注釈の付いた const、`let` の
オブジェクト、関数の static なプロパティ、隠された const オブジェクトの receiver、spread、static の文脈、
static と instance の同名、クラスへの prototype の代入、型でない親、関数の境界をまたぐ `super`、隠された
`extends` の名前、cache での `MembersShareNames` の喪失、static の accessor の対、prototype の矢印関数の
`this`、TS のコンストラクタ関数、JSDoc の `@type`、static ブロックの文脈。検出できない条件として残すものは D17。

### 独立レビュー（Reviewer A: JS / TS）

重大 1 件・中 6 件・低 2 件。対応:

| 指摘 | Severity | 対応 |
|---|---|---|
| ローカルに隠された const オブジェクトへの誤った Strong | 重大 | 条件を戻した（D13）。5 形の負例テスト、Mutation P15 |
| spread がメンバーを上書きする形 | 中 | spread を含むオブジェクトは所有者にしない（D15）。P17 |
| static の文脈がインスタンスのメンバーに解決される、クラスへの prototype の代入 | 中 | `staticScope`、prototype の所有者は関数宣言だけ（D14）。P18・P20 |
| `Scheduler.now` の分類 | 中 | static と instance を別のシンボルに（D14）。rxjs の FS 7 → 0。P19 |
| メンバーの Recall の分母が Ark に依存 | 中 | Oracle 側の分母（`oracle_members`）を追加。2 つの分母で報告 |
| テストの不足（Mutation 6 種が生存） | 中 | 負例を追加。P21〜P23 で検出。残る 2 種は D17 |
| CacheVersion が上がっていない | 中 | ts-8 / js-6（D16） |
| `super` がフィールドを拾う | 低 | 既知の制限（D17） |
| preact の `_catchError` の再代入の併記 | 低 | 上に併記 |

### 再レビュー（Reviewer A2）

前回の重大・中の修正はすべて確認（重大-1 は自作の局所隠蔽 22 形でも誤り 0）。コーパスの外で新たに 3 種:

| 指摘 | Severity | 対応 |
|---|---|---|
| N-1 static と instance の同名 accessor が 1 シンボル（誤った Exact） | 高 | accessor の対の鍵に static の印。テストと P24 |
| N-2 prototype に代入した矢印関数の `this` に Strong | 中 | `this` を関数式のときだけ立てる。テストと P25 |
| N-3 TS のコンストラクタ関数の `this` に Strong | 中 | prototype の所有者と `this` は JavaScript だけ。テスト（`TestJavaScriptPrototypeMembers` の ts 側）と P26 |
| N-4 JSDoc `@type` の const オブジェクトを所有者にする | 低〜中 | 直前の JSDoc に `@type` があれば所有者にしない。テストと P27 |
| N-5 先頭の spread も除外 | 低 | D15 のとおり（Recall の損失のみ） |
| N-6 `static {}` ブロックを走査しない | 低 | `staticScope` で走査。テストと P28 |

修正の確認（モデルの外の証拠）: 2 回のレビューの fixture 10 組（`reviewA2/fx/{js,js2,sh,ts,sch}`、
`rereviewA2/fx/{js,sh,js3,ts,ts2}`）を、修正後のコードと TypeScript 5.6.3 の Oracle で採点し直し、
`exact_fp` / `strong_fp` / `local_fp` / `false_outside` / `local_outside` / `unknown_strong` がすべて 0。

## Phase 3（HTML Parser の選定と安全性）

> Phase 9 の注記（監査 B、F9B-1）: 下の表の ctxcheck の数値（html5lib の kept / dropped / 変わったファイル
> 475 / 25 / 4、synth の unverified 1）は Phase 3 のコミット（ff65920）のハーネスのもの。Phase 4 の D23
> （R3・S1: 最後のタグの印が無い、落ちた template）で `attrlib.go` の `verify` が厳しくなり、HEAD の
> ハーネスでは 473 / 27 / 125、unverified 3（増えた 121 件は末尾のタグを木構築器が無視する入力）。ff65920 の
> ハーネスで再実行すると表の数値と一致する（監査 B が確認）。

再現: `docs/scripts/html-parser-eval/run.sh WORKDIR`（白紙の作業領域で再実行し、下の数値が一致。
Reviewer B も独立に再実行して一致を確認）。版: golang.org/x/net v0.50.0、github.com/tdewolff/parse/v2
v2.8.16、github.com/odvcencio/gotreesitter v0.55.1。コーパス: html5lib の木構築テストの入力 1,708 件
（x/net の `testdata/webkit/*.dat`）、実 HTML 148 件（GOMODCACHE の `.html`、3.3MB）、合成 79 件
（`synth/`: 手書き 17 件、Reviewer B の反例 `cx-*` 22 件・`rx-*` 17 件と属性の境界ケース `ac-*` 23 件）。

### 比較表

| 項目 | x/net Tokenizer | tdewolff | gotreesitter |
|---|---|---|---|
| 入力を途中で止める | 0 | 16 件（NUL。`Err()` で検出可） | エラー付きで 101 / 6 / 15 件（html5lib / 実 / 合成） |
| エラーを出さずにタグを落とす | 0（EOF の未完のタグは仕様どおり捨て、eof-in-tag で検出） | SVG / MathML の中身は 1 トークンで、中のタグを出さない | html5lib 19 件 |
| エラーの報告（html5lib / 実） | なし（仕様上の回復） | 16 / 0 | 925 / 14 |
| タグ数（html5lib / 実） | 3,955 / 6,865 | — | 3,665 / 5,447 |
| 仕様との差（字句） | 重複属性を残す、NUL を置き換えない（D19 で Provider が扱う） | 加えて、未完のタグを出す、SVG 内の属性なし | — |
| 時間・最大 RSS（実 + html5lib、続けて 3 回。`run.sh` の最初の 1 回はファイルのキャッシュが冷えていて x/net 0.4〜0.5 秒） | 0.10〜0.11 秒・19〜22MB | 0.08〜0.09 秒・14〜15MB | 1.31〜1.34 秒・230〜239MB |

x/net と tdewolff の属性の差（html5lib 34 ファイル、実 8 ファイル）はすべて分類した: tdewolff の
SVG / MathML 内の属性の欠落、EOF の未完のタグ（1692）、`noscript` の中、NUL での停止、テンプレート
構文（Hugo の `src="{{ "assets/main.js" | absURL }}"`。x/net の `assets`、`main.js"` が仕様どおり）。

### 範囲の独立検証（P3-2）

`attrcheck`: `Raw()` を HTML5 の属性の状態機械で独自に走査し、名前（ASCII だけ小文字化）と、値の範囲の
生の文字列に x/net と同じ正規化（改行、属性モードの文字参照。`attrlib.go` の `attrValue`）をかけたものが、
Tokenizer の属性と **厳密に** 一致するかを全タグで確かめる。

| コーパス | タグ | 属性 | 不一致 | 変異（値の範囲を 1 バイト後ろへ） |
|---|---|---|---|---|
| 合成 | 176 | 127 | 0 | 87 タグで検出 |
| html5lib | 3,955 | 500 | 0 | 215 |
| 実 | 6,865 | 4,483 | 0 | 3,040 |

合成には CRLF / CR を含む値、参照の混在（`&copy=2&amp;b=3&notit;&not`）、数値参照の境界、非 ASCII の
大文字の名前、非 UTF-8、NUL、`=` で始まる名前、閉じない引用符、`<` を含む名前を含む。途中で見つけた
ハーネスの誤り: `Token()` / `TagAttr()` はバッファの上で参照を展開するので `Raw()` は先にコピーする。
当初の照合（生の値か `html.UnescapeString` の結果のどちらか）は CR と参照の混在を誤って不一致にした
（Reviewer B の指摘 4）。

### 事実の裏付け（`ctxcheck`、P3-1・P3-2）

(要素, 属性, 値) を、Tokenizer と `html.Parse` の木で比べた。

| コーパス | 素の Tokenizer にだけある | 木にだけある | 印で検証して使う属性 / 使わない属性 | 使う属性のうち偽 | 印で木が変わったファイル |
|---|---|---|---|---|---|
| 合成 | 21 | 28 | 96 / 24 | 0 | 1（`rx-e11`: Tokenizer がタグ名 `b--` と読む `<b-->` の名前の後に印が入り、木構築のコメントの終わり `-->` を分ける。木の比較で検出） |
| html5lib | 8 | 36 | 475 / 25 | 0 | 4（Noah's Ark） |
| 実 | 0 | 0 | 4,480 / 0 | 0 | 0 |

- 使わない属性の内訳: 偽のタグ（反例 14 件と合成 16・17）、木構築が無視するタグ（frameset に置き換わる
  `input`、テンプレートの中の `body`）、`html` / `body` に統合されない属性（同名の属性が既にある）、
  Noah's Ark の 4 ファイルの全属性。
- 「使う属性のうち偽」は、反例が偽の属性の値に `fake` と書いたものを数える（a02・a16・a17 の値は、
  ブラウザが実際に作る要素なので `real` に改めた。a16・a17 は統合点の中でも CDATA が許される）。
- 「使う属性」は、要素名を含めてすべて木にある（`kept_not_in_tree`。再レビューの指摘 1 の前は、
  印が別の要素に付く形で成り立たなかった）。
- 実 HTML の 4,483 と 4,480 の差 3 は重複属性（1 ファイルの `type`、`name` ×2）を最初の 1 つにした分で、
  実 HTML にも重複属性がある。
- NUL を含む属性を使わない規則は検証側にも入れた（`cx-a19` の 2 属性）。
- Reviewer B の乱数生成器（`gen4.py`: rawtext、RCDATA、コメント、CDATA、引用符、select、template、
  foreign content を混ぜ、id をすべて一意にした文書）で、種 9・1・2・3 の各 3 万件（計 12 万件、属性
  約 19 万）: 木に無い属性を使った件数 0、偽 0。
- 変異: タグ n の属性を、印 n の要素ではなく木のどの要素で探してもよいことにすると、合成 17（木構築にだけ
  見える本物と、Tokenizer にだけ見える同じ値の偽）の偽を使ってしまう（kept_fake=1）。出現数の比較
  （素の Tokenizer と木の差）も、この偽を見抜けない（Tokenizer にだけあるもの 0）。
- 印の形の検討で見つけた誤り: 値の無い印は後ろの ` =b=c` を値として取り（合成 `ac-b05`）、引用符付きの
  値の印は偽のタグを含む引用符付きの属性値を閉じた（反例 12 件）。どちらも「印を除いた木が元の木と同じ」
  の検査で検出され、印の形（`=1` と後ろの空白）を改めた。

### 無言の切り捨て（P3-1）と不完全な解析（P3-3）

- 監査の 46 バイトの最小再現（合成 01）: x/net は `<div>` と `<p id="after">` を正しい範囲で出す。
  gotreesitter は 16 バイトまでで `<p>` を落とす（この版では `HasError` が真）。
- x/net の「全バイトの消費」は構成上いつも成り立ち、証拠にならない（Reviewer B の指摘 3）。検出できる
  不完全さは eof-in-tag で、`eoftail` で html5lib 17 件、合成 3 件、実 0 件。
- x/net 自身のテスト（`go test golang.org/x/net/html`）: 合格。

### 独立レビュー（Reviewer B: HTML / CSS）

高 2 件・中 3 件・低 1 件（複数の項目）。対応:

| 指摘 | Severity | 対応 |
|---|---|---|
| 1. AllowCDATA の固定と、foreign content / select の rawtext のずれで偽の id が出る | 高 | 印による検証（D19）。反例 22 件を合成に追加し、偽 0 |
| 2. 重複属性を x/net が残す（照合が同じ誤りで一致する盲点） | 高 | 最初の属性だけを使う（D19）。検証側も木の属性に同じ規則 |
| 3. 消費の和と `Err()` の規則は何も検出しない | 中 | eof-in-tag の診断（D19）、`eoftail` |
| 4. 照合が CR と参照の混在で正しい属性を落とす | 中 | x/net と同じ正規化（`attrValue`）、ASCII だけの小文字化。境界ケースを合成に追加し、不一致 0 |
| 5. tdewolff の「診断なし」は誤り | 中 | 訂正（`Err()` で検出可） |
| 6. 数値と記述の誤り（実のエラー 14 件、`assets/main.js"` の実入力、21% の内訳、対象属性、計測が手順に無い、NUL、camelCase の要素名） | 低 | 比較表と D19 を訂正。計測を `run.sh` に追加。ctxcheck は木の要素名を小文字化して比べる |

### 再レビュー（Reviewer B）

前回の指摘 1〜6 の対応と数値の再現を確認（白紙で `run.sh` を再実行し一致）。偽の属性を使う反例は、
手作り 16 件と乱数 12 万件で作れなかった（Tokenizer の rawtext は必ず `>` で終わるので、再開点で木構築は
データ状態・引用符付きの値・コメント・CDATA のどれかにあり、印はその中に入るか、木の比較で検出される、
という場合分けも確認）。新たな指摘:

| 指摘 | Severity | 対応 |
|---|---|---|
| 1. 印 n が別のタグの要素に付き、要素名を取り違える | 中 | 印の要素名がタグ n の名前と同じときだけ使う（D19 (1)）。反例を `rx-e17` に |
| 2. `html` / `body` の統合で、同じ値の属性を二重に使う | 中 | 前のタグに同名の属性があれば使わない（D19 (3)）。`rx-e03` |
| 3. `&fjlig;` と `&#x;` で `attrValue` が x/net と違う | 低 | 移植を直した。レビュアーのファズ（全実体名 2,231 × 接尾辞 10、乱数 200 万件）で除外なしに不一致 0 |
| 4. 印の文字列が文書にあると全体が検証不能、Noah's Ark の影響、NUL 規則がハーネスに無い、重複属性の証拠、tdewolff の欄 | 低 | 印の接頭辞をファイルごとに選ぶ。D19 と上の表に記録。NUL 規則を検証側に追加 |

判断: GREEN（条件の 1・2 は D19 と検証の実装に反映済み）。

推奨のうち、html5lib の tokenizer テスト（`tokenizer/*.test`）を Oracle に加えることは行っていない
（x/net のモジュールに含まれず、取得が要る）。Phase 4 で Provider の字句のテストを書くときに再検討する。

## Phase 4（HTML Provider）

実装は D21、Parser の規則は D19。`internal/languages/html`（`attrs.go`・`document.go`・`provider.go`・
`refs.go`）。依存: golang.org/x/net v0.50.0。

### テスト（P4-1〜P4-5）

| テスト | 内容 |
|---|---|
| `TestSymbols` | 文書・id・SVG の id・テンプレートのスコープ（入れ子）の Qualified とバイト範囲、`IdentityOnly`、位置 |
| `TestSymbols_OnlyWhatTheTreeHas` | D19 の反例 17 形（偽のタグ、無視されるタグ、重複属性、統合、印が別の要素に付く形、NUL、空白、テンプレート構文、空、eof-in-tag、Noah's Ark）で偽の id 0、本物は残る |
| `TestDiagnostics` | eof-in-tag は error、Noah's Ark は `html_tree_unverified`、正しい入力・境界ケースで error も照合不一致も無い |
| `TestIDReferences_StaticDocument` | 各 IDREF 属性・IDREFS・フラグメント・SVG の `href` / `xlink:href` / `url(#)` が Exact、宣言なしは Unresolved（Outside でない）、重複は Candidate |
| `TestIDReferences_Negatives` | script・`on*`・`javascript:`・テンプレート構文の文書は Strong、`<base>` の文書のフラグメントは出さない（`label for` は出す）、テンプレートの内外をまたがない、宣言的 shadow root は Exact、`#`・`#top`・`%`・テキストフラグメント・テンプレート構文・空白を出さない |
| `TestLinks` | 他の文書・その id が Strong、`.htm`・クエリ・パーセントエンコード、宣言なしは Unresolved、ルート相対・絶対 URL・`//`・ディレクトリ・ルートの外・HTML 以外・`\`・テンプレート構文・クエリだけを出さない |
| `TestHashInFileName`、`TestDeterministic` | `#` を含むファイル名、2 回の抽出の一致 |
| `TestCacheColdWarmEquivalence` | cache なし・cold・warm・warm の index（シンボル・edge・未帰属・参照・診断）が一致（G6） |
| `FuzzExtractHTML` | panic なし、範囲、決定性、木を Oracle として id の数が木の要素の数以下。60 秒で約 126 万回、失敗 0。CI の fuzz smoke（グループ 2）に登録 |
| conformance | `RunContract` と broken（eof-in-tag の error）に登録。品質監査の Q7 は 0 が正しい（IMPROVEMENTS.md） |
| golden | `internal/golden/testdata/html`（抽出・index・completeness）。他の言語の golden は変化なし |
| MCP（`html_e2e_test.go`） | 登録、`get_language_support`、`get_symbols`・`get_symbol`・`find_symbol`・`find_references`、`get_callers`（`referenced_by`・`linked_from`）・`get_callees`（`references`・`links`）、動的な文書は Strong、テンプレートの中は edge にしない、SVG の `<style>` の中の偽の id を出さない |

### 独立の実装との一致（P4-6）

`TestHarnessAgreement`（opt-in）: Provider の要素と、`docs/scripts/html-parser-eval` の `ctxcheck ids`
（`attrlib.go`）が検証を通した id（Provider が拒む空・空白・テンプレート構文を除く）を、ファイルとタグの
位置で突き合わせる。

| コーパス | ファイル | 一致した要素 | 不一致 |
|---|---|---|---|
| 合成 + html5lib + 実 | 1,935 | 995 | 0 |
| Reviewer B の乱数生成器（種 9） | 30,000 | 36,492 | 0 |

### 回帰（G7・G8）

- `gofmt`、`go vet ./...`、`go test ./...`、`go test -race ./...`、staticcheck（CI と同じ範囲、CI の固定版）:
  通過（`verify-p4/`）。
- JS / TS の採点（`measure.sh p4-a`）: 7 コーパスすべてで `report.scored` と `report.sites` が Phase 2 の
  最終（`p2-g`）とバイト単位で同一。FS 0、FO 0。抽出・解決の時間はばらつきの範囲。最大 RSS は lodash で
  53→62MB（テストのバイナリに x/net が入った分を含む）、他はばらつきの範囲。
- HTML の抽出: 3 コーパス 1,935 ファイル（10MB）の抽出と差分テストが 0.2 秒。
- 共通部分の変更: grammar の無い言語（registry、`syntax.Parse`、`ark syntax` は `ErrNoSyntaxTree`）、
  `syntax.ParseNamed`（MCP の `get_symbols`・`get_symbol`・`find_symbol` が実際のファイル名で抽出。
  find_symbol のメモのキーにファイル名を含めた）、SymbolKind `document`・`element`、ReferenceKind
  `id_reference`（→ `references`）・`resource_link`（→ 新しい `links`）、`refKindRank`、context の
  `links`、MCP の表示名 `linked_from` と説明文、CLI の補完と help、docs（language-support・mcp-tools・
  resolution-model・README）、fuzz smoke の数（16→17）。
- README の JavaScript の行が Phase 2 より前の記述（「クラスのメソッドは抽出しない」）のままだったので直した。

### 独立レビュー（Reviewer B: HTML）

重大 2・高 3・中 4・低 4。すべて対応（D22）。

| 指摘 | Severity | 対応 |
|---|---|---|
| 1. 木にだけ見える要素が重複 id を隠し、誤った Exact | 重大 | 木の id の数より Symbol が少ない id は Symbol にしない。テスト（svg style / title）、fuzz の Oracle を「木の数 = Symbol の数」に |
| 2. 動的な文書の判定が Tokenizer だけ | 重大 | 木の script・on*・javascript: も見る。テスト、fuzz の Oracle（木に script / on* があれば同じ文書の参照は cap） |
| 3. `<base>` の判定が Tokenizer だけ | 高 | 木の `base[href]` も見る。テスト |
| 4. 宣言的 shadow root（フラグメント、ホスト、2 つ目、template の中） | 高 | ホストと先行の条件、スコープ全体の判定、ハイパーリンクは文書のスコープ。テスト 5 件 |
| 5. `javascript:` の検出 | 高 | URL の前処理の後に判定。テスト 2 件 |
| 6. `iframe srcdoc` | 中 | 動的。テスト |
| 7. フレームワークの束縛 | 中 | 束縛属性と Angular の制御フローを動的に。テスト 3 件 |
| 8. IDREF の型と要素 | 中 | 単一の id、`form`・`popovertarget`・`list` の要素の制約。テスト 4 件 |
| 9. fuzz の Oracle が片側だけ | 中 | 上の 2 つの Oracle。120 秒・約 280 万回で失敗 0 |
| 10. `#` を含むファイル名 | 低 | 何も出さない。テスト |
| 11. フラグメントの文字、`url(#a\62)`、`iframe src="#a"` | 低 | 参照にしない。テスト 3 件。SMIL は既知の制限 |
| 12. `%2F` | 低 | 参照にしない。テスト |
| 13. 終了タグの eof-in-tag | 低 | warning。テスト |

隠れた重複の規則による Recall の損失: 実 HTML 0、合成 1、html5lib 9 要素（差分テストの一致数は
3 コーパス 995 → 985、乱数 36,492 → 36,381。いずれも不一致 0）。

### 独立レビュー（Reviewer C: 共通部分）

高 1・中 1・低 5。すべて対応（D22）。

| 指摘 | Severity | 対応 |
|---|---|---|
| F1. `find_symbol` が絶対パスを Provider に渡し、出力に OS のパスが出る（HTML・Rust・Terraform） | 高 | リポジトリ相対の名前で抽出。`TestSymbolTools_RepositoryRelativeNames`（修正を戻すと 3 つの root すべてで失敗することを確認） |
| F2. Rust・Terraform の symbol ツールの出力の変化が未記載 | 中 | D22 と `ParseNamed` のコメントに記載。Rust の仮の名前が出ないこと、`override.tf` が宣言しないことをテストで固定 |
| F3. secrets-scan の提案が HTML をコードと数える | 低 | 数えない。`TestScan_SuggestionsHTMLIsNotCode` |
| F4. 新しい種類の言語中立のテストが無い | 低 | `TestGraphBuilder_KindMapping` に追加、`TestReasonForEdge`（`links` は深さ 2 で展開しない） |
| F5. 名前空間のテストに HTML が無い | 低 | `mixedRepo` に HTML の 2 文書（他言語の名前への IDREF・フラグメント・リンク）と HTML 自身の edge |
| F6. 文書の漏れ | 低 | mcp-tools（`get_callers` / `get_callees`）、language-support（Graph の節、`ark syntax`）、README の図 |
| F7. `ExtractCommentRanges` が構文木の無い言語で黙る | 低 | `ErrNoSyntaxTree` を返す |

対応後: `go vet ./...`、`go test ./...`、`go test -race ./...`、staticcheck 通過（`verify-p4/test2.txt`、
`race2.txt`、`staticcheck2.txt`）。

再レビュー（Reviewer C）: F1〜F7 の修正を確認（symlink の root、実体の絶対パスでも `parent` は相対名）。
新しい指摘 G1（低）: D22 で抽出の意味が変わったのに CacheVersion が `html-1` のまま → `html-2` に上げた
（未コミットでも、作業中のバイナリの永続 cache が古い抽出を返さないように。D16 と同じ方針）。
判断: 共通部分は GREEN。

### 再レビュー（Reviewer B）

前回の 13 件の修正を確認。新しい指摘 9 件（R1 高、R2〜R5 中、R6〜R9 低）にすべて対応（D23）。
確認で R3 の残り 1 件（S1、中: 途中で落ちた template の後の状態のずれ）を指摘され、対応（D23）。
いずれも反例を `TestReviewB_NoFalseExact` に固定。判断: GREEN（S1 の対応後）。

| 指摘 | 対応 |
|---|---|
| R1 SVG の `<a>` のフラグメント | 文書のスコープ（テスト 2 件） |
| R2 NUL と U+FFFD の重複 | 数え上げで正規化（テスト） |
| R3 / S1 x/net の木の途切れ | 最後のタグの印・落ちた template で unverified（テスト 2 件） |
| R4 adoption agency で動いた先行の宣言 | ホストの子孫を見る（テスト） |
| R5 select の中の隠れた重複 | 無視されたタグの id を数える（テスト） |
| R6 custom element name | 文法と予約名（テスト 3 件） |
| R7 type の空白 | 除かない（テスト） |
| R8 フレーム・SSI・Blade・Svelte | 動的（テスト 5 件） |
| R9 fuzz の Oracle | NUL、`javascript:`、srcdoc、束縛属性 |

最終の検証: `go vet ./...`、`go test ./...`（1 回目に `TestExtractScalesLinearly`（Rust、時間の比を見る
テスト）が全パッケージ並列の負荷で失敗。Rust に差分は無く、単独で 3 回通過）、race（html・mcp・index。
全体は D23 の前に通過: `verify-p4/race3.txt`）、staticcheck（CI の固定版）、fuzz 120 秒（失敗 0）、
差分テスト（3 コーパス 985 要素、乱数 3 万件 34,429 要素、不一致 0。実 HTML の要素数は変化なし）。

### 判定: GREEN

P4-1〜P4-6 と G1〜G9 を満たす。Reviewer B（HTML）・C（共通部分）の重大・高・中の指摘は残っていない。
既知の制限は `docs/language-support.md` の HTML の節と D21〜D23。

## Phase 5（CSS Parser の選定と安全性）

再現: `docs/scripts/css-parser-eval/run.sh WORKDIR`（白紙の作業領域で実行し、下の数値を得た。Reviewer B も
独立に再実行して一致を確認）。版: css-parsing-tests `203ce36bffd617db7f118c551e32794561fb273d`、
github.com/tdewolff/parse/v2 v2.8.16、github.com/odvcencio/gotreesitter v0.55.1。コーパス: npm の 15
パッケージ（bootstrap 5.3.3、bulma 1.0.2、animate.css 4.1.1、open-props 1.7.7、normalize.css 8.0.1、
modern-normalize 3.0.1、@picocss/pico 2.0.6、@fontsource/inter 5.1.0、tailwindcss 3.4.14、@primer/css 21.5.1、
water.css 2.1.1、milligram 1.4.1、tachyons 4.12.0、font-awesome 4.7.0、@fortawesome/fontawesome-free 6.6.0）と
GOMODCACHE の CSS、計 967 ファイル（内容で重複除去）。合成 20 件（`synth/`）。

### 公開テスト（CSS Syntax Level 3）

| テスト | 件数 | 自前の実装 | tdewolff の Lexer（境界と種類） |
|---|---|---|---|
| component_value_list | 50 | 50 | 26（失敗 24 のうち 8 は unicode-range） |
| one_component_value | 10 | 10 | 10 |
| declaration_list | 10 | 10 | 10 |
| blocks_contents（ネスト） | 13 | 13 | 13 |
| one_declaration | 21 | 21 | 21 |
| one_rule | 14 | 14 | 14 |
| rule_list | 15 | 15 | 15 |
| stylesheet | 16 | 16 | 16 |
| 計 | 149 | 149 | 125 |

構文解析（§5）は両者で共通のハーネスの実装なので、差はトークン化だけを表す。照合のときだけ、テストが
残す 2014 年の CR のトークン（unicode-range、match）を出す（D24）。

### 実 CSS（967 ファイル）

| 項目 | 結果 |
|---|---|
| トークンの境界と種類（自前 vs tdewolff） | 差は既知の 3 種類だけ: `url("x")`（tdewolff は 1 トークン）3,547 件、unicode-range 1,526 件、match トークン 333 件（いずれも tdewolff は 2014 年の CR のトークン）。それ以外 0。tdewolff が途中で止まるファイル 0 |
| 宣言の数（自前 vs gotreesitter） | 494,564 で一致 |
| ブロックの数 | 自前 184,408、gotreesitter 182,536。差は 378 ファイルにあり、すべて自前の方が多い。gotreesitter は `@keyframes` のブロックを `keyframe_block_list` で表す（差のある 378 ファイルの `@keyframes` の出現は 2,051（data URI の中も含むテキストの数）。差がその数と一致するのは 142 ファイル、残りは差の方が小さい） |
| gotreesitter のエラーの報告 | 436 ファイル（45%） |
| 時間（構造の数え上げ、967 ファイル） | 自前 4.0〜4.3 秒、gotreesitter 36.6〜37.9 秒 |
| 構造の診断（967 ファイル） | `!invalid` 3 件だけ（いずれも IE のハック `*zoom: 1`。現行のブラウザも捨てる宣言）。`!rule-from-declaration` などの誤検出 0 |

### 最悪の入力（Reviewer B の指摘 1・2 の対応後）

| 入力 | 規則 | 宣言 | 時間 |
|---|---|---|---|
| `{` 100 万個 | 256（上限） | 0 | 0.74 秒 |
| `.x{a:` + `(` 100 万個 | 1 | 1 | 0.73 秒 |
| `@media x{` + 規則 32,000 | 32,001 | 32,000 | 0.18 秒 |
| `a{` 4,000 段 | 256（上限） | 0 | 0.01 秒 |
| `.x{` + `a:b{` 4,000 段 | 256（上限） | 0 | 0.02 秒 |
| `.x{` + `--a:{` 4,000 段 | 1 | 1 | 0.01 秒 |
| `.x{` + `a:{` 250 段 + 値 `b ` 40 万個（801KB） | 251 | 0 | 0.62 秒 |
| `.x{` + `a:{` 200 段 + 規則 20,000 | 20,201 | 20,000 | 0.09 秒 |

（白紙の再実行では、`@media` 32,000 規則 1.01 秒、`(` 100 万個 1.41 秒になった回もある。計測のばらつき。）

対応前は、`@media` の 8,000 規則で 24 秒（二次）、`(` 100 万個でスタック溢れによるプロセスの停止、
`{` 100 万個で 600 秒を超えた（深さの上限の後も中身を読み直していた）。

### 必須の構文（P5-1）と範囲（P5-2）

合成 20 件の構造（規則・at-rule・宣言とバイト範囲、診断）を `synth/expected.txt` に固定し、`run.sh` が
差分を取る。対象: ネスト（`&:hover`、入れ子の規則、規則の中の `@media`、宣言と規則の混在）、名前付きの
`@container` と `style()`、範囲のメディアクエリ、`@import` の `url()`・`layer()`・`supports()`、`@layer` の
宣言とブロック、`@supports`、`@property`、`@keyframes`（引用符の名前）、`@font-face`、`var()` の入れ子と
フォールバック、空と `{}` を含むカスタムプロパティ（`--x: a {b}`、`--foo:hover{…}` は宣言、トップレベルの
`--x:… {}` は無効）、CSS Modules（`composes`、`:global`、`:local`、`@value`）、非 ASCII とエスケープを含む
識別子、コメントと文字列の中の `{` `}`、`<!--` `-->`、`!important` の大文字小文字と空白、壊れた構文（閉じない
ブロック・文字列・URL・コメント、括弧の中の `}`、内側だけ閉じたブロック、bad-url、bad-string、`;` の書き忘れ、
トップレベルの宣言、余分な `}`）、UTF-8 の BOM（取り除いてオフセットは元のまま）、UTF-16 の BOM、
`@charset "iso-8859-1"`、不正な UTF-8 と NUL。範囲はトークンの範囲から作り、トークンの範囲は tdewolff と
（上の 3 種類を除き）一致する。宣言の範囲は末尾の空白とコメントを含めない。

### 独立レビュー（Reviewer B: HTML / CSS）

重大 1・高 3・中 3・低 3。選定は支持（代わりの候補はどちらも同じかそれ以上に悪い）。すべて対応（D24）。

| 指摘 | Severity | 対応 |
|---|---|---|
| 1. 入れ子の宣言の読み直しが二次（`@media` 8,000 規則で 24 秒） | 重大 | 一度読みの再帰下降と早い打ち切り。最悪の入力の計測を `run.sh` に。Phase 6 の受け入れ条件 |
| 2. 深い入れ子でスタック溢れ（プロセスごと停止） | 高 | 256 段の上限と `!too-deep`。上限の後は中身を読まない |
| 3. 閉じの判定の誤り（括弧の中の `}`、内側だけ閉じたブロック） | 高 | `block()` が閉じたかを返す。合成 12〜14 |
| 4. UTF-8 の BOM を取り除かず、`@import` と最初の規則を失う | 高 | 取り除く（オフセットは元のまま）。UTF-16 の BOM・`@charset` の診断。合成 15・18・19 |
| 5. カスタムプロパティの `{}` の扱いが現行の草案と違う | 中 | 例外と `--x:` の prelude を実装。合成 16 |
| 6. 仕様の版の記述の事実誤認 | 中 | D24 を訂正。2014 年の CR のトークンはテストの照合のときだけ。識別子の範囲を明示的に決めた |
| 7. 黙って失われる内容（`;` の書き忘れ、トップレベルの宣言、余分な `}`、置換文字、符号化） | 中 | 診断を追加。合成 17〜19 |
| 8. 不正な UTF-8 の U+FFFD の数 | 低 | WHATWG と同じく最大の部分ごとに 1 つ。合成 19 |
| 9. 宣言の範囲に末尾の空白とコメント | 低 | 含めない。期待値を更新 |
| 10. 記述の精度、`curl` の失敗の検出 | 低 | 上の表の記述を訂正。`run.sh` は取得の失敗で止まる |

### 確認（Reviewer B）

指摘 1〜10 の修正を確認（前回のプローブで 1〜5・8・9 が直り、6・10 の記述が正しくなった）。修正で入った
新しい指摘:

| 指摘 | Severity | 対応 |
|---|---|---|
| A. 普通のセレクタ（`a:hover`）に `!rule-from-declaration`（実 CSS で 398 件） | 高 | colon の次が空白か ident・関数・colon 以外のときだけ（D24）。合成 20、実 CSS で 0 件 |
| B. 値が `{}` で始まる宣言の読み直しが深さ × 大きさ（801KB で 20.6 秒） | 中〜高 | 括弧の対応の表でブロックを飛ばす（D24）。0.62 秒。`run.sh` の最悪の入力に追加 |
| C. `b: {} !important` を宣言として読めない | 低 | `!` と `important` を続けて読む。合成 20 |

Reviewer B の条件（A と B を直し、合成と最悪の入力の計測に加える）を満たした。

### 判定: GREEN

P5-1〜P5-3 を満たす。選定（自前の CSS Syntax Level 3）は Reviewer B が支持。Phase 6 への受け入れ条件:
D24 の実装を移し、深さの上限と最悪の入力の時間をテストと fuzz で固定する。

## Phase 6（CSS Provider）

> Phase 9 の注記（監査 B、F9B-7）: P6-1 が挙げる `url()` のうち、画像・フォントの `url()` は D27 で Phase 7 に
> 移し、Phase 7 は PARTIAL（未実装）。Phase 6 の判定の「P6-1 を満たす」は `@import` の `url()` までで、
> 画像・フォントの `url()` は未達として Phase 7 に含む。

実装は D25・D26、読み取りは D24。`internal/languages/css`（`tokenizer.go`・`reader.go`・`provider.go`）。新しい
依存は無い。

### テスト（P6-1〜P6-4）

| テスト | 内容 |
|---|---|
| `TestSymbols` | スタイルシート・`@keyframes`（ident と文字列。`none`・`inherit`・名前 2 つは出さない）・`@font-face`（小文字）・`@property`・カスタムプロパティの Kind・Qualified・範囲、`IdentityOnly` |
| `TestModuleClasses` | `.module.css` の局所クラス（`:global(…)`・`:global` の後・`:local(…)`・`:not()`・ネスト・`@media`・エスケープ）、`.module.css` 以外にクラスの Symbol が無いこと |
| `TestReferences` | `animation-name`・`animation`（キーワードの除外、名前 2 つは出さない）・`font-family`（総称の除外、大文字小文字、引用符の中の空白）・`var()`（入れ子、フォールバック、Candidate）、重複は Candidate、宣言なしは Unresolved（Outside でない）、別のファイルの同じ名前に解決しない、`var()` で計算される名前とテンプレート構文は出さない |
| `TestImportsAndComposes` | `@import`（文字列・`url()`・`layer()`・`..`）、ルート相対・絶対 URL・HTML 以外の拡張子は出さない、宣言なしは Unresolved、`composes`（同じファイルは Exact、`from` は Strong、`from global`・`.module.css` 以外は出さない） |
| `TestDiagnostics` | 各診断の重さと、正しい CSS（`a:hover` の入れ子、`{} !important`、UTF-8 の BOM）で何も出ないこと、BOM の後の位置 |
| `TestWorstCases` | Phase 5 の最悪の入力 8 種が 10 秒以内、深さの上限の報告 |
| `TestDeterministic`、`TestCacheColdWarmEquivalence` | 2 回の抽出の一致、cache なし・cold・warm の一致（G5・G6） |
| `FuzzExtractCSS` | panic なし、範囲、決定性、深さの上限。120 秒・約 430 万回で失敗 0。CI の fuzz smoke（グループ 3、18 本目） |
| conformance・golden | 登録（broken は bad-url の error）。`internal/golden/testdata/css`。他の言語の golden は変化なし |
| MCP（`css_e2e_test.go`） | 登録、`get_language_support`、`get_symbols`・`find_references`、`get_callers`（`referenced_by`・`linked_from`）・`get_callees`（`composes` の Exact と Strong）、`var()` は edge にならない |
| 名前空間 | `mixedRepo` に CSS の 2 ファイル（他言語の名前の `animation-name`・`font-family`・`var()`）と CSS 自身の edge |

### ハーネスとの一致（`TestHarnessAgreement`）

| コーパス | ファイル | 項目（規則・at-rule・宣言・問題） | 食い違い |
|---|---|---|---|
| 実 CSS（Phase 5 の 967 ファイル） | 967 | 679,569 | 0 |
| 合成（`synth/`） | 20 | 195 | 0 |

最初の実行で 2 種類の食い違い（空の値の宣言の範囲、`{} !important` の範囲）が見つかり、読み取り部をハーネスに
合わせた（D26）。

### 回帰（G7・G8）

- `gofmt`、`go vet ./...`、`go test ./...`、`go test -race ./...`、staticcheck（CI の固定版）: 通過（`verify-p6/`）。
- JS / TS の採点（`measure.sh p6-a`）: 7 コーパスの `report.scored` と `report.sites` が Phase 2 の最終と同一。
  1 回目の three の時間（抽出 1,672 ms、解決 229 ms）は負荷によるばらつきで、再計測（`p6-b`）で 1,220 ms / 73 ms。
- CSS の抽出: Phase 5 の 967 ファイル（約 32MB）の差分テストが 3.9 秒。

### 独立レビュー（Reviewer B: CSS）

高 2・中 3・低 4。すべて対応か記録（D27）。反例は `TestReviewB_NoFalseStrong` に固定。

| 指摘 | Severity | 対応 |
|---|---|---|
| 1. `:global` の後の `:is(.a, .b)` などで偽の局所クラスと誤った Exact | 高 | モードを括弧ごとに受け継ぐ。テスト 6 形と `composes` の解決 |
| 2. `animation` の `!important` を名前として拾う | 高 | 末尾の `!important` を除く。テスト |
| 3. `@font-face` の名前に最初の記述子、`src` なし | 中 | 最後の有効な記述子、`src` を要求。テスト |
| 4. 無効な位置の `@import` を Strong の参照に | 中 | 位置の規則。テスト |
| 5. スタイル規則の中の `@keyframes` / `@font-face` | 中 | Symbol にしない（`@media` の中は維持）。テスト |
| 6. ファミリ名の大文字小文字の文書 | 低 | D25 を訂正 |
| 7. 書かれた U+FFFD と `@charset "utf8"` に warning | 低 | 元のバイトだけ、ラベルの表。テスト |
| 8. 複数語のファミリの範囲、CR の行、fuzz の検査 | 低 | 範囲を広げた（テスト）、CR は記録、fuzz に参照の範囲の検査 |
| 9. timeline・接頭辞・大域の `composes`・P6-1 の `url()` | 低 | 記録（D27） |

### 独立レビュー（Reviewer C: 共通部分）

中 2・低 4。不変条件の違反は無し（名前空間、MCP の単一ターゲット、cache・指紋、決定性、出力のマスクを観測で
確認。weburl への移動は旧実装と乱数 200 万件と fuzz で同値）。

| 指摘 | Severity | 対応 |
|---|---|---|
| 1. secrets-scan が CSS をコードに数える | 中 | 数えない。`TestScan_SuggestionsHTMLIsNotCode` を CSS にも |
| 2. カスタムプロパティの多いスタイルシートが repomap の上位を占める | 中 | MapHinter（CSS・HTML）と重み。`TestWebDetailDoesNotDominate`（修正を外すと失敗することを確認） |
| 3. ターゲットの指定の末尾一致 | 低 | 既知の制限（D27） |
| 4. 空白を含むファイル名の Qualified の衝突 | 低 | 既知の制限（D27） |
| 5. ARCHITECTURE の `composes` の Exact、`ark syntax` の注記 | 低 | 直した |
| 6. weburl の単体テスト | 低 | `TestRelative`、`TestTrimAndScheme` |

### 判定: GREEN

P6-1〜P6-4 と G1〜G9 を満たす（P6-1 の `url()` の画像・フォントは Phase 7 へ移した: D27）。Reviewer B・C の
重大・高・中の指摘は残っていない（いずれもテストか記録で対応）。最終の検証: `go vet ./...`、`go test ./...`、
race（言語・repomap・mcp・index・secretscan）、staticcheck（CI の固定版）、`FuzzExtractCSS` 60 秒（失敗 0）、
差分テスト（実 CSS 967 ファイル・合成 20 件で食い違い 0）。

## Phase 7（Web 横断のリソース関係）

実装は D28。共通部分: `language.ReferenceDraft.FileIdentity`、`reference.Reference.FileIdentity`、
`symbol.KindScript`・`symbol.DeclaresFile`、`resolver`（`fileDecls`・`resolveFileIdentity`）。Provider:
JavaScript（`script` の Symbol、`js-7`）、HTML（スタイルシートとスクリプトのリンク、`html-3`）。

### テスト（P7-1〜P7-4）

| テスト | 内容 |
|---|---|
| `TestWebResourceLinks`（html） | 全言語の Provider で index を作り、関係の一覧が期待と完全に一致: HTML → CSS（`stylesheet`、`alternate stylesheet`）、HTML → JS（`<script src>`、`type=module`、`modulepreload`）は Strong の `links`。`type="text/template"`・`.ts`・ルート相対・絶対 URL・`preload`・`<base>` の文書は参照なし。存在しない `missing.css` は Unresolved（Outside でない）。同じ名前（`run`、`spin`）の HTML の id・CSS の `@keyframes`・JS と TS の関数の間に関係 0 |
| `TestWebCacheColdWarm`（html） | 言語をまたぐ index の関係が cache なし・cold・warm で一致 |
| `TestNameSpaces_NoRelationJoinsTwoNameSpaces`（改訂） | 名前空間をまたぐ関係はパスの証拠の 2 つだけ（HTML → CSS、HTML → JS）。`.ts` へのリンクは関係にならない。名前による関係は従来どおり 0 |
| `TestMCPNameSpaces_NoCrossLanguageRelations`（改訂） | HTML の id と CSS の `@keyframes` の `GoOnly`、`for=jsRun`・`font-family: jsRun` は他言語に届かない。`<script src>` はスクリプトの `linked_from` |
| `TestUnresolved_CrossLanguage`（改訂） | HTML の文書の callees: スクリプトへの edge、他言語の名前と存在しないスクリプトは Unresolved（Outside でない） |

### 回帰

- `gofmt`、`go vet ./...`、`go test ./...`、`go test -race ./...`、staticcheck（CI の固定版）: 通過（`verify-p7/`）。
- golden: JavaScript だけが変化（ファイルごとの `script` の Symbol の追加と統計。参照と関係は不変）。
- JS / TS の採点（`measure.sh p7-a`）: 7 コーパスの `report.scored` と `report.sites` が Phase 2 の最終と同一。

### 未達（D28）

P7-1 のうち、HTML → 画像、CSS → 画像・フォント（`url()`、`@font-face` の `src`）は未実装。索引の外の
ファイルの節点を index に持つ設計の変更が要る。「ありうる一致」の層も実装しない（P7-2 は名前の段を変えない
ことで満たす）。

### 独立レビュー（Reviewer B: HTML のリンク）

中 2・低 5。すべて対応（D29）。反例 33 形を `TestWebResourceLinks_Fetched` に固定。

| 指摘 | Severity | 対応 |
|---|---|---|
| F1. `<script>` の type の判定（`language`、Unicode の空白、`module` の比較） | 中 | 仕様の手順どおりに直した。検証されない属性はリンクを拒否 |
| F2. `<link rel=stylesheet>` の `type` | 中 | `text/css` のときだけ |
| F3. `disabled` | 低 | 出さない |
| F4. `modulepreload` の `as` | 低 | script-like だけ |
| F5. template の中・`nomodule` | 低 | 出す。D29 に理由 |
| F6. 古い記述 | 低 | Provider のヘッダと language-support を直した |
| F7・F8. Recall の損失、網羅性 | 情報・低 | D29 に記録、表形式のテスト |

### 独立レビュー（Reviewer C: 横断）

`FileIdentity` の経路は閉じていて、名前による言語横断の関係は無い（観測と読解で確認）。対応は D30。

| 指摘 | Severity | 対応 |
|---|---|---|
| C-1. ルート直下の `app.js` のメソッド `js` と Qualified が衝突し、edge が消える | 中 | Container・Parent でファイル自身の宣言を後回し。回帰テスト |
| C-2. repomap に script が載る | 中 | `Exported: false`。回帰テスト |
| C-3. import の指定子が同じディレクトリの段で script を候補にする | 低〜中 | 名前の索引のすべてから外す。回帰テスト |
| C-4. type の検証・`language` | 低 | F1 と同じ対応 |
| C-5. template の中の Strong | 低 | D29 に方針 |
| C-6. 文書 | 低 | `cache/version.go`、テストのコメント、D28 の記述 |

Reviewer C の判断: PARTIAL（P7-1 の画像・フォントが未実装）。

### 判定: PARTIAL

実装した範囲（HTML → CSS / JS のパスの証拠、CSS → CSS、名前で横断しないこと、名前空間の不変条件の限定的な
改訂、False Strong 0、決定性、cache、MCP の互換）は基準を満たし、両レビューの指摘はすべて対応した。
P7-1 の HTML → 画像、CSS → 画像・フォントは未実装。理由: Ark の index は Provider のある言語のファイルしか
持たず、索引の外のファイルの節点が無い。再開の条件: 参照された資源ファイルを index の入力と指紋に加え、
資源の Symbol を作る設計を決めること。品質の阻害ではなく範囲の不足なので、BLOCKED ではなく PARTIAL とする。

### レビュー対応後の回帰（最終）

| 検証 | 結果 |
|---|---|
| `go build` / `go vet ./...` / `gofmt -l` | 通過 |
| `go test -count=1 ./...` | 全パッケージ通過 |
| `go test -race ./internal/... ./tests/...` | 通過 |
| staticcheck（CI と同じ範囲 + weburl） | 指摘なし |
| `measure.sh p7-b` | 7 コーパスすべてで `report.scored`・`report.sites` が p7-a（Phase 2 の最終と同一）と一致。False Strong / Outside 0 |

## Phase 8（CSS Modules と埋め込み領域）

### 実装と単体の検証（レビュー前）

| 基準 | テスト | 結果 |
|---|---|---|
| P8-1 CSS Modules | `TestCSSModulesBinding`（JS・TS・TSX、既定・名前空間・名前付きの import、`s.x`・`s?.x`・`s['x']`・`(s).x`、使用位置の名前、負例: 呼び出し、計算されたキー、テンプレート文字列、エスケープ、遮蔽、パラメータ、JSX のタグ名、`.css`、alias・ルートの外・エスケープを含む指定子、`import type`。宣言の無いクラスは Unresolved で Outside 0。名前の一致で関係ができない） | 通過。位置は手で数えた値と一致 |
| P8-1 名前空間 | `TestNameSpaces_NoRelationJoinsTwoNameSpaces`（パスの証拠の種類に `FileScoped` を加え、言語をまたぐ関係は 3 件ちょうど） | 通過 |
| P8-2 範囲 | `TestEmbedded_SymbolsAndRanges`（行・列で切り出した文字列 = バイトで切り出した文字列、参照の範囲が名前を指す）、`TestEmbedded_RangesMap`（CRLF・CR・行の途中で始まる領域・複数バイト文字） | 通過 |
| P8-2 対象 | 同上（`type="text/less"`・`" text/css"` の style、`src` 付き・`text/template` の script、template の中、SVG の中、textarea の中は読まない）、`TestEmbedded_Refused`（NUL、BOM は読まない。table の中の style は読む） | 通過 |
| P8-3 identity・解決 | `TestEmbedded_Resolution`（文書の大域、module の自前の宣言と大域、別の文書・JS ファイルの同名に届かない、import の名前は Unresolved で Outside 0、`<base>` の文書の `@import` は出さない、var() は Candidate） | 通過 |
| P8-3 cache | `TestEmbedded_CacheColdWarm`（関係・Symbol と範囲・候補数・未解決数が、cache なし・cold・warm で一致） | 通過 |
| MCP | `TestMCP_HTML_Symbols`（`get_symbols` に inline script の関数、`get_symbol` がその本文を返す） | 通過 |
| golden | html の extract / index（`docs/form.html` の inline script の `send` が 1 件増えただけ） | 更新 |

### ミューテーション

埋め込み（`embed.go`）12 種: 名前空間の検査、template の検査、BOM、style の type、src、module の自前の宣言、
上限、`IdentityInRepository`、script の型の検査、receiver（テストを追加して殺した）は KILLED。NUL の検査と
木のテキストとの一致の検査は互いに重なり、片方を外すともう片方が捕まえる。一致の検査だけが効く入力は、乱数
生成した 30 万件（タグ・raw text の要素・コメント・foreign content などの組み合わせ）で 0 件（木の印による
検証が先に捕まえる）。多重の防御として残す。

CSS Modules（`cssmodules.go`）・resolver 11 種: 接尾辞、`import type`（テストを追加して殺した）、遮蔽 2 種、
呼び出しの除外、JSX のタグ名、上限、`FileIdentity`、エスケープの拒否は KILLED。subscript の文字列の検査
（`plainString` が文字列以外に空を返すので同値）、`fileDecls` の Qualified の接頭辞の検査（CSS Provider が
常に満たすので同値）は生き残る同値のミューテーション。

### 回帰

| 検証 | 結果 |
|---|---|
| `go build` / `go vet ./...` / `gofmt -l`（意図的に壊した testdata を除く） | 通過 |
| `go test -count=1 ./...` | 全パッケージ通過 |
| `go test -race ./internal/... ./tests/...` | 通過 |
| staticcheck（CI と同じ範囲 + weburl） | 指摘なし |
| `measure.sh p8-a` | 7 コーパスすべてで `report.scored`・`report.sites` が p7-b と同一。False Strong / Outside 0 |
| 実 HTML（Phase 3 のコーパス 1,872 件） | panic なし。埋め込みの Symbol 26 件・参照 390 件のすべてで、行・列とバイトの範囲が一致。最も遅い 1 件は 2.5MB の単一のインライン script を持つ 2.6MB の文書で 1.3〜1.5 秒（JS の構文解析。JS ファイルの解析と同じ速度） |

### 独立レビュー（Reviewer A: JS / TS の CSS Modules）

1 回目: 高 2・中 4・低 3。GREEN 不可（名前付き import の使用位置で誤った Strong を観測）。2 回目: 前回の反例は
すべて解消、低 1（A8R-1）・情報。GREEN 可。対応は D32。

| 指摘 | Severity | 対応 |
|---|---|---|
| A8-1 型の中の引数名・index signature のキー | 高 | `valueUse` の許可リスト |
| A8-2 オブジェクトのメソッドの引数がローカルでない（既存の欠陥） | 高 | `enterFunction`。採点は不変 |
| A8-3 JSX の名前空間付きの名前 | 中 | 許可リスト |
| A8-4 import attributes | 中 | 束縛しない |
| A8-5 `ns.default` | 中 | 除外 |
| A8-6 基準と実装の不一致 | 中 | P8-1 を改訂（D32 に理由） |
| A8-7 代入先・`delete`・`++`・`new` | 低 | 許可リスト |
| A8-8 範囲が引用符を含む | 低 | 文字列の中身 |
| A8-11 テストの網羅 | 低 | 負例を追加、ミューテーションで確認 |
| A8R-1 括弧・non-null で包んだ代入先など | 低 | 括弧と non-null を外へたどる。正例（条件・計算されたキー）も追加 |

### 独立レビュー（Reviewer B: HTML の埋め込み）

1 回目: 高 1（性能）・中 3・低 2・情報。GREEN 不可。2 回目: 前回の指摘はすべて解消、低 2・情報 1、B8-7 を撤回。
GREEN 可。

| 指摘 | Severity | 対応 |
|---|---|---|
| B8-1 入力の終わりで閉じた script を読む | 中 | `tag.closed` |
| B8-2 classic の `nomodule` | 中 | 読まない |
| B8-3 `for` / `event` | 低 | 読まない |
| B8-4 `<style>` の数に二次の時間（4 万個で 39 秒） | 高 | 内容の位置で抽出して写す（4 万個で 0.66 秒）。比のテストで固定（二次のミューテーションを検出） |
| B8-5 テストの欠落 | 中 | 追加、ミューテーション 18 種すべて検出（2 回目の写しで） |
| B8-6 module のトップレベルの束縛 | 低 | `Locals` |
| B8-7 取りこぼし | 低 | 記録。`type=" module"` は撤回 |
| B8-8 文言・cache の版 | 情報 | cache の版を合成。文言は記録 |
| R8-1・R8-2 テストの欠落 | 低 | テストを追加 |
| R8-3 条文の番号 | 情報 | 文言で引く |

### 独立レビュー（Reviewer C: 横断）

1 回目: 中 2・低 3・情報 2。条件付きで GREEN 可。2 回目: 解消を観測、低 2・情報 1。GREEN 可。

| 指摘 | Severity | 対応 |
|---|---|---|
| C8-1 `filePattern` で HTML の宣言が見つからない | 中 | 段ごとに絞る |
| C8-2 `get_symbol` で同名の宣言を選べない | 中 | Provider の Qualified で選べ、再試行名に示す |
| C8-3 HTML の cache の版が CSS / JS に追従しない | 低 | 版を合成 |
| C8-4・C8-9 パスとクラス名の Qualified の衝突 | 低 | TargetKinds で引く宣言の種類を分ける（衝突が無くなる）。テスト |
| C8-5 CSS の時間のテストの揺れ | 低 | 記録（Phase 8 と無関係） |
| C8-7・C8-10 文書 | 情報 | 更新 |
| C8-8 重複した宣言の再試行名 | 低 | Qualified が絞るなら Qualified。テスト |

公開 MCP 契約: Reviewer C が修正前後の 25 呼び出しの出力を比べ、JSON のフィールドの増減なし、`filePattern` なしの
解決は不変、差は意図した 2 点（再試行名、not found → Ambiguity）と repomap の順位の入れ替え（A・B の修正の
結果。決定的）だけと確認。

### race の線形性のテスト

1 回目の race で `TestEmbedded_ManyRegions`（2 万個の script を 8 秒以内）が 82 秒で失敗（race の下では
tree-sitter の解析が大幅に遅い）。絶対時間をやめ、4 倍の規模の時間の比（8 倍以内、二次なら 16 倍）にした
（Rust の `TestExtractScalesLinearly` と同じ形）。二次のコストを戻すミューテーションで失敗することを確認。

### レビュー対応後の回帰（最終）

| 検証 | 結果 |
|---|---|
| `go vet ./...` / `gofmt -l`（意図的に壊した testdata を除く） | 通過 |
| `go test -count=1 ./...` | 全パッケージ通過 |
| `go test -race ./internal/... ./tests/...` | 通過 |
| staticcheck（CI と同じ範囲 + syntax・weburl） | 指摘なし |
| `measure.sh p8-b`（A8-2 の後）・`p8-c`（最終） | 7 コーパスすべてで `report.scored`・`report.sites` が p8-a（= Phase 7）と同一。False Strong / Outside 0 |

## Phase 9（最終の独立統合監査）

### 範囲と手順（監査の前に固定）

対象: `main..HEAD`（Phase 0〜8 のコミット f32178f まで）の全体。新しい文脈の独立監査者 3 名が、台帳（progress・
decisions・verification・acceptance-criteria）とコードとを突き合わせ、モデル外の証拠（実行・観測・条文）で確かめる。

| 監査者 | 観点 |
|---|---|
| A（JS / TS） | Phase 1・2・8 の P1-*・P2-*・P8-1。固定 Oracle の採点を白紙から再現（`measure.sh`）し、台帳の数値と一致するか。誤った Exact / Strong / Outside を新たに探す |
| B（HTML / CSS） | Phase 3〜6・8 の P3-*〜P6-*・P8-2・P8-3。`docs/scripts/html-parser-eval/run.sh`・`css-parser-eval/run.sh` を白紙の作業領域で再実行し、台帳の数値と一致するか。誤った Exact / Strong / 範囲を新たに探す |
| C（横断・統合） | Phase 7・8 の横断と共通部分。名前空間の不変条件、MCP の公開契約、cache、既存の言語（Go・PHP・Python・Rust・Terraform）への回帰、文書と実装の一致、Git の状態（コミットの粒度、未コミットの残り、main への影響が無いこと） |

判定: 3 名の重大・高の指摘がすべて解消するか、根拠を添えて記録されれば、各フェーズの判定（GREEN / PARTIAL）を
確定する。監査で基準を下げない。

### 監査の結果（1 回目）

| 監査者 | 再現 | 指摘 |
|---|---|---|
| A（JS / TS） | `measure.sh audit9a` が p8-c と 7 コーパスでバイト単位で一致。Oracle（`oracle2/` から印を除く）が監査の Oracle と SHA で一致 | 高 1（F9A-1、Phase 2 の誤った Strong）、中 3（F9A-2〜4）、情報 2 |
| B（HTML / CSS） | `css-parser-eval/run.sh` は全件一致。`html-parser-eval/run.sh` は synth・real が一致、html5lib の ctxcheck は Phase 4 の D23 の後の値（F9B-1、注記）。差分テスト HTML 1,935 ファイル・CSS 967 ファイルで食い違い 0 | 中 3（F9B-1〜3）、低 4 |
| C（横断・統合） | §8 の検証（gofmt・vet・test・race・staticcheck）を自ら実行。Go・PHP・Python・Rust・Terraform の索引とコンプリートネスが main とバイト単位で一致。MCP の 47 呼び出しの出力が main / HEAD と一致（意図した差を除く）。cache の Cold / Restore / main の cache の読み込みが一致 | 中 3（F9C-1・2・8）、低 4、情報 1 |

対応は D33。修正後の確認（2 回目）:

| 監査者 | 結果 |
|---|---|
| A | 前回の反例はすべて解消、新たな誤り 0。`measure.sh audit9a2` = p9-b。F9A-7（F9A-2 のテストが関数の中の `require` で空振り）を指摘 → テストをトップレベルの `require` に直し、上書きの drop を外す変異で失敗することを確認。F9A-8 の N7 も負例で検出。N8（prototype の Placement）は F9A-1 の修正と重なる同値の守り |
| B | 前回の反例はすべて解消（shadow root 4 万件 10.2 秒 → 0.70 秒）、新たな誤り 0。BOM の指摘は B が撤回（ミューテーションの書き誤り）。コーパスと fuzz で失敗 0 |
| C | 既存の言語の索引が main とバイト単位で一致（作業ツリーの複製で）、§8 通過、MCP の差は `search_code` の JSON の `Placement` の追加だけ（P9C-R2、記録）、cache 一致、文書の修正は正確 |

### 監査の修正後の回帰（最終）

| 検証 | 結果 |
|---|---|
| `gofmt -l`（意図的に壊した testdata を除く） / `go vet ./...` | 通過 |
| `go test -count=1 ./...` | 全パッケージ通過 |
| `go test -race ./internal/... ./tests/...` | 通過 |
| staticcheck（CI と同じ範囲 + syntax・weburl） | 指摘なし |
| `measure.sh p9-b` | FS / FO はすべて 0。three の Recall 7394→7399、rxjs 1715→1722（Candidate から正解へ 12 件、失った正解 0。監査 A が site ごとに確認） |

### フェーズごとの判定（確定）

| Phase | 基準 | 判定 | 根拠 |
|---|---|---|---|
| 0 | Baseline・設計の固定 | GREEN | baseline.md、Oracle の固定（監査 A が同一性を確認） |
| 1 | P1-1〜 | GREEN | Phase 1 の節、監査 A |
| 2 | P2-1〜 | GREEN | Phase 2 の節。監査で見つかった誤った Strong（F9A-1）は修正済み、テストで固定 |
| 3 | P3-1〜 | GREEN | Phase 3 の節（F9B-1 の注記つき）、監査 B のハーネスの再実行 |
| 4 | P4-1〜P4-6 | GREEN | Phase 4 の節。F9B-2（非線形の時間）は修正済み |
| 5 | P5-1〜 | GREEN | Phase 5 の節、監査 B の再実行で全件一致 |
| 6 | P6-1〜P6-4 | GREEN（P6-1 の画像・フォントの `url()` は Phase 7 へ移し未達、D27・F9B-7） | Phase 6 の節。F9B-3・4 は修正済み |
| 7 | P7-1〜P7-4 | PARTIAL（画像・フォントのリンクが未実装。理由と再開条件は D28 と Phase 7 の節） | Phase 7 の節、監査 C が妥当と判断 |
| 8 | P8-1〜P8-3 | GREEN | Phase 8 の節、監査 A・B・C |
| 9 | 最終の独立統合監査 | 完了（3 名とも重大・高・中の指摘なしを確認） | 本節 |
