package python

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtractPython verifies the Python provider on arbitrary input: it
// never panics, keeps every range inside the source, never emits an empty
// name, states module bindings that pass the structural contract (clean,
// sorted candidates), names no Dynamic reference's target, and is
// deterministic.
func FuzzExtractPython(f *testing.F) {
	seeds := []string{
		"",
		"def f():\n    return g()\n",
		"from . import a\nfrom ..m.n import b as c, d\nfrom .k import *\nimport x.y.z, w as v\n",
		"class C(B, m.D, metaclass=M):\n    x: int = 1\n    def m(self, p=lambda q: q):\n        super().m()\n        self.x = 2\n        return [i for i in self.it if (j := i)]\n",
		"@dec\n@pkg.dec(1)\nclass C:\n    @property\n    def v(self): ...\n    @v.setter\n    def v(self, x): ...\n",
		"def f():\n    global g\n    nonlocal h\n    import os.path\n    from a import b\n    try:\n        pass\n    except E as e:\n        del e\n",
		"match v:\n    case Point(x=a, y=[b, *rest]) | {\"k\": c, **m} as whole:\n        pass\n",
		"type L[T] = list[T]\ndef g[T: int, *Ts, **P](x: T) -> T: ...\nclass K[T]: ...\n",
		"getattr(o, n)()\no[k]()\nf()()\n(lambda: 1)()\n",
		"from typing import overload\n@overload\ndef f(x: int) -> int: ...\ndef f(x): return x\n",
		"def broken(:\n    pass\ndef good():\n    pass\n",
		"class (:\n def\n import\n from . import\n @\n",
		"if x:\n    def f(): pass\nelse:\n    f = 1\nfor f in (): pass\nwith a as (b, c): pass\n",
		"async def f():\n    async with a as b:\n        async for c in d:\n            await e()\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	p := NewProvider()
	f.Fuzz(func(t *testing.T, src []byte) {
		for _, file := range []string{"pkg/sub/mod.py", "pkg/__init__.py", "main.py"} {
			ext, err := p.Extract(context.Background(), source.FileID(file), src)
			if err != nil {
				return // an honest error is fine; it must not panic
			}
			n := uint32(len(src))
			for _, s := range ext.Symbols {
				if s.Name == "" || s.Qualified == "" || !strings.HasSuffix(s.Qualified, s.Name) {
					t.Fatalf("bad name/qualified: %+v", s)
				}
				if s.StartByte > s.EndByte || s.EndByte > n {
					t.Fatalf("symbol out-of-bounds range [%d,%d] len=%d: %q", s.StartByte, s.EndByte, n, s.Name)
				}
				if s.Location.Range.Start.Line == 0 {
					t.Fatalf("symbol has 0 (non-1-based) line: %q", s.Name)
				}
				if s.Parent != "" && !strings.HasPrefix(s.Qualified, s.Parent+".") {
					t.Fatalf("parent %q is not the qualified name's prefix: %q", s.Parent, s.Qualified)
				}
			}
			for _, r := range ext.References {
				if r.Name == "" {
					t.Fatalf("empty reference name: %+v", r)
				}
				if r.Dynamic && (r.NameQualified != "" || r.ReceiverType != "") {
					t.Fatalf("a Dynamic reference names a target: %+v", r)
				}
				if r.Location.Range.Start.Line == 0 {
					t.Fatalf("reference has 0 (non-1-based) line: %+v", r)
				}
			}
			if errs := language.ValidateModuleBindings(ext); len(errs) > 0 {
				t.Fatalf("module bindings: %v", errs)
			}
			again, _ := p.Extract(context.Background(), source.FileID(file), src)
			if !reflect.DeepEqual(ext, again) {
				t.Fatalf("not deterministic for %q", src)
			}
		}
	})
}
