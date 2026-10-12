package secrets

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"sync"
)

// Rule describes one rule of a policy.
type Rule struct {
	ID          string
	Description string
	Kind        RuleKind
	Source      RuleSource
	Enabled     bool
	// Reason is why a built-in rule is disabled, as its configuration says.
	Reason string
}

// Spec is what a policy is built from: the built-in rules, less those it
// disables, plus its own rules. The zero Spec is the recommended policy.
type Spec struct {
	Disable     []Disable
	Patterns    []PatternSpec
	Assignments []AssignmentSpec
}

// Disable turns a built-in rule off. RuleGenericAssignment turns every
// generic-assignment-* rule off.
type Disable struct {
	ID     string
	Reason string
}

// PatternSpec is a pattern rule: every match of Pattern (or, with Group > 0,
// that capture group of every match) is masked.
type PatternSpec struct {
	ID          string
	Description string
	Pattern     string
	Group       int
}

// AssignmentSpec is an assignment rule: the value of an assignment whose
// key ends in one of Keys is masked, as the built-in generic-assignment
// rules mask theirs (value boundaries, heredocs, block scalars, references).
type AssignmentSpec struct {
	ID          string
	Description string
	Keys        []string
}

// Limits of a Spec, so a policy stays cheap to apply to every line.
const (
	MaxRules       = 1000 // pattern and assignment rules, together
	MaxPatternLen  = 4096 // bytes of one pattern
	MaxKeys        = 100  // keys of one assignment rule
	MaxKeyLen      = 64   // bytes of one key
	MaxIDLen       = 64
	MaxDescription = 200
)

var (
	ruleIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	keyPattern    = regexp.MustCompile(`^[A-Za-z0-9_]+(?:-[A-Za-z0-9_]+)*$`)
)

// Weakening reports the rules s disables that mask (not the reference
// exceptions, whose disabling masks more): what makes its policy mask less
// than the recommended one.
func (s Spec) Weakening() []Disable {
	var out []Disable
	for _, d := range s.Disable {
		if d.ID == RuleKeepActionsReference || d.ID == RuleKeepTerraformReference {
			continue
		}
		out = append(out, d)
	}
	return out
}

// WithoutWeakening returns s without the disabling of rules that mask.
func (s Spec) WithoutWeakening() Spec {
	out := s
	out.Disable = nil
	for _, d := range s.Disable {
		if d.ID == RuleKeepActionsReference || d.ID == RuleKeepTerraformReference {
			out.Disable = append(out.Disable, d)
		}
	}
	return out
}

// Policy is a compiled set of rules: what Mask masks and Find finds. It is
// immutable and safe for concurrent use; build one once and reuse it for
// every text.
type Policy struct {
	rules    []Rule
	patterns []compiledPattern
	pemRule  int // index of the PEM block rule, or -1 when disabled
	assign   *assignment
}

// Mode is how a text is read: its syntax (from the file it was read from,
// never from the text) and whether it is in a compact format, in which "␤"
// stands for a line break.
type Mode struct {
	Syntax  Syntax
	Compact bool
}

// SyntaxForFile returns the syntax of a file from its name: the name of the
// regular file actually read (after symlinks), never a name asked for or a
// link's. A Terraform configuration file (".tf") is SyntaxTerraform; any
// other is SyntaxUnknown. The CLI dump and the MCP server both decide by it.
func SyntaxForFile(name string) Syntax {
	if strings.HasSuffix(name, ".tf") {
		return SyntaxTerraform
	}
	return SyntaxUnknown
}

var (
	recommendedOnce sync.Once
	recommended     *Policy
)

// Recommended returns Ark's recommended policy: every built-in rule, which
// masks exactly what v6.1.0 masked.
func Recommended() *Policy {
	recommendedOnce.Do(func() {
		p, err := NewPolicy(Spec{})
		if err != nil {
			panic("secrets: the built-in rules do not compile: " + err.Error())
		}
		recommended = p
	})
	return recommended
}

