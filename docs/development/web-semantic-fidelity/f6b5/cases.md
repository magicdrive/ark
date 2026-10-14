# Phase A: semver の 16 件

## 再現

`d401ad1` の Ark で semver 7.6.3 を固定の旧 Oracle で採点し（`TestFidelityBaseline`）、`report.false` の 16 件を
得た。分類はすべて自動の `local_fp`（Oracle の宣言が Ark のシンボルでないのに Ark が Strong / Exact）。

各 site を TypeScript 5.6.3 の Compiler API（`docs/scripts/oracle/shorthand-inspect.js`。旧 Oracle と同じ
program の設定）で調べた。**Ark の出力は正解の根拠に使っていない**: Ark の解決先は、TS が独立に導いた値の
宣言と位置を突き合わせただけ。検査の全出力は作業領域 `f6b5/inspect-16.txt`。

## 個別の結果

列: site（ファイル:行:列、1 始まり、バイト列）、式、site の AST の親、Oracle（旧）の宣言、TS の
`getShorthandAssignmentValueSymbol` から alias（`getImmediateAliasedSymbol`）をたどった値の宣言、Ark の解決先と
確信度、分類。

| # | site | 式 | 親 | 旧 Oracle の宣言（Property） | TS の値の宣言（Value） | Ark | 分類 |
|---|---|---|---|---|---|---|---|
| 1 | `bin/semver.js:103:29` | `semver.coerce(...)` | PropertyAccess（呼び出し） | `index.js:66:3` `{ coerce }` | `index.js:28:7` `const coerce = require('./functions/coerce')` → `functions/coerce.js:5:7` `const coerce = (...) =>` | `functions/coerce.js:5:7` function, Exact | 1（値の側に alias 1 段） |
| 2 | `bin/semver.js:105:19` | `semver.valid(...)` | 同上 | `index.js:45:3` | `index.js:7:7` → `functions/valid.js:2:7` | `functions/valid.js:2:7`, Exact | 1（alias 1 段） |
| 3 | `bin/semver.js:116:21` | `semver.satisfies(...)` | 同上 | `index.js:69:3` | `index.js:31:7` → `functions/satisfies.js:2:7` | `functions/satisfies.js:2:7`, Exact | 1（alias 1 段） |
| 4 | `bin/semver.js:124:22` | `semver.clean(...)` | 同上 | `index.js:46:3` | `index.js:8:7` → `functions/clean.js:2:7` | `functions/clean.js:2:7`, Exact | 1（alias 1 段） |
| 5 | `bin/semver.js:125:28` | `semver.inc(...)` | 同上 | `index.js:47:3` | `index.js:9:7` → `functions/inc.js:3:7` | `functions/inc.js:3:7`, Exact | 1（alias 1 段） |
| 6 | `classes/semver.js:22:26` | `MAX_LENGTH`（比較の項） | BinaryExpression | `internal/constants.js:27:3` | `internal/constants.js:5:7` `const MAX_LENGTH = 256` | `internal/constants.js:5:7` constant, Exact | 1 |
| 7 | `classes/semver.js:24:35` | `MAX_LENGTH`（テンプレート） | TemplateSpan | `internal/constants.js:27:3` | `internal/constants.js:5:7` | 同上 | 1 |
| 8 | `classes/semver.js:48:22` | `MAX_SAFE_INTEGER` | BinaryExpression | `internal/constants.js:30:3` | `internal/constants.js:6:7` `const MAX_SAFE_INTEGER = ...` | `internal/constants.js:6:7`, Exact | 1 |
| 9 | `classes/semver.js:52:22` | 同上 | BinaryExpression | 同上 | 同上 | 同上 | 1 |
| 10 | `classes/semver.js:56:22` | 同上 | BinaryExpression | 同上 | 同上 | 同上 | 1 |
| 11 | `classes/semver.js:67:33` | 同上 | BinaryExpression | 同上 | 同上 | 同上 | 1 |
| 12 | `internal/re.js:158:27` | `MAX_SAFE_COMPONENT_LENGTH` | TemplateSpan | `internal/constants.js:28:3` | `internal/constants.js:10:7` `const MAX_SAFE_COMPONENT_LENGTH = 16` | `internal/constants.js:10:7`, Exact | 1 |
| 13 | `internal/re.js:159:31` | 同上 | TemplateSpan | 同上 | 同上 | 同上 | 1 |
| 14 | `internal/re.js:160:31` | 同上 | TemplateSpan | 同上 | 同上 | 同上 | 1 |
| 15 | `internal/re.js:26:11` | `MAX_LENGTH` | ArrayLiteral | `internal/constants.js:27:3` | `internal/constants.js:5:7` | `internal/constants.js:5:7`, Exact | 1 |
| 16 | `internal/re.js:27:22` | `MAX_SAFE_BUILD_LENGTH` | ArrayLiteral | `internal/constants.js:29:3` | `internal/constants.js:14:7` `const MAX_SAFE_BUILD_LENGTH = MAX_LENGTH - 6` | `internal/constants.js:14:7`, Exact | 1 |

