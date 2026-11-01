# Ark v6.11.4 — Deterministic file stability test

Ark v6.11.4 makes a test that failed CI on a busy runner deterministic. Only test code changes: the `ark` binary behaves exactly as v6.11.3 — no CLI, MCP, index, cache or configuration change.

## CI reliability

`internal/fsroot`'s `TestReadStable_ConcurrentWriters` failed the v6.11.3 release CI twice on `macos-latest` with the race detector (run 41311999161; the third attempt passed). It ran a writer and a reader for one second of wall-clock time and required at least one *settled* read: a stable read whose file had not changed for the timestamp granularity (100 ms) before it began. The writer paused 150 ms after every 200 writes, so a read could settle only if it began in the last ~50 ms of a pause. On a slow or loaded runner, either no pause fell within the second or the reader was not scheduled within that window, and the test failed (0 settled reads in about 20,000) with nothing wrong in the code. The same failure was reproduced locally under CPU load (1 in 8 runs).

The test now has the writer and the reader take turns through channels and the existing read hook, not time. In each of its rounds:

1. The writer writes a burst while the reader reads. Any read may fail with `ErrChanged` (or find no file between rename steps); a read that succeeds and is settled must be a whole version written.
2. The writer stops and says which version it left. The reader reads it again after `Stamp.SettleTime` — the same wait `accesspolicy` uses before confirming a rule file — and the read must be settled and that version.
3. The writer writes once while the reader reads the settled file, between the read's first `fstat` and its first read system call, and waits for that read to end. In place, to other bytes of the same size with the modification time restored, so that only the change time tells, the read must fail with `ErrChanged` and return no bytes. By rename, it must fail or return the old version whole.

Every step happens in every run, whatever the scheduler does; a slow runner only makes the waits longer.

### Guarantees kept

- A stable read never returns a torn or partial file as a success: a change it can detect (object, size, modification or change time, bytes read) is `ErrChanged` with no bytes. This is now checked deterministically against an in-place rewrite that keeps the size and the modification time, as well as under unscripted concurrent writes.
- A settled read is a whole version, and an unchanged file is read settled after its `SettleTime`.
- `fsroot` has no retry of its own; a stable read is a single pass that ends with the content or `ErrChanged`. Callers retry (`accesspolicy`), unchanged.

### Guarantee removed

- The old test required that *some* read settle within one second while the writer ran. That is a property of the scheduler, not of the code, and nothing in Ark promises it; it is replaced by the settled read of step 2, which is required in every round.

### Verified

- Mutations of the stable read are each detected by the package's tests, with the change written to a scratch copy and never committed. The mutations: skipping the `fstat` comparison or the size check, comparing without the change time, returning partial bytes or no error on a change, skipping the second `fstat`, a `Settled` that is always true, always false or ignores the granularity, and an early `SettleTime`. The rewritten test alone detects eight of them deterministically. The others are caught by the existing deterministic tests (`TestReadStable_Checks`, `TestStamp_Settled`).
- `go test ./internal/fsroot -count=100`, `go test -race ./internal/fsroot -count=30`, the test 30 times with the race detector under CPU load, and `go test ./...` and `go test -race ./...` pass.

## Known Limitations

- The undetectable cases documented for stable reads are unchanged (a single write in progress before the read began, writes through a shared mapping, clock changes, file systems without a usable change time).
- On a file system with whole-second times, the test runs two rounds instead of eight, each waiting more than two seconds to settle.
- On Windows a rename over an open file may fail; the test then checks that the old version is read whole.

## Installation / Upgrade

```sh
brew upgrade ark            # or: brew install magicdrive/tap/ark
# or, without Homebrew (macOS, Linux):
curl -fsSLO https://raw.githubusercontent.com/magicdrive/ark/main/install.sh
sh install.sh               # sh install.sh --upgrade to upgrade
```
