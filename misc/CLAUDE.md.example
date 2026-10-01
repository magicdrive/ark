# Project Name

<!-- Replace this section with your own project description -->

## Code Exploration

This project uses Ark MCP tools. Prefer these over Read/Bash for code navigation:

| Tool | When to use |
|------|-------------|
| `mcp__ark__get_directory_tree` | First step — understand project layout |
| `mcp__ark__get_symbols` | List functions/types in a file **instead of reading it** |
| `mcp__ark__find_symbol` | Search for a symbol by name across the repo |
| `mcp__ark__get_symbol` | Get source code of one specific function/type |
| `mcp__ark__search_in_files` | Full-text or regex search across files |
| `mcp__ark__list_files` | Filter-aware file listing |
| `mcp__ark__get_file_content` | Read a whole file *(last resort)* |
| `mcp__ark__get_file_info` | File metadata (size, lines, language) |
| `mcp__ark__get_project_stats` | Language breakdown, file counts |
| `mcp__ark__get_files_arklite` | Multiple files in compressed format |

### Workflow

Start broad, drill down:

```
get_directory_tree   →   Understand project structure
        ↓
    find_symbol      →   Locate "where is Foo?"
        ↓
    get_symbols      →   List what's in a file
        ↓
    get_symbol       →   Extract exact source code
```

### Rules

1. NEVER read entire files immediately — use `mcp__ark__get_symbols` first
2. Use `mcp__ark__find_symbol` to locate definitions before browsing
3. Use `mcp__ark__get_symbol` to retrieve exact source of a specific function/type
4. Use `mcp__ark__get_file_content` only when surrounding context is needed
