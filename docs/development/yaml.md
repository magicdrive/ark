# YAML and GitHub Actions support: design record and evaluation

This page records why YAML and GitHub Actions support is built the way it
is, what was measured, and how. The rules themselves are in the headers of
`internal/languages/yaml/provider.go`, `internal/languages/yaml/yamldoc/yamldoc.go`
and `internal/languages/yaml/actions/actions.go`, and in `ARCHITECTURE.md`
§1 and §4; users' documentation is in
[language support](../language-support.md#yaml-and-github-actions).

## Parser

- **Library.** go.yaml.in/yaml/v3 v3.0.5 (2026-07-26), the YAML
  organization's maintained successor of gopkg.in/yaml.v3 (a Go port of
  libyaml), MIT and Apache-2.0, pure Go. Its `yaml.Node` API gives every
  node with its kind, style, tag, anchor and alias, and position — the API
  actionlint also builds on. `govulncheck` reports nothing for it.
- **Not chosen.** The Tree-sitter YAML grammar bundled by gotreesitter
  parses the corpus below without an error tree but, on an invalid file,
  often makes the whole file one `ERROR` node, and reading scalar values
  (escapes, folding, block scalars) from its tree would mean implementing
  that part of YAML in Ark. goccy/go-yaml is another full implementation;
  yaml/v3 is the one the GitHub Actions tooling shares.
- **What the library does not give, and Ark adds** (`yamldoc`): where a node
  ends (the library reports a start only), byte offsets (it counts
  characters and YAML 1.1's line breaks — CR, NEL, LS and PS included — after
  a byte order mark), recovery per document (it stops at the first error of
  a stream: Ark splits the stream at its document markers, which YAML
  forbids anywhere else), duplicate keys (it builds the tree without a
  word), and a YAML 1.2 directive (`%YAML 1.2`, which it rejects; read as
  blanks). A UTF-16 or UTF-32 file is not read: the library would decode it,
  and its positions would not be the file's bytes.
- **Safety.** Aliases are never expanded (an alias node names its anchor;
  `Deref` follows a chain of at most 16); the library bounds nesting at
  10000; files over 4 MiB are not read; a file states at most 5000
  declarations, 20000 references (and reads as many needs) and 1000
  diagnostics — content an alias repeats is read within these bounds, so a
  needs list one alias repeats in every job costs no more than they allow.
  The library accepts an anchor whose node holds its own alias
  (`&a [*a]`): every walk keeps a visited set. Positions are converted per
  line once (an ASCII line by arithmetic, another by a table of its
  characters), so a file whose nodes share one line — minified JSON — is
  read in linear time.

## Model

- **Two layers.** `yamldoc` knows YAML; `actions` knows GitHub Actions and
  reads only `yamldoc`'s nodes. A file is a workflow by its path (directly
  in a `.github/workflows` directory, as GitHub reads it), an action by its
  name and content (`action.yml` / `action.yaml` with `runs`); any other
  YAML file is data.
- **Identities.** Keys are JSON Pointers (`<file>#/a/b`), GitHub Actions
  declarations `::` paths (`<file>::<job>::<step>`), so the two never
  collide (a job named `permissions` is no setting). A step's key is its
  `id`, else `name`, else `uses`, else `run`; the n-th step of one key in a
  job is `<key>#<n>`: inserting a different step keeps every identity, and
  every step has one of its own (a `contains` edge needs a unique target).
  An action is its directory: `uses: ./dir` names a directory, and the
  runner reads the `action.yml` or `action.yaml` in it.
