package secrets

import (
	"regexp"
	"strings"
	"sync"
)

// assignment finds the values of secret-like assignments in a line: the
// engine of every KindAssignment rule. Its key pattern matches a key that
// ends in one of the rules' keywords, the operator after it and an opening
// quote; the value's extent is decided from the text after them (cuts). The
// keyword a key matched names the rule that masks its value.
type assignment struct {
	key      *regexp.Regexp // key (group 1), operator (2), opening quote (3)
	anchored *regexp.Regexp // key anchored at the start (startsAssignment)
	keyGroup int            // submatch of the key
	quote    int            // submatch of the opening quote
	suffixes []*regexp.Regexp
	rules    []int // the rule of each keyword

	keepActions   bool // keep a value that is one GitHub Actions reference
	keepTerraform bool // keep a value that is one Terraform reference (.tf)
}

// keyword is one keyword alternative of an assignment's key pattern: a
// regular-expression fragment the key must end in, and its rule.
type keyword struct {
	fragment string
	rule     int
}

// newAssignment compiles the key pattern of keywords, which must not be
// empty. A key may be quoted or an index ("password":, 'password' =>,
// $cfg['api_key'] =): the quote and bracket that close it are no value.
// With the built-in keywords, in their order, it is v6.1.0's key pattern.
func newAssignment(keywords []keyword, keepActions, keepTerraform bool) *assignment {
	alts := make([]string, len(keywords))
	for i, k := range keywords {
		alts[i] = k.fragment
	}
	pat := `(?i)(\b[A-Za-z0-9_\-\$]*(?:` + strings.Join(alts, "|") + `)\b)["']?\]?\s*(=>|:=|==|\|\||&&|=|:|\-)\s*(['"]?)`
	a := &assignment{
		key:           regexp.MustCompile(pat),
		anchored:      regexp.MustCompile(`^(?:` + pat + `)`),
		keyGroup:      1,
		quote:         3,
		keepActions:   keepActions,
		keepTerraform: keepTerraform,
	}
	for _, k := range keywords {
		a.suffixes = append(a.suffixes, regexp.MustCompile(`(?i)(?:`+k.fragment+`)$`))
		a.rules = append(a.rules, k.rule)
	}
	return a
}

// legacyAssignments holds the assignment of each legacy ValueMaskRule key
// pattern (by its source): groups 1 (key) and 3 (opening quote), one rule.
var legacyAssignments sync.Map

func legacyAssignment(re *regexp.Regexp) *assignment {
	if a, ok := legacyAssignments.Load(re.String()); ok {
		return a.(*assignment)
	}
	a := &assignment{
		key:           re,
		anchored:      regexp.MustCompile(`^(?:` + re.String() + `)`),
		keyGroup:      1,
		quote:         3,
		keepActions:   true,
		keepTerraform: true,
	}
	legacyAssignments.Store(re.String(), a)
	return a
}

// ruleOf returns the rule of a key match: the rule of the keyword the key
// pattern matched it by. The pattern matches a key as a greedy prefix and
// the first keyword alternative that completes it, so the keyword is the
// shortest one the key ends in, the earliest of equally long ones.
func (a *assignment) ruleOf(line string, m []int) int {
	key := line[m[2*a.keyGroup]:m[2*a.keyGroup+1]]
	best, bestLen := 0, -1
	for i, re := range a.suffixes {
		if loc := re.FindStringIndex(key); loc != nil {
			if n := loc[1] - loc[0]; bestLen < 0 || n < bestLen {
				best, bestLen = a.rules[i], n
			}
		}
	}
	return best
}

