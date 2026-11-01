package css

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The reader's work is linear in the stylesheet: it looks at each token a
// bounded number of times (a block or function it steps over is skipped
// through the bracket table, not read again), and the tokenizer decodes each
// code point a bounded number of times (a lookahead of at most three). The
// counts (reader.steps, reader.tokSteps) do not depend on the machine or its
// load, so the bound holds under the race detector and on a busy CI runner
// alike, and they tell a quadratic reading from a linear one where a time
// bound cannot (TestWorstCases).
//
// The bounds are about twice the most measured: at most 5.71 steps per token
// (declchain; 4.67 over 270 stylesheets of other projects and the
// css-parser-eval synth suite) and 5.50 decodes per byte (blockvalue; 4.83).
// A reading that steps through a skipped block token by token instead of
// jumping over it takes 252 steps per token on blockvalue (and passes
// TestWorstCases); a name read with a lookahead from its start on every code
// point, fifty thousand decodes per byte on longname.
const (
	maxStepsPerToken  = 10
	maxDecodesPerByte = 10
)

// WorstCases are the worst cases of docs/scripts/css-parser-eval/run.sh:
// long runs of rules, nesting far past the bound, block values deep inside
// declarations. Exported (to the tests only) for TestWorstCases.
func WorstCases() map[string]string {
	return map[string]string{
		"media32000":       "@media x{" + strings.Repeat("div{color:red}\n", 32000) + "}",
		"nest4000":         strings.Repeat("a{", 4000),
		"declnest4000":     ".x{" + strings.Repeat("a:b{", 4000),
		"custom4000":       ".x{" + strings.Repeat("--a:{", 4000),
		"paren1m":          ".x{a:" + strings.Repeat("(", 1000000),
		"brace1m":          strings.Repeat("{", 1000000),
		"blockvalue":       ".x{" + strings.Repeat("a:{", 250) + strings.Repeat("b ", 400000) + strings.Repeat("}x", 250) + "}",
		"blockvalue_rules": ".x{" + strings.Repeat("a:{", 200) + strings.Repeat("p{c:d}", 20000) + strings.Repeat("} b", 200) + "}",
	}
}

func TestReaderStepsLinear(t *testing.T) {
	cases := WorstCases()
	// Other paths of the reader and the tokenizer: "!important" after a {}
	// value, a declaration's {} value, url() with whitespace, escapes,
	// comments, and long tokens.
	for name, src := range map[string]string{
		"declchain": ".x{" + strings.Repeat("a:{b}c;", 8000) + "}",
		"important": ".x{" + strings.Repeat("a:{b} ! important;", 8000) + "}",
		"urlws":     ".x{a:" + strings.Repeat("url(   x   ) ", 8000) + "}",
		"escapes":   strings.Repeat(`.\31 23\,x{a:"\"\\`+"\n"+`"}`, 8000),
		"comments":  strings.Repeat("/* x */a{b:c}", 8000) + "/*",
		"longname":  "." + strings.Repeat("a", 200000) + "{}",
		"longvalue": ".x{a:\"" + strings.Repeat("b", 200000) + "\" " + strings.Repeat("9", 200000) + "}",
	} {
		cases[name] = src
	}
	files, _ := filepath.Glob(filepath.Join("testdata", "site", "*", "*.css"))
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		cases[f] = string(src)
	}
	for name, src := range cases {
		r := read([]byte(src))
		if limit := maxStepsPerToken * max(len(r.toks), 1); r.steps > limit {
			t.Errorf("%s: %d steps for %d tokens (at most %d per token): the reading is not linear", name, r.steps, len(r.toks), maxStepsPerToken)
		}
		if limit := maxDecodesPerByte * max(len(src), 1); r.tokSteps > limit {
			t.Errorf("%s: %d decodes for %d bytes (at most %d per byte): the tokenizing is not linear", name, r.tokSteps, len(src), maxDecodesPerByte)
		}
	}
}