- **Relations.** One generic reference kind was added, `contains`
  (`reference.KindContains` → `index.EdgeContains`, `contained_by` in
  reverse): a workflow's triggers and jobs, a job's and a composite
  action's steps are its structure, neither calls nor uses. `needs` is
  `depends_on` (as Terraform's), `uses` a call. A local reusable workflow
  is path evidence (`FileIdentity`): the file at the caller's commit, Exact.
  A local action is capped at Strong: `./` is the runner's workspace, which
  a checkout to another path, or of another repository, changes. A remote
  action or workflow and a docker image are outside the repository, an
  expression is a dynamic name, an invalid `uses` a diagnostic and no
  reference (it names nothing: as a reference without identity, it was
  attributed to the step of its own name).
- **Aliases** (GitHub supports anchors and aliases, not merge keys).
  Content an alias repeats is the job's own and is located at the alias;
  two references of one kind and name there would share a `ReferenceID`,
  and are one.
- **`.github`.** The index never entered hidden directories. It now enters
  `.github` and reads there only the files of a provider that reads it
  (`language.HiddenDirReader`: the YAML provider): a Python helper under
  `.github` would otherwise change which Python names are unique in the
  repository.
- **Names are data.** A YAML key, a step's `name`, `id` and `uses` are
  names, and names leave the MCP server as identifiers. So a text that is no
  one-line name (a block of text, a control character, over 256 bytes)
  declares nothing — it could forge a line of a text result —, a
  `docker://` reference's credentials are left out of names, keys are named
  by their pointer (`/a/b`, so no data key shares a bare name with code),
  and the server masks declaration names by the token rules (a PEM block, a
  known token format; never an assignment rule), which also covers the
  existing languages' names; paths, IDs and kinds stay unmasked. Script
  text and `with` / `env` values never enter a name or a signature
  (`TestExpressionsAndScriptsAreText`, `TestMCPYAML_NamesNeverLeakSecrets`).
- **Reference IDs.** A `ReferenceID` now includes the reference's
  container (`reference.NewReferenceIDIn`): two steps an alias repeats that
  use one action have two references at one place, which a (file, kind,
  name, span) identity merged — one step's relation was lost while its
  completeness read 0.
- **Case.** Job and step ids are case-insensitive as GitHub reads them:
  `needs` resolves by the folded id, and two spellings of one id are
  duplicates (`actions_duplicate_job_id`, `actions_duplicate_step_id`).

## Evaluation

- **Independent oracle.** `docs/scripts/oracle/actions-psych.rb` reads every
  workflow of a repository with Ruby's Psych (libyaml 0.2.5) and prints its
  triggers, jobs, `needs` and each step's `uses` in order;
  `TestRealRepositories` (opt-in, `ARK_YAML_REAL_REPOS`,
  `ARK_YAML_ORACLE_DIR`) prints the same from Ark's extraction and checks
  the index (no syntax error, a `contains` edge to every job, every `needs`
  of a declared job Exact, every local `uses` of a repository declaration an
  edge). On 2026-10-31, for Ark, kjak, kjak-notify-macos, Hephaestus and
  Enma (16 workflows, 623 lines) the outputs are byte for byte equal.
- **Diagnostics against actionlint** 1.7.12 (`-shellcheck= -pyflakes=`: no
  external command) on the fixture: the same invalid `uses`, missing `needs`
  job and `needs` cycle.
- **Corpus.** 8051 YAML files (the Go module cache and local repositories,
  40 MB): no failure, every range within its file, 15 s single-threaded,
  the slowest file (a 1.2 MB OpenAPI document) 138 ms. Every
  `yaml_syntax` there is a file Psych rejects too (test data of invalid
  YAML, Latin-1 files); no GitHub Actions diagnostic outside Ark's own
  fixtures.
- **Index cost** (MCP `get_repository_map`, cold, `--no-cache`, two rounds
  each, alternating with v6.9.0 on one machine): docker/docker v26.1.3
  (2002 Go and Python files, 43 YAML; 17484 → 29690 symbols, mostly
  OpenAPI keys): 8.2–8.4 s → 9.0–9.2 s (+10%), peak memory 1.1–1.3 GB with
  v6.9.0 and 1.0–1.4 GB with this version (within the rounds' spread);
  Hephaestus: 1.7 s both.
- **One long line.** 2 MB on one line (a flow sequence of a million
  scalars) 1.7 s; 2.5 MB of minified JSON with non-ASCII text 1.1 s.
- **Independent audits.** Three rounds (two reviewers, two re-auditors,
  one final re-auditor) audited the change: design, YAML, GitHub Actions,
  identities and graph; security, compatibility, tests, documents and
  release. Their findings — quadratic time on one long line, a needs alias
  bomb, secrets in names, a lost relation of an alias, case-insensitive
  ids, CRLF with directives, numbered step names, output forging by names,
  the shape of a local reusable workflow, bounds of diagnostics, a root
  under a `.github` ancestor dropping other languages, Windows separators,
  the package language of a repository map, and smaller ones — are fixed,
  each with a test; the last round found no Critical, High or Medium.
- **Mutation testing.** 71 mutants of the provider, the document and
  meaning layers and the generic changes (identity, confidence, location,
  bounds, masking of names, the `.github` rule, reference IDs, the
  repository map): 68 killed by the tests; 3 are equivalent (a plain
  scalar's trailing comment line is also cut as an inline comment; a CRLF
  directive line is also found through the blank line its break leaves;
  skipping the container of an alias past the reference bound only saves
  time).

## Remaining work

- Expressions: `needs.<job>.outputs`, `steps.<id>.outputs`, `inputs.*`
  references; a job's `with:` to a reusable workflow's `workflow_call`
  inputs (`ParameterScope` would carry it).
- The runner's choice between `action.yml` and `action.yaml` (both are
  candidates now).
- Schemas beyond GitHub Actions (Kubernetes, Compose, OpenAPI), keys below
  sequences.
