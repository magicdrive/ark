package secrets

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

var defaultPatterns = []*regexp.Regexp{
	// AWS
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`ASIA[0-9A-Z]{16}`),
	regexp.MustCompile(`A3T[A-Z0-9]{16}`),
	regexp.MustCompile(`AGPA[A-Z0-9]{16}`),
	regexp.MustCompile(`AIDA[A-Z0-9]{16}`),
	regexp.MustCompile(`AROA[A-Z0-9]{16}`),
	regexp.MustCompile(`AIPA[A-Z0-9]{16}`),
	regexp.MustCompile(`ANPA[A-Z0-9]{16}`),
	regexp.MustCompile(`ANVA[A-Z0-9]{16}`),
	// GCP
	regexp.MustCompile(`"type":\s*"service_account".*"private_key_id":\s*"[a-f0-9]+".*"private_key":\s*"-----BEGIN PRIVATE KEY-----[^"]+"`),
	regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`),
	// GCP service account keys
	regexp.MustCompile(`private_key_id\s*=\s*"[a-f0-9]{32}"`),
	regexp.MustCompile(`"private_key_id"\s*:\s*"[a-f0-9]{32}"`),
	regexp.MustCompile(`"private_key"\s*:\s*"(-----BEGIN PRIVATE KEY-----[^"]+)"`),
	// Azure
	regexp.MustCompile(`AccountKey=([A-Za-z0-9+/=]{88})`),
	regexp.MustCompile(`[0-9a-f]{32}-[0-9a-f]{32}-[0-9a-f]{32}`),
	// GitHub/GitLab
	regexp.MustCompile(`ghp_[A-Za-z0-9_]{36,255}`),
	regexp.MustCompile(`ghu_[A-Za-z0-9_]{36,255}`),
	regexp.MustCompile(`ghs_[A-Za-z0-9_]{36,255}`),
	regexp.MustCompile(`glpat-[0-9a-zA-Z\-\_]{20}`),
	// Slack
	regexp.MustCompile(`xox[baprs]-([0-9a-zA-Z]{10,48})?`),
	// Stripe
	regexp.MustCompile(`sk_live_[0-9a-zA-Z]{24}`),
	regexp.MustCompile(`pk_live_[0-9a-zA-Z]{24}`),
	// SendGrid
	regexp.MustCompile(`SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}`),
	// Google OAuth/Client
	regexp.MustCompile(`ya29\.[0-9A-Za-z\-_]+`),
	regexp.MustCompile(`[0-9]+-([0-9A-Za-z_]{32})\.apps\.googleusercontent\.com`),
	// Private Keys
	regexp.MustCompile(`-----BEGIN (RSA|DSA|EC|OPENSSH|PRIVATE|ENCRYPTED) PRIVATE KEY-----`),
	// JWT
	regexp.MustCompile(`JWT_SECRET(.{0,10})?([=:])(.{0,100})`),
	regexp.MustCompile(`eyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}`),
}

// ===== Allow-list patterns (to avoid false positives for known test/demo values) =====
var defaultAllowed = []*regexp.Regexp{
	regexp.MustCompile(`AKIAIOSFODNN7EXAMPLE`),
	regexp.MustCompile(`wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`),
}

type SecretMatch struct {
	File    string
	Line    int
	Text    string
	Pattern string
}

type ValueMaskRule struct {
	KeyPattern *regexp.Regexp
}

func defaultValueMaskRule() *ValueMaskRule {
	// A key may be quoted or an index ("password":, 'password' =>,
	// $cfg['api_key'] =): the quote and bracket that close it are no value.
	pat := `(?i)(\b[A-Za-z0-9_\-\$]*(?:secret|password|passwd|pass|pw|token|api[_\-]?key|access[_\-]?key|private[_\-]?key)\b)["']?\]?\s*(=>|:=|==|\|\||&&|=|:|\-)\s*(['"]?)`
	return &ValueMaskRule{
		KeyPattern: regexp.MustCompile(pat),
	}
}

