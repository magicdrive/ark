# Phase E: 二重の採点（契約 v1 と v2、3 版、12 コーパス）

## E1. 条件

- **版**:
  - `8e83a02`: Hardening の前。
  - `d401ad1`: Hardening の後。
  - 現在: `506cb7c` に作業ツリーを加えたもの。Phase D の修正 `ts-14` を含む（CommonJS の export を、値を
    変えうる形がある名前では出さない）。
- **ハーネス**: 3 版とも、作業ツリーの `internal/index/fidelity_baseline_test.go`（v2 に対応した最終版）を、
  それぞれの版のソース（`git archive <rev>`）に載せて採点した。採点のスクリプトは、ハーネスがバイト単位で
  同一であることを確かめてから採点し、各版のハーネスと `commonjs.go` の sha256 を残す（ハーネスは 3 版で同一。
  `commonjs.go` は 8e83a02 と d401ad1 が同一で、現在の版だけが異なる）。
- **Oracle**:
  - v1（`javascript-tsc.js`）: 12 コーパスそれぞれで再生成し、保存済みの Oracle とバイト単位で一致することを
    確かめてから使った。
  - v2（`javascript-tsc-value.js` の最終版、`oracle-design.md` C6）: 2 回生成して一致することを確かめた。
  - 環境: TypeScript 5.6.3、Node v22.17.0。
- **v2 の値の層が付くのは semver と commander だけ**。semver は値 18、不明 50（うち 48 は `export_unsafe`、
  2 は `not_identifier`）。commander は不明 14（すべて `export_unsafe`。下の要点）。ほかの 10 コーパスには、CommonJS の
  export オブジェクトを通る site も、安全でない export を通る site も無い。そのため v2 の Oracle は v1 と
  バイト単位で同一になり、v2 の採点も v1 と同一になる。
  semver の 48 site は `internal/re.js` の `exports = module.exports = {}`（トップレベル以外の `module.exports` の
  代入にあたり、`replaced`）を通る `re`・`t` などの読み取りである。
- **コーパス**: lodash-es 4.17.21、express 4.21.2、preact（dist を除く）、three 0.170.0 `src`、jsfix、jsfix2、
  rxjs 7.8.1 `src`（TypeScript）、axios 1.7.9 `lib`、d3-array 3.2.4 `src`、commander 12.1.0 `lib`、
  semver 7.6.3、immer 10.1.1 `src`（TypeScript）。
- **再現**: 作業領域の `f6b5/gen-oracles.sh`（Oracle）と `f6b5/score-all.sh`（72 回の採点）を使う。
  集計は `f6b5/table.py`・`f6b5/series2.py`・`f6b5/make-scoring.py`。

指標:
- FS = exact_fp + strong_fp + local_fp + external_fp + ambiguous_fp（評価 §4）。FO = false_outside。
- Recall = recovered / representable。
- v2 では representable が増える。プロパティが Ark の Symbol でなくても、値の宣言が Ark の Symbol なら
  数えるからである。そのため **v1 と v2 の Recall の値を直接比べてはならない**。版の比較は、同じ契約の
  中で、分母を固定した Series 1（E3）で行う。

## E2. 全体の表（72 回）

