# ark.fish -- Fish shell completion for `ark`
# Place in ~/.config/fish/completions/

function __fish_ark_is_first_arg
    # True if we are completing the first non-option argument
    set cmd (commandline -opc)
    test (count $cmd) -eq 1
end

function __fish_ark_no_subcommand
    # True when argv[1] is not a subcommand: the general (default) command, whose
    # options are only valid before/without a subcommand.
    set cmd (commandline -opc)
    test (count $cmd) -lt 2; and return 0
    not contains -- $cmd[2] mcp-server mcp-init setup syntax symbol skill
end

function __fish_ark_setup_client_position
    # True only for the first operand after `ark setup`: the client must come
    # first (the parser rejects flags-before-client).
    set cmd (commandline -opc)
    test (count $cmd) -eq 2; and test "$cmd[2]" = setup
end

# ----- sub-command ----------------------------------------------------------
complete -c ark -n '__fish_ark_is_first_arg'    \
        -a 'mcp-server'                         \
        -d 'Start MCP server'
complete -c ark -n '__fish_ark_is_first_arg'    \
        -a 'syntax'                             \
        -d 'Parse file and output AST'
complete -c ark -n '__fish_ark_is_first_arg'    \
        -a 'symbol'                             \
        -d 'Extract symbols from file'
complete -c ark -n '__fish_ark_is_first_arg'    \
        -a 'skill'                              \
        -d 'Generate Cline/ChatGPT Skill'
complete -c ark -n '__fish_ark_is_first_arg'    \
        -a 'setup'                              \
        -d 'Configure Ark for a coding agent'
complete -c ark -n '__fish_ark_is_first_arg'    \
        -a 'mcp-init'                           \
        -d 'Register MCP server entry in settings.json'

# ----- general flags (no argument) ------------------------------------------
complete -c ark -n '__fish_ark_no_subcommand' -s h -l help          -d 'Show help'
complete -c ark -n '__fish_ark_no_subcommand' -s v -l version       -d 'Show version'
complete -c ark -n '__fish_ark_no_subcommand' -s c -l compless      -d 'Compress output (arklite)'
complete -c ark -n '__fish_ark_no_subcommand' -s S -l silent        -d 'Silent mode'
complete -c ark -n '__fish_ark_no_subcommand' -s s -l skip-non-utf8 -d 'Skip non-UTF8 files'
complete -c ark -n '__fish_ark_no_subcommand' -s D -l delete-comment -d 'Delete code comments'

# ----- general options with arguments ---------------------------------------
complete -c ark -n '__fish_ark_no_subcommand' -l output-filename -s o -d 'Output file'      -r -f
complete -c ark -n '__fish_ark_no_subcommand' -l scan-buffer     -s b -d 'Buffer size'      -a '1M 5M 10M 100K'
complete -c ark -n '__fish_ark_no_subcommand' -l output-format   -s f -d 'Output format'    -a 'txt md xml arklite auto'
complete -c ark -n '__fish_ark_no_subcommand' -l mask-secrets    -s m -d 'Mask secrets'     -a 'on off'
complete -c ark -n '__fish_ark_no_subcommand' -l allow-gitignore -s a -d 'Use .gitignore'   -a 'on off'
complete -c ark -n '__fish_ark_no_subcommand' -l additionally-ignorerule -s A -d 'Extra ignore file' -r -f
complete -c ark -n '__fish_ark_no_subcommand' -l with-line-number -s n -d 'Line numbers'    -a 'on off'
complete -c ark -n '__fish_ark_no_subcommand' -l ignore-dotfile   -s d -d 'Ignore dotfiles' -a 'on off'
complete -c ark -n '__fish_ark_no_subcommand' -l pattern-regex    -s x -d 'Pattern regexp'  -r
complete -c ark -n '__fish_ark_no_subcommand' -l include-ext      -s i -d 'Include ext'     -r
complete -c ark -n '__fish_ark_no_subcommand' -l exclude-file-regex -s g -d 'Exclude file regex' -r
complete -c ark -n '__fish_ark_no_subcommand' -l exclude-dir-regex  -s G -d 'Exclude dir regex'  -r
complete -c ark -n '__fish_ark_no_subcommand' -l exclude-ext      -s e -d 'Exclude ext'     -r
complete -c ark -n '__fish_ark_no_subcommand' -l exclude-dir      -s E -d 'Exclude dir'     -r

# ----- mcp-server flags ------------------------------------------------------
complete -c ark -n '__fish_seen_subcommand_from mcp-server' -l no-cache -d 'Disable persistent index cache'
complete -c ark -n '__fish_seen_subcommand_from mcp-server' -s s -l skip-non-utf8  -d 'Skip non-UTF8 files'
complete -c ark -n '__fish_seen_subcommand_from mcp-server' -s D -l delete-comment -d 'Delete code comments'
complete -c ark -n '__fish_seen_subcommand_from mcp-server' -s h -l help            -d 'Show help'
complete -c ark -n '__fish_seen_subcommand_from mcp-server' -s v -l version         -d 'Show version'

# ----- mcp-server options with arguments ------------------------------------
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l root -s r   -d 'Root directory'  -r -f
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l type -s t   -d 'Mcp Type'        -a 'stdio http'
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l http-port -s p   -d 'HttpPort'   -a '8008 8522 8080 9000'
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
        -l exclude-file-regex -s g -d 'Exclude file regex' -r
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l exclude-dir-regex  -s G -d 'Exclude dir regex'  -r
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l exclude-ext -s e -d 'Exclude ext' -r
complete -c ark -n '__fish_seen_subcommand_from mcp-server' \
        -l exclude-dir -s E -d 'Exclude dir' -r

