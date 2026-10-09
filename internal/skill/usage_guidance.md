## Using Ark MCP

Ark answers one question: what code do I need to read to understand or safely modify this? Choose tools by what you are trying to learn, use the smallest set that answers it, and stop exploring once you have enough evidence to proceed safely. Nothing here is a required sequence.

### What are you trying to learn?

| Goal | Tool |
|------|------|
| How the repository is organised (modules, key symbols, dependencies) | `get_repository_map` |
| What the directory layout looks like | `get_directory_tree` |
| Where something is defined | `find_symbol` |
| A symbol whose exact name you do not know (partial identifier) | `search_context` |
| What one file contains | `get_symbols` |
| Understand, change, debug or review a symbol | `get_context` |
| The exact implementation of one symbol | `get_symbol` |
| How a symbol connects to others | `get_relations` |
| Who calls a symbol / what it calls | `get_callers` / `get_callees` |
| What a change could affect | `analyze_change_impact` |
| Symbols matching structural criteria (kind, name pattern, calls, type usage) | `search_code` |
| A literal text or regular-expression match | `search_in_files` |
| A file as a whole | `get_file_content` |
| Which files Ark could not fully analyze (regions the parser rejected, unreadable files) | `get_diagnostics` |

### Choosing between them

- **Understanding or modifying a symbol:** start with `get_context` instead of reading several files by hand. It selects the relevant code (guided by the dependency graph), ranks it and keeps it within a token budget. Use `get_symbol` instead when only that one definition matters.
- **Orientation:** `get_repository_map` suits an unfamiliar codebase or an architecture question; skip it when the symbol or file is already known. Use `get_directory_tree` when the directory layout itself is what you need.
- **Relationships:** ask the graph tools ("who calls this?", "what depends on this?") instead of grepping or reading files to infer them.
- **Impact:** use `analyze_change_impact` when a change may reach beyond the code you are editing, such as a public API, a shared type, an interface or a widely used function. Skip it for small, local changes. It labels heuristic findings as possible dependents: treat those as leads to verify, not as guaranteed impact.
- **Search:** use `search_context` when you know only part of a name ("auth", "getUser"): it ranks candidates by how closely their names match, returns up to `limit` of them (default 5) and attaches context to the first `contextLimit` (default 1), all within one budget — one call can replace `find_symbol` followed by `get_context`. Candidates are ranked by name, not by what the code refers to: read the whole list before relying on rank 1. When another candidate is the one you need, raise `contextLimit` to its rank or call `get_context` with its qualified name and its path as `filePattern`. Use `find_symbol` for a regular expression over declarations and `search_code` for structural criteria; use `search_in_files` for literal text, strings, comments and regular expressions.
- **Whole files:** prefer the tools above when they answer the question efficiently; read a file with `get_file_content` when whole-file context is genuinely useful.

### Ambiguity and uncertainty

- When a symbol name matches several symbols, Ark reports the candidates. Narrow the query with a qualified name or a file filter; do not pick the first result.
- Treat ambiguous, unresolved, heuristic or low-confidence results as evidence with uncertainty, not as established facts. Do not report a call, dependency or impact as certain when Ark did not establish it.
- When the distinction matters (before a risky change, or when reporting findings), confirm in the source with `get_symbol`.
- A result with `indexDiagnostics` comes from an index with diagnostics: code they cover (e.g. a region the parser rejected) is not analyzed, so "no callers" may be incomplete. List them with `get_diagnostics`. Zero diagnostics proves no completeness either: files of unsupported formats are never examined.

### Examples

These illustrate how tools combine; they are not sequences to follow.

- **Understand an unfamiliar subsystem:** `get_repository_map`, then `search_context`, `find_symbol` or `search_code` for its entry points, then `get_context` on the central symbol, then `get_relations` if how it connects matters.
- **Modify a known symbol:** `get_context` (after `find_symbol` if you need its location), then `analyze_change_impact` if downstream effects matter, and `get_symbol` if you need the exact current implementation to edit it.
- **Debug a call path:** `find_symbol`, then `get_callers` / `get_callees` along the path, then `get_context` for the symbols that look relevant.
