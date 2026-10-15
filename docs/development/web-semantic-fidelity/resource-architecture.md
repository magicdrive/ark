# Resource Graph の設計（Hardening Phase B）

Phase 7（D28）で未実装だった、Provider の無いファイル（画像・フォント・音声・動画など）への参照を扱う。

## B1. 既存のモデル（コードで確認）

- **ファイルの集合**: `index.walkSources` → `sourceStep` が、Provider の拡張子のファイルだけを読み、
  `Extract` に渡す。それ以外のファイルは索引に存在しない（節点が無い）。
- **指紋**: `sourceDigest` が、Provider の版と各ソースの内容の SHA-256 を順に足す。MCP は
  `SourceFingerprint`（同じ `sourceStep` で歩く）と索引の指紋を比べ、違えば作り直す（`mcp/index_cache.go`）。
- **cache**: 1 ファイルの抽出結果（`SymbolDraft` など）を、内容と Provider の版のキーで保存する
  （`cache/store.go`）。索引そのものは保存しない。
- **パスの証拠**: `ReferenceDraft.FileIdentity` の参照は、`resolver.fileDecls`（言語を問わない、ファイル
  自身の宣言 `symbol.DeclaresFile` と、ファイルに属する宣言 `symbol.FileScoped` の索引）だけを引く。
  名前の段には入らない。`TargetKinds` で引く宣言の種類を限る。
- **Symbol を使う側**: context の engine は Symbol の範囲のソースを読む（`readSourceLines`）。repomap は
  `Files()` のファイルを読むことがある（生成ファイルの判定）。MCP のパスで指定するツール（`get_symbols`
  など）は、拡張子の Provider で抽出する。

## 案の比較

| 案 | 内容 | Soundness / 不変条件 | API・MCP | cache・増分 | 判定 |
|---|---|---|---|---|---|
| 1. Resource Node | 新しい節点の種類を graph に足す | 良い | Edge の終点が Symbol でなくなる。`GetSymbol`・edge の JSON・context・impact のすべてが新しい種類を扱う必要がある（公開契約の変更） | 新しい永続化 | 影響が広すぎる |
| 2. File Symbol | 資源にファイル自身の Symbol を与え、ソースのファイルと同じに扱う | 資源が `Files()`・`SymbolsByFile` に入り、context がバイナリを読む、repomap が開く、など「意味のシンボル」と混同される | 互換 | 資源を読む必要（不要な I/O） | 混同の危険 |
| 3. Separate Resource Graph | 資源の graph を別に持ち、関係の層でつなぐ | 良い | 既存の `links` / `linked_from` と別の API が要る | 別の永続化 | 二重の仕組み |
| **4. Hybrid（採用）** | 資源は **別の集合**（意味のファイル・シンボルとは分ける）に置く。identity と edge は既存の仕組み（パスの証拠、`links`）を共有する | 資源は名前の段にも、ソースのファイルの一覧にも入らない。edge の終点として `GetSymbol` で引けるが、種類 `asset` で区別され、ソースを持たない | edge・`linked_from` の形は不変（終点の種類が 1 つ増えるだけ。追加） | 資源は読まない（存在だけ）。指紋に存在を足す | 採用 |

## 採用する設計（Hybrid）

### 資源の集合

- **資源**: Provider の拡張子でなく、資源の拡張子の表にある、リポジトリの中の通常ファイル。
  - 画像: `.png .jpg .jpeg .gif .webp .avif .svg .ico .bmp .apng`
  - フォント: `.woff .woff2 .ttf .otf .eot`
  - 音声・動画・字幕: `.mp4 .webm .ogg .ogv .oga .mp3 .wav .m4a .flac .vtt`
- 索引の歩き方（`sourceStep`）を共有し、同じ規則で除外する（隠しディレクトリ、`vendor`・`node_modules`、
  `.arkignore` など）。**内容は読まない。**