// MaskLine masks the value of every secret-like assignment of line, as text
// of no particular syntax (see MaskLineIn).
func (r *ValueMaskRule) MaskLine(line string) string {
	return r.MaskLineIn(line, SyntaxUnknown)
}

// MaskLineIn is MaskLine for a line of a file in syntax, which must come from
// the file actually read (never from the text itself). A quoted value ends at
// its closing quote only when nothing continues it (quotedValueEnds); an
// unquoted value runs to the end of the line, short of a separator that ends
// it (plainValueEnd), and a GitHub Actions expression ("${{ ... }}") is never
// cut. A value the line does not end is masked to the end of the line. A
// value is kept as it is written only when it is a reference to a
// value, never a value: one GitHub Actions context reference
// (isActionsReference), or — in a Terraform file — one Terraform reference
// (isTerraformReference). Anything else is masked.
func (r *ValueMaskRule) MaskLineIn(line string, syntax Syntax) string {
	// A CRLF file's line ends in "\r": it ends the line, not a value.
	if body, ok := strings.CutSuffix(line, "\r"); ok {
		return r.MaskLineIn(body, syntax) + "\r"
	}
	var result strings.Builder
	rest := line
	for {
		m := r.KeyPattern.FindStringSubmatchIndex(rest)
		if m == nil {
			result.WriteString(rest)
			break
		}
		// m[0] = full match start, m[1] = full match end,
		// m[2]=key start, m[3]=key end, m[4]=delimiter start,
		// m[5]=delimiter end, m[6]=quote start, m[7]=quote end

		// 1. llways output the part after the previous match up to the key (commas, spaces, etc. can be entered here)
		result.WriteString(rest[:m[2]])

		// 2. print key, separator, leading space, and opening quote
		result.WriteString(rest[m[2]:m[6]])
		quote := ""
		if m[6] != -1 && m[7] != -1 {
			quote = rest[m[6]:m[7]]
			result.WriteString(quote)
		}

		maskedValue := "*****MASKED*****"
		valStart := m[7] // starting index of the value body (immediately after the quote)
		val := rest[valStart:]

		if quote == `"` || quote == `'` {
			// With quotes: up to the closing quote, scanned by byte (a quote
			// and a backslash are ASCII, never part of a multi-byte rune).
			closingIdx := -1
			for i := 0; i < len(val); i++ {
				if val[i] == '\\' {
					i++
					continue
				}
				if val[i] == quote[0] {
					closingIdx = i
					break
				}
			}
			ended := closingIdx != -1 && quotedValueEnds(val[closingIdx+1:])
			if ended && isActionsReference(val[:closingIdx]) {
				result.WriteString(val[:closingIdx])
			} else {
				result.WriteString(maskedValue)
			}
			if ended {
				result.WriteString(quote)
				valStart += closingIdx + 1
			} else {
				// No closing quote, or one the value goes on after
				// (concatenation, an operator, a doubled quote): the rest
				// of the line is the value.
				valStart += len(val)
			}
		} else {
			// no quoted
			var end int
			keep := false
			switch {
			case strings.HasPrefix(val, "${{"):
				// A GitHub Actions value: YAML's, to the end of the line.
				end = actionsValueEnd(val)
				keep = isActionsReference(val[:end])
			case syntax == SyntaxTerraform:
				// A Terraform expression: kept when it is one reference.
				end, keep = terraformValueEnd(val)
			default:
				end = r.plainValueEnd(val)
			}
			switch {
			case keep:
				result.WriteString(val[:end])
			case end > 0: // an empty value has nothing to hide
				result.WriteString(maskedValue)
			}
			valStart += end
		}

		// remainder is rest.
		rest = rest[valStart:]
	}
	return result.String()
}

// Syntax is the format of the file a text comes from, when the caller knows
// it from the file it read. It only lets a reference be told from a value;
// it never stops a secret pattern from matching.
type Syntax string

