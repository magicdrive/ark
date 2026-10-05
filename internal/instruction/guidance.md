## Ark Code Intelligence

Use Ark MCP as the primary tool for repository exploration and code understanding when its structured tools can answer the question. Pick the tool that fits the question; you do not need to call them all.

- **Repository structure and architecture:** use `get_repository_map`. Use `get_directory_tree` only when the directory layout itself is what you need.
- **Locating a definition:** use `find_symbol`. Use `get_symbols` to see the structure of one file.
- **Understanding, modifying, debugging or reviewing a symbol:** prefer `get_context`. It returns the code you need, ranked and token-budgeted, so you do not have to assemble it by hand from several files.
- **Exact source of one symbol:** use `get_symbol`.
- **Dependencies and call relationships:** use `get_relations`, `get_callers` (who calls it) and `get_callees` (what it calls).
- **Impact of a change:** when downstream effects matter, use `analyze_change_impact`. Skip it for small, local changes.
- **Searching:** use `search_code` for structural queries (kind, name pattern, calls, type usage) and `search_in_files` for plain text or regex.
- **Whole files:** prefer the symbol, context and relationship tools when they are sufficient; use `get_file_content` when whole-file context is genuinely useful.
- **Uncertainty:** treat ambiguous, unresolved or low-confidence Ark results as uncertainty. Do not turn them into an exact relationship that Ark did not establish. If a symbol name is ambiguous, narrow it (a qualified name or a file filter) instead of guessing, and check the source when it matters.
