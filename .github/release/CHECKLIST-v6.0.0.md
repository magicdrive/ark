# Ark v6.0.0 — release checklist (draft)

Prepared by the release audit. Items marked [audit] were run during the audit
on macOS x86_64; re-run what changed since.

## Decide before tagging

- [x] Module path stays `github.com/magicdrive/ark` (decided): Ark ships as a
      CLI / MCP server through Homebrew and GitHub Releases; README documents
      `go install …@main` and why `@latest` gives v1.2.3.
- [ ] Optional: re-run the Go oracle on a repository of your choice
      (`ARK_GO_ORACLE_ROOT=<repo> go test -run TestGoOracleMeasure -v ./internal/languages/golang/`):
      `FP 0` expected.

## Repository

- [ ] Review and commit the cross-language isolation changes (`git status`):
      name-space keyed lookups (resolver), `language.Dialect` (TSX),
      `FileIndex.NameSpace` (index), tests, docs.
- [ ] `git diff v5.0.1..HEAD` matches the changelog in `NOTES-v6.0.0.md`.
- [ ] No untracked artifacts (`dist/`, `.ark/`).

## Local gates [audit]

- [ ] `files=$(git ls-files '*.go' | grep -v '/testdata/' | xargs gofmt -l)` is empty
- [ ] `go vet ./...`
- [ ] `go test ./...`
- [ ] `go test -race ./...`
- [ ] staticcheck on the CI package set
- [ ] every CI fuzz step exactly as written in `ci.yml` (each `-fuzz` matches one test)
- [ ] `.github/ts-oracle/run.sh`
- [ ] `goreleaser release --snapshot --clean --skip=publish` with GoReleaser **v1**
      (CI pins `~> v1`; v2 reports `brews` as deprecated) and `.github/release/verify-dist.sh dist`

## CI

- [ ] Push to `main`; CI green (test job and TypeScript compiler differential).

## Tag and release

- [ ] Tag `v6.0.0` on the commit CI verified; push the tag.
- [ ] Release workflow: `ci` and `release-smoke` green before `release` runs.
- [ ] Release body: paste `NOTES-v6.0.0.md` (GoReleaser's changelog comes from a
      shallow checkout in the `release` job).
- [ ] Homebrew tap updated by GoReleaser; `brew upgrade ark` then `ark --version`
      prints `ark version v6.0.0`.

## After release

- [ ] Download one archive per OS family; `ark --version`.
- [ ] `ark mcp-server --version` prints the version; an MCP client's
      `initialize` shows `serverInfo.version: v6.0.0`.
- [ ] Restart an MCP client and confirm `search_context` is listed.
