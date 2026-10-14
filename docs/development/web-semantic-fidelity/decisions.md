# Decisions

## D0. ブランチの準備（Phase 0）

- 開始時の状態: `feat/web-semantic-fidelity`、`research/multilang-fidelity-baseline`、`main` が
  すべて `bab9b1e`（v6.4.2）。作業ツリーに未コミットの監査成果物 11 ファイル（監査報告の範囲と
  一致）。
- 監査ブランチへ切り替え（作業ツリーの変更はそのまま）、`go vet ./...` と
  `go test ./internal/index/ .` を再確認して、監査成果物をコミット（`32005c8`）。
- `feat/web-semantic-fidelity` は独自のコミットを持たず `32005c8` の祖先だったので、
  `git branch -f` で前方へ移動した（履歴の書き換えではない。`git merge-base --is-ancestor` で確認）。

## D1. JavaScript の抽出に TypeScript の抽出器を共有する（Phase 1）

- 採用: TypeScript の `extractor` を JavaScript の文法で動かし、文法差は抽出器が吸収する。
- 理由: binding / export / メンバー / receiver の型 / JSX の抽出は ES の構文と意味が同じ。監査では
  JS を TS の Provider にかけると lodash の誤りが 38 → 0、正しい Exact も増えた（ただし TS の欠陥も
  持ち込む）。欠陥（shadowing、誤った Outside）は Phase 1 で TS と同時に直す。
- 不採用:
  - TS の Provider を JS にそのまま使う（`.js` を TS の文法で読む）: TS の文法は `a < b > (c)` を
    ジェネリック呼び出しとして読むなど、JS と意味が違う構文がある。
  - 抽出器全体を新しい共通パッケージへ移す: TS の多数のテストがパッケージ内部の関数を使っており、
    移動の差分が大きく、認定済みの TS を壊す危険に見合わない。共通化はスコープ解析（新しい
    コード）から始める。
  - `tdewolff/parse/v2/js` への移行: 既存の tree-sitter ベースの JS / TS 共通化を損なうので行わない
    （ミッションの指示）。比較は Phase 2 の終わりに記録する。

## D2. ローカルに隠された名前は Candidate に cap する（Phase 1）

- 採用: `ConfidenceCap = "candidate"`（Go の `localCap` と同じ）。スコープは過大近似。
- 不採用: 参照を出さない（Go の値の参照の扱い）。find_references は構文上の参照を一覧するツールで、
  呼び出しを消すと利用者から見えなくなる。Candidate なら edge は作られない。
- 不採用: 正確なブロックスコープと TDZ のモデル化。過大近似の Recall の損失は小さく（同じ名前が
  関数内の別ブロックで宣言される場合だけ）、正確なモデルは実装の誤りの危険が大きい。

## D3. 共通 resolver: 「表せない」と「外部」を分ける（Phase 1）

- 変更: `resolveViaReceiverBinding` は、binding の先に Ark のシンボル（クラスでない値）や
  リポジトリ内の module があってメンバーが見つからないとき、OutsideRepository にしない。
  `resolveViaBinding` も、binding の module がリポジトリ内で解決できたのに export が見つからない
  ときは OutsideRepository にしない。
- 理由: OutsideRepository は「参照先がリポジトリの宣言の外にある」という主張。object literal の
  メンバーや、Ark が export を抽出できない module（CommonJS など）は、リポジトリの中にある。
- 影響: TypeScript（と binding を出す言語すべて）。全言語の回帰テストと rxjs の採点で確かめる。

## D4. CommonJS の最小限の binding を Phase 1 に前倒しする（Phase 1）

- 経緯: JavaScript が `var` を抽出するようになり、`var x = require("pkg")` の変数がシンボルになって、
  その呼び出しが同じファイルの変数に Exact で結ばれた（express で誤り 47 件。Oracle の答えは外部
  パッケージ）。`require` で初期化した変数は、実体は import の束縛なので、TS の import と同じく
  シンボルにしないことにした（JS / TS の両方）。
- その結果、以前は名前の一意性で当たっていた解決（`var Layer = require("./layer")` の `new Layer()`）が
  Candidate に落ち、express の Series 1 の回復が 38 → 33（13% 減）となり、合格基準 P1-4（5% 以内）を
  満たさなかった。原因は Phase 1 の Soundness 修正そのもので、時間の不足ではない。
- 採用: CommonJS の束縛と export のうち、構文が意味を決める形だけを Phase 1 で実装する
  （`typescript/commonjs.go`、`ecmascript.RequireBindings`）。束縛は言語中立の BindingDraft
  （`Imported` = `"module.exports"` か export 名）、export は ExportDraft。トップレベルの文だけ。
  同じ export 名の二重の代入は 2 つの export になり、束縛は曖昧（Candidate）。
- Phase 2 に残すもの: object literal のメンバー、prototype のメソッド、関数内の require、その他の形。
- 基準の変更: なし（P1-4 を満たすための実装の前倒し）。

## D5. 相対パスの未索引の module は外部ではない（Phase 1）

- D3 の実装中に見つけた: 相対 specifier が索引されないファイル（`./data.json`、`./a.css`）を指すと、
  module が解決できないことを「外部」とみなして OutsideRepository にしていた。相対パスはリポジトリ内の
  パスを名指しするので、候補を持つ module の未解決は dangling（リポジトリのもの、表せない）とする。
  候補を持たない module（パッケージ）だけが external。

## D6. receiver の型の証拠を、隠された名前と型演算子から出さない（Phase 1）

- `const Animal = require("./a"); const x = new Animal()` の `Animal` はローカル（束縛）なので、
  名前の一致で別のファイルの同名クラスに結ばれる危険がある。型名がローカルに隠されているときは
  receiver の型の証拠を捨てる（`types.go` の `enterFunction`）。
- TypeScript の標準ユーティリティ型（`Partial`、`Exclude` など）は値の型の名前ではないので、
  receiver の型の証拠にしない（rxjs の `teardown: Exclude<TeardownLogic, void>` が外部の型 `Exclude`
  として Outside になっていた）。
- resolver: receiver の型名がリポジトリの値（クラスでない宣言）に解決されるとき、または binding が
  リポジトリ内で途切れるときは、OutsideRepository にしない（`resolveTypeName` の `inRepository`）。

## D7. JavaScript の同名の宣言はそれぞれシンボルにする（Phase 1）

- 共有の抽出器は TS で同じ identity を 1 度だけ記録する（オーバーロードのシグネチャと実装）。
  JavaScript は同名の関数宣言の重複が合法（後のものが勝つ）なので、JS の dialect では重複を排除せず、
  index の identity 規則（序数の ID）に任せる（`TestDeclarationIdentity_ReferenceToNamesakesStaysAmbiguous`）。

## D8. 既知の制限として残すもの（Phase 1）

- パッケージが自分自身を名前で import する（preact の `import { render } from "preact"`）と、bare
  specifier なので外部（Outside）とする。Provider は `package.json` を読めない。Oracle も判定不能に
  分類するので採点外。修正には index が `package.json` の `name` / `exports` を Provider の外で
  解釈する必要があり、今回は行わない。
- スコープの過大近似による Recall の損失（three の 3 件: 同じ関数の別ブロックや本体で同名のローカルが
  宣言）。TS の `declare module` / `namespace` / `declare const` の中の名前もファイル全体のローカルになる。
- 合格基準 P1-4（Series 1 の減少 5% 以内）を文字どおりに満たさないのは jsfix2（Series 1 が 1 site で
  1 → 0）だけ。module から classic script のグローバルを呼ぶ形で、Candidate が健全な答え。
- module のファイルから classic script のグローバル関数を呼ぶ形（jsfix2 の `render`）は Candidate。
  script が読み込まれることは静的に証明できない。

## D9. JavaScript の module の候補の順位（Phase 1、Reviewer A の指摘）

- `./x` は `x.js` を最優先（0）、`x.jsx` / `x.mjs` / `x.cjs` と `x/index.*` は同順位（1、複数あれば曖昧）。
  Node.js と bundler は `x.js` をディレクトリの index より先に選ぶ点で一致し、他の拡張子の扱いは異なる
  （Node.js の require はそれらを試さない）。

## D10. JS / TS の継承を辿る（Phase 2）

- 継承の索引（`resolver/inheritance.go` の `buildStructuralIndex`）は PHP の修飾名で親を特定していた。
  修飾名の無い継承の参照（JS / TS の `extends`）は、receiver の無い名前に限り、その名前の解決
  （import の binding か同じファイルの宣言）が Exact で一意な型のときだけ親とする。それ以外は不明で、
  探索はそこで止まる（既存の規則）。
- receiver の型や binding を証拠とする経路（`memberResolution`）でも、自分の型にメンバーが無ければ
  `inheritedResolution` で親を辿る（Speculative の参照は除く）。証拠の種類は元の経路のものを引き継ぐ。
- 呼び出しが呼び出せるメンバーだけを探す規則は PHP の意味論（メソッドとプロパティの名前空間が別）。
  ECMAScript では同じ名前空間なので、関数型のプロパティが親のメソッドを隠す（rxjs の `Scheduler.now`）。
  Provider が `Extraction.MembersShareNames` を宣言し（TS / JS）、その言語の型では種類を問わず最も近い
  メンバーを返す。最初は経路（R0 か否か）で区別したが、PHP も receiver の型の経路を通るため
  （`TestInheritance_CallSelectsMethodNotProperty` が失敗）、言語の性質として明示する形に改めた。
- `super.m()`: クラスの `extends` が単純な名前（ローカルに隠されていない）なら、それを receiver の型
  とする。`TestRefs_NoFabrication` の「super は推測しない」は、静的な意味（`extends` の名前）に基づく
  証拠として改めた。

## D11. 値のメンバー（Phase 2）

- 採用: トップレベルの `const` オブジェクトのメンバー、`F.prototype.m = …` / `F.prototype = {…}`、
  `const` オブジェクトへの後付けの `X.m = …` を、所有者のメンバー（Parent / Receiver）として抽出する
  （`typescript/values.go`）。コンストラクタ関数（prototype のメンバーを持つ関数宣言）の本体の `this` は
  そのインスタンス。`const` オブジェクトを指す receiver の名前は、そのオブジェクトを receiver の型とする。
- resolver: 型でない宣言（関数・定数）でも、包含するメンバーを持てば receiver の型として扱い、
  **Strong を上限** にする（コードがメンバーを書き換えうる）。binding の先がそのような値のときも同じ。
- 不採用: 関数自身のプロパティ（`F.m = …`）。メンバーのシンボルは prototype のメンバーと区別できず、
  `new F().m()` を static なメンバーに結んでしまう。`let` / `var` のオブジェクト（再代入）、TS の型注釈の
  付いた `const` オブジェクト（注釈の型がオブジェクトの型で、TypeScript もそれに結ぶ）。

## D12. CommonJS の名前空間（Phase 2）

- `const u = require("./u")` は `module.exports` への束縛（`language.ModuleExportsName`）。モジュールが
  `module.exports = X` を述べていれば X、述べていなければ `exports.a = …` で埋まるオブジェクト、つまり
  モジュールの名前空間を指す（resolver の `bindingTargets`）。`module.exports` を置き換えるモジュールは
  プロパティ export を出さない（D4 の改訂）ので、名前空間は空になり、Unresolved。

