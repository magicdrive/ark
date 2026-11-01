package markdown

import (
	"reflect"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func sections(x language.Extraction) []string {
	var out []string
	for _, l := range symbolLines(x) {
		if strings.HasPrefix(l, "section ") {
			out = append(out, l)
		}
	}
	return out
}

func refsOf(x language.Extraction, kinds ...string) []string {
	var out []string
	for i, r := range x.References {
		for _, k := range kinds {
			if r.Kind == k {
				out = append(out, refLines(x)[i])
			}
		}
	}
	return out
}

// TestHeadings_ATXAndSetext: ATX headings H1 to H6 (closing sequence and
// all), setext headings of one or more lines, and what is no heading
// (seven '#', no space, four spaces of indentation, an escaped '#').
func TestHeadings_ATXAndSetext(t *testing.T) {
	src := "# One\n## Two ##\n### Three\n#### Four\n##### Five\n###### Six\n####### Seven\n#NoSpace\n    # indented\n\\# escaped\n\nSetext one\n===\n\nSetext\ntwo lines\n---\n"
	x := extract(t, "a.md", src)
	equalLines(t, "sections", sections(x), []string{
		// Lines 7 to 10 are one paragraph, which the sections hold.
		`section a.md#one "One" parent=a.md sig=h1 @1:1-10:11`,
		`section a.md#two "Two" parent=a.md#one sig=h2 @2:1-10:11`,
		`section a.md#three "Three" parent=a.md#two sig=h3 @3:1-10:11`,
		`section a.md#four "Four" parent=a.md#three sig=h4 @4:1-10:11`,
		`section a.md#five "Five" parent=a.md#four sig=h5 @5:1-10:11`,
		`section a.md#six "Six" parent=a.md#five sig=h6 @6:1-10:11`,
		`section a.md#setext-one "Setext one" parent=a.md sig=h1 @12:1-17:4`,
		// A soft line break is "\n" in the text content, which the anchor
		// rule removes (GitHub: "setexttwo-lines").
		`section a.md#setexttwo-lines "Setext two lines" parent=a.md#setext-one sig=h2 @15:1-17:4`,
	})
}

// TestSections_Hierarchy: a section ends before the next heading of its
// level or a higher one — a lower level closes what is deeper, a skipped
// level nests all the same —, trailing blank lines excluded; a heading in a
// block quote or a list item is a section of its own extent, inside the
// section around it. The document contains the top sections, each section
// its subsections.
func TestSections_Hierarchy(t *testing.T) {
	src := "# A\n\ntext\n\n### C\n\nmore\n\n## B\n\n> ## Quoted\n\n- ### Listed\n\n\n# D\n"
	x := extract(t, "a.md", src)
	equalLines(t, "sections", sections(x), []string{
		`section a.md#a "A" parent=a.md sig=h1 @1:1-13:13`,
		`section a.md#c "C" parent=a.md#a sig=h3 @5:1-7:5`,
		`section a.md#b "B" parent=a.md#a sig=h2 @9:1-13:13`,
		`section a.md#quoted "Quoted" parent=a.md#b sig=h2 @11:3-11:12`,
		`section a.md#listed "Listed" parent=a.md#b sig=h3 @13:3-13:13`,
		`section a.md#d "D" parent=a.md sig=h1 @16:1-16:4`,
	})
	equalLines(t, "contains", refsOf(x, "contains"), []string{
		`contains "a.md#a" -> a.md#a [repo kinds=section] in a.md @1:1-1:4`,
		`contains "a.md#c" -> a.md#c [repo kinds=section] in a.md#a @5:1-5:6`,
		`contains "a.md#b" -> a.md#b [repo kinds=section] in a.md#a @9:1-9:5`,
		`contains "a.md#quoted" -> a.md#quoted [repo kinds=section] in a.md#b @11:3-11:12`,
		`contains "a.md#listed" -> a.md#listed [repo kinds=section] in a.md#b @13:3-13:13`,
		`contains "a.md#d" -> a.md#d [repo kinds=section] in a.md @16:1-16:4`,
	})
}

// TestSections_NamesAndAnchors: a section is named by its heading's text
// content (inline markup, escapes and entities resolved, white space
// collapsed) and identified by GitHub's anchor of it, repeats numbered; a
// heading whose text is no name is named by its anchor; one whose anchor
// makes no name declares nothing — but takes its anchor all the same.
func TestSections_NamesAndAnchors(t *testing.T) {
	long := strings.Repeat("x", 300)
	src := "# Install `ark` *now* &amp; [here](b.md) ![img](i.png) <b>bold</b>\n" +
		"# Install ark now  here  bold\n" +
		"# 日本語の見出し 🚀\n" +
		"# 🚀\n" +
		"# 🚀\n" +
		"# Tab\\#1 &#35;2\n" +
		"# " + long + "\n" +
		"# " + long + "\n"
	x := extract(t, "a.md", src)
	equalLines(t, "sections", sections(x), []string{
		`section a.md#install-ark-now--here--bold "Install ark now & here bold" parent=a.md sig=h1 @1:1-1:67`,
		`section a.md#install-ark-now--here--bold-1 "Install ark now here bold" parent=a.md sig=h1 @2:1-2:30`,
		`section a.md#日本語の見出し- "日本語の見出し 🚀" parent=a.md sig=h1 @3:1-3:29`,
		`section a.md# "🚀" parent=a.md sig=h1 @4:1-4:7`,
		`section a.md#-1 "🚀" parent=a.md sig=h1 @5:1-5:7`,
		`section a.md#tab1-2 "Tab#1 #2" parent=a.md sig=h1 @6:1-6:16`,
	})
	for _, s := range x.Symbols {
		if strings.Contains(s.Qualified, "xxx") || strings.Contains(s.Name, "xxx") {
			t.Errorf("a heading of 300 bytes declares %q", s.Qualified)
		}
	}
}

// TestLinks_Forms: an inline link and image, the three reference link
// forms (labels matched case-insensitively), an autolink and an extended
// autolink are links; an undefined reference, a definition itself, a
// footnote, a link in a code span, a code block or an HTML block, and a
// link in an image's description are none.
func TestLinks_Forms(t *testing.T) {
	src := "[inline](a.md) ![image](i.png \"t\")\n" +
		"[full][Def] [Def][] [def] [undefined][nope]\n" +
		"<https://example.com/x> see www.example.org.\n" +
		"text[^1] `[code](c.md)`\n" +
		"![outer [inner](in.md)](o.png)\n" +
		"\n" +
		"    [indented](ind.md)\n" +
		"\n" +
		"```\n[fenced](f.md)\n```\n" +
		"\n" +
		"<div>\n[html](h.md)\n</div>\n" +
		"\n" +
		"[def]: d.md\n" +
		"[unused]: u.md\n" +
		"[^1]: note.md\n"
	x := extract(t, "a.md", src)
	equalLines(t, "links", refsOf(x, "resource_link", "id_reference"), []string{
		`resource_link "a.md" -> a.md [file repo] in a.md @1:1-1:15`,
		`resource_link "i.png" -> i.png [file repo kinds=asset] in a.md @1:16-1:35`,
		`resource_link "d.md" -> d.md [file repo] in a.md @2:1-2:12`,
		`resource_link "d.md" -> d.md [file repo] in a.md @2:13-2:20`,
		`resource_link "d.md" -> d.md [file repo] in a.md @2:21-2:26`,
		`resource_link "https://example.com" -> url:https://example.com [] in a.md @3:1-3:24`,
		`resource_link "http://www.example.org" -> url:http://www.example.org [] in a.md @3:29-3:44`,
		`resource_link "o.png" -> o.png [file repo kinds=asset] in a.md @5:1-5:31`,
	})
}

// TestLinks_Destinations: what each destination names (dest.go).
func TestLinks_Destinations(t *testing.T) {
	for _, c := range []struct{ dest, want string }{
		{"#Usage", `id_reference "Usage" -> d/a.md#Usage [repo kinds=section,anchor]`},
		{"#%E6%97%A5%E6%9C%AC", `id_reference "日本" -> d/a.md#日本 [repo kinds=section,anchor]`},
		{"b.md", `resource_link "b.md" -> d/b.md [file repo]`},
		{"./b.md?plain=1", `resource_link "b.md" -> d/b.md [file repo]`},
		{"b.md#sec", `id_reference "sec" -> d/b.md#sec [repo kinds=section,anchor]`},
		{"B.MARKDOWN#sec", `id_reference "sec" -> d/B.MARKDOWN#sec [repo kinds=section,anchor]`},
		{"b.md#", `resource_link "b.md" -> d/b.md [file repo]`},
		{"a.md#self", `id_reference "self" -> d/a.md#self [repo kinds=section,anchor]`},
		{"../top.md", `resource_link "top.md" -> top.md [file repo]`},
		{"sub/my%20doc.md", `resource_link "my doc.md" -> d/sub/my doc.md [file repo]`},
		{"<sub/my doc.md>", `resource_link "my doc.md" -> d/sub/my doc.md [file repo]`},
		{"/docs/x.md", `resource_link "x.md" -> docs/x.md [file repo cap=strong]`},
		{"/docs/x.md#sec-1", `id_reference "sec-1" -> docs/x.md#sec-1 [repo cap=strong kinds=section,anchor]`},
		{"page.html#id", `resource_link "page.html" -> d/page.html [file repo]`},
		{"img/a.SVG", `resource_link "a.SVG" -> d/img/a.SVG [file repo kinds=asset]`},
		{"styles.css", `resource_link "styles.css" -> d/styles.css [file repo]`},
		{"../../out.md", `resource_link "../../out.md" ->  []`},
		{"/../out.md", `resource_link "/../out.md" ->  []`},
		{"https://user:pw@Example.COM:8443/a/b?token=x#frag", `resource_link "https://example.com:8443" -> url:https://example.com:8443 []`},
		{"//cdn.example.com/x.js", `resource_link "https://cdn.example.com" -> url:https://cdn.example.com []`},
		{"mailto:someone@example.com", ``},
		{"javascript:alert(1)", ``},
		{"data:text/plain,x", ``},
		{"tel:+100", ``},
		{"sub%2Fx.md", ``},
		{"a\\\\b.md", ``},
		{"%zz.md", ``},
		{"?q=1", ``},
		{"#", ``},
		{"..", ``},
		{"./", `resource_link "d" -> d [file repo]`}, // the directory: no file
		{"#a:~:text=x", ``},
	} {
		x := extract(t, "d/a.md", "[x]("+c.dest+")\n")
		got := ""
		if len(x.References) > 0 {
			got = refLines(x)[0]
			got = got[:strings.Index(got, " in ")]
		}
		if len(x.References) > 1 {
			t.Errorf("%s: %d references", c.dest, len(x.References))
		}
		if got != c.want {
			t.Errorf("[x](%s):\n got  %s\n want %s", c.dest, got, c.want)
		}
	}
}

// TestCodeBlocks: a fenced code block with a language declares a code block
// named by it (the info string's first word), in the section around it,
// from its opening to its closing fence; backticks and tildes, a longer
// closing fence, an unclosed fence (to the end of the document), a fence in
// a list item. A block without a language, an indented code block, and an
// info string that is no name declare nothing; a heading or a link inside a
// block is text.
func TestCodeBlocks(t *testing.T) {
	src := "# A\n\n```go title=\"x\"\nfunc main() {}\n# not a heading [x](y.md)\n```\n\n~~~ Shell\nls\n~~~~\n\n```\nplain\n```\n\n    indented\n\n- item\n\n  ```py\n  pass\n  ```\n\n```\x01bad\n```\n\n```yaml\nopen: true\n"
	x := extract(t, "a.md", src)
	var got []string
	for _, l := range symbolLines(x) {
		if strings.HasPrefix(l, "code_block ") {
			got = append(got, l)
		}
	}
	equalLines(t, "code blocks", got, []string{
		`code_block a.md#a::go "go" parent=a.md#a sig= @3:1-6:4`,
		`code_block a.md#a::Shell "Shell" parent=a.md#a sig= @8:1-10:5`,
		`code_block a.md#a::py "py" parent=a.md#a sig= @20:1-22:6`,
		`code_block a.md#a::yaml "yaml" parent=a.md#a sig= @27:1-28:11`,
	})
	if len(sections(x)) != 1 || len(refsOf(x, "resource_link")) != 0 {
		t.Errorf("a code block's text declares or links: %v %v", sections(x), refsOf(x, "resource_link"))
	}
}

// TestPositions_UnicodeAndCRLF: lines are 1-based and end at '\n'; columns
// count bytes from 1 (a CR before the '\n' is the line's last byte, never a
// position's line break); multi-byte text shifts columns by its bytes.
func TestPositions_UnicodeAndCRLF(t *testing.T) {
	src := "# 見出し\r\n\r\n本文 [リンク](b.md)\r\n\r\nSetext\r\n---\r\n\r\n```go\r\nx\r\n```\r\n"
	x := extract(t, "a.md", src)
	equalLines(t, "symbols", symbolLines(x), []string{
		`document a.md "a.md" parent= sig= @1:1-11:1`,
		`section a.md#見出し "見出し" parent=a.md sig=h1 @1:1-10:4`,
		`section a.md#setext "Setext" parent=a.md#見出し sig=h2 @5:1-10:4`,
		`code_block a.md#setext::go "go" parent=a.md#setext sig= @8:1-10:4`,
	})
	equalLines(t, "links", refsOf(x, "resource_link"), []string{
		`resource_link "b.md" -> b.md [file repo] in a.md#見出し @3:8-3:25`,
	})
	for _, s := range x.Symbols {
		if s.Location.Range.Start.Line == 0 || s.Location.Range.Start.Column == 0 {
			t.Errorf("%s: position %v is not 1-based", s.Qualified, s.Location.Range)
		}
	}
}

// TestHTMLAnchors: an id of raw HTML (a block or inline) and an <a name>
// are anchors a fragment names, in the section around them; a name of any
// other element, an id in a code span or block, and an id that is no name
// are none. A heading's anchor and an HTML id of the same text are two
// declarations of one identity: a link to it is ambiguous.
func TestHTMLAnchors(t *testing.T) {
	src := "# Top\n\n<div id=\"block\">\n<a name=\"old-name\"></a>\n</div>\n\ntext <span id='inline'>x</span> <input name=\"field\"> `<i id=\"code\">`\n\n```\n<b id=\"fenced\">\n```\n\n<p id=\"top\"></p>\n\n[t](#top)\n"
	x := extract(t, "a.md", src)
	var got []string
	for _, l := range symbolLines(x) {
		if strings.HasPrefix(l, "anchor ") {
			got = append(got, l)
		}
	}
	equalLines(t, "anchors", got, []string{
		`anchor a.md#block "block" parent=a.md#top sig= @3:1-3:17`,
		`anchor a.md#old-name "old-name" parent=a.md#top sig= @4:1-4:20`,
		`anchor a.md#inline "inline" parent=a.md#top sig= @7:6-7:24`,
		`anchor a.md#top "top" parent=a.md#top sig= @13:1-13:13`,
	})
}

// TestReferenceDefinitions: a reference link takes its definition's
// destination (the first definition of a label wins; labels match
// case-insensitively, white space collapsed); a definition in a code block
// is text; a definition used by nothing states nothing.
func TestReferenceDefinitions(t *testing.T) {
	src := "[One][a  B] [two][c]\n\n```\n[c]: code.md\n```\n\n[A b]: first.md\n[a b]: second.md\n[unused]: unused.md\n"
	x := extract(t, "a.md", src)
	equalLines(t, "links", refsOf(x, "resource_link"), []string{
		`resource_link "first.md" -> first.md [file repo] in a.md @1:1-1:12`,
	})
}

// TestBounds: a file over maxBytes declares itself only, with a
// diagnostic; past maxSymbols declarations (or maxRefs references) the rest
// is left out, with one diagnostic.
func TestBounds(t *testing.T) {
	big := extract(t, "big.md", "# A\n"+strings.Repeat("x", maxBytes))
	if len(big.Symbols) != 1 || big.Symbols[0].Kind != symbol.KindDocument || len(big.Diagnostics) != 1 || big.Diagnostics[0].Code != CodeTooLarge {
		t.Errorf("a large file: %d symbols, diagnostics %v", len(big.Symbols), big.Diagnostics)
	}
	many := extract(t, "many.md", strings.Repeat("# h\n", maxSymbols+10))
	if len(many.Symbols) != maxSymbols || len(many.Diagnostics) != 1 || many.Diagnostics[0].Code != CodeTruncated {
		t.Errorf("many headings: %d symbols, diagnostics %v", len(many.Symbols), many.Diagnostics)
	}
	links := extract(t, "links.md", strings.Repeat("[x](#y) [x](#y)\n\n", maxRefs/2+10))
	if len(links.References) != maxRefs || len(links.Diagnostics) != 1 || links.Diagnostics[0].Code != CodeTruncated {
		t.Errorf("many links: %d references, diagnostics %v", len(links.References), links.Diagnostics)
	}
}

// TestWindowsFileIDs: identities built from paths are spelled with the
// index's separator — "\" when the file ID is.
func TestWindowsFileIDs(t *testing.T) {
	x := extract(t, `docs\adr\a.md`, "# T\n[d](../design.md#x) [i](img.png) [s](#t)\n")
	equalLines(t, "refs", refsOf(x, "resource_link", "id_reference"), []string{
		`id_reference "x" -> docs\design.md#x [repo kinds=section,anchor] in docs\adr\a.md#t @2:1-2:20`,
		`resource_link "img.png" -> docs\adr\img.png [file repo kinds=asset] in docs\adr\a.md#t @2:21-2:33`,
		`id_reference "t" -> docs\adr\a.md#t [repo kinds=section,anchor] in docs\adr\a.md#t @2:34-2:41`,
	})
}

// TestDeterminism_AndIdentityStability: an extraction is the same every
// time; editing one section's text moves no other section's identity.
func TestDeterminism_AndIdentityStability(t *testing.T) {
	src := "# A\ntext [l](b.md)\n## B\n```go\nx\n```\n# C\n"
	a, b := extract(t, "a.md", src), extract(t, "a.md", src)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two extractions differ")
	}
	edited := extract(t, "a.md", "# A\nmuch more text\n\nand a paragraph\n## B\n```go\nx\ny\n```\n# C\n")
	q := func(x language.Extraction) []string {
		var out []string
		for _, s := range x.Symbols {
			out = append(out, string(s.Kind)+" "+s.Qualified)
		}
		return out
	}
	equalLines(t, "identities after an edit", q(edited), q(a))
}

