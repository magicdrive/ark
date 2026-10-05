# ark-completion.bash -- Bash completion for `ark`
# Place in /etc/bash_completion.d/ or source manually. Works with or without
# the bash-completion package. Requires bash 4.1+.
#
# The tables below mirror the CLI contract (subcommands, flags, finite values,
# supported setup clients); internal/completion tests fail if they drift from
# internal/commandline, the setup client registry or the language registry.

# ---- CLI contract tables (kept in sync with internal/commandline by tests) ----
_ark_subcommands="mcp-server mcp-init setup syntax symbol skill instruction"
_ark_skill_subcommands="init add-explorer update inspect"
_ark_setup_clients="claude cursor codex cline copilot-vscode copilot-cli"
_ark_instruction_targets="claude codex cursor cline copilot-vscode copilot-cli"   # a separate registry from the setup clients (same names today, by coincidence)
_ark_langs="go typescript tsx javascript python php"
_ark_exts="go js ts py java c cpp h txt md html css xml yml yaml json"
_ark_gen_flags="--help -h --version -v --compless -c --silent -S --skip-non-utf8 -s --delete-comment -D"
_ark_gen_opts="--output-filename -o --scan-buffer -b --output-format -f --mask-secrets -m \
    --allow-gitignore -a --additionally-ignorerule -A --with-line-number -n --ignore-dotfile -d \
    --pattern-regex -x --include-ext -i --exclude-file-regex -g --exclude-dir-regex -G \
    --exclude-ext -e --exclude-dir -E"
_ark_mcp_flags="--skip-non-utf8 -s --delete-comment -D --no-cache --help -h --version -v"
_ark_mcp_opts="--root -r --type -t --http-port -p --scan-buffer -b --mask-secrets -m --allow-gitignore -a \
    --additionally-ignorerule -A --ignore-dotfile -d --pattern-regex -x --include-ext -i \
    --exclude-file-regex -g --exclude-dir-regex -G --exclude-ext -e --exclude-dir -E"
_ark_setup_opts="--name -n --ark-path -p --root -r --global -g --force -f -h --help"
_ark_mcp_init_opts="--name -n --ark-path -p --root -r --global -g --force -f -h --help"
_ark_syntax_opts="--lang --format -h --help"
_ark_skill_opts="--name --output --archive --force -h --help"
_ark_skill_init_opts="--name --output --archive -h --help"
_ark_skill_update_opts="--force --dry-run -h --help"
_ark_skill_inspect_opts="-h --help"
_ark_instruction_opts="-h --help"

# ---- helpers (no hard dependency on the bash-completion package) --------------
if ! declare -F _filedir >/dev/null 2>&1; then
  # Minimal fallback: -d completes directories only, otherwise files and directories.
  _filedir() {
    local _c=${COMP_WORDS[COMP_CWORD]} _r
    compopt -o filenames 2>/dev/null
    if [[ ${1-} == -d ]]; then
      while IFS= read -r _r; do COMPREPLY+=( "$_r" ); done < <(compgen -d -- "$_c")
    else
      while IFS= read -r _r; do COMPREPLY+=( "$_r" ); done < <(compgen -f -- "$_c")
    fi
  }
fi

# Offer the words of $1 that start with the word being completed.
_ark_offer() {
  local _w
  for _w in $1; do
    [[ $_w == "${COMP_WORDS[COMP_CWORD]}"* ]] && COMPREPLY+=( "$_w" )
  done
}

