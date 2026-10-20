package mcp

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
	"github.com/magicdrive/ark/internal/textenc"
)

// FuzzSearchableText checks the text search_in_files searches in a file
// against the file's bytes: it is valid UTF-8, the same every time, keeps
// the file's line feeds (a line number is the file's own), never holds a
// replacement character the file does not, is never taken from a file
// --skip-non-utf8 leaves out, and for UTF-8 without a byte order mark or an
// escape is the file's bytes, searched exactly when they were before
// (core.IsBinary). Decode's own properties are FuzzDecode's.
func FuzzSearchableText(f *testing.F) {
	for _, s := range []string{"", "a\nb\r\nc", "\xEF\xBB\xBFbom\n", "\xFF\xFEa\x00\n\x00", "\xFE\xFF\x00a\x00\n",
		"\x90\xdd\x92\xe8\n\x83t\x83@\x83C\x83\x8b\n", "\x1b$B$3$s\x1b(B\n", "bin\x00ary", "\x01\x02\x03\x04ctl", "caf\xe9\n"} {
		f.Add([]byte(s), uint8(0), false)
	}
	f.Add([]byte("\x90\xdd\x92\xe8\n"), uint8(4), false)
	f.Add([]byte("a\x00\n\x00"), uint8(2), true)
	names := textenc.Names()
	f.Fuzz(func(t *testing.T, data []byte, enc uint8, skip bool) {
		opt := &commandline.Option{SkipNonUTF8Flag: skip}
		explicit := ""
		if i := int(enc) % (len(names) + 1); i > 0 {
			explicit = names[i-1]
			opt.FileEncodingRules = []commandline.FileEncodingRule{{Pattern: "*", Encoding: explicit}}
		}
		text, ok := searchableText(data, "f.txt", opt)
		// UTF-8 (a byte order mark or not) without an escape is searched
		// exactly when it was before, named UTF-8 or not.
		if (explicit == "" || explicit == "utf-8") && utf8.Valid(data) && bytes.IndexByte(data, 0x1B) < 0 && ok != !core.IsBinary(data) {
			t.Fatalf("UTF-8 searched %v, IsBinary %v", ok, core.IsBinary(data))
		}
		if again, ok2 := searchableText(data, "f.txt", opt); again != text || ok2 != ok {
			t.Fatal("not deterministic")
		}
		if !ok {
			if text != "" {
				t.Fatalf("text %q for a file not searched", text)
			}
			return
		}
		if !utf8.ValidString(text) {
			t.Fatalf("not valid UTF-8: %q", text)
		}
		if skip && core.IsBinary(data) {
			t.Fatal("searched a file --skip-non-utf8 leaves out")
		}
		if strings.ContainsRune(text, utf8.RuneError) && !bytes.Contains(data, []byte("�")) {
			t.Fatalf("a replacement character the file does not hold: %q", text)
		}
		r := textenc.Decode(data, explicit)
		feeds := bytes.Count(data, []byte{'\n'})
		switch r.Encoding {
		case "utf-16le":
			feeds = bytes.Count(utf16Units(data, 0), []byte{1})
		case "utf-16be":
			feeds = bytes.Count(utf16Units(data, 1), []byte{1})
		}
		if got := strings.Count(text, "\n"); got != feeds {
			t.Fatalf("%s: %d line feeds, the file has %d", r.Encoding, got, feeds)
		}
		if explicit == "" && utf8.Valid(data) && !bytes.HasPrefix(data, []byte("\xEF\xBB\xBF")) && bytes.IndexByte(data, 0x1B) < 0 && text != string(data) {
			t.Fatalf("UTF-8 changed: %q", text)
		}
	})
}

// utf16Units marks with 1 each UTF-16 code unit of data that is a line feed
// (lo is the index of the low byte in a unit: 0 little-endian, 1 big-endian).
func utf16Units(data []byte, lo int) []byte {
	var out []byte
	for i := 0; i+1 < len(data); i += 2 {
		if data[i+lo] == '\n' && data[i+1-lo] == 0 {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}
	}
	return out
}

// For UTF-8 the search reads exactly the files it read before — those
// core.IsBinary does not call binary — as their bytes, less a byte order
// mark.
func TestSearchableText_UTF8AsBefore(t *testing.T) {
	opt := &commandline.Option{}
	for _, s := range []string{"", "a", "\xEF\xBB\xBFabcdef\x01\n", "\xEF\xBB\xBF\x01\x02", "日本語\r\n🦊\n", "\t\n\r", "ctl\x01\x02\x03\x04\x05\x06\x07\x08", "nul\x00", "\xC3", "\xE6\x97", "�"} {
		text, ok := searchableText([]byte(s), "f.txt", opt)
		if ok != !core.IsBinary([]byte(s)) || ok && text != strings.TrimPrefix(s, "\uFEFF") {
			t.Errorf("%q: %q %v, IsBinary %v", s, text, ok, core.IsBinary([]byte(s)))
		}
		named := &commandline.Option{FileEncodingRules: []commandline.FileEncodingRule{{Pattern: "*", Encoding: "utf-8"}}}
		if text2, ok2 := searchableText([]byte(s), "f.txt", named); text2 != text || ok2 != ok {
			t.Errorf("%q named UTF-8: %q %v, not as decided from the bytes", s, text2, ok2)
		}
	}
}
