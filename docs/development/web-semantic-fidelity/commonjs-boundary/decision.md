# リリース判定: READY

対象は、定義した静的な意味のモデルと Confidence の契約である（`contract.md`）。JavaScript の任意の実行時の挙動を
保証するという意味ではない。リリースの操作（push・PR・merge・tag・release）は、ユーザーの承認を待つ。

## 判定の根拠（ミッションの READY の条件）

| 条件 | 結果 | 根拠 |
|---|---|---|
| CommonJS の export 値を通る参照に、不適切な Exact / Strong が無い | 満たす | 由来による上限（`ExportDraft.Mutable`）を入れた。動的な変更を検出するかどうかに依らない。独立レビューの 2 ラウンド（351 参照）で漏れは 0。ミューテーション M1〜M6・M8〜M10 はすべて検出された。固定コーパスでは `unsafe_strong` が 290 件から 0 件になった |
| 字句の束縛の Soundness | 維持 | 境界のテストの陽性対照。レビュー 2 で 8f8f249 と同じ Exact が 14 件、差は 0 |
| ESM / TypeScript の Soundness | 維持 | ミューテーション M7・M11（ESM と同じファイルの継承の降格）を、それぞれ 42 件・7 件のテストが検出する。言語横断の TS（rxjs、immer）と lodash は、8f8f249 と同一。降格しすぎの例外は 2 つで、`contract.md` §6 に記録した |
| 固定コーパスで重大な回帰が無い | 満たす | 誤った Exact / Strong と False Outside は 0 のまま。候補を含む発見率は 20 コーパスのすべてで同一で、実際の Recall の損失は 0。高 Confidence の Recall の低下は、契約による降格で、正解は Candidate として残る（`fidelity.md`） |
| Cache / MCP の契約 | 維持 | ts-16 は JS・HTML の合成の版に伝わる。cold・warm・版の更新は `--no-cache` と同一。MCP の応答のキーは既存の項目だけで、`internal/mcp` のコードは変えていない |
| 最終レビューに未解決の High が無い | 満たす | ラウンド 1 は GREEN（Medium 1 件 → 修正）、ラウンド 2 は GREEN（Low 3 件 → 文書で対応） |
| 既知の制限の文書化 | 満たす | 下の表、`contract.md`、`docs/language-support.md`、`ARCHITECTURE.md` §2 |

## 保証の範囲

### Guaranteed（Exact / Strong の主張に責任を持つもの。静的なモデルの中で）

- Lexical Binding Resolution。CommonJS のモジュールの中の字句の参照も含む。
- 検証済みの静的な参照解決: ESM の import / export、TypeScript のソースレベルの export、モジュールの中の宣言。
- 既存の ESM / TypeScript の意味解析（メンバー、継承、型の位置）。

### Best effort（Candidate として提示する。値の同一性は主張しない）

- CommonJS の static export mapping（`module.exports = { a }`、`exports.a = f`）。
- CommonJS の export を通る参照の候補。`require`、ESM からの import、再 export、CommonJS のクラスの継承を含む。
- トランスパイル済み CommonJS（tsc・Babel の出力）のナビゲーション。

### Not guaranteed

- CommonJS の動的な Export Value Resolution。実行時に、export の値が宣言のままであること。
- 任意の反射的な変更の経路。前のミッションの D7〜D13 で見つかった形は、どれも Exact / Strong の主張の外になった。
- 実行時の export の値の不変性。

## リリース前の必須作業

1. ユーザーによる差分の確認と承認。
2. push・PR・CI の確認。これらは承認の後に行い、本ミッションでは行わない。
3. CHANGELOG / リリースノートへの記載。CommonJS の export を通る参照の Confidence が、Exact から Candidate に下がる。
   そのため、`get_callers` などのグラフの辺から外れ、同じ応答の `candidates` に移る。利用者から見える変化として明記する。

## リリース後の課題

- Export Value Analysis の独立レイヤー化（下）。
- ESM の `export default { … }` を通るメンバーアクセスの Recall（F6B-5 からの持ち越し）。
- トランスパイル済み CommonJS の Recall。主因は、getter による再 export（`Object.defineProperty(exports, …)`）と、ES5 のクラスの `this` の扱いである（`fidelity.md`）。
- Low-A の非対称の解消。`require()` が ESM を指すときの名前空間の扱いを、実行環境のモデルとして決める必要がある。

## 将来のアーキテクチャ（今回は実装しない）

- **Export Value Analysis を独立したレイヤーにする。** いまの `Mutable` は、「この export を通った」という由来の印にすぎない。
  将来は、export の値について「静的に閉じた（Sealed）」「書き込みが見える」「流出がある」などを判定する層を、
  resolver の外に置く。その証拠を、Candidate の順位や、閉世界を仮定した Strong（オプトイン）に使う設計が考えられる。
- 必要になるもの:
  - モジュールをまたぐ書き込みの解析
  - `Object.freeze` などの封印の証拠
  - トランスパイラの定型の認識（`__exportStar`、getter の再 export）
  - 実行環境（Node の require(esm)、バンドラ）のモデル
- **前のミッションの監査の層（526e8de の `commonjs_audit.go`）は、その層の材料として保全してある。** これを Exact の根拠に戻さない。
  D7〜D13 のとおり、構文の規則では反射的な経路を閉じられないからである。