_ark() {
  COMPREPLY=()
  local cword=$COMP_CWORD
  local cur=${COMP_WORDS[cword]} prev=""
  (( cword > 0 )) && prev=${COMP_WORDS[cword-1]}

  # First word: a subcommand (only valid as argv[1]), a general option, or the
  # directory operand of the default command.
  if (( cword == 1 )); then
    if [[ $cur == -* ]]; then
      _ark_offer "${_ark_gen_flags} ${_ark_gen_opts}"
    else
      _ark_offer "${_ark_subcommands}"
      _filedir -d
    fi
    return 0
  fi

  # Mode comes from argv[1] only, exactly like the CLI dispatcher.
  local cmd=${COMP_WORDS[1]} sub=""
  case $cmd in
    mcp-server|mcp-init|setup|syntax|symbol|skill|instruction) ;;
    *) cmd=general ;;
  esac
  if [[ $cmd == skill ]] && (( cword > 2 )); then
    case ${COMP_WORDS[2]} in
      init|add-explorer|update|inspect) sub=${COMP_WORDS[2]} ;;
    esac
  fi

  # Value of the preceding option. Short options mean different things per
  # command (-n, -f, -p), so this is decided per mode.
  case $cmd in
    general)
      case $prev in
        --output-format|-f) _ark_offer "txt md xml arklite auto"; return 0 ;;
        --mask-secrets|-m|--allow-gitignore|-a|--with-line-number|-n|--ignore-dotfile|-d)
          _ark_offer "on off"; return 0 ;;
        --scan-buffer|-b) _ark_offer "1M 5M 10M 100K"; return 0 ;;
        --include-ext|-i|--exclude-ext|-e) _ark_offer "${_ark_exts}"; return 0 ;;
        --output-filename|-o|--additionally-ignorerule|-A) _filedir; return 0 ;;
        --exclude-dir|-E) _filedir -d; return 0 ;;
        --pattern-regex|-x|--exclude-file-regex|-g|--exclude-dir-regex|-G) return 0 ;;
      esac ;;
    mcp-server)
      case $prev in
        --type|-t) _ark_offer "stdio http"; return 0 ;;
        --http-port|-p) _ark_offer "8008 8522 8080 9000"; return 0 ;;
        --mask-secrets|-m|--allow-gitignore|-a|--ignore-dotfile|-d) _ark_offer "on off"; return 0 ;;
        --scan-buffer|-b) _ark_offer "1M 5M 10M 100K"; return 0 ;;
        --include-ext|-i|--exclude-ext|-e) _ark_offer "${_ark_exts}"; return 0 ;;
        --root|-r) _filedir -d; return 0 ;;
        --additionally-ignorerule|-A) _filedir; return 0 ;;
        --exclude-dir|-E) _filedir -d; return 0 ;;
        --pattern-regex|-x|--exclude-file-regex|-g|--exclude-dir-regex|-G) return 0 ;;
      esac ;;
    mcp-init|setup)
      case $prev in
        --root|-r) _filedir -d; return 0 ;;
        --ark-path|-p) _filedir; return 0 ;;
        --name|-n) return 0 ;;
      esac ;;
    syntax|symbol)
      case $prev in
        --lang) _ark_offer "${_ark_langs}"; return 0 ;;
        --format) _ark_offer "text json"; return 0 ;;
      esac ;;
    skill)
      case $prev in
        --output) _filedir -d; return 0 ;;
        --name) return 0 ;;
      esac ;;
  esac

  # Options and positional operands.
  case $cmd in
    general)
      if [[ $cur == -* ]]; then _ark_offer "${_ark_gen_flags} ${_ark_gen_opts}"; else _filedir -d; fi ;;
    mcp-server)
      _ark_offer "${_ark_mcp_flags} ${_ark_mcp_opts}" ;;
    mcp-init)
      _ark_offer "${_ark_mcp_init_opts}" ;;
    setup)
      # `ark setup <client> [OPTIONS]`: the client is the first operand and must
      # precede every option (the parser rejects flags-before-client).
      if (( cword == 2 )) && [[ $cur != -* ]]; then
        _ark_offer "${_ark_setup_clients}"
      else
        _ark_offer "${_ark_setup_opts}"
      fi ;;
    syntax|symbol)
      if [[ $cur == -* ]]; then _ark_offer "${_ark_syntax_opts}"; else _filedir; fi ;;
    instruction)
      # `ark instruction <target>`: the target is the first operand.
      if (( cword == 2 )) && [[ $cur != -* ]]; then
        _ark_offer "${_ark_instruction_targets}"
      else
        _ark_offer "${_ark_instruction_opts}"
      fi ;;
    skill)
      case $sub in
        init|add-explorer) _ark_offer "${_ark_skill_init_opts}" ;;
        update)            _ark_offer "${_ark_skill_update_opts}" ;;
        inspect)           _ark_offer "${_ark_skill_inspect_opts}" ;;
        *)
          if (( cword == 2 )) && [[ $cur != -* ]]; then
            _ark_offer "${_ark_skill_subcommands}"
          else
            _ark_offer "${_ark_skill_opts}"
          fi ;;
      esac ;;
  esac
  return 0
}

complete -F _ark ark
