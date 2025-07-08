# ---------------------------------------------------------------------------
# ark-completion.sh — Bash & Zsh completion for `ark`
# ---------------------------------------------------------------------------
#   Works on:
#     bash 4.1+   (with or without bash-completion package)
#     zsh  5.1+   (no dependency on bashcompinit helpers)
# ---------------------------------------------------------------------------

###############################
# Common option lists
###############################
_ark_gen_flags="--help -h --version -v --compless -c --silent -S --skip-non-utf8 -s --delete-comments -D"
_ark_gen_opts_arg="--output-filename -o --scan-buffer -b --output-format -f --mask-secrets -m \
    --allow-gitignore -a --additionally-ignorerule -A --with-line-number -n --ignore-dotfile -d \
    --pattern-regex -x --include-ext -i --exclude-dir-regex -g --exclude-file-regex -G \
    --exclude-ext -e --exclude-dir -E"
_ark_mcp_flags="--skip-non-utf8 -s --delete-comments -D"
_ark_mcp_opts_arg="--root -r --port -p --scan-buffer -b --mask-secrets -m --allow-gitignore -a \
    --additionally-ignorerule -A --ignore-dotfile -d --pattern-regex -x --include-ext -i \
    --exclude-dir-regex -g --exclude-file-regex -G --exclude-ext -e --exclude-dir -E"
_ark_subcommands="mcp-server"

###############################
# Bash part
###############################
_ark_bash() {
  # --- minimal fallbacks (for systems w/o bash-completion) -----------------
  if ! declare -F _get_comp_words_by_ref >/dev/null 2>&1; then
    _get_comp_words_by_ref() {
      local cur prev
      cur=${COMP_WORDS[COMP_CWORD]}
      prev=${COMP_WORDS[COMP_CWORD-1]}
      while [[ $1 ]]; do
        case $1 in
          cur)   printf -v cur   '%s' "$cur"   ;;
          prev)  printf -v prev  '%s' "$prev"  ;;
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
  # -------------------------------------------------------------------------

  local cur prev words cword
  _init_completion -n : || return

  # ----- first token -----
  if (( cword == 1 )); then
    if [[ $cur == -* ]]; then
      COMPREPLY=( $(compgen -W "${_ark_gen_flags} ${_ark_gen_opts_arg}" -- "$cur") )
    else
      COMPREPLY=( $(compgen -W "${_ark_subcommands}" -- "$cur") )
    fi
    __ltrim_colon_completions "$cur"
    return
  fi

  # detect mode (mcp-server subcommand or general)
  local mode="general"
  for w in "${words[@]}"; do [[ $w == mcp-server ]] && { mode="mcp"; break; }; done

  # value completion helper
  _ark_values() {
    case "$prev" in
      --output-format|-f)
        COMPREPLY=( $(compgen -W "txt md xml arklite" -- "$cur") ); return 0 ;;
      --mask-secrets|-m|--allow-gitignore|-a|--with-line-number|-n|--ignore-dotfile|-d|--skip-non-utf8|-s)
        COMPREPLY=( $(compgen -W "on off" -- "$cur") ); return 0 ;;
      --include-ext|-i|--exclude-ext|-e)
        COMPREPLY=( $(compgen -W "go js ts py java c cpp h txt md html css xml yml yaml json" -- "$cur") ); return 0 ;;
      --output-filename|-o|--additionally-ignorerule|-A|--root|-r)
        _filedir; return 0 ;;
      --port|-p)
        COMPREPLY=( $(compgen -W "8008 8522 8080 9000" -- "$cur") ); return 0 ;;
      --scan-buffer|-b)
        COMPREPLY=( $(compgen -W "1M 5M 10M 100K" -- "$cur") ); return 0 ;;
    esac
    return 1
  }
  _ark_values && return

  # option completion
  if [[ $mode == mcp ]]; then
    COMPREPLY=( $(compgen -W "${_ark_mcp_flags} ${_ark_mcp_opts_arg}" -- "$cur") )
  else
    COMPREPLY=( $(compgen -W "${_ark_gen_flags} ${_ark_gen_opts_arg} ${_ark_subcommands}" -- "$cur") )
  fi
}

###############################
# Zsh part (native)
###############################
_ark_zsh() {
  local context state
  typeset -A opt_args

  local -a general_opts=(
    '--help[-h]' '--version[-v]' '--compless[-c]' '--silent[-S]'
    '--skip-non-utf8[-s]' '--delete-comments[-D]'
    '--output-filename[-o]:output file:_files'
    '--scan-buffer[-b]:buffer size:(1M 5M 10M 100K)'
    '--output-format[-f]:format:(txt md xml arklite)'
    '--mask-secrets[-m]:on/off:(on off)'
    '--allow-gitignore[-a]:on/off:(on off)'
    '--additionally-ignorerule[-A]:ignore rule file:_files'
    '--with-line-number[-n]:on/off:(on off)'
    '--ignore-dotfile[-d]:on/off:(on off)'
    '--pattern-regex[-x]:regexp:'
    '--include-ext[-i]:extensions:(go js ts py java c cpp h txt md html css xml yml yaml json)'
    '--exclude-dir-regex[-g]:regexp:'
    '--exclude-file-regex[-G]:regexp:'
    '--exclude-ext[-e]:extensions:(go js ts py java c cpp h txt md html css xml yml yaml json)'
    '--exclude-dir[-E]:dirname:'
  )

  local -a mcp_opts=(
    '--root[-r]:root directory:_files -/'
    '--port[-p]:port number:(8008 8522 8080 9000)'
    '--scan-buffer[-b]:buffer size:(1M 5M 10M 100K)'
    '--mask-secrets[-m]:on/off:(on off)'
    '--allow-gitignore[-a]:on/off:(on off)'
    '--additionally-ignorerule[-A]:ignore rule file:_files'
    '--ignore-dotfile[-d]:on/off:(on off)'
    '--pattern-regex[-x]:regexp:'
    '--include-ext[-i]:extensions:(go js ts py java c cpp h txt md html css xml yml yaml json)'
    '--exclude-dir-regex[-g]:regexp:'
    '--exclude-file-regex[-G]:regexp:'
    '--exclude-ext[-e]:extensions:(go js ts py java c cpp h txt md html css xml yml yaml json)'
    '--exclude-dir[-E]:dirname:'
    '--skip-non-utf8[-s]'
    '--delete-comments[-D]'
  )

  local -a subcommands
  subcommands=('mcp-server:Start MCP server')

  _arguments -C \
    "${general_opts[@]}" \
    '1:command:->subcmd' \
    '*::options:->args'

  case $state in
    subcmd)
      _describe 'subcommand' subcommands
      ;;
    args)
      case $words[1] in
        mcp-server)
          _arguments -C "${mcp_opts[@]}" '*:files:_files'
          ;;
        *)
          _arguments -C "${general_opts[@]}" '*:dirname:_files -/'
          ;;
      esac
      ;;
  esac
}

###############################
# Dispatcher
###############################
if [[ -n ${ZSH_VERSION-} ]]; then
  compdef _ark_zsh ark
else
  complete -F _ark_bash ark
fi

