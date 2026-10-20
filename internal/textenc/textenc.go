// Package textenc decodes the bytes of one file to text for display — a
// repository dump, a file read through MCP — whatever its encoding, so that
// files of different encodings can be put together as UTF-8.
//
// Each file is decided on its own:
//
//  1. an encoding named for the file (an explicit rule) is used, strictly;
//  2. a byte order mark names UTF-8 or UTF-16 (the mark is not part of the
//     text);
//  3. bytes with a NUL are binary;
//  4. ISO-2022-JP is recognised by its escape sequences;
//  5. valid UTF-8 is used as it is, never decoded again;
//  6. otherwise the legacy encodings are tried — Shift_JIS (CP932), EUC-JP,
//     GB18030, Big5, EUC-KR and Windows-1252 (which reads ISO-8859-1 text) —
//     and one is used only when it alone reads the bytes as plausible text
//     (decoding is strict: a byte sequence the encoding does not define
//     disqualifies it, nothing is replaced).
//
// The guess is not certain and may decline (Undetermined): a short file, or
// one with too little non-ASCII text, can read as more than one encoding.
// The caller then shows nothing of the file and says so; an explicit rule
// decides it. Text is never produced with characters replaced or dropped.
package textenc

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	xunicode "golang.org/x/text/encoding/unicode"
)

// Kind is what Decode found the bytes to be.
type Kind int

const (
	// Text: Result.Text is the file's text.
	Text Kind = iota
	// Binary: not text (a NUL byte, or mostly control characters).
	Binary
	// Undetermined: not UTF-8, and no encoding could be told with
	// confidence.
	Undetermined
	// Invalid: the bytes are not text in the encoding named for the file.
	Invalid
)

// Result is a decoded file.
type Result struct {
	Text     string
	Encoding string // canonical name of the encoding read ("utf-8", "shift_jis", ...)
	Kind     Kind
	Reason   string // why the file is not Text
}

// The encodings, by canonical name.
var encodings = map[string]encoding.Encoding{
	"utf-8":        nil, // handled directly
	"utf-16le":     xunicode.UTF16(xunicode.LittleEndian, xunicode.IgnoreBOM),
	"utf-16be":     xunicode.UTF16(xunicode.BigEndian, xunicode.IgnoreBOM),
	"shift_jis":    japanese.ShiftJIS, // Code Page 932 / Windows-31J
	"euc-jp":       japanese.EUCJP,
	"iso-2022-jp":  japanese.ISO2022JP,
	"gb18030":      simplifiedchinese.GB18030, // a superset of GBK and GB2312
	"big5":         traditionalchinese.Big5,
	"euc-kr":       korean.EUCKR, // Code Page 949
	"windows-1252": charmap.Windows1252,
	"iso-8859-1":   charmap.ISO8859_1,
}

var aliases = map[string]string{
	"utf8": "utf-8", "utf16le": "utf-16le", "utf16be": "utf-16be",
	"shiftjis": "shift_jis", "sjis": "shift_jis", "cp932": "shift_jis", "windows31j": "shift_jis", "ms932": "shift_jis",
	"eucjp": "euc-jp", "iso2022jp": "iso-2022-jp", "jis": "iso-2022-jp",
	"gb18030": "gb18030", "gbk": "gb18030", "gb2312": "gb18030", "cp936": "gb18030",
	"big5": "big5", "euckr": "euc-kr", "cp949": "euc-kr",
	"windows1252": "windows-1252", "cp1252": "windows-1252",
	"iso88591": "iso-8859-1", "latin1": "iso-8859-1",
}

// Lookup returns the canonical name of an encoding name (any case, with or
// without "-" or "_"), and whether it is one Decode supports.
func Lookup(name string) (string, bool) {
	key := strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(strings.TrimSpace(name)))
	c, ok := aliases[key]
	return c, ok
}

// Names lists the canonical names of the supported encodings.
func Names() []string {
	return []string{"utf-8", "utf-16le", "utf-16be", "shift_jis", "euc-jp", "iso-2022-jp", "gb18030", "big5", "euc-kr", "windows-1252", "iso-8859-1"}
}

