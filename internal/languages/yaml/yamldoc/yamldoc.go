// Package yamldoc reads a YAML stream into its documents: the node trees
// go.yaml.in/yaml/v3 builds, with the byte span of every node and the problems
// of the stream. It knows YAML only; what a document means (a GitHub Actions
// workflow) is another package's (internal/languages/yaml/actions).
//
// The YAML is read by go.yaml.in/yaml/v3, never by a reader of Ark's own:
// this package splits the stream into documents, maps positions and finds
// where a node ends, which the library does not report.
//
//   - Documents. The stream is split at its document markers: a line that
//     starts with "---" or "..." followed by a blank or the end of the line,
//     which YAML forbids anywhere but as a marker (c-forbidden), and the
//     directives before a "---". Each part is read on its own, so an error
//     in one document loses that document only.
//   - Positions. The library reports a node's line and column, counting
//     characters and the line breaks of YAML 1.1 (LF, CR, CRLF, NEL, LS, PS),
//     after a UTF-8 byte order mark. Start converts them to byte offsets of
//     the source as stored; Ark's positions count bytes and LF lines.
//   - Ends. A collection in flow style ends at its closing bracket, one in
//     block style with its last entry, a quoted scalar at its closing quote, a
//     single-line plain scalar with its text and an alias with its name. A
//     block scalar or a multi-line plain scalar ends where the next node
//     begins, less the blank lines, comments and indicators ("-", "?", ":")
//     before it that its indentation leaves out of it.
//   - Problems. A syntax error (yaml_syntax), a UTF-16 or UTF-32 stream
//     (yaml_encoding: positions would not be the file's bytes), a key a
//     mapping states twice (yaml_duplicate_key), a stream larger than the
//     reader takes (yaml_too_large).
//
// Aliases are never expanded: an alias node is a leaf that names its anchor
// (yaml.Node.Alias). Deref follows a chain of aliases a bounded number of
// steps, so a cycle or an alias bomb costs nothing. The library bounds the
// nesting depth of a document (10000).
//
// A YAML 1.2 directive (%YAML 1.2), which the library rejects, is read as
// blanks: the library's reading of a document's structure is YAML 1.2's for
// what this package and its users take from it (keys, sequences, scalar
// text and styles); only the resolution of plain scalars to booleans and
// numbers differs, which no user reads.
package yamldoc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	yaml "go.yaml.in/yaml/v3"
)

// MaxBytes is the largest stream Parse reads. A larger one is reported
// (yaml_too_large) and not read: the node trees of a stream of many small
// scalars take a hundred times its size.
const MaxBytes = 4 << 20

// maxAliasChain bounds the aliases Deref follows.
const maxAliasChain = 16

// Problem codes.
const (
	CodeSyntax       = "yaml_syntax"
	CodeEncoding     = "yaml_encoding"
	CodeDuplicateKey = "yaml_duplicate_key"
	CodeTooLarge     = "yaml_too_large"
)

// Problem is something wrong with the stream, at [Start, End) of its bytes.
type Problem struct {
	Code    string
	Message string
	Start   int
	End     int
	// Error is set for a problem that loses content (a syntax error, an
	// encoding not read); a duplicate key or a size beyond the limit loses
	// nothing YAML defines.
	Error bool
}

// Document is one document of the stream.
type Document struct {
	// Index is the document's position in the stream, from 0 (a document
	// that could not be read keeps its index).
	Index int
	// Root is the document's content node; nil for an empty document.
	Root *yaml.Node
	// Start, End are the document's bytes, its markers included.
	Start, End int
}

// Stream is a read YAML stream.
type Stream struct {
	Src      []byte
	Docs     []Document
	Problems []Problem

	// ProblemsTruncated reports that the stream has more problems than
	// maxProblems, which Problems holds.
	ProblemsTruncated bool

	breaks []int // byte offsets where the library's lines start
	ends   map[*yaml.Node]int
	// Per line of breaks: whether it is ASCII only (1) or not (2), 0 not
	// known yet; for a line that is not, the offset of each of its
	// characters from the line's start. Start converts a column in one
	// step, not by decoding the line up to it for every node.
	ascii []int8
	runes map[int][]int32
}

