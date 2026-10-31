# Ark v6.10.0 — YAML and GitHub Actions

Ark v6.10.0 adds **YAML support** and **GitHub Actions architecture discovery**: an agent can list a repository's workflows, a workflow's jobs, a job's steps and the jobs it needs, find the steps that use an action, the jobs that call a reusable workflow, and what a change to a composite action affects — from the index, without reading the YAML in full.

The guiding principle is unchanged: **resolve what the file can establish, and preserve uncertainty where it cannot.** Ark never runs a workflow, never evaluates an expression (`${{ … }}`), and never fetches an action.

## Highlights

### YAML

`.yaml` and `.yml` files are now indexed (read by go.yaml.in/yaml/v3, a new dependency; pure Go):

- The file itself, and the keys of its mappings up to three levels deep, identified and named by their JSON Pointer: `config/app.yaml#/server/tls` (name `/server/tls`), `config/app.yaml#1/second` in the second document. A data key never shares a bare name with a declaration of code.
- Anchors and aliases: each alias is a `references` edge to the anchor definition it names (merge keys included).
- Multiple documents (an error loses only its document), comments, Unicode, and source locations in bytes, as for every language.
- Diagnostics for syntax errors, duplicate keys, UTF-16/UTF-32 files and files beyond the reader's bounds (4 MiB; 5000 declarations, 20000 references and 1000 diagnostics a file). Aliases are never expanded, and content an alias repeats is read within the same bounds, so an alias bomb costs nothing.

### GitHub Actions

For workflows (`.github/workflows/*.yml`, `*.yaml`) and actions (`action.yml` / `action.yaml`):

- Kinds `workflow`, `trigger` (each event of `on`), `job`, `step`, `action` (by its directory), and `key` for the settings: `permissions`, `concurrency`, `env`, `defaults`, a job's `if`, `strategy`, `environment`, `outputs`, `container`, `services`, `with`, `secrets`, an action's `inputs` and `outputs`.
- Identities: `.github/workflows/ci.yml::build::Checkout` (a step by its `id`, else `name`, else `uses` without its `@ref`, so a Dependabot or Renovate update keeps it), triggers `.github/workflows/ci.yml::on:push`.
- Job and step ids are case-insensitive, as GitHub reads them (`needs: Build` is the job `build`).
- Relations: **`contains`** (a new edge kind: a workflow's triggers and jobs, a job's or a composite action's steps; `contained_by` in reverse), **`depends_on`** for `needs` (string or list form), and **`calls`** for `uses` — a local reusable workflow **exact**, a local action **strong** (its path is relative to the runner's workspace). Remote actions and workflows (`actions/checkout@v4`, `owner/repo/.github/workflows/x.yml@v1`) and `docker://` images are reported as outside the repository; a `uses` with an expression as a dynamic name.
- Content reached through YAML anchors and aliases (`steps: *common`) is read as GitHub reads it.
- Diagnostics: `needs` cycles, invalid `uses` (including a local reusable workflow outside `.github/workflows`), job ids GitHub does not accept, merge keys (no GitHub Actions syntax), duplicate job and step ids.