var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// Decode decodes data. explicit is the canonical name of the encoding named
// for the file (Lookup), or "" to decide from the bytes.
func Decode(data []byte, explicit string) Result {
	if explicit != "" && !strings.HasPrefix(explicit, "utf-16") && bytes.IndexByte(data, 0) >= 0 {
		// Binary whatever the rule says (a rule like "**=…" covers files of
		// every kind); only UTF-16 holds NUL bytes as text.
		return Result{Kind: Binary, Reason: "contains a NUL byte"}
	}
	if explicit != "" {
		body := data
		switch {
		case explicit == "utf-8":
			body = bytes.TrimPrefix(data, bomUTF8)
		case explicit == "utf-16le":
			body = bytes.TrimPrefix(data, bomUTF16LE)
		case explicit == "utf-16be":
			body = bytes.TrimPrefix(data, bomUTF16BE)
		}
		t, ok := decodeAs(explicit, body)
		if !ok {
			return Result{Kind: Invalid, Encoding: explicit, Reason: "not valid " + explicit}
		}
		return text(t, explicit)
	}
	switch {
	case bytes.HasPrefix(data, bomUTF8):
		if t, ok := decodeAs("utf-8", data[len(bomUTF8):]); ok {
			return text(t, "utf-8")
		}
		return Result{Kind: Undetermined, Reason: "a UTF-8 byte order mark, but not valid UTF-8"}
	case bytes.HasPrefix(data, bomUTF16LE):
		return utf16(data[len(bomUTF16LE):], "utf-16le")
	case bytes.HasPrefix(data, bomUTF16BE):
		return utf16(data[len(bomUTF16BE):], "utf-16be")
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return Result{Kind: Binary, Reason: "contains a NUL byte"}
	}
	if utf8.Valid(data) {
		// ISO-2022-JP: escapes to a JIS character set, then pairs of bytes
		// that all are JIS characters (a strict decode), back to ASCII — and
		// at least one of them: a stray escape alone is kept as it is.
		if hasJISEscape(data) {
			if t, ok := decodeAs("iso-2022-jp", data); ok && hasNonASCII(t) {
				return text(t, "iso-2022-jp")
			}
		}
		return text(string(data), "utf-8")
	}
	if controlRatio(data) > maxControlRatio {
		return Result{Kind: Binary, Reason: "mostly control bytes"}
	}
	return guess(data)
}

func utf16(body []byte, name string) Result {
	if len(body)%2 != 0 {
		return Result{Kind: Undetermined, Reason: "a UTF-16 byte order mark, but an odd number of bytes"}
	}
	t, ok := decodeAs(name, body)
	if !ok {
		if bytes.IndexByte(body, 0) >= 0 {
			return Result{Kind: Binary, Reason: "a UTF-16 byte order mark, but no UTF-16 text"}
		}
		return Result{Kind: Undetermined, Reason: "a UTF-16 byte order mark, but not valid UTF-16"}
	}
	// Binary bytes after FF FE read as UTF-16 too: as unassigned,
	// private-use and control characters.
	odd, n := 0, 0
	for _, r := range t {
		n++
		if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			odd++
		}
	}
	if odd*20 > n {
		return Result{Kind: Binary, Reason: "a UTF-16 byte order mark, but mostly no text"}
	}
	return text(t, name)
}

// maxControlRatio is the share of control characters above which bytes are
// binary rather than text (the dump's existing rule, core.IsBinary).
const maxControlRatio = 0.1

func text(t, name string) Result {
	if strings.IndexByte(t, 0) >= 0 {
		return Result{Kind: Binary, Encoding: name, Reason: "contains a NUL character"}
	}
	if controlRatio([]byte(t)) > maxControlRatio {
		return Result{Kind: Binary, Encoding: name, Reason: "mostly control characters"}
	}
	return Result{Kind: Text, Text: t, Encoding: name}
}

// controlRatio is the share of C0 control bytes other than tab, line feed
// and carriage return — core.IsBinary's count, so that the dump keeps
// leaving out the files it always has.
func controlRatio(b []byte) float64 {
	if len(b) == 0 {
		return 0
	}
	n := 0
	for _, c := range b {
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' {
			n++
		}
	}
	return float64(n) / float64(len(b))
}

// decodeAs decodes body as the named encoding, strictly: false when the
// encoding does not define a byte sequence of it (its decoder would replace
// it with U+FFFD).
func decodeAs(name string, body []byte) (string, bool) {
	if name == "utf-8" {
		if !utf8.Valid(body) {
			return "", false
		}
		return string(body), true
	}
	enc, ok := encodings[name]
	if !ok || enc == nil {
		return "", false
	}
	out, err := enc.NewDecoder().Bytes(body)
	if err != nil || bytes.ContainsRune(out, utf8.RuneError) {
		return "", false
	}
	return string(out), true
}

func hasNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}

// hasJISEscape reports whether b contains an ISO-2022-JP escape sequence
// switching to a JIS character set (ESC $ @, ESC $ B, ESC ( J, ESC ( I).
func hasJISEscape(b []byte) bool {
	for i := bytes.IndexByte(b, 0x1B); i >= 0 && i+2 < len(b); {
		switch {
		case b[i+1] == '$' && (b[i+2] == '@' || b[i+2] == 'B'),
			b[i+1] == '(' && (b[i+2] == 'J' || b[i+2] == 'I'):
			return true
		}
		j := bytes.IndexByte(b[i+1:], 0x1B)
		if j < 0 {
			break
		}
		i += 1 + j
	}
	return false
}