- **シンボリックリンクは資源にしない**（`d.Type()` がリンクのエントリ。リンク先がリポジトリの外かを、読まずに
  安全に決められないため。参照は Unresolved になり、Outside にはならない）。

### identity

- 参照が名指す資源ごとに 1 つの節点（参照されない資源は索引にパスだけを残す。`IsAsset`）: SymbolKind `asset`、Language `asset`（Provider の言語ではない。名前空間
  `asset`。`resource` は Terraform の種類が既に使っているので避けた）、Name はファイル名、Qualified はリポジトリ相対の正規のパス（FileID）、Signature は種類
  （`image` / `font` / `media`）。ID は `symbol.NewDeclarationID("asset", path, asset, path, 1)`。
- **内容に依らない**: 内容が変わっても ID は変わらない（参照の identity を不必要に変えない）。
- `symbol.DeclaresFile(asset)` は真（パスの証拠の終点）。名前の索引には入らない（既存の規則）。
- 大文字小文字・正規化: FileID と URL を解決したパスの完全一致だけ（大文字小文字を区別しないファイル
  システムでも、別の綴りは Unresolved）。パスの正規化・リポジトリの外への脱出の拒否・クエリと
  フラグメントの除去・パーセントの復号は `weburl.Relative`（既存）。

### 索引での置き場所

- `RepositoryIndex` に資源の集合を別に持つ（`Assets()`）。節点は参照が名指す資源にだけ作る（`materializeAssets`。参照されない資源の費用はパスだけ）。`Files()`・`SymbolsByFile`・統計の
  `Files`・`Symbols` には入れない（意味のファイルとして扱う側が読まないように）。
- `GetSymbol`・`FindSymbolsByQualified` は資源を返す（edge の終点、MCP の対象の指定 `get_callers
  assets/logo.svg`）。
- resolver には、資源ごとのシンボルだけを持つ `FileIndex`（`IdentityOnly`、参照なし）を渡し、
  `fileDecls` が引けるようにする。
- context の engine は、種類 `asset` の Symbol のソースを読まない（シグネチャだけ）。

### 指紋と cache

- 指紋に資源の **存在**（`asset "<path>"`）を足す。追加・削除・移動で指紋が変わり、MCP の索引が
  作り直される。内容の変更では変わらない（graph に影響しない）。
- 資源には抽出が無いので cache の項目は無い。Cold・Restore・作り直しで同じ集合になる。
- 資源の存在を参照の解決に使うので、参照する側（HTML・CSS）の抽出結果は資源に依らない（cache の
  キーは今までどおり内容と版）。

### 参照（Provider）

- HTML（`internal/languages/html`）: `img src`・`img srcset`、`picture` / `video` / `audio` の
  `source src`・`source srcset`、`video poster`・`video src`、`audio src`、`track src`、
  `input type=image src`、`link rel=icon` / `apple-touch-icon` / `preload`（`as` が image・font・audio・video・track）の
  `href`、SVG の `image` と `use` の `href` / `xlink:href`（ファイルの部分）。
- CSS（`internal/languages/css`）: 宣言の値の中の `url()`（`@font-face` の `src` を含む）と
  `image-set()` の文字列。`@import` は既存（スタイルシート）。
- 参照: `resource_link`、`FileIdentity`、`IdentityInRepository`、`TargetKinds = asset`、
  上限 Strong（URL とファイルの対応は配信の設定次第）。`<base>` のある文書、テンプレート構文、
  スキーム付き・ルート相対・プロトコル相対の URL は出さない（既存の規則）。
- 存在しないファイル: Unresolved（Outside にはしない）。

### 互換性

- MCP: edge の形は不変。`linked_from` に資源の参照元が現れる。`kind` の説明に `asset` を足す。
- 名前空間の不変条件: 資源への関係はパスの証拠だけ（`TestNameSpaces_NoRelationJoinsTwoNameSpaces`
  の許す種類に資源を加える）。
- 資源の位置は行 0（テキストを持たない）。MCP の出力の `path:0` は資源を表す。