| corpus | 版 | 契約 | representable | recovered | Recall | Exact TP | Strong TP | tp_value | FS | うち local_fp | FO | via | Cand hit/miss | Unresolved | not_extracted | unmatched_strong | unsafe_sites / unsafe_strong |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| lodash | 8e83a02 | v1 | 3591 | 1242 | 34.6% | 1235 | 7 | 0 | 0 | 0 | 0 | 0 | 3/0 | 0 | 2346 | 0 | 0 / 0 |
| lodash | 8e83a02 | v2 | 3591 | 1242 | 34.6% | 1235 | 7 | 0 | 0 | 0 | 0 | 0 | 3/0 | 0 | 2346 | 0 | 0 / 0 |
| lodash | d401ad1 | v1 | 3591 | 3543 | 98.7% | 3536 | 7 | 0 | 0 | 0 | 0 | 0 | 3/0 | 0 | 45 | 0 | 0 / 0 |
| lodash | d401ad1 | v2 | 3591 | 3543 | 98.7% | 3536 | 7 | 0 | 0 | 0 | 0 | 0 | 3/0 | 0 | 45 | 0 | 0 / 0 |
| lodash | now | v1 | 3591 | 3543 | 98.7% | 3536 | 7 | 0 | 0 | 0 | 0 | 0 | 3/0 | 0 | 45 | 0 | 0 / 0 |
| lodash | now | v2 | 3591 | 3543 | 98.7% | 3536 | 7 | 0 | 0 | 0 | 0 | 0 | 3/0 | 0 | 45 | 0 | 0 / 0 |
| express | 8e83a02 | v1 | 243 | 80 | 32.9% | 78 | 2 | 0 | 0 | 0 | 0 | 0 | 1/0 | 0 | 162 | 0 | 0 / 0 |
| express | 8e83a02 | v2 | 243 | 80 | 32.9% | 78 | 2 | 0 | 0 | 0 | 0 | 0 | 1/0 | 0 | 162 | 0 | 7 / 0 |
| express | d401ad1 | v1 | 243 | 203 | 83.5% | 201 | 2 | 0 | 0 | 0 | 0 | 0 | 1/0 | 0 | 39 | 0 | 0 / 0 |
| express | d401ad1 | v2 | 243 | 203 | 83.5% | 201 | 2 | 0 | 0 | 0 | 0 | 0 | 1/0 | 0 | 39 | 0 | 7 / 0 |
| express | now | v1 | 243 | 203 | 83.5% | 201 | 2 | 0 | 0 | 0 | 0 | 0 | 1/0 | 0 | 39 | 0 | 0 / 0 |
| express | now | v2 | 243 | 203 | 83.5% | 201 | 2 | 0 | 0 | 0 | 0 | 0 | 1/0 | 0 | 39 | 0 | 7 / 0 |
| preactsrc | 8e83a02 | v1 | 617 | 188 | 30.5% | 188 | 0 | 0 | 0 | 0 | 0 | 0 | 0/0 | 0 | 429 | 0 | 0 / 0 |
| preactsrc | 8e83a02 | v2 | 617 | 188 | 30.5% | 188 | 0 | 0 | 0 | 0 | 0 | 0 | 0/0 | 0 | 429 | 0 | 0 / 0 |
| preactsrc | d401ad1 | v1 | 617 | 551 | 89.3% | 551 | 0 | 0 | 0 | 0 | 0 | 0 | 0/0 | 0 | 66 | 33 | 0 / 0 |
| preactsrc | d401ad1 | v2 | 617 | 551 | 89.3% | 551 | 0 | 0 | 0 | 0 | 0 | 0 | 0/0 | 0 | 66 | 33 | 0 / 0 |
| preactsrc | now | v1 | 617 | 551 | 89.3% | 551 | 0 | 0 | 0 | 0 | 0 | 0 | 0/0 | 0 | 66 | 33 | 0 / 0 |
| preactsrc | now | v2 | 617 | 551 | 89.3% | 551 | 0 | 0 | 0 | 0 | 0 | 0 | 0/0 | 0 | 66 | 33 | 0 / 0 |
| threesrc | 8e83a02 | v1 | 14715 | 7399 | 50.3% | 7356 | 43 | 0 | 0 | 0 | 0 | 0 | 896/414 | 0 | 6006 | 0 | 0 / 0 |
| threesrc | 8e83a02 | v2 | 14715 | 7399 | 50.3% | 7356 | 43 | 0 | 0 | 0 | 0 | 0 | 896/414 | 0 | 6006 | 0 | 0 / 0 |
| threesrc | d401ad1 | v1 | 14715 | 12498 | 84.9% | 12455 | 43 | 0 | 0 | 0 | 0 | 0 | 896/414 | 0 | 907 | 347 | 0 / 0 |
| threesrc | d401ad1 | v2 | 14715 | 12498 | 84.9% | 12455 | 43 | 0 | 0 | 0 | 0 | 0 | 896/414 | 0 | 907 | 347 | 0 / 0 |
| threesrc | now | v1 | 14715 | 12498 | 84.9% | 12455 | 43 | 0 | 0 | 0 | 0 | 0 | 896/414 | 0 | 907 | 347 | 0 / 0 |
| threesrc | now | v2 | 14715 | 12498 | 84.9% | 12455 | 43 | 0 | 0 | 0 | 0 | 0 | 896/414 | 0 | 907 | 347 | 0 / 0 |
| jsfix | 8e83a02 | v1 | 38 | 22 | 57.9% | 20 | 2 | 0 | 0 | 0 | 0 | 0 | 0/1 | 0 | 15 | 0 | 0 / 0 |
| jsfix | 8e83a02 | v2 | 38 | 22 | 57.9% | 20 | 2 | 0 | 0 | 0 | 0 | 0 | 0/1 | 0 | 15 | 0 | 0 / 0 |
| jsfix | d401ad1 | v1 | 38 | 32 | 84.2% | 30 | 2 | 0 | 0 | 0 | 0 | 0 | 0/1 | 0 | 5 | 1 | 0 / 0 |
| jsfix | d401ad1 | v2 | 38 | 32 | 84.2% | 30 | 2 | 0 | 0 | 0 | 0 | 0 | 0/1 | 0 | 5 | 1 | 0 / 0 |
| jsfix | now | v1 | 38 | 32 | 84.2% | 30 | 2 | 0 | 0 | 0 | 0 | 0 | 0/1 | 0 | 5 | 1 | 0 / 0 |
| jsfix | now | v2 | 38 | 32 | 84.2% | 30 | 2 | 0 | 0 | 0 | 0 | 0 | 0/1 | 0 | 5 | 1 | 0 / 0 |
| jsfix2 | 8e83a02 | v1 | 1 | 0 | 0.0% | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 1/0 | 0 | 0 | 0 | 0 / 0 |
| jsfix2 | 8e83a02 | v2 | 1 | 0 | 0.0% | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 1/0 | 0 | 0 | 0 | 0 / 0 |
| jsfix2 | d401ad1 | v1 | 1 | 0 | 0.0% | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 1/0 | 0 | 0 | 0 | 0 / 0 |
| jsfix2 | d401ad1 | v2 | 1 | 0 | 0.0% | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 1/0 | 0 | 0 | 0 | 0 / 0 |
| jsfix2 | now | v1 | 1 | 0 | 0.0% | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 1/0 | 0 | 0 | 0 | 0 / 0 |
| jsfix2 | now | v2 | 1 | 0 | 0.0% | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 1/0 | 0 | 0 | 0 | 0 / 0 |
| rxjs | 8e83a02 | v1 | 3255 | 1722 | 52.9% | 1722 | 0 | 0 | 0 | 0 | 0 | 0 | 224/59 | 12 | 1238 | 0 | 0 / 0 |
| rxjs | 8e83a02 | v2 | 3255 | 1722 | 52.9% | 1722 | 0 | 0 | 0 | 0 | 0 | 0 | 224/59 | 12 | 1238 | 0 | 0 / 0 |
| rxjs | d401ad1 | v1 | 3255 | 1929 | 59.3% | 1931 | 0 | 0 | 0 | 0 | 0 | 0 | 224/59 | 12 | 1031 | 6 | 0 / 0 |
| rxjs | d401ad1 | v2 | 3255 | 1929 | 59.3% | 1931 | 0 | 0 | 0 | 0 | 0 | 0 | 224/59 | 12 | 1031 | 6 | 0 / 0 |
| rxjs | now | v1 | 3255 | 1929 | 59.3% | 1931 | 0 | 0 | 0 | 0 | 0 | 0 | 224/59 | 12 | 1031 | 6 | 0 / 0 |
| rxjs | now | v2 | 3255 | 1929 | 59.3% | 1931 | 0 | 0 | 0 | 0 | 0 | 0 | 224/59 | 12 | 1031 | 6 | 0 / 0 |
| axios | 8e83a02 | v1 | 543 | 212 | 39.0% | 212 | 0 | 0 | 0 | 0 | 0 | 0 | 23/0 | 0 | 308 | 0 | 0 / 0 |
| axios | 8e83a02 | v2 | 543 | 212 | 39.0% | 212 | 0 | 0 | 0 | 0 | 0 | 0 | 23/0 | 0 | 308 | 0 | 0 / 0 |
| axios | d401ad1 | v1 | 543 | 491 | 90.4% | 491 | 0 | 0 | 0 | 0 | 0 | 0 | 23/0 | 0 | 29 | 5 | 0 / 0 |
| axios | d401ad1 | v2 | 543 | 491 | 90.4% | 491 | 0 | 0 | 0 | 0 | 0 | 0 | 23/0 | 0 | 29 | 5 | 0 / 0 |
| axios | now | v1 | 543 | 491 | 90.4% | 491 | 0 | 0 | 0 | 0 | 0 | 0 | 23/0 | 0 | 29 | 5 | 0 / 0 |
| axios | now | v2 | 543 | 491 | 90.4% | 491 | 0 | 0 | 0 | 0 | 0 | 0 | 23/0 | 0 | 29 | 5 | 0 / 0 |
| d3array | 8e83a02 | v1 | 155 | 107 | 69.0% | 107 | 0 | 0 | 0 | 0 | 0 | 0 | 0/0 | 0 | 48 | 0 | 0 / 0 |
| d3array | 8e83a02 | v2 | 155 | 107 | 69.0% | 107 | 0 | 0 | 0 | 0 | 0 | 0 | 0/0 | 0 | 48 | 0 | 0 / 0 |
| d3array | d401ad1 | v1 | 155 | 155 | 100.0% | 155 | 0 | 0 | 0 | 0 | 0 | 0 | 0/0 | 0 | 0 | 0 | 0 / 0 |
| d3array | d401ad1 | v2 | 155 | 155 | 100.0% | 155 | 0 | 0 | 0 | 0 | 0 | 0 | 0/0 | 0 | 0 | 0 | 0 / 0 |
| d3array | now | v1 | 155 | 155 | 100.0% | 155 | 0 | 0 | 0 | 0 | 0 | 0 | 0/0 | 0 | 0 | 0 | 0 / 0 |
| d3array | now | v2 | 155 | 155 | 100.0% | 155 | 0 | 0 | 0 | 0 | 0 | 0 | 0/0 | 0 | 0 | 0 | 0 / 0 |
| commander | 8e83a02 | v1 | 308 | 159 | 51.6% | 159 | 0 | 0 | 0 | 0 | 0 | 0 | 96/14 | 0 | 39 | 0 | 0 / 0 |
| commander | 8e83a02 | v2 | 294 | 146 | 49.7% | 146 | 0 | 0 | 0 | 0 | 0 | 0 | 96/14 | 0 | 38 | 0 | 14 / 13 |
| commander | d401ad1 | v1 | 308 | 171 | 55.5% | 171 | 0 | 0 | 0 | 0 | 0 | 0 | 96/14 | 0 | 27 | 0 | 0 / 0 |
| commander | d401ad1 | v2 | 294 | 157 | 53.4% | 157 | 0 | 0 | 0 | 0 | 0 | 0 | 96/14 | 0 | 27 | 0 | 14 / 14 |
| commander | now | v1 | 308 | 171 | 55.5% | 171 | 0 | 0 | 0 | 0 | 0 | 0 | 96/14 | 0 | 27 | 0 | 0 / 0 |
| commander | now | v2 | 294 | 157 | 53.4% | 157 | 0 | 0 | 0 | 0 | 0 | 0 | 96/14 | 0 | 27 | 0 | 14 / 14 |
| semver | 8e83a02 | v1 | 703 | 230 | 32.7% | 230 | 0 | 0 | 5 | 5 | 0 | 0 | 4/5 | 0 | 464 | 0 | 0 / 0 |
| semver | 8e83a02 | v2 | 676 | 235 | 34.8% | 235 | 0 | 5 | 0 | 0 | 0 | 0 | 4/5 | 0 | 432 | 0 | 48 / 0 |
| semver | d401ad1 | v1 | 703 | 532 | 75.7% | 532 | 0 | 0 | 16 | 16 | 0 | 0 | 4/5 | 45 | 117 | 0 | 0 / 0 |
| semver | d401ad1 | v2 | 676 | 548 | 81.1% | 548 | 0 | 16 | 0 | 0 | 0 | 0 | 4/5 | 0 | 119 | 0 | 48 / 0 |
| semver | now | v1 | 703 | 532 | 75.7% | 532 | 0 | 0 | 16 | 16 | 0 | 0 | 4/5 | 45 | 117 | 0 | 0 / 0 |
| semver | now | v2 | 676 | 548 | 81.1% | 548 | 0 | 16 | 0 | 0 | 0 | 0 | 4/5 | 0 | 119 | 0 | 48 / 0 |
| immer | 8e83a02 | v1 | 648 | 387 | 59.7% | 387 | 0 | 0 | 0 | 0 | 0 | 0 | 1/0 | 1 | 259 | 0 | 0 / 0 |
| immer | 8e83a02 | v2 | 648 | 387 | 59.7% | 387 | 0 | 0 | 0 | 0 | 0 | 0 | 1/0 | 1 | 259 | 0 | 0 / 0 |
| immer | d401ad1 | v1 | 648 | 493 | 76.1% | 493 | 0 | 0 | 0 | 0 | 0 | 0 | 1/0 | 1 | 153 | 1 | 0 / 0 |
| immer | d401ad1 | v2 | 648 | 493 | 76.1% | 493 | 0 | 0 | 0 | 0 | 0 | 0 | 1/0 | 1 | 153 | 1 | 0 / 0 |
| immer | now | v1 | 648 | 493 | 76.1% | 493 | 0 | 0 | 0 | 0 | 0 | 0 | 1/0 | 1 | 153 | 1 | 0 / 0 |
| immer | now | v2 | 648 | 493 | 76.1% | 493 | 0 | 0 | 0 | 0 | 0 | 0 | 1/0 | 1 | 153 | 1 | 0 / 0 |

