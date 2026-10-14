package asset

import "testing"

func TestExtensionsAndCategories(t *testing.T) {
	if len(Extensions()) != len(categories) {
		t.Fatalf("%d extensions, %d categories", len(Extensions()), len(categories))
	}
	for _, e := range Extensions() {
		if Category(e) == "" || Category(e) != Category(stringsToUpper(e)) {
			t.Errorf("%s: %q", e, Category(e))
		}
	}
	for _, e := range []string{".html", ".css", ".js", ".go", ".txt", ""} {
		if Category(e) != "" {
			t.Errorf("%s is an asset", e)
		}
	}
}

func stringsToUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'a' <= c && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}