1〜5 は Hardening の前（`8e83a02`）からある（呼び出し）。6〜16 は Hardening の値の読み取り（D35）で新しく出た。

### 束縛の経路（TS の Symbol の連鎖）

- 1〜5: site の symbol は `Property`（`index.js` の `module.exports = { … }` の shorthand プロパティ）。
  `semver` は `const semver = require('../')`（index.js）。`semver.coerce` の `coerce` は、その module の export の
  プロパティとして解決される。
- 6〜16: site の symbol は `Alias`（`const { MAX_LENGTH, … } = require('../internal/constants')` の BindingElement）、
  `getImmediateAliasedSymbol` の先が `Property`（`constants.js` の `module.exports = { … }` の shorthand
  プロパティ）。旧 Oracle は alias を 1 回だけ解決して（`getAliasedSymbol`）このプロパティで止まる。
- どの shorthand プロパティでも、`getShorthandAssignmentValueSymbol` は同じファイルの同名の `const`（1〜5 は
  さらに `require` の alias で別のファイルの `const`）を返した。

### 値が変わりうるかの確認

- `index.js`・`internal/constants.js` の `module.exports` への代入は各 1 回、shorthand のオブジェクトリテラル。
- コーパス全体に、これらのプロパティへの書き込み（`x.coerce = …` など）、`Object.assign`、`defineProperty` は無い
  （grep で確認）。値の束縛はすべて `const`。

## 分類（ミッションの 7 分類）

| 分類 | 件数 |
|---|---:|
| 1. Property Symbol vs Value Declaration | **16**（うち 5 件は値の側で `require` の alias を 1 段通る。alias の解決は TS 自身の `getImmediateAliasedSymbol`） |
| 2. Exported Property vs Local Binding | 0（別に数えない: 1 は「export されたプロパティ」と「その値の束縛」の違いそのもの） |
| 3. Alias / Re-export | 0（1 の内訳として記録） |
| 4. Actual Wrong Ark Target | **0**（16 件とも、Ark の解決先は TS が独立に導いた値の宣言と位置が一致） |
| 5. Ambiguous Semantic Target | 0（値の束縛は 1 つの `const` に決まる。プロパティへの書き込みも無い） |
| 6. Oracle Site Mismatch | 0（site の位置は両方で一致） |
| 7. Other | 0 |

## Phase A の結論と未確認事項

- 16 件とも、旧 Oracle は「export されたプロパティの宣言」（Property Symbol）を、Ark は「そのプロパティの値の
  宣言」（Semantic Value Target）を答えている。Ark の実際の誤りは 0 件。
- 未確認（Phase B 以降）: (1) Ark がプロパティの値の宣言を **Exact** と答えることの妥当性（CommonJS の export
  オブジェクトは可変で、ESM の名前空間のように固定されない）。(2) 値の束縛が静的に決まらない形（再代入、
  ローカルの遮蔽、オブジェクトのメンバー、getter）で Ark が何を答えるか。(3) 旧 Oracle の分類の規則が、
  shorthand の site そのものでは値の束縛（`getShorthandAssignmentValueSymbol`）を、alias の先ではプロパティを
  答えるという非対称の扱い。
