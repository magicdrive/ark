package integration

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// copyProviderFixture copies a provider's graph fixture
// (internal/languages/<lang>/testdata/repo) into a new project directory.
func copyProviderFixture(t *testing.T, lang string) string {
	t.Helper()
	src := filepath.Join("..", "..", "internal", "languages", lang, "testdata", "repo")
	proj := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(proj, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return proj
}

func mustContain(t *testing.T, what, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, text)
		}
	}
}

// The TOML scenarios through the real binary: a Cargo manifest's package
// and its dependencies, the workspace, a pyproject's project and its build
// system's requirements, any table as a symbol — and the CLI shows the
// same declarations.
func TestMCPTOML_Scenarios(t *testing.T) {
	bin := buildArk(t)
	proj := copyProviderFixture(t, "toml")
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	text, _ := c.tool("search_code", map[string]any{"path": ".", "kind": "package"})
	mustContain(t, "packages", text, "crates/app/Cargo.toml#/package", "py/pyproject.toml#/project")
	text, _ = c.tool("get_callees", map[string]any{"path": ".", "symbol": "crates/app/Cargo.toml#/package"})
	mustContain(t, "app's dependencies", text, `"to": "crates/app/Cargo.toml#/dependencies/serde"`, `"kind": "depends_on"`)
	text, _ = c.tool("get_callees", map[string]any{"path": ".", "symbol": "Cargo.toml#/workspace"})
	mustContain(t, "workspace", text, `"to": "Cargo.toml#/workspace/members"`, `"kind": "contains"`)
	text, _ = c.tool("search_code", map[string]any{"path": ".", "kind": "dependency", "filePattern": "py/"})
	mustContain(t, "pyproject dependencies", text, "py/pyproject.toml#/build-system/requires/setuptools", "py/pyproject.toml#/project/dependencies/requests")
	text, _ = c.tool("search_code", map[string]any{"path": ".", "kind": "table", "filePattern": "config/app.toml"})
	mustContain(t, "tables", text, "config/app.toml#/database")

	cmd := exec.Command(bin, "symbol", "crates/app/Cargo.toml")
	cmd.Dir = proj
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ark symbol: %v\n%s", err, out)
	}
	mustContain(t, "ark symbol", string(out), "(toml)", "package app", "key /package/name", "dependency")
}

// The XML scenarios: a POM's project, its dependencies, attributes of a
// namespaced document; a DOCTYPE with an external entity is reported, not
// read.
func TestMCPXML_Scenarios(t *testing.T) {
	bin := buildArk(t)
	proj := copyProviderFixture(t, "xml")
	if err := os.WriteFile(filepath.Join(proj, "xxe.xml"), []byte("<?xml version=\"1.0\"?>\n<!DOCTYPE r [<!ENTITY x SYSTEM \"file:///etc/passwd\">]>\n<r>&x;</r>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	text, _ := c.tool("search_code", map[string]any{"path": ".", "kind": "package"})
	mustContain(t, "projects", text, "app/pom.xml::com.example:app", "pom.xml::com.example:parent")
	text, _ = c.tool("search_code", map[string]any{"path": ".", "kind": "dependency", "filePattern": "app/pom.xml"})
	mustContain(t, "dependencies", text, "app/pom.xml::dependencies::com.example:core")
	text, _ = c.tool("search_code", map[string]any{"path": ".", "kind": "attribute", "filePattern": "docs/book.xml"})
	mustContain(t, "attributes", text, "docs/book.xml#/b:book/b:title/@xml:id")
	text, _ = c.tool("get_diagnostics", map[string]any{"path": "."})
	mustContain(t, "diagnostics", text, "xml_external_entity")
	text, _ = c.tool("get_file_content", map[string]any{"path": "xxe.xml"})
	if strings.Contains(text, "root:") {
		t.Errorf("an external entity was read:\n%s", text)
	}
}

// The Markdown scenarios: a README's sections, code fence languages, the
// documents linking a document.
func TestMCPMarkdown_Scenarios(t *testing.T) {
	bin := buildArk(t)
	proj := copyProviderFixture(t, "markdown")
	c := startServer(t, proj, t.TempDir(), bin, "mcp-server", "--root", "./", "--no-cache")
	c.handshake()
	text, _ := c.tool("search_code", map[string]any{"path": ".", "kind": "section", "filePattern": "README.md"})
	mustContain(t, "sections", text, "README.md#example-project", "README.md#usage", "README.md#usage-1")
	text, _ = c.tool("search_code", map[string]any{"path": ".", "kind": "code_block"})
	mustContain(t, "code blocks", text, "README.md#usage::bash", "docs/ops/runbook.md#deploy::sh")
	text, _ = c.tool("get_callers", map[string]any{"path": ".", "symbol": "docs/design.md"})
	mustContain(t, "design's callers", text, `"to": "README.md#example-project"`, `"kind": "linked_from"`)

	cmd := exec.Command(bin, "symbol", "README.md")
	cmd.Dir = proj
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ark symbol: %v\n%s", err, out)
	}
	mustContain(t, "ark symbol", string(out), "(markdown)", "section", "code_block bash")
}