`tp_value` は、v2 で値の層に当たった TP で、Exact TP / Strong TP の内数である。`unsafe_sites / unsafe_strong` は、
安全でない export を通る site の数と、そこでの Ark の Strong / Exact の数である（TP にも FP にも数えない）。rxjs の Exact TP が recovered
より多いのは、1 つの site に 2 つの参照がある場合で、v1 からの既存の数え方による。

**要点**

- **v2 の FS は、3 版・12 コーパスのすべてで 0。** v1 の FS が 0 でないのは semver だけで、8e83a02 では 5、
  d401ad1 と現在では 16 である。その全件が v2 では `tp_value` になる（E5）。FO はすべて 0。
- `unsafe_strong` は commander の v2 だけが 0 でない（8e83a02 で 13、d401ad1 と現在で 14）。原因は
  `command.js:112` の `for (let command = this; …)` で、クラスのメソッドの `this` を流している。Oracle はこの規則を
  プログラム全体に当てるので、commander の 14 の export の site（`error.js` の `CommanderError`、`option.js` の
  `Option` など。どれも `command.js` 以外のモジュールの export）がすべて `export_unsafe` になる。Ark は同じ規則を
  モジュールごとに当てるので、それらの export は Exact のまま残る。`command.js` のメソッドが別のモジュールの
  export に載るには、モジュールをまたぐ書き込みが必要で、これは B4 の規則 5 の既知の制限と同じ種類である。
  14 件の Ark のターゲットは、どれも Oracle の `decl` と同じ宣言である（6 番目の版の Oracle での採点では、14 件
  とも `tp`。E6）。semver の 48 site には、
  Ark は Strong / Exact を出していない。
