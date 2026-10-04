# ark.bash -- Bash completion for `ark`
# Place in /etc/bash_completion.d/ or source manually.

# -------- Common option lists ------------------------------------------------
_gen_flags="--help -h --version -v --compless -c --silent -S --skip-non-utf8 -s --delete-comment -D"
_gen_opts="--output-filename -o --scan-buffer -b --output-format -f --mask-secrets -m \
--allow-gitignore -a --additionally-ignorerule -A --with-line-number -n --ignore-dotfile -d \
--pattern-regex -x --include-ext -i --exclude-file-regex -g --exclude-dir-regex -G \
--exclude-ext -e --exclude-dir -E"
_mcp_flags="--skip-non-utf8 -s --delete-comment -D --no-cache --help -h --version -v"
_mcp_opts="--root -r --type -t --http-port -p --scan-buffer -b --mask-secrets -m --allow-gitignore -a \
--additionally-ignorerule -A --ignore-dotfile -d --pattern-regex -x --include-ext -i \
--exclude-file-regex -g --exclude-dir-regex -G --exclude-ext -e --exclude-dir -E"
_subcmds="mcp-server mcp-init setup syntax symbol skill"
_syntax_opts="--lang --format -h --help"
_symbol_opts="--lang --format -h --help"
_skill_opts="init add-explorer update inspect --name --output --archive --force -h --help"
_skill_init_opts="--name --output --archive -h --help"
_skill_update_opts="--force --dry-run -h --help"
_skill_inspect_opts="-h --help"
_setup_opts="--name -n --ark-path -p --root -r --global -g --force -f -h --help"
_setup_clients="claude cursor codex cline"
_mcp_init_opts="--name -n --ark-path -p --root -r --global -g --force -f -h --help"

# -------- Fallback helpers (if bash-completion is missing) -------------------
if ! declare -F _get_comp_words_by_ref >/dev/null 2>&1; then
  _get_comp_words_by_ref() {
    local cur prev
    cur=${COMP_WORDS[COMP_CWORD]}
    prev=${COMP_WORDS[COMP_CWORD-1]}
    while [[ $1 ]]; do
      case $1 in
        cur)   printf -v cur   '%s' "$cur" ;;
        prev)  printf -v prev  '%s' "$prev" ;;
        words) printf -v words '%s' "${COMP_WORDS[*]}" ;;
        cword) printf -v cword '%s' "$COMP_CWORD" ;;
      esac
      shift
    done
  }
fi
if ! declare -F _init_completion >/dev/null 2>&1; then
  _init_completion() { _get_comp_words_by_ref cur prev words cword; }
fi
if ! declare -F __ltrim_colon_completions >/dev/null 2>&1; then
  __ltrim_colon_completions() { :; }
fi
# -----------------------------------------------------------------------------

_ark() {
  local cur prev words cword
  _init_completion -n : || return

  # First token → either option or sub-command
  if (( cword == 1 )); then
    if [[ $cur == -* ]]; then
      COMPREPLY=( $(compgen -W "${_gen_flags} ${_gen_opts}" -- "$cur") )
    else
      COMPREPLY=( $(compgen -W "${_subcmds}" -- "$cur") )
    fi
    return
  fi

  # Decide mode from subcommand token
  local mode="general"
  local w
  for w in "${COMP_WORDS[@]}"; do
    case $w in
      mcp-server) mode="mcp";      break ;;
      mcp-init)   mode="mcp-init"; break ;;
      setup)      mode="setup";    break ;;
      syntax)     mode="syntax";   break ;;
      symbol)     mode="symbol";   break ;;
      skill)      mode="skill";    break ;;
    esac
  done
  if [[ $mode == skill ]]; then
    for w in "${COMP_WORDS[@]}"; do
      case $w in
        init)         mode="skill-init";         break ;;
        add-explorer) mode="skill-add-explorer"; break ;;
        update)       mode="skill-update";       break ;;
        inspect)      mode="skill-inspect";      break ;;
      esac
    done
  fi

  # Value suggestions
  case "$prev" in
    --output-format|-f)     COMPREPLY=( $(compgen -W "txt md xml arklite" -- "$cur") ); return ;;
    --mask-secrets|-m|--allow-gitignore|-a|--with-line-number|-n|--ignore-dotfile|-d)
                            COMPREPLY=( $(compgen -W "on off" -- "$cur") ); return ;;
    --include-ext|-i|--exclude-ext|-e)
                            COMPREPLY=( $(compgen -W "go js ts py java c cpp h txt md html css xml yml yaml json" -- "$cur") ); return ;;
    --output-filename|-o|--additionally-ignorerule|-A|--root|-r|--ark-path) _filedir; return ;;
    --type|-t)              COMPREPLY=( $(compgen -W "stdio http" -- "$cur") ); return ;;
    --http-port)            COMPREPLY=( $(compgen -W "8008 8522 8080 9000" -- "$cur") ); return ;;
    -p)
      if [[ $mode == setup || $mode == mcp-init ]]; then
        _filedir
      else
        COMPREPLY=( $(compgen -W "8008 8522 8080 9000" -- "$cur") )
      fi
      return ;;
    --scan-buffer|-b)       COMPREPLY=( $(compgen -W "1M 5M 10M 100K" -- "$cur") ); return ;;
    --lang)                 COMPREPLY=( $(compgen -W "go typescript tsx javascript python" -- "$cur") ); return ;;
    --format)               COMPREPLY=( $(compgen -W "text json" -- "$cur") ); return ;;
  esac

  # Option suggestions
  case $mode in
    mcp)                   COMPREPLY=( $(compgen -W "${_mcp_flags} ${_mcp_opts}" -- "$cur") ) ;;
    mcp-init)              COMPREPLY=( $(compgen -W "${_mcp_init_opts}" -- "$cur") ) ;;
    setup)
      # Suggest the client as the first positional, options afterwards.
      local _has_client=0 _i
      for (( _i=2; _i<cword; _i++ )); do
        case "${COMP_WORDS[_i]}" in
          claude|cursor|codex|cline) _has_client=1; break ;;
        esac
      done
      if [[ $_has_client -eq 0 && $cur != -* ]]; then
        COMPREPLY=( $(compgen -W "${_setup_clients}" -- "$cur") )
      else
        COMPREPLY=( $(compgen -W "${_setup_opts}" -- "$cur") )
      fi
      ;;
    syntax)                COMPREPLY=( $(compgen -W "${_syntax_opts}" -- "$cur") ) ;;
    symbol)                COMPREPLY=( $(compgen -W "${_symbol_opts}" -- "$cur") ) ;;
    skill)                 COMPREPLY=( $(compgen -W "${_skill_opts}" -- "$cur") ) ;;
    skill-init|skill-add-explorer) COMPREPLY=( $(compgen -W "${_skill_init_opts}" -- "$cur") ) ;;
    skill-update)          COMPREPLY=( $(compgen -W "${_skill_update_opts}" -- "$cur") ) ;;
    skill-inspect)         COMPREPLY=( $(compgen -W "${_skill_inspect_opts}" -- "$cur") ) ;;
    *)                     COMPREPLY=( $(compgen -W "${_gen_flags} ${_gen_opts} ${_subcmds}" -- "$cur") ) ;;
  esac
}

complete -F _ark ark

