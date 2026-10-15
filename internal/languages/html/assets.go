package html

import (
	"path"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/magicdrive/ark/internal/asset"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/internal/weburl"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// Links to asset files (internal/asset: images, fonts, media), by relative
// URL, in the attributes whose URL the browser fetches as such a file:
//
//	img src, img srcset; source src (in video, audio); source srcset (in
//	picture); video poster, video src; audio src; track src (in video,
//	audio); input type=image src; link href with rel icon / apple-touch-icon,
//	or rel preload as image, font, audio, video or track; SVG image and use
//	href / xlink:href (the file part of the URL)
//
// Each URL is path evidence (FileIdentity) to the asset node of the file it
// names, Strong at most (which file a URL serves depends on the server); a
// file that is no asset, or missing, leaves the reference unresolved.

// assetAttr reports whether the attribute a of t names assets, and whether
// its value is a srcset (a list of image candidates) rather than one URL.
func (e *extractor) assetAttr(t *tag, a attr) (ok, srcset bool) {
	parent := ""
	if t.el != nil && t.el.Parent != nil && t.el.Parent.Type == xhtml.ElementNode && t.el.Parent.Namespace == "" {
		parent = t.el.Parent.Data
	}
	switch t.namespace {
	case "":
		switch {
		case t.name == "img" && a.key == "src":
			return true, false
		case t.name == "img" && a.key == "srcset":
			return true, true
		case t.name == "source" && a.key == "src":
			return parent == "video" || parent == "audio", false
		case t.name == "source" && a.key == "srcset":
			return parent == "picture", true
		case t.name == "video" && (a.key == "poster" || a.key == "src"):
			return true, false
		case t.name == "audio" && a.key == "src":
			return true, false
		case t.name == "track" && a.key == "src":
			return parent == "video" || parent == "audio", false
		case t.name == "input" && a.key == "src":
			return inputType(t) == "image", false
		case t.name == "link" && a.key == "href":
			return e.assetLink(t), false
		}
	case "svg":
		if (t.name == "image" || t.name == "use") && (a.key == "href" || a.key == "xlink:href") {
			// "#id" names an element of the document (fragmentOnly).
			return !strings.HasPrefix(weburl.Trim(a.val), "#"), false
		}
	}
	return false, false
}

// assetLink reports whether a <link> fetches an asset: rel icon or
// apple-touch-icon, or rel preload as image, font, audio, video or track.
func (e *extractor) assetLink(t *tag) bool {
	rel, ok := e.attrState(t, "rel")
	if !ok {
		return false
	}
	r := tokens(rel)
	if r["icon"] || r["apple-touch-icon"] {
		return true
	}
	if !r["preload"] {
		return false
	}
	as, known := e.attrState(t, "as")
	if !known {
		return false
	}
	switch asciiLower(strings.Trim(as, asciiWhitespace)) {
	case "image", "font", "audio", "video", "track":
		return true
	}
	return false
}

// assets states the asset links of the attribute a of t.
func (e *extractor) assets(t *tag, a attr, srcset bool) {
	if e.base {
		return
	}
	urls := []string{a.val}
	if srcset {
		urls = srcsetURLs(a.val)
	}
	for _, u := range urls {
		target, _, ok := weburl.Relative(string(e.file), weburl.Trim(u), asset.Extensions()...)
		if !ok {
			continue
		}
		s := e.valueSpan(a, u)
		name := path.Base(target)
		if len(urls) > 1 && s == a.valS {
			// The candidate's own span is not known (character references,
			// a repeated text): the whole value stands for each, and the
			// target's path tells their references apart.
			name = target
		}
		e.addRef(language.ReferenceDraft{
			Name:                 name,
			Kind:                 string(reference.KindResourceLink),
			Location:             e.loc(s.start, s.end),
			NameQualified:        target,
			FileIdentity:         true,
			IdentityInRepository: true,
			ConfidenceCap:        "strong",
			TargetKinds:          string(symbol.KindAsset),
		})
	}
}

// srcsetURLs returns the URLs of a srcset attribute's image candidates
// (HTML "parse a srcset attribute": a candidate is a run of non-whitespace
// characters, trailing commas removed, then descriptors up to a comma
// outside parentheses).
func srcsetURLs(v string) []string {
	isSpace := func(c byte) bool { return strings.IndexByte(asciiWhitespace, c) >= 0 }
	var out []string
	i := 0
	for i < len(v) {
		for i < len(v) && (isSpace(v[i]) || v[i] == ',') {
			i++
		}
		if i >= len(v) {
			break
		}
		start := i
		for i < len(v) && !isSpace(v[i]) {
			i++
		}
		u := v[start:i]
		if strings.HasSuffix(u, ",") {
			out = append(out, strings.TrimRight(u, ","))
			continue
		}
		out = append(out, u)
		// Descriptors: up to a comma outside parentheses.
		depth := 0
		for i < len(v) {
			c := v[i]
			if c == '(' {
				depth++
			} else if c == ')' && depth > 0 {
				depth--
			} else if c == ',' && depth == 0 {
				i++
				break
			}
			i++
		}
	}
	return out
}