// maxProblems bounds the problems a stream records (a file of a million
// duplicate keys would otherwise hold a million).
const maxProblems = 1000

func (s *Stream) problem(p Problem) {
	if len(s.Problems) >= maxProblems {
		s.ProblemsTruncated = true
		return
	}
	s.Problems = append(s.Problems, p)
}

// Span is [Start, End) of the source's bytes.
type Span struct{ Start, End int }

var docMarker = regexp.MustCompile(`(?m)^(?:---|\.\.\.)(?:[ \t\r]|$)`)

var yaml12 = regexp.MustCompile(`(?m)^%YAML[ \t]+1\.[2-9][0-9]*[ \t]*(?:#[^\r\n]*)?\r?$`)

// Parse reads src. It never fails: what it cannot read is a Problem.
func Parse(src []byte) *Stream {
	s := &Stream{Src: src, ends: map[*yaml.Node]int{}}
	if len(src) > MaxBytes {
		s.Problems = append(s.Problems, Problem{Code: CodeTooLarge,
			Message: fmt.Sprintf("the file is larger than %d bytes: its structure is not read", MaxBytes)})
		return s
	}
	for _, bom := range [][]byte{{0, 0, 0xFE, 0xFF}, {0xFF, 0xFE, 0, 0}, {0xFE, 0xFF}, {0xFF, 0xFE}} {
		if bytes.HasPrefix(src, bom) {
			s.Problems = append(s.Problems, Problem{Code: CodeEncoding, Error: true, End: len(bom),
				Message: "a UTF-16 or UTF-32 byte order mark: the file is not read as YAML"})
			return s
		}
	}
	s.breaks = lineStarts(src)
	read := src
	if yaml12.Match(src) {
		read = yaml12.ReplaceAllFunc(bytes.Clone(src), func(b []byte) []byte {
			out := bytes.Repeat([]byte(" "), len(b))
			if b[len(b)-1] == '\r' {
				out[len(out)-1] = '\r' // the line break stays
			}
			return out
		})
	}
	for _, part := range parts(read) {
		s.readPart(read, part)
	}
	for i := range s.Docs {
		s.measure(&s.Docs[i])
	}
	return s
}

// parts splits src into the spans its document markers start: each "---"
// line starts one, with the directive lines ("%...") before it (comment and
// blank lines may stand between them); text before the first marker is a
// part of its own.
func parts(src []byte) []Span {
	var cuts []int
	for _, m := range docMarker.FindAllIndex(src, -1) {
		if src[m[0]] != '-' {
			continue // "..." ends a document; the part goes on to the next "---"
		}
		at := m[0]
		for cursor := at; cursor > 0; {
			prev := prevLineStart(src, cursor)
			line := bytes.TrimRight(src[prev:cursor], "\r\n")
			trimmed := bytes.TrimLeft(line, " \t")
			switch {
			case len(line) > 0 && line[0] == '%':
				at = prev
			case len(trimmed) == 0 || trimmed[0] == '#':
			default:
				prev = 0
			}
			if prev == 0 {
				break
			}
			cursor = prev
		}
		if at > 0 && (len(cuts) == 0 || cuts[len(cuts)-1] < at) {
			cuts = append(cuts, at)
		}
	}
	out := make([]Span, 0, len(cuts)+1)
	start := 0
	for _, c := range cuts {
		out = append(out, Span{start, c})
		start = c
	}
	return append(out, Span{start, len(src)})
}

// lineStartBefore returns the start of the line that holds src[i].
func lineStartBefore(src []byte, i int) int {
	for i > 0 && src[i-1] != '\n' && src[i-1] != '\r' {
		i--
	}
	return i
}

// prevLineStart returns the start of the line before the one starting at
// start (> 0), whose line break — LF, CR or CRLF — ends just before start.
func prevLineStart(src []byte, start int) int {
	j := start - 1
	if src[j] == '\n' && j > 0 && src[j-1] == '\r' {
		j--
	}
	return lineStartBefore(src, j)
}

