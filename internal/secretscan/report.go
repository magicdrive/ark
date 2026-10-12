package secretscan

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/secretpolicy"
	"github.com/magicdrive/ark/internal/secrets"
)

// SchemaVersion is the version of the JSON output; fields are only added
// within a version.
const SchemaVersion = 1

// Suggestion thresholds. They are heuristics that sort files for a reader to
// decide on, not a security assessment.
const (
	// A file other than source code is suggested for .arkignore when at
	// least DenseShare of its non-blank lines, and at least DenseLines
	// lines, have findings: it is mostly secrets, so masking leaves little
	// worth reading. (Source code so masked points at a rule to review.)
	DenseShare = 0.5
	DenseLines = 3
	// A rule is suggested for review when it finds at least CodeFindings
	// spans in source code files: an assignment rule there mostly masks
	// code (token := next()), not secrets.
	CodeFindings = 10
)

// credentialFile reports whether a file's name is one that usually holds
// credentials only — where excluding the file serves better than masking
// some of its values.
func credentialFile(rel string) bool {
	name := strings.ToLower(path.Base(rel))
	switch {
	case name == ".env" || strings.HasPrefix(name, ".env.") && !strings.HasSuffix(name, ".example") && !strings.HasSuffix(name, ".sample") && !strings.HasSuffix(name, ".template"):
		return true
	case name == ".netrc", name == ".pypirc", name == ".npmrc", name == "credentials", strings.HasPrefix(name, "credentials."):
		return true
	case strings.HasPrefix(name, "id_rsa"), strings.HasPrefix(name, "id_dsa"), strings.HasPrefix(name, "id_ecdsa"), strings.HasPrefix(name, "id_ed25519"):
		return true
	}
	switch path.Ext(name) {
	case ".pem", ".key", ".p12", ".pfx", ".jks", ".keystore", ".kdbx", ".tfvars", ".tfstate":
		return true
	}
	return false
}

// codeFile reports whether a file is source code of a language Ark analyzes
// (Terraform aside: its assignments are configuration).
func codeFile(rel string) bool {
	d, ok := languages.Registry().DetectByFilename(rel)
	return ok && d.Language != language.Language("terraform")
}

// Suggestions sorts a scan's files and rules into what to do about them.
type Suggestions struct {
	// Exclude are files to add to .arkignore.
	Exclude []FileSuggestion `json:"exclude"`
	// MaskOnly are files whose findings masking handles: a few values in
	// a file worth reading.
	MaskOnly []FileSuggestion `json:"maskOnly"`
	// ReviewRules are rules that mostly mask code: candidates to disable
	// in the secret rules configuration, after checking their findings.
	ReviewRules []RuleSuggestion `json:"reviewRules"`
}

// FileSuggestion is one file and why.
type FileSuggestion struct {
	Path     string `json:"path"`
	Findings int    `json:"findings"`
	Reason   string `json:"reason"`
	Excluded bool   `json:"excluded,omitempty"` // already excluded
}

// RuleSuggestion is one rule and where it finds code.
type RuleSuggestion struct {
	RuleID       string   `json:"ruleId"`
	CodeFindings int      `json:"codeFindings"`
	Examples     []string `json:"examples"` // path:line, at most 5
}

// Suggest sorts the report's files and rules.
func (r *Report) Suggest() Suggestions {
	var s Suggestions
	perFile := map[string]int{}
	for _, f := range r.Findings {
		perFile[f.Path]++
	}
	paths := make([]string, 0, len(perFile))
	for p := range perFile {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		st := r.files[p]
		fsug := FileSuggestion{Path: p, Findings: perFile[p], Excluded: st != nil && st.excluded}
		switch {
		case credentialFile(p):
			fsug.Reason = "a credentials file by its name"
			s.Exclude = append(s.Exclude, fsug)
		case st != nil && !codeFile(p) && st.findingLines >= DenseLines && float64(st.findingLines) >= DenseShare*float64(st.lines):
			fsug.Reason = fmt.Sprintf("%d of %d non-blank lines have findings", st.findingLines, st.lines)
			s.Exclude = append(s.Exclude, fsug)
		default:
			fsug.Reason = "a few values in a file worth reading: masking handles them"
			s.MaskOnly = append(s.MaskOnly, fsug)
		}
	}
	byRule := map[string]*RuleSuggestion{}
	for _, f := range r.Findings {
		if f.Kind != secrets.KindAssignment || !codeFile(f.Path) {
			continue
		}
		rs := byRule[f.RuleID]
		if rs == nil {
			rs = &RuleSuggestion{RuleID: f.RuleID}
			byRule[f.RuleID] = rs
		}
		rs.CodeFindings++
		if len(rs.Examples) < 5 {
			rs.Examples = append(rs.Examples, fmt.Sprintf("%s:%d", f.Path, f.Line))
		}
	}
	for _, rs := range byRule {
		if rs.CodeFindings >= CodeFindings {
			s.ReviewRules = append(s.ReviewRules, *rs)
		}
	}
	sort.Slice(s.ReviewRules, func(i, j int) bool {
		if s.ReviewRules[i].CodeFindings != s.ReviewRules[j].CodeFindings {
			return s.ReviewRules[i].CodeFindings > s.ReviewRules[j].CodeFindings
		}
		return s.ReviewRules[i].RuleID < s.ReviewRules[j].RuleID
	})
	return s
}

