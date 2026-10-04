package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompletionsListAllClients guards against drift between the supported-client
// registry and the hand-written shell completions (plan §21). Every supported
// client must appear in every completion file.
func TestCompletionsListAllClients(t *testing.T) {
	files := map[string]string{
		"bash": filepath.Join("..", "..", "misc", "completions", "bash", "ark-completion.bash"),
		"zsh":  filepath.Join("..", "..", "misc", "completions", "zsh", "_ark"),
		"fish": filepath.Join("..", "..", "misc", "completions", "fish", "ark.fish"),
	}
	for shell, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: cannot read %s: %v", shell, path, err)
		}
		content := string(data)
		for _, id := range SupportedClientStrings() {
			if !strings.Contains(content, id) {
				t.Errorf("%s completion (%s) does not mention supported client %q", shell, path, id)
			}
		}
	}
}

// TestHelpTextListsAllClients guards the embedded CLI help against drift.
func TestHelpTextListsAllClients(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "commandline", "help.txt"))
	if err != nil {
		t.Fatalf("read help.txt: %v", err)
	}
	content := string(data)
	for _, id := range SupportedClientStrings() {
		if !strings.Contains(content, id) {
			t.Errorf("help.txt does not mention supported client %q", id)
		}
	}
}