const (
	// SyntaxUnknown is text of no known format: no Terraform rule applies.
	SyntaxUnknown Syntax = ""
	// SyntaxTerraform is a Terraform configuration file (.tf), where an
	// unquoted value is an expression and never a string literal.
	SyntaxTerraform Syntax = "terraform"
)

// plainValueEnd returns where an unquoted value starting val ends, in text
// of no known syntax: at the end of the line, trailing blanks excluded, or
// before a comma or semicolon that ends the line (a member of a literal,
// a statement) or starts another secret-like assignment (password=a;token=b),
// which is then masked on its own. Nothing else ends
// it — not a space (password: correct horse battery is one value in YAML,
// a .env file or prose), not a comment marker, not a comma before anything
// but such an assignment — so no part of a value is left out of its mask.
// A GitHub Actions expression ("${{ ... }}") is skipped whole.
func (r *ValueMaskRule) plainValueEnd(val string) int {
	for i := 0; i < len(val); {
		if strings.HasPrefix(val[i:], "${{") {
			i = actionsExpressionEnd(val, i)
			continue
		}
		if (val[i] == ',' || val[i] == ';') && (strings.TrimRight(val[i+1:], " \t") == "" || r.startsAssignment(val[i+1:])) {
			return len(strings.TrimRight(val[:i], " \t"))
		}
		i++
	}
	return len(strings.TrimRight(val, " \t"))
}

// startsAssignment reports whether s, its leading blanks and an opening
// quote trimmed, starts with a secret-like key and its operator (the rule's
// KeyPattern, anchored: a long line of many separators costs a match attempt
// at each, never a search of its rest).
func (r *ValueMaskRule) startsAssignment(s string) bool {
	s = strings.TrimLeft(s, " \t")
	if s != "" && (s[0] == '"' || s[0] == '\'') {
		s = s[1:]
	}
	return anchoredPattern(r.KeyPattern).MatchString(s)
}

// anchoredPatterns caches, by its source, a pattern anchored at the start.
var anchoredPatterns sync.Map

func anchoredPattern(re *regexp.Regexp) *regexp.Regexp {
	if a, ok := anchoredPatterns.Load(re.String()); ok {
		return a.(*regexp.Regexp)
	}
	a := regexp.MustCompile(`^(?:` + re.String() + `)`)
	anchoredPatterns.Store(re.String(), a)
	return a
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

type RuleSet struct {
	FullMaskPatterns []*regexp.Regexp // Default+Added patterns (order matters!)
	Allowed          []*regexp.Regexp
	ValueMaskRules   []*ValueMaskRule
}

func DefaultRuleSet() *RuleSet {
	return &RuleSet{
		FullMaskPatterns: slices.Clone(defaultPatterns),
		Allowed:          slices.Clone(defaultAllowed),
		ValueMaskRules:   []*ValueMaskRule{defaultValueMaskRule()},
	}
}

func (r *RuleSet) AddPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	r.FullMaskPatterns = append(r.FullMaskPatterns, re)
	return nil
}

func (r *RuleSet) AddAllowed(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	r.Allowed = append(r.Allowed, re)
	return nil
}

func (r *RuleSet) AddValueMaskKey(keywordList []string) {
	var quoted []string
	for _, k := range keywordList {
		if !regexp.MustCompile(`^[A-Za-z0-9_\-\$]+$`).MatchString(k) {
			continue
		}
		quoted = append(quoted, regexp.QuoteMeta(k))
	}
	keyword := strings.Join(quoted, "|")
	// e.g.  (?i)(secret|password|api_key)\s*([=:\-])\s*(['"]?)([^\s'"]+)(['"]?)
	pat := fmt.Sprintf(`(?i)(%s)\s*([=:\-])\s*(['"]?)([^'"]+)(['"]?)`, keyword)
	r.ValueMaskRules = append(r.ValueMaskRules, &ValueMaskRule{
		KeyPattern: regexp.MustCompile(pat),
	})
}