## D13. 冗長な防御は残さない（Phase 2）——ただし 1 件は誤りだった

- Mutation で、二重の防御のため検出できない条件が 3 つ見つかった（`replacesModuleExports`、変数宣言側の
  `const` の確認、receiver の型の局所名の確認）。前の 2 つは別の条件（`finishCommonJS`、`valueOwners`）が
  同じ誤りを防いでいたので削除した。
- **3 つ目（receiver の型の局所名の確認）を `localCap` が代わりに防ぐと判断して削除したのは誤り**だった。
  `localCap` が cap するのは receiver の型が空のときだけで、`receiverType` が const オブジェクトの名前を
  返すと cap が効かない。Reviewer A（Phase 2）が、ローカルや引数に隠された const オブジェクトへの誤った
  Strong（JS / TS、Oracle と食い違い）として検出した（重大-1）。条件を戻し、5 形の負例テストを加え、
  Mutation（P15）をこの経路に向け直した。教訓: 「冗長」と判断する前に、代わりの条件が同じ経路に効くことを
  テストで確かめる。

## D14. static の文脈と同名のメンバー（Phase 2、Reviewer A）

- TS の抽出器は、同じクラスの static と instance の同名メンバー（同じ種類）を 1 つのシンボルにまとめていた。
  解決先が先に宣言された方（多くは static）になり、rxjs の `Scheduler.now` で誤った Exact を作っていた
  （Phase 0〜2 で「採点上の不一致」と分類していたが、実際には static の宣言を指す誤り）。static の印を
  重複排除の鍵に加え、別のシンボルにした（参照は 2 つの候補で Candidate）。
- static メソッドと static フィールドの初期化子では、`this` はクラス自身、`super` は親クラス自身で、
  インスタンスではない。シンボルが static とインスタンスを区別しないので、そこでは `this` / `super` を
  証拠にしない（`staticScope`）。three で 1 件の正解が Candidate になった（健全な喪失）。
- クラスへの prototype の代入（`K.prototype.b = …`）は所有者にしない（クラス名の receiver の static な
  呼び出し `K.b()` がそれに結ばれてしまう）。prototype の所有者は関数宣言だけ。

## D15. spread を含むオブジェクト（Phase 2、Reviewer A）

- `{ m() {}, ...base }` の後ろの spread はメンバーを置き換えうる。spread を含むオブジェクトは所有者に
  しない（前にある spread でも除外する過大な扱いで、Recall の損失だけ）。

## D16. CacheVersion（Phase 2、Reviewer A）

- Phase 2 で抽出の意味が変わった（値のメンバー、`super` の参照、static の分離、`MembersShareNames`）ので、
  TS を `ts-8`、JS を `js-6` に上げた。未リリースのブランチでも、コミットごとに抽出の意味が変われば上げる
  （開発ビルドの cache が古い抽出を返さないように）。

## D17. 既知の制限（Phase 2）

- `super.m()` は、親クラスのインスタンスのフィールド（矢印関数で初期化したもの）を拾う。実行時は prototype
  をたどるので、その親の親のメソッドが呼ばれる。TS は矢印関数のフィールドをメソッドとして扱うので、種類では
  区別できない。Oracle（TypeScript）も同じ答えで、TS 5.3 以降はコンパイルエラーになる形。
- メンバーの Recall は、Oracle 側の分母（`oracle_members`: property access の名前の site）では three 23.2%、
  rxjs 21.8%、lodash 1.6%、express 1.2%。型の証拠の無い receiver（引数、戻り値）が大半で、構文の証拠だけでは
  届かない。
- Mutation で検出できない条件として残すもの: 継承の親の特定を Strong でも受け入れる変異（module-scoped な
  ファイルでは名前の解決が Strong にならないので到達しない）、CommonJS の名前空間の `sub.complete` の条件
  （循環でしか効かない）。

## D18. 再レビューの修正（Phase 2、Reviewer A2）

- static と instance の同名の accessor は別のメンバー（対にするのは同じ static 性の get / set だけ）。
- prototype のメンバーとコンストラクタ関数の `this` は JavaScript だけ（TypeScript は `.ts` の関数を
  prototype の代入でコンストラクタとして読まない）。prototype に代入した矢印関数の `this` はモジュールの
  `this` で、インスタンスではない。
- JSDoc の `@type` が直前にある const オブジェクトは所有者にしない（型注釈と同じ理由）。
- `static {}` ブロックは static の文脈として走査する（以前は走査していなかった）。

## D19. HTML Parser は golang.org/x/net/html（Tokenizer と木構築の両方）（Phase 3）

比較の結果（`verification.md` の Phase 3）から、HTML Provider は `golang.org/x/net/html` v0.50.0 を使う。
位置は **Tokenizer** から、事実の裏付けは **木構築（`html.Parse`）** から取る。

- **採用の理由**: html5lib の木構築テスト（x/net 自身のテストで、template と foreign content の組み合わせ
  2 件の除外を除き合格）。差分を調べたすべての箇所で仕様どおりの字句解析（`<p/x/y/z>` の属性、
  `<rdar://problem/…>` のタグ名、EOF で終わる未完のタグを捨てる、`noscript` をスクリプト有効の
  ブラウザと同じく rawtext として扱う、属性値の中の旧式の参照）。BSD で Go チームの管理。使うのは
  `html` パッケージだけ。
- **x/net の仕様からの逸脱（Reviewer B）**: 重複した属性を残す（仕様 §13.2.5.33 は後のものを除く。
  Tokenizer も木も残す）。属性の名前と値の NUL を U+FFFD に置き換えない。select の解析は従来の仕様。
  Provider は前の 2 つを自分で扱う（下）。select の差は、木を Oracle とする以上、x/net の版に従う
  （既知の制限）。
- **tdewolff/parse/v2/html を採らない理由**: NUL（0x00）で入力の途中で止まり、以降を失う（html5lib の
  16 件。`Err()` が `unexpected NULL character` を返すので検出はできる）。EOF で終わる未完のタグを出す
  （仕様では捨てる）。SVG / MathML の中身を 1 つのトークンにまとめ、属性を出さない。速度は x/net と
  同程度（下）で決め手にならない。
- **gotreesitter（tree-sitter-html）を採らない理由**: html5lib の 19 件で、エラーを報告せずにタグを
  落とす（`<!--><div>--<!-->` の `<div>`、`<script><` の後）。エラーの報告は html5lib の 925 件、
  実 HTML の 14 件。実 HTML で報告しないタグ 1,418 個は 4 ファイル（いずれもエラー付き）で、1 ファイルが
  1,397 個を占める。時間は x/net の約 13 倍、メモリは約 11 倍。監査の 46 バイトの最小再現は、この版では
  `HasError` が真になる（`<p>` は落ちる）。
- **位置は Tokenizer から**: 木はノードにソースの位置を持たない。Tokenizer は属性の位置も返さないので、
  Provider は `Raw()` を HTML5 の属性の状態機械（§13.2.5.32〜39）で走査して名前と値の範囲を求め、
  Tokenizer の属性と 1 対 1 で照合する。照合は厳密な一致で、名前は ASCII だけを小文字化し、値には
  x/net と同じ正規化（改行の変換、属性モードの文字参照: 英数字の連続全体が参照名のときだけ展開し、
  `;` なしで `=` が続けば展開しない。x/net 固有の `&#x;` → U+FFFD と、ASCII 2 文字に展開される
  `&fjlig;` を含む）をかけてから比べる。`Raw()` は `Token()` / `TagAttr()` が
  バッファをその場で書き換える前にコピーする。照合が合わないタグは範囲を出さず診断にする。
- **事実は木で裏付ける（Reviewer B の指摘 1・2 への対応）**: Tokenizer は rawtext / RCDATA / CDATA を
  開始タグの名前だけで決めるが、木構築は挿入モードで決める（foreign content では rawtext にしない、
  select や frameset では開始タグを無視する、CDATA は foreign content でだけ許す）。そのため Tokenizer が
  報告するタグには、木構築にとってはテキスト・属性値・コメントの一部であるもの（偽のタグ）や、無視される
  もの、属性が別の要素（`html` / `body`）に統合されるものがある。偽は「タグを隠す」側にだけ働くとした
  当初の主張（AllowCDATA の固定）は誤りだった（Reviewer B の反例 15 件）。
  Provider は、Tokenizer が報告した開始タグ n の名前の直後に印の属性 ` <接頭辞>n=1 ` を挿入した
  ソースを `html.Parse` にかけ、タグ n の属性を次のすべてを満たすときだけ事実として使う:
  (1) 印 n を持つ要素（タグ n が作った要素とその複製）があり、その要素名がタグ n の名前と同じ
  （ASCII の大小と SVG の名前の調整を除く。`image` は `img`）。Tokenizer が rawtext として読んだ本物の
  タグの属性値の直後に空白なしでタグ n が続くと、印 n はその本物の要素に付く（Reviewer B の再レビューの
  指摘 1。`<math id='v1'><template>'<script><a t="</script>"<rect id=v2>` の `id=v2` は `<a>` の属性）。
  (2) その要素の、同じ名前の最初の属性が同じ値を持つ。
  (3) その要素が持つ印のうち n より前のタグに、同じ名前の属性が無い（`html` / `body` の統合は、まだ
  無い名前の属性だけを加える。同じ値でも前のタグのもの。再レビューの指摘 2）。
  (4) 名前にも値にも NUL を含まない。
  印がテキスト・コメント・属性値に現れたタグは偽、どの要素にも無いタグは無視されたタグ。印の名前は
  `data-ark-tok-` で、ソースにその文字列があれば別の接頭辞にする（文書の文字列や属性を印と取り違えない）。
  印は木構築が読む属性（type、color、face、size、encoding）でなく、値と後ろの空白で終わり（後ろの
  ` =b` や `/` の読み方を変えない）、引用符を含まない（偽のタグが木構築の引用符付きの属性値の中に
  あっても、その値を閉じない）。
  印が木構築を変えないことは仮定せず、毎回確かめる: 印を入れた木から印（属性と、テキスト・コメント・
  属性値の中の印の文字列）を除いたものが、元のソースの木と同じでなければ、そのファイルの属性はすべて
  使わない（Noah's Ark は書式要素の属性を比べるので、印で結果が変わる。html5lib の 4 件は同じ
  `<font size=…>` や `<b>` を 4 つ以上重ねた形で、古い WYSIWYG の HTML では起こりうる。ファイル全体の
  Recall を失うので診断を出す。書式要素だけ印を外して再試行する方法は、必要になったら検討する）。
  (要素, 属性, 値) の出現数を Tokenizer と木で比べる方法は採らない: 木構築にだけ見える本物と、
  Tokenizer にだけ見える同じ値の偽が打ち消し合う（合成 17）。
  これで Tokenizer の CDATA の扱いは既定のまま（AllowCDATA を使わない）でよい。木構築にだけ見える
  タグ（Tokenizer が rawtext として読み飛ばした本物）は Recall の損失になる。