// TestNoPanic_OnHostileInput: input no Markdown author writes — invalid
// UTF-8, NUL, unbalanced brackets, deep nesting, a lone CR — is read
// without panic, deterministically, with every name a name.
func TestNoPanic_OnHostileInput(t *testing.T) {
	for _, src := range []string{
		"", "\x00", "\xff\xfe# \xff\n", "#\n#\n", "[", "]", "[](", "[x](<", "![", "<", "<a id=", "\r", "#\r# a\r",
		strings.Repeat("[", 5000), strings.Repeat("> ", 2000) + "# deep", strings.Repeat("- ", 2000) + "x",
		strings.Repeat("*", 10000), "```" + strings.Repeat("`", 1000), "[a]: <\n[a]", "# a\n\x00## \x00\n",
	} {
		a := extract(t, "a.md", src)
		b := extract(t, "a.md", src)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%q: two extractions differ", src)
		}
		checkExtraction(t, src, a)
	}
}

// checkExtraction checks the invariants of any extraction: ranges inside
// the file and ordered, names that are names, the containers and parents
// declared.
func checkExtraction(t testing.TB, src string, x language.Extraction) {
	t.Helper()
	declared := map[string]bool{}
	for _, s := range x.Symbols {
		declared[s.Qualified] = true
	}
	end := newLines([]byte(src)).pos(len(src))
	in := func(l source.Location) bool {
		r := l.Range
		before := func(a, b source.Position) bool {
			return a.Line < b.Line || a.Line == b.Line && a.Column <= b.Column
		}
		return r.Start.Line >= 1 && r.Start.Column >= 1 && before(r.Start, r.End) && before(r.End, end)
	}
	for _, s := range x.Symbols {
		if !isName(s.Name) || !isName(s.Qualified) {
			t.Errorf("%q: symbol name %q / %q is no name", src, s.Name, s.Qualified)
		}
		if !in(s.Location) || int(s.EndByte) > len(src) || s.StartByte > s.EndByte {
			t.Errorf("%q: symbol %s at %v (bytes %d-%d) is outside the file", src, s.Qualified, s.Location.Range, s.StartByte, s.EndByte)
		}
		if s.Parent != "" && !declared[s.Parent] {
			t.Errorf("%q: %s's parent %s is not declared", src, s.Qualified, s.Parent)
		}
	}
	for _, r := range x.References {
		if !isName(r.Name) {
			t.Errorf("%q: reference name %q is no name", src, r.Name)
		}
		if !in(r.Location) {
			t.Errorf("%q: reference %s at %v is outside the file", src, r.Name, r.Location.Range)
		}
		if !declared[r.Container] {
			t.Errorf("%q: reference %s's container %s is not declared", src, r.Name, r.Container)
		}
	}
}