// cuts appends to dst the values of line to mask, in line's coordinates: a
// quoted value ends at its closing quote only when nothing continues it
// (quotedValueEnds); an unquoted value runs to the end of the line, short of
// a separator that ends it (plainValueEnd), and a GitHub Actions expression
// ("${{ ... }}") is never cut. A value the line does not end is masked to the
// end of the line. A value is kept as it is written only when it is a
// reference to a value, never a value: one GitHub Actions context reference
// (isActionsReference), or — in a Terraform file — one Terraform reference
// (isTerraformReference). Anything else is masked. A CRLF file's line ends
// in "\r": it ends the line, not a value. An empty quoted value is a cut of
// no length: its mask is inserted.
func (a *assignment) cuts(line string, syntax Syntax, dst []cut) []cut {
	rest := strings.TrimRight(line, "\r")
	base := 0
	for {
		m := a.key.FindStringSubmatchIndex(rest)
		if m == nil {
			return dst
		}
		quote := ""
		if qs, qe := m[2*a.quote], m[2*a.quote+1]; qs != -1 && qe != -1 {
			quote = rest[qs:qe]
		}
		rule := a.ruleOf(rest, m)
		valStart := m[2*a.quote+1] // the value starts after the opening quote
		val := rest[valStart:]
		var consumed int
		if quote == `"` || quote == `'` {
			// With quotes: up to the closing quote, scanned by byte (a quote
			// and a backslash are ASCII, never part of a multi-byte rune).
			closing := -1
			for i := 0; i < len(val); i++ {
				if val[i] == '\\' {
					i++
					continue
				}
				if val[i] == quote[0] {
					closing = i
					break
				}
			}
			ended := closing != -1 && quotedValueEnds(val[closing+1:])
			if !(ended && a.keepActions && isActionsReference(val[:closing])) {
				end := len(val)
				if ended {
					end = closing
				}
				dst = append(dst, cut{base + valStart, base + valStart + end, rule})
			}
			if ended {
				consumed = closing + 1
			} else {
				// No closing quote, or one the value goes on after
				// (concatenation, an operator, a doubled quote): the rest
				// of the line is the value.
				consumed = len(val)
			}
		} else {
			var end int
			keep := false
			switch {
			case strings.HasPrefix(val, "${{"):
				// A GitHub Actions value: YAML's, to the end of the line.
				end = actionsValueEnd(val)
				keep = a.keepActions && isActionsReference(val[:end])
			case syntax == SyntaxTerraform:
				// A Terraform expression: kept when it is one reference.
				end, keep = terraformValueEnd(val)
				if keep && !a.keepTerraform {
					end, keep = len(val), false
				}
			default:
				end = a.plainValueEnd(val)
			}
			if !keep && end > 0 { // an empty value has nothing to hide
				dst = append(dst, cut{base + valStart, base + valStart + end, rule})
			}
			consumed = end
		}
		next := valStart + consumed
		base += next
		rest = rest[next:]
	}
}

// plainValueEnd returns where an unquoted value starting val ends, in text
// of no known syntax: at the end of the line, trailing blanks excluded, or
// before a comma or semicolon that ends the line (a member of a literal,
// a statement) or starts another secret-like assignment (password=a;token=b),
// which is then masked on its own. Nothing else ends
// it — not a space (password: correct horse battery is one value in YAML,
// a .env file or prose), not a comment marker, not a comma before anything
// but such an assignment — so no part of a value is left out of its mask.
// A GitHub Actions expression ("${{ ... }}") is skipped whole.
func (a *assignment) plainValueEnd(val string) int {
	for i := 0; i < len(val); {
		if strings.HasPrefix(val[i:], "${{") {
			i = actionsExpressionEnd(val, i)
			continue
		}
		if (val[i] == ',' || val[i] == ';') && (strings.TrimRight(val[i+1:], " \t") == "" || a.startsAssignment(val[i+1:])) {
			return len(strings.TrimRight(val[:i], " \t"))
		}
		i++
	}
	return len(strings.TrimRight(val, " \t"))
}

// startsAssignment reports whether s, its leading blanks and an opening
// quote trimmed, starts with a secret-like key and its operator (the key
// pattern, anchored: a long line of many separators costs a match attempt
// at each, never a search of its rest).
func (a *assignment) startsAssignment(s string) bool {
	s = strings.TrimLeft(s, " \t")
	if s != "" && (s[0] == '"' || s[0] == '\'') {
		s = s[1:]
	}
	return a.anchored.MatchString(s)
}

// quotedValueEnds reports whether a quoted value ends at its closing quote,
// from what follows the quote (after): nothing but blanks, a comment, a
// closing bracket, or a comma or semicolon before the end of the line or
// before another key. Anything else — another quote, a word, an operator —
// continues the value (shell concatenation, YAML's doubled quote, an
// expression), which is then masked to the end of the line.
func quotedValueEnds(after string) bool {
	t := strings.TrimLeft(after, " \t")
	switch {
	case t == "":
		return true
	case strings.HasPrefix(t, "#"), strings.HasPrefix(t, "//"), strings.HasPrefix(t, "/*"):
		// A comment after a blank; "x"#y is one shell word.
		return t != after
	case strings.ContainsAny(t[:1], ")]}"):
		return true
	case t[0] == ',' || t[0] == ';':
		next := strings.TrimLeft(t[1:], " \t")
		return next == "" || strings.ContainsAny(next[:1], ")]}") || keyStart.MatchString(next)
	}
	return false
}

