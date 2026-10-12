// Package secretpolicy reads a repository's secret masking configuration —
// .ark-secrets-rules.toml or ark-secrets-rules.toml at its root — and builds
// the masking policy from it (secrets.NewPolicy). The CLI dump, the MCP
// server and `ark secrets-scan` load it here, so they read one file the same
// way and give it one meaning.
//
// The configuration adjusts Ark's recommended rules: it adds pattern and
// assignment rules and disables built-in rules by their stable IDs
// (docs/secret-rules.md). Without a file the policy is the recommended one.
// A file that cannot be read, parsed or compiled is an error, never the
// recommended policy in its place: rules a user added are not dropped
// silently.
//
// Disabling a rule weakens masking. A caller that must not let the
// repository weaken it (the MCP server without its operator's
// --secret-rule-overrides on) loads with Options.AllowWeakening false: the
// disabling entries are then left out and reported (Loaded.Ignored), and
// everything else applies.
package secretpolicy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/magicdrive/ark/internal/secrets"
)

// The configuration file names, both read at the repository root. Both
// present is an error: which one applies would be a guess.
const (
	FileName    = ".ark-secrets-rules.toml"
	AltFileName = "ark-secrets-rules.toml"
)

// MaxFileSize bounds the configuration file.
const MaxFileSize = 1 << 20

// Version is the configuration schema version this Ark reads.
const Version = 1

// Options are how a configuration is applied.
type Options struct {
	// AllowWeakening applies the entries that disable built-in masking
	// rules. Without it they are left out and listed in Loaded.Ignored.
	AllowWeakening bool
}

// Loaded is a configuration read and compiled.
type Loaded struct {
	// File is the configuration file read ("" when there is none).
	File string
	// Fingerprint identifies the configuration's bytes and the options it
	// was compiled with: equal fingerprints compile to equal policies.
	Fingerprint string
	// Spec is the whole configuration, Ignored entries included.
	Spec secrets.Spec
	// Ignored are the disabling entries not applied (AllowWeakening off).
	Ignored []secrets.Disable
	// Policy is the compiled policy.
	Policy *secrets.Policy
}

// Error is a configuration problem: the file, the line when known, and what
// is wrong — never a value the configuration's patterns would match.
type Error struct {
	File string
	Line int // 0: not known
	Msg  string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Msg)
}

// None is the configuration of a repository with no configuration file.
func None(opt Options) *Loaded {
	return &Loaded{Fingerprint: Fingerprint("", nil, opt), Policy: secrets.Recommended()}
}

// Fingerprint identifies a configuration file's name and bytes (Read) and
// the options: what Build compiles them to.
func Fingerprint(name string, data []byte, opt Options) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%t\x00", name, opt.AllowWeakening)
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// file is the configuration's TOML document.
type file struct {
	Version    *int         `toml:"version"`
	Preset     string       `toml:"preset"`
	Override   []override   `toml:"override"`
	Rule       []rule       `toml:"rule"`
	Assignment []assignment `toml:"assignment"`
}

type override struct {
	ID      string `toml:"id"`
	Enabled *bool  `toml:"enabled"`
	Reason  string `toml:"reason"`
}

type rule struct {
	ID           string `toml:"id"`
	Description  string `toml:"description"`
	Pattern      string `toml:"pattern"`
	Mask         string `toml:"mask"`
	CaptureGroup *int   `toml:"capture_group"`
}

type assignment struct {
	ID          string   `toml:"id"`
	Description string   `toml:"description"`
	Keys        []string `toml:"keys"`
}

