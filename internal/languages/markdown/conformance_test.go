package markdown

import (
	"testing"

	"github.com/magicdrive/ark/internal/conformance"
)

// The conformance cases proposed for internal/conformance/conformance_test.go
// (fixtures()), run here against the provider: the contract, and the
// broken source's error diagnostic.
var (
	conformanceValid = []conformance.Case{
		{Name: "readme", File: "README.md", Source: []byte("# Project\n\nSee [the design](docs/design.md#architecture) and ![logo](img/logo.png).\n\n## Usage\n\n```bash\nmake\n```\n\n[Back to top](#project)\n")},
		{Name: "design", File: "docs/design.md", Source: []byte("Design\n======\n\n## Architecture\n\n> Note: see [ADR 1][adr].\n\n### Index\n\n<a id=\"legacy\"></a>\n| a | b |\n|---|---|\n| [x](#index) | `y` |\n\n[adr]: adr/0001.md\n")},
		{Name: "adr", File: "docs/adr/0001.md", Source: []byte("# ADR 0001: Use Markdown\r\n\r\n## Context\r\n\r\n- [design](../design.md#index)\r\n- <https://example.com/rfc?token=x>\r\n\r\n## Decision\r\n\r\n~~~go\r\nx := 1\r\n~~~\r\n")},
	}
	// The one broken Markdown: bytes that are no UTF-8 text. The heading
	// after them is read all the same.
	conformanceBroken = conformance.Case{Name: "broken", File: "broken.md", Source: []byte("# Good\n\n\xff\xfe broken bytes\n\n## After\n")}
	// No Markdown name is computed at run time: a link whose destination is
	// a script states nothing (Q7 is 0 by design).
	conformanceDynamic = conformance.Case{Name: "dynamic", File: "dyn.md", Source: []byte("# D\n\n[run](javascript:go())\n")}
)

func TestConformance(t *testing.T) {
	conformance.RunContract(t, NewProvider(), conformanceValid)
	conformance.CheckBrokenSourceDiagnosed(t, NewProvider(), conformanceBroken)
	x := extract(t, conformanceBroken.File, string(conformanceBroken.Source))
	if len(sections(x)) != 2 {
		t.Errorf("the broken file's sections: %v", sections(x))
	}
	if d := extract(t, conformanceDynamic.File, string(conformanceDynamic.Source)); len(d.References) != 1 {
		t.Errorf("the dynamic case states %d references, want the contains only", len(d.References))
	}
}
