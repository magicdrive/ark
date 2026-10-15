# 独立レビュー

範囲は、ミッションの指定した 5 点に限った。反射的な書き込みの経路を新たに探すことは、目的にしない。
1. CommonJS の export 値が、Exact / Strong に漏れていないか。
2. 字句の束縛を、誤って降格していないか。
3. ESM / TypeScript を壊していないか。
4. Candidate / Unresolved が、公開の契約に合っているか。
5. Cache / MCP に回帰がないか。

上限は 2 ラウンドとした。

## ラウンド 1: GREEN（Medium 1 件・Low 4 件 → すべて対応）

レビュアーは、変更後の写しと 8f8f249 相当の写しで、約 219 の参照の解決を比べた。

| 観点 | 結果 |
|---|---|
| 1 | 新しい漏れなし（約 70 ケース）。`bindResult` を作る経路は、`bindingTargets` と `export` に限られる。受け手の型（`resolveTypeName` → `resolveViaBinding`）、名前の一意性による解決（`capModuleScope`・`capUntypedReceiver`）、`import x = require`、`.cjs` / `.mjs`、re-export の連鎖と循環も、Candidate になった |
| 2 | 誤った降格なし（約 25 ケース） |
| 3 | 誤った降格なし（約 30 ケース。8f8f249 との差分 0） |
| 4 | ずれが 1 件（Medium-1） |
| 5 | 回帰なし。`Mutable` は cache の JSON で往復する。ts-16 は、JS の版（`js-12+ts-16`）と HTML の版、index の Fingerprint、MCP の index cache に伝わる。MCP の 5 つのツールでは、CommonJS を通る呼び出しが `edges` から `candidates` に移るだけで、キーは既存の項目に限られる |

| ID | 指摘 | 対応 |
|---|---|---|
| Medium-1 | CommonJS で import したクラスを継承したサブクラスの `this.cm()` が Unresolved になる（8f8f249 では Exact）。上位型の特定が「Exact で対象が 1 つ」に限られていたので、関係が特定されず、探索が止まった。正解を候補ごと失う | `typeRelation.capped` を加えた。CommonJS の export を通って特定した上位型は、関係として残す。その関係を通して見つけたメンバーは、`ambiguous` と同じく Candidate を上限にする。回帰テスト: `commonjs_boundary_test.go` の `app/s.js:3:cm` |
| Low-1（変更前から） | `module.exports` を置き換え、ESM の export も持つモジュールを `require` すると、名前空間の fallback を通って Exact になる | 名前空間の fallback も、CommonJS の値の解決として `mutable` にした。回帰テスト: `mixtu.ts:2:te` |
| Low-2 | `newfunction` の期待と、`kept` の注記が古い（監査の層の前提のままだった） | 期待を「Candidate として残る」に改め、注記を直した |
| Low-3 | `mutableDetail` の定数が、`resolveViaReceiverBinding` の doc comment と関数のあいだに入っていた | 定数を doc comment の前に移した |
| Low-4 | golden が `Mutable` を固定していない | golden の export の行に、`Mutable` のときだけ ` mutable` を出すようにした（既存の snapshot は変わらない）。ただし golden の fixture には CommonJS が無いので、golden 自体はこの行を固定していない（レビュー 2 の Low-C で訂正）。provider の付け漏れは、ミューテーション M1 がテストで検出する。`CurrentSchemaVersion` は上げない。追加したのは omitempty の項目だけで、CommonJS の export を含む TS / JS のエントリーは、ts-16 のキーで作り直されるためである |

## ラウンド 2: GREEN（High・Medium なし。Low 3 件 → 文書で対応）

レビュアーは、ラウンド 1 の 219 参照に加えて 132 参照を、8f8f249 相当の写しと比べた。

| 観点 | 結果 |
|---|---|
| 1 | 漏れなし。CommonJS を通る 34 件が、8f8f249 の Exact から Candidate に下がった。Exact / Strong のまま残ったものは 0 件。確かめた形は、継承の関係（`extends` / `implements` / `interface extends`、re-export、ESM のクラスの上位型が CommonJS のもの）、名前空間の fallback である |
| 2 | 誤った降格なし。8f8f249 と同じ Exact が 14 件で、差は 0。CommonJS のファイルの中の継承も Exact のまま |
| 3 | 破壊なし（40 参照）。同じファイル、ESM、TS の継承は、すべて Exact のまま。降格しすぎが 2 種（Low-A・Low-B） |
| 4 | 契約に合っている。文言とのずれが 1 件（Low-B） |
| 5 | 回帰なし。MCP の出力はラウンド 1 と同一。新しく現れるキーは、既存の omitempty の項目だけ |

| ID | 指摘 | 対応 |
|---|---|---|
| Low-A | `require()` が純粋な ESM を指すとき、名前空間のメンバーが Candidate になる（分割代入は Exact のまま） | 安全側の例外として、契約の §6 に明記した。コードは変えない |
| Low-B | CommonJS の上位型を持つ型では、ESM のインタフェースから見つけたメンバーも Candidate になる | 実行時に CommonJS の上位型の値が同じ名前を持ちうるので、妥当な降格とした。契約の §2 の文言を実装に合わせ、§6 に記録した |
| Low-C | golden の ` mutable` を固定するテストが無い | ラウンド 1 の Low-4 の記述を訂正した。付け漏れの検出はミューテーション M1 が担う |

ミッションの上限（2 ラウンド）に達した。未解決の High・Medium は無い。
