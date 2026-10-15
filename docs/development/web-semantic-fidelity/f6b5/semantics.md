# Phase B: CommonJS の shorthand export の参照の意味論

> **現在の契約（D39、`../commonjs-boundary/contract.md`）**: この文書の「Ark の対象」（Semantic Value Target）は、
> CommonJS の **静的な export の対応**（どの宣言を export の文が指すか）として、今も有効である。一方、表の
> Confidence（Exact / Strong）は、ts-14 の時点の記録である。ts-16 以降、CommonJS の export を通る参照は、
> この対象を **Candidate** として返す（値の同一性は主張しない）。B3・B4 の「出さない」規則（書き込みが見える
> export を落とす）は、Candidate を出さない規則として残っている。

## B1. 区別する層

`module.exports = { parse }` と、別の module の `m.parse()` / `const { parse } = require("./m")` について:

| 層 | 何か | この例では |
|---|---|---|
| Property Declaration | export されるオブジェクトのプロパティそのものの宣言（構文の節点） | `{ parse }` の `ShorthandPropertyAssignment`（`m.js:2:20`） |
| Value Binding | shorthand の識別子が値として読む字句的な束縛 | `parse`（`function parse() {}`、`m.js:1:10`） |
| Export Binding | CommonJS の `module.exports` が外へ公開する名前と、その値 | `"parse"` → Value Binding |
| Semantic Value Target | 値の読み取りが静的に指す宣言（Value Binding を alias の先までたどったもの） | `function parse() {}` |
| Reference Site | Ark が参照を記録する位置 | 使う側の `m.parse` の `parse`、`const { parse }` から読んだ名前 |

TypeScript の checker の Symbol（旧 Oracle の答え）は、使う側の site から alias を 1 回たどった先の Symbol で、
それが **Property Declaration**（`flags=Property`）のことも、**Value Binding の先の宣言**のこともある（下）。
Ark の参照の対象は **Semantic Value Target**（Ark は CommonJS の export のオブジェクトのプロパティを Symbol に
せず、ESM の `export { parse }` と同じく export の束縛 `Exported → Local` として表す。D12・Phase 2）。

## B2. TypeScript の checker 自身の扱い（TS 5.6.3、`docs/scripts/oracle/shorthand-inspect.js` で観測）

TS の binder は、`module.exports = { … }` の右辺が **shorthand のプロパティだけ** のとき、各プロパティを
export の alias（`flags=Alias|Assignment`）として束縛し、`getImmediateAliasedSymbol` が Value Binding の宣言に
進む。プロパティが 1 つでも shorthand でない（`X: 1`、`q: q`、`r: require(...)`、spread）と、`{ parse }` も
ただの `Property` になり、alias は無い（作業領域 `f6b5/spec2/` の 5 形で確認）。

| `module.exports =` の右辺 | 使う側の `m.parse` の Symbol | 旧 Oracle の宣言 |
|---|---|---|
| `{ parse }` | `Alias|Assignment` → `Function` | `function parse`（Value の宣言） |
| `{ parse, X: 1 }` | `Property` | `{ parse }` のプロパティ |
| `{ parse, q: q }` | `Property` | 同上 |
| `{ parse, r: require("./a") }` | `Property` | 同上 |
| `{ parse, ...{} }` | `Property` | 同上 |

**shorthand の式 `parse` の値は、兄弟のプロパティに依らない**（ECMAScript の object initializer の評価では、
shorthand `parse` の値は識別子 `parse` の参照の値）。したがって旧 Oracle の答えが Property になるか Value の
宣言になるかは、意味ではなく TS の binder の形の判定で決まる（TS 5.6.3 の `bindModuleExportsAssignment`:
`every(properties, isShorthandPropertyAssignment)` のときだけ `bindExportAssignedObjectMemberAlias`。Phase B の
レビューで確認）。ただし **export されるプロパティの値**は、後ろの兄弟に依ることがある。object initializer は
プロパティを左から順に定義する（PropertyDefinitionEvaluation）ので、後ろの spread（CopyDataProperties）・計算
キー・同じキーは、前のプロパティを上書きする（B4 の規則 2）。semver の `index.js`（`SemVer:
require(...)` などを含む）と `internal/constants.js`（`FLAG_INCLUDE_PRERELEASE: 0b001` などを含む）はどちらも
混在の形で、16 件はすべて後者の扱いを受けた。

