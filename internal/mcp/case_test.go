package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// C1: on a case-insensitive file system (macOS by default, Windows) an
// excluded file named in another case, below a directory named in another
// case, or (Windows) through an alternate data stream is refused by the path
// gate; what the rules do not name stays readable in any spelling, and the
// tool is handed the path as the file system names it.
func TestCase_PathGate(t *testing.T) {
	root := policyRepo(t, "secret.txt", "Secrets/key.txt", "public.txt", "src/private.txt", "src/open.txt")
	write(t, root, ".arkignore", "secret.txt\nSecrets/\n")
	write(t, root, "src/.arkignore", "private.txt\n")
	probe := filepath.Join(root, "case-probe")
	os.WriteFile(probe, nil, 0o644)
	_, err := os.Lstat(filepath.Join(root, "CASE-PROBE"))
	os.Remove(probe)
	if err != nil {
		if os.Getenv("ARK_REQUIRE_CASE_FOLDING") != "" {
			t.Fatalf("ARK_REQUIRE_CASE_FOLDING: the file system at %s is case-sensitive", root)
		}
		t.Skipf("SKIP: the file system at %s is case-sensitive: a case-folding bypass is not possible here", root)
	}
	h := serverHandler(t, root, root).forRequest()
	refused := []string{"SECRET.TXT", "Secret.txt", "SECRETS/key.txt", "secrets/KEY.TXT", "SRC/PRIVATE.TXT", "src/Private.txt"}
	if runtime.GOOS == "windows" {
		refused = append(refused, "secret.txt::$DATA", "SECRET.TXT::$DATA", "secret.txt:x", `SECRETS\key.txt`,
			"secret.txt.", "secret.txt ", "SECRET.TXT. .", "Secrets./key.txt")
	}
	for _, p := range refused {
		text, isErr := callText(t, h, "get_file_content", map[string]interface{}{"path": p})
		if !isErr || strings.Contains(text, "secret.txt\n") || strings.Contains(text, "key.txt\n") || strings.Contains(text, "private.txt\n") {
			t.Errorf("%s: %q (error %v)", p, text, isErr)
		}
	}
	for p, want := range map[string]string{"PUBLIC.TXT": "public.txt\n", "SRC/OPEN.TXT": "src/open.txt\n"} {
		if text, isErr := callText(t, h, "get_file_content", map[string]interface{}{"path": p}); isErr || !strings.Contains(text, want) {
			t.Errorf("%s: %q (error %v)", p, text, isErr)
		}
	}
	if _, rel, err := h.resolveToolPath("SRC/OPEN.TXT"); err != nil || rel != filepath.FromSlash("src/open.txt") {
		t.Errorf("resolveToolPath(SRC/OPEN.TXT) = %q %v, want src/open.txt", rel, err)
	}
}
