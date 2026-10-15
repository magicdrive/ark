# Phase C: Semantic Value Oracle（評価契約 v2）

## C1. 方針

- **旧 Oracle（契約 v1）は変えない。** `docs/scripts/oracle/javascript-tsc.js` は 1 バイトも変更していない
  （`git diff` が空）。v1 のスコアは上書きしない（Phase E は v1 と v2 の両方で採点する）。
- **v2 は v1 に「値の層」を足すだけ。** `docs/scripts/oracle/javascript-tsc-value.js` は、同じ引数で v1 の
  スクリプトを子プロセスとして実行し、その出力の各行をそのまま写す。v1 の site・`class`・`decl`・`via`・
  `member` は変えずに欄を足すだけなので、v2 のファイルから値の層の欄を除くと v1 と同一になる（12 コーパス
  すべてで、行ごとの JSON の比較と Reviewer B のバイト単位の比較で確認。C4）。
- **Ark の出力を入力にしない。** 値の層は TypeScript の checker と AST だけから求める。
- **特定のコーパスの特別扱いをしない。** 規則は AST と Symbol の形だけで決まり、ファイル名・パッケージ名・
  識別子名を見ない（Reviewer B が差分で確認）。
- **値を述べる条件は保守的に取る。** 値の層は正解を足す方向にしか働かない。Oracle が誤った値を述べると、
  Ark の誤りを TP として隠しうる。疑わしい場合は値を述べず、v1 の答えだけで採点する（`value_unknown`）。

## C2. 2 層と対象

| 層 | 欄 | 意味 |
|---|---|---|
| Syntactic Target | `decl`（v1 のまま） | TS の checker の site の Symbol（alias を最後までたどったもの）の宣言 |
| Semantic Value Target | `value`、`value_kind`、`value_via`、`value_rule` | `decl` が CommonJS の export オブジェクトのプロパティのとき、そのプロパティが持つ値の宣言 |
| 不明 | `value_unknown`、`value_unknown_at` | 値を述べない理由と、その原因の位置 |
| 安全でない export | `export_unsafe` | site が通る CommonJS の export に、モジュールやプログラムが別の値を書きうる（下の UNSAFE の理由） |

**対象は CommonJS の export オブジェクトに限る。** 具体的には、モジュールのトップレベルの文
`module.exports = { … }` の右辺のオブジェクトリテラルのプロパティである。F6B-5 の主題がこの形であり、
そこでは TS の binder の形の判定によって答えの層が変わる（`semantics.md` B2）。

それ以外のオブジェクトリテラルは対象にしない。export でないローカルの const、`export default { … }`、
設定のオブジェクトなどは、プログラムが静的な規則で抑えられない形で書き換えうる可変のデータだからである
（Reviewer B の B-7・B-8）。これらの site は v1 の答えだけで採点する。

最初の版は、あらゆるオブジェクトリテラルのプロパティを対象にしていた。Reviewer B の指摘で、公開の可変データ
（three の `ColorManagement.workingColorSpace`）や、引数・ローカルを値とする site が含まれていたので、
対象を絞った（C6）。

値の層を求める手順（`valueLayer`）は次のとおり。

1. site が書き込みの対象（`a = b`、`m.a = b`、`a++`、`delete m.a`）なら何もしない。そこでは値が読まれない。
   site が shorthand そのもの（`{ parse }` の `parse`）でも何もしない。v1 がすでに
   `getShorthandAssignmentValueSymbol` で、値を site の Symbol にしている。
2. site の識別子の Symbol を `getSymbolAtLocation` で取る。alias のときは、その束縛がプログラムのどこかで
   代入されていれば `alias_reassigned` とする（`let { a } = require(…); a = f`）。そうでなければ
   `getAliasedSymbol` でたどる（v1 と同じ）。
3. Symbol が、宣言のすべてがオブジェクトリテラルのプロパティであるプロパティでなければ、何もしない。
   CommonJS の export オブジェクトのプロパティでなければ何もしない。ただし、そのモジュールが
   `module.exports` を 2 回以上、またはトップレベルの文以外で代入しているときは、`replaced` とする。