- semver の v2 の `value_tp` は 16 件で、うち 11 件はデータ（`MAX_LENGTH` などの定数。`value_tp_data`）、
  5 件は関数である。データの値は閉世界の仮定に依る（`oracle-design.md` C2）。
- **Phase D の修正（`ts-14`）は、12 コーパスのどれにも影響しない。** 現在の版と d401ad1 の `report.scored` と
  `report.sites` は、24 通り（12 コーパス × 2 契約）すべてでバイト単位で同一だった。修正が対象とする形
  （後ろの spread・計算キー・同じキー、`Object.assign(module.exports, …)`、2 回目の置き換え、`exports` の
  流出など）は、どのコーパスにも無い。効果は回帰テストで固定した（`verification.md`）。
- Reviewer C の C-4 のとおり、12 コーパスはすべてソースで、トランスパイルされた CommonJS（tsc や Babel の
  `lib/`・`dist/`）を含まない。そのような出力での修正の Recall への影響（`exports` を関数に渡す
  `__exportStar` などで、名前つきの export を落とす）は、このコーパスでは測っていない（`semantics.md`
  B3 の追加 2 の「既知の近似」）。

## E3. Series 1（分母の固定）

### 契約 v1: 8e83a02 → d401ad1

| corpus | S1（両方で representable） | 前の TP | 後の TP | 前 % | 後 % | 失った | 得た | S2（後だけ representable） | S2 の TP | FS 前 → 後 |
|---|---|---|---|---|---|---|---|---|---|---|
| lodash | 3591 | 1242 | 3543 | 34.6% | 98.7% | 0 | 2301 | 0 | 0 | 0 → 0 |
| express | 243 | 80 | 203 | 32.9% | 83.5% | 0 | 123 | 0 | 0 | 0 → 0 |
| preactsrc | 617 | 188 | 551 | 30.5% | 89.3% | 0 | 363 | 0 | 0 | 0 → 0 |
| threesrc | 14715 | 7399 | 12498 | 50.3% | 84.9% | 0 | 5099 | 0 | 0 | 0 → 0 |
| jsfix | 38 | 22 | 32 | 57.9% | 84.2% | 0 | 10 | 0 | 0 | 0 → 0 |
| jsfix2 | 1 | 0 | 0 | 0.0% | 0.0% | 0 | 0 | 0 | 0 | 0 → 0 |
| rxjs | 3255 | 1722 | 1929 | 52.9% | 59.3% | 0 | 207 | 0 | 0 | 0 → 0 |
| axios | 543 | 212 | 491 | 39.0% | 90.4% | 0 | 279 | 0 | 0 | 0 → 0 |
| d3array | 155 | 107 | 155 | 69.0% | 100.0% | 0 | 48 | 0 | 0 | 0 → 0 |
| commander | 308 | 159 | 171 | 51.6% | 55.5% | 0 | 12 | 0 | 0 | 0 → 0 |
| semver | 703 | 230 | 532 | 32.7% | 75.7% | 0 | 302 | 0 | 0 | 5 → 16 |
| immer | 648 | 387 | 493 | 59.7% | 76.1% | 0 | 106 | 0 | 0 | 0 → 0 |

