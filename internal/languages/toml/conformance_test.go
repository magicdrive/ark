package toml

import (
	"testing"

	"github.com/magicdrive/ark/internal/conformance"
)

// conformanceCases are the fixtures proposed for
// internal/conformance/conformance_test.go (PROPOSAL.md): run here against
// the provider contract until the provider is registered.
var (
	conformanceValid = []conformance.Case{
		{Name: "cargo", File: "crates/app/Cargo.toml", Source: []byte("[package]\nname = \"app\"\nversion = \"0.1.0\"\n\n[dependencies]\ncore = { path = \"../core\" }\nserde = { version = \"1\", features = [\"derive\"] }\n\n[target.'cfg(unix)'.dependencies]\nlibc = \"0.2\"\n\n[features]\ndefault = [\"serde/std\"]\n\n[[bin]]\nname = \"app-cli\"\npath = \"src/main.rs\"\n")},
		{Name: "pyproject", File: "py/pyproject.toml", Source: []byte("[build-system]\nrequires = [\"setuptools>=61\"]\nbuild-backend = \"setuptools.build_meta\"\n\n[project]\nname = \"sample\"\nversion = \"1.0\"\ndependencies = [\"requests>=2\", \"rich; python_version >= '3.9'\"]\n\n[tool.ruff]\nline-length = 100\n")},
		{Name: "data", File: "config/app.toml", Source: []byte("title = \"x\"\n\n[server]\nhost = \"localhost\"\nports = [8000, 8001]\ntls = { cert = \"c.pem\", key = \"k.pem\" }\n\n[[users]]\nname = \"alice\"\n\n[users.prefs]\ntheme = \"dark\"\n")},
	}
	// A syntax error: what precedes it is read, the rest is not.
	conformanceBroken = conformance.Case{Name: "broken", File: "broken.toml", Source: []byte("ok = 1\n[bad\nlater = 2\n")}
	// TOML has no computed names; the nearest is a glob workspace member,
	// observed and Unresolved (not Dynamic).
	conformanceDynamic = conformance.Case{Name: "dynamic", File: "Cargo.toml", Source: []byte("[workspace]\nmembers = [\"crates/*\"]\n")}
)

func TestConformance(t *testing.T) {
	conformance.RunContract(t, NewProvider(), conformanceValid)
	conformance.CheckBrokenSourceDiagnosed(t, NewProvider(), conformanceBroken)
	if ex := extract(t, conformanceDynamic.File, string(conformanceDynamic.Source)); len(ex.References) == 0 {
		t.Error("the glob member is not observed")
	}
}