// readPart decodes the documents of one part of the stream.
func (s *Stream) readPart(src []byte, p Span) {
	baseLine := s.lineOf(p.Start) // 0-based
	dec := yaml.NewDecoder(bytes.NewReader(src[p.Start:p.End]))
	var docs []Document
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("yaml: %v", r)
			}
		}()
		for {
			var n yaml.Node
			if err := dec.Decode(&n); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
			shift(&n, baseLine, map[*yaml.Node]bool{})
			d := Document{Index: len(s.Docs) + len(docs), Start: s.Start(&n)}
			if len(n.Content) == 1 {
				d.Root = n.Content[0]
			}
			if d.Start < p.Start || d.Start > p.End {
				d.Start = p.Start
			}
			docs = append(docs, d)
		}
	}()
	if len(docs) > 0 {
		docs[0].Start = p.Start
	}
	for i := range docs {
		docs[i].End = p.End
		if i+1 < len(docs) {
			docs[i].End = docs[i+1].Start
		}
	}
	s.Docs = append(s.Docs, docs...)
	if err != nil {
		s.problem(s.syntaxProblem(err, p, baseLine))
		if len(docs) == 0 {
			// Keep the document's place in the stream: a later document's
			// index does not depend on whether this one could be read.
			s.Docs = append(s.Docs, Document{Index: len(s.Docs), Start: p.Start, End: p.End})
		}
	}
}

// shift makes the positions of a part's nodes the stream's.
func shift(n *yaml.Node, lines int, seen map[*yaml.Node]bool) {
	if n == nil || seen[n] {
		return
	}
	seen[n] = true
	n.Line += lines
	for _, c := range n.Content {
		shift(c, lines, seen)
	}
}

var errLine = regexp.MustCompile(`^yaml: line ([0-9]+): `)

// syntaxProblem locates a decoding error: on the line the library names
// (counted from the part's start, so the message leaves it to the location),
// else over the whole part.
func (s *Stream) syntaxProblem(err error, p Span, baseLine int) Problem {
	msg := err.Error()
	pr := Problem{Code: CodeSyntax, Error: true, Message: msg, Start: p.Start, End: p.End}
	if m := errLine.FindStringSubmatch(msg); m != nil {
		pr.Message = "yaml: " + msg[len(m[0]):]
		if n, e := strconv.Atoi(m[1]); e == nil && n >= 1 {
			line := baseLine + n - 1
			if line < len(s.breaks) {
				pr.Start = s.breaks[line]
				pr.End = len(s.Src)
				if line+1 < len(s.breaks) {
					pr.End = s.breaks[line+1]
				}
			}
		}
	}
	if pr.Start > pr.End {
		pr.Start = pr.End
	}
	return pr
}

// lineStarts returns the offsets where the library's lines start: after a
// UTF-8 byte order mark, and after each of LF, CR, CRLF, NEL, LS and PS.
func lineStarts(src []byte) []int {
	start := 0
	if bytes.HasPrefix(src, []byte("\xEF\xBB\xBF")) {
		start = 3
	}
	out := []int{start}
	for i := start; i < len(src); {
		switch c := src[i]; {
		case c == '\r' && i+1 < len(src) && src[i+1] == '\n':
			i += 2
			out = append(out, i)
		case c == '\r' || c == '\n':
			i++
			out = append(out, i)
		case c == 0xC2 && i+1 < len(src) && src[i+1] == 0x85,
			c == 0xE2 && i+2 < len(src) && src[i+1] == 0x80 && (src[i+2] == 0xA8 || src[i+2] == 0xA9):
			if c == 0xC2 {
				i += 2
			} else {
				i += 3
			}
			out = append(out, i)
		default:
			i++
		}
	}
	return out
}

// lineOf returns the 0-based line of the library's lines that holds off.
func (s *Stream) lineOf(off int) int {
	return max(sort.Search(len(s.breaks), func(i int) bool { return s.breaks[i] > off })-1, 0)
}

