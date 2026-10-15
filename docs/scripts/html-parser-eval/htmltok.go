//go:build ignore

// htmltok prints, for each file, every start tag and attribute two HTML
// tokenizers find, with byte spans, as JSON lines:
//
//	{"p":"xnet|tdewolff","f":file,"k":"tag|attr|end","name":..,"s":start,"e":end,"vs":valStart,"ve":valEnd,"consumed":n}
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tdewolff/parse/v2"
	tdhtml "github.com/tdewolff/parse/v2/html"
	"golang.org/x/net/html"
)

type rec struct {
	P, F, K, Name string
	S, E, VS, VE  int
	Consumed      int `json:",omitempty"`
}

func out(r rec) {
	b, _ := json.Marshal(r)
	fmt.Println(string(b))
}

// xnet tokenizes with x/net/html, tracking offsets through Raw().
func xnet(f string, src []byte) {
	z := html.NewTokenizer(bytes.NewReader(src))
	off := 0
	for {
		tt := z.Next()
		raw := append([]byte(nil), z.Raw()...) // TagAttr unescapes in place
		start := off
		off += len(raw)
		if tt == html.ErrorToken {
			out(rec{P: "xnet", F: f, K: "eof", Consumed: off})
			return
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		out(rec{P: "xnet", F: f, K: "tag", Name: string(name), S: start, E: off})
		// Attribute spans: re-scan raw for each key=value in order.
		pos := 1 + len(name) // after '<' and the name
		for hasAttr {
			var k []byte
			k, _, hasAttr = z.TagAttr()
			i := indexFold(raw, pos, k)
			if i < 0 {
				out(rec{P: "xnet", F: f, K: "attr", Name: string(k), S: -1, E: -1})
				continue
			}
			s, e := start+i, start+i+len(k)
			vs, ve := -1, -1
			j := i + len(k)
			for j < len(raw) && isSpace(raw[j]) {
				j++
			}
			if j < len(raw) && raw[j] == '=' {
				j++
				for j < len(raw) && isSpace(raw[j]) {
					j++
				}
				if j < len(raw) && (raw[j] == '"' || raw[j] == '\'') {
					q := raw[j]
					end := bytes.IndexByte(raw[j+1:], q)
					if end >= 0 {
						vs, ve = start+j+1, start+j+1+end
						j = j + 2 + end
					}
				} else {
					st := j
					for j < len(raw) && !isSpace(raw[j]) && raw[j] != '>' {
						j++
					}
					vs, ve = start+st, start+j
				}
			}
			out(rec{P: "xnet", F: f, K: "attr", Name: string(k), S: s, E: e, VS: vs, VE: ve})
			pos = j
		}
	}
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

// indexFold finds key (lower case, as x/net reports it) case-insensitively
// in raw at or after pos, as a whole attribute name.
func indexFold(raw []byte, pos int, key []byte) int {
	lr := bytes.ToLower(raw)
	for i := pos; i+len(key) <= len(lr); i++ {
		if bytes.Equal(lr[i:i+len(key)], key) && (i == 0 || isSpace(lr[i-1]) || lr[i-1] == '"' || lr[i-1] == '\'' || lr[i-1] == '/') {
			e := i + len(key)
			if e == len(lr) || isSpace(lr[e]) || lr[e] == '=' || lr[e] == '>' || lr[e] == '/' {
				return i
			}
		}
	}
	return -1
}

// tdewolff tokenizes with tdewolff/parse/v2/html. Token bytes are slices of
// the input; the input's offset after a token gives its start.
func tdewolff(f string, src []byte) {
	in := parse.NewInputBytes(src)
	l := tdhtml.NewLexer(in)
	for {
		tt, data := l.Next()
		if tt == tdhtml.ErrorToken {
			out(rec{P: "tdewolff", F: f, K: "eof", Consumed: in.Offset(), Name: fmt.Sprint(l.Err())})
			return
		}
		switch tt {
		case tdhtml.StartTagToken:
			s := in.Offset() - len(data)
			out(rec{P: "tdewolff", F: f, K: "tag", Name: strings.ToLower(string(l.Text())), S: s, E: in.Offset()})
		case tdhtml.AttributeToken:
			s := in.Offset() - len(data)
			k, v := l.AttrKey(), l.AttrVal()
			// data is the whole attribute: leading space, key, = value.
			ks := s + bytes.Index(data, k)
			vs, ve := -1, -1
			if len(v) > 0 {
				i := bytes.LastIndex(data, v)
				vs, ve = s+i, s+i+len(v)
				if v[0] == '"' || v[0] == '\'' {
					vs, ve = vs+1, ve-1
				}
			}
			out(rec{P: "tdewolff", F: f, K: "attr", Name: strings.ToLower(string(k)), S: ks, E: ks + len(k), VS: vs, VE: ve})
		}
	}
}

func main() {
	for _, f := range os.Args[2:] {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		switch os.Args[1] {
		case "xnet":
			xnet(f, src)
		case "tdewolff":
			tdewolff(f, append([]byte(nil), src...))
		}
	}
}