## B3. 仕様ケース（作業領域 `f6b5/spec/`、TS と Ark の観測）

「使う側」は `use.js`（`const mN = require("./mN")` と関数 `uN() { return mN.parse() }`）。Ark の列は
`d401ad1` の観測。期待は B4 の規則から決めた。

| # | 形 | Property Declaration | Value Binding | 旧 Oracle（TS の Symbol） | 期待する Ark の対象 | 最大の確信度 | Ark の観測 |
|---|---|---|---|---|---|---|---|
| 1 | `module.exports = { parse }` | `{ parse }` | `function parse` | Alias → `function parse` | `function parse` | Exact | Exact `function parse` ✓ |
| 2 | `{ parse: parse }` | `parse: parse` | `function parse` | Property（`parse: parse`） | `function parse` | Exact | Exact ✓（旧 Oracle とは食い違う） |
| 3 | `{ execute: parse }`（`m.execute()`） | `execute: parse` | `function parse` | Property（`execute: parse`） | `function parse` | Exact | Exact ✓（同上） |
| 4 | `function create(parse) { return { parse } }` | `{ parse }`（export でない） | 引数 `parse` | shorthand の site は Value（引数） | なし（ローカルの使用は参照にしない） | — | 参照なし ✓ |
| 5 | `let parse = function first(){}; parse = function second(){}; module.exports = { parse }` | `{ parse }` | `let parse`（1 つの宣言） | Alias → `let parse` | `let parse` の宣言（束縛の identity）。値は実行時に `second` だが、字句的な束縛先は変わらない | Exact（束縛として） | Exact `let parse` ✓ |
| 6 | `const parse = require("./parser"); module.exports = { parse }` | `{ parse }` | `const parse`（alias） | Alias → … → `parseImpl` | `parser.js` の `function parseImpl`（alias の先） | Exact | Exact ✓ |
| 7 | `{ parse: api.parse }` | `parse: api.parse` | なし（値はメンバーの式） | Property | 出さないか Unresolved（メンバーの値は Ark の export の束縛で表さない） | — | Unresolved ✓ |
| 8 | getter: `module.exports = api`、`get parse()` | getter | なし（実行時の戻り値） | GetAccessor | `m8` の読み取りは `api`（オブジェクト）。`m8.parse()` はメンバーの getter `api.parse`（Property の層。ケース 14 と同じ） | Strong | `api` の constant ✓。`m8.parse()` は Strong `api.parse` ✓（呼び出しのない `m8.parse` の読み取りは出さない） |
| 9 | `{ tools: { parse } }`（`m.tools.parse()`） | 内側の `{ parse }` | `const parse` | Property（内側） | 出さないか Candidate（ネストした export は表さない） | Candidate | 名前の Candidate ✓（Strong にならない） |
| 10 | `const { parse } = require("./parser2"); module.exports = { parse }` | `{ parse }` | 分割代入の束縛（alias） | Alias → … → `function parse`（parser2） | `parser2.js` の `function parse` | Exact | Exact ✓ |
| 11 | `module.exports = { parse }; module.exports.parse = other` | `{ parse }` + 代入 | `const parse`、後に `other` | Alias（宣言が 2 つ） | 出さない（2 つの値。F9A-2） | — | Unresolved ✓ |
| 12 | ESM `export { parse }` | `export { parse }` | `function parse` | Alias → `function parse` | `function parse` | Exact | Exact ✓ |
| 13 | TS `export { parse }` | 同上 | 同上 | 同上 | 同上 | Exact | Exact ✓ |
| 14 | `const obj = { parse }; obj.parse()`（export でない） | `{ parse }` | `function parse` | Property（`obj.parse`） | `obj.parse`（Ark はオブジェクトのメンバーを Symbol にする。D11） | Strong（値のメンバー） | Strong `obj.parse` ✓ |
| 15 | `module.exports = { parse }`（`parse` の宣言が無い） | `{ parse }` | なし | Alias、値の Symbol なし | Unresolved（Outside でない） | — | Unresolved ✓ |
| 16 | ブロックの中の `module.exports = { parse }`（内側の `const parse`） | `{ parse }` | 内側の `const parse` | Alias → 内側の `const` | 出さないか内側の `const`（トップレベルの文でない export は表さない） | — | Unresolved ✓（外側の `function parse` には結ばない） |

