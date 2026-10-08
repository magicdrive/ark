## Ark Code Intelligence

Use Ark MCP as the primary tool for repository exploration and code understanding when its structured tools can answer the question. Pick the tool that fits the question; you do not need to call them all.

- **Repository structure and architecture:** use `mcp__ark__get_repository_map`. Use `mcp__ark__get_directory_tree` only when the directory layout itself is what you need.
- **Locating a definition:** use `mcp__ark__find_symbol`. Use `mcp__ark__get_symbols` to see the structure of one file.
- **Understanding, modifying, debugging or reviewing a symbol:** prefer `mcp__ark__get_context`. It returns the code you need, ranked and token-budgeted, so you do not have to assemble it by hand from several files.
- **Exact source of one symbol:** use `mcp__ark__get_symbol`.
- **Dependencies and call relationships:** use `mcp__ark__get_relations`, `mcp__ark__get_callers` (who calls it) and `mcp__ark__get_callees` (what it calls).
- **Impact of a change:** when downstream effects matter, use `mcp__ark__analyze_change_impact`. Skip it for small, local changes.
- **Searching:** use `mcp__ark__search_code` for structural queries (kind, name pattern, calls, type usage) and `mcp__ark__search_in_files` for plain text or regex.
- **Whole files:** prefer the symbol, context and relationship tools when they are sufficient; use `mcp__ark__get_file_content` when whole-file context is genuinely useful.
- **Analysis limits:** when a result carries `indexDiagnostics`, part of the repository was not analyzed (e.g. a region the parser rejected); use `mcp__ark__get_diagnostics` to see which files before concluding that something has no callers or dependents. No diagnostics does not prove completeness: files of unsupported formats are not examined.
- **Uncertainty:** treat ambiguous, unresolved or low-confidence Ark results as uncertainty. Do not turn them into an exact relationship that Ark did not establish. If a symbol name is ambiguous, narrow it (a qualified name or a file filter) instead of guessing, and check the source when it matters.
