# Fidelity: Before / After

- Before: 8f8f249（ts-14。最後のコミット済みの版）。After: 本ブランチの作業ツリー（ts-16）。どちらも同じ採点のハーネス
  （`internal/index/fidelity_baseline_test.go`、無変更）と、同じ Oracle（v1 `javascript-tsc.js`、v2 `javascript-tsc-value.js`、
  無変更）で採点した。作業領域の `gate/cb-verify.sh` と `gate/cb-table.py` で再現できる。
- **高 Confidence の Recall** = (Exact の正解 + Strong の正解) / represented。
- **候補を含む発見率** = (Exact の正解 + Strong の正解 + Candidate hit) / represented。正しい対象が Candidate としても失われた
  ときだけ下がる（実際の Recall の損失）。
- 「誤った Exact・Strong 等」は、`exact_fp`・`strong_fp`・`local_fp`・`external_fp`・`ambiguous_fp` の合計である。

## 12 コーパス

### Oracle v1

| corpus | represented | Exact 正解 前/後 | Strong 正解 | Candidate hit | Candidate miss | Unresolved | 誤った Exact・Strong 等 | False Outside | 高 Confidence の Recall % | 候補を含む発見率 % |
|---|---:|---|---|---|---|---|---|---|---|---|
| lodash | 3591 | 3536 / 3536 | 7 / 7 | 3 / 3 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 98.7 / 98.7 | 98.7 / 98.7 |
| express | 243 | 201 / 190 | 2 / 2 | 1 / 12 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 83.5 / 79.0 | 84.0 / 84.0 |
| preactsrc | 617 | 551 / 551 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 89.3 / 89.3 | 89.3 / 89.3 |
| threesrc | 14715 | 12455 / 12455 | 43 / 43 | 896 / 896 | 414 / 414 | 0 / 0 | 0 / 0 | 0 / 0 | 84.9 / 84.9 | 91.0 / 91.0 |
| jsfix | 38 | 30 / 27 | 2 / 1 | 0 / 4 | 1 / 1 | 0 / 0 | 0 / 0 | 0 / 0 | 84.2 / 73.7 | 84.2 / 84.2 |
| jsfix2 | 1 | 0 / 0 | 0 / 0 | 1 / 1 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0.0 / 0.0 | 100.0 / 100.0 |
| rxjs | 3255 | 1931 / 1931 | 0 / 0 | 224 / 224 | 59 / 59 | 12 / 12 | 0 / 0 | 0 / 0 | 59.3 / 59.3 | 66.2 / 66.2 |
| axios | 543 | 491 / 491 | 0 / 0 | 23 / 23 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 90.4 / 90.4 | 94.7 / 94.7 |
| d3array | 155 | 155 / 155 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 100.0 / 100.0 | 100.0 / 100.0 |
| commander | 308 | 171 / 156 | 0 / 0 | 96 / 111 | 14 / 14 | 0 / 0 | 0 / 0 | 0 / 0 | 55.5 / 50.6 | 86.7 / 86.7 |
| semver | 703 | 532 / 359 | 0 / 0 | 4 / 177 | 5 / 5 | 45 / 45 | 16 / 0 | 0 / 0 | 75.7 / 51.1 | 76.2 / 76.2 |
| immer | 648 | 493 / 493 | 0 / 0 | 1 / 1 | 0 / 0 | 1 / 1 | 0 / 0 | 0 / 0 | 76.1 / 76.1 | 76.2 / 76.2 |

### Oracle v2

| corpus | represented | Exact 正解 前/後 | Strong 正解 | Candidate hit | Candidate miss | Unresolved | 誤った Exact・Strong 等 | False Outside | 高 Confidence の Recall % | 候補を含む発見率 % |
|---|---:|---|---|---|---|---|---|---|---|---|
| lodash | 3591 | 3536 / 3536 | 7 / 7 | 3 / 3 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 98.7 / 98.7 | 98.7 / 98.7 |
| express | 243 | 201 / 190 | 2 / 2 | 1 / 12 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 83.5 / 79.0 | 84.0 / 84.0 |
| preactsrc | 617 | 551 / 551 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 89.3 / 89.3 | 89.3 / 89.3 |
| threesrc | 14715 | 12455 / 12455 | 43 / 43 | 896 / 896 | 414 / 414 | 0 / 0 | 0 / 0 | 0 / 0 | 84.9 / 84.9 | 91.0 / 91.0 |
| jsfix | 38 | 30 / 27 | 2 / 1 | 0 / 4 | 1 / 1 | 0 / 0 | 0 / 0 | 0 / 0 | 84.2 / 73.7 | 84.2 / 84.2 |
| jsfix2 | 1 | 0 / 0 | 0 / 0 | 1 / 1 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0.0 / 0.0 | 100.0 / 100.0 |
| rxjs | 3255 | 1931 / 1931 | 0 / 0 | 224 / 224 | 59 / 59 | 12 / 12 | 0 / 0 | 0 / 0 | 59.3 / 59.3 | 66.2 / 66.2 |
| axios | 543 | 491 / 491 | 0 / 0 | 23 / 23 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 90.4 / 90.4 | 94.7 / 94.7 |
| d3array | 155 | 155 / 155 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 100.0 / 100.0 | 100.0 / 100.0 |
| commander | 294 | 157 / 156 | 0 / 0 | 96 / 97 | 14 / 14 | 0 / 0 | 0 / 0 | 0 / 0 | 53.4 / 53.1 | 86.1 / 86.1 |
| semver | 676 | 548 / 359 | 0 / 0 | 4 / 193 | 5 / 5 | 0 / 0 | 0 / 0 | 0 / 0 | 81.1 / 53.1 | 81.7 / 81.7 |
| immer | 648 | 493 / 493 | 0 / 0 | 1 / 1 | 0 / 0 | 1 / 1 | 0 / 0 | 0 / 0 | 76.1 / 76.1 | 76.2 / 76.2 |

