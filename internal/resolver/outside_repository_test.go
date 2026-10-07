package resolver_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/magicdrive/ark/internal/resolver"
)

// OutsideRepository marks exactly the Unresolved resolutions whose
// authoritative evidence places the referent outside the repository, and never
// changes confidence or candidates.
func TestOutsideRepository(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"app/Models/Request.php": "<?php\nnamespace App\\Models;\n\nclass Request\n{\n    public function input() { return 1; }\n}\n",
		"app/Base.php":           "<?php\nnamespace App;\n\nabstract class Base\n{\n    protected function common() { return 1; }\n}\n",
		"app/Child.php": `<?php
namespace App;

use Illuminate\Http\Request;
use App\Models\Request as Local;

class Child extends Base
{
    public function vendorTyped(Request $r) { return $r->input(); }
    public function vendorStatic() { return Request::capture(); }
    public function vendorNew() { return new Request(); }
    public function localTyped(Local $r) { return $r->input(); }
    public function inherited() { return $this->common(); }
    public function missing() { return nowhere(); }
}
`,
		"src/use.ts": "import { get } from \"axios\";\nimport * as ax from \"axios\";\n\nexport function f() {\n  get();\n  ax.post();\n}\n",
	}
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fis := extractTree(t, root)
	r := resolver.New(fis)

	type key struct{ container, name string }
	got := map[key]resolver.Resolution{}
	for _, fi := range fis {
		for _, ref := range fi.References {
			got[key{ref.Container, ref.Name}] = r.ResolveReference(ref, fi)
		}
	}
	cases := []struct {
		container, name string
		wantConf        resolver.Confidence
		wantOutside     bool
	}{
		{`App\Child.vendorTyped`, "input", resolver.ConfidenceUnresolved, true},    // receiver type not declared
		{`App\Child.vendorStatic`, "capture", resolver.ConfidenceUnresolved, true}, // static scope not declared
		{`App\Child.vendorNew`, "Request", resolver.ConfidenceUnresolved, true},    // class name not declared
		{`App\Child.localTyped`, "input", resolver.ConfidenceExact, false},
		{`App\Child.inherited`, "common", resolver.ConfidenceUnresolved, false}, // repository type, member not on it
		{`App\Child.missing`, "nowhere", resolver.ConfidenceUnresolved, false},  // no evidence at all
		{"f", "get", resolver.ConfidenceUnresolved, true},                       // external named binding
		{"f", "post", resolver.ConfidenceUnresolved, true},                      // external namespace binding
	}
	for _, tc := range cases {
		res, ok := got[key{tc.container, tc.name}]
		if !ok {
			t.Errorf("no reference %s in %s", tc.name, tc.container)
			continue
		}
		if res.Confidence != tc.wantConf || res.OutsideRepository != tc.wantOutside {
			t.Errorf("%s in %s: confidence=%s outside=%v, want %s %v (evidence %v)",
				tc.name, tc.container, res.Confidence, res.OutsideRepository, tc.wantConf, tc.wantOutside, res.Evidence)
		}
		if res.OutsideRepository && len(res.Candidates) != 0 {
			t.Errorf("%s in %s: an outside-repository resolution has candidates", tc.name, tc.container)
		}
	}
}
