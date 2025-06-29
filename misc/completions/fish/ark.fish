# Fish completion script for ark

function __fish_ark_onoff
  echo on off
end

function __fish_ark_formats
  echo txt md xml arklite
end

complete -c ark -s h -l help -d "Show this help message and exit"
complete -c ark -s v -l version -d "Show version"

complete -c ark -s o -l output-filename -r -d "Specify ark output txt filename"
complete -c ark -s b -l scan-buffer -r -d "Specify the line scan buffer size (e.g. 100K, 10M)"
complete -c ark -s f -l output-format -r -a "(__fish_ark_formats)" -d "Specify the format of the output file"
complete -c ark -s m -l mask-secrets -r -a "(__fish_ark_onoff)" -d "Detect secrets and mask output (on/off)"
complete -c ark -s a -l allow-gitignore -r -a "(__fish_ark_onoff)" -d "Enable .gitignore file filter"
complete -c ark -s p -l additionally-ignorerule -r -d "File with additional ignore rules"
complete -c ark -s n -l with-line-number -r -a "(__fish_ark_onoff)" -d "Include line numbers in output"
complete -c ark -s d -l ignore-dotfile -r -a "(__fish_ark_onoff)" -d "Ignore dotfiles"
complete -c ark -s x -l pattern-regex -r -d "Pattern to match files"
complete -c ark -s i -l include-ext -r -d "Include file extensions (comma-separated)"
complete -c ark -s g -l exclude-dir-regex -r -d "Exclude directories by regexp"
complete -c ark -s G -l exclude-file-regex -r -d "Exclude files by regexp"
complete -c ark -s e -l exclude-ext -r -d "Exclude file extensions (comma-separated)"
complete -c ark -s E -l exclude-dir -r -d "Exclude directory names (comma-separated)"
complete -c ark -s c -l compless -d "Compless output with arklite"
complete -c ark -s s -l skip-non-utf8 -d "Skip non-UTF8 files"
complete -c ark -s S -l silent -d "Suppress output messages"
complete -c ark -s D -l delete-comments -d "Delete code comments"

# Positional argument: target directory
complete -c ark -f -a "(__fish_complete_directories)"