// Parse reads a configuration file's bytes (name is its name, for errors)
// into a specification, checking its schema.
func Parse(name string, data []byte) (secrets.Spec, error) {
	fail := func(line int, format string, args ...any) error {
		return &Error{File: name, Line: line, Msg: fmt.Sprintf(format, args...)}
	}
	if len(data) > MaxFileSize {
		return secrets.Spec{}, fail(0, "larger than %d bytes", MaxFileSize)
	}
	var f file
	md, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&f)
	if err != nil {
		var pe toml.ParseError
		if errors.As(err, &pe) {
			// The parser's message names a character or a type, which the
			// recommended rules still mask if it looks like a secret.
			return secrets.Spec{}, fail(pe.Position.Line, "invalid TOML: %s", secrets.MaskAll(pe.Message))
		}
		return secrets.Spec{}, fail(0, "%s", secrets.MaskAll(err.Error()))
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, len(und))
		for i, k := range und {
			keys[i] = k.String()
		}
		sort.Strings(keys)
		return secrets.Spec{}, fail(0, "unknown field %s (fields: version, preset, [[override]] id/enabled/reason, [[rule]] id/description/pattern/mask/capture_group, [[assignment]] id/description/keys)", strings.Join(keys, ", "))
	}

	var errs []error
	switch {
	case f.Version == nil:
		errs = append(errs, fail(0, "version = %d is required", Version))
	case *f.Version != Version:
		errs = append(errs, fail(0, "version %d: this Ark reads version %d", *f.Version, Version))
	}
	if f.Preset != "" && f.Preset != "recommended" {
		errs = append(errs, fail(0, "preset %q: the only preset is \"recommended\"", f.Preset))
	}

	var spec secrets.Spec
	for i, o := range f.Override {
		what := fmt.Sprintf("[[override]] #%d (id %q)", i+1, o.ID)
		switch {
		case o.ID == "":
			errs = append(errs, fail(0, "[[override]] #%d: id is required", i+1))
		case o.Enabled == nil:
			errs = append(errs, fail(0, "%s: enabled = false is required (an override disables a built-in rule)", what))
		case *o.Enabled:
			errs = append(errs, fail(0, "%s: enabled = true changes nothing; remove the override", what))
		case strings.TrimSpace(o.Reason) == "":
			errs = append(errs, fail(0, "%s: reason is required: say why the rule is disabled", what))
		default:
			spec.Disable = append(spec.Disable, secrets.Disable{ID: o.ID, Reason: o.Reason})
		}
	}
	for i, r := range f.Rule {
		what := fmt.Sprintf("[[rule]] #%d (id %q)", i+1, r.ID)
		group := 0
		switch {
		case r.ID == "":
			errs = append(errs, fail(0, "[[rule]] #%d: id is required", i+1))
			continue
		case r.Pattern == "":
			errs = append(errs, fail(0, "%s: pattern is required", what))
			continue
		case r.Mask == "" || r.Mask == "match":
			if r.CaptureGroup != nil {
				errs = append(errs, fail(0, "%s: capture_group needs mask = \"group\"", what))
				continue
			}
		case r.Mask == "group":
			if r.CaptureGroup == nil || *r.CaptureGroup < 1 {
				errs = append(errs, fail(0, "%s: mask = \"group\" needs capture_group = 1 or more", what))
				continue
			}
			group = *r.CaptureGroup
		default:
			errs = append(errs, fail(0, "%s: mask %q: \"match\" or \"group\"", what, r.Mask))
			continue
		}
		spec.Patterns = append(spec.Patterns, secrets.PatternSpec{ID: r.ID, Description: r.Description, Pattern: r.Pattern, Group: group})
	}
	for i, a := range f.Assignment {
		if a.ID == "" {
			errs = append(errs, fail(0, "[[assignment]] #%d: id is required", i+1))
			continue
		}
		spec.Assignments = append(spec.Assignments, secrets.AssignmentSpec{ID: a.ID, Description: a.Description, Keys: a.Keys})
	}
	if len(errs) > 0 {
		return secrets.Spec{}, errors.Join(errs...)
	}
	return spec, nil
}

// Build parses and compiles a configuration file's bytes.
func Build(name string, data []byte, opt Options) (*Loaded, error) {
	spec, err := Parse(name, data)
	if err != nil {
		return nil, err
	}
	l := &Loaded{File: name, Fingerprint: Fingerprint(name, data, opt), Spec: spec}
	apply := spec
	if !opt.AllowWeakening {
		l.Ignored = spec.Weakening()
		apply = spec.WithoutWeakening()
	}
	// The whole specification compiles first, so a mistake in an ignored
	// entry (an unknown rule ID) is reported, not hidden by the options.
	p, err := secrets.NewPolicy(spec)
	if err != nil {
		return nil, &Error{File: name, Msg: err.Error()}
	}
	if len(l.Ignored) > 0 {
		if p, err = secrets.NewPolicy(apply); err != nil {
			return nil, &Error{File: name, Msg: err.Error()}
		}
	}
	l.Policy = p
	return l, nil
}

// IgnoredIDs returns the IDs of the ignored entries, for a message.
func (l *Loaded) IgnoredIDs() []string {
	ids := make([]string, len(l.Ignored))
	for i, d := range l.Ignored {
		ids[i] = d.ID
	}
	return ids
}