- **重複属性と NUL**: 名前（ASCII で小文字化）が同じ属性は最初のものだけを使う（範囲も最初のもの）。
  名前か値に NUL を含む属性は使わない。
- **不完全な解析**: Tokenizer は `Raw()` で入力を隙間なく分割し、`bytes.Reader` から読んで
  `SetMaxBuf` を使わない限り `Err()` は `io.EOF` だけを返す（Reviewer B が `token.go` で確認）。
  そのため「`Raw()` の長さの和 = ファイルの長さ」と「`Err()` が `io.EOF`」は常に成り立ち、何も
  検出しない（防御的な表明として残す）。検出できる不完全さは **eof-in-tag**（EOF で終わる未完のタグ。
  仕様どおり捨てられ、最後の ErrorToken の `Raw()` に残る）で、Provider は診断を出す。未完のコメント、
  doctype、rawtext はトークンとして出るので対象外。印の検証が成り立たないファイルにも診断を出す。

## D20. 比較のハーネスをリポジトリに置く（Phase 3）

`docs/scripts/html-parser-eval/run.sh WORKDIR` で、版を固定した 3 つの Parser を一時モジュールで
ビルドし、コーパスを作り、比較の全項目を出力する。Go のソースは `//go:build ignore` で、Ark の
モジュールにも依存にも入らない。実 HTML のコーパスは既定で GOMODCACHE の `.html`（内容で重複除去）で、
マシンにより変わる。html5lib と合成のコーパスは固定。

## D21. HTML Provider の設計（Phase 4）

Parser の規則は D19（`docs/scripts/html-parser-eval/attrlib.go` と同じ結果）。言語は `html`、拡張子は
`.html` と `.htm`（`.xhtml` は XML の構文なので対象外）。tree-sitter の grammar は持たない。

**Symbol**

| 対象 | Kind | Name / Qualified | 範囲 |
|---|---|---|---|
| 文書（1 ファイルに 1 つ） | `document`（新設） | ファイル名 / FileID | ファイル全体 |
| id を持つ要素（HTML・SVG・MathML） | `element`（新設） | id / `<FileID>#<id>`（文書のスコープ）、`<FileID>#<k> <id>`（k 番目の `<template>` の内容。入れ子は `#<k1> <k2> <id>`） | 開始タグ |

- id は D19 の検証を通った属性だけ。空、ASCII の空白を含む（仕様で不正。IDREFS で参照できない）、
  テンプレート構文（`{{`、`{%`、`<%`、`<?`、`${`）を含む id は Symbol にしない。空白を含まないので、
  スコープの区切りの空白は id と衝突しない。FileID が `#` を含むファイルは要素の Symbol を出さない
  （相対 URL で参照できず、Qualified の区切りと衝突する）。
- 同じスコープの同じ id は、序数で別の Symbol（既存の規則）。参照は Candidate。
- `<template>` の内容は別の tree scope。木（印付き）で祖先の `<template>` をたどり、そのタグの序数で表す。
- `<a name>` と `<map name>` は Phase 4 では扱わない（IDREF は name を見ないので、id と同じ Qualified に
  すると誤る。種類を分ける必要がある）。

**Reference**（`ReferenceKind`: `id_reference`（新設）、`resource_link`（新設））

| 参照 | 例 | NameQualified | Confidence の上限 |
|---|---|---|---|
| 同じ文書の IDREF | `for`（label・output）、`form`、`list`、`headers`、`aria-activedescendant`・`aria-controls`・`aria-describedby`・`aria-details`・`aria-errormessage`・`aria-flowto`・`aria-labelledby`・`aria-owns`、`popovertarget`、`commandfor`、`itemref` | 参照のある場所のスコープの `#id` | Exact（動的な文書は Strong） |
| 同じ文書のフラグメント | `<a href="#x">`、`<area href>`、SVG の `href` / `xlink:href="#x"`、SVG の表示属性の `url(#x)`（`fill`、`stroke`、`clip-path`、`mask`、`filter`、`marker-start`・`-mid`・`-end`） | 同上 | 同上 |
| 他の HTML 文書 | `<a href="b.html">`、`<area>`、`<iframe src>`、`<frame src>` | 文書の FileID | Strong（URL とファイルの対応は配信の方式で変わる） |
| 他の HTML 文書の id | `<a href="b.html#x">` | `<b の FileID>#x` | Strong |

- 文書は `IdentityOnly`。参照はすべて `IdentityInRepository`（宣言が無ければ Unresolved。Outside に
  しない）。Container は文書。
- 動的な文書: `<script>` 要素、`on*` 属性、`javascript:` の URL、テンプレート構文がある文書では、DOM が
  実行時に変わりうるので、同じ文書の参照を Strong に cap する。
- `<template>` の内容の中の参照は、そのテンプレートのスコープで解決し、Candidate に cap する（内容が
  複製される先の tree は静的に分からない）。宣言的 shadow root（`shadowrootmode`）のテンプレートは、
  内容がその shadow root になるので cap しない（動的な文書の cap は残る）。
- 参照にしないもの: 空のフラグメント、`#top`（仕様の特別扱い）、`%` を含むフラグメント（パーセント
  デコードの規則が二通り）、テキストフラグメント（`:~:`）、`<base href>` がある文書の相対リンクと
  フラグメント、ルート相対（`/x.html`。Web のルートが分からない）、スキーム付き・`//` で始まる URL、
  ディレクトリ（`dir/`。index の解決は配信の方式次第）、HTML 以外の拡張子（CSS・JS・画像は Phase 7）、
  ルートの外に出るパス、値にテンプレート構文があるもの、IDREFS のうち空白で分けた後に空のもの。
- 相対パスは文書のディレクトリから解決し、クエリとフラグメントを除き、パーセントデコードする（不正な
  エスケープ、NUL、`\` を含むものは参照にしない）。

**Diagnostic**

| Code | Severity | 条件 |
|---|---|---|
| `html_eof_in_tag` | error | EOF で終わる未完のタグ（仕様で捨てられ、書かれた内容が失われる） |
| `html_tree_unverified` | warning | 印で木が変わり、属性を検証できない（D19。ファイルの id と参照を出さない） |
| `html_attribute_mismatch` | warning | 属性の範囲の照合が合わないタグ（そのタグの属性を出さない） |

インラインの script / style は Phase 8 まで解析しない。診断にすると、ほぼすべての文書で警告が出て
グラフ系のツールの注記が常に付くので、診断にはせず `docs/language-support.md` に書く。

**Graph**: `id_reference` → `EdgeReferences`。`resource_link` → 新しい `EdgeLinks`（Phase 7 の
言語横断のリソース関係でも使う）。

**共通部分の変更**: grammar の無い言語を registry と `syntax` が扱う（`ark syntax` は構文木が無いと
返す。`get_symbols` / `get_symbol` / `find_symbol` は Provider で抽出し、実際のファイル名を渡す）。
`TestRegistry_*`、CLI の補完と help、MCP の説明文、docs の言語表。SupportLevel は Graph。

## D22. Phase 4 のレビューの修正（Reviewer B・C）

D21 を次のとおり改める。

- **文書の事実は木から取る**（Reviewer B の指摘 1〜3）。Tokenizer が rawtext として読む区間の中身は、
  木構築には要素でありうる（`<svg><style><script>`）。そのため、動的な文書の判定（script 要素
  （HTML と SVG）、`on*` 属性、`javascript:` の URL）と `<base href>`（template の内容の外）は、
  Tokenizer と元のソースの木の和で決める。
- **隠れた重複のある id は Symbol にしない**。木で id 値 v を持つ要素の数が、Provider が Symbol に
  した v の数より多いとき（Tokenizer に見えない要素、書式要素の複製）、v の Symbol をすべて出さない。
  Symbol が「その id を持つ唯一の要素」を主張できないので、同じ文書からの参照も、他の文書からの
  `b.html#v` も Exact / Strong にならない（Unresolved）。Recall の損失は、実 HTML で 0、合成と
  html5lib で 10 要素。
- **動的な文書の判定を広げる**（指摘 5〜7）。`javascript:` は URL の前処理（先頭と末尾の C0 制御文字と
  空白、途中のタブと改行の除去）の後に判定する。`<iframe srcdoc>`（親と同じオリジンの文書）、
  フレームワークの束縛属性（`[x]`、`(x)`、`*x`、`#x`、`:x`、`@x`、`v-`、`x-`、`th:`、`ng-`、
  `data-ng-`、`hx-`、`data-hx-`、`data-bind`）、Angular の制御フローブロック（`@if (`、`@for (`、
  `@switch (`、`@defer`）を動的とする。
- **宣言的 shadow root**（指摘 4）。shadow root として扱うのは、`shadowrootmode` が open / closed で、
  親が shadow host になれる HTML 要素（valid custom element name、article、aside、blockquote、body、
  div、footer、h1〜h6、header、main、nav、p、section、span）で、その親に先行する宣言的 shadow root が
  無い template だけ（それ以外は仕様上ふつうの template）。参照の cap は、スコープのすべての template が
  shadow root のときだけ外す（通常の template の中の宣言的 shadow root は、template の内容の文書で
  有効かが確かめられないので Candidate）。
- **ハイパーリンクのフラグメントは文書のスコープ**。`<a>` / `<area>` の `#x` は、仕様の「indicated
  element」の探索が文書の木だけを見るので、template の内容や shadow root の中からでも文書の id を指す
  （template の内容の中なら cap は Candidate のまま）。SVG の `href` / `xlink:href` / `url(#)` は
  tree scope で解決する（主要な実装の動作）。`<iframe src="#x">` は同じ文書の別のインスタンスを開く
  ので参照にしない。SMIL（`<set attributeName="href">` など）による実行時の変化は扱わない（既知の制限）。
- **IDREF の型と要素**（指摘 8）。`aria-activedescendant`・`aria-details`・`aria-errormessage` は
  単一の id（ARIA 1.2。値全体が id なので、空白を含めば参照にしない）。`form` は form-associated の
  要素（button、fieldset、input、object、output、select、textarea）だけ（form-associated custom
  element は扱わない）。`popovertarget` は button と、button 状態の input（submit、image、reset、
  button）。`list` は候補リストを持つ状態の input だけ（hidden、password、checkbox、radio、file と
  button 状態を除く）。
- **フラグメントの文字**（指摘 11）: 非 ASCII、C0 制御文字、空白、`"`、`<`、`>`、`` ` ``、`%`、`\`
  を含むフラグメントは参照にしない（URL の前処理でエンコードされる、または CSS のエスケープ）。
- **パス**（指摘 12）: `%2F` / `%5C` を含むパスは参照にしない（URL では区切りでなく、サーバーの扱いが
  決まらない）。
- **ファイル名の `#`**（指摘 10）: パスに `#` を含むファイルは何も出さない（文書の Qualified が別の
  ファイルの要素の Qualified と一致しうる）。
