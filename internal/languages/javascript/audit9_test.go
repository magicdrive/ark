package javascript_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/languages/javascript"
	"github.com/magicdrive/ark/internal/languages/typescript"
)

// Phase 9 audit (F9A-1, F9A-2): a function's own properties are not its
// prototype's members, and an export a later assignment overwrites is no
// export of its first value.

func relationsTo(t *testing.T, files map[string]string, from string) string {
	t.Helper()
	idx, _ := indexOf(t, files)
	var out []string
	for _, s := range idx.FindSymbolsByQualified(from) {
		for _, e := range idx.GetCallees(s.ID) {
			to, _ := idx.GetSymbol(e.To)
			out = append(out, fmt.Sprintf("%s:%s %s", to.Location.File, to.Qualified, e.Confidence))
		}
		c := idx.CandidateCalleeSample(s.ID)
		u := idx.UnresolvedOutgoing(s.ID)
		out = append(out, fmt.Sprintf("candidates=%d unresolved=%d outside=%d", c.Total, u.Unresolved, u.OutsideRepository))
	}
	return strings.Join(out, "\n")
}

func TestAudit9_ConstructorFunctionReceiver(t *testing.T) {
	got := relationsTo(t, map[string]string{
		"jq.js":    "function jQuery() {}\njQuery.prototype.extend = function () {}\njQuery.extend = function () {}\nmodule.exports = jQuery\n",
		"only.js":  "function Only() {}\nOnly.prototype.run = function () {}\nmodule.exports = Only\n",
		"esm.mjs":  "export default function Emitter() {}\nEmitter.prototype.on = function () {}\n",
		"use.js":   "const $ = require('./jq')\nconst O = require('./only')\nfunction use() { $.extend(); O.run(); new O().run() }\n",
		"use2.mjs": "import E from './esm.mjs'\nexport function use2() { E.on() }\n",
	}, "use")
	if strings.Contains(got, "jQuery.extend") || strings.Contains(got, "Only.run strong") || strings.Contains(got, "outside=1") {
		t.Errorf("a function's property reached a prototype member:\n%s", got)
	}
	got2 := relationsTo(t, map[string]string{
		"esm.mjs":  "export default function Emitter() {}\nEmitter.prototype.on = function () {}\n",
		"use2.mjs": "import E from './esm.mjs'\nexport function use2() { E.on() }\n",
	}, "use2")
	if strings.Contains(got2, "Emitter.on") || strings.Contains(got2, "outside=1") {
		t.Errorf("E.on():\n%s", got2)
	}
}

func TestAudit9_OverwrittenCommonJSExport(t *testing.T) {
	for name, src := range map[string]string{
		"object then property": "function a() {}\nfunction b() {}\nmodule.exports = { a }\nmodule.exports.a = b\n",
		"property twice":       "function a() {}\nexports.a = a\nexports.a = function b() {}\n",
	} {
		got := relationsTo(t, map[string]string{
			"over.js": src,
			"use.js":  "const o = require('./over')\nconst { a } = require('./over')\nfunction use() { o.a(); a() }\n",
		}, "use")
		if strings.Contains(got, "over.js:a exact") || strings.Contains(got, "over.js:a strong") {
			t.Errorf("%s: the overwritten a is the export:\n%s", name, got)
		}
	}
	// Two identifier values stay two exports: ambiguous, never a pick.
	got := relationsTo(t, map[string]string{
		"two.js": "function a() {}\nfunction a2() {}\nexports.a = a\nexports.a = a2\n",
		"use.js": "const t = require('./two')\nfunction use() { t.a() }\n",
	}, "use")
	if strings.Contains(got, " exact") || strings.Contains(got, " strong") {
		t.Errorf("two exports of a:\n%s", got)
	}
}

// F9A-4: a classic script's free name reaches another classic script's
// global, never a module's top-level declaration (exported or not).
func TestAudit9_ScriptDoesNotSeeModules(t *testing.T) {
	got := relationsTo(t, map[string]string{
		"lib/mod.js":    "function nonExported() {}\nexport function exported() {}\n",
		"lib/cjs.js":    "function inCjs() {}\nmodule.exports = { inCjs }\n",
		"lib/global.js": "function globalFn() {}\n",
		"lib/script.js": "function run() { nonExported(); exported(); inCjs(); globalFn() }\n",
	}, "run")
	if strings.Contains(got, "nonExported") || strings.Contains(got, "lib/mod.js:exported") || strings.Contains(got, "inCjs") {
		t.Errorf("a script reached a module's declaration:\n%s", got)
	}
	if !strings.Contains(got, "lib/global.js:globalFn strong") || !strings.Contains(got, "outside=0") {
		t.Errorf("a script's global:\n%s", got)
	}
}

// F9C-4: the JavaScript cache version follows the shared extractor's.
func TestAudit9_CacheVersionJoinsTypeScript(t *testing.T) {
	v := javascript.NewProvider().CacheVersion()
	if !strings.HasPrefix(v, "js-") || !strings.HasSuffix(v, "+"+typescript.NewProvider().CacheVersion()) {
		t.Errorf("CacheVersion %q", v)
	}
}

// F9A-8: the same file's F.run() (a prototype member, through the function
// itself) is no member; a class name a local hides keeps its instance
// members as possible targets.
func TestAudit9_PlacementNegatives(t *testing.T) {
	got := relationsTo(t, map[string]string{
		"f.js": "function F() {}\nF.prototype.run = function () {}\nfunction use() { F.run() }\n",
	}, "use")
	if strings.Contains(got, "F.run strong") || strings.Contains(got, "F.run exact") {
		t.Errorf("F.run() reached the prototype's run:\n%s", got)
	}
	got = relationsTo(t, map[string]string{
		"c.js": "class Clock { tick() {} }\nfunction use(Clock) { Clock.tick() }\n",
	}, "use")
	if !strings.Contains(got, "candidates=1") {
		t.Errorf("a hidden class name lost the instance member candidate:\n%s", got)
	}
}