// TestFrontMatter: YAML front matter at the file's start is no Markdown —
// no thematic break, no setext heading of its last key —; positions after
// it are the file's. "---" anywhere else is Markdown as usual.
func TestFrontMatter(t *testing.T) {
	x := extract(t, "a.md", "---\r\ntitle: Guide\r\ntags: [a]\r\n---\r\n# Guide\r\ntext\r\n\r\n---\r\nSetext\r\n---\r\n")
	equalLines(t, "sections", sections(x), []string{
		`section a.md#guide "Guide" parent=a.md sig=h1 @5:1-10:4`,
		`section a.md#setext "Setext" parent=a.md#guide sig=h2 @9:1-10:4`,
	})
	if y := extract(t, "b.md", "---\n# Not front matter\n"); len(sections(y)) != 1 {
		t.Errorf("an unclosed front matter is Markdown: %v", sections(y))
	}
}

// TestCodeBlocks_Repeats: the n-th code block of a language in one section
// is "<section>::<language>[n]", the first keeping its name; another
// language, or another section, counts apart.
func TestCodeBlocks_Repeats(t *testing.T) {
	src := "# Foo\n\n```go\na\n```\n\n```go\nb\n```\n\n```py\nc\n```\n\n```go\nd\n```\n\n## Bar\n\n```go\ne\n```\n"
	x := extract(t, "x.md", src)
	var got []string
	for _, l := range symbolLines(x) {
		if strings.HasPrefix(l, "code_block ") {
			got = append(got, l)
		}
	}
	equalLines(t, "code blocks", got, []string{
		`code_block x.md#foo::go "go" parent=x.md#foo sig= @3:1-5:4`,
		`code_block x.md#foo::go[2] "go" parent=x.md#foo sig= @7:1-9:4`,
		`code_block x.md#foo::py "py" parent=x.md#foo sig= @11:1-13:4`,
		`code_block x.md#foo::go[3] "go" parent=x.md#foo sig= @15:1-17:4`,
		`code_block x.md#bar::go "go" parent=x.md#bar sig= @21:1-23:4`,
	})
}

