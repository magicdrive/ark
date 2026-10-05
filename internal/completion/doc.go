// Package completion holds the drift tests that keep Ark's hand-written shell
// completions (misc/completions) equal to the CLI contract.
//
// The completion files are static; the semantic sources of truth live in Go:
//
//   - subcommands and skill sub-subcommands: the dispatcher in cmd/ark/mod.go
//   - flags: the FlagSets built by internal/commandline
//   - setup clients: the registry in internal/setup
//   - --lang values: the language registry in internal/languages
//
// The tests derive the contract from those sources and then run the real
// completion scripts non-interactively (bash: the completion function itself;
// zsh: the script under a real zsh with its _arguments/_describe calls
// captured; fish: static analysis, because fish is not required in CI).
package completion
