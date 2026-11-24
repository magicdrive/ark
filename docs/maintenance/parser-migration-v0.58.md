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
of leak freedom or a peak-memory bound.

## Reproduction

    go test ./internal/tsparse -run '^$' -bench '^BenchmarkProductionPool$' -benchmem -count=5 -cpu=1
    go test ./internal/tsparse -run '^$' -bench '^BenchmarkMigrationWorkload$' -benchmem -count=3 -cpu=1,4,8
    ARK_POOL_HEAP_PROBE=1 go test ./internal/tsparse -run '^TestProductionPool_HeapPlateau$' -count=1 -v

The temporary migration workflow is not a permanent CI gate.

## Cache compatibility

Invalidate extraction caches for Go, Python, TypeScript, PHP, Rust, Zig,
Java and Terraform. JavaScript and HTML inherit the TypeScript version.
The serialized cache schema is unchanged.