### 契約 v1: d401ad1 → now

| corpus | S1（両方で representable） | 前の TP | 後の TP | 前 % | 後 % | 失った | 得た | S2（後だけ representable） | S2 の TP | FS 前 → 後 |
|---|---|---|---|---|---|---|---|---|---|---|
| lodash | 3591 | 3543 | 3543 | 98.7% | 98.7% | 0 | 0 | 0 | 0 | 0 → 0 |
| express | 243 | 203 | 203 | 83.5% | 83.5% | 0 | 0 | 0 | 0 | 0 → 0 |
| preactsrc | 617 | 551 | 551 | 89.3% | 89.3% | 0 | 0 | 0 | 0 | 0 → 0 |
| threesrc | 14715 | 12498 | 12498 | 84.9% | 84.9% | 0 | 0 | 0 | 0 | 0 → 0 |
| jsfix | 38 | 32 | 32 | 84.2% | 84.2% | 0 | 0 | 0 | 0 | 0 → 0 |
| jsfix2 | 1 | 0 | 0 | 0.0% | 0.0% | 0 | 0 | 0 | 0 | 0 → 0 |
| rxjs | 3255 | 1929 | 1929 | 59.3% | 59.3% | 0 | 0 | 0 | 0 | 0 → 0 |
| axios | 543 | 491 | 491 | 90.4% | 90.4% | 0 | 0 | 0 | 0 | 0 → 0 |
| d3array | 155 | 155 | 155 | 100.0% | 100.0% | 0 | 0 | 0 | 0 | 0 → 0 |
| commander | 308 | 171 | 171 | 55.5% | 55.5% | 0 | 0 | 0 | 0 | 0 → 0 |
| semver | 703 | 532 | 532 | 75.7% | 75.7% | 0 | 0 | 0 | 0 | 16 → 16 |
| immer | 648 | 493 | 493 | 76.1% | 76.1% | 0 | 0 | 0 | 0 | 0 → 0 |