unsafe_strong (v2): express 0→0, commander 14→0, semver 0→0, rxjs 0→0, axios 0→0, lodash 0→0, immer 0→0

## トランスパイル済み CommonJS（8 コーパス）

コーパスの定義（作業領域 `gate/tc/` の `build.sh` で再生成できる。生成は決定的で、2 回の生成で一致した）:

| コーパス | 元 | 生成 | 主な形 |
|---|---|---|---|
| tsc-rxjs-es5（251 ファイル） | rxjs 7.8.1 `src`（npm の tarball） | TypeScript 5.6.3 `tsc --module commonjs --target es5 --lib es2015,dom --downlevelIteration --esModuleInterop` | `__exportStar`・`__createBinding`・`__assign.apply(this, …)`・`__extends`・`__generator`・`exports.x = void 0` の連鎖 |
| tsc-immer-es2017（15） | immer 10.1.1 `src` | 同 `--target es2017 --lib es2017,dom --esModuleInterop` | `__exportStar`・`__createBinding` |
| tsc-mix-es5（5） | 自作（`gate/tc/mix-src/ts`） | 同 `--target es5 --lib es2015 --downlevelIteration --esModuleInterop` | `__importDefault`・`module.exports =`（`export =`）・`__awaiter`・`__assign` |
| dist-rxjs-cjs（250） | rxjs 7.8.1 の公開物 `dist/cjs`（実プロジェクトの tsc の出力） | そのまま | 同上（inline helper） |
| babel-axios-ie11（61） | axios 1.7.9 `lib`（ESM） | Babel 7.25.2 + preset-env 7.25.4、`targets: "ie 11"`、`modules: "commonjs"` | `_classCallCheck(this, …)`・`_interopRequireDefault`・`Object.defineProperty(exports, "__esModule", …)`・`exports.default` |
| babel-d3array（61） | d3-array 3.2.4 `src` | 同 `targets: "defaults"` | 同上 |
| babel-mix-ie11（4） | 自作（`gate/tc/mix-src/js`） | 同 `targets: "ie 11"` | `_classCallCheck(this, …)` |
| dist-uuid（21） | uuid 9.0.1 の公開物 `dist/*.js`（実プロジェクトの Babel の出力、tarball の sha256 `9fa6f415…3f7e`） | そのまま | `_interopRequireDefault`・`exports.default` |

生成物の sha256（ファイルを連結した先頭 16 桁）は `gate/tc/corpus/manifest.txt` にある。生成は決定的で、2 回の生成で一致した。

Oracle は v1 / v2 をそのまま適用した。TS の checker が判断できない site（`__exportStar` を通る名前など）は Unknown として
数え、評価の分母に入れない。

### Oracle v1