- **診断**（指摘 13）: EOF で切れたのが終了タグなら `html_eof_in_tag` は warning（失うのは閉じだけ）。
- **共通部分**（Reviewer C）: `find_symbol` はファイルをリポジトリ相対の名前で抽出する（F1。絶対パスが
  出力に出ていた）。`ParseNamed` で実際のファイル名を渡すことは、HTML のほか Rust（crate のパス）と
  Terraform（module のディレクトリ、override ファイル）の `get_symbols` / `get_symbol` /
  `find_symbol` の出力も正しくする（F2。`_.rust` などの仮の名前が出力に漏れ、`override.tf` に宣言が
  あると誤っていた）。`secrets-scan` の提案では HTML をコードに数えない（F3）。新しい種類の対応を
  言語中立のテストで固定し（F4）、名前空間のテストに HTML を加えた（F5）。`ExtractCommentRanges` は
  構文木の無い言語で `ErrNoSyntaxTree` を返す（F7）。
- Phase 7 で `links` を言語横断のリソース関係に使うときは、`TestNameSpaces_NoRelationJoinsTwoNameSpaces`
  の不変条件を「名前による関係は名前空間をまたがない。またぐのはパスの証拠による edge と Candidate
  だけ」に改める（監査の設計どおり。Phase 7 の開始時に具体化する）。

## D23. Phase 4 の再レビューの修正（Reviewer B）

- **SVG のハイパーリンク**（R1）: svg の `<a>` の `href` / `xlink:href` は HTML の `<a>` と同じく扱う
  （フラグメントは文書のスコープ、他の文書へのリンクも同じ規則）。
- **NUL**（R2）: 木の id の数え上げでは、値の NUL を U+FFFD に置き換える（仕様の値）。
- **x/net の木の途切れ**（R3）: x/net の木は、SVG / MathML の統合点の中の `<template>` で以降をすべて
  失う（x/net のテストの除外と同じ系統）。印の検証は通ってしまうので、Tokenizer が報告した最後のタグの
  印が木のどこにも無いとき（途切れ、または末尾のタグが無視された: frameset の文書など）は、
  `html_tree_unverified` とし、何も述べない。
- **先行する宣言的 shadow root**（R4）: ホストの子孫のどこかに、文書の順で先行する宣言的 shadow root が
  あれば shadow root にしない（ブラウザは最初のものを木に入れずに付けるが、x/net は木に残し、adoption
  agency が書式要素の複製の中へ動かしうる。子孫のホストのものも数えるのは保守的な側）。
- **無視されたタグの id**（R5）: Tokenizer が報告し、印が木のどこにも無いタグ（木構築が無視した。x/net の
  select は従来の仕様）の id も、木の id の数に加える（現行の仕様では要素でありうる）。
- **custom element name**（R6）: valid custom element name の文法（PCENChar）と予約名を実装した。
- **列挙属性**（R7）: input の type は空白を除かずに ASCII の大小だけを無視して比べる。
- **動的な文書**（R8）: 同じオリジンでありうる文書を読み込む要素（`iframe src`、`frame src`、
  `object data`、`embed src`）、SSI（`<!--#`）、Blade（`@foreach(` など）・Svelte（`{#each` など）の
  制御構文を動的とする。SMIL による変化は既知の制限。
- **fuzz の Oracle**（R9）: id の数の比較で NUL を正規化し、動的の判定に `javascript:`、`srcdoc`、
  フレームワークの束縛属性を加えた。
- **落ちた template**（確認の S1）: 印が木のどこにも無い HTML の `<template>` 開始タグがあれば
  `html_tree_unverified`（最後のタグが残っても、x/net の状態が仕様から離れうる: 後の SVG `<title>` の
  `<base>` が失われる）。仕様の木構築が template の開始タグを無視するのは frameset 系の挿入モードだけ。
  診断の文言を、木の途切れも含む形に改めた（S2）。
- CacheVersion は `html-2` のまま（未コミットの間の変更）。

## D24. CSS Parser は CSS Syntax Level 3 の自前の実装（Phase 5）

比較の結果（`verification.md` の Phase 5）から、CSS Provider は CSS Syntax Level 3 のトークン化（§4）と
構文解析（§5。ネストした規則を含む現行の Editor's Draft の「block の内容」「宣言」「qualified rule」）を
自前で実装する（`docs/scripts/css-parser-eval/syntax.go`・`walk.go` を Phase 6 で `internal/languages/css`
に移す）。新しい依存は入れない。

- **採用の理由**: 仕様の公開テスト（SimonSapin/css-parsing-tests、149 件）に全件合格。実 CSS 967 ファイル
  （npm の 15 パッケージと GOMODCACHE の CSS）で、tdewolff の Lexer とトークンの境界・種類が、既知の
  3 種類の違い（下）を除いて完全に一致し、gotreesitter の CSS と宣言の数が完全に一致（494,564）。元の
  バイトの上で動くので、範囲は入力の前処理（BOM、CR LF・CR・FF、NUL、不正な UTF-8）の影響を受けない。
  CSS の仕様はエラーからの回復を決めている（解析は失敗しない）ので、選択は仕様への忠実さで決まる。
- **tdewolff/parse/v2/css を採らない理由**: 公開テストの 24 件で失敗し、うち 16 件は現行の仕様からのずれ
  （エスケープを含む識別子、NUL、改行を含む文字列、`url()` の各形。残り 8 件は仕様に無い unicode-range の
  トークンのテストで、公平のため数えない）。`url("x")` を 1 つの URL トークンにし（仕様では関数と文字列）、
  `--x` を独自の種類にする。値の復号（エスケープ）は利用側の仕事で、結局その部分を自前で書く必要がある。
  比較の Oracle（トークン境界の差分）として使う。
- **gotreesitter（tree-sitter-css）を採らない理由**: 実 CSS の 436/967 ファイル（45%）でエラーを報告する
  （宣言の数は一致するので、多くは文法が知らない構文への過剰な報告）。時間は約 9 倍。
- **仕様の版**（Reviewer B の指摘 6 で訂正）: 公開テストは、2014 年の CR の表現（unicode-range と match
  トークン `~=` `|=` `^=` `$=` `*=` `||`）を残している。どちらも 2021 年の CR にも現行の草案にも無い。
  ハーネスはテストの照合のときだけそれらを出し（`legacy`）、Provider に移す実装は現行の仕様どおり delim と
  ident・数値を出す。`one_declaration` のテストは値の前後の空白を残す古い手順で、ハーネスはその手順で照合し、
  Provider は現行の草案の手順を使う。現行の草案は 2021 年の CR から次を変えた: カスタムプロパティの値は
  `{}` とほかの値を混ぜてよい（#9317）、`--x:` で始まる prelude は qualified rule にならない（#9336）、
  入れ子の宣言を読めなかったときの扱い（#8834）、識別子の非 ASCII の範囲の制限（#7129）。前の 3 つは
  実装した。#7129 は採らず、U+0080 以上をすべて識別子の文字とする（2021 年の CR。主要なブラウザが #7129 を
  実装したかは確認できない。`@keyframes ✨x` のような名前を Symbol にする側の誤りは、同じ名前の参照も同じ
  規則で読むので、名前の一致に閉じる）。
- **安全性**（Reviewer B の指摘 1・2、確認の B）: 構文解析は 1 本のトークン列を一度だけ読む再帰下降で、
  入れ子の宣言を読めなかったときの読み直しは早く打ち切る（ident の次が colon でない、非カスタムの値で `{}` の
  前後に別の値がある）。宣言の試し読みは、括弧の対応を一度だけ前計算した表でブロックと関数を飛ばし、中身を
  読まない（中身は規則として読む側が一度だけ読む）。ブロックと関数の入れ子は 256 段で打ち切り、それより深い中身は読まずに `!too-deep` とする（再帰で
  スタックを使い切らない）。最悪の入力（`{` 100 万個、`(` 100 万個、32,000 規則の `@media`、4,000 段の
  入れ子、値が `{}` で始まる宣言の 250 段の入れ子と 800KB の値）が線形の時間（いずれも 1.5 秒以下）で終わる
  ことを `run.sh` で計測する。Phase 6 では、この上限と時間をテストと fuzz で
  固定する（受け入れ条件）。
- **不完全さと失われる内容の報告**（P5-3、Reviewer B の指摘 3・4・7）: CSS の解析は仕様の回復で常に最後
  まで進むので、「途中で止まる」ことは無い。書かれた内容が失われる、または閉じられずに終わる箇所を
  診断にする: 閉じないブロック（`}` を閉じとして消費したかで判定。括弧や内側のブロックの中の `}` は数えない）・
  コメント・文字列・URL（EOF）、bad-string、bad-url、規則にならない prelude（`!invalid`）、`;` を書き忘れた
  宣言が入れ子の規則として読まれる形（prelude が `ident :` で始まり、colon の次が空白か、ident・関数・colon
  以外。`a:hover`、`a::marker`、`b:not(.c)` のようなセレクタは除く。確認の A）、トップレベルの prelude の中の `;`・`}`、
  置換文字（NUL、不正な UTF-8。WHATWG の UTF-8 の復号と同じく、不正な列の最大の部分ごとに U+FFFD 1 つ）、
  UTF-16 の BOM と UTF-8 以外の `@charset`、入れ子の上限。先頭の UTF-8 の BOM は取り除く（オフセットは元の
  まま）。重さと Provider での扱いは Phase 6 で決める。
- 宣言の範囲は名前から値の最後の空白でないトークンまで（末尾の空白とコメントを含めない）。

## D25. CSS Provider の設計（Phase 6）

Parser は D24（`docs/scripts/css-parser-eval/syntax.go`・`walk.go` を `internal/languages/css` に移す）。
言語は `css`、拡張子は `.css`。CSS Modules の意味（クラスの局所化、`composes`）は、ファイル名が `.module.css`
で終わるファイルにだけ当てはめる（css-loader・Vite・Next.js の規約。設定は読まない）。tree-sitter の grammar
は持たない。

**Symbol**（Kind はすべて新設。Qualified の `<FileID>` の後ろは空白で区切る: 名前に空白を含みうるので、空白の
後ろの全体を名前とする）

| 対象 | Kind | Name / Qualified | 範囲 |
|---|---|---|---|
| スタイルシート（1 ファイルに 1 つ） | `stylesheet` | ファイル名 / FileID | ファイル全体 |
| `@keyframes` の名前（ident か文字列） | `keyframes` | 名前 / `<FileID> @keyframes <名前>` | at-rule 全体 |
| `@font-face` の `font-family` | `font_face` | ファミリ名（Unicode の小文字化。仕様の Default Caseless Matching の近似で、一致を逃す方向にしか違わない） / `<FileID> @font-face <名前>` | at-rule 全体 |
| `@property --x` | `property_registration` | `--x` / `<FileID> @property --x` | at-rule 全体 |
| カスタムプロパティの宣言 `--x: …` | `custom_property` | `--x` / `<FileID> --x` | 宣言 |
| CSS Modules の局所クラス（`.module.css`） | `css_class` | クラス名 / `<FileID> .<名前>` | 最初に現れたクラスセレクタ（`.` から）。1 つの名前に 1 つ |

