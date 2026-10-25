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

`make publish` checks each of these and refuses to tag until they are met
(Hephaestus [publish conventions](https://github.com/magicdrive/hephaestus/blob/v1.2.0/docs/publish.md#conventions)):

- Environment `release`, deployment policy: selected tags `v*` only (no
  branch policy).
- Environment secret `HOMEBREW_TAP_GITHUB_TOKEN` in `release`: a fine-grained
  token (or GitHub App token) with **Contents** and **Pull requests** write on
  `magicdrive/homebrew-tap` only. The release itself uses the workflow's
  `GITHUB_TOKEN`; the former `PERSONAL_GITHUB_TOKEN` secret is no longer used.
- An **active** branch ruleset on `main` requiring the status check
  `ci-gate`, and an **active** tag ruleset on `refs/tags/v*` with the rules
  deletion and update (and nothing that blocks creating the tag).
- `gh` authenticated (`gh auth status`) and the `origin` remote set to
  `magicdrive/ark`.

## Procedure

1. Merge everything for the release into `main` and wait for CI (`ci-gate`)
   to be green on the `main` commit you will tag (`make publish` requires the
   `ci.yml` push run of exactly that commit to have succeeded).
2. Optional: write the notes (e.g. `.github/release/NOTES-vX.Y.Z.md`).
3. On an up-to-date, clean `main`:

   ```sh
   make release-check                 # hephaestus check + snapshot, verified (local)
   make publish-check vX.Y.Z          # the preflight only: read-only
   make publish vX.Y.Z                # preflight, confirm, push the tag, follow, verify
   ```

   `make publish vX.Y.Z` (also `VERSION=vX.Y.Z` or, as before,
   `tag=vX.Y.Z`; `YES=1` skips the confirmation) runs `hephaestus publish`
   of `HEPHAESTUS_VERSION`: it checks the conventions and the repository
   settings above, `hephaestus preflight --strict`, that the tag exists
   nowhere yet and that CI passed on the commit; asks; creates and pushes
   one annotated tag; follows the `Release` run it started; and verifies the
   release (assets, checksums, the provenance attestation of every asset,
   the prerelease flag, the tap pull request). It never publishes anything
   itself: `release.yml` stays the only publisher. `make publish-help`
   explains every step and its safety rules.
4. The `Release` workflow runs: `ci` (the full CI at the tag) and `build`
   (read-only, no secret: build, archive, checksum, verify), then `release`
   (environment `release`): verify again, plan the release (prerelease and
   tap, `command: plan`), rehearse the tap pull request (dry run), attest
   provenance, publish the GitHub release (generated notes), open the tap
   pull request.
5. Edit the release notes on GitHub if needed.
6. Review and merge the pull request on `magicdrive/homebrew-tap`, then
   `brew update && brew upgrade ark && ark --version`.

A tag with a pre-release part (`v1.2.3-rc.1`) is published as a prerelease
and does not update the tap: the plan says `tap=skip`, the two steps that use
the tap token (`Rehearse the formula and tap pull request (read-only)`,
`Generate the formula and open a tap pull request`) are skipped and never
receive it, and `make publish` checks that the tap is untouched.

`make preflight tag=vX.Y.Z` (strict `hephaestus preflight`) is still there;
`make publish-check` runs it as part of its preflight.

## Recovery

Interrupting `make publish` (Ctrl-C, a lost connection, a timeout) stops
the monitoring, not the release; it exits 3 (outcome unknown):

```sh
make publish-status vX.Y.Z   # tag, run, release, assets, attestations, tap (read-only)
make publish-watch vX.Y.Z    # find and follow the run again, then verify
```

- Running `make publish` again for a version whose tag exists is refused by
  the preflight; use `publish-status` and `publish-watch`.
- A failed `ci` or `build` job: nothing was published. Fix the cause on
  `main` and release the next version, or, for a transient failure, re-run
  the failed jobs.
- A failed `release` job: re-run it. Hephaestus resumes a draft, skips
  identical assets and never replaces a published asset.
- Release published, tap pull request missing: re-run the `release` job; the
  release is a no-op and the pull request is opened.
- `hephaestus status --tag vX.Y.Z` reports `conflict`: do not patch the
  release; release the next version.

Never move or delete a pushed tag. Hephaestus's own guides:
[publish](https://github.com/magicdrive/hephaestus/blob/v1.2.0/docs/publish.md#recovery),
[recovery](https://github.com/magicdrive/hephaestus/blob/v1.2.0/docs/recovery.md).

## Rolling back the migration

The migration from GoReleaser is one pull request; reverting it restores
`.goreleaser.yml` and the GoReleaser workflow (which needs the
`PERSONAL_GITHUB_TOKEN` secret).
