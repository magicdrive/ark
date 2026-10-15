# 検証

検証した版: ブランチ `fix/commonjs-confidence-boundary` の作業ツリー（2026-10-15）。基準は 8f8f249 である。

| ファイル | sha256（先頭） |
|---|---|
| `internal/languages/typescript/commonjs.go`（8f8f249 の版に、`Mutable: true` の 1 行を加えたもの） | `9924890d16a9e89d` |
| `internal/resolver/modules.go` | `b123ea413d5911a5` |
| `internal/resolver/receivers.go` | `d296d3df1bf9f06b` |
| `internal/resolver/inheritance.go`（ラウンド 1 の Medium-1 の修正） | `5a6d8dcfb59ed9cd` |
| `internal/language/provider.go` | `596f680927ad9c4e` |
| `internal/index/fidelity_baseline_test.go`（採点のハーネス。無変更） | `e05f21921921ba4d` |

## V1. テストと静的検査

| 検査 | 結果 |
|---|---|
| `go vet ./...` | OK |
| `go test -count=1 ./...` | 全パッケージ OK |
| `go test -count=1 -race ./...` | 全パッケージ OK |
| staticcheck（作業領域 `bin-pin/staticcheck`） | 9 件。すべて変更していないパッケージ（`internal/core`・`internal/model`・`internal/skill`・`internal/spinner`）の既存の指摘 |
| `gofmt -l internal/` | 変更したファイルは差分なし（`internal/golden/testdata/go/broken.go` は、意図して壊した既存の testdata） |

## V2. 境界のテスト

- `internal/resolver/mutable_export_test.go`（言語に依存しない契約）
  - 名前の束縛: CommonJS は Candidate、ESM の対照は Exact。
  - 再 export の連鎖（`export { x } from`、`export *`）を通っても、上限が伝わる。
  - 名前空間の受け手: 同じモジュールの Mutable でない export は、Exact のまま。
  - 受け手の束縛のメンバー。
- `internal/languages/typescript/commonjs_boundary_test.go`（実際のソース）
  - 字句の束縛の陽性対照（`const fn = original`、関数の中の `value`、CommonJS のモジュールの中の `original`）は Exact。
  - CommonJS の export の値は Candidate で、正しい宣言を候補として保つ。
  - `module.exports.parse = replacement`・`Object.assign(module.exports, …)`・`module.exports[key] = …` の後でも、
    Exact / Strong にならない。
  - ESM と TS の export は Exact。
  - ESM から CommonJS への import と再 export、TS で書いた CommonJS は Candidate。
- 既存の CommonJS のテスト（F6B-5 と、前のミッションの反例 400 件余り）は、期待を「Candidate として正解を保つ」
  （`cand`）または「Exact / Strong の対象が無い」に改めて、すべて通る。`scope_fidelity_test.go` の `resolution` に候補の一覧を、
  `expect` に `cand` を加えた。
- 期待を「落とす」に変えたのは `mainfilename` の 1 件だけである。8f8f249 の走査は `require.main` の読み取りで export を落とす。
  監査の版（526e8de）では残していた形で、8f8f249 との比較では変化はない。

## V3. ミューテーション（`gate/mut-cb.py`、最終のコード）

| 変異 | 結果 |
|---|---|
| M1 CommonJS の export に `Mutable` を付けない | 検出（5 テスト） |
| M2 走査で `Mutable` を拾わない | 検出（9） |
| M3 `merge` で `mutable` を伝えない | 検出（14） |
| M4 名前の束縛の上限を外す | 検出（6） |
| M5 受け手の束縛の上限を外す | 検出（10） |
| M6 名前空間の受け手の上限を外す | 検出（1） |
| M7 すべての export を Mutable として扱う（ESM の降格） | 検出（42。ESM / TS の Exact を守るテスト） |
| M8 継承の関係の上限を外す | 検出（1） |
| M9 CommonJS で import した上位型を特定しない（ラウンド 1 の Medium-1 の再発） | 検出（1） |
| M10 名前空間の fallback に印を付けない（ラウンド 1 の Low-1 の再発） | 検出（1） |
| M11 継承の上限を Exact の関係にも当てる（過剰な降格） | 検出（7。ESM と同じファイルの継承の Exact を守るテスト） |

## V4. 固定コーパス

`fidelity.md` に記録した。対象は、12 コーパス（Oracle v1 / v2）とトランスパイル済みの 8 コーパスである。
- 誤った Exact / Strong と False Outside は、0 のままである。
- 候補を含む発見率は、20 コーパスのすべてで変わらない。
- `unsafe_strong` は、290 件から 0 件になった。

## V5. 言語横断（`gate/xl/run.sh`。参照の一覧 `report.sites` を 8f8f249 と比べる）

| コーパス | 言語 | 結果 |
|---|---|---|
| Ark 自身の `internal/` | Go | 同一 |
| Slim 4.14.0 | PHP | 同一 |
| `mlf/py/corpus` | Python | 同一 |
| tokio 0.2.13、syn 1.0.16 | Rust | 同一 |
| `tf/infra` | Terraform | 同一 |
| rxjs 7.8.1 `src`、immer 10.1.1 `src` | TypeScript | 同一 |
| lodash-es 4.17.21 | JavaScript（ESM） | 同一 |
| axios → Babel | JavaScript（Babel の CommonJS） | 同一 |
| `hard/perfcorp/html` | HTML | 同一 |
| `wsf/csscorpus` | CSS | 同一 |
| rxjs `dist/cjs` | JavaScript（tsc の CommonJS） | 異なる（想定どおり。CommonJS の export を通る 270 件が Candidate になった） |

## V6. キャッシュと MCP（`gate/cache/`）

| 検査 | rxjs `dist/cjs` | axios → Babel |
|---|---|---|
| cold・warm・版の更新（ts-14 のキャッシュを ts-16 で読む）の応答が、`--no-cache` の応答と同一 | 同一（エントリーは 250 / 250 / 500） | 同一（61 / 61 / 122） |
| ts-14 のキャッシュを使わない（ts-16 のキーで抽出し直す） | OK | OK |
| MCP の応答のキーの集合（8f8f249 と比べる） | 消えたキーは無い。現れたキー（`candidates`・`candidatesTotal`・`candidateRelationsTotal`・`candidateCallees`・`candidateCalleeRelations`・`references`）は、どれも既存の応答の項目である（`internal/mcp/tools_callers.go`・`tools_relations.go`。値が無いときは省略される） | 同一 |
| MCP の応答の Confidence | exact 43 → exact 6 / candidate 14。CommonJS を通る辺が候補へ移った | 同一 |
| ツールの実行エラー | 0 | 0 |

`internal/mcp` のコードは変えていない。CommonJS の export を通る呼び出しは、`get_callers` などのグラフの辺から外れて、
同じ応答の `candidates` に入る（ARCHITECTURE.md §2: Candidate はグラフの辺にならない）。`unattributed` は、その分だけ増える。