4. プログラムに `with`・`eval`・`Function` があるか、モジュールのラッパーの別の経路（トップレベルの
   `arguments`、`require.cache`・`require.main`・`module.constructor`・`process.mainModule` の先、
   `require('module')`、`require(__filename)`、`require` の流出）を使っていれば、`dynamic_code` とする。
   関数とメソッド（クラスの constructor・フィールドの初期化子・static ブロックを除く）の `this` に、名前の無い
   書き込みか流出（`const self = this` を含み、`return this` を除く）があれば、そのメソッドがどこかの export に
   載りうるので、すべての export の値を述べない（`written_any`）。この 2 つはプログラム全体に効く（Ark は
   モジュールごと。`scoring.md` E2 の commander）。トップレベルで `module` / `exports` を
   宣言するモジュール（`function module() {}`）の `module.exports` は、export ではないので `replaced` とする。
5. 各段で、次を順に確かめる。
   - プロパティの宣言が 1 つであること（`multiple_declarations`）。
   - リテラルの中で、そのプロパティより後ろに spread・計算キー・同じキーが無いこと（`overwritten_in_literal`）。
   - プログラムがその**名前**のプロパティに書き込んでいないこと（`written`）。名前で照合するので、キャスト・
     `any`・別名・呼び出しで束縛される `this` を通しても隠れない（Reviewer B の B-1・B-2）。対象は
     `o.a = b`・`o.a += b`・`o.a++`・`delete o.a`・分割代入の対象・`for (o.a of …)`・`this.a = b`・`o["a"] = b`、
     キーが `a` の `Object.defineProperty`・`Reflect.*`・`__defineGetter__`・`__defineSetter__`（添字で呼ぶ形を
     含む）、`a` を持つ
     リテラルを源とする `Object.assign`・`Object.defineProperties`。
   - 名前の無い書き込みが無いこと（`written_any`）。そのプロパティを持つか `any` の型の式への `o[k] = b`、
     リテラルでない源の `Object.assign` が該当する。
   - そのプロパティを持つ型の式が、値として流れていないこと（`escapes`）。流れる位置は、引数・代入の値・型の
     注釈（JSDoc を含む）のある初期化子・`let`/`var` の初期化子・キャスト・配列やオブジェクトの要素・spread・
     return・throw・yield・`&&`/`||`/`??` の項・`?:` の枝・テンプレートの式である（Reviewer B の B-3）。
     安全な位置は、メンバーの読み取り・呼び出し・比較・条件・`typeof`/`void`/`!`・注釈の無い `const` の
     初期化子・代入の左辺・`module.exports = X` の右辺である。最後のものは再 export
     （`module.exports = require('./index.js')`）にあたり、同じオブジェクトを別のモジュールが export するだけで、
     その使う側も同じ型として走査される。照合は、型のプロパティの Symbol と宣言の同一性で行う（`module.exports` や
     `require()` の型は、リテラルのプロパティに別の Symbol を持つ）。同じファイルの `module.exports` は、
     構文からも export オブジェクトとみなす。
6. 宣言が `ShorthandPropertyAssignment` なら `getShorthandAssignmentValueSymbol` を使う
   （`value_rule: "shorthand"`）。`PropertyAssignment` で初期化子が識別子なら、その識別子の
   `getSymbolAtLocation` を使う（`"identifier_initializer"`）。それ以外は `not_identifier` とする。
7. 値の Symbol が alias なら、その束縛が代入されていれば `alias_reassigned` とする（B-5）。そうでなければ、
   宣言を `value_via` に入れて `getAliasedSymbol` でたどる（失敗したら `alias_unresolved`）。
8. 値の宣言を v1 と同じ規則で絞る（後からのメンバーの代入の宣言を除く）。0 なら `no_value_symbol`、コーパスの
   外なら `external_value`、複数なら `ambiguous_value`、`.d.ts` だけなら `declaration_file_value` とする。
   値がモジュール（ソースファイル、`export =`、`module.exports =`）なら `module_value` とする（B-6）。
9. 値の宣言がまた export オブジェクトのプロパティなら、その宣言を `value_via` に入れて 5 から繰り返す（最大
   16 段。超えたら `chain_limit`）。`value_rule` は段ごとの規則を `>` でつなぐ。それ以外なら、その宣言を
   `value` とする。`value_kind` は、関数・クラスの宣言、および関数式・アロー関数・クラス式で初期化された
   変数なら `function` / `class`、それ以外は `data` とする。

