package markdown

import (
	"net/url"
	"path"
	"strings"

	"github.com/magicdrive/ark/internal/asset"
	"github.com/magicdrive/ark/internal/languages/internal/declname"
	"github.com/magicdrive/ark/internal/languages/internal/weburl"
	"github.com/magicdrive/ark/internal/symbol"
)

// What a link destination names, read from its text alone as GitHub reads
// a link of a Markdown file in a repository:
//
//   - "#x": the anchor x of the document itself;
//   - a relative path ("b.md", "../c/d.png", "e.md#x"): the repository
//     file at that path from the file's directory — percent-escapes
//     decoded, the query ignored — and, after "#", its anchor;
//   - a path from the root ("/docs/a.md"): GitHub reads it from the
//     repository's root, which Ark takes to be the index's root;
//   - a URL with an authority ("https://host/p", "//host/p"): a resource
//     outside the repository, named by its scheme, host and path only;
//   - anything else (mailto:, javascript:, another scheme; a path that
//     leaves the repository; an escaped slash, a backslash, an invalid
//     escape) names nothing Ark represents.
type destKind int

const (
	destNone     destKind = iota
	destAnchor            // fragment of this document
	destFile              // a repository file (frag: its anchor, "" for none)
	destOutside           // a path that leaves the repository root
	destExternal          // a URL outside the repository
)

type dest struct {
	kind     destKind
	path     string // destFile: the file's path from the root, slash-separated; destOutside: the path as written
	frag     string // destAnchor, destFile: the anchor (percent-escapes decoded)
	fromRoot bool   // destFile: written from the root ("/a.md")
	url      string // destExternal: scheme://host/path
}

// readDest reads the destination v of a link written in file (its path
// from the root, slash-separated).
func readDest(file, v string) dest {
	v = weburl.Trim(v)
	if v == "" {
		return dest{}
	}
	if strings.HasPrefix(v, "#") {
		f, ok := fragment(v[1:])
		if !ok {
			return dest{}
		}
		return dest{kind: destAnchor, frag: f}
	}
	if strings.HasPrefix(v, "//") || weburl.HasScheme(v) {
		return external(v)
	}
	p, frag := v, ""
	if i := strings.IndexByte(p, '#'); i >= 0 {
		p, frag = p[:i], p[i+1:]
	}
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	if p == "" {
		return dest{} // "?q" alone: this document, which no anchor names
	}
	if strings.ContainsAny(p, "\\\x00") {
		return dest{}
	}
	// An escaped slash is no path separator to GitHub's URL parser; which
	// file it names is not fixed.
	if l := strings.ToLower(p); strings.Contains(l, "%2f") || strings.Contains(l, "%5c") {
		return dest{}
	}
	dec, err := url.PathUnescape(p)
	if err != nil || strings.ContainsAny(dec, "\\\x00") {
		return dest{}
	}
	f := ""
	if frag != "" {
		var ok bool
		if f, ok = fragment(frag); !ok {
			f = "" // a fragment no anchor can be (empty, invalid): the file only
		}
	}
	fromRoot := strings.HasPrefix(dec, "/")
	var target string
	if fromRoot {
		// "/../x" leaves the root as "../x" does: path.Clean would keep it
		// at the root.
		target = path.Join(".", strings.TrimLeft(dec, "/"))
	} else {
		target = path.Join(path.Dir(file), dec)
	}
	switch {
	case target == "" || target == ".":
		return dest{} // the root directory itself: no file
	case target == ".." || strings.HasPrefix(target, "../"):
		return dest{kind: destOutside, path: dec}
	}
	return dest{kind: destFile, path: target, frag: f, fromRoot: fromRoot}
}

// fragment decodes a URL fragment as the anchor it names: percent-escapes
// decoded (GitHub links a non-ASCII anchor by its escapes). An empty
// fragment, an invalid escape or a text fragment (":~:") names none.
func fragment(f string) (string, bool) {
	if f == "" || strings.Contains(f, ":~:") {
		return "", false
	}
	d, err := url.PathUnescape(f)
	if err != nil || d == "" {
		return "", false
	}
	return d, true
}

// external names a URL outside the repository by its scheme, host and path
// alone: credentials, the query and the fragment — where a token may be —
// are no part of the name. A URL without an authority (mailto:, tel:,
// javascript:, data:) names nothing.
func external(v string) dest {
	if strings.HasPrefix(v, "//") {
		v = "https:" + v
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return dest{}
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)
	if scheme == "" || strings.ContainsAny(host, "@ ") {
		return dest{}
	}
	name := scheme + "://" + host + u.EscapedPath()
	if !declname.IsName(name) {
		name = scheme + "://" + host
	}
	if !declname.IsName(name) {
		return dest{}
	}
	return dest{kind: destExternal, url: name}
}

// targetKinds are the kinds of file declarations a link to path may reach:
// an asset's node for an asset file (the index makes it only for a link
// that names it as one), any file's own declaration otherwise.
func targetKinds(p string) string {
	if asset.Category(path.Ext(p)) != "" {
		return string(symbol.KindAsset)
	}
	return ""
}