// Start returns the offset of a node's first byte.
func (s *Stream) Start(n *yaml.Node) int {
	if n == nil || n.Line < 1 || len(s.breaks) == 0 {
		return 0
	}
	line := n.Line - 1
	if line >= len(s.breaks) {
		return len(s.Src)
	}
	off := s.breaks[line]
	limit := len(s.Src)
	if line+1 < len(s.breaks) {
		limit = s.breaks[line+1]
	}
	if n.Column <= 1 {
		return off
	}
	if s.ascii == nil {
		s.ascii = make([]int8, len(s.breaks))
		s.runes = map[int][]int32{}
	}
	if s.ascii[line] == 0 {
		s.ascii[line] = 1
		for _, c := range s.Src[off:limit] {
			if c >= utf8.RuneSelf {
				s.ascii[line] = 2
				break
			}
		}
	}
	if s.ascii[line] == 1 {
		return min(off+n.Column-1, limit)
	}
	offs, ok := s.runes[line]
	if !ok {
		for i := off; i < limit; {
			offs = append(offs, int32(i-off))
			_, size := utf8.DecodeRune(s.Src[i:limit])
			i += size
		}
		s.runes[line] = offs
	}
	if n.Column-1 < len(offs) {
		return off + int(offs[n.Column-1])
	}
	return limit
}

// lineStartOf returns the start of the library's line that holds off.
func (s *Stream) lineStartOf(off int) int {
	if len(s.breaks) == 0 {
		return 0
	}
	return min(s.breaks[s.lineOf(off)], off)
}

// End returns the offset just past a node's last byte.
func (s *Stream) End(n *yaml.Node) int {
	if e, ok := s.ends[n]; ok {
		return e
	}
	return s.Start(n)
}

// NodeSpan returns a node's bytes.
func (s *Stream) NodeSpan(n *yaml.Node) Span { return Span{s.Start(n), s.End(n)} }

// EntrySpan returns the bytes of a mapping entry: its key and its value.
func (s *Stream) EntrySpan(key, value *yaml.Node) Span {
	sp := Span{s.Start(key), s.End(key)}
	if value != nil {
		if e := s.End(value); e > sp.End {
			sp.End = e
		}
	}
	return sp
}

// measure records where every node of a document ends and reports the keys
// its mappings state twice.
func (s *Stream) measure(d *Document) {
	if d.Root == nil {
		return
	}
	bound := d.End
	s.measureNode(d.Root, bound, -1, map[*yaml.Node]bool{})
	if e := s.ends[d.Root]; e > d.End {
		s.ends[d.Root] = d.End
	}
}

// measureNode records n's end, n lying before bound, owned by a key or a
// sequence entry at column ownerCol (0-based; -1 at a document's root).
func (s *Stream) measureNode(n *yaml.Node, bound, ownerCol int, seen map[*yaml.Node]bool) int {
	if seen[n] {
		return s.End(n)
	}
	seen[n] = true
	start := s.Start(n)
	end := start
	switch n.Kind {
	case yaml.AliasNode:
		end = start + 1 + len(n.Value)
	case yaml.ScalarNode:
		end = s.scalarEnd(n, start, bound, ownerCol)
	case yaml.MappingNode, yaml.SequenceNode:
		flow := n.Style&yaml.FlowStyle != 0
		step := 1
		if n.Kind == yaml.MappingNode {
			step = 2
		}
		col := s.column(start)
		for i := 0; i < len(n.Content); i += step {
			next := bound
			if i+step < len(n.Content) {
				next = s.Start(n.Content[i+step])
			}
			owner := col
			if flow {
				owner = ownerCol
			}
			if step == 2 {
				k := n.Content[i]
				kb := next
				if i+1 < len(n.Content) {
					kb = s.Start(n.Content[i+1])
				}
				if e := s.measureNode(k, kb, owner, seen); e > end {
					end = e
				}
				if i+1 < len(n.Content) {
					if e := s.measureNode(n.Content[i+1], next, s.column(s.Start(k)), seen); e > end {
						end = e
					}
				}
				continue
			}
			if e := s.measureNode(n.Content[i], next, owner, seen); e > end {
				end = e
			}
		}
		if flow {
			end = s.closingBracket(n, start, end, bound)
		}
		if n.Kind == yaml.MappingNode {
			s.duplicates(n) // once its keys' ends are known
		}
	}
	if end > bound && bound >= start {
		end = bound
	}
	if end < start {
		end = start
	}
	s.ends[n] = end
	return end
}

