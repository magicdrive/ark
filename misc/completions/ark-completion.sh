# ark-completion.sh - Bash & Zsh completion for ark CLI
# Usage: source this file in your shell (bash or zsh)

# -------- Zsh Section --------
if [[ -n ${ZSH_VERSION-} ]]; then
  _ark() {
    local state

    _arguments -C \
      '--help[Show this help message and exit]' \
      '--version[Show version]' \
      '(-o --output-filename)'{-o,--output-filename}'[Specify output filename]:output file:_files' \
      '(-b --scan-buffer)'{-b,--scan-buffer}'[Line scan buffer size]:buffer (e.g. 100K, 10M)' \
      '(-f --output-format)'{-o,--output-format}'[Output file format]:(txt md xml arklite)' \
      '(-m --mask-secrets)'{-m,--mask-secrets}'[mask secret string (on/off)]:(on off)' \
      '(-a --allow-gitignore)'{-a,--allow-gitignore}'[Enable .gitignore(on/off)]:(on off)' \
      '(-p --additionally-ignorerule)'{-p,--additionally-ignorerule}'[Additional ignore rule file]:file:_files' \
      '(-n --with-line-number)'{-n,--with-line-number}'[Line number output (on/off)]:(on off)' \
      '(-d --ignore-dotfile)'{-d,--ignore-dotfile}'[Ignore dotfiles (on/off)]:(on off)' \
      '(-x --pattern-regex)'{-x,--pattern-regex}'[File match pattern]:regexp:' \
      '(-i --include-ext)'{-i,--include-ext}'[Include file extensions]:extensions:' \
      '(-g --exclude-dir-regex)'{-g,--exclude-dir-regex}'[Exclude dir regexp]:regexp:' \
      '(-G --exclude-file-regex)'{-G,--exclude-file-regex}'[Exclude file regexp]:regexp:' \
      '(-e --exclude-ext)'{-e,--exclude-ext}'[Exclude extensions]:extensions:' \
      '(-E --exclude-dir)'{-E,--exclude-dir}'[Exclude directories]:dirnames:' \
      '(-s --skip-non-utf8)'{-s,--skip-non-utf8}'[Ignore non-UTF8 files]' \
      '(-S --silinet)'{-s,--silent}'[Without displaying messages]' \
      '(-D --delete-comments)'{-D,--delete-comments}'[Delete code comments]' \
      '*::dirname:->dir' && return 0

    if [[ $state == dir ]]; then
      _directories
    fi
  }

  compdef _ark ark

# -------- Bash Section --------
elif [[ -n ${BASH_VERSION-} ]]; then
  _ark() {
    local cur prev opts
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    opts="\
    --help -h \
    --version -v \
    --output-filename -o \
    --scan-buffer -b \
    --output-format -o \
    --allow-gitignore -a \
    --mask-secrets -m \
    --additionally-ignorerule -p \
    --with-line-number -n \
    --ignore-dotfile -d \
    --pattern-regex -x \
    --include-ext -i \
    --exclude-dir-regex -g \
    --exclude-file-regex -G \
    --exclude-ext -e \
    --exclude-dir -E \
    --silent -S \
    --delete-comments -D \
    --skip-non-utf8 -s"

    case "$prev" in
      -o|--output-filename|-a|--additionally-ignorerule)
        COMPREPLY=( $(compgen -f -- "$cur") )
        return 0
        ;;
      -E|--exclude-dir|-e|--exclude-ext|-i|--include-ext)
        COMPREPLY=()
        return 0
        ;;
      -f|--output-format)
        COMPREPLY=( $(compgen -W "txt md" -- "$cur") )
        return 0
        ;;
      -n|--with-line-number|-d|--ignore-dotfile|-p|--allow-gitignore|-a|--mask-secrets|-m)
        COMPREPLY=( $(compgen -W "on off" -- "$cur") )
        return 0
        ;;
      *)
        ;;
    esac

    COMPREPLY=( $(compgen -W "${opts}" -- "$cur") )
    return 0
  }

  complete -F _ark ark
fi