### alias で export を通る site（確認の F-4）

shorthand だけの `module.exports = { a, b }` と `exports.a = a` では、TS の checker が alias を値の宣言まで
たどる。そのため v1 の `decl` がすでに値であり、値の層は要らない。しかし、モジュールやプログラムがその
export に別の値を書く形（関数の中の `this.a = b`、`const e = exports; e.a = b`、`mix(exports)` など）でも、
v1 は値の宣言を正解とする。このため、そこで誤った Exact を返す Ark が TP として数えられ、誤りが隠れていた。

そこで、site の Symbol の alias の連鎖（`getImmediateAliasedSymbol`）が export のプロパティ（export
オブジェクトのプロパティ、`exports.a = …` / `module.exports.a = …` の代入）を通るときは、5 と同じ検査を行う
（宣言そのものの代入は書き込みに数えない）。安全でなければ、`value_unknown` と `export_unsafe` を付ける。
`exports` や `module` の識別子そのものの site は対象にしない（TS は `exports.a = …` の左の `exports` にプロパティの
Symbol を返すが、site はモジュールのオブジェクトである）。

`value_unknown` の理由のうち、`replaced`・`overwritten_in_literal`・`multiple_declarations`・`written`・
`written_any`・`escapes`・`dynamic_code`・`alias_reassigned` を UNSAFE とし、その site には `export_unsafe` を付ける
（混在のリテラルの site も含む）。

### `getShorthandAssignmentValueSymbol` の実証（Phase A・B）

`shorthand-inspect.js` で、semver の 16 件と仕様 fixture の各ケースについて、`getShorthandAssignmentValueSymbol`
とその alias の先を出した（`cases.md`、`semantics.md` B2・B3）。semver の 16 件の値の宣言は、v2 の Oracle の
`value` と完全に一致した（C4）。

### 閉世界の仮定

値の層は、コーパスの外のコード（公開されたパッケージの利用者など）が export オブジェクトに書き込まない
ことを仮定する。そのため `value_kind` で関数・クラスとデータを分け、採点では、データの値に当たった TP を
`value_tp_data` として別に数える（B-7）。semver の 16 件のうち 11 件はデータ（`MAX_LENGTH` などの定数）である。

## C3. 採点（`internal/index/fidelity_baseline_test.go`、契約の版）

`oracleSite` に `Value`・`ValueVia`・`ValueKind` を足した。`declaration` の site では次のように採点する。

- `want`（`decl` の Ark の Symbol）か `value`（`value` の Ark の Symbol）のどちらかがあれば representable と
  する。`value` だけのときは `value_only_representable` に数える。名前の無い `value`（モジュール）は表せない
  ものとして扱う（B-6）。`value` が `want` と同じなら、`value` は無いものとして扱う。
- Ark の Strong / Exact の対象が `want` なら `tp`、`value` なら `tp_value`（`value_tp` に数え、Exact / Strong の
  TP にも入れる。データなら `value_tp_data` にも数える）。`via` なら `via` とする。`value_via` は、`value`
  を数えるときだけ `via` に加える（B-9）。それ以外は `exact_fp` / `strong_fp` とする（v1 と同じ）。
- Candidate は、候補に `want` か `value` があれば `candidate_hit` とする。
- `export_unsafe` の site では、Oracle は値を決められない。そこで Ark の Strong / Exact を TP にも FP にもせず、
  `unsafe_strong` として別に数える（`unsafe_sites`、`unsafe_strong`。`declaration_file_strong` と同じく人手で
  確かめる対象）。Ark の規則では、モジュールの中の書き込みがあれば Exact を出さないので、ここでの Strong /
  Exact は、Oracle の名前による検出の粗さ（無関係なオブジェクトの同じ名前）か、Ark の取りこぼしのどちらかである。
- `value` の無い site（v1 のファイルのすべての site、v2 でも安全でない理由以外の `value_unknown` の site）は、
  v1 とまったく同じに採点する。新しい集計の欄は `omitempty` なので、v1 の `report.json` には現れない。
- 誤りの行（`ARK_FIDELITY_FALSE`）には、`value` があれば `value=` を足す。

`via_stop` の定義（評価 §4: import の束縛や `const x = require(…)` で止まること）は、v2 では「値に至る途中の
束縛や、export オブジェクトのプロパティで止まること」まで広がる。export オブジェクトのプロパティは Ark の
Symbol でないので、実際には束縛だけが当たる。