### B3 の追加（Phase B のレビュー R-1・R-2 の後。`TestECMAScriptCommonJSOverwrites`）

| 形 | 実行時の `m.parse` | 期待 | d401ad1 の Ark | 修正後（ts-14） |
|---|---|---|---|---|
| `{ parse, ...require('./ov') }` | `ov` の parse | 出さない | **Exact `parse`（誤り）** | 出さない ✓ |
| `{ parse, X: 1, ...ext }` | `ext.parse` | 出さない | **Exact（誤り）** | 出さない ✓ |
| `{ parse, [k]: other }` | `other` | 出さない | **Exact（誤り）** | 出さない ✓ |
| `{ parse, X: 1, parse() {} }`、`{ parse, 'parse': other }` | 後ろのもの | 出さない | **Exact（誤り）** | 出さない ✓ |
| `{ parse, X: 1, parse: other }` | `other` | `other`（最後のものが残る） | Candidate | Exact `other` ✓ |
| `{ ...require('./ov'), parse }` | `parse` | `parse` | Exact ✓ | Exact ✓ |
| 後で `Object.assign(module.exports, { parse: other })`、`Object.defineProperty(exports, 'parse', …)` | `other` | 出さない | **Exact（誤り）** | 出さない ✓ |
| 関数の中の `module.exports.parse = other`、`module.exports.parse += 1`、`delete module.exports.parse` | 変わる | 出さない | **Exact（誤り）** | 出さない ✓ |
| `module.exports[k] = other`（計算キー） | 変わりうる | 名前の export をすべて出さない | **Exact（誤り）** | 出さない ✓ |
| `module.exports['parse'] = other` | `other` | `parse` だけ出さない（`keep` は残る） | **Exact（誤り）** | 出さない、`keep` は Exact ✓ |
| 分岐の中の `module.exports = { parse: other }` | 変わりうる | 出さない | **Exact（誤り）** | 出さない ✓ |
| `const obj = { parse }; module.exports = obj` | `parse` | メンバー `obj.parse`（Property の層） | Strong `obj.parse` | Strong `obj.parse`（既知の非対称。B4 の規則 4） |

これらはいずれも、12 の評価コーパスには現れない（Phase E: 修正の前後で採点が同一）。

### B3 の追加 2（Phase F の Reviewer A・C の後。`TestECMAScriptCommonJSOtherWrites`・`TestECMAScriptCommonJSWriteScope`）

Reviewer A が、修正前の Ark（HEAD）と最初の修正の両方で、まだ誤った Exact が出る形を示した。修正後（ts-14 の
最終）の挙動は次のとおり。

