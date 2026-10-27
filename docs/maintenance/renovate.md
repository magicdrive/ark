# Renovate PR 保守 Runbook

Renovate が作る依存関係更新の PR を、人が一人で確認・検証・修復し、マージ可否を判断するための手順。
AI ツールは前提にしない。ここに書いたコマンドは、状態を変える GitHub / git の操作（再実行、push、PR のクローズなど）を除き、
実際に実行して確かめてある（[付録 A](#付録-a-この-runbook-の検証記録)）。

- 対象: `renovate.json` が作る PR（Go modules、GitHub Actions）。依存更新の PR は Renovate だけが作る。
  Dependabot は Alerts（脆弱性の検知）にだけ使い、`.github/dependabot.yml` は置かない。
- 前提: `git`、`gh`（`gh auth login` 済み）、Go（`go.mod` の `go` 行の版）、`python3`。
  TypeScript の差分テストだけ Node.js と npm が要る。
- 書き方: `<N>` は PR 番号、`<RUN_ID>` は Actions の run ID、`<BRANCH>` は PR の head ブランチ。

## 0. 全体像

### Renovate が作る PR

`renovate.json` の設定による。スケジュールは毎週月曜 10 時前（Asia/Tokyo）。

| 種類 | head ブランチ | 例 | 特記 |
|---|---|---|---|
| golang.org/x のまとめ | `renovate/golang.orgx-packages` | x/net・x/text | `group: golang.org/x packages` |
| その他の minor / patch のまとめ | `renovate/all-minor-patch` | gotreesitter | `group:allNonMajor`。**gotreesitter は [4 章](#4-gotreesitter-の更新)** |
| Go の major | （グループ名 `Go major dependencies`） | — | 毎月 1 日 |
| GitHub Actions | `renovate/github-actions` | checkout・setup-go | digest に pin される |
| Hephaestus | `renovate/hephaestus` | `magicdrive/hephaestus` action と Makefile の `HEPHAESTUS_VERSION` | 両者を 1 つの PR で上げる（`TestHephaestusVersionIsConsistent` が一致を確かめる） |
| Makefile のツール | `renovate/development-tools` | govulncheck・actionlint | `customManagers` が `# renovate:` コメントの付いた版を追う。staticcheck は対象外（3.4） |
| Renovate validator | `renovate/renovate-validator` | `.github/renovate-check` の renovate・Node.js（`.node-version`） | `make renovate-check` が使う Renovate。npm manager が `package.json` と `package-lock.json` を、nodenv manager が `.node-version` を上げる |
| lockfile の更新 | `renovate/lock-file-maintenance` | `.github/renovate-check/package-lock.json` の推移的依存 | 毎週月曜。`.github/ts-oracle` は対象外 |
| 脆弱性修正 | 例 `renovate/go-golang.org-x-net-vulnerability` | x/net（PR #65） | タイトル末尾が ` [SECURITY]`。スケジュールを待たない |

- `minimumReleaseAge: 3 days`: 公開から 3 日経たない版は `renovate/stability-days` が **pending** のまま。
  CI の失敗ではない。Go モジュールの patch 更新だけはこの待ちがない（`minimumReleaseAge: null`）。
- `.github/ts-oracle/**`（CI の TypeScript コンパイラ）は Renovate の対象外。意図して上げる。
- **`security` ラベルは脆弱性修正の印ではない。** 現在の設定（`Vulnerable packages are given top priority`）は
  Go の PR すべてに `security` を付ける（例: PR #67・#68 は脆弱性修正ではない）。
  脆弱性修正の印は、タイトル末尾の `[SECURITY]` と、本文の CVE / GO-ID の節である（[5.3](#53-脆弱性修正を見落とさない)）。

### CI（`.github/workflows/ci.yml`）とローカルでの再現

PR では `CI` workflow が走る。`ci-gate` が全ジョブの成功と race シャードの網羅を確かめる。
CI は ubuntu-latest、`GOTOOLCHAIN: local`、Go は `go.mod` の版。

| ジョブ | 内容 | ローカルで同じことをするコマンド |
|---|---|---|
| lint | gofmt / go vet / staticcheck（版を pin、対象は intelligence core） | [3.4](#34-静的解析) |
| unit | `go test -json ./...` | `go test ./...` |
| race (1)〜(4) | `go test -race` を 4 分割 | `make race`（全パッケージを一度に） |
| fuzz-smoke (1)〜(3) | 18 個の fuzz を各 10 秒 | `.github/ci/fuzz-smoke.sh 1`（2、3 も） |
| filesystem-security | Windows と macOS（case-insensitive の通常ボリュームに加え、case-sensitive のボリュームも作る）で race 付き | ci.yml の該当ジョブの `go test -race ...` 3 行を macOS でそのまま実行できる。case-sensitive ボリュームと Windows は再現しない |
| typescript-compiler-differential | 固定版の TypeScript コンパイラとの差分 | `make ts-oracle` |
| native-tests (1)〜(4) | macOS で `go test -race ./...` を race と同じ 4 分割（分割は macOS の `go list ./...` から計算。Windows では全体はまだ通らない） | macOS では `go test -race ./...`、1 シャードなら `python3 .github/ci/race-shard.py run 1 out.json` |
| vulncheck | govulncheck | `make vulncheck` |
| workflow-lint | actionlint・shellcheck・renovate-check の install の失敗注入テスト・`renovate-config-validator --strict` | `make actionlint`、`make renovate-check-test`、`make renovate-check` |
| release-snapshot | Hephaestus action で全 8 ターゲットの snapshot を作り、`verify-dist.sh --snapshot` で検証 | `make snapshot` |
| release-binaries | snapshot のバイナリを各 OS・arch の runner で実行（`smoke.sh`） | 手元の OS の分だけ `.github/release/smoke.sh` |
| homebrew-e2e | snapshot から formula を作り、Linux と macOS の Homebrew で style・audit・install・test・uninstall | `make snapshot homebrew-e2e`（Homebrew の prefix に入れて消す） |
| ci-gate | 上の全ジョブ成功と、各テストがちょうど 1 回走ったこと（macOS は各パッケージがちょうど 1 回: `check-native.py`） | 再現しない |

`release.yml` は `v*` タグの push でだけ走る。**PR の CI は `release.yml` を実行しない**が、`release-snapshot` ジョブが
release と同じ Hephaestus action・同じビルドで snapshot を作って検証する（[6 章](#6-github-actions-の更新)）。
リリース手順は [releasing.md](../development/releasing.md)。

## 1. 通常の Renovate PR 対応

### 1.1 PR を一覧する

```sh
gh pr list --author app/renovate --state open
```

ラベルで絞らないこと（`dependencies` と `security` に分かれる）。依存の全体は Dependency Dashboard（issue #28）。

### 1.2 変更内容を確認する

```sh
gh pr view <N> --json title,baseRefName,headRefName,headRefOid,files \
  --jq '{title,base:.baseRefName,head:.headRefName,sha:.headRefOid,files:[.files[].path]}'
gh pr diff <N>                      # 全体（gh pr diff はファイルを指定できない）
git fetch origin <BRANCH>
git diff origin/main origin/<BRANCH> -- go.mod go.sum
```

- Go の PR は `go.mod` と `go.sum` だけが変わるのが普通。ほかのファイルが変わっていれば、その理由を読む。
- base が古い（`main` より後ろにある）と、`main` で直った問題が PR に残る:

```sh
git fetch origin
git rev-list --count origin/<BRANCH>..origin/main   # 0 なら最新の main の上にある
```

### 1.3 CI の結果を確認する

```sh
gh pr checks <N>
```

`fail` があれば [2 章](#2-ci-が失敗したとき)。`renovate/stability-days` の pending は待つだけでよい。

### 1.4 安全な更新かを判断する

| 状況 | 判断 |
|---|---|
| CI がすべて成功、変更は `go.mod`/`go.sum` だけ、上流のリリースノートに破壊的変更なし | マージしてよい |
| gotreesitter の更新 | CI が成功していても [4 章](#4-gotreesitter-の更新) の確認をする |
| GitHub Actions の digest 更新 | CI の成功は action を検証していない。[6 章](#6-github-actions-の更新) を見る |
| CI が失敗 | 原因が分かるまでマージしない。[2 章](#2-ci-が失敗したとき) |
| 脆弱性修正（`[SECURITY]`）が失敗 | 放置しない。[7.4](#74-脆弱性修正がブロックされたとき) |

上流のリリースノートは、例えば次で読める:

```sh
gh release view v0.56.0 -R odvcencio/gotreesitter
```

### 1.5 マージ前のチェックリスト

- [ ] `gh pr checks <N>` がすべて pass（`renovate/stability-days` 含む）
- [ ] 変更ファイルが想定どおり（1.2）
- [ ] 上流のリリースノートを読んだ（破壊的変更、動作変更、Go の版の要求）
- [ ] gotreesitter なら 4 章、Actions なら 6 章を済ませた
- [ ] テストを弱めていない・skip を足していない（PR に人が足したコミットがある場合）
- [ ] 判断と根拠を PR にコメントした

## 2. CI が失敗したとき

### 2.1 失敗したジョブとログを取る

```sh
gh pr checks <N>                       # fail の行の URL に run ID と job ID がある
gh run view <RUN_ID> --log-failed > /tmp/ark-run.log
grep -nE -- '--- FAIL|[[:space:]]FAIL[[:space:]]|panic:|DATA RACE' /tmp/ark-run.log   # 各行の先頭にはジョブ名と時刻が付く
```

unit と race のログには、テストの要約が 1 行出る（`.github/ci/test-report.py`）:

```text
unit-linux: 57 packages (2 failed, 0 cached); top-level tests 1569 pass / 2 fail / 33 skip; ...
```

その下に、失敗したテストごとの出力が並ぶ。パッケージごと・テストごとの結果（TSV）は artifact にある:

```sh
gh api repos/magicdrive/ark/actions/runs/<RUN_ID>/artifacts --jq '.artifacts[].name'
gh run download <RUN_ID> -n test-report-unit-attempt-1 -D /tmp/ark-report   # 再実行した run は attempt-2 など
ls /tmp/ark-report          # unit-linux.packages.tsv  unit-linux.tests.tsv
```

- `ci-gate` の失敗は、たいてい他のジョブの失敗の結果である。先に他のジョブを見る。
- 最後のエラーではなく、**最初に出た意味のあるエラー**を見る。

### 2.2 原因を切り分ける

1. **同じテストが `main` や他の PR で通っているか**。他の PR は依存の版だけが違う。

   ```sh
   gh run list --branch main --limit 5
   gh pr checks <他の PR 番号>
   ```

   同じテストコードで他が通り、この PR だけ落ちるなら、依存の更新か偶発的な失敗（flake）である。
2. **テストが何を固定しているか**をテストのコメントで読む。Ark には「依存の振る舞いを固定するゲート」がある
   （`TestRoute_*`、`TestParserBehavior_GenericCallsDerivedAsComparisons` など。[4.2](#42-ゲートになっているテスト)）。
   ゲートの失敗は「依存の振る舞いが変わった」という知らせで、Ark の不具合とは限らない。
3. **失敗の種類**を決める: 依存の API の変化 / 依存の振る舞いの変化 / 計時（性能比）テストの揺れ /
   CI 基盤（ダウンロード失敗など）/ Ark の既存の不具合。

### 2.3 ローカルで再現する

PR ごとに worktree を作る（作業中のツリーを汚さない）:

```sh
git fetch origin <BRANCH>
git worktree add -b work/pr<N> ../ark-pr<N> origin/<BRANCH>
cd ../ark-pr<N>
GOTOOLCHAIN=local go test -count=1 -run '<失敗したテスト名>' ./<パッケージ>/
```

更新前と比べるときは、`main` の worktree も作る:

```sh
git worktree add --detach ../ark-main origin/main
```

終わったら `git worktree remove ../ark-pr<N>`。

### 2.4 修正と再検証

- テストの期待値を変えるのは、**新しい値が正しいと示せたとき**だけ（[4.4](#44-差分を分類するゲートを更新する手順)）。
  落ちるテストを消す・skip する・閾値を緩めることはしない。
- 修正の後は、落ちたテスト → そのパッケージ → 全体（[3.3](#33-テスト)・[3.4](#34-静的解析)）の順に流す。
- `main` にも効く修正（テストの揺れなど）は、依存の PR に混ぜず、別の PR にする。
  依存の PR はその修正が `main` に入ってから rebase する（Renovate の PR にある rebase のチェックボックス）。
- 新しい版でしか通らない修正（PR #68 のゲートの更新は v0.56.0 が前提で、v0.55.1 の `main` では失敗する）は、
  依存の更新と同じ PR に入れるしかない。どちらかを選ぶ:
  - **自分の PR を作る（推奨）**: `main` からブランチを作り、`go get github.com/odvcencio/gotreesitter@v0.56.0`
    と `go mod tidy` で更新し、修正のコミットを足して PR にする。マージ後、Renovate の PR は不要になる
    （閉じられなければ手で閉じる）。Renovate のブランチには触れない。
  - **Renovate のブランチに push する**: `git push origin HEAD:<BRANCH>`。以後 Renovate はそのブランチを
    更新しない。rebase のチェックボックスを入れると、足したコミットは消える。
- Renovate のブランチに人がコミットを足すと、Renovate はそのブランチを更新しなくなる。
  rebase のチェックボックスを入れると、Renovate はブランチを自分のコミットで作り直す（人のコミットは残らない）。
  Renovate のコミットは amend しない（[Renovate のドキュメント](https://docs.renovatebot.com/updating-rebasing/)）。

### 2.5 再実行してよい失敗

再実行（`gh run rerun <RUN_ID> --failed`）は、次のどれかを**示せたとき**だけ:

- CI 基盤の失敗（モジュールやツールのダウンロード失敗、runner の喪失）。ログにそう出ている。
- 計時（性能比）テストの失敗で、同じテストコードが `main` や他の PR で通っていて、ローカルで比を測ると閾値から遠い。
  一度通っても、揺れたテストは直す（[8.2](#82-計時テストの揺れtestaudit9_html)）。

依存の更新で振る舞いが変わった失敗は、再実行で通っても解決ではない。

## 3. Go の依存関係の更新

### 3.1 go.mod / go.sum

```sh
git diff origin/main origin/<BRANCH> -- go.mod go.sum
GOTOOLCHAIN=local go mod verify                    # all modules verified
GOTOOLCHAIN=local go mod tidy -diff && echo tidy   # 差分がなければ "tidy" とだけ出る
```

Renovate は `gomodTidy` を実行するので、普通 `go mod tidy -diff` は空である。

### 3.2 Go の toolchain との互換性

- `go.mod` の `go` 行（例 `go 1.27.2`）が CI とリリースの Go の版である（`go-version-file: 'go.mod'`）。
- CI は `GOTOOLCHAIN: local` で動く。依存が新しい Go を要求すると、ローカルでも CI でも失敗する。
  依存の要求は、その版の `go.mod` で確かめる:

  ```sh
  grep '^go ' "$(go mod download -json golang.org/x/net@v0.61.0 | sed -n 's/.*"GoMod": "\(.*\)",/\1/p')"
  # 例: go 1.26.0（Ark の go 1.27.2 以下なので可）
  ```

- ローカルの Go が `go.mod` と違うと、結果が CI と一致しない。`go version` を確かめる（[8.3](#83-ローカルの-go-が-compile-の版の不一致で失敗する)）。

### 3.3 テスト

```sh
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local make race     # go test -race ./...（cgo が要る）
.github/ci/fuzz-smoke.sh 1      # 2、3 も。各グループ約 1〜2 分
```

`make fuzz` は CI の fuzz smoke とは別の（短い）一覧である。CI と同じものは `fuzz-smoke.sh`。

### 3.4 静的解析

```sh
files=$(git ls-files '*.go' | grep -v '/testdata/' | xargs gofmt -l); echo "[$files]"   # [] なら可
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local make staticcheck
```

`make staticcheck` は CI と同じ版の staticcheck（`go run` で取得）を CI と同じパッケージに対して走らせる
（`TestMakefile_StaticcheckMatchesCI` が両者の一致を保つ）。staticcheck 2026.2.1（v0.8.1）は Go 1.27.2 の
export data を読めない。2026.2.2（v0.8.2）は読める（CI と同じ対象で指摘なしを確認済み）。CI の pin を
リリース版に戻すときは、`ci.yml` と `Makefile` の版を同時に変える（テストが不一致を検出する）。

`make lint` は vet・test・race・staticcheck をまとめて流す（gofmt は含まない）。

### 3.5 間接依存と、使っているパッケージの差分

- `go.sum` だけが変わる間接依存の更新も、`go mod verify` と全テストで確かめる。
- 依存のうち Ark が実際に import しているパッケージは次で分かる。そのソースの差分を読むと影響の有無が分かる:

  ```sh
  GOTOOLCHAIN=local go list -deps ./... | grep '^golang.org/x/'
  M=$(go env GOMODCACHE)/golang.org/x
  GOTOOLCHAIN=local go mod download golang.org/x/net@v0.60.0 golang.org/x/net@v0.61.0
  diff -r $M/net@v0.60.0/html $M/net@v0.61.0/html && echo same
  ```

  PR #67（x/net v0.60.0→v0.61.0、x/text v0.42.0→v0.43.0）は、Ark が使うパッケージのソースが新旧で同一だった。

- 脆弱性の有無（ツールのインストール不要）:

  ```sh
  GOTOOLCHAIN=local go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
  ```

## 4. gotreesitter の更新

### 4.1 何を確かめるか

gotreesitter は Go・Rust・TypeScript/TSX・JavaScript・Python・PHP・Terraform の構文解析を担う
（すべて `internal/tsparse/tsparse.go` を通る。HTML 内のスクリプトも JavaScript / TypeScript として解析する）。
更新で確かめるのは次の 3 つ:

1. Ark が正しく解析できていたソースを、更新後も正しく解析できる。
2. 解析結果の差分を、改善・正当な仕様変更・回帰のどれかに根拠をもって分類できる。
3. クラッシュ・データ競合・非決定性・大きな性能低下がない。

解析結果の完全一致は目的ではない。正しい結果への変化は改善として受け入れる。
件数の増減だけで判断しない（増えて誤りが増えることも、減って誤りが消えることもある）。

### 4.2 ゲートになっているテスト

どれも CI の unit / race で走る。失敗したら、テストのコメントの指示に従う。

| テスト | 固定しているもの | 失敗の意味 |
|---|---|---|
| `TestRoute_*`（`internal/tsparse/tsparse_test.go`） | 既知の入力がどの parse route を通るか | route の振る舞いが変わった。fallback を測り直す |
| `TestParse_*` | Ark が依存する parse の契約（route によらない） | Ark の解析が壊れた。**マージしない** |
| `TestParserBehavior_GenericCallsDerivedAsComparisons` | TypeScript の `f<T>(x)` を比較式と誤って導出する入力 | 誤導出が変わった。回復処理を測り直す |
| `TestExtract_GenericCallReadAsComparison` など `TestExtract_*` | 抽出の契約（パーサの版によらない） | **回帰**。マージしない |
| `TestProbe_*` | forest route の probe と非 probe の一致、線形コスト | probe の前提が崩れた |

「契約」のテストが通り、「ゲート」だけが落ちるなら、依存の振る舞いが変わっただけの可能性が高い。4.3 で測る。

### 4.3 更新前後を比べる

条件を揃えるため、`main`（旧版）と PR（新版）の worktree を作り、同じコーパスに同じコマンドを流す（2.3）。
コーパスは手元にあるもので足りる。今回（PR #68）に使ったもの:

| 言語 | コーパス |
|---|---|
| Go | `$(go env GOROOT)/src` |
| Rust | `~/.cargo/registry/src/*/` |
| Python | Python の標準ライブラリ（例 `.../lib/python3.12`） |
| TypeScript | zod（`git clone --depth 1 --branch v3.23.8 https://github.com/colinhacks/zod.git`） |
| JavaScript | npm の `lib/`（パスに `node_modules` を含まない場所へコピーする） |
| PHP / TSX | このリポジトリの `internal/` の testdata |

**(a) parse の route と木（既存の corpus テスト）** — 新版で、probe と非 probe の route・木が一致するか:

```sh
ARK_TSPARSE_CORPUS="go:.go:$(go env GOROOT)/src;typescript:.ts:/path/to/zod" \
  GOTOOLCHAIN=local go test -count=1 -v -run '^TestProbe_Corpus$' ./internal/tsparse/
```

言語は `go rust typescript tsx javascript python php hcl`。Go の標準ライブラリ全体で数分かかる。

**(b) Ark の解析結果（シンボル・参照・解決）** — 両方の worktree で:

```sh
sh docs/scripts/fidelity-baseline.sh /path/to/corpus /tmp/fb-main    # ../ark-main で
sh docs/scripts/fidelity-baseline.sh /path/to/corpus /tmp/fb-pr      # ../ark-pr<N> で
diff <(grep 'files=' /tmp/fb-main/report.txt) <(grep 'files=' /tmp/fb-pr/report.txt)   # 言語別の symbols / refs / confidence
diff <(sort /tmp/fb-main/report.sites) <(sort /tmp/fb-pr/report.sites) | grep -c '^[<>]'
```

`report.sites` は参照 1 件 1 行（言語、位置、種類、名前、confidence、outside、根拠、解決先）。
差分の行だけを読めばよい。

**(c) TypeScript は言語のコンパイラと比べる**（正解が分かる）。両方の worktree で:

```sh
mkdir -p /tmp/ts-oracle && cp .github/ts-oracle/package*.json /tmp/ts-oracle/ \
  && npm ci --prefix /tmp/ts-oracle --ignore-scripts --no-audit --no-fund
ARK_TYPESCRIPT_MODULE=/tmp/ts-oracle/node_modules/typescript ARK_TYPESCRIPT_VERSION=5.6.3 \
ARK_TS_ORACLE_REQUIRED=1 ARK_TS_FIDELITY_ROOTS=/path/to/zod \
  GOTOOLCHAIN=local go test -count=1 -v -run '^TestFidelity_TypeScriptCompiler$' ./internal/languages/typescript/
```

`N references agree, missing: ...` と false references の件数を新旧で比べる。false reference が 1 件でもあると
このテストは FAIL する（コーパスによっては旧版でも FAIL する。PR #68 の zod では旧版が FAIL、新版が PASS だった）。
FAIL かどうかより、件数の増減と、新版だけに出る false reference を見る。
（`ARK_TYPESCRIPT_VERSION` は `.github/ts-oracle/package.json` の版。）

### 4.4 差分を分類する・ゲートを更新する手順

差分が出たら、その箇所だけを詳しく調べる。

| 分類 | 見分け方 |
|---|---|
| 改善 | 言語のフロントエンド（コンパイラ等）の結果に近づいた。誤りが消えた |
| 正当な仕様変更 | 上流のリリースノートに書かれ、文法どおりの結果に揃った |
| 意味的に同じ | 順序・表現だけの違い |
| Ark 側の期待値・実装の問題 | テストの期待値や抽出が、誤った木に依存していた |
| gotreesitter の回帰 | 旧版で正しく解析できた正しいソースが、新版で誤る（言語のフロントエンドで確かめる） |
| 原因未確定 | 上のどれとも示せない。未確定として残し、マージを保留する |

- 文法がそもそも受け付けない構文（両方の版でエラー）は回帰ではない。小さな再現入力を作り、両方の版で比べる。
- ゲートの期待値を更新するのは、上の測定で新しい振る舞いを説明できたときだけ。
  更新では、ゲートが**新版で残っている誤導出**を固定し、契約のテストがその回復の経路を通るようにする
  （PR #68 では、旧入力の誤導出が新版で消えたため、新版が誤る入力 `f(a < b, c > (d))` に替えた）。
- 回復処理が要らなくなったように見えても、消さない（別の入力で誤ることがある）。

追加の調査が要るのは次のとき:

- `TestParse_*` か `TestExtract_*`（契約）が落ちる
- 4.3 で exact / strong の参照が消える・変わる、またはシンボルが減る
- 正しいソースで、新版だけに parse_error（ERROR ノード）が出る
- 時間が大きく増える（`TestProbe_Corpus` の before / now、fidelity-baseline の `extract=` / `resolve=`）

## 5. 脆弱性修正

### 5.1 判別

```sh
gh pr list --author app/renovate --state open --search 'SECURITY in:title'
gh pr view <N> --json body --jq .body | grep -E 'CVE-|GO-20'
```

### 5.2 影響の確認

`main` に対して govulncheck を流す（3.5）。結果に出る脆弱性は、Ark が実際にそのコードを呼んでいるものである。

### 5.3 脆弱性修正を見落とさない

- `security` ラベルでは判別できない（0 章）。毎週 5.1 のコマンドを流す。
- 脆弱性修正の PR は他より先に処理する。失敗しても閉じない（閉じると Renovate はその版を再提案しない）。

## 6. GitHub Actions の更新

### 6.1 digest の正当性

`uses: owner/repo@<sha> # v7` の sha が変わる。新しい sha が、そのリポジトリの正しいタグを指し、
旧 sha からの早送りであることを確かめる:

```sh
gh api repos/actions/checkout/git/matching-refs/tags/v7 --jq '.[]|.ref+" "+.object.sha+" "+.object.type'
# type が tag（注釈付きタグ）なら、sha はタグオブジェクトのもの。commit の sha は次で得る:
# gh api repos/actions/checkout/git/tags/<sha> --jq .object.sha
gh api repos/actions/checkout/compare/<旧sha>...<新sha> \
  --jq '{status,ahead_by,behind_by,commits:[.commits[]|.sha[0:8]+" verified="+(.commit.verification.verified|tostring)+" "+(.commit.message|split("\n")[0])]}'
```

- `status: ahead` かつ `behind_by: 0` で、新 sha がタグ（例 `v7.3.0`）と一致すること。
- 中身を読む: `--jq '.files[].filename'` で変更ファイル、`.files[]|select(.filename=="src/...")|.patch` で差分。
  特にダウンロード・検証・権限・`action.yml` の `runs.using`（Node の版）の変化を見る。

### 6.2 権限とセキュリティ

- ワークフローの `permissions:` が変わっていないこと（`gh pr diff <N>`）。
- action がダウンロードするバイナリの検証方法が弱まっていないこと（6.1 の差分で読む）。
  （PR #69 の goreleaser-action は、Hephaestus への移行で使われなくなった。）
- `magicdrive/hephaestus` の更新は、Makefile の `HEPHAESTUS_VERSION` と同じ PR で上がること
  （`TestHephaestusVersionIsConsistent`）。action（v1.2.0 以降）は pin された commit の Release のバイナリを
  取得し、checksum と provenance attestation（その commit と tag）を検証して実行する。Go を入れず、ビルドもしない
  （Go は各ジョブの setup-go が `go.mod` の版を入れる）。CI のログの `Install hephaestus` で `verified:` を確かめる。
  token は action の input でだけ渡し、ビルドの step には渡さない（`release.yml` の `build` ジョブ）。
- pin が prerelease（例 `v1.2.0-rc.1`）のとき、Renovate は正式版（例 `v1.2.0`）が出ると（`minimumReleaseAge` の後）
  それを 1 つの PR（`renovate/hephaestus`）で提案する。安定版の pin からは prerelease を提案しない。
  どちらの PR でも action の digest（8 箇所）と `HEPHAESTUS_VERSION` が一緒に動くことを確かめる。

### 6.3 ローカルで再現できる範囲

- action そのものはローカルで動かせない。PR の CI も `release.yml` を動かさない。
  ただし `release-snapshot`・`release-binaries`・`homebrew-e2e` が、release と同じ Hephaestus action と
  同じビルドで snapshot を作り、検証し、各 OS で実行し、Homebrew で入れる。
  publish の部分（release・tap）が試されるのは次のタグの push である。
- 設定と成果物は CLI でローカルに確かめられる（Makefile の `HEPHAESTUS_VERSION` を `go run` する）:

  ```sh
  make hephaestus-check
  make snapshot             # dist/ に 8 archives。verify-dist.sh --snapshot まで
  make homebrew-e2e         # Homebrew が要る。prefix に入れて消す
  ```

## 7. 安全な復旧

### 7.1 PR を保留する

- マージしなければよい。PR に理由をコメントする。
- 一時的に止める: Dependency Dashboard（issue #28）、または PR を閉じる。
  **閉じた PR の版は Renovate が再提案しない**（その版を無視する）。脆弱性修正は閉じない。

### 7.2 更新を取り消す判断とロールバック

マージ後に回帰が見つかったら、マージコミットを revert する PR を作る:

```sh
git switch -c revert/pr<N> origin/main
git revert -m 1 <マージコミット>
GOTOOLCHAIN=local go test ./...
```

push と PR の作成は通常どおり。取り消した版を Renovate が再提案しないよう、`renovate.json` の
`packageRules` に `allowedVersions` を足すか、Renovate の PR を閉じる。

### 7.3 上流（upstream）の問題として切り分ける

- 両方の版で、最小の入力を作って比べる（PR #68 の Rust の `macro_rules!` の例は 3 行で再現した）。
- 言語のフロントエンド（go/parser、tsc、CPython の `ast.parse`、rustc）でソースが正しいことを示す。
- 上流のリリースノートで、変更がその挙動を狙ったものかを確かめる。
- issue には、版、最小入力、期待する木（言語の解釈）、実際の木を書く。

### 7.4 脆弱性修正がブロックされたとき

1. govulncheck（3.5）で、Ark が該当コードを呼ぶか確かめる。
2. CI の失敗を 2 章で切り分ける。テストが新版の正しい振る舞いを固定していなかっただけなら、テストを両方の版で
   正しく通るよう直す（PR #65 の e39de54 が例）。
3. 修正が長引くなら、マージを急ぐか（呼ばれていない脆弱性か）を判断して PR に書く。PR は閉じない。

## 8. トラブルシューティング（実例）

### 8.1 gotreesitter のゲートの失敗（PR #68、v0.55.1→v0.56.0）

- 症状: unit と race (3) で `TestParserBehavior_GenericCallsDerivedAsComparisons` が
  `gotreesitter's tree changed` で失敗。契約のテストは通った。
- 原因: v0.56.0 が、ゲートの入力の generic call を正しく call として導出するようになった
  （上流の #1443、#1414）。誤導出が消えたので、ゲートの期待値が古くなった。
- 測定: 回復処理が働く箇所は zod 両版で 239→0。TypeScript コンパイラとの比較で、一致する参照が増え、
  false reference は 1→0。一方、v0.56.0 は `f(a < b, c > (d))` を新たに比較式 2 つと導出する
  （コンパイラは call `a<b, c>(d)`）。Ark の回復処理が call として読むので、抽出結果は同じ。
- 対処: ゲートを v0.56.0 の誤導出に替え、契約のテストにその入力を足した（4.4）。

### 8.2 計時テストの揺れ（`TestAudit9_HTML`）

- 症状: PR #68 の unit で `shadow root references: 1500 21.2ms, 6000 172.1ms (more than linear)`
  （比 8.1、閾値 8）。同じテストコードの PR #67・#69・main では通った。
- 原因: 小さい入力を 3 回、次に大きい入力を 3 回測っていたので、その間に負荷が上がると比だけが上がる。
  ローカルの比は 3.9〜4.4 で線形。同じ形の揺れは 6dde9b4 が `TestEmbedded_ManyRegions` で直している。
- 対処: 6dde9b4 と同じく、2 つの大きさを交互に測り、閾値を超えたら最大 3 回まで測り直す。閾値は変えない。
  二次のコストを入れると、どの試行でも失敗する（比 9.5〜10.5）。

### 8.3 ローカルの Go が compile の版の不一致で失敗する

- 症状: `compile: version "go1.27.1" does not match go tool version "go1.27.2"`。
- 原因: 環境変数 `GOROOT` が別の版の Go を指している（goenv などの設定）。
- 対処: `go env GOROOT` と `go version` を確かめ、`GOROOT` を外して実行する（`env -u GOROOT go test ./...`）。

### 8.4 `make staticcheck` と `make race` が CI と違った

- `make staticcheck` は `@latest` の staticcheck を案内していたが、手元にあった 2026.2.1 は
  `export data version 5 is greater than maximum supported version 4` で動かなかった。また CI の pin 版でも
  `./...` 全体には CI の対象外の指摘が 5 件あり、`make staticcheck` と `make lint` は通らなかった。
  CI と同じ版・同じ対象にした。
- `make race` は `CGO_ENABLED=0` で動き、Linux では `go: -race requires cgo` で失敗した（macOS では動く）。
  cgo を有効のまま動かすようにした。

### 8.5 x/net の更新で HTML の木が変わった（PR #65）

- x/net v0.60.0（脆弱性修正）は HTML 標準の新しい select の扱いで木を作る。旧い木だけを固定していた
  テストが落ちた。Ark のコードは木に従っているので変えず、テストが x/net の作る木を尋ね、
  両方の版の正しい答えを確かめるようにした（e39de54）。

### 8.6 その他

- `.github/ts-oracle/run.sh` の `ARK_TS_ORACLE_DIR` は既存のディレクトリでなければならない（無いと `cp` が失敗する）。
  指定しなければ一時ディレクトリを使う。
- `make renovate-check` の Renovate は `.github/renovate-check/package-lock.json` が依存ツリーごと固定し、
  `npm ci --ignore-scripts --engine-strict` で入れる（`.github/renovate-check/run.sh`）。以前の
  `npx --package renovate@<版>` は推移的依存を実行時に解決していたため、数分前に公開された版
  （2026-10-26 の bowser 2.15.0）を取りに行き、registry にまだ無く 404 で落ちた（CI run 40393137410）。
  Node.js は Renovate の engines（`^24.11.0`）を満たす `.github/renovate-check/.node-version` の版。
  それより古い Node.js では `EBADENGINE` で失敗する。
- `--ignore-scripts` のため re2 は組み込まれず、validator は `RE2 not usable, falling back to RegExp` と警告する。
  以前の `npx` でも re2 は入っていなかった（同じ警告）。

## 9. 人が判断すること

- CI の staticcheck を pin した開発版から 2026.2.2（v0.8.2）に戻すか（3.4）
- 依存の振る舞いの変化を受け入れるか（4.4 の分類が「原因未確定」なら保留）
- テストの期待値の更新（新しい値が正しいという根拠があるか）
- 計時テストの失敗を揺れとみなして再実行するか（2.5）
- 脆弱性修正をどれだけ急ぐか（7.4）
- Renovate の PR に人がコミットを足すか、`main` に別 PR で入れてから rebase するか（2.4）
- GitHub Actions の更新の受け入れ（6 章。PR の CI では action は試されない）
- `renovate.json` の変更（ラベル・グループ・待ち日数）。例えば、`security` ラベルを脆弱性修正にだけ付けるには、
  `Vulnerable packages are given top priority` のルールから `labels` を外し、最上位に
  `"vulnerabilityAlerts": { "labels": ["security"] }` を足す。変えたら validator で確かめる:

  ```sh
  make renovate-check   # Config validated successfully
  ```

## 付録 A. この Runbook の検証記録

2026-10-20、macOS（darwin/amd64）、go1.27.2、`GOTOOLCHAIN=local` で、PR #67・#68・#69 を題材に実行した。

- 1 章・2 章の `gh` コマンド（`pr list`・`pr view`・`pr checks`・`run view --log-failed`・artifact の一覧）: 動作を確認
- 3 章: `go mod verify`、`go mod tidy -diff`、`go test ./...`、`go vet ./...`、`make staticcheck`、
  `.github/ci/fuzz-smoke.sh 1..3`、`go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`: 成功を確認
- `make race`: macOS で成功。Linux（`golang:1.27.2` のコンテナ、zsh を追加）でも全パッケージが race 付きで成功
  （修正前の `CGO_ENABLED=0` では `go: -race requires cgo` で失敗）
- 4 章: `TestProbe_Corpus`、`fidelity-baseline.sh`、`TestFidelity_TypeScriptCompiler`（zod）を新旧で実行
- 6 章: `gh api .../compare` と `matching-refs` で PR #69 の digest を確認。GoReleaser v1.26.2 のリリースバイナリ
  （checksum OK）で、`main` のクローンに `check`・snapshot・`verify-dist.sh`（8 archives）が成功
- 9 章: 現行の `renovate.json` と、上に書いた変更を加えたものが、どちらも `renovate-config-validator` を通る
  （`prPriority` は `vulnerabilityAlerts` の中では使えない、と validator が拒否することも確認した）
- 実行していない: `gh run rerun`、`git revert` 以降の push、PR のクローズ、`renovate.json` の変更（状態を変える操作）