// BuiltinRuleIDs returns the IDs of the built-in rules, in listing order.
func BuiltinRuleIDs() []string {
	ids := make([]string, len(builtinRules))
	for i, r := range builtinRules {
		ids[i] = r.ID
	}
	return ids
}

// NewPolicy compiles spec. Every problem is reported, joined, naming the
// rule and field — never a value a pattern would match.
func NewPolicy(spec Spec) (*Policy, error) {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	p := &Policy{pemRule: -1}
	index := map[string]int{}
	for _, r := range builtinRules {
		r.Enabled = true
		index[r.ID] = len(p.rules)
		p.rules = append(p.rules, r)
	}

	disabled := map[string]bool{}
	for _, d := range spec.Disable {
		ids := []string{d.ID}
		if d.ID == RuleGenericAssignment {
			ids = nil
			for _, k := range builtinKeywords {
				ids = append(ids, k.rule)
			}
		} else if _, ok := index[d.ID]; !ok {
			fail("disable %q: no built-in rule has this ID", d.ID)
			continue
		}
		if disabled[d.ID] {
			fail("disable %q: disabled twice", d.ID)
			continue
		}
		disabled[d.ID] = true
		for _, id := range ids {
			r := &p.rules[index[id]]
			r.Enabled = false
			if r.Reason == "" {
				r.Reason = d.Reason
			}
		}
	}

	if n := len(spec.Patterns) + len(spec.Assignments); n > MaxRules {
		fail("%d rules: at most %d are allowed", n, MaxRules)
	}
	own := func(id, desc, what string) bool {
		switch {
		case len(id) > MaxIDLen || !ruleIDPattern.MatchString(id):
			fail("%s %q: an ID is lower-case letters and digits in hyphen-separated words, at most %d bytes", what, id, MaxIDLen)
		case id == RuleGenericAssignment:
			fail("%s %q: the ID is reserved for the built-in rules", what, id)
		case index[id] >= 0 && p.hasRule(index, id):
			if p.rules[index[id]].Source == SourceBuiltin {
				fail("%s %q: a built-in rule has this ID", what, id)
			} else {
				fail("%s %q: defined twice", what, id)
			}
		case len(desc) > MaxDescription:
			fail("%s %q: description longer than %d bytes", what, id, MaxDescription)
		default:
			return true
		}
		return false
	}

	// Patterns: the built-in ones (enabled) in their order, then the
	// configuration's.
	for _, bp := range builtinPatterns {
		if i := index[bp.rule]; p.rules[i].Enabled {
			p.patterns = append(p.patterns, compiledPattern{re: bp.re, rule: i})
		}
	}
	for _, ps := range spec.Patterns {
		if !own(ps.ID, ps.Description, "rule") {
			continue
		}
		if len(ps.Pattern) > MaxPatternLen {
			fail("rule %q: pattern longer than %d bytes", ps.ID, MaxPatternLen)
			continue
		}
		re, err := regexp.Compile(ps.Pattern)
		if err != nil {
			fail("rule %q: pattern does not compile: %s", ps.ID, regexpProblem(err))
			continue
		}
		if re.MatchString("") {
			fail("rule %q: pattern matches the empty string, so it would match everywhere", ps.ID)
			continue
		}
		if ps.Group < 0 || ps.Group > re.NumSubexp() {
			fail("rule %q: capture group %d: the pattern has %d", ps.ID, ps.Group, re.NumSubexp())
			continue
		}
		index[ps.ID] = len(p.rules)
		p.rules = append(p.rules, Rule{ID: ps.ID, Description: ps.Description, Kind: KindPattern, Source: SourceConfig, Enabled: true})
		p.patterns = append(p.patterns, compiledPattern{re: re, group: ps.Group, rule: index[ps.ID]})
	}

	if i := index[RulePEMBlock]; p.rules[i].Enabled {
		p.pemRule = i
	}

	// Assignments: the built-in keywords (enabled) in their order, then the
	// configuration's keys, all in one key pattern.
	var keywords []keyword
	for _, k := range builtinKeywords {
		if i := index[k.rule]; p.rules[i].Enabled {
			keywords = append(keywords, keyword{fragment: k.fragment, rule: i})
		}
	}
	for _, as := range spec.Assignments {
		if !own(as.ID, as.Description, "assignment") {
			continue
		}
		if len(as.Keys) == 0 || len(as.Keys) > MaxKeys {
			fail("assignment %q: between 1 and %d keys are needed", as.ID, MaxKeys)
			continue
		}
		ok := true
		for _, k := range as.Keys {
			if len(k) > MaxKeyLen || !keyPattern.MatchString(k) {
				fail("assignment %q: key %q: a key is letters, digits and underscores, in hyphen-separated words, at most %d bytes", as.ID, k, MaxKeyLen)
				ok = false
			}
		}
		if !ok {
			continue
		}
		index[as.ID] = len(p.rules)
		p.rules = append(p.rules, Rule{ID: as.ID, Description: as.Description, Kind: KindAssignment, Source: SourceConfig, Enabled: true})
		for _, k := range as.Keys {
			keywords = append(keywords, keyword{fragment: regexp.QuoteMeta(k), rule: index[as.ID]})
		}
	}
	if len(keywords) > 0 {
		p.assign = newAssignment(keywords,
			p.rules[index[RuleKeepActionsReference]].Enabled,
			p.rules[index[RuleKeepTerraformReference]].Enabled)
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return p, nil
}

// hasRule reports whether index has id.
func (p *Policy) hasRule(index map[string]int, id string) bool {
	_, ok := index[id]
	return ok
}

// regexpProblem is a regexp compile error without the pattern's text.
func regexpProblem(err error) string {
	var se *syntax.Error
	if errors.As(err, &se) {
		return string(se.Code)
	}
	return "invalid regular expression"
}

// Rules returns the policy's rules, in listing order: the built-in ones,
// then the configuration's.
func (p *Policy) Rules() []Rule { return slices.Clone(p.rules) }

// Mask returns content with every span the policy finds replaced by
// "*****MASKED*****" (overlapping spans become one mask).
func (p *Policy) Mask(content string, mode Mode) string {
	return p.run(content, mode, nil)
}

// Finding is one span a rule of a policy masks, located in the original
// text. It never holds the text it covers.
type Finding struct {
	RuleID string
	// Start and End are byte offsets of the span in the text; End == Start
	// for a mask inserted at an empty value ("").
	Start, End int
	// Line and Column (1-based; Column in bytes) are where the span starts.
	Line, Column int
}

// Find returns the spans the policy masks in content, ordered by start, end
// and rule ID: what Mask would mask, found by the same pipeline. Spans of
// different rules may overlap — a secret can match several — each with its
// own rule.
func (p *Policy) Find(content string, mode Mode) []Finding {
	var rec spans
	p.run(content, mode, &rec)
	out := make([]Finding, len(rec))
	for i, f := range rec {
		out[i] = Finding{RuleID: p.rules[f.rule].ID, Start: f.oStart, End: f.oEnd}
	}
	slices.SortStableFunc(out, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(a.Start, b.Start), cmp.Compare(a.End, b.End), strings.Compare(a.RuleID, b.RuleID))
	})
	out = slices.Compact(out)
	locate(content, out)
	return out
}

// locate sets the Line and Column of findings, sorted by Start.
func locate(content string, findings []Finding) {
	line, lineStart, pos := 1, 0, 0
	for i := range findings {
		for pos < findings[i].Start && pos < len(content) {
			if content[pos] == '\n' {
				line++
				lineStart = pos + 1
			}
			pos++
		}
		findings[i].Line = line
		findings[i].Column = findings[i].Start - lineStart + 1
	}
}
