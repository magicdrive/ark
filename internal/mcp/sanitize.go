package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/magicdrive/ark/internal/secrets"
)

// MCP output sanitization.
//
// Everything a tool or resource returns leaves the server through
// sanitizeToolResult / sanitizeResourceResult, which mask the secrets the
// repository-dump rules detect (secrets.MaskAll) in repository-derived text.
// Masking happens here, at the output boundary, and only here: the index,
// resolver, graph, cache and symbol identities see the source as written, so
// masking changes no analysis result.
//
// Masking is on unless the server is started with --mask-secrets off
// (masking.go), which is logged once as a warning; a request cannot turn it
// off.
//
// A tool result is either JSON or plain text:
//
//   - JSON: every string value is masked except values of identifierKeys —
//     names, paths, IDs and Ark's own enumerations, which a client passes back
//     in later calls and which masking must not alter. Keys, numbers, booleans,
//     null, array and object structure and key order are never touched. A
//     result in which nothing is masked is returned byte for byte.
//   - Text: the whole text is masked, except the text of the tools in
//     identifierTextTools, whose text is built only from paths, declaration
//     names, kinds and counts.
//
// Unknown keys and unknown tools are masked: a new field or tool is protected
// until it is reviewed. If a JSON result cannot be re-encoded, the whole text
// is masked as plain text — never returned unmasked.

// maskedValue is what secrets.MaskAll substitutes for a secret.
const maskedValue = "*****MASKED*****"

// identifierKeys are the JSON keys whose string values are identifiers (file
// paths, symbol names, symbol IDs, evidence built from them) or values of
// Ark's own enumerations. Lower camel case keys are MCP response fields;
// capitalized ones are fields of index types serialized as they are
// (search_code, get_repository_map in JSON). "receiver" is deliberately
// absent: it carries a receiver expression, which is source text.
var identifierKeys = map[string]bool{
	// identity and location
	"id": true, "symbolId": true, "name": true, "qualifiedName": true, "qualified": true,
	"symbol": true, "from": true, "to": true, "file": true, "path": true, "target": true,
	"target_file": true, "target_name": true, "container": true, "uri": true,
	"basename": true, "extension": true, "extensions": true, "affected_files": true,
	"ID": true, "Name": true, "Qualified": true, "File": true, "Path": true, "RootPath": true,
	"From": true, "To": true, "Parent": true, "ParentQualified": true, "Receiver": true,
	"Skipped": true,
	// enumerations and metadata
	"kind": true, "confidence": true, "category": true, "direction": true, "language": true,
	"matchType": true, "status": true, "reason": true, "severity": true, "code": true,
	"level": true, "type": true, "mimeType": true, "modTime": true, "evidence": true,
	"Kind": true, "Language": true,
	// echo of the client's own request
	"query": true,
}

// identifierTextTools are the tools whose text output contains only paths,
// declaration names, kinds, counts and the client's own query: list_files
// (paths), get_repository_map (package paths and declaration names) and
// search_code (path:line, kind and qualified name per match).
var identifierTextTools = map[string]bool{
	"list_files":         true,
	"get_repository_map": true,
	"search_code":        true,
}

// sanitizeToolResult masks the secrets in a tool result in place.
func sanitizeToolResult(tool string, r *CallToolResult) *CallToolResult {
	if r == nil {
		return nil
	}
	for i := range r.Content {
		r.Content[i].Text = sanitizeText(r.Content[i].Text, identifierTextTools[tool])
	}
	return r
}

// sanitizeResourceResult masks the secrets in a resource read result.
func sanitizeResourceResult(r *ReadResourceResult) *ReadResourceResult {
	if r == nil {
		return nil
	}
	for i := range r.Contents {
		r.Contents[i].Text = sanitizeText(r.Contents[i].Text, false)
	}
	return r
}

// sanitizeText masks one text: as JSON when it is a JSON object or array,
// otherwise as plain text unless identifiersOnly.
func sanitizeText(text string, identifiersOnly bool) string {
	if masked, isJSON := maskJSON(text); isJSON {
		return masked
	}
	if identifiersOnly {
		return text
	}
	return secrets.MaskAll(text)
}

// maskJSON masks the string values of a JSON object or array. isJSON is false
// when text is not one, and the text must then be treated as plain text.
func maskJSON(text string) (masked string, isJSON bool) {
	body := strings.TrimSpace(text)
	if body == "" || (body[0] != '{' && body[0] != '[') || !json.Valid([]byte(body)) {
		return "", false
	}
	compact, changed, err := rewriteJSON(body)
	if err != nil {
		return secrets.MaskAll(text), true
	}
	if !changed {
		return text, true
	}
	out := compact
	if strings.Contains(body, "\n") {
		var indented bytes.Buffer
		if json.Indent(&indented, compact, "", indentUnit(body)) == nil {
			out = indented.Bytes()
		}
	}
	return text[:strings.Index(text, body[:1])] + string(out) + text[len(strings.TrimRight(text, " \t\r\n")):], true
}

// indentUnit returns the indentation of the first indented line of an
// indented JSON text.
func indentUnit(body string) string {
	for _, line := range strings.Split(body, "\n")[1:] {
		if trimmed := strings.TrimLeft(line, " \t"); trimmed != line {
			return line[:len(line)-len(trimmed)]
		}
	}
	return "  "
}

// jsonFrame is an open object or array while a JSON text is rewritten.
type jsonFrame struct {
	object  bool
	wantKey bool   // object: the next string is a key
	key     string // object: the current member's key; array: its owner's key
	n       int    // values written
}

// rewriteJSON re-encodes a JSON text token by token, compactly, masking every
// string value whose key is not an identifier key. Array elements take the
// key of the member that holds the array.
func rewriteJSON(body string) ([]byte, bool, error) {
	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	var out bytes.Buffer
	var stack []*jsonFrame
	changed := false
	top := func() *jsonFrame {
		if len(stack) == 0 {
			return nil
		}
		return stack[len(stack)-1]
	}
	beginValue := func() {
		if f := top(); f != nil && !f.object && f.n > 0 {
			out.WriteByte(',')
		}
	}
	endValue := func() {
		if f := top(); f != nil {
			f.n++
			if f.object {
				f.wantKey = true
			}
		}
	}
	writeString := func(s string) error {
		b, err := json.Marshal(s)
		if err != nil {
			return err
		}
		out.Write(b)
		return nil
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, err
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{', '[':
				beginValue()
				owner := ""
				if f := top(); f != nil {
					owner = f.key
				}
				out.WriteByte(byte(t))
				stack = append(stack, &jsonFrame{object: t == '{', wantKey: t == '{', key: owner})
			default:
				out.WriteByte(byte(t))
				stack = stack[:len(stack)-1]
				endValue()
			}
		case string:
			if f := top(); f != nil && f.object && f.wantKey {
				if f.n > 0 {
					out.WriteByte(',')
				}
				if err := writeString(t); err != nil {
					return nil, false, err
				}
				out.WriteByte(':')
				f.key, f.wantKey = t, false
				continue
			}
			beginValue()
			value := t
			if f := top(); f == nil || !identifierKeys[f.key] {
				if m := secrets.MaskAll(t); m != t {
					value, changed = m, true
				}
			}
			if err := writeString(value); err != nil {
				return nil, false, err
			}
			endValue()
		case json.Number:
			beginValue()
			out.WriteString(t.String())
			endValue()
		case bool:
			beginValue()
			if t {
				out.WriteString("true")
			} else {
				out.WriteString("false")
			}
			endValue()
		case nil:
			beginValue()
			out.WriteString("null")
			endValue()
		}
	}
	return out.Bytes(), changed, nil
}
