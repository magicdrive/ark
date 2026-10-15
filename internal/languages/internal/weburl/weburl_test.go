package weburl

import "testing"

func TestRelative(t *testing.T) {
	for _, c := range []struct {
		file, v      string
		target, frag string
		ok           bool
	}{
		{"d/a.html", "b.html", "d/b.html", "", true},
		{"d/a.html", "./b.html#x", "d/b.html", "x", true},
		{"d/a.html", "../b.htm?q=1#y", "b.htm", "y", true},
		{"d/a.html", "sub/%62.html", "d/sub/b.html", "", true},
		{"d/a.html", "B.HTML", "d/B.HTML", "", true},
		{"a.html", "../b.html", "", "", false},      // outside the repository
		{"d/a.html", "/b.html", "", "", false},      // root-relative
		{"d/a.html", "//cdn/b.html", "", "", false}, // scheme-relative
		{"d/a.html", "https://x/b.html", "", "", false},
		{"d/a.html", "mailto:x@y", "", "", false},
		{"d/a.html", "a\\b.html", "", "", false},    // a backslash is a separator
		{"d/a.html", "sub%2Fb.html", "", "", false}, // an escaped slash
		{"d/a.html", "sub%5cb.html", "", "", false},
		{"d/a.html", "%zz.html", "", "", false}, // an invalid escape
		{"d/a.html", "?q=1", "", "", false},     // a query alone
		{"d/a.html", "sub/", "", "", false},     // a directory
		{"d/a.html", "b.css", "", "", false},    // another extension
		{"d/a.html", "", "", "", false},
	} {
		target, frag, ok := Relative(c.file, c.v, ".html", ".htm")
		if target != c.target || frag != c.frag || ok != c.ok {
			t.Errorf("Relative(%q, %q) = %q %q %v, want %q %q %v", c.file, c.v, target, frag, ok, c.target, c.frag, c.ok)
		}
	}
}

func TestTrimAndScheme(t *testing.T) {
	if got := Trim(" \x01 java\tscr\nipt:x() \x1f"); got != "javascript:x()" {
		t.Errorf("Trim = %q", got)
	}
	for v, want := range map[string]bool{"http:x": true, "a+b-c.d:x": true, "1a:x": false, ":x": false, "a/b:c": false, "ab": false} {
		if HasScheme(v) != want {
			t.Errorf("HasScheme(%q) != %v", v, want)
		}
	}
}