// TestCodeBlocks_RepeatsAreUnique: a language spelled as another's ordinal
// ("go[2]") takes no other block's name.
func TestCodeBlocks_RepeatsAreUnique(t *testing.T) {
	x := extract(t, "x.md", "```go[2]\n```\n```go\n```\n```go\n```\n```go\n```\n```go[2]\n```\n")
	var got []string
	for _, s := range x.Symbols {
		if s.Kind == symbol.KindCodeBlock {
			got = append(got, s.Qualified+" "+s.Name)
		}
	}
	equalLines(t, "code blocks", got, []string{"x.md::go[2] go[2]", "x.md::go go", "x.md::go[3] go", "x.md::go[4] go", "x.md::go[2][2] go[2]"})
}

// TestSecrets_HeadingsAndIDs: a heading or an HTML id whose text holds what
// Ark's recommended secret policy masks — an assignment of a secret's
// name, a token format before an anchor lowers its case — declares no
// section or anchor: no declaration's name or qualified name holds it, its
// text is the document's (a link in it is contained by the section
// around); the other headings, their anchors' repeats included, are as
// before.
func TestSecrets_HeadingsAndIDs(t *testing.T) {
	src := "# Setup\n\n## password = hunter2secretvalue\n\n[in](in.md)\n\n## aws AK" + "IAIOSFODNN7EXAMPLE\n\n<div id=\"api_key=hunter3secretvalue\"></div>\n<a id=\"kept\"></a>\n\n## Password reset\n\n# Setup\n\n[a](#password--hunter2secretvalue) [b](#aws-akiaiosfodnn7example)\n"
	x := extract(t, "a.md", src)
	for _, s := range x.Symbols {
		for _, leak := range []string{"hunter2", "hunter3", "AKIA", "akia"} {
			if strings.Contains(s.Name, leak) || strings.Contains(s.Qualified, leak) {
				t.Errorf("%s %q (%s) shows a secret", s.Kind, s.Name, s.Qualified)
			}
		}
	}
	equalLines(t, "sections", sections(x), []string{
		`section a.md#setup "Setup" parent=a.md sig=h1 @1:1-12:18`,
		`section a.md#password-reset "Password reset" parent=a.md#setup sig=h2 @12:1-12:18`,
		`section a.md#setup-1 "Setup" parent=a.md sig=h1 @14:1-16:66`,
	})
	equalLines(t, "links", refsOf(x, "resource_link"), []string{
		`resource_link "in.md" -> in.md [file repo] in a.md#setup @5:1-5:12`,
	})
	var anchors []string
	for _, l := range symbolLines(x) {
		if strings.HasPrefix(l, "anchor ") {
			anchors = append(anchors, l)
		}
	}
	equalLines(t, "anchors", anchors, []string{`anchor a.md#kept "kept" parent=a.md#setup sig= @10:1-10:14`})
}