規則は `TestScoreSiteValueContract` と `TestScoreSiteValueSeveralReferences`
（`internal/index/fidelity_contract_test.go`、CI で実行）で固定した。対象は、v1 の site、値、別の対象、値の
alias、Candidate、メンバーのプロパティ、メンバーの値、`value == want`、名前の無い値、値の無い `value_via`、
データ、1 つの site の複数の参照である。

**v1 の採点が変わらないこと**: 仕様 fixture で、変更前のハーネスと変更後のハーネスの `report.scored` が同一
だった（`cmp`）。Reviewer C は、506cb7c の旧ハーネスで semver と commander の v1 を採点し、同一であることを
確かめた。

### 契約の版

| 版 | Oracle | 正解 | 用途 |
|---|---|---|---|
| v1 | `javascript-tsc.js` | `decl`（TS の Symbol の宣言） | 既存のすべてのスコア（Phase 0〜9、Hardening）。保持する |
| v2 | `javascript-tsc-value.js` | `decl` または `value` | CommonJS の export オブジェクトを通る値の読み取りの意味を採点する |

v2 は v1 より緩くない。正解が増えるのは、CommonJS の export オブジェクトのプロパティが識別子の値を持ち、上の
条件をすべて満たす場合だけである。その値の宣言は、TS が静的に導いたものである。Ark の誤り（別の対象）は、
v1 と同じく FP になる。

## C4. 妥当性の確認

| 確認 | 結果 |
|---|---|
| v1 の Oracle の再生成が、保存済みのものと一致する（12 コーパス） | 12/12 でバイト単位で一致（作業領域の `f6b5/oracles/sums.txt`） |
| v2 の決定性（2 回の生成を比べる。12 コーパス） | 12/12 で一致 |
| v2 から値の層の欄を除くと v1 と同一 | 12/12 |
| semver の値の宣言が、Phase A の独立の導出と一致する | 16/16 |
| fixture の検査（`check-value-fixtures.sh`） | ok |

<!-- E の採点の後に、コーパスごとの値の層の内訳を入れる -->

### fixture と検査（リポジトリに置いた）

`docs/scripts/oracle/value-fixtures/` には次の 2 種類を置いた。

- `spec/`: 仕様の 16 ケースを 1 つのプログラムにしたもの（`semantics.md` B3）。
- `cases/*`: 1 ケースを 1 つのプログラムにしたもの。名前による書き込みは、同じプログラムのすべての export
  オブジェクトに及ぶので、ケースごとに分けた。

`cases/*` のケースは次のとおり。
- 値を述べる形: 書き込みの無い形（`plain`）、2 段の連鎖（`chain`）。
- `written`: 別名を通した書き込み、メソッドと関数式の中の `this.a`、`Object.assign`、`Reflect.defineProperty`、
  `__defineGetter__`、分割代入、`delete`、JSDoc のキャスト。
- `written_any`: 動的なキー。
- `escapes`: 引数（定義側と使う側）、配列、入れ物、JSDoc の注釈。
- その他: `alias_reassigned`（連鎖の途中と site 自身）、`module_value`、`overwritten_in_literal`、
  `replaced`、`dynamic_code`。
- 対象の外: ローカルのオブジェクト、`Object.defineProperty`（TS が宣言として束ねるので v1 で ambiguous）。

`check-value-fixtures.sh <typescript-dir>` は、値の層（`value`・`value_kind`・`value_rule`・`value_via`・
`value_unknown`・`value_unknown_at`）を `expected.tsv` と比べる。期待の各行は、設計の規則から 1 行ずつ
確かめた。

### Oracle のミューテーション（作業領域 `f6b5/mut/oracle3.py`〜`oracle5.py`）

規則を 1 つずつ外し、fixture の検査が失敗するかを見た。40 種のうち 37 種を単独で検出した。

