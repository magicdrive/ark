# CommonJS の Confidence の境界

ブランチ `fix/commonjs-confidence-boundary`（526e8de から分岐。526e8de は前のミッションの監査を保全したコミット）。

前のミッション（リリース判定の監査。台帳はアーカイブ、`../README.md`）では、CommonJS の export オブジェクトへの反射的な書き込みを、構文の規則で封じようとした。
しかし、独立レビューは 6 回とも RED だった。本ミッションでは、その方式をやめた。代わりに、CommonJS の export 値の同一性を、
Exact / Strong の保証の対象から外した。

| ファイル | 内容 |
|---|---|
| `contract.md` | 意味論の契約。字句の束縛、静的な export の対応、export 値の解決の 3 つを区別する。安全側の例外も記す |
| `fidelity.md` | 固定コーパスの Before / After（12 コーパスとトランスパイル済みの 8 コーパス、Oracle v1 / v2） |
| `verification.md` | テスト・race・vet・staticcheck、境界のテスト、ミューテーション、言語横断、キャッシュと MCP |
| `review.md` | 独立レビューの 2 ラウンドと、その対応 |
| `decision.md` | リリース判定（READY）、保証の範囲、リリース前の必須作業、将来の設計 |

## 実装（8f8f249 との差分の要点）

- `internal/language/provider.go`: `ExportDraft.Mutable` を加えた。
- `internal/languages/typescript/commonjs.go`: CommonJS の export の文から作る export に、`Mutable: true` を付ける。抽出の規則は 8f8f249 の版のままにした。前のミッションの監査の層（`commonjs_audit.go`、ts-15）は外した。
- `internal/resolver/modules.go`: `bindResult.mutable` を加えた。export の走査と、`require` の名前空間の fallback で立つ。
- `internal/resolver/receivers.go`: 名前の束縛、受け手の束縛、名前空間の受け手の解決で、`mutable` があれば Candidate を上限にする。
- `internal/resolver/inheritance.go`: CommonJS で import した上位型の関係を特定する（`capped`）。その関係をたどって見つけたメンバーは、Candidate を上限にする。
- CacheVersion は ts-16。ts-15 は、未リリースの監査の版なので飛ばした。
- 文書: `docs/language-support.md`、`ARCHITECTURE.md` §2。

## 再現（作業領域。リポジトリの外）

- `gate/cb-verify.sh`: 12 コーパス（`f6b5/score-cb.sh`）、トランスパイル済み（`gate/tc/score.sh`）、言語横断（`gate/xl/run.sh`）、キャッシュと MCP（`gate/cache/run.sh`）を順に実行する。
- `gate/cb-table.py`: Before / After の表を作る。
- `gate/mut-cb.py`: ミューテーション。
