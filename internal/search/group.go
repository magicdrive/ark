package search

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
)

// GroupKey is a property symbols are counted by (Group).
type GroupKey string

const (
	GroupKind      GroupKey = "kind"
	GroupLanguage  GroupKey = "language"
	GroupDirectory GroupKey = "directory" // the file's directory: a Terraform module
	GroupFile      GroupKey = "file"
	// GroupResourceType is the type of the instance a declaration declares,
	// as its language's provider states it (language.MapHinter.DeclaredType):
	// a Terraform resource's type with the mode's namespace — "aws_vpc"
	// (managed), "data.aws_vpc" (data source), "ephemeral.random_password"
	// (ephemeral). A symbol that declares none is NoResourceType. It is the
	// type as declared, not a classification by cloud service.
	GroupResourceType GroupKey = "resourceType"
)

// NoResourceType is the GroupResourceType key of a symbol that declares no
// resource, data source or ephemeral resource.
const NoResourceType = "(none)"

// GroupKeys are the valid keys, in documentation order.
var GroupKeys = []GroupKey{GroupKind, GroupLanguage, GroupDirectory, GroupFile, GroupResourceType}

// MaxGroupLines bounds the lines of a grouped result, whatever is asked.
const MaxGroupLines = 1000

// Group is the count of the matches whose key is Key, with its subgroups by
// the next key.
type Group struct {
	Key    string  `json:"name"`
	Count  int     `json:"count"`
	Groups []Group `json:"groups,omitempty"`
}

// GroupResult counts every match of a query by one or two keys.
type GroupResult struct {
	TotalCount int        `json:"totalCount"` // matches counted
	By         []GroupKey `json:"groupBy"`
	Groups     []Group    `json:"groups"`
	// TotalLines is the number of group lines (groups and subgroups);
	// OmittedLines those left out by the line limit.
	TotalLines   int `json:"totalGroups"`
	OmittedLines int `json:"omittedGroups"`
}

// ParseGroupKeys parses one or two comma-separated group keys.
func ParseGroupKeys(s string) ([]GroupKey, error) {
	var keys []GroupKey
	for _, f := range strings.Split(s, ",") {
		k := GroupKey(strings.TrimSpace(f))
		if k == "" {
			continue
		}
		valid := false
		for _, v := range GroupKeys {
			valid = valid || k == v
		}
		if !valid {
			return nil, fmt.Errorf("unknown groupBy key %q (one or two of kind, language, directory, file, resourceType)", k)
		}
		for _, prev := range keys {
			if prev == k {
				return nil, fmt.Errorf("groupBy key %q given twice", k)
			}
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 || len(keys) > 2 {
		return nil, fmt.Errorf("groupBy takes one or two keys, got %q", s)
	}
	return keys, nil
}

// GroupCount counts every symbol of idx that q matches (as Search would find
// them, from the index alone) by keys, groups ordered by count, largest
// first, then by key. At most maxLines group lines (subgroups included) are
// returned, never more than MaxGroupLines; counts are always of every match.
func GroupCount(_ context.Context, idx *index.RepositoryIndex, q Query, keys []GroupKey, maxLines int) (*GroupResult, error) {
	if len(keys) == 0 || len(keys) > 2 {
		return nil, fmt.Errorf("groupBy takes one or two keys")
	}
	if maxLines <= 0 {
		maxLines = 50
	}
	if maxLines > MaxGroupLines {
		maxLines = MaxGroupLines
	}
	matches := collect(idx, q)
	groups := groupBy(matches, keys)
	res := &GroupResult{TotalCount: len(matches), By: keys}
	budget := maxLines
	for _, g := range groups {
		res.TotalLines += 1 + len(g.Groups)
		if budget == 0 {
			continue
		}
		budget--
		sub := g.Groups
		if len(sub) > budget {
			sub = sub[:budget]
		}
		budget -= len(sub)
		res.Groups = append(res.Groups, Group{Key: g.Key, Count: g.Count, Groups: sub})
	}
	shown := 0
	for _, g := range res.Groups {
		shown += 1 + len(g.Groups)
	}
	res.OmittedLines = res.TotalLines - shown
	return res, nil
}

func groupBy(matches []Match, keys []GroupKey) []Group {
	byKey := map[string][]Match{}
	for _, m := range matches {
		k := groupKey(m, keys[0])
		byKey[k] = append(byKey[k], m)
	}
	groups := make([]Group, 0, len(byKey))
	for k, ms := range byKey {
		g := Group{Key: k, Count: len(ms)}
		if len(keys) > 1 {
			g.Groups = groupBy(ms, keys[1:])
		}
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Key < groups[j].Key
	})
	return groups
}

func groupKey(m Match, key GroupKey) string {
	s := m.Symbol
	switch key {
	case GroupKind:
		return string(s.Kind)
	case GroupLanguage:
		return s.Language
	case GroupDirectory:
		return filepath.Dir(string(m.File))
	case GroupFile:
		return string(m.File)
	case GroupResourceType:
		// The type the language's provider states the declaration declares
		// (language.MapHinter.DeclaredType).
		if p, ok := languages.Registry().Provider(language.Language(s.Language)); ok {
			if h, ok := p.(language.MapHinter); ok {
				if typ := h.DeclaredType(s.Kind, s.Name); typ != "" {
					return typ
				}
			}
		}
		return NoResourceType
	}
	return ""
}

// FormatGroups returns a grouped result as text: one line per group, its
// count first, subgroups indented below their group.
func FormatGroups(r *GroupResult, q Query) string {
	if r.TotalCount == 0 {
		return "No matches found.\n"
	}
	keys := make([]string, len(r.By))
	for i, k := range r.By {
		keys[i] = string(k)
	}
	var sb strings.Builder
	desc := buildQueryDesc(q)
	if desc == "" {
		desc = "the query"
	}
	fmt.Fprintf(&sb, "%d symbols matching %s, counted by %s", r.TotalCount, desc, strings.Join(keys, " then "))
	if r.OmittedLines > 0 {
		fmt.Fprintf(&sb, " (%d of %d groups shown; raise maxResults or narrow the query)", r.TotalLines-r.OmittedLines, r.TotalLines)
	}
	sb.WriteString("\n\n")
	for _, g := range r.Groups {
		fmt.Fprintf(&sb, "%6d  %s\n", g.Count, g.Key)
		for _, s := range g.Groups {
			fmt.Fprintf(&sb, "  %6d  %s\n", s.Count, s.Key)
		}
	}
	return sb.String()
}