| 外した規則 | 結果 |
|---|---|
| O1〜O17、O19、O20、O22〜O29、O31〜O40（名前による書き込み、escape、名前の無い書き込み、識別子でない値、`value_via`、`identifier_initializer`、export オブジェクトに限る、shorthand の site、プロパティの連鎖、リテラルの中の上書き、動的なコード、alias の再代入、モジュールの値、書き込みの site、置き換え、値として使う位置、宣言による照合、`__defineGetter__`、ラッパーの経路、`module` の宣言、再 export、export の連鎖の安全性、`exports`・`module` の site、`module[…]`、`require`・`module` の先の経路、宣言そのものの代入、`&&` の左の項、`return this`、関数の `this` の名前の無い書き込み・流出・別名、`require` の流出、`require('module')`・`require(__filename)`、constructor の `this`、mutator の第 1 引数の `this`） | 検出 |
| O18 構文の `module.exports`、O30 `exports.a` の構文の照合 | 単独では検出されない（O17 の型による照合と重なる）。O17 と同時に外すとどちらも検出される |
| O21 宣言が 2 つ以上 | 単独では検出されない（名前による書き込みの検出が同じ形を捕らえる） |

## C5. 限界

- 値の層は、「CommonJS の export オブジェクトのプロパティの初期化子の値のうち、プログラムの中に書き込みも
  流出も動的なコードも見えないもの」である。実行時の値の完全な解析ではない。閉世界を仮定する（C2）。
- 名前による書き込みの検出は粗い。同じ名前のプロパティへの無関係な書き込みでも、値を述べなくなる（例:
  仕様 fixture では、m11 の `module.exports.parse = other` が m2 の `parse` の値も消す）。これは安全側の
  近似で、v2 の分母を小さくするだけである。
- TypeScript のファイル（.ts）では、`module.exports` は CommonJS の export として扱われない（v1 が
  `unknown` を答える）。値の層も対象にならない。
- `value_unknown` の site は、v1 の規則で採点する（値の層で正解を足さない）。

## C6. 経緯（v2 の Oracle の版）

1. 最初の版: すべてのオブジェクトリテラルのプロパティを対象にし、書き込みを見ていなかった。
2. 書き込み（Symbol の同一性）と escape（引数の直下）を加えた。three の `settings.endingStart` で、書き換え
   られるプロパティに値を述べていたためである。
3. プロパティの連鎖を加えた。自己検査のミューテーションで、連鎖の途中で止まって Ark の正しい答えを FS に
   していたことが分かったためである。
4. Reviewer B の指摘（B-1〜B-11）に対応した最終版: 対象を CommonJS の export オブジェクトに絞り、名前による
   書き込み、流出の位置の拡張、動的なコード、alias の再代入、モジュールの値、置き換え、リテラルの中の上書き、
   書き込みの site、`value_kind`、`value_unknown_at`、理由の細分（`alias_unresolved`・`chain_limit`）を加えた。
   再レビューの N-4〜N-6（ラッパーの `arguments`・`require.cache`、添字で呼ぶ `__defineGetter__`、トップレベルの
   `module` の宣言）と、確認の F-3・F-4（`module[…]`・`require.main` などの経路、alias で export を通る site の
   検査、`export_unsafe` と `unsafe_strong`）もこの版に加えた（6 番目の版）。

5. 再 export の右辺を安全な位置にした（最終版）。4 の版で、semver の `preload.js`
   （`module.exports = require('./index.js')`）が流出とみなされ、`bin/semver.js` の 5 件（`semver.coerce` など）が
   `escapes` になった。再 export はオブジェクトを書き換えず、その使う側の書き込みと流出は同じ規則で見つかる
   ので、一般の規則として除外した。fixture の `reexport`（値を述べる）と `reexport-escape`（使う側の流出で
   `escapes`）で固定した。

6. 7 番目の版（最終版）: 3 回目の確認の指摘に対応した。`&&` の左の項と `return this` を流出から外した。
   関数とメソッド（constructor などを除く）の `this` に名前の無い書き込み（`this[k] = v`、
   `Object.assign(this, src)`）か流出（`const self = this` を含む）があれば、すべての export の値を述べない
   （`written_any`）ようにした。`require` の流出、`require('module')`、`require(__filename)`、
   `process.mainModule` を動的なコードとした。

1〜6 番目の版の変更では、Ark の Strong / Exact の判定（TP・FP）が変わった site は無く、変わったのは分母だけ
だった。7 番目の版では、commander の 13（8e83a02）・14（d401ad1・現在）の TP が `unsafe_strong` に移った
（FP への変化は無い。`scoring.md` E2・E6）。