- 名前はエスケープを解いた値（`.\31 23` は `123`）。範囲は元のバイト。
- `@keyframes` の名前が `none` や CSS 全体のキーワード（`initial` など）なら Symbol にしない（無効）。
- 同じ Qualified の宣言が複数あれば、序数で別の Symbol（既存の規則）。参照は Candidate。

**Reference**（ファイルはすべて `IdentityOnly`、参照は `IdentityInRepository`。Container はスタイルシート）

| 参照 | 例 | Kind | NameQualified | Confidence の上限 |
|---|---|---|---|---|
| アニメーションの名前 | `animation-name: spin`、`animation: 1s spin`（キーワードでない ident か文字列がちょうど 1 つ） | `value_reference` | `<同じファイル> @keyframes spin` | Strong（同じ名前の `@keyframes` を、同じ文書の別のスタイルシートが後で定義すれば、そちらが使われる） |
| フォントファミリ | `font-family: "Inter Var", system-ui`（総称ファミリと CSS 全体のキーワードを除く各要素） | `value_reference` | `<同じファイル> @font-face <小文字>` | Strong（同上） |
| カスタムプロパティ | `var(--x)`、`var(--x, fallback)` | `value_reference` | `<同じファイル> --x` | Candidate（値は DOM の継承で決まる） |
| `composes`（`.module.css`） | `composes: a b;` | `value_reference` | `<同じファイル> .a` | Exact（局所クラスは名前に 1 つ） |
| 同上、他のファイル | `composes: a from "./b.module.css";` | `value_reference` | `<b のパス> .a` | Strong（パスの解決はバンドラ次第） |
| `@import` | `@import "b.css";`、`@import url(./b.css) layer(x);` | `resource_link` | 相対パスで解決した `.css` の FileID | Strong |

- 参照にしないもの: 他のファイルの `@keyframes` / `@font-face` / カスタムプロパティ（どのスタイルシートが同じ
  文書に読まれるかは CSS が決めない）、`url()` の画像・フォント（Phase 7）、ルート相対・絶対 URL、`composes:
  x from global`、`@value`、`font` ショートハンドの中のファミリ、エスケープや `var()` を含んで静的に決まらない値、
  テンプレート構文（`{{`、`{%`、`<%`、`<?`、`${`）を含む値。
- CSS Modules: クラスセレクタ `.x` は局所。`:global(…)` の中、および `:global`（括弧なし）から同じセレクタの
  終わり（`,` か prelude の終わり）までは大域で、Symbol にしない。`:local(…)` の中は局所。`.module.css` 以外の
  ファイルのクラスは Symbol にしない（大域の名前で、宣言の地点が無い）。

**Diagnostic**（walker の問題を写す）: `css_bad_string`・`css_bad_url`・`css_rule_from_declaration`・
`css_stray_in_prelude`・`css_encoding`（UTF-16 の BOM）は error（書かれた内容が失われる、または読めない）。
`css_invalid`（規則にならない prelude。IE のハックなど現実の CSS にもある）、`css_eof_in_block`・
`css_eof_in_comment`・`css_eof_in_string`・`css_eof_in_url`（回復で失うものは無い）、`css_replacement_character`、
`css_charset`（UTF-8 以外の `@charset`）、`css_too_deep` は warning。

**Graph**: `value_reference` → `references`、`resource_link` → `links`。

**安全性**（Phase 5 の受け入れ条件）: 入れ子の上限 256、最悪の入力（`run.sh` の 8 種）の時間をテストで
固定、fuzz（panic なし、範囲、決定性、上限）。SupportLevel は Graph、CacheVersion は `css-1`。

## D26. CSS Provider の実装の細部（Phase 6）

- 読み取り部（`internal/languages/css/reader.go`）は、ハーネスの walker と同じ手順で、値の読み飛ばしを括弧の
  対応の表だけで行う（再帰は規則のブロックの入れ子だけで、256 段が上限）。ハーネスと同じ構造を出すことを、
  差分テスト（`TestHarnessAgreement`、opt-in）で確かめる。
- 空の値の宣言（`--x:;`）の範囲は colon まで。`b: {} !important` の範囲は `!important` まで。
- `@keyframes` はベンダー接頭辞（`-webkit-` など）も同じ名前空間（同じ名前の接頭辞つきと接頭辞なしは 2 つの
  宣言になり、参照は Candidate）。
- `@font-face` の名前は最後の有効な `font-family` 記述子（ファミリが 1 つ）。`src` の無い `@font-face` は名前を出さない（D27）。
- HTML と CSS が使う相対 URL の解決は `internal/languages/internal/weburl` に置く（HTML の振る舞いは同じ）。

## D27. Phase 6 のレビューの修正と既知の制限（Reviewer B・C）

- **CSS Modules の `:global` の範囲**（B-1）: postcss-modules-local-by-default と同じく、引数を持つ疑似クラス
  （`:is()`・`:where()`・`:not()` など）の中は、その位置のモードで始まり、中での変化は外へ漏れない。`,` は
  その括弧の入口のモードに戻す。
- **`!important`**（B-2）: 値の末尾の `!important` を除いてから名前を取り出す（`animation`、`animation-name`、
  `font-family`）。
- **`@font-face`**（B-3）: 最後の有効な `font-family` 記述子が名前（CSS Fonts 4 §4）。`src` の無い規則は
  考慮しない。
- **`@import` の位置**（B-4）: トップレベルで、`@charset`・`@layer` の文・ほかの `@import` 以外の規則より前に
  あるものだけ（CSS Cascade 5 §2）。
- **スタイル規則の中の at-rule**（B-5）: `@keyframes`・`@font-face`・`@property` を、祖先にスタイル規則が
  あるときは Symbol にしない（CSS Nesting 1 が許すのは条件付きグループ規則・`@layer`・`@scope`）。
- **診断**（B-7）: 置換文字は元のバイトの NUL と不正な UTF-8 だけ（書かれた U+FFFD とエスケープは除く）。
  `@charset` は Encoding 標準の UTF-8 のラベルの表で判定する。
- **範囲**（B-8）: 引用符なしの複数語のファミリの参照は、最初の語から最後の語まで。
- **repomap**（C-2）: CSS と HTML の Provider が `language.MapHinter` を実装する（スタイルシートと文書は
  Built、`@keyframes`・`@font-face`・`@property`・CSS Modules のクラスは Exposed、カスタムプロパティと id の
  要素は Detail）。パッケージの大きさの重みで、カスタムプロパティと要素は変数・定数と同じ 1。
- **secrets-scan**（C-1）: CSS もコードに数えない（HTML と同じ）。
- **既知の制限**:
  - MCP のターゲットの指定で、空白を含む Qualified の中の単語の末尾一致（`symbol: "var"` が
    `… @font-face inter var` に一意に解決される）。曖昧なときは拒否する（C-3）。
  - ファイル名に空白を含むと、別の Symbol と Qualified が衝突しうる（誤った Exact / Strong にはならず
    Candidate。C-4）。
  - CR だけの改行では行番号が進まない（他の Provider と同じく `\n` で数える。B-8）。
  - `--` で始まる `@keyframes` の名前と timeline、`-webkit-animation` の扱い、大域のセレクタの中の
    `composes`（B-9）。
- **合格基準の範囲の変更**（B-9）: P6-1 は参照に `url()` を挙げるが、画像・フォントの `url()` はリポジトリの
  索引の外のファイルへのリソース関係で、Phase 7（Web 横断のリソース関係）で扱う。Phase 6 で `url()` を扱うのは
  `@import` の中だけ。基準を下げるのではなく、扱うフェーズを移す（Phase 7 の期待表に入れる）。

## D28. Web 横断のリソース関係（Phase 7）

監査の設計（Resource Graph と Semantic Graph の分離、限定的な例外）を実装する。

- **パスの証拠だけが名前空間をまたぐ**: `ReferenceDraft.FileIdentity`（新設）は、NameQualified がリポジトリの
  ファイルのパスであることを示す。resolver は言語を問わない索引（`fileDecls`）で、そのファイル自身を宣言する
  Symbol（`symbol.DeclaresFile` の種類: 文書・スタイルシート・スクリプトで、Qualified が自分の FileID）だけを
  引く。名前による段（`nameKey`）と R0（言語ごと）は変えない。種類を限るのは、`Foo.js` のメソッド `js`
  （Qualified `Foo.js`）のような偶然の一致をファイルの宣言にしないため。
- **JavaScript のファイルの Symbol**: JS の Provider（共有の抽出器の JS 側）がファイルごとに `script`（新設の
  種類、Name はファイル名、Qualified は FileID）を出す（Exported は false。名前の索引には載らない。D30）。
  golden の差はこの Symbol だけ。TypeScript は出さない（ブラウザが `<script
  src>` で読むのは JavaScript）。CacheVersion `js-7`。
- **HTML のリンク**: `<link rel="stylesheet" href>` → `.css`（`stylesheet`）、`<script src>`（`type` が無いか
  `module` か JavaScript の MIME 型）・`<link rel="modulepreload" href>` → `.js` / `.mjs`（`script`）。相対
  パスだけ、`<base>` の文書は出さない、Strong が上限（URL とファイルの対応は配信の方式次第）。`resource_link`
  → `links`。CacheVersion `html-3`。
- **改訂したテスト**（削除しない）: `TestNameSpaces_NoRelationJoinsTwoNameSpaces` は、名前空間をまたぐ関係を
  「`links` の edge（または `resource_link` の候補）で、相手がファイル自身の宣言」に限って許し、その数が
  fixture のパスの証拠の数（2）と一致することを確かめる。`TestMCPNameSpaces_NoCrossLanguageRelations` と
  `TestUnresolved_CrossLanguage` に HTML と CSS を加え、名前（`GoOnly`、`jsRun`）では関係ができないことと、
  `<script src>` がスクリプトへの `linked_from` になること、存在しないスクリプトが Unresolved（Outside でない）
  になることを確かめる。従来の保証（名前による関係は名前空間をまたがない）はそのまま。
- **扱わないもの**（Phase 7 の範囲の判断）:
  - 画像・フォント・その他の索引にないファイル（`<img src>`、CSS の `url()`、`@font-face` の `src`）への
    リソース関係。Ark の index は Provider のある言語のファイルだけを持ち、ファイルそのものの節点が無い。
    索引の外のファイルの存在を参照の解決に使うには、index の入力（指紋・cache）にそれらを加える設計の変更が
    要る。P6-1 から移した `url()` を含め、未実装として記録する（P7-1 のうち画像・フォント・CSS → 画像・
    フォントは未達）。
  - 「ありうる一致」の層（HTML の id ↔ JS の `getElementById`、HTML の class ↔ CSS のセレクタ）。名前による
    Strong を出さないこと（P7-2）は、名前の段を変えないことで満たす。ありうる一致を別の出力として示す層は
    実装しない（後続の課題）。

## D29. Phase 7 のレビューの修正（Reviewer B: HTML のリンク）

ブラウザが取得するファイルにだけリンクを出す（HTML Living Standard）。