// column returns the 0-based byte column of off on its line.
func (s *Stream) column(off int) int { return off - s.lineStartOf(off) }

// closingBracket returns the end of a flow collection: past the "]" or "}"
// that follows its last entry.
func (s *Stream) closingBracket(n *yaml.Node, start, end, bound int) int {
	want := byte(']')
	if n.Kind == yaml.MappingNode {
		want = '}'
	}
	i := end
	if i <= start {
		i = start + 1
	}
	for i < bound && i < len(s.Src) {
		switch c := s.Src[i]; c {
		case want:
			return i + 1
		case ' ', '\t', '\r', '\n', ',':
			i++
		case '#':
			for i < len(s.Src) && s.Src[i] != '\n' && s.Src[i] != '\r' {
				i++
			}
		default:
			return end
		}
	}
	return end
}

// scalarEnd returns where a scalar starting at start ends.
func (s *Stream) scalarEnd(n *yaml.Node, start, bound, ownerCol int) int {
	src := s.Src
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0:
		for i := start + 1; i < len(src) && i < bound; i++ {
			switch src[i] {
			case '\\':
				i++
			case '"':
				return i + 1
			}
		}
	case n.Style&yaml.SingleQuotedStyle != 0:
		for i := start + 1; i < len(src) && i < bound; i++ {
			if src[i] == '\'' {
				if i+1 < len(src) && src[i+1] == '\'' {
					i++
					continue
				}
				return i + 1
			}
		}
	case n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		return s.trimTail(start, s.blockEnd(start, bound, ownerCol), ownerCol, 0, false)
	default:
		// The node's text as written, after the anchor and the tag that
		// hold its position ("&a !t x"), unless it spans lines (folded).
		text := start
		for text < bound && text < len(src) && (src[text] == '&' || src[text] == '!') {
			// An anchor or a tag ends at a blank or, in flow, at an
			// indicator ("[&a]", "{k: &a , j: 1}").
			for text < bound && text < len(src) && !isBlank(src[text]) && !strings.ContainsRune(",[]{}", rune(src[text])) {
				text++
			}
			for text < bound && text < len(src) && (src[text] == ' ' || src[text] == '\t') {
				text++
			}
		}
		if text+len(n.Value) <= len(src) && string(src[text:text+len(n.Value)]) == n.Value {
			return text + len(n.Value)
		}
		return s.trimTail(start, bound, ownerCol, 0, true)
	}
	return s.trimTail(start, bound, ownerCol, 0, true)
}

// blockEnd returns where a block scalar's text ends: at the first line
// after its header's that is not blank and is less indented than its text
// (the indentation of its first such line, deeper than its owner when it
// has none) — a comment there and every line after it are no part of it.
func (s *Stream) blockEnd(start, bound, ownerCol int) int {
	src := s.Src
	i := start
	textIndent := -1
	for i < bound && src[i] != '\n' && src[i] != '\r' {
		// An indentation indicator ("|2", ">-1") fixes the text's
		// indentation: the owner's plus it.
		if (src[i] == '|' || src[i] == '>') && textIndent < 0 {
			for j := i + 1; j < bound && j < i+3; j++ {
				if c := src[j]; c >= '1' && c <= '9' {
					textIndent = max(ownerCol, 0) + int(c-'0')
					break
				}
			}
		}
		i++
	}
	for i < bound {
		for i < bound && (src[i] == '\n' || src[i] == '\r') {
			i++
		}
		lineStart := i
		indent := 0
		for i < bound && src[i] == ' ' {
			indent++
			i++
		}
		blank := i >= bound || src[i] == '\n' || src[i] == '\r'
		if !blank {
			if textIndent < 0 {
				textIndent = max(indent, ownerCol+1)
			}
			if indent < textIndent {
				return lineStart
			}
		}
		for i < bound && src[i] != '\n' && src[i] != '\r' {
			i++
		}
	}
	return bound
}

