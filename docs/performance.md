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
