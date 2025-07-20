# ark.bash -- Bash completion for `ark`
# Place in /etc/bash_completion.d/ or source manually.

# -------- Common option lists ------------------------------------------------
_gen_flags="--help -h --version -v --compless -c --silent -S --skip-non-utf8 -s --delete-comments -D"
_gen_opts="--output-filename -o --scan-buffer -b --output-format -f --mask-secrets -m \
--allow-gitignore -a --additionally-ignorerule -A --with-line-number -n --ignore-dotfile -d \
--pattern-regex -x --include-ext -i --exclude-dir-regex -g --exclude-file-regex -G \
--exclude-ext -e --exclude-dir -E"
_mcp_flags="--skip-non-utf8 -s --delete-comments -D"
_mcp_opts="--root -r --type -t --http-port -p --scan-buffer -b --mask-secrets -m --allow-gitignore -a \
--additionally-ignorerule -A --ignore-dotfile -d --pattern-regex -x --include-ext -i \
--exclude-dir-regex -g --exclude-file-regex -G --exclude-ext -e --exclude-dir -E"
_subcmds="mcp-server"

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

  # Decide mode
  local mode="general"
  [[ ${COMP_WORDS[*]} =~ \ bmcp-server\b ]] && mode="mcp"

  # Value suggestions
  case "$prev" in
    --output-format|-f)     COMPREPLY=( $(compgen -W "txt md xml arklite" -- "$cur") ); return ;;
    --mask-secrets|-m|--allow-gitignore|-a|--with-line-number|-n|--ignore-dotfile|-d|--skip-non-utf8|-s)
                            COMPREPLY=( $(compgen -W "on off" -- "$cur") ); return ;;
    --include-ext|-i|--exclude-ext|-e)
                            COMPREPLY=( $(compgen -W "go js ts py java c cpp h txt md html css xml yml yaml json" -- "$cur") ); return ;;
    --output-filename|-o|--additionally-ignorerule|-A|--root|-r) _filedir; return ;;
    --type|-t)              COMPREPLY=( $(compgen -W "stdio http" -- "$cur") ); return ;;
    --http-port|-p)              COMPREPLY=( $(compgen -W "8008 8522 8080 9000" -- "$cur") ); return ;;
    --scan-buffer|-b)       COMPREPLY=( $(compgen -W "1M 5M 10M 100K" -- "$cur") ); return ;;
  esac

  # Option suggestions
  if [[ $mode == mcp ]]; then
    COMPREPLY=( $(compgen -W "${_mcp_flags} ${_mcp_opts}" -- "$cur") )
  else
    COMPREPLY=( $(compgen -W "${_gen_flags} ${_gen_opts} ${_subcmds}" -- "$cur") )
  fi
}

complete -F _ark ark