- `<script src>`（"prepare the script element" の 8〜10）: type 属性、それが無ければ空でない `language` の
  `"text/" + language` を type string とし、空か無ければ JavaScript。ASCII の空白（TAB, LF, FF, CR, SPACE）
  だけを除いた値が JavaScript の MIME type の essence（大文字小文字を区別しない）なら classic、空白を除かない
  値が `module` なら module、それ以外は取得しない。`nomodule` は旧いブラウザが取得するので出す。
- `<link rel=stylesheet>`: `disabled` があれば取得しない。`type` があれば `text/css`（ASCII の空白を除き、大文字
  小文字を区別しない）のときだけ。空の `type` も取得しないとする（仕様。Chrome は空を CSS として扱うが、出さない
  のは Recall の損失だけ）。
- `<link rel=modulepreload>`: `as` が無いか script-like の destination のときだけ。
- 判断に使う属性（`type`、`language`、`rel`、`as`、`disabled`）が、ありながら木で検証されないとき（NUL、
  重複）は、リンクを出さない。
- `<template>` の中のリンクは Strong のまま（クローンされると取得され、URL はクローン先の文書にほぼ依存しない）。
- Recall の損失として残るもの: `preload as=script`、`.cjs`・`.ts`、SVG の `<script href>`、`<noscript>` の中
  （scripting が有効な読み方）、`<template>` の中の `<base>` が文書全体のリンクを止めること。

## D30. Phase 7 のレビューの修正（Reviewer C）

ファイル自身の宣言（`symbol.DeclaresFile` の種類: 文書・スタイルシート・スクリプト）は、パスの証拠
（`FileIdentity`）からしか引かない。

- **名前の段から外す**（C-3）: resolver の名前の索引（`byName`・`members`・`suffixes`・`dirSymbols`）、
  同じファイルの段、モジュールのエクスポートの探索、completeness の名前の一致から除く。JS の `import 'b.js'`・
  `require('b.js')` の指定子の文字列が、同じディレクトリの段で `b.js` の script を候補にしていた。
- **Container と Parent**（C-1）: 同じ Qualified の候補にファイル自身の宣言とそれ以外があれば、それ以外を選ぶ
  （ルート直下の `app.js` のオブジェクト `app` のメソッド `js` は Qualified が `app.js`。script がファイル全体を
  覆うので範囲で絞れず、メソッドからの edge が消えていた）。ほかの候補が無ければファイル自身の宣言（HTML・CSS の
  参照の Container は文書・スタイルシート）。
- **repomap**（C-2）: JS の script は `Exported: false`（既定の地図の候補にしない）。
- 回帰テスト: `internal/languages/javascript/script_test.go`。
- **範囲の注記**: `analyze_change_impact` で JS の関数を変えても、それを読む HTML の文書は影響範囲に入らない
  （script とその中の宣言の間に関係を作らない）。`<template>` の中のリンクは Strong のまま（D29）。

## D31. Phase 8 の設計（CSS Modules の binding と埋め込み領域）

**前段の状態**: Phase 7 は PARTIAL（画像・フォントのリンクが未実装）。Phase 8 が使うのは、実装済みで基準を
満たしたパスの証拠の仕組み（`FileIdentity`）だけで、未実装の部分に依存しない。よって Phase 8 に進む。

**CSS Modules の binding**（JS・TS・TSX。抽出器を共有するので両方）
- 対象: 相対の指定子（`relativeTarget`）で `.module.css` で終わる import。既定の import と名前空間の import
  （css-loader の `namedExport` の有無でどちらも使う）の局所名 `s` について、`s.x`・`s?.x`・`s['x']`
  （エスケープの無い文字列）のうち、呼び出しの callee でないもの。名前付きの import `{ x }`・`{ x as y }` は
  指定子の位置に 1 つ（Vite・css-loader の named export）。`import type` は出さない。
- `s` がローカルに隠されていれば出さない（D2 の locals）。import は再代入できない。
- 参照: Kind `value_reference`、NameQualified `<解決したパス> .x`、`FileIdentity`、`IdentityInRepository`、
  TargetKinds `css_class`、ConfidenceCap `strong`（バンドラの `exportLocalsConvention` などの設定で、書いた
  名前のままのクラスが無い場合がある）。
- resolver: パスの証拠の索引（`fileDecls`）に、ファイル自身の宣言に加えて「ファイルに属する宣言」
  （`symbol.FileScoped`: CSS Modules の局所クラス。Qualified が `<自分の FileID> ` で始まる）を入れる。
  名前の索引は変えない（CSS の `composes` は今までどおり）。
- 出さないもの: `require('./a.module.css')`、camelCase 変換を仮定した名前の対応、`.module.scss` など。

**埋め込み領域**（HTML の Provider）
- 対象の要素: 木の検証を通った HTML の名前空間の `<style>`・`<script>`。`<template>` の中は対象外（不活性）。
  木の要素のテキスト（子が 1 つのテキストか、子が無い）が、tokenizer の範囲の内容（CR LF と CR を LF に
  正規化）と一致すること。NUL を含む内容は対象外（木が U+FFFD に置き換える）。先頭が BOM の内容も対象外。
- style: `type` が無いか空か `text/css`（ASCII の大文字小文字を区別しない）。CSS Provider の抽出を内容に当て、
  位置を写す。スタイルシートの Symbol と CSS Modules のクラスは出さない（文書のスタイル）。`@import` は
  文書から相対で解決し、`FileIdentity` を付ける（言語をまたぐパスの証拠）。`<base>` のある文書では出さない。
- script: `src` が無く、`loadsJavaScript` が真（classic か module）。JS の抽出器（script の Symbol なし）を
  内容に当てる。Symbol の Qualified・Parent に前置（classic は文書で 1 つの大域、module は script ごと）。
  参照: receiver の無い名前の参照だけを出し、NameQualified に前置した名前、`IdentityInRepository`。
  module では、その script の宣言ならその script の前置、import の局所名なら（解決しない）その script の前置、
  それ以外は大域（classic）の前置。上限は Strong（同じ大域に外部の script も書く。実行順）。receiver の
  ある参照・import の binding・export は出さない（文書の大域と外部の script の関係を表せない）。
  `Exported` は false。
- 位置: 内容の先頭のホストの位置 (L0, C0) に対して、内容の 1 行目は列を C0-1 だけずらし、2 行目以降は行を
  L0-1 だけずらす（列はバイト、行は `\n`。全 Provider と同じ）。バイトの範囲は内容の開始を足す。
- CacheVersion `html-4`。

## D32. Phase 8 のレビューの修正（Reviewer A・B・C）

**合格基準の改訂（P8-1、A8-6）**: 変更前は「名前付き import `{ x }` は指定子の位置に 1 つ」。変更後は「束縛した
名前の値としての使用ごとに 1 つ」。理由: トップレベルの参照（指定子）は、JS の既存の扱いでどの宣言からの
関係にもならず、グラフに現れない（観測）。使用の位置で出せば、使う関数からの関係になる。影響: 使用の位置の
判定が誤ると誤った参照になる（A8-1・A8-3 で実際に起きた）ので、次の許可リストで固めた。基準を下げる変更では
なく、未達の項目は無い。

**値の使用（`valueUse`、A8-1・A8-3・A8-7）**: 参照を出すのは、式が値として読まれる位置だけ。親が引数・配列・
二項演算・括弧・三項演算・JSX の式・テンプレートの置換・spread・await・return・式文・カンマ式・yield・throw・
non-null・オブジェクト（shorthand）、`delete` 以外の単項演算、`as` / `satisfies` の式、宣言子・pair・クラスの
フィールド・既定値・`case` の値、代入の右辺、arrow の本体、メンバーアクセスの object と添字。それ以外（呼び出しの
callee、`new` の constructor、代入先、`delete`・`++`、パラメータ、キー、型の中の名前、JSX のタグ名・属性名）は
出さない。一覧に無い構文も出さない。メンバー（`s.x`）自身にも同じ判定を当てる。

**その他の JS / TS（Reviewer A）**
- A8-2: オブジェクトリテラルのメソッドの本体を `enterFunction` で歩く（引数とローカルがスコープに入る）。既存の
  欠陥で、`{ m(f) { return f() } }` がトップレベルの `f` を呼ぶことになっていた。JS / TS の採点は変わらない。
- A8-4: import attributes（`with { type: 'css' }`）の import は束縛しない（CSS module script の値はスタイル
  シートのオブジェクト）。
- A8-5: 名前空間の import の `ns.default` はクラスではない（既定の import の `s.default` はクラス `default`）。
- A8-8: `s['x-y']` の範囲は引用符を除いた文字列。

**HTML の埋め込み（Reviewer B）**
- B8-1: 入力の終わりで閉じた `<script>` は実行されない（"text" insertion mode の EOF で already started、
  prepare the script element の手順 2）。`tokenize` が raw text の後の終了タグを記録し（`tag.closed`）、閉じた
  script だけを読む。style は入力の終わりで閉じても適用されるので読む。
- B8-2・B8-3: classic の `nomodule`、`for` と `event` が `window` / `onload`・`onload()` 以外の classic script は
  実行されない（同 手順 13・17）。検証されない属性は判断に使わない（読まない）。
- B8-4: `<style>` ごとに文書全体の行の表を作っていた（O(要素数 × 文書)、4 万個で 39 秒）。CSS の
  `ExtractEmbedded` は内容の位置で返し、HTML が JS と同じ `shift` で写す（2 万個で 1 秒前後、テストで固定）。
- B8-6: module script のトップレベルの束縛のうち Symbol にならないもの（分割代入など、`ProgramLocals`）は
  その script の名前（Unresolved）で、大域ではない。
- B8-5: テストの欠落（JS の診断の位置、遮蔽された参照の Candidate、import の名前、module の入れ子の Parent、
  埋め込みの CSS Modules の参照）を補い、ミューテーションで確かめた。

**共通（Reviewer C）**
- C8-1: MCP のターゲットの解決で、`filePattern` を段ごとに当てる（Qualified の一致が他のファイルにしか無ければ、
  名前の段に進む）。`filePattern` の無い呼び出しは変わらない。
- C8-2: `get_symbol` は Provider の Qualified でも選べ（`syntax.Symbol.Qualified`、JSON には出さない）、
  曖昧なときは、`Owner.name` が他の候補と重なる候補に Qualified を示す。
- C8-3・B8-8: HTML の CacheVersion は `html-4+<CSS の版>+<JS の版>`（埋め込みを読む抽出器の変更で HTML の cache
  も無効になる）。

**既知の制限として記録するもの**
- 曖昧なターゲットの一覧（`filePattern` なし）は、Qualified の一致があれば名前の一致を挙げない（既存の契約。
  HTML の宣言は `filePattern` か Qualified で選ぶ）（C8-1 の残り）。
- ファイル名とクラス名から作る Qualified が別のファイルの Qualified と形の上で重なりうる（`q/a.module.css .c.css`
  というファイル）。TargetKinds で絞るので誤った Exact / Strong にはならない（C8-4）。
