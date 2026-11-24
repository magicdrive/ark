# gotreesitter v0.58.0 migration

PR #120 replaces the upstream production parser pool with bounded exclusive
Ark-owned leases to preserve the explicit legacy route.

## Measurements

Small serial fixtures, five-run medians, same runner:
[run 45343789102](https://github.com/magicdrive/ark/actions/runs/45343789102).

| Language | v0.56 pooled ns/op | v0.58 pooled ns/op | v0.58 fresh ns/op | Old/new/fresh B/op |
| --- | ---: | ---: | ---: | --- |
| Go | 22721 | 23626 | 28108 | 1144 / 1339 / 7416 |
| PHP | 21290 | 21636 | 25883 | 1184 / 1365 / 6976 |
| TypeScript | 35696 | 36603 | 42907 | 4117 / 4373 / 12232 |

Parallel Go fixtures, three-run medians:
[run 45349492895](https://github.com/magicdrive/ark/actions/runs/45349492895).

| Fixture | GOMAXPROCS | Base ns/op | PR ns/op |
| --- | ---: | ---: | ---: |
| Small | 1 | 29579 | 27725 |
| Small | 4 | 13664 | 13692 |
| Small | 8 | 14099 | 16499 |
| Medium | 1 | 6204967 | 5583780 |
| Medium | 4 | 2616541 | 2520578 |
| Medium | 8 | 2646746 | 2555050 |
| Large | 1 | 55073120 | 51644737 |
| Large | 4 | 81206222 | 76013359 |
| Large | 8 | 85716607 | 86962594 |

Small at P=8 regressed by 17%; profile real repositories before changing
pool capacity. Large inputs bypass the new pool. Dependency and pool changes
are confounded in the base/head comparison. These synthetic workloads do
not establish end-to-end indexing performance.

The opt-in heap probe performed 6144 Go parses with GC every 1024.
HeapAlloc changed from 12198272 to 12196016 bytes in
[run 45348027238](https://github.com/magicdrive/ark/actions/runs/45348027238),
and from 12200240 to 12204576 bytes in run 45349492895.
This shows stable live heap at sampled lease-cycle boundaries, not a proof
of leak freedom or a peak-memory bound. Sampling every 1024 parses aligns
with the 32-use retirement cycle and does not measure a live idle lease.
The separate idle-heap probe samples at age 31 and after clearing the pool.

## Idle lease retention audit

[Run 45354764511](https://github.com/magicdrive/ark/actions/runs/45354764511)
measured six cycles of 31 Go parses, followed by GC with the idle parser
retained and again after clearing its pool entry.

| Cycle | Idle HeapAlloc (bytes) | Cleared HeapAlloc (bytes) |
| --- | ---: | ---: |
| 1 | 22484776 | 12553968 |
| 2 | 17525368 | 12554000 |
| 3 | 17525496 | 12554112 |
| 4 | 17525616 | 12554112 |
| 5 | 17529080 | 12554112 |
| 6 | 17525632 | 12554128 |

The steady-state difference was about 4.74 MiB; the first cycle retained
about 9.47 MiB more than the cleared measurement. Cleared HeapAlloc grew
by only 160 bytes across the six cycles. These are process-wide post-GC
measurements, not exact per-parser accounting or a bound across languages,
inputs or concurrent leases. They do not measure peak RSS.

The expanded ownership test compares node types, spans, points and source
slices across Go, PHP and TypeScript under concurrent reuse. All 28 normal
CI jobs passed in [run 45354764336](https://github.com/magicdrive/ark/actions/runs/45354764336).

## Reproduction

    go test ./internal/tsparse -run '^$' -bench '^BenchmarkProductionPool$' -benchmem -benchtime=300ms -count=5 -cpu=1
    go test ./internal/tsparse -run '^$' -bench '^BenchmarkMigrationWorkload$' -benchmem -benchtime=300ms -count=3 -cpu=1,4,8
    ARK_POOL_HEAP_PROBE=1 go test ./internal/tsparse -run '^TestProductionPool_HeapPlateau$' -count=1 -v

    ARK_POOL_HEAP_PROBE=1 go test ./internal/tsparse -run '^TestProductionPool_IdleHeap is not a permanent CI gate.

## Cache compatibility

Invalidate extraction caches for Go, Python, TypeScript, PHP, Rust, Zig,
Java and Terraform. JavaScript and HTML inherit the TypeScript version.
The serialized cache schema is unchanged.
 -count=1 -v

The temporary migration workflow is not a permanent CI gate.

## Cache compatibility

Invalidate extraction caches for Go, Python, TypeScript, PHP, Rust, Zig,
Java and Terraform. JavaScript and HTML inherit the TypeScript version.
The serialized cache schema is unchanged.
