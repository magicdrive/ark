# Web Semantic Fidelity（開発台帳）

JavaScript / TypeScript のスコープとモジュール、HTML と CSS の provider、Web 横断のリソース関係の開発台帳です。
利用者向けの正式な説明は、次の文書にあります。この台帳は、それらの根拠と再現の手順を残すものです。

- `docs/language-support.md`
- `docs/resolution-model.md`
- `ARCHITECTURE.md`

## 現行の文書

| ファイル | 内容 |
|---|---|
| `decisions.md` | 設計の決定 D1〜D39。コードのコメントが参照する（HTML の D19・D21・D31、CSS の D24・D25 など） |
| `commonjs-boundary/` | CommonJS の Confidence の境界（D39）。契約、固定コーパスの Before / After、検証、独立レビュー、リリース判定 |
| `f6b5/` | CommonJS の export の意味論（どの宣言を指すか）と、評価契約 v2（Oracle の設計と採点） |
| `resource-architecture.md` | 資源（画像・フォント・メディア）のノードの設計（D34） |
| `css-parser-review.md` | CSS パーサーの選定の再評価 |
| `verification.md` | Phase 0〜9 の検証の記録。HTML / CSS パーサーの比較の結果を含む |

再現の道具は、リポジトリの `docs/scripts/` にあります。

| 道具 | 内容 |
|---|---|
| `oracle/` | 独立 Oracle（v1 `javascript-tsc.js`、v2 `javascript-tsc-value.js`）と、値の層の fixture |
| `html-parser-eval/`、`css-parser-eval/` | パーサーの比較 |
| `fidelity-baseline.sh` | 評価の実行 |

評価の定義は `docs/research/multilang-fidelity/evaluation.md` にあります。

## アーカイブ

次の資料は、役目を終えたので公開ツリーから外しました。内容は、コミット `508e402`（ローカルのブランチ
`archive/web-semantic-fidelity-ledger`）に残っています。

- 進捗の台帳: `progress.md`、`hardening-progress.md`、`f6b5/progress.md`
- Phase 0 の計画: `acceptance-criteria.md`、`baseline.md`、`architecture.md`
- 過去の段階の検証と分析: `hardening-verification.md`、`js-recall-analysis.md`、`f6b5/verification.md`
- 実装前の調査のロードマップと設計案: `docs/research/multilang-fidelity/roadmap.md`、`web-architecture.md`
  （段階ごとの状況は `docs/research/multilang-fidelity/README.md` に統合した）
- リリース判定の監査: `release-gate/`
  - 反射的な書き込みを構文の規則（ts-15）で封じる方式で、独立レビュー D7〜D13 で打ち切った。
  - 監査の層のコード（`commonjs_audit.go`）は、保全コミット `526e8de`（ブランチ `audit/web-semantic-release-gate`）にだけあり、
    リリースの版には含まれない。

## リリース後の課題（Backlog）

将来の検討事項で、リリースの判定（`commonjs-boundary/decision.md`、READY）を変えるものではない。

1. **CommonJS の Export Value Analysis**
   - export の値が封印されているか、書き込み・流出があるかを判定する層を、resolver の外に独立して置く。
   - Candidate の順位付けや、オプトインの閉世界の Strong に使う。
   - 前のミッションの監査の層（526e8de）は、その材料である。Exact の根拠には戻さない。
2. **ESM の default export の Recall**
   - `export default { … }` を通るメンバーアクセスを解決できるようにする。
3. **トランスパイル済み CommonJS の Recall**
   - getter による再 export（`Object.defineProperty(exports, "x", { get })`、`__exportStar`）を扱う。
   - ES5 のクラスの `this` の扱いを改める（`commonjs-boundary/fidelity.md`）。
4. **CommonJS / ESM の境界の Confidence の精緻化**
   - `require()` が ESM を指すときの名前空間を、実行環境（Node の require(esm)、バンドラ）のモデルとして決める（`commonjs-boundary/contract.md` §6）。
   - CommonJS の上位型を持つ型のメンバーの扱いも、あわせて見直す。
5. **Oxc Semantic Analysis の部分移植と Pure Go の実装の技術調査**
   - スコープ、シンボル、参照の解析の正確さと速さを比べる。
   - cgo を避ける Pure Go の制約のもとで、何を移植できるかを調べる。