// A heading whose words the secret policy reads as an assignment after a
// "-" ("--password-stdin", "Pass-Through") is a heading (docker/cli's
// login.md, runc's terminals.md): only a value after "=" or ":", or a
// token, is a secret. A code block's language and a linked file name that
// hold a secret declare and reference nothing.
func TestSecrets_WordsAndOtherNames(t *testing.T) {
	src := "# Provide a password using STDIN (--password-stdin)\n\n<a name=\"password-stdin\"></a>\n\n## Pass-Through\n\n## --secret-add\n\n## Token-source behavior\n\n" +
		"```password=hunter4secret\nx\n```\n\n```go\nx\n```\n\n[x](img/password=hunter10secret.png) [y](img/logo.png) [z](#pass-through)\n"
	x := extract(t, "a.md", src)
	equalLines(t, "sections", sections(x), []string{
		`section a.md#provide-a-password-using-stdin---password-stdin "Provide a password using STDIN (--password-stdin)" parent=a.md sig=h1 @1:1-19:74`,
		`section a.md#pass-through "Pass-Through" parent=a.md#provide-a-password-using-stdin---password-stdin sig=h2 @5:1-5:16`,
		`section a.md#--secret-add "--secret-add" parent=a.md#provide-a-password-using-stdin---password-stdin sig=h2 @7:1-7:16`,
		`section a.md#token-source-behavior "Token-source behavior" parent=a.md#provide-a-password-using-stdin---password-stdin sig=h2 @9:1-19:74`,
	})
	all := strings.Join(symbolLines(x), "\n")
	for _, want := range []string{`anchor a.md#password-stdin`, `code_block a.md#token-source-behavior::go "go"`} {
		if !strings.Contains(all, want) {
			t.Errorf("no %s in:\n%s", want, all)
		}
	}
	for _, s := range x.Symbols {
		if strings.Contains(s.Name, "hunter") || strings.Contains(s.Qualified, "hunter") {
			t.Errorf("%s %q (%s) shows a secret", s.Kind, s.Name, s.Qualified)
		}
	}
	for _, r := range x.References {
		if strings.Contains(r.Name, "hunter") || strings.Contains(r.NameQualified, "hunter") {
			t.Errorf("reference %q (%s) shows a secret", r.Name, r.NameQualified)
		}
	}
	equalLines(t, "links", refsOf(x, "resource_link"), []string{
		`resource_link "logo.png" -> img/logo.png [file repo kinds=asset] in a.md#token-source-behavior @19:38-19:55`,
	})
}

// A zero-width space does not hide a secret in a heading.
func TestSecrets_FormatCharacters(t *testing.T) {
	x := extract(t, "a.md", "# password​= hunterA2\n\n# ok\n")
	for _, s := range x.Symbols {
		if strings.Contains(s.Name, "hunter") || strings.Contains(s.Qualified, "hunter") {
			t.Errorf("%s %q (%s) shows a secret", s.Kind, s.Name, s.Qualified)
		}
	}
}
