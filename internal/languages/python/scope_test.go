package python_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/languages/python"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
)

// The scope, module and member fidelity of the Python provider (the Level 2
// table of docs/research/multilang-fidelity/evaluation.md §5): a name a
// local binding, a rebinding or a star import may change, a builtin, an
// external module and a receiver without evidence never resolve Strong or
// Exact; imports, re-exports, self, cls, super() and inherited members
// resolve to the declaration Python's rules make them denote.

// resolution is how Ark resolved one reference.
type resolution struct {
	conf       resolver.Confidence
	target     string   // "file:Qualified" of a unique Strong / Exact target
	candidates []string // "file:Qualified" of every candidate
	outside    bool
}

// resolveAll extracts the files as the index does and returns the
// resolution of every reference by "file:line:name".
func resolveAll(t *testing.T, files map[string]string) map[string][]resolution {
	t.Helper()
	reg := languages.Registry()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var fis []resolver.FileIndex
	for _, name := range names {
		desc, ok := reg.DetectByExtension(filepath.Ext(name))
		if !ok {
			continue
		}
		prov, _ := reg.Provider(desc.Language)
		ex, err := prov.Extract(context.Background(), source.FileID(name), []byte(files[name]))
		if err != nil {
			t.Fatal(err)
		}
		fis = append(fis, index.NewFileIndex(prov, source.FileID(name), ex))
	}
	byID := map[string]string{}
	for _, fi := range fis {
		for _, r := range fi.References {
			byID[string(r.ID)] = fmt.Sprintf("%s:%d:%s", fi.FileID, r.Location.Range.Start.Line, r.Name)
		}
	}
	out := map[string][]resolution{}
	for _, res := range resolver.NewInRoot(fis, "r").Resolve() {
		key := byID[string(res.ReferenceID)]
		r := resolution{conf: res.Confidence, outside: res.OutsideRepository}
		if res.HasUniqueTarget() {
			r.target = fmt.Sprintf("%s:%s", res.Candidates[0].File, res.Candidates[0].Qualified)
		}
		for _, c := range res.Candidates {
			r.candidates = append(r.candidates, fmt.Sprintf("%s:%s", c.File, c.Qualified))
		}
		out[key] = append(out[key], r)
	}
	return out
}

// expect is what the references of one key must resolve to: target
// ("file:Qualified") Strong or Exact — with confidence conf when set — or,
// for target "", no Strong or Exact target at all. cand is a target that
// must stay a Candidate.
type expect struct {
	key    string
	target string
	conf   string
	cand   string
}

func check(t *testing.T, got map[string][]resolution, want []expect) {
	t.Helper()
	// Python states no evidence that places a referent outside the
	// repository: an import names repository files only, and a receiver
	// type is stated only where the lookup finds it.
	for k, rs := range got {
		for _, r := range rs {
			if r.outside && strings.Contains(k, ".py:") {
				t.Errorf("%s: outside the repository %v", k, r.candidates)
			}
		}
	}
	for _, w := range want {
		rs := got[w.key]
		if len(rs) == 0 {
			t.Errorf("%s: no reference (have %s)", w.key, keys(got))
			continue
		}
		for _, r := range rs {
			switch {
			case w.cand != "" && (r.conf != resolver.ConfidenceCandidate || !slices.Contains(r.candidates, w.cand)):
				t.Errorf("%s: resolved %v %v, want the Candidate %q", w.key, r.conf, r.candidates, w.cand)
			case w.cand != "":
			case w.target != "" && r.target != w.target:
				t.Errorf("%s: resolved %v %q %v, want Strong or Exact %q", w.key, r.conf, r.target, r.candidates, w.target)
			case w.target != "" && w.conf != "" && r.conf.String() != w.conf:
				t.Errorf("%s: resolved %v, want %s", w.key, r.conf, w.conf)
			case w.target == "" && r.target != "":
				t.Errorf("%s: resolved %v %q, want no Strong or Exact target", w.key, r.conf, r.target)
			}
		}
	}
}

func keys(m map[string][]resolution) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, " ")
}

type pyCase struct {
	name  string
	files map[string]string
	want  []expect
}

func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

const util = "def helper():\n    pass\n\ndef other():\n    pass\n"

