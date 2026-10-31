package xml

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/languages/internal/declname"
	"github.com/magicdrive/ark/internal/source"
)

// FuzzExtractXML: on any input, as a POM and as another XML file,
// extraction never panics or fails, every range lies within the source and
// is ordered, a symbol's location is its bytes', every name and identity
// is a one-line name (but for the file's own path in it), every reference's container is a declaration of the
// file, and a second extraction is the same.
func FuzzExtractXML(f *testing.F) {
	for _, s := range []string{
		`<project xmlns="http://maven.apache.org/POM/4.0.0"><parent><groupId>g</groupId><artifactId>p</artifactId></parent><artifactId>a</artifactId><modules><module>m</module></modules><dependencies><dependency><groupId>${x}</groupId><artifactId>d</artifactId></dependency></dependencies></project>`,
		`<r xmlns:p="u" xmlns="d"><p:a p:x="1" id="i"/><a xml:id="j"/><l href="#i"/><xi:include xmlns:xi="http://www.w3.org/2001/XInclude" href="../a.xml"/></r>`,
		"<!DOCTYPE a [<!ENTITY e \"&e;&e;\"><!ENTITY x SYSTEM \"file:///etc/passwd\">]><a b='&e;'>&x;</a>",
		"<?xml version='1.0'?>\r\n<a>\r\n<![CDATA[x]]><!--c--><?pi d?></a>",
		"<a><b></a></c><d",
		"\xef\xbb\xbf<résumé é='é'/>",
		"<a x='1' x='2'/><b/>text",
	} {
		f.Add([]byte(s))
	}
	p := NewProvider()
	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 1<<16 {
			return
		}
		for _, file := range []string{"m/pom.xml", "d/data.xml"} {
			ex, err := p.Extract(context.Background(), source.FileID(file), src)
			if err != nil {
				t.Fatal(err)
			}
			ls := newLines(src)
			qualified := map[string]bool{}
			for _, s := range ex.Symbols {
				qualified[s.Qualified] = true
				if s.StartByte > s.EndByte || int(s.EndByte) > len(src) {
					t.Fatalf("%s: symbol %s at [%d,%d) of %d bytes", file, s.Qualified, s.StartByte, s.EndByte, len(src))
				}
				if s.Location.Range.Start != ls.pos(int(s.StartByte)) || s.Location.Range.End != ls.pos(int(s.EndByte)) {
					t.Fatalf("%s: symbol %s location %v is not its bytes'", file, s.Qualified, s.Location.Range)
				}
				// The file's path, which the index chose, is exempt within an
				// identity; the rest is the file's text.
				rest := strings.TrimPrefix(s.Qualified, file)
				if !declname.IsName(s.Name) && s.Kind != "document" || rest != "" && !declname.IsName(rest) || s.Signature != "" && !declname.IsName(s.Signature) {
					t.Fatalf("%s: no name %q %q %q", file, s.Name, s.Qualified, s.Signature)
				}
			}
			for _, r := range ex.References {
				rg := r.Location.Range
				if rg.Start.Line > rg.End.Line || rg.Start.Line == rg.End.Line && rg.Start.Column > rg.End.Column {
					t.Fatalf("%s: reference %s range %v", file, r.Name, rg)
				}
				if !qualified[r.Container] {
					t.Fatalf("%s: reference %s in %q, no declaration of the file", file, r.Name, r.Container)
				}
				if rest := strings.TrimPrefix(r.Name, file); rest != "" && !declname.IsName(rest) {
					t.Fatalf("%s: reference name %q", file, r.Name)
				}
			}
			for _, d := range ex.Diagnostics {
				if d.Code == "" {
					t.Fatalf("%s: diagnostic without a code: %s", file, d.Message)
				}
			}
			again, _ := p.Extract(context.Background(), source.FileID(file), src)
			if !reflect.DeepEqual(ex, again) {
				t.Fatalf("%s: two extractions differ", file)
			}
		}
	})
}