| 形 | 実行時の `m.parse` | 修正後 |
|---|---|---|
| `module.exports = { parse }` の後にトップレベルで `module.exports = function () {}`、`= require('./base')` | 別の値 | 出さない（最後の置き換えに無い名前は、前の置き換えから落とす） |
| `exports.__defineGetter__('parse', …)` | getter | 出さない |
| `const e = exports; e.parse = other`、`mix(exports)`、`(function (e) { … })(exports)`、`x && exports` | 書き換えうる | 出さない（export オブジェクトが値として流れたら、名前つきの export をすべて落とす） |
| トップレベルの `this.parse = other`、`Object.assign(this, …)`、トップレベルのアロー関数の中の `this` | `other` | 出さない（トップレベルの `this` は `exports` と同じオブジェクト） |
| `(exports).parse = other`、`(module.exports).parse`、TS の `(module.exports as any).parse`、`module.exports!.parse` | `other` | 出さない（括弧・`as`・`satisfies`・`!`・`<T>` を外して判定） |
| `module['exports'].parse = other`、`module['exports'] = { … }` | `other` | 出さない |
| `module.exports.p\u0061rse = other`、`{ parse, p\u0061rse: other }` | `other` | 出さない（エスケープを含む名前は「任意の名前」） |
| `for (exports.parse of [other]);` | `other` | 出さない |
| `exports = {}; exports.parse = parse`、トップレベルの `var exports = {}` | export されない | 出さない（`exports` の再束縛で、プロパティの export を落とす） |
| `Object.defineProperty(module, 'exports', …)`、`const { assign } = Object; assign(exports, …)` | 別の値 | 出さない（`module` や `exports` が引数として流れる） |
| `{ parse, __proto__: other }` の `m.__proto__` | プロトタイプ | `__proto__` を export しない（プロパティを作らない） |
| export のメソッド・関数・getter の中の `this.parse = other`、`this[k] = other`（再レビューの N-1）、`var self = this; self.parse = other`・`Object.assign(this, …)`・`return this`・クラスの static メソッドや取り出したメソッドの `this`（確認の F-1・F-2） | `exports.init()` などで `other` | 出さない。関数やオブジェクトのメソッドの `this` は、呼ばれたオブジェクトであり、export オブジェクトのこともある。そこで `exports` と同じ規則で数える（書き込みは名前で、流出・計算キー・`Object.assign` などはすべて）。クラスのメソッドの `this` も同じに扱う（メソッドは `exports.m = A.prototype.m`・`defineProperty`・別名などのどの経路でも export に載りうる。確認の F-2 と、その後の判定の漏れの指摘による）。constructor・フィールドの初期化子・static ブロックの `this` は、常に新しいインスタンスかクラスとみなす。`return this`（アロー関数の式の本体の `this` を含む）は流出に数えない |
| `with (module.exports) { … }`、直接の `eval(…)`（N-2） | 実行時に決まる | すべての export を出さない（`new Function` も、大域のコードが `process` を通して export に届くので落とす。release-gate の H3 で訂正） |
| `class A { static e = module.exports }`、`const { e = module.exports } = {}`、`` tag`${module.exports}` ``（N-3） | 書き換えうる | 出さない（流出） |
| `arguments[2].exports = …`、`require.cache[__filename].exports = …`（N-4） | 別の値 | すべての export を出さない |
| `module.exports['__defineGetter__']('parse', …)`（N-5） | getter | 出さない |
| トップレベルの `function module() {}`、`function exports() {}`（N-6） | export されない | 出さない |
| `module[k] = …`、`` module[`exports`].parse = other ``、`require.main.exports.parse = other`、`require['cache'][f].exports = …`、`module.constructor._cache[f].exports.parse = other`（確認の F-3）、`const r = require`、`require('module')`、`require(__filename)`、`process.mainModule.exports`（その後の確認） | 別の値 | すべての export を出さない（`module` の静的でないキーは `module.exports` とみなす。`require.cache`・`require.main`・`module.constructor`・`parent`・`children`・`process.mainModule` は、その先をたどる場合に限って数える。`require.main === module`・`!module.parent` は数えない） |

逆に、修正が正しい Exact を失っていた形（Reviewer A の A-2・A-4、Reviewer C の C-1・C-2）は次のように直した。

