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
_ark_gen_flags="--help -h --version -v --compless -c --silent -S --skip-non-utf8 -s --delete-comment -D"
_ark_gen_opts_arg="--output-filename -o --scan-buffer -b --output-format -f --mask-secrets -m \
    --allow-gitignore -a --additionally-ignorerule -A --with-line-number -n --ignore-dotfile -d \
    --pattern-regex -x --include-ext -i --exclude-file-regex -g --exclude-dir-regex -G \
    --exclude-ext -e --exclude-dir -E"
_ark_mcp_flags="--skip-non-utf8 -s --delete-comment -D --help -h --version -v"
_ark_mcp_opts_arg="--root -r --type -t --http-port -p --scan-buffer -b --mask-secrets -m --allow-gitignore -a \
    --additionally-ignorerule -A --ignore-dotfile -d --pattern-regex -x --include-ext -i \
    --exclude-file-regex -g --exclude-dir-regex -G --exclude-ext -e --exclude-dir -E"
_ark_subcommands="mcp-server mcp-init setup syntax symbol skill"
_ark_setup_opts="--name -n --ark-path -p --root -r --global -g --force -f -h --help"
_ark_mcp_init_opts="--name -n --ark-path -p --root -r --global -g --force -f -h --help"
_ark_skill_opts="init add-explorer update inspect --name --output --archive --force -h --help"
_ark_skill_init_opts="--name --output --archive -h --help"
_ark_skill_update_opts="--force --dry-run -h --help"
_ark_skill_inspect_opts="-h --help"

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

  # detect mode from subcommand token
  local mode="general"
  for w in "${words[@]}"; do
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
    for w in "${words[@]}"; do
      case $w in
        init)         mode="skill-init";         break ;;
        add-explorer) mode="skill-add-explorer"; break ;;
        update)       mode="skill-update";       break ;;
        inspect)      mode="skill-inspect";      break ;;
      esac
    done
  fi

  # value completion helper
  _ark_values() {
    case "$prev" in
      --output-format|-f)
        COMPREPLY=( $(compgen -W "txt md xml arklite" -- "$cur") ); return 0 ;;
      --mask-secrets|-m|--allow-gitignore|-a|--with-line-number|-n|--ignore-dotfile|-d)
        COMPREPLY=( $(compgen -W "on off" -- "$cur") ); return 0 ;;
      --include-ext|-i|--exclude-ext|-e)
        COMPREPLY=( $(compgen -W "go js ts py java c cpp h txt md html css xml yml yaml json" -- "$cur") ); return 0 ;;
      --output-filename|-o|--additionally-ignorerule|-A|--root|-r|--ark-path)
        _filedir; return 0 ;;
      --type|-t)
        COMPREPLY=( $(compgen -W "stdio http" -- "$cur") ); return 0 ;;
      --http-port)
        COMPREPLY=( $(compgen -W "8008 8522 8080 9000" -- "$cur") ); return 0 ;;
      -p)
        if [[ $mode == setup || $mode == mcp-init ]]; then
          _filedir
        else
          COMPREPLY=( $(compgen -W "8008 8522 8080 9000" -- "$cur") )
        fi
        return 0 ;;
      --scan-buffer|-b)
        COMPREPLY=( $(compgen -W "1M 5M 10M 100K" -- "$cur") ); return 0 ;;
      --lang)
        COMPREPLY=( $(compgen -W "go typescript tsx javascript python" -- "$cur") ); return 0 ;;
      --format)
        COMPREPLY=( $(compgen -W "text json" -- "$cur") ); return 0 ;;
    esac
    return 1
  }
  _ark_values && return

  # option completion
  case $mode in
    mcp)                   COMPREPLY=( $(compgen -W "${_ark_mcp_flags} ${_ark_mcp_opts_arg}" -- "$cur") ) ;;
    mcp-init)              COMPREPLY=( $(compgen -W "${_ark_mcp_init_opts}" -- "$cur") ) ;;
    setup)                 COMPREPLY=( $(compgen -W "${_ark_setup_opts}" -- "$cur") ) ;;
    syntax)                COMPREPLY=( $(compgen -W "--lang --format -h --help" -- "$cur") ) ;;
    symbol)                COMPREPLY=( $(compgen -W "--lang --format -h --help" -- "$cur") ) ;;
    skill)                 COMPREPLY=( $(compgen -W "${_ark_skill_opts}" -- "$cur") ) ;;
    skill-init|skill-add-explorer) COMPREPLY=( $(compgen -W "${_ark_skill_init_opts}" -- "$cur") ) ;;
    skill-update)          COMPREPLY=( $(compgen -W "${_ark_skill_update_opts}" -- "$cur") ) ;;
    skill-inspect)         COMPREPLY=( $(compgen -W "${_ark_skill_inspect_opts}" -- "$cur") ) ;;
    *)                     COMPREPLY=( $(compgen -W "${_ark_gen_flags} ${_ark_gen_opts_arg} ${_ark_subcommands}" -- "$cur") ) ;;
  esac
}

