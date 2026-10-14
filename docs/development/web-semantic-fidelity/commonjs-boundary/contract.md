# CommonJS の Confidence の境界: 意味論の契約

前のミッション（リリース判定の監査。台帳はアーカイブ、`../README.md`）では、CommonJS の export オブジェクトへの
反射的な書き込みを、構文の規則で封じようとした。しかし、独立レビュー D7〜D12 は 6 回とも RED だった。
そこで本ミッションでは、反射的な経路を検出する方式をやめる。代わりに、**CommonJS の export 値の同一性そのものを、
Exact / Strong の保証の対象から外す**。

## 1. 区別する 3 つの解決

| 解決 | 例 | 何を根拠にするか | 実行時の変化 | Confidence |
|---|---|---|---|---|
| 字句の束縛（Lexical Binding） | `function parse() {}; const fn = parse` | 言語のスコープの規則 | 束縛先は変わらない | **Exact**（従来どおり） |
| 静的な export の対応（Static Export Mapping） | `module.exports = { parse }` → `module.exports.parse ↦ parse` | export の文の構文 | 記録するのはソース上の対応関係で、値の不変性は主張しない | export の表として保持する（ナビゲーションと候補に使う） |
| export 値の解決（Export Value Resolution） | `const m = require('./m'); m.parse()` | export の対応関係と、それが実行時にも保たれるという仮定 | CommonJS の export オブジェクトは可変で、反射的な経路を静的に閉じられない | **CommonJS では Candidate が上限** |

ESM の export は、その名前の束縛そのものを指す（live binding）。モジュールの外から書き換えられないので、ESM と
TypeScript のソースレベルの export を通る解決は、**従来の規則のまま**とする（Exact）。

## 2. 方式: 由来による上限（検出による降格ではない）

- 動的な変更を検出して Confidence を下げる方式は、採らない。反射的な経路を網羅できないことは、前のミッションで確かめた。
- 代わりに、export の由来を記録する。CommonJS の export の文（`module.exports = …`、`exports.a = …`、`module.exports.a = …`）から
  作った export は、「実行時に値が変わりうる export（Mutable）」として provider が印を付ける。
- resolver は、import の束縛や受け手の束縛をたどる途中で、印のある export を一度でも通った解決を、**Candidate を上限**にする。
  動的な変更を検出できたかどうかには依らない。
- `require(x)` が、`module.exports` の export を述べないモジュールの名前空間を指すとき（`const u = require('./u'); u.a()`）も、
  CommonJS の値の解決として同じ上限をかける。
- CommonJS の export を通して import したクラスを継承するとき（`class S extends require('./c').C`）は、上位型の関係を特定する。
  ただし、その型の継承をたどって見つけたメンバー（`this.cm()`、`super.cm()`）は、Candidate を上限にする。別の上位型
  （ESM のインタフェースなど）の関係で見つけたメンバーも、同じ型をたどる限り Candidate になる。CommonJS の上位型の値が、
  同じ名前のメンバーを実行時に持ちうるためである（PHP の特定できない trait と同じ扱い。レビュー 2 の Low-B）。
- 上限は下げる方向にだけ働く（ARCHITECTURE.md §2「caps only lower it」）。

## 3. 影響を受けないもの

- 字句の参照: CommonJS のモジュールの中の参照も含む。`module.exports = { parse }` の `parse` の識別子は、`function parse` へ
  Exact のまま。
- ESM の import / export。TypeScript のソースレベルの export（`export function`、`export { x }`、`export default`）。
- CommonJS の束縛（`const m = require('./m')`）が指すモジュールの特定。モジュールの依存とファイルの関係は、従来どおり。
- HTML / CSS / JS の言語横断のリンク、Go / PHP / Python / Rust / Terraform。

## 4. 対象になるもの

- export の由来が CommonJS の export の文である、すべての export。TypeScript のファイルに書いた CommonJS の文と、
  トランスパイル済みの CommonJS（`exports.x = x`）を含む。
- ESM の `import { parse } from './cjs.js'` のように、ESM の側から CommonJS のモジュールを import する形も、同じ export を
  通るので Candidate になる。
- 既存の、書き込みを検出して export を落とす規則（8f8f249、ts-14 の走査）は残す。落とした export は、候補としても出さない
  （Unresolved）。落とさなかった export は、Candidate として提示する。

## 5. 公開の契約との関係

- Candidate は、グラフの辺にならない（ARCHITECTURE.md §2）。そのため、CommonJS の export を通る呼び出しは、
  `get_callers` などのグラフの辺から外れる。参照そのもの（`find_references`、`get_relations` の候補）としては残る。
- 応答の構造（キー）は変えない。変わるのは、該当する参照の Confidence の値だけである。
- 評価（Oracle v1 / v2）は変えない。Exact / Strong の正解の数が減るのは、想定どおりである。候補を含めた発見率は、
  別に数える（`fidelity.md`）。

## 6. 安全側の例外（既知。Exact を失うが、正解は Candidate として残る）

- `require()` が ESM のモジュールを指すとき、名前空間の受け手を通る参照（`const ns = require('./esm.js'); ns.em()`）は Candidate になる。
  `require()` が ESM のモジュールに返す値は、実行環境やトランスパイラに依存する（Node の require(esm) の名前空間、
  トランスパイルされた CommonJS の exports オブジェクト）。そのため、CommonJS の値の解決として扱う。一方、分割代入
  （`const { em } = require('./esm.js')`）は ESM の export を直接たどるので、Exact のまま残る。この非対称は既知である
  （レビュー 2 の Low-A）。
- CommonJS の上位型を持つ型のメンバーは、ESM のインタフェースから見つけたものも Candidate になる（§2。レビュー 2 の Low-B）。