| 形 | 修正後 |
|---|---|
| `Object.defineProperty(exports, "__esModule", { value: true })`（tsc・Babel の CommonJS 出力の定型） | 静的なキーは、その名前だけを落とす。`exports.bf = bf` は Exact のまま |
| `Object.assign(module.exports, { b: 1 })`、`Object.defineProperties(exports, { b: … })` | リテラルのキーがすべて静的なら、その名前だけを落とす |
| `function factory(module) { module.exports = {} }`、`function install(exports) { exports.pa = 1 }`、アロー関数・catch の引数 | 引数が束縛する範囲の書き込みは数えない |
| `require.main === module`、`typeof exports === 'object'` | 比較・`typeof` は値の流出ではない。export は残る |
| tsc の CommonJS 出力 `exports.keep = exports.parse = void 0;` の後の `exports.parse = parse`（再レビューの N-8） | `void 0` の連鎖は、同じ名前の export より前にある限り、書き込みとして数えない。`parse`・`keep` は Exact。export の後の `exports.parse = void 0` は書き込みとして数える |

**既知の近似（安全側。誤りは出さず、Recall を失う）**

- 関数の中のローカル変数として宣言された `exports` / `module`（引数でないもの）への書き込みは、モジュールへの
  書き込みとして数える（どの書き込みが局所のものか、走査が決められない場合は書き込みとみなす）。
- `module.exports = { … }` の後のトップレベルの `this.x = …` は、古いオブジェクトへの書き込みだが、数える。
- tsc の `__exportStar(require('./a'), exports)`・`__createBinding(exports, …)` のように、`exports` を関数に渡す
  ファイルでは、名前つきの export をすべて落とす（呼ばれる関数が既存のプロパティを上書きしないかは、静的に
  決められない。Babel の `_exportStar` 相当は上書きしうる）。
- `exports = module.exports = { … }` の後の `exports.parse = other` は、修正前は正しく `other` だったが、今は出さ
  ない（トップレベル以外の `module.exports` の置き換えとして扱う）。
- 関数とメソッド（クラスのメソッドを含み、constructor を除く）の中の `this.x = …` は、その関数がどのオブジェクトに
  対して呼ばれても、名前 `x` の書き込みとして数える。`this[k] = …`・`var self = this`・`Object.assign(this, …)`・
  `f(this)` があれば、名前つきの export をすべて落とす（ES5 のコンストラクタ関数、`Object.assign(this, opts)` の
  メソッドを持つクラスなど）。
- トランスパイルされた CommonJS のヘルパーの本体（tsc の `__assign.apply(this, arguments)`、Babel の
  `_classCallCheck(this, X)` など）は、`this` を引数として流すので、そのファイルの名前つきの export をすべて落とす
  （ヘルパーの意味は静的に決めない。再確認の N-5。`(this && this.__awaiter)` の頭部と `__generator` の
  `return this` は落とさない）。
- `a && b` の左の項 `a` は、偽値のときにしか値として残らないので、流出に数えない（tsc のヘルパー
  `(this && this.__awaiter) || …` は export を落とさない。確認の F-5）。
- `console.log(module.exports)`、`Object.prototype.hasOwnProperty.call(exports, 'x')`、`window.lib = module.exports`
  のように、書き換えない関数への引数や代入も流出として数え、名前つきの export を落とす（再レビューの N-8）。

**既知の制限（Ark が誤った Exact を返しうる。静的な範囲の外）**

- 他のモジュールの書き込み（`require('./m').parse = other`、使う側の束縛を通す書き込み）。B4 の規則 5。
- export オブジェクトのメソッドの戻り値を通す書き込み（`module.exports.valueOf()[k] = other`、`return this` の
  メソッドの戻り値への書き込み。再レビューの N-7、その後の確認）。
