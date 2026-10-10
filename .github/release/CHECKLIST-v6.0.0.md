# Ark v6.0.0 — release checklist (draft)

Prepared by the release audit. Items marked [audit] were run during the audit
on macOS x86_64; re-run what changed since.

## Decide before tagging

- [x] Module path stays `github.com/magicdrive/ark` (decided): Ark ships as a
      CLI / MCP server through Homebrew and GitHub Releases; README documents
      `go install …@main` and why `@latest` gives v1.2.3.
- [x] Positioning: `ark --help` and the Homebrew formula description say
      "Code intelligence engine for AI coding agents"; help defaults and URL
      corrected (`TestHelp_*`).
- [x] Secret masking: on by default for every MCP response;
      `mcp-server --mask-secrets off` turns it off with a warning
      (`TestMCPSecretMasking_*`).
- [x] Per-request `maskSecrets` cannot override the server setting (decided;
      `TestFileContent_SecurityOverridesStillIgnored`,
      `TestMCPSecretMasking_Settings`).
- [x] `.arkignore` access policy: each request reads every rule file once,
      named paths read only the rule files above them, compiled rules reused
      by SHA-256 fingerprint; fail-closed (`TestAccessPolicyCache_*`,
      `TestIgnoreReader_*`, `TestIgnoreFiles_*`, `TestMatchesRel_*`).
- [x] Symlinks leading outside the root: refused and skipped by default;
      `mcp-server --allow-external-symlinks on` allows them, `.arkignore`
      still applies (`TestMCPExternalSymlinks_*`).
- [ ] Confirm for v6.0.0 that walks and the index no longer read files
      through symlinks leading outside the root by default (NOTES, Breaking
      changes).
- [ ] `.arkignore` now hides files from the MCP server (NOTES, Breaking
      changes): confirm for v6.0.0.
- [x] Dump and MCP read ignore files one way: rules rooted at the processed
      directory, `.arkignore` / `.gitignore` as separate sources, symlinks
      never stop the dump (`TestIgnoreSemantics_*`, `TestCLIIgnore_*`).
- [x] Dump changes approved for v6.0.0: a subdirectory target does not apply
      its parents' ignore files; an unreadable ignore file stops the dump
      before any output (`TestCLIIgnoreFailClosed_*`).
- [x] One repository walk per index request: the rule-file walk's listing
      feeds the freshness fingerprint (`TestSourceFingerprintListed_*`,
      `TestListedFingerprint_*`); a symlinked root keeps its `.arkignore`
      rules (`TestListedFingerprint_SymlinkedRootKeepsTheRules`).
- [x] A symlinked root (MCP `--root`, dump target) answers as its directory:
      four root spellings compared over 21 tools and every dump format
      (`TestSymlinkedRoot_*`).
- [x] C1 (another spelling of an excluded path on case-insensitive file
      systems) fixed in the MCP path gate, MCP walks and the dump; verified on
      macOS (APFS, case-insensitive and case-sensitive volumes): `TestC1_*`,
      `TestCase_PathGate`, `TestMCPCase_*`, `TestCLICase_*`.
- [ ] C1 on Windows, macOS and Linux: the `filesystem-security` CI jobs and
      the `test` job green (not yet run).
- [x] Phase 3-B: the files a client names, and snippets, are decided and read
      in one operation on the request's pinned root (`TestB1_*`…`TestB10_*`,
      `TestScoped*`); verified on macOS.
- [ ] Phase 3-C: walks, the index and the dump (still read by path after
      the check; `SECURITY.md`, Concurrent changes).
- [ ] Decide on the TOCTOU limitation: ship with the documented trust model
      (the access policy is not a boundary against writers of the
      repository or its root link; `SECURITY.md`), or close it before the
      release (request-pinned root and descriptor-based reads).
- [ ] Optional: re-run the Go oracle on a repository of your choice
      (`ARK_GO_ORACLE_ROOT=<repo> go test -run TestGoOracleMeasure -v ./internal/languages/golang/`):
      `FP 0` expected.

## Repository

- [ ] Review and commit the documentation overhaul and the MCP secret
      masking (`git status`): README, README_ja, `docs/`, SECURITY,
      ARCHITECTURE pointers, release notes, `internal/mcp/sanitize.go`,
      help text, `.goreleaser.yml`, tests (`docs_test.go`,
      `mcp_secret_masking_integration_test.go`, `mcp_arkignore_integration_test.go`,
      `internal/mcp/sanitize_test.go`, `internal/commandline/help_test.go`).
- [ ] After tagging, check the README's Quick start on a clean machine
      (`brew install`, `ark setup <client>`) and that `ark --version`
      prints `v6.0.0`.
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
