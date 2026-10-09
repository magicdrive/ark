## Ark Code Intelligence

Use Ark MCP as the primary tool for repository exploration and code understanding when its structured tools can answer the question. Pick the tool that fits the question; you do not need to call them all.

- **Repository structure and architecture:** use `get_repository_map`. Use `get_directory_tree` only when the directory layout itself is what you need.
- **Locating a definition:** use `find_symbol`. Use `get_symbols` to see the structure of one file.
- **Finding a symbol whose exact name you do not know:** use `search_context` with a partial identifier (e.g. "auth", "getUser", "user_profile"). It returns up to 5 ranked candidates and, by default, context for rank 1 only. Check the other candidates: a rank is name similarity, not proof that it is the symbol you need. For another candidate, raise `contextLimit`, or request its context by its qualified name with its path as the file filter.
- **Understanding, modifying, debugging or reviewing a symbol:** prefer `get_context`. It returns the code you need, ranked and token-budgeted, so you do not have to assemble it by hand from several files.
- **Exact source of one symbol:** use `get_symbol`.
- **Dependencies and call relationships:** use `get_relations`, `get_callers` (who calls it) and `get_callees` (what it calls).
- **Impact of a change:** when downstream effects matter, use `analyze_change_impact`. Skip it for small, local changes.
- **Searching:** use `search_code` for structural queries (kind, name pattern, calls, type usage) and `search_in_files` for plain text or regex.
- **Whole files:** prefer the symbol, context and relationship tools when they are sufficient; use `get_file_content` when whole-file context is genuinely useful.
- **Analysis limits:** when a result carries `indexDiagnostics`, part of the repository was not analyzed (e.g. a region the parser rejected); use `get_diagnostics` to see which files before concluding that something has no callers or dependents. No diagnostics does not prove completeness: files of unsupported formats are not examined.
- **Uncertainty:** treat ambiguous, unresolved or low-confidence Ark results as uncertainty. Do not turn them into an exact relationship that Ark did not establish. If a symbol name is ambiguous, narrow it (a qualified name, a file filter, or the `symbolId` the candidate list gives) instead of guessing, and check the source when it matters.
