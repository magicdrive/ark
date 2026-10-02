## Code Exploration

This project uses Ark MCP tools. Prefer these over Read/Bash for code navigation:

### Intelligence tools (prefer these first)

| Tool | When to use |
|------|-------------|
| `mcp__ark__get_context` | **Primary tool** — token-budgeted, relevance-ranked source context for a symbol |
| `mcp__ark__get_repository_map` | Orient yourself in an unfamiliar repo — logical structure, key symbols, dependencies |
| `mcp__ark__analyze_change_impact` | Before modifying a symbol — understand what else is affected |
| `mcp__ark__find_references` | Find all call sites / usages of a name |
| `mcp__ark__get_relations` | Callers and callees of a symbol (with confidence) |
| `mcp__ark__get_callers` | What calls this symbol |
| `mcp__ark__get_callees` | What this symbol calls |
| `mcp__ark__search_code` | Structural search: kind=function, callsName=os.Open, etc. |
| `mcp__ark__get_language_support` | List supported languages and their support levels |

### Symbol tools

| Tool | When to use |
|------|-------------|
| `mcp__ark__get_symbols` | List functions/types in a file **instead of reading it** |
| `mcp__ark__find_symbol` | Search for a symbol by name across the repo |
| `mcp__ark__get_symbol` | Get source code of one specific function/type |

### File tools

| Tool | When to use |
|------|-------------|
| `mcp__ark__get_directory_tree` | Understand project layout |
| `mcp__ark__list_files` | Filter-aware file listing |
| `mcp__ark__search_in_files` | Full-text or regex search across files |
| `mcp__ark__get_file_content` | Read a whole file *(last resort)* |
| `mcp__ark__get_file_info` | File metadata (size, lines, language) |
| `mcp__ark__get_project_stats` | Language breakdown, file counts |
| `mcp__ark__get_files_arklite` | Multiple files in compressed format |

### Workflow

Start broad, drill down:

```
get_repository_map   →   Understand repo structure
        ↓
    find_symbol      →   Locate "where is Foo?"
        ↓
    get_context      →   Get token-budgeted context for a symbol
        ↓
    get_symbol       →   Extract exact source of one function (if needed)
```

For impact analysis:

```
analyze_change_impact(symbol)  →  direct dependents + tests + transitive
find_references(name)          →  all call sites
get_callers / get_callees      →  graph neighbours
```

### Rules

1. NEVER read entire files immediately — use `mcp__ark__get_symbols` first
2. Prefer `mcp__ark__get_context` over manually chasing symbol chains
3. Use `mcp__ark__find_symbol` to locate definitions before browsing
4. Use `mcp__ark__get_symbol` to retrieve exact source of a specific function/type
5. Use `mcp__ark__get_file_content` only when surrounding context is needed
6. `mcp__ark__search_code` excludes test files by default — pass `excludeTest: false` when working with test code
