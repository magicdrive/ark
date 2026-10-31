package markdown

import (
	"strings"
	"testing"
)

// TestSlug_GitHubRule pins github-slugger's slug of a heading's text content
// (the cases of its own fixtures, and GitHub's rendering of them): lower
// case, every character but letters, marks, digits, letter numbers, "_",
// "-" and " " removed, each space "-".
func TestSlug_GitHubRule(t *testing.T) {
	for in, want := range map[string]string{
		"Hello World":               "hello-world",
		"Installation":              "installation",
		"Snake_case and kebab-case": "snake_case-and-kebab-case",
		"C++ Developer":             "c-developer",
		"What's new?":               "whats-new",
		"API (v2)":                  "api-v2",
		"1. Introduction":           "1-introduction",
		"a.b.c":                     "abc",
		"a  b":                      "a--b", // spaces are not collapsed
		" leading":                  "-leading",
		"I ♥ unicode":               "i--unicode",
		"Emoji 😄 test":              "emoji--test",
		"😄 emoji":                   "-emoji",
		"🚀":                         "",
		"Привет non-latin 你好":       "привет-non-latin-你好",
		"日本語の見出し":                   "日本語の見出し",
		"全角　スペース":                   "全角スペース", // U+3000 is no space to the rule
		"Ünïcödé":                   "ünïcödé",
		"en–dash and em—dash":       "endash-and-emdash",
		"²nd ½ Ⅻ":                   "nd--ⅻ", // other numbers removed, letter numbers kept
		"tab\there":                 "tabhere",
		"line\nbreak":               "linebreak",
		"non breaking":              "nonbreaking",
		"<tag> & entity":            "tag--entity",
		"İstanbul":                  "i̇stanbul",
		"ΟΔΟΣ":                      "οδος",
		"ΣΑΣ Σ":                     "σας-σ",
		"`code` *em* ~strike~":      "code-em-strike",
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSlug_Duplicates pins github-slugger's rule for repeats: the n-th
// repeat of an anchor is "-n", skipping an anchor a heading already took.
func TestSlug_Duplicates(t *testing.T) {
	s := newSlugger()
	var got []string
	for _, h := range []string{"foo", "foo", "foo 1", "foo-1", "foo", "Foo", "", ""} {
		got = append(got, s.next(h))
	}
	want := "foo foo-1 foo-1-1 foo-1-2 foo-2 foo-3  -1"
	if strings.Join(got, " ") != want {
		t.Errorf("anchors = %q, want %q", strings.Join(got, " "), want)
	}
	// A heading took "a-1" first: the second "a" skips it.
	s = newSlugger()
	got = nil
	for _, h := range []string{"a-1", "a", "a", "a"} {
		got = append(got, s.next(h))
	}
	if strings.Join(got, " ") != "a-1 a a-2 a-3" {
		t.Errorf("anchors = %q, want %q", strings.Join(got, " "), "a-1 a a-2 a-3")
	}
}
