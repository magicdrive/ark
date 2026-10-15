// Package asset names the repository files Ark indexes as assets:
// files no language provider extracts (images, fonts, audio, video, text
// tracks) that web documents link to by path. An asset is a node of the
// graph — the target of path evidence (language.ReferenceDraft.FileIdentity)
// — never a source: its content is not read, and it declares and refers to
// nothing (docs/development/web-semantic-fidelity/resource-architecture.md).
package asset

import "strings"

// Category of an asset file.
const (
	Image = "image"
	Font  = "font"
	Media = "media"
)

var categories = map[string]string{
	".png": Image, ".jpg": Image, ".jpeg": Image, ".gif": Image, ".webp": Image,
	".avif": Image, ".svg": Image, ".ico": Image, ".bmp": Image, ".apng": Image,
	".woff": Font, ".woff2": Font, ".ttf": Font, ".otf": Font, ".eot": Font,
	".mp4": Media, ".webm": Media, ".ogg": Media, ".ogv": Media, ".oga": Media,
	".mp3": Media, ".wav": Media, ".m4a": Media, ".flac": Media, ".vtt": Media,
}

// Category returns the category of a file with extension ext (with the dot,
// any case), "" when such a file is no asset.
func Category(ext string) string {
	return categories[strings.ToLower(ext)]
}

// Extensions returns every asset extension (lower case, with the dot), in
// a fixed order.
func Extensions() []string {
	return []string{
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".svg", ".ico", ".bmp", ".apng",
		".woff", ".woff2", ".ttf", ".otf", ".eot",
		".mp4", ".webm", ".ogg", ".ogv", ".oga", ".mp3", ".wav", ".m4a", ".flac", ".vtt",
	}
}

// Language is the language of asset nodes: no provider's, a name space of
// its own.
const Language = "asset"
