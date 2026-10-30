# install.sh: the release installer

`install.sh` at the root of this repository installs ark from its GitHub Releases. Users
should read the README ("Install script"); this page is for maintainers.

## Where it comes from

The script is the installer of [Enma](https://github.com/magicdrive/enma)
(`install.sh` at commit f2b0b1fe661b49f86b59cb697097aea5b7669a62), unchanged except for the project configuration:

```sh
APP_NAME="ark"
GITHUB_URL="https://github.com/magicdrive/ark"
```

Its design, security model and the distribution contract it relies on are described in Enma's
[docs/installer.md](https://github.com/magicdrive/enma/blob/f2b0b1fe661b49f86b59cb697097aea5b7669a62/docs/installer.md).
When Enma's script changes, copy it again and set these lines back; do not add ark-specific
logic to it.

## The distribution contract, as ark meets it

With `TAG` the release tag and `VERSION` = `TAG` without the leading `v`:

| What | ark |
|---|---|
| Tag | `vMAJOR.MINOR.PATCH`, optionally `-PRERELEASE` |
| Latest | `https://github.com/magicdrive/ark/releases/latest` redirects to the latest stable release |
| Archives used | `ark_VERSION_OS_ARCH.tar.gz` for `darwin` and `linux`, `amd64` and `arm64` (`hephaestus.yaml`, `archives`) |
| Checksums | `ark_checksums.txt` (`checksum.name` in `hephaestus.yaml`), `<sha256>  <file>` lines |
| Archive content | the binary `ark` at the top of the archive (other files are ignored) |
| Version output | the first line of `ark --version` is `ark version vVERSION` |

Every ark release so far (v0.0.2 to v6.8.0) meets it: installing v0.0.2, v1.0.0, v4.0.0 and v6.7.0 from github.com with the script was checked, and `TestRealReleases` installs v6.5.0 and the latest release.

Changing any of these in `hephaestus.yaml` or in the binary breaks the installer. CI checks
them on every pull request: the `release-snapshot` job installs the archives it has just built with
`install.sh` (`TestDistArchives`).

## Tests

`internal/installer` (Go, `go test ./internal/installer/`; not on Windows) runs the real
`install.sh` against a fake github.com: a local TLS server with a test CA, reached through a
local HTTPS proxy, with releases built by the tests. No network is needed. They cover install,
reinstall, upgrade, downgrade and pre-releases; the command line; the platforms; wrong
`releases/latest` answers; HTTP errors, truncated and stalled downloads, refused connections and
untrusted certificates; the checksum file; unsafe archives; the install directory, Homebrew and
links; failing tools; concurrent and interrupted runs; and the `sh install.sh` lines of the
README (`TestReadmeCommands`).

Variables:

- `ARK_INSTALLER_SHELLS` (e.g. `sh dash bash bash-nonposix ksh busybox`) and
  `ARK_INSTALLER_DOWNLOADERS` (`curl wget`) name the shells and downloaders that must be
  tested; one that is missing fails the tests. Without them, those found are used.
- `ARK_INSTALLER_BUSYBOX`: a busybox for the BusyBox userland tests, or `required`.
- `ARK_INSTALLER_DIST=<dir>` runs `TestDistArchives` on the `dist` directory of
  `make snapshot`.
- `ARK_INSTALLER_NETWORK=1` runs `TestRealReleases`, which installs real ark releases from
  github.com (not run in CI).

`make shellcheck` checks the script with ShellCheck (`-s sh`). CI runs it with the ShellCheck
of the Ubuntu runner image and prints its version, which changes with the image (0.9.0 and
0.11.0 have both been seen); the script has no warning with either.

## Known limits

Found in a review of the script before it was copied here; none of them lets a download that
fails its checks be installed:

- A signal sent to the script's process alone (not to its process group, as Ctrl-C does) is
  handled only when the running `curl` or `wget` ends: at the latest after `INSTALLER_TIMEOUT`
  seconds without data, but a slow download that keeps going is not interrupted.
- The size of a download is not limited (the archive's SHA-256 is checked before it is
  decompressed).
- With `umask 000`, a new `--dir` is created writable by everyone and then refused; the
  directories created stay.
- A directory writable by its group trusts the group's members (as documented by Enma), also for
  the moment between the last check and the rename. Access control lists are not checked.
- `TMPDIR` is trusted: point it at a directory only you can write to (the default is fine).