func (r *RuleSet) isAllowed(s string) bool {
	for _, a := range r.Allowed {
		if a.MatchString(s) {
			return true
		}
	}
	return false
}

func (r *RuleSet) ScanReader(rd io.Reader, filename string) ([]SecretMatch, error) {
	var matches []SecretMatch
	scanner := bufio.NewScanner(rd)
	lineNum := 0

LINE:
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if r.isAllowed(line) {
			continue
		}
		for _, pat := range r.FullMaskPatterns {
			if pat.MatchString(line) {
				matches = append(matches, SecretMatch{
					File:    filename,
					Line:    lineNum,
					Text:    line,
					Pattern: pat.String(),
				})
				continue LINE
			}
		}
		for _, vmr := range r.ValueMaskRules {
			if vmr.KeyPattern.MatchString(line) {
				matches = append(matches, SecretMatch{
					File:    filename,
					Line:    lineNum,
					Text:    line,
					Pattern: "value-masking",
				})
				break
			}
		}
	}
	return matches, scanner.Err()
}

func (r *RuleSet) ScanFile(path string) ([]SecretMatch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return r.ScanReader(f, path)
}

func (r *RuleSet) ScanDir(root string, recursive bool) ([]SecretMatch, error) {
	var results []SecretMatch
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !IsTextFile(path) {
			return nil
		}
		m, err := r.ScanFile(path)
		if err == nil && len(m) > 0 {
			results = append(results, m...)
		}
		return nil
	})
	return results, err
}

func IsTextFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 800)
	n, _ := f.Read(buf)
	for i := range n {
		b := buf[i]
		if b < 0x09 || (b > 0x0D && b < 0x20) {
			return false
		}
	}
	return true
}

// keyBlockPattern matches a whole PEM private-key block, across lines.
var keyBlockPattern = regexp.MustCompile(`(?ms)-----BEGIN (?:RSA|DSA|EC|OPENSSH|PRIVATE|ENCRYPTED) PRIVATE KEY-----.*?-----END (?:RSA|DSA|EC|OPENSSH|PRIVATE|ENCRYPTED) PRIVATE KEY-----`)

func MaskSecretKeyBlocks(content string) string {
	return keyBlockPattern.ReplaceAllString(content, "*****MASKED*****")
}

// MaskAll returns content with every secret the default rules detect
// replaced by "*****MASKED*****": whole private-key blocks, the value of
// every secret-like assignment, and every match of a secret pattern. It is a
// pure function of content, safe for concurrent use.
func MaskAll(content string) string {
	return MaskAllIn(content, SyntaxUnknown)
}

// MaskAllIn is MaskAll for the content of a file in syntax — known from the
// file read, never from content — which lets a value that is a reference be
// kept as written (MaskLineIn). Every secret pattern still applies to every
// line, references included.
func MaskAllIn(content string, syntax Syntax) string {
	return maskLines(content, syntax, false)
}

// MaskCompactIn is MaskAllIn for text in one of Ark's compact formats
// (arklite), in which "␤" stands for a line break: a value ends at a "␤" as
// at a newline, so masking one never swallows the lines after it. Only text
// whose "␤"s are known to be line breaks may be masked this way — a "␤" a
// file holds is no line break, and MaskAllIn masks a value through it; the
// compact formats mask each file's content with MaskAllIn before they join
// its lines with "␤".
func MaskCompactIn(content string, syntax Syntax) string {
	return maskLines(content, syntax, true)
}

// maskLines masks content line by line; with compact, a "␤" breaks lines too.
func maskLines(content string, syntax Syntax, compact bool) string {
	content = MaskSecretKeyBlocks(content)
	rules := DefaultRuleSet()
	var block valueBlock
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if !compact {
			lines[i] = block.mask(rules, line, syntax)
			continue
		}
		segments := strings.Split(line, lineBreakSymbol)
		for j, seg := range segments {
			segments[j] = block.mask(rules, seg, syntax)
		}
		lines[i] = strings.Join(segments, lineBreakSymbol)
	}
	return strings.Join(lines, "\n")
}