The structure Ark reads (triggers, jobs, needs and every step's `uses`) was compared with an independent reader, Ruby's Psych (libyaml), on the 16 workflows of five real repositories (83 jobs, 427 steps): byte-for-byte equal. Its diagnostics agree with actionlint on the test fixtures, and three rounds of independent audit, fuzzing and mutation testing preceded the release.

```text
search_code       kind=workflow                      → the workflows
get_callees       ci.yml::test                       → its steps (contains) and needs (depends_on)
find_references   actions/checkout                   → the steps using it
get_callers       .github/workflows/reusable.yml     → the jobs calling it
get_callers       .github/actions/setup              → the steps using the composite action
analyze_change_impact .github/actions/setup          → steps, jobs and workflows it affects
```

YAML support is at the **graph** level (not `context_quality_certified`). `yaml` is accepted wherever a language is named (`--lang yaml`, the `language` / `lang` arguments of MCP tools).

## Changed

- **Indexing:** `.yaml` and `.yml` files are now indexed, and the index now enters `.github` (at any depth) — for YAML files only; scripts there are still not indexed, and every other hidden directory is still skipped. A directory below `.github` given as the index root (a JavaScript action's code) is indexed as any directory. Repositories with YAML files show more symbols in `search_code`, `get_repository_map` and `get_project_stats`, and indexing takes somewhat longer (docker/docker: 8.9 s → 9.9 s, peak memory unchanged).
- **Graph:** a new edge kind `contains` (reverse `contained_by`), and the reference kind `contains` for `find_references`. It is stated only for YAML files.
- **Repository map:** a package is named by the language of its code, not of the YAML beside it (a Go root package with `action.yml` and `.golangci.yml` stays `go`).
- **Secret masking of names (MCP):** symbol names, and the evidence built from them, are now masked where they hold a secret of its own form (a PEM block or a known token format such as `ghp_…`, `AKIA…`) — a name can be data, such as a YAML key or a step name. File paths, symbol IDs and kinds are still never altered, and a declaration whose name is masked is still selected by its `symbolId`. The text of `search_code` and `get_repository_map` is masked the same way, and is now refused, like every result with repository text, while the secret-rules configuration cannot be read or compiled (only `list_files` still answers then).
- **Reference IDs** (`find_references`' opaque `id`) now also depend on the reference's container, so two references at one place in two containers (what a YAML alias repeats in two steps) are both kept. IDs were never stable across Ark versions.
- **`ark secrets-scan`:** YAML files are treated as configuration, not source code, in the suggestions (as Terraform, HTML and CSS): a YAML file dense with findings is suggested for `.arkignore`, as before this release.

## Compatibility

- No CLI flag, MCP tool, argument or response field was removed or renamed; `yaml` is added to the accepted languages, `contains` / `contained_by` to the edge kinds, and `document` / `key` / `anchor` / `workflow` / `trigger` / `job` / `step` / `action` to the symbol kinds.
- Results for existing languages are unchanged, except: a name that holds a secret token is masked in MCP results (above); reference IDs differ from v6.9.0's; and a bare name that a GitHub Actions job or step shares (a job `release` beside a function `release`) is now ambiguous — select with `filePattern` or `symbolId`.
- The cache format is unchanged (schema 2); no migration is needed.
- Built with Go 1.27.3 for the same eight platforms as before.

## Known Limitations

- Expressions are text: `if:` conditions, `${{ matrix.* }}`, `needs.<job>.outputs` and `steps.<id>.outputs` are not evaluated or resolved, and a job's `with:` is not linked to a reusable workflow's inputs.
- A step's `uses: ./path` is resolved at **strong** at most: the workspace a checkout makes decides it. A directory with both `action.yml` and `action.yaml` leaves both candidates.
- A remote action or workflow, also one of the repository itself at another ref, is outside the repository.
- Generic YAML files state keys up to three levels deep and none below a sequence; schemas other than GitHub Actions (Kubernetes, Compose, OpenAPI) are not interpreted. Files over 4 MiB declare only themselves; a file states at most 5000 declarations.
- `ark syntax` prints no syntax tree for YAML (no Tree-sitter grammar); `ark symbol` and every MCP tool work.
- Whether a file is a workflow is decided from its path below the indexed directory: index the repository root, not `.github`, for workflows to be read as workflows.
- On Windows, local reusable workflows and actions resolve by the index's own path spelling; this was verified by simulation in tests, not on Windows hardware.
- A pathological YAML file (4 MiB of one flow sequence) can take several hundred MB while it is read; files over 4 MiB are not read.

Details: [YAML and GitHub Actions in language support](https://github.com/magicdrive/ark/blob/v6.10.0/docs/language-support.md#yaml-and-github-actions) and [the design record](https://github.com/magicdrive/ark/blob/v6.10.0/docs/development/yaml.md).

## Installation / Upgrade

```sh
brew upgrade ark            # or: brew install magicdrive/tap/ark
# or, without Homebrew (macOS, Linux):
curl -fsSLO https://raw.githubusercontent.com/magicdrive/ark/main/install.sh
sh install.sh               # sh install.sh --upgrade to upgrade
```
