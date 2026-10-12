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
)

// defaultPatterns are the built-in pattern rules' expressions, in order: the
// legacy RuleSet's FullMaskPatterns.
var defaultPatterns = func() []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(builtinPatterns))
	for i, p := range builtinPatterns {
		out[i] = p.re
	}
	return out
}()

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
// the file actually read (never from the text itself): the value of every
// assignment whose key the rule's KeyPattern matches is masked as the
// built-in assignment rules mask (see assignment.cuts).
//
// Deprecated: use a Policy (Recommended, NewPolicy), which applies every
// rule; MaskLineIn applies one key pattern to one line.
func (r *ValueMaskRule) MaskLineIn(line string, syntax Syntax) string {
	t := ptext{s: line}
	t.apply(legacyAssignment(r.KeyPattern).cuts(line, syntax, nil), nil)
	return t.s
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

// RuleSet is the legacy, mutable rule set of the scanner (ScanReader).
//
// Deprecated: masking uses a Policy (Recommended, NewPolicy); ScanReader
// does not find what masking masks. Use Policy.Find.
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

// MaskSecretKeyBlocks masks every whole PEM private-key block of content.
func MaskSecretKeyBlocks(content string) string {
	return keyBlockPattern.ReplaceAllString(content, maskText)
}

// MaskAll returns content with every secret the recommended rules detect
// replaced by "*****MASKED*****": whole private-key blocks, the value of
// every secret-like assignment, and every match of a secret pattern. It is a
// pure function of content, safe for concurrent use.
func MaskAll(content string) string {
	return MaskAllIn(content, SyntaxUnknown)
}

// MaskAllIn is MaskAll for the content of a file in syntax — known from the
// file read, never from content — which lets a value that is a reference be
// kept as written. Every secret pattern still applies to every line,
// references included.
func MaskAllIn(content string, syntax Syntax) string {
	return Recommended().Mask(content, Mode{Syntax: syntax})
}

// MaskCompactIn is MaskAllIn for text in one of Ark's compact formats
// (arklite), in which "␤" stands for a line break: a value ends at a "␤" as
// at a newline, so masking one never swallows the lines after it. Only text
// whose "␤"s are known to be line breaks may be masked this way — a "␤" a
// file holds is no line break, and MaskAllIn masks a value through it; the
// compact formats mask each file's content with MaskAllIn before they join
// its lines with "␤".
func MaskCompactIn(content string, syntax Syntax) string {
	return Recommended().Mask(content, Mode{Syntax: syntax, Compact: true})
}