### 契約 v2: 8e83a02 → d401ad1

| corpus | S1（両方で representable） | 前の TP | 後の TP | 前 % | 後 % | 失った | 得た | S2（後だけ representable） | S2 の TP | FS 前 → 後 |
|---|---|---|---|---|---|---|---|---|---|---|
| lodash | 3591 | 1242 | 3543 | 34.6% | 98.7% | 0 | 2301 | 0 | 0 | 0 → 0 |
| express | 243 | 80 | 203 | 32.9% | 83.5% | 0 | 123 | 0 | 0 | 0 → 0 |
| preactsrc | 617 | 188 | 551 | 30.5% | 89.3% | 0 | 363 | 0 | 0 | 0 → 0 |
| threesrc | 14715 | 7399 | 12498 | 50.3% | 84.9% | 0 | 5099 | 0 | 0 | 0 → 0 |
| jsfix | 38 | 22 | 32 | 57.9% | 84.2% | 0 | 10 | 0 | 0 | 0 → 0 |
| jsfix2 | 1 | 0 | 0 | 0.0% | 0.0% | 0 | 0 | 0 | 0 | 0 → 0 |
| rxjs | 3255 | 1722 | 1929 | 52.9% | 59.3% | 0 | 207 | 0 | 0 | 0 → 0 |
| axios | 543 | 212 | 491 | 39.0% | 90.4% | 0 | 279 | 0 | 0 | 0 → 0 |
| d3array | 155 | 107 | 155 | 69.0% | 100.0% | 0 | 48 | 0 | 0 | 0 → 0 |
| commander | 294 | 146 | 157 | 49.7% | 53.4% | 0 | 11 | 0 | 0 | 0 → 0 |
| semver | 676 | 235 | 548 | 34.8% | 81.1% | 0 | 313 | 0 | 0 | 0 → 0 |
| immer | 648 | 387 | 493 | 59.7% | 76.1% | 0 | 106 | 0 | 0 | 0 → 0 |

### 契約 v2: d401ad1 → now

| corpus | S1（両方で representable） | 前の TP | 後の TP | 前 % | 後 % | 失った | 得た | S2（後だけ representable） | S2 の TP | FS 前 → 後 |
|---|---|---|---|---|---|---|---|---|---|---|
| lodash | 3591 | 3543 | 3543 | 98.7% | 98.7% | 0 | 0 | 0 | 0 | 0 → 0 |
| express | 243 | 203 | 203 | 83.5% | 83.5% | 0 | 0 | 0 | 0 | 0 → 0 |
| preactsrc | 617 | 551 | 551 | 89.3% | 89.3% | 0 | 0 | 0 | 0 | 0 → 0 |
| threesrc | 14715 | 12498 | 12498 | 84.9% | 84.9% | 0 | 0 | 0 | 0 | 0 → 0 |
| jsfix | 38 | 32 | 32 | 84.2% | 84.2% | 0 | 0 | 0 | 0 | 0 → 0 |
| jsfix2 | 1 | 0 | 0 | 0.0% | 0.0% | 0 | 0 | 0 | 0 | 0 → 0 |
| rxjs | 3255 | 1929 | 1929 | 59.3% | 59.3% | 0 | 0 | 0 | 0 | 0 → 0 |
| axios | 543 | 491 | 491 | 90.4% | 90.4% | 0 | 0 | 0 | 0 | 0 → 0 |
| d3array | 155 | 155 | 155 | 100.0% | 100.0% | 0 | 0 | 0 | 0 | 0 → 0 |
| commander | 294 | 157 | 157 | 53.4% | 53.4% | 0 | 0 | 0 | 0 | 0 → 0 |
| semver | 676 | 548 | 548 | 81.1% | 81.1% | 0 | 0 | 0 | 0 | 0 → 0 |
| immer | 648 | 493 | 493 | 76.1% | 76.1% | 0 | 0 | 0 | 0 | 0 → 0 |

