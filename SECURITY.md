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

All filesystem access requested through the MCP adapter is validated
against the configured repository root using `filepath.Rel`.

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

Ark currently performs **lexical** path containment only. A symlink
inside the repository root that points to a file outside the root will
be followed by the OS and the resulting path will pass lexical
containment.

**Known limitation:** Ark does not perform `filepath.EvalSymlinks` before
the containment check. Symlink-based escapes are theoretically possible
on systems where the repository contains attacker-controlled symlinks.

Mitigation: run Ark in an environment where the repository tree is not
controlled by an untrusted party (e.g., read-only checkout, container
with restricted filesystem).

A dedicated follow-up is required to harden symlink handling.

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