// trimTail returns bound less the text before it that belongs to no node
// starting at start: trailing blanks; then, line by line from the end, lines
// that hold only indicators ("-", "?", ":") at most at ownerCol and comment
// lines — any comment line after a plain scalar (plain text holds none), one
// less indented than textIndent after a block scalar (as indented, it is the
// scalar's text); then a plain scalar's comment on its last line.
func (s *Stream) trimTail(start, bound, ownerCol, textIndent int, plain bool) int {
	src := s.Src
	if bound > len(src) {
		bound = len(src)
	}
	e := bound
	for {
		for e > start && isBlank(src[e-1]) {
			e--
		}
		ls := s.lineStartOf(e - 1)
		if ls <= start || e == ls {
			break
		}
		line := src[ls:e]
		indent := 0
		for indent < len(line) && (line[indent] == ' ' || line[indent] == '\t') {
			indent++
		}
		rest := line[indent:]
		switch {
		case onlyIndicators(rest) && indent <= ownerCol:
			e = ls
			continue
		case len(rest) > 0 && rest[0] == '#' && (plain || indent < textIndent):
			e = ls
			continue
		}
		break
	}
	if plain && e > 0 {
		ls := s.lineStartOf(e - 1)
		if ls < start {
			ls = start
		}
		for i := ls; i+1 < e; i++ {
			if (src[i] == ' ' || src[i] == '\t') && src[i+1] == '#' {
				e = i
				break
			}
		}
		for e > start && isBlank(src[e-1]) {
			e--
		}
	}
	return e
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func onlyIndicators(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c != '-' && c != '?' && c != ':' && c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}

// duplicates reports the scalar keys of a mapping that an earlier key of it
// states too (YAML requires keys to be unique; readers differ on which wins).
func (s *Stream) duplicates(m *yaml.Node) {
	seen := map[string]bool{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		if k.Kind != yaml.ScalarNode || k.Tag == "!!merge" {
			continue
		}
		id := k.Tag + "\x00" + k.Value
		if seen[id] {
			s.problem(Problem{Code: CodeDuplicateKey, Start: s.Start(k), End: s.End(k),
				Message: fmt.Sprintf("the key %q is stated twice in one mapping", k.Value)})
			continue
		}
		seen[id] = true
	}
}

// Deref returns the node an alias names, following a chain of aliases at
// most a bounded number of steps; any other node is returned as it is. ok is
// false for a chain that does not end.
func Deref(n *yaml.Node) (*yaml.Node, bool) {
	for i := 0; n != nil && n.Kind == yaml.AliasNode; i++ {
		if i == maxAliasChain {
			return nil, false
		}
		n = n.Alias
	}
	return n, n != nil
}

// Pairs calls f with each key and value of a mapping, in order.
func Pairs(m *yaml.Node, f func(k, v *yaml.Node)) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		f(m.Content[i], m.Content[i+1])
	}
}

// Get returns the first entry of a mapping whose key is the plain or quoted
// scalar key.
func Get(m *yaml.Node, key string) (k, v *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if c := m.Content[i]; c.Kind == yaml.ScalarNode && c.Value == key && c.Tag != "!!merge" {
			return c, m.Content[i+1]
		}
	}
	return nil, nil
}

// Scalar returns the text of a scalar node (an alias of one included);
// ok is false for any other node.
func Scalar(n *yaml.Node) (string, bool) {
	n, ok := Deref(n)
	if !ok || n.Kind != yaml.ScalarNode {
		return "", false
	}
	return n.Value, true
}

// MaxName bounds the bytes of a text that is a declaration's name.
const MaxName = 256

// IsName reports whether a key or a value may be a declaration's name: one
// line of at most MaxName bytes of valid UTF-8 without a control, line
// break, zero-width or bidirectional formatting character. Names reach the output as identifiers: a multi-line or
// control-character "name" could forge a line of a text result, and a long
// one is data, not a name.
func IsName(s string) bool {
	if s == "" || len(s) > MaxName || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		// Control (Cc), format (Cf: zero-width, bidirectional and other
		// invisible characters), line and paragraph separators.
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Bidi_Control, r) ||
			r == 0x2028 || r == 0x2029 || r == 0x180e {
			return false
		}
	}
	return true
}