- どの比較でも、「失った」正解は 0 件。
- v1 の `8e83a02 → d401ad1` の数値は、Hardening の記録（`js-recall-analysis.md`、アーカイブ: semver 32.7% → 75.7%、
  FS 5 → 16、axios 90.4%、lodash 98.7% など）を再現する。

## E4. v1 の採点が変わらないこと

- v2 に対応したハーネスで v1 の Oracle を採点し、変更前のハーネスと同一の結果になることを確かめた。
  - 仕様 fixture: `cmp` で確認。
  - Reviewer C: 506cb7c の旧ハーネスで semver と commander を採点し、同一であることを確認。
  - 12 コーパス: v1 の数値が Hardening の記録と一致した（E3）。
- 新しい集計の欄は `omitempty` なので、v1 の `report.json` には現れない。
- v2 の Oracle から値の層の欄を除くと、12 コーパスすべてで v1 と同一になる。
- v1 の Oracle を再生成すると、12 コーパスすべてで保存済みのものとバイト単位で一致する。

## E5. semver の 16 件

| # | site | 名前 | v1 Oracle（decl: プロパティ） | v2 Oracle（value、value_kind、規則、value_via） | Ark のターゲット（確信度、規則） | v1 判定 8e83a02 / d401ad1・現在 | v2 判定 8e83a02 / d401ad1 / 現在 |
|---|---|---|---|---|---|---|---|
| 1 | bin/semver.js:103:29 | coerce | index.js:66:3 | functions/coerce.js:5:7（function、shorthand、index.js:28） | functions/coerce.js:coerce（exact、module_binding） | local_fp / local_fp | tp_value / tp_value / tp_value |
| 2 | bin/semver.js:105:19 | valid | index.js:45:3 | functions/valid.js:2:7（function、shorthand、index.js:7） | functions/valid.js:valid（exact、module_binding） | local_fp / local_fp | tp_value / tp_value / tp_value |
| 3 | bin/semver.js:116:21 | satisfies | index.js:69:3 | functions/satisfies.js:2:7（function、shorthand、index.js:31） | functions/satisfies.js:satisfies（exact、module_binding） | local_fp / local_fp | tp_value / tp_value / tp_value |
| 4 | bin/semver.js:124:22 | clean | index.js:46:3 | functions/clean.js:2:7（function、shorthand、index.js:8） | functions/clean.js:clean（exact、module_binding） | local_fp / local_fp | tp_value / tp_value / tp_value |
| 5 | bin/semver.js:125:28 | inc | index.js:47:3 | functions/inc.js:3:7（function、shorthand、index.js:9） | functions/inc.js:inc（exact、module_binding） | local_fp / local_fp | tp_value / tp_value / tp_value |
| 6 | classes/semver.js:22:26 | MAX_LENGTH | internal/constants.js:27:3 | internal/constants.js:5:7（data、shorthand、-） | internal/constants.js:MAX_LENGTH（exact、module_binding） | 参照なし / local_fp | not_extracted / tp_value / tp_value |
| 7 | classes/semver.js:24:35 | MAX_LENGTH | internal/constants.js:27:3 | internal/constants.js:5:7（data、shorthand、-） | internal/constants.js:MAX_LENGTH（exact、module_binding） | 参照なし / local_fp | not_extracted / tp_value / tp_value |
| 8 | classes/semver.js:48:22 | MAX_SAFE_INTEGER | internal/constants.js:30:3 | internal/constants.js:6:7（data、shorthand、-） | internal/constants.js:MAX_SAFE_INTEGER（exact、module_binding） | 参照なし / local_fp | not_extracted / tp_value / tp_value |
| 9 | classes/semver.js:52:22 | MAX_SAFE_INTEGER | internal/constants.js:30:3 | internal/constants.js:6:7（data、shorthand、-） | internal/constants.js:MAX_SAFE_INTEGER（exact、module_binding） | 参照なし / local_fp | not_extracted / tp_value / tp_value |
| 10 | classes/semver.js:56:22 | MAX_SAFE_INTEGER | internal/constants.js:30:3 | internal/constants.js:6:7（data、shorthand、-） | internal/constants.js:MAX_SAFE_INTEGER（exact、module_binding） | 参照なし / local_fp | not_extracted / tp_value / tp_value |
| 11 | classes/semver.js:67:33 | MAX_SAFE_INTEGER | internal/constants.js:30:3 | internal/constants.js:6:7（data、shorthand、-） | internal/constants.js:MAX_SAFE_INTEGER（exact、module_binding） | 参照なし / local_fp | not_extracted / tp_value / tp_value |
| 12 | internal/re.js:158:27 | MAX_SAFE_COMPONENT_LENGTH | internal/constants.js:28:3 | internal/constants.js:10:7（data、shorthand、-） | internal/constants.js:MAX_SAFE_COMPONENT_LENGTH（exact、module_binding） | 参照なし / local_fp | not_extracted / tp_value / tp_value |
| 13 | internal/re.js:159:31 | MAX_SAFE_COMPONENT_LENGTH | internal/constants.js:28:3 | internal/constants.js:10:7（data、shorthand、-） | internal/constants.js:MAX_SAFE_COMPONENT_LENGTH（exact、module_binding） | 参照なし / local_fp | not_extracted / tp_value / tp_value |
| 14 | internal/re.js:160:31 | MAX_SAFE_COMPONENT_LENGTH | internal/constants.js:28:3 | internal/constants.js:10:7（data、shorthand、-） | internal/constants.js:MAX_SAFE_COMPONENT_LENGTH（exact、module_binding） | 参照なし / local_fp | not_extracted / tp_value / tp_value |
| 15 | internal/re.js:26:11 | MAX_LENGTH | internal/constants.js:27:3 | internal/constants.js:5:7（data、shorthand、-） | internal/constants.js:MAX_LENGTH（exact、module_binding） | 参照なし / local_fp | not_extracted / tp_value / tp_value |
| 16 | internal/re.js:27:22 | MAX_SAFE_BUILD_LENGTH | internal/constants.js:29:3 | internal/constants.js:14:7（data、shorthand、-） | internal/constants.js:MAX_SAFE_BUILD_LENGTH（exact、module_binding） | 参照なし / local_fp | not_extracted / tp_value / tp_value |