- 自分自身をパスで require する形（`require('./m').parse = other`）と、他のモジュールの関数を export に載せ、その
  関数の `this` が書く形（`exports.init = require('./a').init`）。B4 の規則 5 と同じく、モジュールをまたぐ。
- トップレベルの `return` より後ろの export（実行されないことがある。N-7）。

## B4. 規則（不変条件）

1. **shorthand の識別子は、プロパティの名前であると同時に、値として読まれる束縛である。** Ark は CommonJS の
   export のオブジェクトを export の束縛（`Exported → Local`）として表す（Phase 2、D12）。使う側の参照
   （`m.parse`、`const { parse } = require(...)` の読み取り）の対象は、その束縛の **Semantic Value Target** で、
   ESM の `export { parse }`（ケース 12・13）と同じ。
2. **対象は静的な束縛の証拠だけで決める。** Exact になるのは、次のすべてが成り立つときである。
   - export が、トップレベルの文の `module.exports = { … }` 1 回である。
   - オブジェクトリテラルの中で、そのプロパティより**後ろに** spread も、静的でないキー（計算キー、
     エスケープを含む文字列）も、同じキーのプロパティ（メソッド・getter・文字列のキーを含む）も無い。
     同じキーが後ろにあれば、最後のものが export になる。
   - モジュールの中に、その名前への他の書き込みが無い。トップレベル以外の文、複合代入、`++`/`--`、`delete`、
     分割代入の対象による書き込みが該当する。ケース 11 は F9A-2 により export を出さない。
   - export オブジェクト全体への書き込みが無い。`Object.assign` / `Object.defineProperty` /
     `Object.defineProperties` / `Reflect.set` / `Reflect.defineProperty` / `Reflect.deleteProperty` の第 1 引数が
     `module.exports` か `exports` である場合と、計算キーによる書き込みが該当する。
   - トップレベル以外で `module.exports` を置き換えていない。
   - モジュールの中で export オブジェクトが値として流れていない。引数、初期化子、代入の右辺、配列、プロパティの
     値、spread、return・throw・yield、`&&`・`||`・`??` の項、`?:` の枝が「流れる」位置にあたる。
     export オブジェクトとは、`exports`、`module.exports`、トップレベルの `this` を指す（括弧と TS の
     包む式を外して判定する）。関数の引数が `module` や `exports` を束縛する範囲は、そのモジュールのものでは
     ない。
   - トップレベルの `module.exports = …` が 2 回以上あれば、最後のものに無い名前は export しない。`exports` を
     別のオブジェクトに束縛し直していれば、`exports.a = f` の export を出さない。
   - プロパティの値が識別子（shorthand か `name: ident`）であり、その束縛がモジュールのトップレベルの宣言か、
     一意に解決する import / require の束縛（alias の先まで一意）である。
   - それ以外の形（メンバーの式、getter、ネスト、ブロックの中、宣言なし）では、export を出さないか、
     Unresolved / Candidate にする（ケース 7・9・15・16）。
3. **値の動的な変化と、字句的な束縛先の変化を混同しない。** `let` を後で書き換えても（ケース 5）、束縛先の
   宣言は 1 つで変わらない。Ark の対象は束縛の宣言（TS の `getShorthandAssignmentValueSymbol` と同じ）。
   束縛先が字句的に変わる形（遮蔽、ブロック）は、上の規則 2 で出さない。補足として、CommonJS の export が
   持つのは `module.exports = { … }` を評価した時点の値のスナップショットであり、live binding ではない。
   export の後に `parse = …` と書き換えても、export には届かない（ESM の `export { parse }` なら届く）。
   それでも対象は同じ束縛の宣言である。規則 1 の「ESM と同じ」は、対象が宣言である範囲に限って成り立つ。
