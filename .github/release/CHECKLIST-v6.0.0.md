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
- [x] Index freshness is checked under the request's own snapshot: a
      reused index is served only after a walk of the sources through the
      request's pinned root and policy snapshot (`internal/safewalk`) hashes
      every source's content and equals the index's fingerprint; a rule
      change that alters the file set forces a rebuild
      (`TestC3_5_StaleIndexAfterRuleChange`, `TestIndexReuse_*`,
      `TestWarmIndexReads_*`); a symlinked root keeps its `.arkignore` rules
      (`TestSymlinkedRoot_*`). A request therefore walks the repository twice
      (once to read the rule files, once for the fingerprint). The rule-file
      walk still records its listing (`IgnoreReader.CollectEntries`), but no
      production path uses it since Phase 3-C: `SourceFingerprintListed` and
      `TestSourceFingerprintListed_*` / `TestListedFingerprint_*` test that
      dormant mechanism only (removal deferred past v6.0.0).
- [x] A symlinked root (MCP `--root`, dump target) answers as its directory:
      four root spellings compared over 21 tools and every dump format
      (`TestSymlinkedRoot_*`).
- [x] C1 (another spelling of an excluded path on case-insensitive file
      systems) fixed in the MCP path gate, MCP walks and the dump; verified on
      macOS (APFS, case-insensitive and case-sensitive volumes): `TestC1_*`,
      `TestCase_PathGate`, `TestMCPCase_*`, `TestCLICase_*`.
- [x] C1 on Windows, macOS and Linux: the `filesystem-security` CI jobs
      (windows-latest, macos-latest) and the `test` job (ubuntu-latest) green
      on `da4c43a` (CI run #180). Re-confirm on the release commit (CI below).
- [x] Phase 3-B: the files a client names, and snippets, are decided and read
      in one operation on the request's pinned root (`TestB1_*`…`TestB10_*`,
      `TestScoped*`); verified on macOS.
- [x] Phase 3-C: walks, the index and the dump read through the pinned root
      and a policy snapshot (`internal/safewalk`; `TestC3_*`); verified on
      macOS.
- [x] Phase 3-D (performance): raw-descriptor reads, reads of a walked entry
      against the identity its walk found (`fsroot.Entry.ReadAs`), index
      snippets read through the request's walk (`Walker.ReadReported`), the
      parse memo, parallel index extraction (`TestEntryReadAs`,
      `TestReadResolved_DirectoryReplaced`, `TestWarmIndexReads_*`,
      `TestParseMemo_*`, `TestParallel*`); verified on macOS, and on Linux by
      CI run #180. Windows: the `filesystem-security` job runs the new
      `fsroot` tests and, since this checklist revision, the new `mcp` /
      `index` / `golden` tests — confirm that job green on the release
      commit.
- [x] Final security audit before tagging (Phases 3-A…3-D), independent,
      on `da4c43a`: CONDITIONAL GO, no CRITICAL or HIGH finding. Conditions
      — F-5: release built with a Go carrying five reachable standard
      library vulnerabilities (Go 1.27.1; fixed in 1.27.2) → `go.mod` now
      requires Go 1.27.2, and CI and the release take their Go from it;
      F-6: this checklist described the freshness check wrongly → corrected
      above; F-7: the release notes' performance section predated Phases
      3-C/3-D → rewritten. Deferred past v6.0.0: F-1 (a Terraform test for
      the parse memo key), F-2 (the parse memo's 64 MB bound counts source
      bytes, not the memory its results hold).
- [ ] Decide on the TOCTOU limitation: ship with the documented trust model
      (the access policy is not a boundary against writers of the
      repository or its root link; `SECURITY.md`), or close it before the
      release (request-pinned root and descriptor-based reads). Fact for the
      decision: Phases 3-A…3-C implemented the request-pinned root and
      descriptor-based reads; what remains is documented in `SECURITY.md`
      (renames and hard links made by writers of the repository).
      Maintainer's decision.
- [ ] Optional: re-run the Go oracle on a repository of your choice
      (`ARK_GO_ORACLE_ROOT=<repo> go test -run TestGoOracleMeasure -v ./internal/languages/golang/`):
      `FP 0` expected.

## Repository

- [x] Review and commit the documentation overhaul and the MCP secret
      masking (`git status`): README, README_ja, `docs/`, SECURITY,
      ARCHITECTURE pointers, release notes, `internal/mcp/sanitize.go`,
      help text, `.goreleaser.yml`, tests (`docs_test.go`,
      `mcp_secret_masking_integration_test.go`, `mcp_arkignore_integration_test.go`,
      `internal/mcp/sanitize_test.go`, `internal/commandline/help_test.go`)
      — all tracked at `da4c43a` (e.g. `129e63b`).
- [ ] After tagging, check the README's Quick start on a clean machine
      (`brew install`, `ark setup <client>`) and that `ark --version`
      prints `v6.0.0`.
- [ ] `git diff v5.0.1..HEAD` matches the changelog in `NOTES-v6.0.0.md`.
- [x] No untracked artifacts (`dist/`, `.ark/`): no untracked file and no
      `dist/` (`git status --short --ignored`, 2026-10-11); a local `.ark/`
      cache exists, ignored by `.gitignore`, untracked, and in no archive of
      the snapshot build (archives hold the binary and completions only).

## Local gates [audit]

Run 2026-10-11 on macOS x86_64 with Go 1.27.2 (the release's Go), on the
working tree of this checklist revision:

- [x] `files=$(git ls-files '*.go' | grep -v '/testdata/' | xargs gofmt -l)` is empty
- [x] `go vet ./...` (also with `GOOS=linux` and `GOOS=windows`)
- [x] `go test ./...` (44 packages)
- [x] `go test -race ./...` (44 packages, no data race)
- [x] staticcheck on the CI package set (darwin, linux, windows) with the
      version CI now pins, `v0.7.0-0.dev.0.20261009230814-452d5bb86b45`
      (installed with Go 1.27.2): no finding; it still fails a package with
      a finding (checked). Why pinned: staticcheck 2026.2.1 (v0.8.1, the
      latest release) cannot read Go 1.27.2's export data ("export data
      version 5 is greater than maximum supported version 4").
- [ ] After v6.0.0: return CI to a staticcheck release once one supports
      Go 1.27.2.
- [ ] every CI fuzz step exactly as written in `ci.yml` (each `-fuzz` matches one test)
- [ ] `.github/ts-oracle/run.sh`
- [x] `goreleaser release --snapshot --clean --skip=publish` with GoReleaser **v1**
      (CI pins `~> v1`; v2 reports `brews` as deprecated) and `.github/release/verify-dist.sh dist`
      — GoReleaser v1.26.2, in a separate clone: 8 archives, checksums
      verified; every binary built with go1.27.2 (`go version <binary>`);
      `govulncheck -mode=binary`: no vulnerability (darwin, linux, windows
      amd64); archives hold the binary and the four completion files only.

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