// keyStart matches the start of a key and its operator: a bare, quoted or
// indexed name followed by "=>", ":=", "=" or ":" (user: x, "user": x,
// 'user' => x).
var keyStart = regexp.MustCompile(`^["']?[A-Za-z_$][A-Za-z0-9_$.\-]*["']?\]?\s*(?:=>|:=|=|:)`)

// actionsValueEnd returns where an unquoted YAML value that starts with a
// GitHub Actions expression ends: before a comment (" #" outside an
// expression) or at the end of the line, trailing blanks excluded. A YAML
// plain value runs to the end of its line — what follows an expression is
// the same value, so it is masked with it.
func actionsValueEnd(val string) int {
	end := len(val)
	for i := 0; i < len(val); {
		if strings.HasPrefix(val[i:], "${{") {
			i = actionsExpressionEnd(val, i)
			continue
		}
		if val[i] == '#' && i > 0 && (val[i-1] == ' ' || val[i-1] == '\t') {
			end = i
			break
		}
		i++
	}
	return len(strings.TrimRight(val[:end], " \t"))
}

// terraformValueEnd returns where the unquoted value of a Terraform attribute
// ends and whether it is kept: one Terraform reference (its first word,
// isTerraformReference, up to a blank, "," or ";") followed by nothing but
// blanks, a comment ("#", "//"), a separator (",") or a closing bracket is
// kept and ends there; any
// other expression — an operator, a literal, a call after or around a
// reference — is masked to the end of the line.
func terraformValueEnd(val string) (end int, keep bool) {
	word := strings.IndexAny(val, " \t,;")
	if word < 0 {
		word = len(val)
	}
	if !isTerraformReference(val[:word]) {
		return len(val), false
	}
	after := strings.TrimLeft(val[word:], " \t")
	if after == "" || strings.HasPrefix(after, "#") || strings.HasPrefix(after, "//") || strings.ContainsAny(after[:1], ",)]}") {
		return word, true
	}
	return len(val), false
}

// actionsExpressionEnd returns the index just past the expression starting at
// val[start] ("${{"), or len(val) if it is not closed.
func actionsExpressionEnd(val string, start int) int {
	inString := false
	for i := start + 3; i < len(val); i++ {
		switch {
		case inString && val[i] == '\'':
			if i+1 < len(val) && val[i+1] == '\'' {
				i++ // '' is a quote inside the literal
			} else {
				inString = false
			}
		case inString:
		case val[i] == '\'':
			inString = true
		case strings.HasPrefix(val[i:], "}}"):
			return i + 2
		}
	}
	return len(val)
}

// actionsReference is one GitHub Actions expression that only reads a
// context property: "${{ secrets.NAME }}", "${{ steps.id.outputs.x }}". It has
// no literal, operator, function call or index, so it names a value and cannot
// hold one. The contexts are those GitHub Actions defines.
var actionsReference = regexp.MustCompile(`^\$\{\{ *(?:secrets|vars|env|inputs|github|steps|needs|matrix|jobs|job|runner|strategy)(?:\.[A-Za-z_][A-Za-z0-9_-]{0,99})+ *\}\}$`)

func isActionsReference(v string) bool { return actionsReference.MatchString(v) }

// terraformReference is one Terraform reference, as written unquoted in a .tf
// file: an input variable, a local value, a module output, a data source's or
// a managed resource's attribute — a static traversal of identifiers and
// numeric or splat indexes, with no literal, operator or function call, so it
// names a value and cannot hold one. A resource is TYPE.NAME.ATTRIBUTE, TYPE
// with the provider's prefix and an underscore (aws_db_instance), so a
// two-part word such as "my.secret" is never taken for one.
var terraformReference = func() *regexp.Regexp {
	id := `[A-Za-z_][A-Za-z0-9_-]{0,99}`             // a Terraform identifier
	typ := `[a-z][a-z0-9]*_[a-z0-9_]{1,99}`          // a resource type: provider_name
	step := `(?:\.` + id + `|\[[0-9]{1,9}\]|\[\*\])` // .attr, [0], [*]
	return regexp.MustCompile(`^(?:` +
		`(?:var|local)\.` + id + step + `*` +
		`|module\.` + id + step + `*` +
		`|data\.` + typ + `\.` + id + step + `*` +
		`|` + typ + `\.` + id + step + `+` +
		`)$`)
}()

func isTerraformReference(v string) bool { return terraformReference.MatchString(v) }
