# Ark Security Model

This document describes the security posture of the Ark code intelligence
engine when analyzing untrusted repositories.

## Scope

Ark is a static analysis tool. It reads source files and builds an
in-memory symbol graph. It does NOT:

- execute any code from the analyzed repository;
- invoke build systems, package managers, or compilers;
- make network requests on behalf of the repository;
- evaluate scripts or configuration files;
- load native plugins or dynamic libraries from the repository.

The following applies to all analysis paths: CLI, MCP server, and
programmatic API.

## Repository boundary enforcement

The MCP server resolves `--root` once, at startup: a relative root is
resolved against the launch working directory, and the result is an
absolute, clean path that must be an existing directory (otherwise the
server exits with an error). Later working-directory changes do not alter
the root.

Every path argument of every MCP tool (and the `file://` / `directory://`
resources) goes through one gate, `resolveToolPath`. Relative paths are
resolved against the root; absolute paths are accepted when they lie inside
it.

**Containment invariant:**

```
resolved_path must satisfy:
  rel, err := filepath.Rel(root, resolved_path)
  err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
```

This check rejects:

- `../` relative escapes;
- `../../` deep relative escapes;
- absolute paths outside root;
- prefix collisions such as `/repo-other` when root is `/repo`.

String-prefix checks (`strings.HasPrefix(path, root)`) are intentionally
**not** used, because they are vulnerable to prefix collisions.

## Symlink policy

When the requested path exists, its symlink-resolved form must also lie
inside the symlink-resolved root. A symlink inside the repository that
points outside it is therefore refused when it is named as a tool's path
argument (directly, or as a component of the path); a symlink that stays
inside the repository keeps working. An absolute path that reaches the root
through another spelling of a symlinked prefix (for example macOS
`/var` vs `/private/var`) is accepted and reported in the root's own
spelling. Index-based tools additionally build their index over the
canonical directory (`canonicalDir`).

**Known limitations:**

- The check is made when a request arrives. It does not guard against the
  repository being changed concurrently (time-of-check/time-of-use).
- Directory walks started from an accepted path (for example
  `search_in_files`, `list_files`, `get_project_stats`, `find_symbol`,
  `find_references`, and index building) do not descend into symlinked
  directories, but they may read a symlinked *file* found during the walk,
  even if it points outside the repository.

Mitigation: run Ark in an environment where the repository tree is not
controlled by an untrusted party (e.g., read-only checkout, container
with restricted filesystem).

## Oversized file handling

Ark reads source files into memory for Tree-sitter parsing. There is
currently no per-file size limit. Repositories containing extremely large
files (multi-GB) may exhaust available memory.

Recommended mitigation: configure OS-level memory limits for the Ark
process when analyzing untrusted repositories.

## Cache trust model

Cache entries are stored as JSON files on disk. The cache parser is
defensive:

- corrupt JSON → graceful miss, file removed;
- schema version mismatch → graceful miss;
- content hash mismatch → miss;
- provider version mismatch → miss.

A maliciously crafted cache file cannot cause Ark to panic (verified by
fuzz testing). It may, however, cause Ark to use stale extraction
results if the attacker can write files matching the expected cache path.

Mitigation: store the cache directory outside the analyzed repository
and restrict write access.

## Output bounds

Ark does not impose hard limits on MCP response sizes. A repository with
very many symbols may produce large responses. MCP clients should apply
their own size limits.

## Determinism and reproducibility

Ark's analysis is fully deterministic for a given set of source files and
Ark version. It does not call external services, use random seeds, or
depend on wall-clock time for analysis results.

## Responsible disclosure

To report a security issue, please open a GitHub issue with the label
`security` or contact the maintainers directly. Do not disclose
vulnerabilities publicly before coordinating with maintainers.