4. **export でないオブジェクトリテラル**（ケース 14）では、Ark はメンバーを Symbol にし、その Symbol を対象に
   する（Strong が上限、D11）。export でないオブジェクトを変数経由で export する形
   （`const obj = { parse }; module.exports = obj`）も同じである。`module.exports = { parse }` と実行時の意味は
   同じだが、答えの層（Property と Value）が違う。これは **Ark の既知の非対称** である（Phase B のレビュー
   R-2）。どちらの層も TS の checker の答えの一方と一致し、評価の契約 v2 はどちらも正解とする
   （`oracle-design.md`）。
5. **確信度の上限**: 規則 2 を満たす CommonJS の export を通る参照は Exact（ESM と同じ）。**既知の制限**
   として、**他のモジュール**のコードが使う側の束縛を通して書き換える形（`require('./m').parse = x`、
   `const m = require('./m'); m.parse = x`）は見ていない。リポジトリの中で静的に見える書き込みでも、Ark の
   export の表はモジュールごとに作られ、他のファイルの書き込みを知らないためである（Phase B のレビュー
   R-3。そのときの Ark は Exact のまま）。ESM の名前空間は外から書き換えられない（[[Set]] は false）が、
   CommonJS の `module.exports` は普通の可変オブジェクトなので、この違いは残る。評価では、v2 の Oracle がプログラム全体の書き込み・流出・動的なコードを名前と型で見て、そのような
   export を通る site に `export_unsafe` を付ける。そこでの Ark の Strong / Exact は `unsafe_strong` として
   別に数えるので、TP として隠れない（shorthand だけのリテラルや `exports.a = f` の形も含む。`oracle-design.md`）。
   12 の評価コーパスには現れなかった。

## B5. 結論（Phase B）

- 16 件はすべてケース 1〜3 の形（混在のオブジェクトリテラルの shorthand、または値を識別子で与えるプロパティ）
  で、規則 1・2 を満たす。Ark の対象は Semantic Value Target として正しい。
- 旧 Oracle の答え（Property）は、TS の binder が混在の形で alias を作らないことによる。ケース 1 と、同じ
  意味のケース 2・3 で答えの層が変わる（Value と Property）ので、旧 Oracle は「値の読み取りの対象」の Oracle
  としては非対称。
- semver の 16 件と B3 の 16 ケースでは、Ark の誤り（Actual Wrong Ark Target）は 0 件だった。
- ただし Phase B のレビュー（R-1）で、表の外の形に **Ark の実際の誤り** が見つかった。プロパティより後ろの
  spread・計算キー・同じキーによる上書きと、`Object.assign(module.exports, …)` などモジュールの中の他の
  書き込みである。これらでは、実行時の値が別のものなのに Ark は元の宣言を Exact と答えていた。Phase D で
  規則 2 のとおりに修正した（`ts-14`、B3 の追加の表）。


## CommonJS の Confidence の境界での改訂（D39、ts-16）

リリース判定の監査（台帳はアーカイブ、`../README.md`）では、反射的な書き込みを構文の規則（ts-15）で封じようとした。しかし、独立レビューで
閉じられないことが分かり、打ち切った。その監査の層は、リリースの版には含まれない。代わりに、CommonJS の export を通る
参照を、Candidate を上限とした（`../commonjs-boundary/`）。

そのため、下の形は誤った Exact にはならない。Candidate の対象が、実行時の値と違いうる形として残る（静的な export の
対応の限界）:

- 他のモジュールからの書き込み（`require('./m').parse = other`、自分自身をパスで require する形、他のモジュールの
  関数を export に載せてその `this` が書く形）。B4 の規則 5。
- メソッドの戻り値を通す書き込み（`return this` のメソッドの結果への書き込み、`get me() { return this }` の getter を
  通す `module.exports.me.parse = other`）。
- トップレベルの `return` より後ろの export。
- 大域のオブジェクトの汚染（`Object.prototype` など）や、反射的な経路（Function のコンストラクタ、スタックのフレーム、
  文字列を組み立てたプロパティ名）。
