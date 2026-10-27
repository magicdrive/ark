# Performance

This page reports what Ark's MCP server costs in time and memory, how it was
measured, and how to measure it on your own repository. It makes no claim
about how many tokens or how much time an agent saves by using Ark: that
depends on the agent, the model and the task, and Ark has no reproducible
measurement of it.

## What is measured

| Measurement | What it covers |
|---|---|
| **Cold** | The first graph request of a server started with `--no-cache`: walk the repository, parse and extract every source file, resolve every reference, build the graph. |
| **Warm** | A later request on the same server: re-fingerprint the sources (read and hash every source file), reuse the index, answer. |
| **Restore** | The first request of a new server process with a populated extraction cache: read the cached extractions, resolve, build the graph. |
| **Peak RSS** | Peak resident memory of the cold server process. |

All three timings use `get_diagnostics`, whose answer is small, so they
measure index work rather than output size. Other tools add their own
(usually small) query cost on top of the warm figure.

## Results

Ark v6.0.0 (the release candidate: commit `c1c76d3` plus the MCP secret
masking and `.arkignore` access policy), five runs per repository, median
with [min–max]. Times in milliseconds.

| Repository (version) | Language | Files indexed | Cold | Warm | Restore | Peak RSS |
|---|---|---:|---:|---:|---:|---:|
| [ky](https://github.com/sindresorhus/ky) v1.7.2 | TypeScript | 34 | 144 [143–145] | 2 [2–3] | 21 [21–21] | 50 MB |
| [requests](https://github.com/psf/requests) v2.32.3 | Python | 36 | 267 [258–272] | 4 [4–4] | 36 [34–38] | 79 MB |
| [guzzle](https://github.com/guzzle/guzzle) 7.9.2 | PHP | 41 | 279 [277–297] | 2 [2–3] | 24 [23–26] | 78 MB |
| [terraform-aws-vpc](https://github.com/terraform-aws-modules/terraform-aws-vpc) v5.13.0 | Terraform | 57 | 290 [288–307] | 4 [4–4] | 58 [57–60] | 53 MB |
| [Slim](https://github.com/slimphp/Slim) 4.14.0 | PHP | 72 | 211 [206–218] | 3 [3–3] | 32 [31–34] | 74 MB |
| [terraform-aws-eks](https://github.com/terraform-aws-modules/terraform-aws-eks) v20.24.0 | Terraform | 76 | 298 [285–300] | 6 [5–6] | 52 [50–54] | 43 MB |
| [express](https://github.com/expressjs/express) 4.21.1 | JavaScript | 152 | 402 [396–423] | 10 [9–10] | 82 [79–84] | 56 MB |
| [zod](https://github.com/colinhacks/zod) v3.23.8 | TypeScript | 170 | 2,364 [2,308–2,412] | 9 [8–9] | 183 [181–211] | 162 MB |
| Ark (`c1c76d3`) | Go | 600 | 3,780 [3,644–3,883] | 41 [39–44] | 326 [321–351] | 246 MB |
| [golang.org/x/tools](https://github.com/golang/tools) v0.36.0 | Go | 1,875 | 18,545 [18,522–18,878] | 138 [128–142] | 1,704 [1,678–1,767] | 959 MB |

Environment: Intel Xeon W-2140B (8 cores, 16 threads), 32 GB RAM, macOS 15.7.9,
Go 1.27.1 (`go build` of the commit), repositories on a local SSD, no other
load. Each repository is the GitHub release archive of the tag shown; Ark is
the source tree of commit `c1c76d3`.

### Rust

Rust support is newer than the table above and was measured separately: the
`feature/rust-language-support` working tree (based on `656da7b`), the same
machine and script, five runs per package. Each package is its crates.io
release as Cargo downloads it (a copy, since the script writes the cache into
it).

| Package (version) | Files indexed | Cold | Warm | Restore | Peak RSS |
|---|---:|---:|---:|---:|---:|
| [quick-error](https://crates.io/crates/quick-error) 1.2.3 | 2 | 430 [421–488] | 1 [1–1] | 5 [4–6] | 96 MB |
| [regex](https://crates.io/crates/regex) 1.3.4 | 62 | 165 [156–167] | 5 [5–6] | 42 [41–44] | 117 MB |
| [tokio](https://crates.io/crates/tokio) 0.2.13 | 372 | 311 [310–368] | 21 [21–26] | 181 [177–183] | 147 MB |
| [syn](https://crates.io/crates/syn) 1.0.16 | 79 | 1,322 [1,303–1,373] | 8 [7–8] | 169 [162–170] | 262 MB |
| [serde](https://crates.io/crates/serde) 1.0.104 | 19 | 1,919 [1,718–2,073] | 4 [4–5] | 62 [55–113] | 263 MB |

Most of the cold time is parsing. A file the parser's production route
rejects (Rust 2015 `try!(...)`, which the grammar reads as the `try`
keyword) is parsed again by fallback routes; the last of them, the forest
route, could take tens of seconds on files dense with attributes and doc
comments, and then fail. A probe now decides first whether it can succeed
([Architecture](../ARCHITECTURE.md), "One parse path"): before it, the same
measurement gave quick-error 38,968 ms cold (157 MB) and serde 19,987 ms
(864 MB). Over 4,810 files of 256 crates.io packages, parsing takes 38.5 s
in all; the slowest file is a 2.6 MB generated table (3.2 s), and 7 files
still run the forest route (those without a comment the probe can blank),
none among the ten slowest.

### Python

Python's graph support (scopes, import bindings, members) is newer than the
table above and was measured separately, before and after it: the commit
`54be77b` and the `feat/python-graph` working tree based on it, on the same
machine and script, with no other process running, alternating the two
builds — three runs each (Django five), median with [min–max]. Each package
is its PyPI sdist (a fresh copy per run, since the script writes the cache
into it); files indexed include the documentation and test sources the
archive ships.

| Package (version) | Files indexed | Cold before → after | Warm before → after | Restore before → after | Peak RSS before → after |
|---|---:|---:|---:|---:|---:|
| [requests](https://pypi.org/project/requests/) 2.32.3 | 34 | 194 [193–209] → 217 [206–690] | 4 → 5 | 34 [31–113] → 48 [47–49] | 102 → 116 MB |
| [attrs](https://pypi.org/project/attrs/) 24.2.0 | 53 | 134 [132–135] → 173 [172–179] | 5 → 5 | 41 [40–43] → 83 [80–92] | 92 → 120 MB |
| [click](https://pypi.org/project/click/) 8.1.7 | 71 | 155 [154–165] → 172 [172–186] | 7 → 6 | 55 [53–56] → 81 [80–82] | 102 → 120 MB |
| [flask](https://pypi.org/project/Flask/) 3.0.3 | 104 | 159 [149–162] → 186 [181–193] | 11 → 11 | 62 [60–63] → 96 [92–97] | 129 → 148 MB |
| [Django](https://pypi.org/project/Django/) 4.2.16 | 3,207 | 6,179 [6,041–7,896] → 6,706 [6,640–7,135] | 469 → 474 | 4,388 [3,948–5,406] → 4,456 [4,440–4,525] | 507 → 1,111 MB |

The index holds more since the change — Django's Python symbols went from
8,569 to 56,307 (methods, nested functions, variables, attributes) and its
graph edges from 1,434 to 36,593 — so the peak memory of a large repository
rose most (Django ×2.2). A heap profile after the build attributes the
retained growth (148 → 394 MB) to the references now attributed to a
container symbol (methods are symbols: 60 MB), the symbol indexes by name,
file, qualified name and kind (77 MB, in proportion to the symbols) and the
resolutions kept by target (37 MB); the Python provider's own module
candidates are a few percent of what is allocated.

### Zig

Zig support was measured separately on the `feat/zig-graph` working tree,
on the same machine and script, three runs each, median with [min–max].
Each repository is a shallow clone of the pinned commit of the
[evaluation record](development/zig.md) (a fresh copy, since the script
writes the cache into it); files indexed include every language the
repository holds (TigerBeetle's client libraries in Go, Java, Python and
others).

| Repository (commit) | Files indexed | Zig files | Cold | Warm | Restore | Peak RSS |
|---|---:|---:|---:|---:|---:|---:|
| [TigerBeetle](https://github.com/tigerbeetle/tigerbeetle) `0450c32` | 368 | 248 | 2,274 [2,253–2,298] | 49 [46–59] | 1,113 [1,105–1,116] | 716 MB |
| [Ghostty](https://github.com/ghostty-org/ghostty) `83fb956` | 832 | 814 | 6,405 [5,944–6,415] | 142 [139–143] | 2,639 [2,624–2,658] | 1,056 MB |

Their Zig sources are large — 6.4 MB and 13.7 MB, files up to 570 and
730 KB — and densely cross-referenced: 87,000 and 233,000 references, of
which 41,000 and 90,000 become graph edges. A heap profile of the
TigerBeetle build attributes the allocations to the index's work per
reference (resolution, edges, completeness, reference identities) in
proportion to their number; the Zig provider's own share is the reference
drafts it emits (about a tenth).

### Reading the results

- **File count is a poor predictor.** Cost depends on the amount of source
  and on the language: zod (170 files, 0.9 MB of TypeScript) takes about six
  times as long as express (152 files, 0.6 MB of JavaScript).
- **The first request pays the cold cost once per server process;** the cache
  turns the next process's first request into the restore cost (5–13 times
  faster here). Clients that start one server per session pay restore, not
  cold, from the second session on.
- **Warm requests are dominated by the walks** that prove the index is
  current and read the `.arkignore` rules. They grow with the repository and
  are independent of the question.
- **Memory** is held for the life of the server: up to three indexes per
  process ([Operations](operations.md#index-lifecycle)).

### Access policy cost

Enforcing `.arkignore` ([SECURITY.md](../SECURITY.md#file-access-policy-arkignore))
reads the rule files on every request. Warm latency per tool, median of 9
runs on one server (milliseconds):

| Repository | `get_diagnostics` | `list_files` | `search_in_files` | `get_file_content` |
|---|---:|---:|---:|---:|
| Ark (one `.arkignore` pattern) | 34.2 | 90.0 | 112.4 | 0.4 |
| golang.org/x/tools (no `.arkignore`) | 119.6 | 105.3 | 73.5 | 0.5 |
| x/tools with 300 nested `.arkignore` files (900 patterns) | 192.1 | 309.0 | 135.5 | 0.5 |
| *same, build without `.arkignore` enforcement* | | | | |
| Ark | 26.8 | 103.3 | 120.3 | 0.2 |
| golang.org/x/tools | 108.0 | 90.5 | 72.8 | 0.4 |
| x/tools, 300 nested `.arkignore` files | 103.8 | 2,490.9 | 733.3 | 0.3 |

- A tool that names one path reads only the rule files above it: no walk.
- Index tools walk the repository once per request: the walk that reads the
  rule files also lists the sources the freshness fingerprint reads. Sharing
  it cut warm `get_diagnostics` from 43.4 to 34.2 ms on Ark, 137.1 to
  119.6 ms on x/tools and 215.3 to 192.1 ms on the nested corpus, and the
  first request after a rule edit by the same amount. Compiling the rules
  takes microseconds and is skipped while their SHA-256 contents are
  unchanged. Index rebuilds and the file tools (`list_files`,
  `search_in_files`) are unchanged by this.
- Matching is indexed by the directory of each rule, which also speeds up
  the file tools' `.gitignore` filtering on repositories with many rule
  files.

## Reproducing

[`docs/scripts/ark-mcp-timing.py`](scripts/ark-mcp-timing.py) is the exact
procedure used above. It needs Python 3 and the `ark` binary; it starts the
server, sends JSON-RPC requests on standard input and prints one JSON line.

```bash
python3 docs/scripts/ark-mcp-timing.py "$(command -v ark)" /path/to/repository 5
```

```json
{"repository": "ky", "files_indexed": 34, "cold_ms": "144 [143-145]", "warm_ms": "2 [2-3]", "restore_ms": "21 [21-21]", "peak_rss_mb": 50, "runs": 5}
```

The script writes the extraction cache into `<repository>/.ark/index` for the
restore runs and deletes it afterwards. Timings vary with CPU, disk and file
system cache; compare versions on the same machine.

The parser changes behind the Rust figures are checked by opt-in tests, not
run in CI:

```bash
# Every file: Parse gives the route and tree it gave before the probe and the
# parser pool (one "language:extension:directory" per corpus).
ARK_TSPARSE_CORPUS="rust:.rs:$HOME/.cargo/registry/src/<index>;go:.go:/path/to/go" \
  go test ./internal/tsparse -run TestProbe_Corpus -v -timeout 3h
# Rust packages: counts, time, heap high-water mark, determinism, crate boundaries.
ARK_RUST_CORPUS=/path/to/crate1:/path/to/crate2 \
  go test ./internal/languages/rust -run TestCorpus_External -v
```

`TestProbe_ScalesLinearly` and `TestExtractScalesLinearly` run in CI: four
times the input must not cost more than ten times the time (a quadratic or
cubic cost does).

## Token figures

Every token budget and count in Ark (`maxTokens`, `estimatedTokens`, the
context `stats` line) is an estimate, `len(text) / 4` over the returned text.
It is not a model's tokenizer, and it counts only tool responses — not the
agent's prompts, reasoning or other tool calls. Use it to keep responses
within a budget, not to compute cost.

`get_context` and `search_context` bound their responses by this estimate.
`get_context` always includes the target symbol, even when the target alone
exceeds the budget (the response then reports `TargetTruncated`).

The v6.0.0 release notes include a tool-level comparison of fixed
procedures on one repository. It is a regression check for Ark, not a
general measure of agent token use.