###############################
# Zsh part (native)
###############################
_ark_zsh() {
  local context state
  typeset -A opt_args

  local -a general_opts=(
    '--help[-h]' '--version[-v]' '--compless[-c]' '--silent[-S]'
    '--skip-non-utf8[-s]' '--delete-comment[-D]'
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
    '--exclude-file-regex[-g]:regexp:'
    '--exclude-dir-regex[-G]:regexp:'
    '--exclude-ext[-e]:extensions:(go js ts py java c cpp h txt md html css xml yml yaml json)'
    '--exclude-dir[-E]:dirname:'
  )

  local -a mcp_opts=(
    '--root[-r]:root directory:_files -/'
    '--type[-t]:mcp type:(stdio http)'
    '--http-port[-p]:port number:(8008 8522 8080 9000)'
    '--scan-buffer[-b]:buffer size:(1M 5M 10M 100K)'
    '--mask-secrets[-m]:on/off:(on off)'
    '--allow-gitignore[-a]:on/off:(on off)'
    '--additionally-ignorerule[-A]:ignore rule file:_files'
    '--ignore-dotfile[-d]:on/off:(on off)'
    '--pattern-regex[-x]:regexp:'
    '--include-ext[-i]:extensions:(go js ts py java c cpp h txt md html css xml yml yaml json)'
    '--exclude-file-regex[-g]:regexp:'
    '--exclude-dir-regex[-G]:regexp:'
    '--exclude-ext[-e]:extensions:(go js ts py java c cpp h txt md html css xml yml yaml json)'
    '--exclude-dir[-E]:dirname:'
    '--skip-non-utf8[-s]'
    '--delete-comment[-D]'
    '--help[-h]' '--version[-v]'
  )

  local -a syntax_opts=(
    '--lang:Language:(go typescript tsx javascript python)'
    '--format:Output format:(text json)'
    '-h[Show help]'
    '--help[Show help]'
  )

  local -a symbol_opts=(
    '--lang:Language:(go typescript tsx javascript python)'
    '--format:Output format:(text json)'
    '-h[Show help]'
    '--help[Show help]'
  )

  local -a skill_opts=(
    '1:subcommand:(init add-explorer update inspect)'
    '--name:Skill name:'
    '--output:Output directory:_files -/'
    '--archive[Create ZIP archive]'
    '--force[Overwrite existing skill]'
    '-h[Show help]'
    '--help[Show help]'
  )

  local -a setup_opts=(
    '--name[-n]:Project name:'
    '--ark-path[-p]:Path to ark binary:_files'
    '--root[-r]:Root directory:_files -/'
    '--global[-g]'
    '--force[-f]'
    '-h[Show help]'
    '--help[Show help]'
  )

  local -a mcp_init_opts=(
    '--name[-n]:MCP server name:'
    '--ark-path[-p]:Path to ark binary:_files'
    '--root[-r]:Root directory:_files -/'
    '--global[-g]'
    '--force[-f]'
    '-h[Show help]'
    '--help[Show help]'
  )

  local -a subcommands
  subcommands=(
    'mcp-server:Start MCP server'
    'mcp-init:Register MCP server entry in settings.json'
    'setup:Setup MCP server and Claude Code skill'
    'syntax:Parse file and output AST'
    'symbol:Extract symbols from file'
    'skill:Generate Cline/ChatGPT Skill'
  )

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
        mcp-init)
          _arguments -C "${mcp_init_opts[@]}"
          ;;
        setup)
          _arguments -C "${setup_opts[@]}"
          ;;
        syntax)
          _arguments -C "${syntax_opts[@]}" '*:file:_files'
          ;;
        symbol)
          _arguments -C "${symbol_opts[@]}" '*:file:_files'
          ;;
        skill)
          case $words[2] in
            init|add-explorer)
              _arguments -C \
                '--name:Skill name:' '--output:Output dir:_files -/' \
                '--archive[Create ZIP archive]' '-h[Show help]' '--help[Show help]'
              ;;
            update)
              _arguments -C \
                '--force[Force update]' '--dry-run[Dry run]' \
                '-h[Show help]' '--help[Show help]'
              ;;
            inspect)
              _arguments -C '-h[Show help]' '--help[Show help]'
              ;;
            *)
              _arguments -C "${skill_opts[@]}" \
                '1:subcommand:(init add-explorer update inspect)'
              ;;
          esac
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