// configName is the report's configuration file, or "".
func (r *Report) configName() string {
	if r.Config == nil {
		return ""
	}
	return r.Config.File
}

// jsonReport is the JSON output (SchemaVersion).
type jsonReport struct {
	Version        int          `json:"version"`
	Root           string       `json:"root"`
	Config         *string      `json:"config"`
	IncludeIgnored bool         `json:"includeIgnored"`
	Summary        jsonSummary  `json:"summary"`
	Findings       []Finding    `json:"findings"`
	Errors         []FileError  `json:"errors"`
	Unfollowed     []string     `json:"unfollowedSymlinks"`
	Suggestions    *Suggestions `json:"suggestions,omitempty"`
}

type jsonSummary struct {
	Findings     int            `json:"findings"`
	Files        int            `json:"files"`
	ScannedFiles int            `json:"scannedFiles"`
	SkippedFiles int            `json:"skippedFiles"`
	ByRule       map[string]int `json:"byRule"`
}

// WriteJSON writes the report as JSON, with suggestions when suggest.
func (r *Report) WriteJSON(w io.Writer, suggest bool) error {
	out := jsonReport{
		Version: SchemaVersion, Root: r.Root, IncludeIgnored: r.IncludeIgnored,
		Summary:  jsonSummary{Findings: len(r.Findings), Files: r.FilesWithFindings(), ScannedFiles: r.Scanned, SkippedFiles: r.Skipped, ByRule: r.ByRule()},
		Findings: r.Findings, Errors: r.Errors,
	}
	if out.Findings == nil {
		out.Findings = []Finding{}
	}
	if out.Errors == nil {
		out.Errors = []FileError{}
	}
	out.Unfollowed = r.Unfollowed
	if out.Unfollowed == nil {
		out.Unfollowed = []string{}
	}
	if name := r.configName(); name != "" {
		out.Config = &name
	}
	if suggest {
		s := r.Suggest()
		out.Suggestions = &s
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// WriteText writes the report for a reader, with suggestions when suggest.
func (r *Report) WriteText(w io.Writer, suggest bool) {
	fmt.Fprintf(w, "Secret scan of %s\n", r.Root)
	if name := r.configName(); name != "" {
		fmt.Fprintf(w, "Rules: Ark's recommended rules adjusted by %s\n", name)
	} else {
		fmt.Fprintln(w, "Rules: Ark's recommended rules (no secret rules configuration)")
	}
	fmt.Fprintln(w)
	used := map[string]bool{}
	for _, f := range r.Findings {
		excluded := ""
		if f.Excluded {
			excluded = "  [excluded by ignore rules]"
		}
		fmt.Fprintf(w, "%s:%d:%d  %s  (%s %s, %d bytes)%s\n", f.Path, f.Line, f.Column, f.RuleID, f.Source, f.Kind, f.Length, excluded)
		used[f.RuleID] = true
	}
	for _, e := range r.Errors {
		fmt.Fprintf(w, "%s: not scanned: %s\n", e.Path, e.Message)
	}
	for _, l := range r.Unfollowed {
		fmt.Fprintf(w, "%s: not scanned: a symlink leading outside the directory or nowhere (the dump follows one leading outside; the MCP server only with --allow-external-symlinks on)\n", l)
	}
	if len(used) > 0 {
		fmt.Fprintln(w, "\nRules found:")
		for _, rule := range r.Rules {
			if used[rule.ID] {
				fmt.Fprintf(w, "  %-32s %s\n", rule.ID, rule.Description)
			}
		}
	}
	fmt.Fprintf(w, "\n%s in %s (%s scanned, %d skipped as binary, non-UTF-8 or too large", plural(len(r.Findings), "finding"), plural(r.FilesWithFindings(), "file"), plural(r.Scanned, "file"), r.Skipped)
	if len(r.Errors) > 0 {
		fmt.Fprintf(w, ", %d not scanned", len(r.Errors))
	}
	fmt.Fprintln(w, ").")
	fmt.Fprintln(w, "Values are not shown. No finding is no proof of no secret: the scan finds what Ark's masking masks.")
	if suggest {
		r.writeSuggestions(w)
	}
}

func (r *Report) writeSuggestions(w io.Writer) {
	s := r.Suggest()
	fmt.Fprintln(w, "\nSuggestions (heuristics to help you decide; nothing is changed):")
	if len(s.Exclude) > 0 {
		fmt.Fprintln(w, "\n  Exclude these files: add to .arkignore")
		for _, f := range s.Exclude {
			note := ""
			if f.Excluded {
				note = " (already excluded)"
			}
			fmt.Fprintf(w, "    %-40s # %s; %s%s\n", "/"+f.Path, f.Reason, plural(f.Findings, "finding"), note)
		}
	}
	if len(s.MaskOnly) > 0 {
		fmt.Fprintln(w, "\n  Masking suffices: a few values in files worth reading")
		for _, f := range s.MaskOnly {
			note := ""
			if f.Excluded {
				note = ", already excluded"
			}
			fmt.Fprintf(w, "    %s (%s%s)\n", f.Path, plural(f.Findings, "finding"), note)
		}
	}
	if len(s.ReviewRules) > 0 {
		fmt.Fprintln(w, "\n  Review these rules: they mostly mask source code here")
		for _, rs := range s.ReviewRules {
			fmt.Fprintf(w, "    %s: %s in source code (e.g. %s)\n", rs.RuleID, plural(rs.CodeFindings, "finding"), strings.Join(rs.Examples, ", "))
		}
		fmt.Fprintln(w, "    If they mask no secret, disable them in .ark-secrets-rules.toml:")
		for _, rs := range s.ReviewRules {
			fmt.Fprintf(w, "      [[override]]\n      id = %q\n      enabled = false\n      reason = \"...\"\n", rs.RuleID)
		}
	}
	if len(s.Exclude)+len(s.MaskOnly)+len(s.ReviewRules) == 0 {
		fmt.Fprintln(w, "  None: no findings.")
	}
	fmt.Fprintln(w, "\n  .arkignore keeps files out of Ark's output only: git, editors and agents with other tools still read them.")
}

// WriteRules lists the rules of a configuration.
func WriteRules(w io.Writer, loaded *secretpolicy.Loaded, asJSON bool) error {
	rules := loaded.Policy.Rules()
	if asJSON {
		type jsonRule struct {
			ID          string             `json:"id"`
			Description string             `json:"description"`
			Kind        secrets.RuleKind   `json:"kind"`
			Source      secrets.RuleSource `json:"source"`
			Enabled     bool               `json:"enabled"`
			Reason      string             `json:"reason,omitempty"`
		}
		out := struct {
			Version int        `json:"version"`
			Config  *string    `json:"config"`
			Rules   []jsonRule `json:"rules"`
		}{Version: SchemaVersion, Rules: []jsonRule{}}
		if loaded.File != "" {
			out.Config = &loaded.File
		}
		for _, r := range rules {
			out.Rules = append(out.Rules, jsonRule{r.ID, r.Description, r.Kind, r.Source, r.Enabled, r.Reason})
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	if name := loaded.File; name != "" {
		fmt.Fprintf(w, "Rules of Ark's recommended set adjusted by %s:\n\n", name)
	} else {
		fmt.Fprintln(w, "Rules of Ark's recommended set (no secret rules configuration):")
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "%-32s %-8s %-11s %-9s %s\n", "ID", "SOURCE", "KIND", "STATE", "DESCRIPTION")
	for _, r := range rules {
		state := "enabled"
		desc := r.Description
		if !r.Enabled {
			state = "disabled"
			desc += " — disabled: " + r.Reason
		}
		fmt.Fprintf(w, "%-32s %-8s %-11s %-9s %s\n", r.ID, r.Source, r.Kind, state, desc)
	}
	if weak := loaded.Spec.Weakening(); len(weak) > 0 {
		fmt.Fprintf(w, "\nThe dump and this scan apply every entry. The MCP server applies the %d that disable masking rules only when started with --secret-rule-overrides on.\n", len(weak))
	}
	return nil
}

// plural is "1 finding", "2 findings".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
