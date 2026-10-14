# F6B-5: CommonJS shorthand export の参照の意味論と Oracle の照合

Hardening の Phase F の Reviewer B が残した中の指摘 F6B-5（`decisions.md` D37）を解決するための台帳。
ブランチ `fix/commonjs-shorthand-oracle`（`feat/web-semantic-fidelity-hardening` の `d401ad1` から分岐、
main は `bab9b1e` のまま）。

| ファイル | 内容 |
|---|---|
| `cases.md` | semver の 16 件の個別の再現と分類（Phase A） |
| `semantics.md` | 参照の意味論の定義と 16 の仕様ケース（Phase B） |
| `oracle-design.md` | 独立の Semantic Value Oracle の設計（Phase C） |
| `scoring.md` | 評価契約の版と、旧・新の採点（Phase E） |

成果物:

| 場所 | 内容 |
|---|---|
| `docs/scripts/oracle/javascript-tsc-value.js` | 評価契約 v2 の Oracle（v1 の `javascript-tsc.js` は不変） |
| `docs/scripts/oracle/check-value-fixtures.sh`、`value-fixtures/` | v2 の値の層の fixture と検査 |
| `docs/scripts/oracle/shorthand-inspect.js` | TS の Symbol と値の束縛の検査ツール（Phase A） |
| `internal/languages/typescript/commonjs.go` | Ark の修正（ts-14） |
| `internal/languages/typescript/commonjs_value_test.go` | 意味論の回帰テスト |
| `internal/index/fidelity_baseline_test.go`、`fidelity_contract_test.go` | 評価ハーネスの v2 対応とその単体テスト |

進捗の台帳（`progress.md`）と検証の記録（`verification.md`）はアーカイブした（`../README.md`）。CommonJS の export を
通る参照の Confidence は、その後の D39 で Candidate が上限になった（`../commonjs-boundary/`）。

## 再現の環境（固定）

- Node.js v22.17.0、TypeScript 5.6.3（作業領域 `mlf/js/oracle/deps/node_modules/typescript`、Phase 0 以来同じ）
- semver 7.6.3（npm tarball、sha256 `376d2ca2c941fc5a37e9ac3ec65302e5e421e2cc1ee3dee57a854d2bd9bee125`）
- 旧 Oracle（`docs/scripts/oracle/javascript-tsc.js`）の出力 `js_semver.jsonl`: sha256
  `b05596a25f7cd4cae3373bdd3180edf65d74bd6fcfd05ac5c6b7477bff9092c5`（作業領域 `f6b5/oracle-v1/`。Reviewer B の
  出力とバイト単位で同一 = 決定的）
- 採点: `internal/index/fidelity_baseline_test.go`（`TestFidelityBaseline`）、評価の定義は
  `docs/research/multilang-fidelity/evaluation.md` §4・§6
- Ark: `d401ad1`（Hardening の最終）
