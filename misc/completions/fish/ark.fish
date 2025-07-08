# ark.fish -- Fish shell completion for `ark`
# Place in ~/.config/fish/completions/

function __fish_ark_is_first_arg
    # True if we are completing the first non-option argument
    set cmd (commandline -opc)
    test (count $cmd) -eq 1
end

# ----- sub-command ----------------------------------------------------------
complete -c ark -n '__fish_ark_is_first_arg'    \
        -a 'mcp-server'                         \
        -d 'Start MCP server'

# ----- general flags (no argument) ------------------------------------------
for opt in help h version v compless c silent S skip-non-utf8 s delete-comments D
    complete -c ark -s (string split " " $opt)[2] -l (string split " " $opt)[1] \
            -d 'see ark --help'
end

# ----- general options with arguments ---------------------------------------
complete -c ark -l output-filename -s o -d 'Output file'      -r -f
complete -c ark -l scan-buffer     -s b -d 'Buffer size'      -a '1M 5M 10M 100K'
complete -c ark -l output-format   -s f -d 'Output format'    -a 'txt md xml arklite'
complete -c ark -l mask-secrets    -s m -d 'Mask secrets'     -a 'on off'
complete -c ark -l allow-gitignore -s a -d 'Use .gitignore'   -a 'on off'
complete -c ark -l additionally-ignorerule -s A -d 'Extra ignore file' -r -f
complete -c ark -l with-line-number -s n -d 'Line numbers'    -a 'on off'
complete -c ark -l ignore-dotfile   -s d -d 'Ignore dotfiles' -a 'on off'
complete -c ark -l pattern-regex    -s x -d 'Pattern regexp'  -r
complete -c ark -l include-ext      -s i -d 'Include ext'     -r
complete -c ark -l exclude-dir-regex -s g -d 'Exclude dir regex' -r
complete -c ark -l exclude-file-regex -s G -d 'Exclude file regex' -r
complete -c ark -l exclude-ext      -s e -d 'Exclude ext'     -r
complete -c ark -l exclude-dir      -s E -d 'Exclude dir'     -r

# ----- mcp-server flags ------------------------------------------------------
for opt in skip-non-utf8 s delete-comments D
    complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
            -s (string split " " $opt)[2] -l (string split " " $opt)[1] \
            -d 'see ark mcp-server --help'
end

# ----- mcp-server options with arguments ------------------------------------
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l root -s r   -d 'Root directory'  -r -f
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l port -s p   -d 'Port'            -a '8008 8522 8080 9000'
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l scan-buffer -s b -d 'Buffer size' -a '1M 5M 10M 100K'
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l mask-secrets -s m -d 'Mask secrets' -a 'on off'
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l allow-gitignore -s a -d 'Use .gitignore' -a 'on off'
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l additionally-ignorerule -s A -d 'Extra ignore file' -r -f
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l ignore-dotfile -s d -d 'Ignore dotfiles' -a 'on off'
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l pattern-regex -s x -d 'Pattern regexp' -r
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l include-ext -s i -d 'Include ext' -r
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l exclude-dir-regex -s g -d 'Exclude dir regex' -r
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l exclude-file-regex -s G -d 'Exclude file regex' -r
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l exclude-ext -s e -d 'Exclude ext' -r
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l exclude-dir -s E -d 'Exclude dir' -r

