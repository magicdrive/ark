# Bash completion for ark
_ark_completion() {
  local cur prev words cword
  _init_completion -n := || return

  local opts="\
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
    --skip-non-utf8 -s"

  case $prev in
    --output-format|-o)
      COMPREPLY=( $( compgen -W "txt md" -- "$cur" ) )
      return
      ;;
    --with-line-number|-n|--ignore-dotfile|-d|--allow-gitignore|-a|--mask-secrets|-m)
      COMPREPLY=( $( compgen -W "on off" -- "$cur" ) )
      return
      ;;
    --scan-buffer|-b)
      COMPREPLY=( $( compgen -W "1K 100K 1M 10M 100M" -- "$cur" ) )
      return
      ;;
    --include-ext|-i|--exclude-ext|-e)
      COMPREPLY=( $( compgen -W "go js ts html css md json txt" -- "$cur" ) )
      return
      ;;
    --exclude-dir|-E|--additionally-ignorerule|-p)
      _filedir
      return
      ;;
  esac

  COMPREPLY=( $( compgen -W "$opts" -- "$cur" ) )
}
complete -F _ark_completion ark