# ----- syntax options --------------------------------------------------------
complete -c ark -n '__fish_seen_subcommand_from syntax' \
        -l lang -d 'Language' -a 'go typescript tsx javascript python php'
complete -c ark -n '__fish_seen_subcommand_from syntax' \
        -l format -d 'Output format' -a 'text json'
complete -c ark -n '__fish_seen_subcommand_from syntax' \
        -s h -l help -d 'Show help'

# ----- symbol options --------------------------------------------------------
complete -c ark -n '__fish_seen_subcommand_from symbol' \
        -l lang -d 'Language' -a 'go typescript tsx javascript python php'
complete -c ark -n '__fish_seen_subcommand_from symbol' \
        -l format -d 'Output format' -a 'text json'
complete -c ark -n '__fish_seen_subcommand_from symbol' \
        -s h -l help -d 'Show help'

# ----- skill sub-subcommands -------------------------------------------------
function __fish_ark_skill_no_subcmd
    set cmd (commandline -opc)
    not contains -- init $cmd; and not contains -- add-explorer $cmd
    and not contains -- update $cmd; and not contains -- inspect $cmd
end

complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_ark_skill_no_subcmd' -a 'init'         -d 'Initialize skill'
complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_ark_skill_no_subcmd' -a 'add-explorer' -d 'Add explorer skill'
complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_ark_skill_no_subcmd' -a 'update'       -d 'Update skill'
complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_ark_skill_no_subcmd' -a 'inspect'      -d 'Inspect skill'

# ----- skill options (no sub-subcommand) -------------------------------------
complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_ark_skill_no_subcmd' \
        -l name -d 'Skill name' -r
complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_ark_skill_no_subcmd' \
        -l output -d 'Output directory' -r -f
complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_ark_skill_no_subcmd' \
        -l archive -d 'Create ZIP archive'
complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_ark_skill_no_subcmd' \
        -l force -d 'Overwrite existing skill'
complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_ark_skill_no_subcmd' \
        -s h -l help -d 'Show help'

# ----- skill init / add-explorer options -------------------------------------
for subcmd in init add-explorer
    complete -c ark -n "__fish_seen_subcommand_from skill; and __fish_seen_subcommand_from $subcmd" \
            -l name    -d 'Skill name' -r
    complete -c ark -n "__fish_seen_subcommand_from skill; and __fish_seen_subcommand_from $subcmd" \
            -l output  -d 'Output dir' -r -f
    complete -c ark -n "__fish_seen_subcommand_from skill; and __fish_seen_subcommand_from $subcmd" \
            -l archive -d 'Create ZIP archive'
    complete -c ark -n "__fish_seen_subcommand_from skill; and __fish_seen_subcommand_from $subcmd" \
            -s h -l help -d 'Show help'
end

# ----- skill update options --------------------------------------------------
complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_seen_subcommand_from update' \
        -l force   -d 'Force update'
complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_seen_subcommand_from update' \
        -l dry-run -d 'Dry run'
complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_seen_subcommand_from update' \
        -s h -l help -d 'Show help'

# ----- skill inspect options -------------------------------------------------
complete -c ark -n '__fish_seen_subcommand_from skill; and __fish_seen_subcommand_from inspect' \
        -s h -l help -d 'Show help'

# ----- setup client (first positional) ---------------------------------------
complete -c ark -n '__fish_ark_setup_client_position' -a 'claude' -d 'Configure Ark for Claude Code'
complete -c ark -n '__fish_ark_setup_client_position' -a 'cursor' -d 'Configure Ark for Cursor'
complete -c ark -n '__fish_ark_setup_client_position' -a 'codex'  -d 'Configure Ark for Codex'
complete -c ark -n '__fish_ark_setup_client_position' -a 'cline'  -d 'Configure Ark for Cline'

# ----- setup options ---------------------------------------------------------
complete -c ark -n '__fish_seen_subcommand_from setup' \
        -l name -s n -d 'Claude skill name (Claude only)' -r
complete -c ark -n '__fish_seen_subcommand_from setup' \
        -l ark-path -s p -d 'Path to ark binary' -r -F
complete -c ark -n '__fish_seen_subcommand_from setup' \
        -l root -s r -d 'Root directory' -r -f
complete -c ark -n '__fish_seen_subcommand_from setup' \
        -l global -s g -d 'Write to global settings.json'
complete -c ark -n '__fish_seen_subcommand_from setup' \
        -l force -s f -d 'Overwrite existing entries'
complete -c ark -n '__fish_seen_subcommand_from setup' \
        -s h -l help -d 'Show help'

# ----- mcp-init options ------------------------------------------------------
complete -c ark -n '__fish_seen_subcommand_from mcp-init' \
        -l name -s n -d 'MCP server name' -r
complete -c ark -n '__fish_seen_subcommand_from mcp-init' \
        -l ark-path -s p -d 'Path to ark binary' -r -F
complete -c ark -n '__fish_seen_subcommand_from mcp-init' \
        -l root -s r -d 'Root directory' -r -f
complete -c ark -n '__fish_seen_subcommand_from mcp-init' \
        -l global -s g -d 'Write to global settings.json'
complete -c ark -n '__fish_seen_subcommand_from mcp-init' \
        -l force -s f -d 'Overwrite existing entry'
complete -c ark -n '__fish_seen_subcommand_from mcp-init' \
        -s h -l help -d 'Show help'