- CSS の `TestWorstCases` の時間の上限は、並列の負荷で超えうる（C8-5。Phase 8 の変更とは無関係）。
- 読まないもの（取りこぼし、誤りではない）: `type=" module"`（Phase 7 の F1 で前後の空白を除かない比較にした。
  B8-7 は除くと指摘しており、両者が食い違うので、安全側の現状を保つ）、`<script/>` の後の raw text、SVG の
  `<script>`・`<style>`、宣言的 shadow root の中、`style` 属性、イベントハンドラ属性。
- 埋め込みの CSS の診断の文言（「end of the file」など）は単独のスタイルシートのまま（B8-8）。
- CSS Modules の取りこぼし: 分割代入、`{ default as s }`、`require`・`import()`、トップレベルの const
  オブジェクトの shorthand（A8-10）。

**再レビュー（Reviewer A2）**: 前回の反例はすべて解消（観測）。A8R-1（括弧・non-null で包んだ代入先・`++`・
`delete`・callee が使用になる）を修正: `valueUse` は括弧と non-null の親を外へたどってから判定する。A8R-2 の
うち、計算されたキー（classnames の `{ [s.active]: on }`、頻出）・条件（`if`・`while`・`do`・`for`・`switch`）・
`for … in` の右辺・分割代入の既定値を値の使用に加えた（正例のテスト `pos/k.ts`）。`export default s.x` と
`for … of` は出さない（取りこぼし）。

**再レビュー（Reviewer B2・C2）**: 両者とも前回の指摘の解消を観測。B2 は前回の B8-7（`type=" module"`）を撤回した
（現行の prepare the script element の手順は、JavaScript の MIME 型の判定だけ前後の空白を除き、`module` の
比較は除かない。Phase 7 の F1 の実装が正しい）。追加の修正: R8-1・R8-2（埋め込みの CSS の診断の位置と
`shift` の位置の無い場合をテストで固定）、R8-3（条文を番号でなく文言で引く）、C8-8（`get_symbol` の再試行名は、
Qualified が `Owner.name` より候補を絞るなら Qualified）、C8-9（パスの証拠の解決は TargetKinds で、ファイル
自身の宣言とファイルに属する宣言のどちらか一方だけを引く。`q/a.module.css .c.css` というファイルはクラス
`c.css` の候補にならず、Candidate が名前空間をまたぐことも無い）、C8-10（文書）。C8-4 は C8-9 の修正で
解消した（既知の制限から外す）。

## D33. Phase 9（最終の独立統合監査）の修正

**JS / TS（監査 A）**
- F9A-1（高、Phase 2 の誤った Strong）: import / require の binding の先がコンストラクタ関数のとき、その
  prototype のメンバーを receiver のメンバーとして引かない（`$.extend()` は jQuery の static な
  `jQuery.extend` で、prototype の `extend` ではない。関数自身のプロパティ `F.m = …` は表さない）。
- F9A-2（中）: CommonJS のプロパティへの代入の名前を、右辺の形を問わずすべて数える。置き換えのオブジェクトの
  名前をプロパティの代入でも書く、または export に表れない値で書く名前の export は出さない。識別子の値で
  2 回書けば 2 つの export（Candidate）のまま。
- F9A-3（中、TS は main から）: クラスのメンバーに置き場所（`SymbolDraft.Placement`: static / instance。
  prototype のメンバーは instance）を記録し、resolver が receiver の届く方だけを引く。メンバーの探索
  （`memberResolution`・継承）では、型の証明された receiver（`this`・型の付いた変数・`new` の変数）は
  インスタンス、それ以外（型の import、型の名前そのもの）は型そのもの。名前の段の候補には後処理
  （`restrictPlacement`）を当て、static は型の証明された receiver から外し、instance は receiver が
  そのメンバーの型の名前そのもの（隠されていない）のときだけ外す。型の不明な receiver（`repo: any`）は
  両方を残す。後処理は確信度を上げない。採点: three の Recall 7394→7399、rxjs 1715→1722（Candidate から
  正解へ 12 件。誤り 0）。D14 の「同名の static とインスタンスは Candidate」は、receiver で区別できる場合は
  解消した。
- F9A-4（中、main から）: classic script（import / export / CommonJS の無い JS / TS のファイル）の自由な名前は、
  他の module のファイルの宣言を候補にしない（module のトップレベルは大域でない）。
- CacheVersion: `ts-10`、`js-9`。JS の版は共有の抽出器の TS の版を含む（`js-9+ts-10`、F9C-4）。

**HTML / CSS（監査 B）**
- F9B-2（中）: 宣言的 shadow root の判定（ホストの部分木をたどる）を template ごとにメモ化（参照ごとに
  たどっていたので二次）。比のテストで固定。
- F9B-3（中、誤った Strong）: `@import` の後の `@layer` 文は、以後の `@import` を無効にする（CSS Cascade 5
  §2、Blink も同じ）。F9B-4（低）: ブロック付きの `@import` は出さない。CacheVersion `css-2`。
- F9B-5（低）: `language` 属性から作る型の文字列は前後の空白を除かない（除くのは `type` 属性の値だけ）。
- F9B-6（低）: 木の途切れの検査（frameset の文書）と BOM の拒否をそれぞれ単独のテストで固定。
- F9B-1・F9B-7（台帳）: verification.md の Phase 3 と Phase 6 に注記。

**横断・文書（監査 C）**
- F9C-1・F9C-2・F9C-3: ARCHITECTURE.md §7、resolution-model.md、README（英日）、conformance の
  IMPROVEMENTS.md の古い記述を実装に合わせた。F9C-5: MCP のターゲットの規則を mcp-tools.md に明記。
  F9C-6: verification.md の Phase 9 にフェーズごとの判定表。F9C-7: P7-1 の期待表の書き分け（基準は不変）。
- F9C-8: 監査中に見えた未コミットの変更は、監査 A の指摘への修正（この D33）。監査の後に全体の検証を
  やり直した（verification.md の Phase 9）。
- 監査 C の再確認（P9C-R2）: `symbol.Symbol.Placement` は `search_code` の `format: json` に現れる（追加だけ、
  `omitempty` で他の言語には出ない）。`search_code` は Symbol のフィールドをそのまま出す（`Visibility` などと
  同じ扱い）ので `json:"-"` にはせず、追加のフィールドとして mcp-tools.md に明記した（cache が保存するのは
  `SymbolDraft` で、その `Placement` は JSON に残す必要がある）。

## D34. 資源のノード（Hardening Phase B、Phase 7 の残り）

設計の比較と詳細は `resource-architecture.md`。Hybrid を採る。

- **資源**: Provider の拡張子でなく、資源の拡張子の表（画像・フォント・音声・動画・字幕、`internal/asset`）に
  ある通常ファイル。索引の歩き方（`sourceStep`）を共有し、同じ規則で除外する。**内容は読まない。**
  シンボリックリンクは資源にしない（読まずに行き先が分からない）。
- **種類の名前**: `resource` は Terraform の SymbolKind が既に使っているので、種類・言語・パッケージとも
  `asset` にした（`resource` にしていたら Terraform の宣言を「ファイル自身の宣言」と誤認していた）。
- **identity**: 種類 `asset`、言語 `asset`（Provider の言語ではない名前空間）、Qualified はリポジトリ相対の
  パス、ID は `NewDeclarationID("asset", path, asset, path, 1)`。内容に依らない。
- **置き場所**: 索引の資源の集合（`RepositoryIndex.Assets()`）。`Files()`・`SymbolsByFile`・統計・名前の
  索引には入らない。`GetSymbol`・`FindSymbolsByQualified`・`SymbolsByKind` は返す。resolver には資源ごとの
  `FileIndex`（`IdentityOnly`）を渡し、`fileDecls`（`DeclaresFile` に `asset` を加えた）が引く。
- **指紋**: 資源のパス（存在）を足す（`asset "<path>"`）。内容は足さない。追加・削除・移動で MCP の索引が
  作り直される（`TestMCP_AssetAddedInvalidatesIndex`、指紋から外すミューテーションで失敗することを確認）。
- **context**: 種類 `asset` はソースを読まず、`(<種類> asset <path>)` と示す（バイナリを文脈に出さない）。
- **参照**: HTML の `img src`・`srcset`、`source`（`video` / `audio` の `src`、`picture` の `srcset`）、
  `video poster` / `src`、`audio src`、`track src`、`input type=image src`、`link rel=icon` /
  `apple-touch-icon` / `preload`（`as` が image・font・audio・video・track）、SVG の `image` / `use` の
  `href`。CSS の宣言の `url()`・`src()`・`image-set()` の文字列（カスタムプロパティの値は除く: 置換の
  ところで解決される）。`resource_link`、`FileIdentity`、`IdentityInRepository`、TargetKinds `asset`、
  上限 Strong。`<base>` の文書・テンプレート構文・スキーム付き・ルート相対・プロトコル相対は出さない。
- **CacheVersion**: `css-3`、`html-5`（HTML の合成の版は `html-5+css-3+js-9+ts-10`）。
- これで Phase 7 の P7-1 の未達（HTML → 画像、CSS → 画像・フォント）を満たす。
- **レビュー（Reviewer A、Hardening）の対応**: HB-1（srcset の候補が属性値全体の範囲に落ちると、同じファイル名で
  参照の ID が衝突して 1 つが捨てられる）→ その場合は参照の名前をパスにして分ける。HB-2（参照されない資源にも
  節点と FileIndex を作るので、資源 10 万個で割り当て 641MB・保持 134MB）→ 節点は参照が名指す資源にだけ作る
  （`materializeAssets`。同じ入力で 33MB・1MB）。HB-3（`rel="icon stylesheet"` でスタイルシートの関係が消える
  回帰）→ `<link>` は資源の後も rel の分岐を読む。HB-4（ID の衝突の報告に資源が出ない）→ 資源も走査。HB-5
  （設計文書の古い名前）→ 直した。HB-6（資源の行が 0）→ 仕様として文書に明記。

## D35. 値の読み取りの参照（Hardening Phase C）

- 分析（`js-recall-analysis.md` C1。アーカイブ）: JS の取りこぼしの 9 割以上は、値としての識別子の読み取りに参照が
  無いこと（呼び出し・構築・JSX だけを出していた）。
- 規則（`typescript/reads.go`）: Go と同じく `value_reference`（edge: `references`）。語彙的な証拠のある名前
  （トップレベルの宣言・import / require の束縛）だけ、値の位置（`valueUse`）だけ、ローカルに隠された名前は
  出さない、`export default X`・`export { X }` を含む。CSS Modules の束縛の名前は除く（cssmodules.go）。
- 昇格は無い: 解決は既存の規則のまま。FS・FO 0 を 7 コーパスと独立の 3 コーパスで確認。
- 影響: JS / TS の参照と `references` の edge が増える（golden の ts_semantics、TS の graph テストの期待を
  更新。すべて正しい関係: `User.create()` の対象の `User` など）。
- CacheVersion: ts-11、js-10（HTML の合成の版も変わる）。

## D36. 既知の制限 4 項目（Hardening Phase E）

