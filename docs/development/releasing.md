# Releasing Ark

Ark is released with [Hephaestus](https://github.com/magicdrive/hephaestus)
(`hephaestus.yaml`). Pushing a `vX.Y.Z` tag starts
`.github/workflows/release.yml`; nothing else publishes a release.

## What a release is

| Asset | Content |
| --- | --- |
| `ark_<version>_<os>_<arch>.tar.gz` | `darwin_amd64`, `darwin_arm64`, `linux_386`, `linux_amd64`, `linux_arm64`, `windows_386`, `windows_amd64`, `windows_arm64` (Windows is tar.gz too). Each holds `ark` (`ark.exe`) and `completions/{ark-completion.sh,bash/ark-completion.bash,fish/ark.fish,zsh/_ark}` at the root |
| `ark_checksums.txt` | `sha256sum -c` format, one line per archive |
| Provenance attestation | `gh attestation verify <archive> -R magicdrive/ark` |
| Homebrew formula | a pull request to `magicdrive/homebrew-tap` (`ark.rb` at the root), merged by the owner |

The binaries are built by `.github/release/build.sh`: `CGO_ENABLED=0`, the Go
toolchain of `go.mod`, `-ldflags "-s -w -X main.version=vX.Y.Z -X
.../internal/cache.release=true"` (the flag only for a tagged, clean build of
the tag; see `ARCHITECTURE.md`), the same flags as the releases up to
v6.7.0. `.github/release/verify-dist.sh` checks all of it, in CI on a snapshot
and in the release workflow twice before publishing.

## Prerequisites (repository settings, once)

- Environment `release`, deployment policy: tags `v*` only.
- Environment secret `HOMEBREW_TAP_GITHUB_TOKEN` in `release`: a fine-grained
  token (or GitHub App token) with **Contents** and **Pull requests** write on
  `magicdrive/homebrew-tap` only. The release itself uses the workflow's
  `GITHUB_TOKEN`; the former `PERSONAL_GITHUB_TOKEN` secret is no longer used.
- Recommended: a tag ruleset on `refs/tags/v*` forbidding deletion and update,
  and a branch ruleset on `main` requiring the `ci-gate` check.
  `make preflight` reports their absence as `WARN`.

## Procedure

1. Merge everything for the release into `main` and wait for CI (`ci-gate`)
   to be green on the `main` commit you will tag.
2. Optional: write the notes (e.g. `.github/release/NOTES-vX.Y.Z.md`).
3. On an up-to-date, clean `main`:

   ```sh
   make release-check                 # hephaestus check + snapshot, verified
   make preflight tag=vX.Y.Z          # read-only; needs GITHUB_TOKEN or GH_TOKEN
   make publish tag=vX.Y.Z            # tests, preflight, then pushes the tag
   ```

   `make preflight` is strict (a `WARN`, such as a missing ruleset, fails it);
   `make publish` stops on `FAIL` or `UNKNOWN` and reports `WARN`s.
4. The `Release` workflow runs: `ci` (the full CI at the tag) and `build`
   (read-only, no secret: build, archive, checksum, verify), then `release`
   (environment `release`): verify again, rehearse the tap pull request
   (dry run), attest provenance, publish the GitHub release (generated notes),
   open the tap pull request.
5. Edit the release notes on GitHub if needed.
6. Review and merge the pull request on `magicdrive/homebrew-tap`, then
   `brew update && brew upgrade ark && ark --version`.

A tag with a pre-release part (`v1.2.3-rc.1`) is published as a prerelease
and does not update the tap.

## Recovery

```sh
go run github.com/magicdrive/hephaestus@v1.1.0 status --tag vX.Y.Z   # read-only: state and next step
```

- A failed `ci` or `build` job: nothing was published. Fix the cause on
  `main` and release the next version, or, for a transient failure, re-run
  the failed jobs.
- A failed `release` job: re-run it. Hephaestus resumes a draft, skips
  identical assets and never replaces a published asset.
- Release published, tap pull request missing: re-run the `release` job; the
  release is a no-op and the pull request is opened.
- `status` reports `conflict`: do not patch the release; release the next
  version.

Never move or delete a pushed tag. Hephaestus's own guide:
[recovery](https://github.com/magicdrive/hephaestus/blob/v1.1.0/docs/recovery.md).

## Rolling back the migration

The migration from GoReleaser is one pull request; reverting it restores
`.goreleaser.yml` and the GoReleaser workflow (which needs the
`PERSONAL_GITHUB_TOKEN` secret).
