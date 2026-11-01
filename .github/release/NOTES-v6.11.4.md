# Ark v6.11.4 — Deterministic file stability test

Ark v6.11.4 makes a test that failed the v6.11.3 release CI on a busy runner deterministic, so that later releases are not held up by it. Only a test and these notes change: the `ark` binary behaves exactly as v6.11.3 — no CLI, MCP, index, cache or configuration change.

## CI reliability

`internal/fsroot`'s `TestReadStable_ConcurrentWriters` failed the v6.11.3 release CI twice on `macos-latest` with the race detector (run 41311999161; the third attempt passed). It ran a writer and a reader for one second of wall-clock time and required at least one *settled* read: a stable read whose file had not changed for the timestamp granularity (100 ms) before it began. The writer paused 150 ms after every 200 writes, so a read could settle only if it began in the last ~50 ms of a pause. On a slow or loaded runner, either no pause fell within the second or the reader was not scheduled within that window, and the test failed (0 settled reads in about 20,000) with nothing wrong in the code. The same failure was reproduced locally under CPU load (1 in 8 runs).

The test now has the writer and the reader take turns through channels and the existing read hook, not time. In each of its rounds:

1. The reader waits until the file is settled, then lets the writer write a burst and reads while it does. A read may fail with `ErrChanged` and no bytes (or find no file between rename steps); a read that succeeds and is settled — one that began before the burst's first change, which may coincide with any of the writes — must be a whole version written. How many reads settle is up to the scheduler, and none is required.
2. The writer stops and says which version it left. The reader reads it again after `Stamp.SettleTime` — the same wait `accesspolicy` uses before confirming a rule file — and the read must be settled and that version.
3. The writer writes once while the reader reads the settled file, between the read's first `fstat` and its first read system call, and does not write again until the next round. In place, to other bytes of the same size with the modification time restored, so that only the change time tells, the read must fail with `ErrChanged` and return no bytes. By rename to another version, it must fail or return the old version whole.

Steps 2 and 3 happen in every round of every run, whatever the scheduler does; a slow runner only makes the waits longer.

### Guarantees kept

- A stable read never returns a torn or partial file as a success: a change it can detect (object, size, modification or change time, bytes read) is `ErrChanged` with no bytes. This is now checked deterministically against an in-place rewrite that keeps the size and the modification time (step 3). Under the burst's unscripted writes (step 1), the test checks the kind of every error, that `ErrChanged` carries no bytes, and the content of every settled read.
- A settled read is a whole version, and an unchanged file is read settled after its `SettleTime`.
- `fsroot` has no retry of its own; a stable read is a single pass that ends with the content or `ErrChanged`. Callers retry (`accesspolicy`), unchanged.

### Guarantee removed

- The old test required that *some* read settle within one second while the writer ran. That is a property of the scheduler, not of the code, and nothing in Ark promises it; it is replaced by the settled read of step 2, which is required in every round. The old test met a write during a settled read by chance, some thousands of times a run; step 3 makes one at the first read system call in every round (writes at later points of a read are `TestReadStable_TornByInPlaceRewrite`'s), and step 1 still meets them by chance, fewer times.

### Verified

- Eleven mutations of the stable read are each detected by the package's tests, applied to a scratch copy and never committed:
  - skipping the stamp comparison, the size check, or both;
  - comparing without the change time;
  - returning partial bytes, or no error, on a change;
  - skipping the second `fstat`;
  - a `Settled` that is always false, always true, or ignores the granularity;
  - an early `SettleTime`.

  The rewritten test alone detects eight of them in every run. The other three are the size check (a real file cannot change its size and keep its stamp), the always-true `Settled` and the granularity; the existing deterministic tests (`TestReadStable_Checks`, `TestStamp_Settled`) catch those. Independent audits ran further mutations; the old test detected one of the eleven.
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