| 項目 | 判断 | 理由・再開条件 |
|---|---|---|
| SVG の `<script>` / `<style>` | **実装**（html-6） | SVG の中は raw text でなく、文字参照と CDATA が読まれる。木のテキストが原文と完全に一致するとき（文字参照・CDATA・コメントを含まない）だけ読むので、範囲の写像は HTML と同じく 1 対 1。SVG の script は classic として実行され、`href` / `xlink:href` があればその内容を読む（テキストを実行しない）ので除く。`type=module` は SVG で確かめられないので除く。テスト `TestEmbedded_SVG`。 |
| 宣言的 Shadow DOM の中のインライン領域 | **実装しない** | shadow root の中のスタイルの名前（`@keyframes` など）は、CSS Scoping の規則で自分の木から host の木へとたどって解決される。Ark の CSS の identity はファイル（文書）ごとで、木の連鎖を表せない。正確なスコープだけにすると、正しい参照を Unresolved にし、文書のスコープに入れると別の木の名前と誤って結ぶ。（Phase F の Reviewer C の訂正: shadow root の中の classic script は、通常の文書の解析では実行され、大域は Window で共有される — HTML の parsing の "Attach a shadow root"、scripting の "If el is not connected"、DOM の shadow-including root。script は文書の大域の identity で読めるが、現状は template の中として読まない。偽の参照は出ない。CSS Scoping は CSS Shadow Module Level 1 に改名されている。）再開の条件: スコープの連鎖（shadow root → host の木）を持つ CSS の identity と、その解決規則の設計。script は別に判断する。 |
| CSS Modules の分割代入・`require` | 分割代入は**実装**（ts-12・js-11）、`require` は**実装しない** | `const { btn, "x-y": xy } = s` はクラスの写像のプロパティを読むので、キーの位置で参照を出す（計算されたキー、rest は除く。名前空間の import の `default` は写像なのでクラスではない）。`require('./a.module.css')` は、css-loader の既定（`esModule: true`）ではモジュールの名前空間を返し、`.btn` は undefined。設定次第で意味が変わり、静的に証明できない。再開の条件: バンドラの設定を読む仕組み。 |
| `export default s.x` | **実装** | `valueUse` の許可リストに `export default` の値を加えた（CSS Modules のクラスと、値の読み取りの両方）。 |


## D37. Phase F の最終レビューの修正と未解決の事項（Hardening）

**Reviewer A（Resource Graph）: GREEN。** F6A-1（参照されない資源を指定すると「無い」と同じ応答）→ 索引に資源の
パスの集合を残し（`IsAsset`）、MCP は「どこからもリンクされていない資源」と答える。F6A-2（古い記述）→ 直した。
F6A-3（`Assets()` が内部の配列を返す）→ コピーを返す。F6A-4（`get_file_content` が資源のバイナリを返す。この
ブランチより前から）・F6A-5（Windows の FileID、未検証）・F6A-6（Unicode の正規化の違いは Unresolved）は記録。

**Reviewer B（JavaScript Fidelity）: 条件付き GREEN → 対応。**
- F6B-1（High）: interface / type alias の名前の値としての読み取りが、型の宣言に Exact で結ばれた（`r instanceof
  Response` の `Response` は大域の値）。→ 読み取りは値の種類の宣言（function・class・variable・constant・enum・
  namespace・module）の名前だけを知っている名前とし、参照の TargetKinds も値の種類に限る。
- F6B-2（High）: enum のメンバーの初期化式の中の名前（`B = A + 1` の `A` は `E.A`）が、同名のトップレベル宣言に
  結ばれた。→ enum の本体の中では読み取りを出さない（`scope.enumBody`）。
- F6B-4（Medium）: 読み取りの規則がテストで固定されていなかった。→ `reads_test.go`（引数・ローカル・catch・
  ネストした関数・キー・callee・`with`・型の名前・enum・別の classic script の大域）。各規則を外す
  ミューテーション 6 種をすべて検出。
- F6B-6（Low）: `with` の本体の中の名前はオブジェクトのプロパティでありうる。→ 読み取りを出さず、呼び出しは
  Candidate。直接の `eval` を含む関数の中の名前は記録のみ（非 module のファイルだけの問題、既存の挙動）。
- F6B-3（Medium）: 評価の §6・§10 が求める `unmatched_strong` の報告が無かった。→ `js-recall-analysis.md` に記録（アーカイブ）。
- **F6B-5（Medium）: 未解決（保守担当者の判断が必要）。** 4 つ目の独立コーパス semver 7.6.3 で、自動の
  False Strong（`local_fp`）が変更前 5 → 変更後 16。すべて CommonJS の `module.exports = { a, … }` の shorthand
  プロパティを経由する参照（`semver.coerce(...)` の呼び出し 5 件は変更前から、`const { MAX_LENGTH } =
  require(...)` の読み取り 11 件が新規）。Oracle（TypeScript の checker）はプロパティの宣言で止まり、Ark は
  その値の宣言（関数・定数）を答える。ESM の再エクスポートでは Oracle も別名をたどって最終の宣言を答える
  ので、Oracle の側の扱いの非対称と判断する（実行時の値は Ark の答え）。ただし評価の方法では、人手の分類で
  自動の FS を差し引けない。Oracle と採点の規則の変更、Oracle に合わせて Ark の答えを弱める特殊な分岐は、
  ミッションの禁止事項なので行わない。選択肢: (a) Oracle が shorthand プロパティをその値の宣言までたどるように
  拡張し、変更前の値も同じ定義で取り直す、(b) CommonJS のオブジェクトの export の確信度の方針を決める
  （D11 との整合）。
- F6B-7（Info）: スコープの過大近似による Recall の損失（D2）。

**Reviewer C（CSS と Web 統合）: 条件付き GREEN → 対応。** F6C-1（fuzz の不変条件が `}` で終わる入力を検査
しない）→ ブロックごとに「閉じた `}` があるか、開いたまま報告されたか」を検査（レビュアーのミューテーションを
60 秒で検出。その入力 `{"}` を種として残した）。F6C-2（`len(src)/4` の容量で、トークンの少ない大きな入力の
割り当てが増える）→ 上限 65,536。F6C-3（SVG の `xlink:href`・コメントの負例が無い）→ 追加。F6C-4（`image-set()`
の入れ子の関数の中の文字列、閉じていない入力）→ 入れ子を飛ばし、閉じていない場合も最後のトークンまで読む
（`TestImageSetURLs`）。F6C-5（D36 の script の記述）→ 訂正。F6C-6（SVG の `<title>` の中の style / script を
読まない。既存）・F6C-7（`style` 属性の `url()` など、設計どおりの取りこぼし）は記録。

- CacheVersion: ts-13、js-12、css-4（HTML の合成の版は `html-6+css-4+js-12+ts-13`）。

## D38. F6B-5 の解決: CommonJS の export オブジェクトの参照の意味論と評価契約 v2

> **D39 で一部を置き換え**: 下の「Ark の答えの層」（対象の宣言）と評価契約 v2 は有効である。ただし、CommonJS の
> export を通る参照の Confidence は、Exact ではなく Candidate を上限とする（D39）。

台帳は `f6b5/`（README・cases・semantics・oracle-design・scoring・verification・progress）。

- **Ark の答えの層**: `module.exports = { a, k: b }` を通る参照の対象は、プロパティが持つ値の宣言
  （Semantic Value Target）である。ESM の `export { a }` と同じ扱いになる。semver の 16 件で Ark の答えは
  正しかった。TS の checker がプロパティで止まるのは、binder の形の判定
  （`every(properties, isShorthandPropertyAssignment)`）による非対称である（`semantics.md` B2）。
- **Ark の修正（ts-14）**: 独立レビューで、モジュールが値を書き換えうる形でも Ark が元の宣言を Exact と答える
  誤りが見つかった。後ろの spread・計算キー・同じキー、`Object.assign` などの書き込み、2 回目の置き換え、
  `exports` の流出、トップレベルの `this`、括弧や TS の包む式など（`semantics.md` B3 の追加・追加 2）である。
  そのような名前の export は出さない。安全側の近似（関数の中のローカルの `exports` への書き込みを数える、
  `exports` を関数に渡すと名前つきの export をすべて落とす）は、Recall を失う既知の制限として記録した。
- **評価契約 v2**: v1 の Oracle は変えない。v2（`javascript-tsc-value.js`）は v1 の出力に値の層を足す。
  対象は CommonJS の export オブジェクトのプロパティだけで、書き込み・流出・動的なコード・alias の再代入が
  見えないときに限る。ハーネスは `decl` か `value` のどちらかを正解とする。データの値は閉世界の仮定に依るので、
  `value_tp_data` として別に数える。
- **結果**: 12 コーパス × 3 版で、v2 の FS は 0（semver の v1 の FS 16 は、すべて `tp_value`）。修正の前後で
  12 コーパスの採点は同一（修正が対象とする形がコーパスに無い）。失った正解は 0。
- **判定**: PARTIAL（`f6b5/verification.md` F8。アーカイブ）。F6B-5 は解決した。READY の条件は、最後の修正（関数と
  メソッドの `this` を常に数える規則）の独立の確認と、トランスパイルされた CommonJS での Recall の測定。
- **残課題**: ESM の `export default { … }` を通るメンバーアクセスの Recall。他のモジュールからの書き込み
  （`require('./m').a = x`）を Ark が見ていないこと。トランスパイルされた CommonJS のコーパスでの Recall の測定。

- CacheVersion: ts-14、js-12、css-4（HTML の合成の版は `html-6+css-4+js-12+ts-14`）。

## D39. CommonJS の Confidence の境界（リリース判定の監査の後継）

台帳は `commonjs-boundary/`（本決定）。監査の台帳 `release-gate/` はアーカイブした（`README.md`）。

- **経緯**: リリース判定の監査（`release-gate/`）では、CommonJS の export オブジェクトへの反射的な書き込みを、default-deny の
  構文の規則（ts-15）で封じようとした。独立レビューは D7〜D12 の 6 回すべてが RED で、D13 は途中で止めた。
  静的なモデルの内側でも、毎回、新しい経路が見つかった。ユーザーの判断で、この方式は打ち切った。
- **決定**: CommonJS の export 値の同一性を、Exact / Strong の保証の対象から外す。
  - provider が CommonJS の export に `ExportDraft.Mutable` の印を付け、resolver はその印を通った解決を Candidate を上限にする。
    動的な変更を検出するかどうかには依らない。
  - 字句の束縛と、ESM / TypeScript の export は、従来どおり Exact とする。
  - 監査の層は外し、抽出は 8f8f249（ts-14）の規則に戻した。CacheVersion は ts-16 にした。
- **結果**:
  - 誤った Exact / Strong は 0 のまま。候補を含む発見率は、20 コーパスのすべてで変わらない。
  - `unsafe_strong` は 290 件から 0 件になった。
  - 高 Confidence の Recall は、CommonJS の多いコーパスで下がる（semver 75.7%→51.1% ほか）。正解は Candidate として残る。
  - 独立レビューは 2 ラウンドとも GREEN。
- **判定**: READY（`commonjs-boundary/decision.md`）。リリースの操作は、ユーザーの承認を待つ。
- CacheVersion: ts-16、js-12、css-4（HTML の合成の版は `html-6+css-4+js-12+ts-16`）。