- 8e83a02 の時点で、1〜5（`semver.coerce(...)` などの呼び出し）はすでに同じ規則（`module_binding`）で解決
  されていた。6〜16（`const { MAX_LENGTH } = require(...)` の読み取り）は、Hardening の D35（JavaScript の
  値の読み取り）で新しく出るようになった参照である。8e83a02 では参照が無く、v2 では not_extracted になる。
- v2 の値（`value`）は 16 件とも、Phase A で `shorthand-inspect.js` を使って TS から独立に導いた値の宣言と
  一致する（`cases.md`）。Ark のターゲットも同じ宣言である。
- 1〜5 の値は、`index.js` の require の束縛（`value_via`）を通る。`preload.js` の再 export
  （`module.exports = require('./index.js')`）は流出とみなさない（`oracle-design.md` C6 の 5）。
- `index.js` と `internal/constants.js` には、Phase D の修正が見る形が無い。そのため修正の後も 16 件は
  Exact のままである。

## E6. Oracle の版による違い（分母だけ）

v2 の Oracle は 7 回作り直した（`oracle-design.md` C6）。8e83a02 と d401ad1 の v2 の採点を、site ごとに最終版と
比べた。

| 比べた版 | Ark の TP・FP の判定が変わった site | representable が変わった site |
|---|---:|---:|
| 最初の版 | 0 | 460（lodash 303、axios 151、three 6 など。値の層の対象を export オブジェクトに絞ったため分母から外れた） |
| 2・3 番目の版 | 0 | 307 |
| 5 番目の版（`export_unsafe` の前） | 0 | 45（semver。`internal/re.js` を通る site。Ark は Unresolved か参照なし） |
| 6 番目の版（関数の `this` の規則の前） | TP → `unsafe_strong` が 13（8e83a02）・14（d401ad1）。FP への変化は 0 | 14（commander。E2 の要点の site） |

4 番目の版の採点は破棄した。古い 2 版のソースに載せたハーネスが最新でなく、現在の版のテストバイナリは
ミューテーションの実行と重なってビルドされたためである（以後、採点のスクリプトはハーネスの同一性を確かめ、
採点中はコードを変更しない）。その版の Oracle の出力では、semver の 1〜5 の 5 件が `escapes` だった
（`preload.js` の再 export）。

最初の版の採点で「v2 で見えるようになった Recall の課題」として挙げていたもの（lodash の
`export default { … }` を通るメンバーなど）は、最終版では v2 の対象の外になった。課題自体は残る
（`progress.md` の残課題、アーカイブ）。