// A valueBlock is the value of a secret-like key that continues on the lines
// after the key's: a heredoc ("<<EOT", "<<-EOT", "<<~EOT", "<<<'EOT'"), whose body runs to its
// terminator line, or a YAML block scalar ("|", ">-", ...), whose body is
// every following line indented deeper than the key, and blank lines. Every
// line of a body is masked whole, its indentation kept; masking per line
// alone would mask only the line of the key.
type valueBlock struct {
	heredoc string // the terminator of an open heredoc
	indent  int    // the key's column of an open block scalar, or -1
	open    bool
}

var (
	heredocStart     = regexp.MustCompile(`^<<[-~<]?[ \t]*(["']?)([A-Za-z_][A-Za-z0-9_]*)(["']?)[ \t]*$`)
	blockScalarStart = regexp.MustCompile(`^[|>][1-9+-]{0,2}[ \t]*(?:#.*)?$`)
)

// mask masks one line: a line of an open body whole, any other by rules
// (maskLine), noting a body its last secret-like value opens.
func (b *valueBlock) mask(rules *RuleSet, line string, syntax Syntax) string {
	body, cr := strings.CutSuffix(line, "\r")
	suffix := ""
	if cr {
		suffix = "\r"
	}
	if b.open {
		trimmed := strings.TrimLeft(body, " \t")
		indent := len(body) - len(trimmed)
		switch {
		case b.heredoc != "":
			if endsHeredoc(trimmed, b.heredoc) {
				b.open = false
				return line
			}
			if trimmed == "" {
				return line
			}
			return body[:indent] + "*****MASKED*****" + suffix
		case trimmed == "":
			return line
		case indent > b.indent:
			return body[:indent] + "*****MASKED*****" + suffix
		}
		b.open = false
	}
	b.opens(rules, body)
	return maskLine(rules, line, syntax)
}

// endsHeredoc reports whether a line, its indentation trimmed, is the
// terminator of a heredoc: the word alone, or followed by ";", "," or ")"
// (PHP).
func endsHeredoc(trimmed, word string) bool {
	rest, ok := strings.CutPrefix(trimmed, word)
	return ok && (rest == "" || strings.ContainsAny(rest[:1], ";,)"))
}

// opens notes the body the last secret-like value of line opens, if any.
func (b *valueBlock) opens(rules *RuleSet, line string) {
	for _, vmr := range rules.ValueMaskRules {
		locs := vmr.KeyPattern.FindAllStringSubmatchIndex(line, -1)
		if len(locs) == 0 {
			continue
		}
		loc := locs[len(locs)-1]
		if loc[7] > loc[6] { // a quoted value (group 3) ends on its line
			continue
		}
		val := line[loc[1]:]
		if m := heredocStart.FindStringSubmatch(val); m != nil && m[1] == m[3] {
			*b = valueBlock{heredoc: m[2], indent: -1, open: true}
			return
		}
		if blockScalarStart.MatchString(val) {
			*b = valueBlock{indent: loc[2], open: true}
			return
		}
	}
}

// lineBreakSymbol is what Ark's compact formats write for a line break.
const lineBreakSymbol = "\u2424"

// maskLine applies every rule of rules to one line: the secret patterns
// first, to the line as written — a value's mask must not break a pattern
// that spans it (a service account's JSON) — then the value rules.
func maskLine(rules *RuleSet, line string, syntax Syntax) string {
	for _, pat := range rules.FullMaskPatterns {
		line = pat.ReplaceAllString(line, "*****MASKED*****")
	}
	for _, vmr := range rules.ValueMaskRules {
		line = vmr.MaskLineIn(line, syntax)
	}
	return line
}