func TestPythonScopes(t *testing.T) {
	cases := []pyCase{
		{
			name: "a parameter shadows an import",
			files: map[string]string{
				"util.py": util,
				"main.py": lines(
					"from util import helper", // 1
					"def f(helper):",          // 2
					"    return helper()",     // 3
					"def g():",                // 4
					"    return helper()",     // 5
				),
			},
			want: []expect{{key: "main.py:3:helper"}, {key: "main.py:5:helper", target: "util.py:helper", conf: "exact"}},
		},
		{
			name: "a name bound anywhere in a function is local to all of it",
			files: map[string]string{
				"main.py": lines(
					"def helper(): pass", // 1
					"def f():",           // 2
					"    helper()",       // 3
					"    helper = 1",     // 4
					"def g():",           // 5
					"    helper()",       // 6
				),
			},
			want: []expect{{key: "main.py:3:helper"}, {key: "main.py:6:helper", target: "main.py:helper"}},
		},
		{
			name: "a class body is no scope of its methods",
			files: map[string]string{
				"main.py": lines(
					"def helper(): pass",         // 1
					"class C:",                   // 2
					"    def helper(self): pass", // 3
					"    def m(self):",           // 4
					"        helper()",           // 5
					"        self.helper()",      // 6
					"    x = helper()",           // 7
				),
			},
			want: []expect{
				{key: "main.py:5:helper", target: "main.py:helper"},
				{key: "main.py:6:helper", target: "main.py:C.helper"},
				{key: "main.py:7:helper", target: "main.py:C.helper"},
			},
		},
		{
			name: "every binding form shadows",
			files: map[string]string{
				"util.py": util,
				"main.py": lines(
					"from util import helper",                // 1
					"def a(xs):",                             // 2
					"    return [helper() for helper in xs]", // 3
					"def b():",                               // 4
					"    try:",                               // 5
					"        pass",                           // 6
					"    except Exception as helper:",        // 7
					"        helper()",                       // 8
					"def c():",                               // 9
					"    with open() as helper:",             // 10
					"        helper()",                       // 11
					"def d(xs):",                             // 12
					"    for helper in xs:",                  // 13
					"        helper()",                       // 14
					"def e(v):",                              // 15
					"    match v:",                           // 16
					"        case [helper]:",                 // 17
					"            helper()",                   // 18
					"def f():",                               // 19
					"    return (lambda helper: helper())",   // 20
					"def g():",                               // 21
					"    if (helper := 1):",                  // 22
					"        helper()",                       // 23
					"def h(xs):",                             // 24
					"    return [helper() for x in xs]",      // 25
					"def i():",                               // 26
					"    helper()",                           // 27
					"def j():",                               // 28
					"    import util as helper",              // 29
					"    helper()",                           // 30
					"def k(*helper):",                        // 31
					"    helper()",                           // 32
				),
			},
			want: []expect{
				{key: "main.py:3:helper"}, {key: "main.py:8:helper"}, {key: "main.py:11:helper"},
				{key: "main.py:14:helper"}, {key: "main.py:18:helper"}, {key: "main.py:20:helper"},
				{key: "main.py:23:helper"}, {key: "main.py:30:helper"}, {key: "main.py:32:helper"},
				{key: "main.py:25:helper", target: "util.py:helper"},
				{key: "main.py:27:helper", target: "util.py:helper"},
			},
		},
		{
			name: "global and nonlocal rebind the enclosing name",
			files: map[string]string{
				"main.py": lines(
					"def helper(): pass", // 1
					"def f():",           // 2
					"    global helper",  // 3
					"    helper = 1",     // 4
					"def g():",           // 5
					"    helper()",       // 6
					"def outer():",       // 7
					"    def inner(): pass",
					"    def k():",           // 9
					"        nonlocal inner", // 10
					"        inner = 2",      // 11
					"    inner()",            // 12
				),
			},
			want: []expect{{key: "main.py:6:helper"}, {key: "main.py:12:inner"}},
		},
		{
			name: "a nested function is the enclosing function's",
			files: map[string]string{
				"main.py": lines(
					"def outer():",           // 1
					"    def helper(): pass", // 2
					"    helper()",           // 3
					"    def deep():",        // 4
					"        helper()",       // 5
					"    return deep",        // 6
					"def other():",           // 7
					"    helper()",           // 8
				),
			},
			want: []expect{
				{key: "main.py:3:helper", target: "main.py:outer.helper", conf: "exact"},
				{key: "main.py:5:helper", target: "main.py:outer.helper"},
				{key: "main.py:8:helper"}, // no module-level helper: a builtin or nothing
			},
		},
		{
			name: "a nested namesake and a module function stay apart",
			files: map[string]string{
				"util.py": util,
				"main.py": lines(
					"from util import other", // 1
					"def helper(): pass",     // 2
					"def outer():",           // 3
					"    def helper(): pass", // 4
					"    def other(): pass",  // 5
					"    helper()",           // 6
					"def g():",               // 7
					"    helper()",           // 8
					"    other()",            // 9
				),
			},
			want: []expect{
				{key: "main.py:6:helper", target: "main.py:outer.helper"},
				// The nested namesakes are outer's alone (LexicalNesting).
				{key: "main.py:8:helper", target: "main.py:helper", conf: "exact"},
				{key: "main.py:9:other", target: "util.py:other", conf: "exact"},
			},
		},
		{
			name: "a module-level rebinding, import or loop hides a def",
			files: map[string]string{
				"util.py": util,
				"main.py": lines(
					"def helper(): pass",      // 1
					"helper = wrap(helper)",   // 2
					"def f():",                // 3
					"    helper()",            // 4
					"from util import other",  // 5
					"def other(): pass",       // 6
					"def g():",                // 7
					"    other()",             // 8
					"for loopvar in (): pass", // 9
					"def loopvar(): pass",     // 10
					"def h():",                // 11
					"    loopvar()",           // 12
					"def gone(): pass",        // 13
					"del gone",                // 14
					"def k():",                // 15
					"    gone()",              // 16
				),
			},
			want: []expect{{key: "main.py:4:helper"}, {key: "main.py:8:other"}, {key: "main.py:12:loopvar"}, {key: "main.py:16:gone"}},
		},
		{
			name: "type parameters shadow in their declaration",
			files: map[string]string{
				"main.py": lines(
					"class T: pass",              // 1
					"def f[T](x: T) -> T:",       // 2
					"    return T()",             // 3
					"class K[T]:",                // 4
					"    def m(self, y: T): ...", // 5
					"def g(x: T): pass",          // 6
				),
			},
			want: []expect{{key: "main.py:2:T"}, {key: "main.py:3:T"}, {key: "main.py:5:T"}, {key: "main.py:6:T", target: "main.py:T"}},
		},
		{
			name: "a variable assigned in two branches has two declarations",
			files: map[string]string{
				"main.py": lines(
					"import sys",                  // 1
					"if sys.platform == 'win32':", // 2
					"    clock = a",               // 3
					"else:",                       // 4
					"    clock = b",               // 5
					"def f():",                    // 6
					"    clock()",                 // 7
				),
			},
			want: []expect{{key: "main.py:7:clock", cand: "main.py:clock"}},
		},
		{
			name: "conditional definitions are no pick",
			files: map[string]string{
				"main.py": lines(
					"import sys",                  // 1
					"if sys.version_info > (3,):", // 2
					"    def compat(): pass",      // 3
					"else:",                       // 4
					"    def compat(): pass",      // 5
					"def run():",                  // 6
					"    compat()",                // 7
				),
			},
			want: []expect{{key: "main.py:7:compat", cand: "main.py:compat"}},
		},
		{
			name: "a builtin, a star import and an undefined name are no repository declaration",
			files: map[string]string{
				"open.py": "def open():\n    pass\ndef run():\n    pass\n",
				"main.py": lines(
					"from somewhere import *", // 1
					"def f():",                // 2
					"    open()",              // 3
					"    run()",               // 4
				),
				"late.py": lines(
					"def helper(): pass",      // 1
					"from somewhere import *", // 2
					"def f():",                // 3
					"    helper()",            // 4
				),
			},
			want: []expect{{key: "main.py:3:open"}, {key: "main.py:4:run"}, {key: "late.py:4:helper"}},
		},
		{
			name: "an overload is no declaration of its own",
			files: map[string]string{
				"main.py": lines(
					"from typing import overload", // 1
					"@overload",                   // 2
					"def f(x: int) -> int: ...",   // 3
					"@overload",                   // 4
					"def f(x: str) -> str: ...",   // 5
					"def f(x):",                   // 6
					"    return x",                // 7
					"def g():",                    // 8
					"    f(1)",                    // 9
				),
			},
			want: []expect{{key: "main.py:9:f", target: "main.py:f", conf: "exact"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { check(t, resolveAll(t, c.files), c.want) })
	}
}

func TestPythonImports(t *testing.T) {
	files := map[string]string{
		"pkg/__init__.py":     "from .core import run\n",
		"pkg/core.py":         "def run():\n    pass\n\ndef _hidden():\n    pass\n",
		"pkg/sub/__init__.py": "",
		"pkg/sub/helpers.py":  "def h():\n    pass\n",
		"pkg/sub/mod.py": lines(
			"from ..core import run as r", // 1
			"from .. import core",         // 2
			"from . import helpers",       // 3
			"def f():",                    // 4
			"    r()",                     // 5
			"    core.run()",              // 6
			"    helpers.h()",             // 7
		),
		"main.py": lines(
			"import pkg",              // 1
			"import pkg.core",         // 2
			"import pkg.core as pc",   // 3
			"from pkg import run",     // 4
			"def go():",               // 5
			"    pkg.run()",           // 6
			"    pkg.core.run()",      // 7
			"    pc.run()",            // 8
			"    run()",               // 9
			"    pkg.core._hidden()",  // 10
			"    pkg.sub.helpers.h()", // 11
		),
		"star.py": lines(
			"from pkg.core import *", // 1
			"def go():",              // 2
			"    run()",              // 3
		),
		// A module inside a package is not on sys.path: the package's json.py
		// is no `import json`; a scripts directory (no package) is.
		"pkg/json.py":     "def loads():\n    pass\n",
		"pkg/a.py":        "import json\ndef f():\n    json.loads()\n",
		"scripts/json.py": "def loads():\n    pass\n",
		"scripts/run.py":  "import json\ndef f():\n    json.loads()\n",
		// The src layout: src/ is a root.
		"src/lib/__init__.py": "def api():\n    pass\n",
		"tests/test_lib.py":   "import lib\nfrom lib import api\ndef test():\n    lib.api()\n    api()\n",
		// Two roots with one module: no pick.
		"twin/__init__.py":    "",
		"one/twin.py":         "def t():\n    pass\n",
		"one/use.py":          "import twin\ndef f():\n    twin.t()\n",
		"rel/__init__.py":     "",
		"rel/deep/x.py":       "from .... import nothing\ndef f():\n    nothing()\n",
		"sibling/__init__.py": "",
		"sibling/a.py":        "def sa():\n    pass\n",
		"sibling/b.py":        "import a\ndef f():\n    a.sa()\n",
	}
	check(t, resolveAll(t, files), []expect{
		{key: "pkg/sub/mod.py:5:r", target: "pkg/core.py:run", conf: "exact"},
		{key: "pkg/sub/mod.py:6:run", target: "pkg/core.py:run"},
		{key: "pkg/sub/mod.py:7:h", target: "pkg/sub/helpers.py:h"},
		{key: "main.py:6:run", target: "pkg/core.py:run"}, // through the package's re-export
		{key: "main.py:7:run", target: "pkg/core.py:run"},
		{key: "main.py:8:run", target: "pkg/core.py:run"},
		{key: "main.py:9:run", target: "pkg/core.py:run"},
		{key: "main.py:10:_hidden", target: "pkg/core.py:_hidden"},
		{key: "main.py:11:h"},  // pkg.sub is not imported here
		{key: "star.py:3:run"}, // names a star import brings are not modelled
		{key: "pkg/a.py:3:loads"},
		{key: "scripts/run.py:3:loads", target: "scripts/json.py:loads"},
		{key: "tests/test_lib.py:4:api", target: "src/lib/__init__.py:api"},
		{key: "tests/test_lib.py:5:api", target: "src/lib/__init__.py:api"},
		{key: "one/use.py:3:t"}, // one/twin.py, and the package twin/ under the root
		{key: "rel/deep/x.py:3:nothing"},
		{key: "sibling/b.py:3:sa"}, // no implicit relative import (Python 3)
	})
}

func TestPythonLocalImportsAndNamespaces(t *testing.T) {
	files := map[string]string{
		"util.py":  util,
		"other.py": "def helper():\n    pass\n",
		"main.py": lines(
			"from util import helper",      // 1
			"def f():",                     // 2
			"    from other import helper", // 3
			"    helper()",                 // 4
			"    def inner():",             // 5
			"        helper()",             // 6
			"def g():",                     // 7
			"    helper()",                 // 8
			"class C:",                     // 9
			"    from other import helper", // 10
			"    def m(self):",             // 11
			"        helper()",             // 12
			"def h():",                     // 13
			"    import util",              // 14
			"    util.other()",             // 15
			"def k():",                     // 16
			"    util.other()",             // 17
		),
		"methods.py": lines(
			"class K:",                         // 1
			"    def helper(self): pass",       // 2
			"    def m(self):",                 // 3
			"        from other import helper", // 4
			"        helper()",                 // 5
		),
		"shadow.py": lines(
			"def helper(): pass",           // 1
			"def f():",                     // 2
			"    from other import helper", // 3
			"    helper()",                 // 4
		),
		"pkg/__init__.py":     "from .core import run\nfrom .api import api\nfrom .shadowed import shadowed\nfrom .kept import x\nx = 1\n",
		"pkg/kept.py":         "x = 0\ndef k():\n    pass\n",
		"pkg/core.py":         "def run():\n    pass\n",
		"pkg/api.py":          "def api():\n    pass\ndef other():\n    pass\n",
		"pkg/shadowed.py":     "def shadowed():\n    pass\ndef s():\n    pass\n",
		"pkg/sub/__init__.py": "",
		"pkg/sub/deep.py":     "def d():\n    pass\n",
		"use.py": lines(
			"import pkg",           // 1
			"def f():",             // 2
			"    pkg.core.run()",   // 3
			"    pkg.api.other()",  // 4
			"    pkg.shadowed.s()", // 5
			"    pkg.sub.deep.d()", // 6
			"    pkg.nothing.f()",  // 7
			"    pkg.kept.k()",     // 8
		),
	}
	check(t, resolveAll(t, files), []expect{
		{key: "main.py:4:helper", target: "other.py:helper", conf: "exact"},
		{key: "main.py:6:helper", target: "other.py:helper"},
		{key: "main.py:8:helper", target: "util.py:helper"},
		{key: "main.py:12:helper", target: "util.py:helper"}, // a class body's import is no method's
		{key: "main.py:15:other", target: "util.py:other"},
		{key: "main.py:17:other"},                               // util is h's only
		{key: "methods.py:5:helper", target: "other.py:helper"}, // a method of the name is no free name
		// The module's helper would win the same-file stage over the local
		// import: no pick, never the module's function.
		{key: "shadow.py:4:helper"},
		{key: "use.py:3:run", target: "pkg/core.py:run"},
		// api is the submodule's function: from .api import api rebinds
		// the attribute the import system set to the submodule.
		{key: "use.py:4:other"},
		{key: "use.py:5:s"}, // the attribute shadowed is the function of that name
		{key: "use.py:6:d"}, // pkg/__init__.py imports nothing from sub
		{key: "use.py:7:f"},
		{key: "use.py:8:k", target: "pkg/kept.py:k"}, // x is rebound, the submodule attribute kept is not
	})
}

func TestPythonMembers(t *testing.T) {
	files := map[string]string{
		"models.py": lines(
			"class Base:",                    // 1
			"    def save(self): pass",       // 2
			"    def load(self): pass",       // 3
			"class Child(Base):",             // 4
			"    def save(self):",            // 5
			"        super().save()",         // 6
			"        self.load()",            // 7
			"        self.save()",            // 8
			"    @classmethod",               // 9
			"    def make(cls):",             // 10
			"        return cls.load()",      // 11
			"    @staticmethod",              // 12
			"    def util(self):",            // 13
			"        self.save()",            // 14
			"    @some_decorator",            // 15
			"    def odd(self):",             // 16
			"        self.save()",            // 17
			"class Mixin:",                   // 18
			"    def load(self): pass",       // 19
			"class Multi(Child, Mixin):",     // 20
			"    def run(self):",             // 21
			"        self.load()",            // 22
			"        super().run()",          // 23
			"class Ext(dict):",               // 24
			"    def run(self):",             // 25
			"        self.keys()",            // 26
			"class Shadow:",                  // 27
			"    def handler(self): pass",    // 28
			"    def __init__(self, h):",     // 29
			"        self.handler = h",       // 30
			"    def run(self):",             // 31
			"        self.handler()",         // 32
			"class Rebound:",                 // 33
			"    def m(self): pass",          // 34
			"    def run(self):",             // 35
			"        self = make()",          // 36
			"        self.m()",               // 37
			"class Made(make_base(), Base):", // 38
			"    def run(self):",             // 39
			"        self.load()",            // 40
			"class Closure:",                 // 41
			"    def m(self): pass",          // 42
			"    def run(self):",             // 43
			"        def inner():",           // 44
			"            self.m()",           // 45
			"        return inner",           // 46
			"class Mangled(Base):",           // 47
			"    def __x(self): pass",        // 48
			"class Sub(Mangled):",            // 49
			"    def run(self):",             // 50
			"        self.__x()",             // 51
			"    def __x(self): pass",        // 52
			"class Attr:",                    // 53
			"    m = None",                   // 54
			"    def m(self): pass",          // 55
			"    def run(self):",             // 56
			"        self.m()",               // 57
			"class Outer:",                   // 58
			"    class Inner(Base):",         // 59
			"        def run(self):",         // 60
			"            self.save()",        // 61
		),
		"use.py": lines(
			"from models import Child, Base", // 1
			"def f(c):",                      // 2
			"    Child.save(c)",              // 3
			"    Base.load(c)",               // 4
			"    Child.load(c)",              // 5
			"    c.save()",                   // 6
		),
		"other.py": "class app:\n    def run(self):\n        pass\n",
		"script.py": lines(
			"app = make()", // 1
			"app.run()",    // 2
		),
	}
	check(t, resolveAll(t, files), []expect{
		{key: "models.py:6:save", target: "models.py:Base.save", conf: "strong"},
		{key: "models.py:7:load", target: "models.py:Base.load"},
		{key: "models.py:8:save", target: "models.py:Child.save", conf: "exact"},
		{key: "models.py:11:load", target: "models.py:Base.load"},
		{key: "models.py:14:save"},
		{key: "models.py:17:save"},
		{key: "models.py:22:load"}, // Base.load through Child, and Mixin.load: no pick
		{key: "models.py:23:run"},
		{key: "models.py:26:keys"},
		{key: "models.py:32:handler"},
		{key: "models.py:37:m"},
		{key: "models.py:40:load"}, // an unidentified base comes first
		{key: "models.py:45:m", target: "models.py:Closure.m"},
		{key: "models.py:51:__x", target: "models.py:Sub.__x"},
		{key: "models.py:57:m"},
		{key: "models.py:61:save"}, // a class nested in a class body: no receiver type, never outside
		{key: "use.py:3:save", target: "models.py:Child.save"},
		{key: "use.py:4:load", target: "models.py:Base.load"},
		{key: "use.py:5:load", target: "models.py:Base.load"},
		{key: "use.py:6:save"},
		{key: "script.py:2:run"},
	})
}

func TestPythonDecoratorsAndDynamic(t *testing.T) {
	files := map[string]string{
		"main.py": lines(
			"import functools",            // 1
			"from functools import wraps", // 2
			"def deco(f):",                // 3
			"    @wraps(f)",               // 4
			"    def wrapper(*a):",        // 5
			"        return f(*a)",        // 6
			"    return wrapper",          // 7
			"@deco",                       // 8
			"def decorated(): pass",       // 9
			"@functools.lru_cache",        // 10
			"def cached(): pass",          // 11
			"def run(o, n, k):",           // 12
			"    decorated()",             // 13
			"    cached()",                // 14
			"    getattr(o, n)()",         // 15
			"    o[k]()",                  // 16
			"def property(f): return f",   // 17
			"class P:",                    // 18
			"    @property",               // 19
			"    def v(self):",            // 20
			"        self.w()",            // 21
			"    def w(self): pass",       // 22
		),
	}
	got := resolveAll(t, files)
	check(t, got, []expect{
		{key: "main.py:8:deco", target: "main.py:deco"},
		{key: "main.py:13:decorated", cand: "main.py:decorated"},
		{key: "main.py:14:cached", target: "main.py:cached", conf: "exact"},
		{key: "main.py:15:getattr(o, n)"},
		{key: "main.py:16:o[k]"},
		// property is the module's own function here: an unknown decorator.
		{key: "main.py:21:w"},
	})
}

func TestPythonAnnotationsAndIsolation(t *testing.T) {
	files := map[string]string{
		"models.py": "class User:\n    pass\n",
		"lib.js":    "function helper() {}\nmodule.exports = { helper }\n",
		"main.py": lines(
			"from models import User",   // 1
			"def f(u: User) -> 'User':", // 2
			"    x: list[User] = []",    // 3
			"    helper()",              // 4
			"    return u",              // 5
		),
	}
	got := resolveAll(t, files)
	check(t, got, []expect{
		{key: "main.py:2:User", target: "models.py:User"},
		{key: "main.py:3:User", target: "models.py:User"},
		{key: "main.py:4:helper"},
	})
	// The string annotation is not read: one type use on line 2.
	if n := len(got["main.py:2:User"]); n != 1 {
		t.Errorf("line 2: %d type uses of User, want 1", n)
	}
}

// The counterexamples of the independent audit: each resolved Strong or
// Exact to a declaration Python's rules do not make it denote.
func TestPythonAuditCounterexamples(t *testing.T) {
	files := map[string]string{
		"other.py": "def helper():\n    pass\n",
		// PEP 695: an annotation scope in a class body sees the class's
		// names; a default is evaluated where the def is.
		"pep695.py": lines(
			"def helper(): pass",         // 1
			"class X: pass",              // 2
			"class C:",                   // 3
			"    def helper(self): pass", // 4
			"    class X: pass",          // 5
			"    def m[T](self, a: X, b=helper()): pass", // 6
			"class B: pass",           // 7
			"class Outer:",            // 8
			"    class B: pass",       // 9
			"    class D[T](B): pass", // 10
		),
		// nonlocal names the nearest enclosing function binding the name.
		"nonlocal.py": lines(
			"def outer():",                  // 1
			"    def helper(): pass",        // 2
			"    def mid():",                // 3
			"        def inner():",          // 4
			"            nonlocal helper",   // 5
			"            helper = 1",        // 6
			"        inner()",               // 7
			"    mid()",                     // 8
			"    helper()",                  // 9
			"class C:",                      // 10
			"    def helper(self): pass",    // 11
			"    def m(self):",              // 12
			"        def mid():",            // 13
			"            def inner():",      // 14
			"                nonlocal self", // 15
			"                self = 1",      // 16
			"            inner()",           // 17
			"        mid()",                 // 18
			"        self.helper()",         // 19
		),
		// A module or class body runs top to bottom.
		"order.py": "len([1])\ndef len(x): pass\n",
		"classorder.py": lines(
			"def helper(): pass",         // 1
			"class C:",                   // 2
			"    x = helper()",           // 3
			"    def helper(self): pass", // 4
		),
		// A class body rebinding a member's name.
		"rebind.py": lines(
			"class C:",                       // 1
			"    def helper(self): pass",     // 2
			"    from other import helper",   // 3
			"    def m(self): self.helper()", // 4
			"class D:",                       // 5
			"    def helper(self): pass",     // 6
			"    del helper",                 // 7
			"    def m(self): self.helper()", // 8
			"class E:",                       // 9
			"    def helper(self): pass",     // 10
			"    for helper in []: pass",     // 11
			"    def m(self): self.helper()", // 12
			"class F:",                       // 13
			"    global helper",              // 14
			"    def helper(self): pass",     // 15
			"    def m(self): self.helper()", // 16
		),
		"staticassign.py": "class C:\n    def helper(self): pass\n    def f(x):\n        x.helper()\n    f = staticmethod(f)\n",
		"baseattr.py": lines(
			"class B:", // 1
			"    def __init__(self): self.helper = 1", // 2
			"class D(B):",                    // 3
			"    def helper(self): pass",     // 4
			"    def m(self): self.helper()", // 5
		),
		// from pkg import n: an n the package binds wins over the submodule.
		"pkg7/__init__.py":  "try:\n    from ._native import n\nexcept ImportError:\n    n = None\nfor m in range(3): pass\n",
		"pkg7/n.py":         "def run(): pass\n",
		"pkg7/m.py":         "def run(): pass\n",
		"use7.py":           "from pkg7 import n, m\nn.run()\nm.run()\n",
		"pkg8/__init__.py":  "from .impl import *\n",
		"pkg8/impl.py":      "def n(): pass\n",
		"pkg8/n.py":         "def run(): pass\n",
		"use8.py":           "from pkg8 import n\nn.run()\n",
		"pkg9/__init__.py":  "def __getattr__(name): return 1\n",
		"pkg9/thing.py":     "def run(): pass\n",
		"use9.py":           "from pkg9 import thing\nthing.run()\n",
		"pkg10/__init__.py": "",
		"pkg10/sub.py":      "def run(): pass\n",
		"use10.py":          "from pkg10 import sub\nsub.run()\n",
		"pkg11/__init__.py": "def sub(): pass\nfrom .sub import run\n",
		"pkg11/sub.py":      "def run(): pass\n",
		"use11.py":          "import pkg11\npkg11.sub()\n",
		"dup.py":            "class C:\n    def helper(self): pass\nclass C:\n    def other(self): pass\nC.helper()\n",
		"overload.py":       "from other import helper\nfrom mydispatch import overload\n@overload\ndef helper(x): pass\nhelper()\n",
		"patch.py":          "class C:\n    def helper(self): pass\n    def m(self): self.helper()\ndef other(): pass\nC.helper = other\n",
		// A class under an unknown decorator: its members are capped too.
		"deco.py": "def wrap(cls):\n    return cls\n",
		"a.py":    "from deco import wrap\n@wrap\nclass Base:\n    def hello(self): pass\nclass Child(Base):\n    def x(self): return self.hello()\nBase.hello(None)\n",
		"b.py":    "from a import Base\nBase.hello(None)\n",
	}
	check(t, resolveAll(t, files), []expect{
		{key: "pep695.py:6:X"}, {key: "pep695.py:6:helper"}, {key: "pep695.py:10:B"},
		{key: "nonlocal.py:9:helper"}, {key: "nonlocal.py:19:helper"},
		{key: "order.py:1:len"}, {key: "classorder.py:3:helper"},
		{key: "rebind.py:4:helper"}, {key: "rebind.py:8:helper"}, {key: "rebind.py:12:helper"}, {key: "rebind.py:16:helper"},
		{key: "staticassign.py:4:helper"},
		{key: "baseattr.py:5:helper"},
		{key: "use7.py:2:run"}, {key: "use7.py:3:run"}, {key: "use8.py:2:run"}, {key: "use9.py:2:run"},
		{key: "use10.py:2:run", target: "pkg10/sub.py:run", conf: "exact"}, // the control: a submodule nothing shadows
		{key: "use11.py:2:sub"},
		{key: "dup.py:5:helper"},
		{key: "overload.py:5:helper"},
		{key: "patch.py:3:helper"},
		{key: "a.py:6:hello", cand: "a.py:Base.hello"},
		{key: "a.py:7:hello", cand: "a.py:Base.hello"},
		{key: "b.py:2:hello", cand: "a.py:Base.hello"},
	})
}

// The re-audit's variants, and the cases the mutation audit found no test
// pinning.
func TestPythonAuditVariants(t *testing.T) {
	files := map[string]string{
		"other.py": "def helper():\n    pass\n",
		// An __init__.py importing its own submodule absolutely.
		"pkg/__init__.py": "def sub(): pass\nfrom pkg.sub import run\n",
		"pkg/sub.py":      "def run(): pass\n",
		"usepkg.py":       "import pkg\npkg.sub()\n",
		// A name read in its own definition's header is another binding.
		"header.py": "class TimeoutError(TimeoutError): pass\ndef int(x=int()) -> int: pass\n",
		// An assignment through a subclass hides the base's member.
		"patchsub.py": lines(
			"class C:",                       // 1
			"    def helper(self): pass",     // 2
			"class D(C):",                    // 3
			"    def m(self): self.helper()", // 4
			"D.helper = None",                // 5
		),
		// Annotations under from __future__ import annotations are not
		// evaluated where they are written: a later class is what they name.
		"future.py": lines(
			"from __future__ import annotations", // 1
			"def f(x: Later) -> Later: ...",      // 2
			"class Later: pass",                  // 3
		),
		// An attribute assigned through an unproven first parameter still
		// hides a subclass's method.
		"instattr.py": lines(
			"def deco(f): return f",                // 1
			"class B:",                             // 2
			"    @deco",                            // 3
			"    def setup(self): self.helper = 1", // 4
			"class E(B):",                          // 5
			"    def helper(self): pass",           // 6
			"    def m(self): self.helper()",       // 7
		),
		// A package re-exporting its submodule by name and importing from
		// it: the attribute is the submodule.
		"flaskish/__init__.py":      "from . import json as json\nfrom .json import jsonify as jsonify\ndef __getattr__(name): raise AttributeError(name)\n",
		"flaskish/json/__init__.py": "def loads(): pass\ndef jsonify(): pass\n",
		"useflask.py":               "import flaskish\ndef f():\n    flaskish.json.loads()\n",
		// A declaration directly in the container that is not what a
		// decorator or default (read outside it) names.
		"container.py": lines(
			"def helper(f): return f", // 1
			"@helper",                 // 2
			"def outer(x=helper(1)):", // 3
			"    def helper(): pass",  // 4
		),
		// A module attribute rebound, mixed or after a star import is no
		// export another file resolves through.
		"rebound.py":    "def f(): pass\nfor f in range(3): pass\nfrom other import helper\ndef helper(): pass\ndef g(): pass\nfrom somewhere import *\n",
		"userebound.py": "from rebound import f, helper, g\ndef go():\n    f()\n    helper()\n    g()\n",
		// An assignment expression in a comprehension binds in the function.
		"walrus.py": lines(
			"def f(): pass",                   // 1
			"def use(xs):",                    // 2
			"    [x for x in xs if (f := x)]", // 3
			"    f()",                         // 4
		),
		// Name mangling: a subclass's self.__x is another attribute.
		"mangle.py": lines(
			"class A:",                     // 1
			"    def __x(self): pass",      // 2
			"class B(A):",                  // 3
			"    def go(self): self.__x()", // 4
		),
		// A rebound self declares no attribute of the class.
		"selfrebind.py": lines(
			"class C:",                  // 1
			"    def m(self):",          // 2
			"        self = Other()",    // 3
			"        self.x = 1",        // 4
			"    def n(self): self.x()", // 5
		),
		// A property and its setter are one declaration, of kind property.
		"prop.py": lines(
			"class A:",                  // 1
			"    @property",             // 2
			"    def v(self): return 1", // 3
			"    @v.setter",             // 4
			"    def v(self, x): pass",  // 5
			"    def m(self): self.v()", // 6
		),
	}
	got := resolveAll(t, files)
	check(t, got, []expect{
		{key: "usepkg.py:2:sub"},
		{key: "header.py:1:TimeoutError"}, {key: "header.py:2:int"},
		{key: "patchsub.py:4:helper"},
		{key: "future.py:2:Later", target: "future.py:Later", conf: "exact"},
		{key: "instattr.py:7:helper"},
		{key: "useflask.py:3:loads", target: "flaskish/json/__init__.py:loads", conf: "exact"},
		{key: "container.py:2:helper"}, {key: "container.py:3:helper"},
		{key: "userebound.py:3:f"}, {key: "userebound.py:4:helper"}, {key: "userebound.py:5:g"},
		{key: "walrus.py:4:f"},
		{key: "mangle.py:4:__x"},
		{key: "prop.py:6:v", target: "prop.py:A.v", conf: "exact"},
	})
	// The rebound self declares no field x: n's self.x() has no target to
	// find (and is unresolved, not a Candidate of a fabricated field).
	for _, r := range got["selfrebind.py:5:x"] {
		if len(r.candidates) != 0 {
			t.Errorf("selfrebind.py:5:x: candidates %v, want none (no field from a rebound self)", r.candidates)
		}
	}
}

// The kinds the provider states: a property is no method.
func TestPythonPropertyKind(t *testing.T) {
	ex, err := python.NewProvider().Extract(context.Background(), "p.py", []byte("class A:\n    @property\n    def v(self): return 1\n    @v.setter\n    def v(self, x): pass\n"))
	if err != nil {
		t.Fatal(err)
	}
	var vs []string
	for _, s := range ex.Symbols {
		if s.Name == "v" {
			vs = append(vs, string(s.Kind))
		}
	}
	if strings.Join(vs, ",") != "property" {
		t.Errorf("v declarations %v, want one property", vs)
	}
}

// The third audit round: an absolute import that may name the package's own
// submodule makes a same-named binding unproven, and never states the
// attribute (it may name another package).
func TestPythonOwnSubmodules(t *testing.T) {
	files := map[string]string{
		"pkg/__init__.py":       "def other(): pass\nimport pkg.other as o\n",
		"pkg/other.py":          "def x(): pass\n",
		"usepkg.py":             "import pkg\npkg.other()\n",
		"a/__init__.py":         "",
		"a/b/__init__.py":       "def sub(): pass\nfrom a.b.sub import run\n",
		"a/b/sub.py":            "def run(): pass\n",
		"usea.py":               "import a.b\na.b.sub()\n",
		"utils/__init__.py":     "",
		"utils/helpers.py":      "def f(): pass\n",
		"lib/__init__.py":       "",
		"lib/utils/__init__.py": "from utils.helpers import f\n",
		"lib/utils/helpers.py":  "def g(): pass\n",
		"uselib.py":             "import lib.utils\nlib.utils.helpers.g()\n",
		// An absolute import that may name another package's module, then a
		// relative one of the package's own submodule of that name: which
		// value the attribute holds is the order's (no pick, either order).
		"top/__init__.py":      "",
		"top/helpers.py":       "def top(): pass\n",
		"lib2/__init__.py":     "",
		"lib2/top/__init__.py": "from top import helpers\nfrom .helpers import x\n",
		"lib2/top/helpers.py":  "x = 1\n",
		"uselib2.py":           "import lib2.top\nlib2.top.helpers.top()\n",
		"static.py": lines(
			"class C:",                       // 1
			"    def helper(self): pass",     // 2
			"    @staticmethod",              // 3
			"    def mk(o): o.helper = 1",    // 4
			"    def m(self): self.helper()", // 5
		),
	}
	check(t, resolveAll(t, files), []expect{
		{key: "usepkg.py:2:other"},
		{key: "usea.py:2:sub"},
		{key: "uselib.py:2:g"},
		{key: "uselib2.py:2:top"},
		{key: "static.py:5:helper", target: "static.py:C.helper", conf: "exact"}, // a staticmethod's parameter is no instance
	})
}

// Decorators: the declaration is a symbol whatever its decorators, and its
// body's calls are its edges; a reference to its name resolves to it only
// under known decorators. An unknown one may return another object, so the
// reference stays a Candidate (no edge) — for functions, classes, methods,
// a class's members and what is found through it, and for any stack with an
// unknown decorator in it.
func TestPythonDecoratorConfidence(t *testing.T) {
	files := map[string]string{
		"mylib.py": "def cache(f):\n    return lambda: None\n",
		"lib.py": lines(
			"import functools", // 1
			"import abc",       // 2
			"from functools import lru_cache, wraps, total_ordering", // 3
			"from dataclasses import dataclass",                      // 4
			"from typing import final, override",                     // 5
			"from mylib import cache",                                // 6
			"def replace(f):",                                        // 7
			"    def other(): pass",                                  // 8
			"    return other",                                       // 9
			"def keep(f):",                                           // 10
			"    @wraps(f)",                                          // 11
			"    def wrapper(*a):",                                   // 12
			"        return f(*a)",                                   // 13
			"    return wrapper",                                     // 14
			"def helper(): pass",                                     // 15
			"@replace",                                               // 16
			"def replaced():",                                        // 17
			"    helper()",                                           // 18
			"@functools.lru_cache",                                   // 19
			"def cached(): pass",                                     // 20
			"@lru_cache(maxsize=1)",                                  // 21
			"def cached2(): pass",                                    // 22
			"@functools.cache",                                       // 23
			"@final",                                                 // 24
			"def stacked_known(): pass",                              // 25
			"@functools.cache",                                       // 26
			"@replace",                                               // 27
			"def stacked_unknown(): pass",                            // 28
			"@cache",                                                 // 29
			"def fake_cache(): pass",                                 // 30
			"@dataclass",                                             // 31
			"class Point:",                                           // 32
			"    x: int = 0",                                         // 33
			"    def norm(self): pass",                               // 34
			"@total_ordering",                                        // 35
			"class Version:",                                         // 36
			"    def __lt__(self, o): pass",                          // 37
			"class Root:",                                            // 38
			"    def root(self): pass",                               // 39
			"@replace",                                               // 40
			"class Swapped(Root):",                                   // 41
			"    def own(self): pass",                                // 42
			"class Sub(Swapped):",                                    // 43
			"    def m(self):",                                       // 44
			"        self.own()",                                     // 45
			"        self.root()",                                    // 46
			"class K:",                                               // 47
			"    @staticmethod",                                      // 48
			"    def s(): pass",                                      // 49
			"    @classmethod",                                       // 50
			"    def c(cls): pass",                                   // 51
			"    @property",                                          // 52
			"    def p(self): pass",                                  // 53
			"    @abc.abstractmethod",                                // 54
			"    def a(self): pass",                                  // 55
			"    @override",                                          // 56
			"    def o(self): pass",                                  // 57
			"    @replace",                                           // 58
			"    def u(self): pass",                                  // 59
			"    def run(self):",                                     // 60
			"        self.s()",                                       // 61
			"        self.c()",                                       // 62
			"        self.p()",                                       // 63
			"        self.a()",                                       // 64
			"        self.o()",                                       // 65
			"        self.u()",                                       // 66
			"def go():",                                              // 67
			"    replaced()",                                         // 68
			"    cached()",                                           // 69
			"    cached2()",                                          // 70
			"    stacked_known()",                                    // 71
			"    stacked_unknown()",                                  // 72
			"    fake_cache()",                                       // 73
			"    Point().norm()",                                     // 74
			"    Point()",                                            // 75
			"    Version()",                                          // 76
			"    Swapped()",                                          // 77
			"    keep(helper)",                                       // 78
			"    K.s()",                                              // 79
		),
		"use.py": "from lib import Swapped, Point\ndef f():\n    Swapped()\n    Swapped.own(None)\n    Point.norm(None)\n",
	}
	check(t, resolveAll(t, files), []expect{
		// unknown: a reference to the name is a Candidate; the body's
		// own call is the declaration's.
		{key: "lib.py:68:replaced", cand: "lib.py:replaced"},
		{key: "lib.py:18:helper", target: "lib.py:helper", conf: "exact"},
		{key: "lib.py:16:replace", target: "lib.py:replace", conf: "exact"}, // the decorator itself is called
		// known, alone, called, dotted, stacked
		{key: "lib.py:69:cached", target: "lib.py:cached", conf: "exact"},
		{key: "lib.py:70:cached2", target: "lib.py:cached2", conf: "exact"},
		{key: "lib.py:71:stacked_known", target: "lib.py:stacked_known", conf: "exact"},
		{key: "lib.py:72:stacked_unknown", cand: "lib.py:stacked_unknown"},
		{key: "lib.py:73:fake_cache", cand: "lib.py:fake_cache"}, // mylib's cache is no functools.cache
		{key: "lib.py:13:f"}, // a parameter
		// classes
		{key: "lib.py:75:Point", target: "lib.py:Point", conf: "exact"},
		{key: "lib.py:76:Version", target: "lib.py:Version", conf: "exact"},
		{key: "lib.py:77:Swapped", cand: "lib.py:Swapped"},
		{key: "lib.py:45:own", cand: "lib.py:Swapped.own"},
		{key: "lib.py:46:root"}, // found through the decorated base: not proven
		{key: "use.py:3:Swapped", cand: "lib.py:Swapped"},
		{key: "use.py:4:own", cand: "lib.py:Swapped.own"},
		{key: "use.py:5:norm", target: "lib.py:Point.norm", conf: "exact"},
		// methods
		{key: "lib.py:61:s", target: "lib.py:K.s", conf: "exact"},
		{key: "lib.py:62:c", target: "lib.py:K.c", conf: "exact"},
		{key: "lib.py:63:p", target: "lib.py:K.p", conf: "exact"},
		{key: "lib.py:64:a", target: "lib.py:K.a", conf: "exact"},
		{key: "lib.py:65:o", target: "lib.py:K.o", conf: "exact"},
		{key: "lib.py:66:u", cand: "lib.py:K.u"},
		{key: "lib.py:79:s", target: "lib.py:K.s", conf: "exact"},
	})
}

// The graph keeps a decorated declaration's own calls, and never an edge to
// it through an unknown decorator.
func TestPythonDecoratedGraph(t *testing.T) {
	dir := t.TempDir()
	src := "def replace(f):\n    return print\n\n\ndef helper():\n    pass\n\n\n@replace\ndef decorated():\n    helper()\n\n\ndef caller():\n    decorated()\n"
	if err := os.WriteFile(filepath.Join(dir, "m.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := index.New(context.Background(), dir, []language.Provider{python.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range idx.FindSymbols("") {
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			got = append(got, s.Qualified+" -> "+to.Qualified)
		}
	}
	sort.Strings(got)
	if want := "decorated -> helper,decorated -> replace"; strings.Join(got, ",") != want {
		t.Errorf("edges %v, want %s (caller's call of decorated is no edge)", got, want)
	}
}

// Known decorators are known only as their documented forms are written:
// wraps only called, the identity ones only applied (D(x) decorates with
// x's result), lru_cache / dataclass called only to configure, the
// descriptors only in a class body; a star import or an assignment of the
// file may rebind the name; an accessor only of a property of the class.
func TestPythonDecoratorRecognition(t *testing.T) {
	files := map[string]string{
		"deco.py": "def replace(f):\n    return 42\nstaticmethod = replace\ncache = replace\n",
		"m.py": lines(
			"import functools",                // 1
			"import abc",                      // 2
			"import typing",                   // 3
			"from deco import replace",        // 4
			"@functools.wraps",                // 5
			"def w(): pass",                   // 6
			"@abc.abstractmethod(replace)",    // 7
			"def a1(): pass",                  // 8
			"@typing.final(replace)",          // 9
			"def a2(): pass",                  // 10
			"@functools.cache(replace)",       // 11
			"def a3(): pass",                  // 12
			"@functools.lru_cache(replace)",   // 13
			"def a4(): pass",                  // 14
			"@functools.lru_cache(maxsize=3)", // 15
			"def ok1(): pass",                 // 16
			"@functools.lru_cache(128)",       // 17
			"def ok2(): pass",                 // 18
			"@property",                       // 19
			"def mp(): pass",                  // 20
			"@classmethod",                    // 21
			"def mcm(): pass",                 // 22
			"def go():",                       // 23
			"    w()",                         // 24
			"    a1()",                        // 25
			"    a2()",                        // 26
			"    a3()",                        // 27
			"    a4()",                        // 28
			"    ok1()",                       // 29
			"    ok2()",                       // 30
			"    mp()",                        // 31
			"    mcm()",                       // 32
		),
		"star.py": lines(
			"from functools import cache", // 1
			"from deco import *",          // 2
			"class S:",                    // 3
			"    @staticmethod",           // 4
			"    def s(): pass",           // 5
			"@cache",                      // 6
			"def g(): pass",               // 7
			"def go():",                   // 8
			"    S.s()",                   // 9
			"    g()",                     // 10
		),
		"setter.py": lines(
			"class x:",                     // 1
			"    @staticmethod",            // 2
			"    def setter(f): return 42", // 3
			"@x.setter",                    // 4
			"def x(v): pass",               // 5
			"class P:",                     // 6
			"    @property",                // 7
			"    def v(self): return 1",    // 8
			"    @v.setter",                // 9
			"    def v(self, val): pass",   // 10
			"    def m(self): self.v()",    // 11
			"def go():",                    // 12
			"    x(1)",                     // 13
		),
		"accessor.py": lines(
			"def helper(): pass",                 // 1
			"class Fake:",                        // 2
			"    def setter(self, f): return 42", // 3
			"class R:",                           // 4
			"    @property",                      // 5
			"    def x(self): return 1",          // 6
			"    x = Fake()",                     // 7
			"    @x.setter",                      // 8
			"    def x(self, v):",                // 9
			"        helper()",                   // 10
		),
		"alias.py": lines(
			"import functools as ft",                  // 1
			"import functools",                        // 2
			"from deco import replace",                // 3
			"functools.cache = replace",               // 4
			"@ft.cache",                               // 5
			"def h(): pass",                           // 6
			"import builtins",                         // 7
			"builtins.staticmethod = replace",         // 8
			"class P:",                                // 9
			"    @staticmethod",                       // 10
			"    def s(): pass",                       // 11
			"@ft.lru_cache(maxsize=replace)",          // 12
			"def a3(): pass",                          // 13
			"@ft.lru_cache(maxsize=None, typed=True)", // 14
			"def ok(): pass",                          // 15
			"def go():",                               // 16
			"    h()",                                 // 17
			"    P.s()",                               // 18
			"    a3()",                                // 19
			"    ok()",                                // 20
		),
		"fromafter.py": "import functools\nfrom deco import replace\nfunctools.cache = replace\nfrom functools import cache\n@cache\ndef k(): pass\ndef go():\n    k()\n",
		"patch.py": lines(
			"import functools",          // 1
			"from deco import replace",  // 2
			"functools.cache = replace", // 3
			"@functools.cache",          // 4
			"def h(): pass",             // 5
			"def go():",                 // 6
			"    h()",                   // 7
		),
	}
	check(t, resolveAll(t, files), []expect{
		{key: "m.py:24:w", cand: "m.py:w"},
		{key: "m.py:25:a1", cand: "m.py:a1"},
		{key: "m.py:26:a2", cand: "m.py:a2"},
		{key: "m.py:27:a3", cand: "m.py:a3"},
		{key: "m.py:28:a4", cand: "m.py:a4"},
		{key: "m.py:29:ok1", target: "m.py:ok1", conf: "exact"},
		{key: "m.py:30:ok2", target: "m.py:ok2", conf: "exact"},
		{key: "m.py:31:mp", cand: "m.py:mp"},
		{key: "m.py:32:mcm", cand: "m.py:mcm"},
		{key: "star.py:9:s", cand: "star.py:S.s"},
		{key: "star.py:10:g", cand: "star.py:g"},
		{key: "setter.py:13:x"},
		{key: "setter.py:11:v", target: "setter.py:P.v", conf: "exact"},
		{key: "patch.py:7:h", cand: "patch.py:h"},
		{key: "accessor.py:10:helper", target: "accessor.py:helper", conf: "exact"},
		{key: "fromafter.py:8:k", cand: "fromafter.py:k"},
		{key: "alias.py:17:h", cand: "alias.py:h"},
		{key: "alias.py:18:s", cand: "alias.py:P.s"},
		{key: "alias.py:19:a3", cand: "alias.py:a3"},
		{key: "alias.py:20:ok", target: "alias.py:ok", conf: "exact"},
	})
}

// An instance attribute a base assigns is what a subclass's self.x names
// when nothing else declares x: no method is hidden, nothing is lowered.
func TestPythonInheritedInstanceAttribute(t *testing.T) {
	files := map[string]string{
		"m.py": lines(
			"class B:",                            // 1
			"    def __init__(self): self.cb = 1", // 2
			"class D(B):",                         // 3
			"    def m(self): self.cb()",          // 4
		),
	}
	check(t, resolveAll(t, files), []expect{{key: "m.py:4:cb", target: "m.py:B.cb", conf: "exact"}})
}

// A setter decorated through a name the class body rebound is no accessor:
// its declaration and its body's edges stay.
func TestPythonAccessorAfterRebinding(t *testing.T) {
	ex, err := python.NewProvider().Extract(context.Background(), "a.py", []byte("class Fake:\n    def setter(self, f): return 42\nclass R:\n    @property\n    def x(self): return 1\n    x = Fake()\n    @x.setter\n    def x(self, v):\n        pass\n"))
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, s := range ex.Symbols {
		if s.Qualified == "R.x" {
			kinds = append(kinds, string(s.Kind)+"/"+s.ConfidenceCap)
		}
	}
	sort.Strings(kinds)
	if strings.Join(kinds, ",") != "method/candidate,property/,variable/" && strings.Join(kinds, ",") != "field/,method/candidate,property/" {
		t.Errorf("R.x declarations %v, want the property, the attribute and the setter-named def (capped)", kinds)
	}
}