| corpus | represented | Exact 正解 前/後 | Strong 正解 | Candidate hit | Candidate miss | Unresolved | 誤った Exact・Strong 等 | False Outside | 高 Confidence の Recall % | 候補を含む発見率 % |
|---|---:|---|---|---|---|---|---|---|---|---|
| babel-axios-ie11 | 1494 | 1305 / 1305 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 87.3 / 87.3 | 87.3 / 87.3 |
| babel-d3array | 599 | 417 / 417 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 69.6 / 69.6 | 69.6 / 69.6 |
| babel-mix-ie11 | 129 | 92 / 92 | 0 / 0 | 0 / 0 | 0 / 0 | 1 / 1 | 0 / 0 | 0 / 0 | 71.3 / 71.3 | 71.3 / 71.3 |
| dist-rxjs-cjs | 2169 | 778 / 508 | 0 / 0 | 0 / 270 | 0 / 0 | 295 / 295 | 0 / 0 | 0 / 0 | 35.9 / 23.4 | 35.9 / 35.9 |
| dist-uuid | 308 | 235 / 235 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 76.3 / 76.3 | 76.3 / 76.3 |
| tsc-immer-es2017 | 203 | 121 / 121 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 59.6 / 59.6 | 59.6 / 59.6 |
| tsc-mix-es5 | 53 | 27 / 25 | 0 / 0 | 0 / 2 | 0 / 0 | 1 / 1 | 0 / 0 | 0 / 0 | 50.9 / 47.2 | 50.9 / 50.9 |
| tsc-rxjs-es5 | 1997 | 531 / 525 | 0 / 0 | 0 / 6 | 0 / 0 | 84 / 84 | 0 / 0 | 0 / 0 | 26.6 / 26.3 | 26.6 / 26.6 |

### Oracle v2

| corpus | represented | Exact 正解 前/後 | Strong 正解 | Candidate hit | Candidate miss | Unresolved | 誤った Exact・Strong 等 | False Outside | 高 Confidence の Recall % | 候補を含む発見率 % |
|---|---:|---|---|---|---|---|---|---|---|---|
| babel-axios-ie11 | 1494 | 1305 / 1305 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 87.3 / 87.3 | 87.3 / 87.3 |
| babel-d3array | 589 | 417 / 417 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 70.8 / 70.8 | 70.8 / 70.8 |
| babel-mix-ie11 | 126 | 92 / 92 | 0 / 0 | 0 / 0 | 0 / 0 | 1 / 1 | 0 / 0 | 0 / 0 | 73.0 / 73.0 | 73.0 / 73.0 |
| dist-rxjs-cjs | 1239 | 508 / 508 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 41.0 / 41.0 | 41.0 / 41.0 |
| dist-uuid | 305 | 235 / 235 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 77.0 / 77.0 | 77.0 / 77.0 |
| tsc-immer-es2017 | 201 | 121 / 121 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 60.2 / 60.2 | 60.2 / 60.2 |
| tsc-mix-es5 | 48 | 27 / 25 | 0 / 0 | 0 / 2 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 56.2 / 52.1 | 56.2 / 56.2 |
| tsc-rxjs-es5 | 1067 | 525 / 525 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 49.2 / 49.2 | 49.2 / 49.2 |

unsafe_strong (v2): babel-axios-ie11 0→0, babel-d3array 0→0, babel-mix-ie11 0→0, dist-rxjs-cjs 270→0, dist-uuid 0→0, tsc-immer-es2017 0→0, tsc-mix-es5 0→0, tsc-rxjs-es5 6→0

## 読み方

- **候補を含む発見率は、20 コーパスのすべてで変わらない。** Exact / Strong から外れた正解は、すべて Candidate として残った。
  候補ごと失われた参照（実際の Recall の損失）は 0 件である。
- **高 Confidence の Recall** は、CommonJS の export を通る参照の多いコーパスで下がる。これは契約による降格で、回帰ではない。
  - semver（v1）: 75.7% → 51.1%
  - dist-rxjs-cjs（v1）: 35.9% → 23.4%
  - commander（v1）: 55.5% → 50.6%
  - express: 83.5% → 79.0%
  - jsfix: 84.2% → 73.7%

  ESM のコーパス（lodash、preact、three、rxjs と immer の TS ソース、d3-array、axios）と Babel の出力は、1 件も変わらない。
  axios の `lib` は ESM である。Babel の出力は、モジュールをまたぐ参照が `_interopRequireDefault(…).default` を通るので、
  もともと解決していない。
- **`unsafe_strong`（v2）は 0 になった。** 前のミッションで「Oracle の過剰な近似」と分類した 290 件（commander の 14 件、
  dist-rxjs-cjs の 270 件、tsc-rxjs-es5 の 6 件）は、そもそも Strong / Exact を主張しなくなった。
- semver の v1 の `local_fp` 16 件も 0 になった。`bin/semver.js` の `semver.valid(v)` などで、`semver` は `require('../')` の
  束縛である。CommonJS の export を通るこれらの site では、v1 の契約が Ark と食い違っていた（v2 は誤りとして数えない。F6B-5）。
  Ark が Exact を主張しなくなったので、v1 でも誤りに数えなくなった。
- 誤った Exact / Strong、False Outside、`unknown_strong` は、Before と After のどちらでも 0 である。
- 前のミッションの監査の版（526e8de）では、express の `View` の 1 site を失っていた。今回の版では、`View` は Candidate として残る
  （Before と After で、候補を含む発見率は同じ）。
