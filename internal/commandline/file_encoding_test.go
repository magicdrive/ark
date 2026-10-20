package commandline

import (
	"path/filepath"
	"testing"
)

func TestParseFileEncodings(t *testing.T) {
	rules, err := parseFileEncodings(" ./legacy/** = CP932 , *.euc=eucjp,docs//zh.txt/=GBK ")
	if err != nil {
		t.Fatal(err)
	}
	want := []FileEncodingRule{{"legacy/**", "shift_jis"}, {"*.euc", "euc-jp"}, {"docs/zh.txt", "gb18030"}}
	if len(rules) != len(want) {
		t.Fatalf("rules %+v, want %+v", rules, want)
	}
	for i := range want {
		if rules[i] != want[i] {
			t.Errorf("rule %d: %+v, want %+v", i, rules[i], want[i])
		}
	}
	if rules, err := parseFileEncodings(""); err != nil || len(rules) != 0 {
		t.Errorf("empty: %+v, %v", rules, err)
	}
	for _, bad := range []string{"legacy", "=sjis", "a.txt=ebcdic", "[=sjis", "/abs/a.txt=sjis", "../x.txt=sjis", "a/../../b=sjis", ".=sjis"} {
		if _, err := parseFileEncodings(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// A rule names the encoding of the files its pattern matches, relative to
// the target directory (or the server root); the first match wins.
func TestFileEncodingFor(t *testing.T) {
	root := filepath.FromSlash("/repo")
	rules, err := parseFileEncodings("legacy/**=cp932,*.euc=euc-jp,docs/zh.txt=gb18030,*.txt=latin1")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []*Option{
		{TargetDirname: root, FileEncodingRules: rules},
		{FileEncodingRoot: root, FileEncodingRules: rules},
	} {
		for p, want := range map[string]string{
			"/repo/legacy/a.txt":     "shift_jis",
			"/repo/legacy/x/y.c":     "shift_jis",
			"/repo/sub/m.euc":        "euc-jp",
			"/repo/docs/zh.txt":      "gb18030",
			"/repo/docs/other.txt":   "iso-8859-1",
			"/repo/README.md":        "",
			"/repo/legacyfile.txt":   "iso-8859-1",
			"/elsewhere/legacy/a.go": "",
		} {
			if got := o.FileEncodingFor(filepath.FromSlash(p)); got != want {
				t.Errorf("%s: %q, want %q", p, got, want)
			}
		}
	}
	rel := &Option{TargetDirname: ".", FileEncodingRules: rules}
	if got := rel.FileEncodingFor(filepath.FromSlash("legacy/a.txt")); got != "shift_jis" {
		t.Errorf("relative walk path: %q", got)
	}
	if got := (&Option{}).FileEncodingFor("/repo/a.txt"); got != "" {
		t.Errorf("no rules: %q", got)
	}
}

// The rules that match none of the files are reported: a pattern naming
// nothing is a mistake.
func TestUnusedFileEncodingRules(t *testing.T) {
	rules, err := parseFileEncodings("legacy/**=cp932,*.euc=euc-jp,missing.txt=big5")
	if err != nil {
		t.Fatal(err)
	}
	o := &Option{TargetDirname: filepath.FromSlash("/repo"), FileEncodingRules: rules}
	unused := o.UnusedFileEncodingRules([]string{filepath.FromSlash("/repo/legacy/a.txt"), filepath.FromSlash("/repo/b.euc")})
	if len(unused) != 1 || unused[0].Pattern != "missing.txt" {
		t.Errorf("unused %+v, want missing.txt", unused)
	}
}
