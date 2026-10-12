package search_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/golang"
	"github.com/magicdrive/ark/internal/languages/terraform"
	"github.com/magicdrive/ark/internal/search"
	"github.com/magicdrive/ark/internal/symbol"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func inventoryIndex(t *testing.T) *index.RepositoryIndex {
	t.Helper()
	root := writeTree(t, map[string]string{
		"modules/net/main.tf": `resource "aws_vpc" "main" {}
resource "aws_subnet" "a" {}
resource "aws_subnet" "b" {}
data "aws_vpc" "shared" {}
variable "cidr" {}
`,
		"modules/db/main.tf": `resource "aws_rds_cluster" "aurora" {}
resource "aws_rds_cluster_instance" "a" {}
resource "aws_rds_cluster_instance" "b" {}
resource "aws_vpc" "main" {}
ephemeral "random_password" "db" {}
resource "output" "odd" {}
`,
		"modules/db/nested/main.tf": `resource "aws_rds_cluster_parameter_group" "pg" {}
`,
		"envs/prod/main.tf": "module \"net\" {\n  source = \"../../modules/net\"\n}\nmodule \"db\" {\n  source = \"../../modules/db\"\n}\n",
		"envs/stg/main.tf":  "module \"net\" {\n  source = \"../../modules/net\"\n}\nresource \"aws_ecs_service\" \"web\" {}\n",
		"app/main.go":       "package app\n\nfunc Run() {}\n",
	})
	idx, err := index.New(context.Background(), root, []language.Provider{terraform.NewProvider(), golang.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func groups(t *testing.T, idx *index.RepositoryIndex, q search.Query, by string, max int) *search.GroupResult {
	t.Helper()
	keys, err := search.ParseGroupKeys(by)
	if err != nil {
		t.Fatal(err)
	}
	r, err := search.GroupCount(context.Background(), idx, q, keys, max)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func flat(gs []search.Group) map[string]int {
	out := map[string]int{}
	for _, g := range gs {
		out[g.Key] = g.Count
		for _, s := range g.Groups {
			out[g.Key+" / "+s.Key] = s.Count
		}
	}
	return out
}

func sameCounts(t *testing.T, what string, got, want map[string]int) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %v, want %v", what, got, want)
		return
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s: %s = %d, want %d (all: %v)", what, k, got[k], n, got)
		}
	}
}

// Resources count by type with the mode's namespace: a data source and a
// managed resource of the same type are two types, an ephemeral resource is
// its own, and a type that reads as a reference root is its plain type.
func TestGroupCount_ResourceTypes(t *testing.T) {
	idx := inventoryIndex(t)
	r := groups(t, idx, search.Query{Language: "terraform"}, "resourceType", 0)
	sameCounts(t, "language=terraform by resourceType", flat(r.Groups), map[string]int{
		"aws_vpc": 2, "aws_subnet": 2, "aws_rds_cluster_instance": 2, "aws_rds_cluster": 1, "aws_rds_cluster_parameter_group": 1,
		"aws_ecs_service": 1, "output": 1, "data.aws_vpc": 1, "ephemeral.random_password": 1,
		search.NoResourceType: 4, // var.cidr, three module calls
	})
	if r.TotalCount != 16 {
		t.Errorf("TotalCount %d, want 16", r.TotalCount)
	}
	// Another language's symbols have no resource type.
	r = groups(t, idx, search.Query{Language: "go"}, "resourceType", 0)
	sameCounts(t, "language=go by resourceType", flat(r.Groups), map[string]int{search.NoResourceType: 1})
}

// Directory is the module: the same address in two modules counts once in
// each, a nested module and each environment on its own.
func TestGroupCount_Directories(t *testing.T) {
	idx := inventoryIndex(t)
	r := groups(t, idx, search.Query{Kind: symbol.KindResource}, "directory", 0)
	sameCounts(t, "kind=resource by directory", flat(r.Groups), map[string]int{
		filepath.FromSlash("modules/net"): 3, filepath.FromSlash("modules/db"): 6, filepath.FromSlash("modules/db/nested"): 1, filepath.FromSlash("envs/stg"): 1,
	})
	r = groups(t, idx, search.Query{NamePattern: "aws_rds"}, "resourceType,directory", 0)
	sameCounts(t, "name~aws_rds by resourceType then directory", flat(r.Groups), map[string]int{
		"aws_rds_cluster_instance": 2, "aws_rds_cluster_instance / " + filepath.FromSlash("modules/db"): 2,
		"aws_rds_cluster": 1, "aws_rds_cluster / " + filepath.FromSlash("modules/db"): 1,
		"aws_rds_cluster_parameter_group": 1, "aws_rds_cluster_parameter_group / " + filepath.FromSlash("modules/db/nested"): 1,
	})
	r = groups(t, idx, search.Query{Language: "terraform"}, "kind", 0)
	sameCounts(t, "language=terraform by kind", flat(r.Groups), map[string]int{
		"resource": 11, "data_source": 1, "module": 3, "variable": 1,
	})
}

// Groups are ordered by count, largest first, then by key; a line limit keeps
// the largest and states what it leaves out; the counts are of every match.
func TestGroupCount_OrderAndLimit(t *testing.T) {
	idx := inventoryIndex(t)
	r := groups(t, idx, search.Query{Kind: symbol.KindResource}, "resourceType", 3)
	var keys []string
	for _, g := range r.Groups {
		keys = append(keys, fmt.Sprintf("%s=%d", g.Key, g.Count))
	}
	if strings.Join(keys, " ") != "aws_rds_cluster_instance=2 aws_subnet=2 aws_vpc=2" {
		t.Errorf("first three groups %v", keys)
	}
	// 8 types: aws_vpc, aws_subnet, aws_rds_cluster_instance (2 each),
	// aws_rds_cluster, aws_rds_cluster_parameter_group, aws_ecs_service,
	// output, ephemeral.random_password (1 each).
	if r.TotalCount != 11 || r.TotalLines != 8 || r.OmittedLines != 5 {
		t.Errorf("TotalCount %d TotalLines %d OmittedLines %d, want 11 8 5", r.TotalCount, r.TotalLines, r.OmittedLines)
	}
	text := search.FormatGroups(r, search.Query{Kind: symbol.KindResource})
	if !strings.Contains(text, "11 symbols matching kind=resource, counted by resourceType (3 of 8 groups shown") {
		t.Errorf("FormatGroups:\n%s", text)
	}
	// The line limit counts subgroups too.
	r = groups(t, idx, search.Query{Kind: symbol.KindResource}, "directory,resourceType", 4)
	shown := 0
	for _, g := range r.Groups {
		shown += 1 + len(g.Groups)
	}
	if shown != 4 || r.OmittedLines != r.TotalLines-4 {
		t.Errorf("shown %d lines, omitted %d of %d", shown, r.OmittedLines, r.TotalLines)
	}
	for i := 0; i < 5; i++ {
		again := groups(t, idx, search.Query{Kind: symbol.KindResource}, "directory,resourceType", 0)
		if fmt.Sprint(again.Groups) != fmt.Sprint(groups(t, idx, search.Query{Kind: symbol.KindResource}, "directory,resourceType", 0).Groups) {
			t.Fatal("grouping is not deterministic")
		}
	}
}

func TestGroupCount_NoMatch(t *testing.T) {
	r := groups(t, inventoryIndex(t), search.Query{NamePattern: "no_such_type"}, "resourceType", 0)
	if r.TotalCount != 0 || len(r.Groups) != 0 {
		t.Errorf("%+v", r)
	}
	if got := search.FormatGroups(r, search.Query{}); got != "No matches found.\n" {
		t.Errorf("FormatGroups = %q", got)
	}
}

func TestParseGroupKeys(t *testing.T) {
	for _, ok := range []string{"kind", "resourceType", "directory,resourceType", " language , file "} {
		if _, err := search.ParseGroupKeys(ok); err != nil {
			t.Errorf("ParseGroupKeys(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", ",", "type", "kind,kind", "kind,file,language"} {
		if _, err := search.ParseGroupKeys(bad); err == nil {
			t.Errorf("ParseGroupKeys(%q) accepted", bad)
		}
	}
}

// A repository of 600 resources of 60 types counts every one, and the
// complete type table stays small.
func TestGroupCount_LargeRepository(t *testing.T) {
	files := map[string]string{}
	for m := 0; m < 30; m++ {
		var b strings.Builder
		for r := 0; r < 20; r++ {
			fmt.Fprintf(&b, "resource \"aws_type_%02d\" \"r%02d\" {}\n", (m*20+r)%60, r)
		}
		files[fmt.Sprintf("modules/m%02d/main.tf", m)] = b.String()
	}
	idx, err := index.New(context.Background(), writeTree(t, files), []language.Provider{terraform.NewProvider()})
	if err != nil {
		t.Fatal(err)
	}
	r := groups(t, idx, search.Query{Kind: symbol.KindResource}, "resourceType", 100)
	total := 0
	for _, g := range r.Groups {
		total += g.Count
		if g.Count != 10 {
			t.Errorf("%s: %d, want 10", g.Key, g.Count)
		}
	}
	if r.TotalCount != 600 || total != 600 || len(r.Groups) != 60 || r.OmittedLines != 0 {
		t.Errorf("TotalCount %d, sum %d, %d groups, %d omitted; want 600, 600, 60, 0", r.TotalCount, total, len(r.Groups), r.OmittedLines)
	}
	if text := search.FormatGroups(r, search.Query{Kind: symbol.KindResource}); len(text) > 3000 {
		t.Errorf("the table of 60 types is %d bytes", len(text))
	}
	if r := groups(t, idx, search.Query{Kind: symbol.KindResource}, "directory,resourceType", 5000); r.TotalLines-r.OmittedLines > search.MaxGroupLines {
		t.Errorf("%d lines shown, above MaxGroupLines", r.TotalLines-r.OmittedLines)
	}
}

// Grouping counts exactly what Search finds for the same query, each match
// once at every level: the groups add up to the total, a group's subgroups
// to the group, whatever the keys and the filters.
func TestGroupCount_CountsWhatSearchFinds(t *testing.T) {
	idx := inventoryIndex(t)
	for _, q := range []search.Query{
		{},
		{Kind: symbol.KindResource},
		{NamePattern: "aws_vpc"},
		{Language: "terraform"},
		{Kind: symbol.KindFunction},
	} {
		want, err := search.Search(context.Background(), idx, q, 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, by := range []string{"kind", "language", "directory", "file", "resourceType", "directory,resourceType", "resourceType,directory", "kind,file", "language,kind"} {
			r := groups(t, idx, q, by, search.MaxGroupLines)
			if r.TotalCount != want.TotalCount {
				t.Errorf("%+v by %s: TotalCount %d, Search finds %d", q, by, r.TotalCount, want.TotalCount)
			}
			sum, lines := 0, 0
			for _, g := range r.Groups {
				sum += g.Count
				lines += 1 + len(g.Groups)
				if len(g.Groups) == 0 {
					continue
				}
				sub := 0
				for _, s := range g.Groups {
					sub += s.Count
				}
				if sub != g.Count {
					t.Errorf("%+v by %s: %s counts %d, its subgroups %d", q, by, g.Key, g.Count, sub)
				}
			}
			if sum != r.TotalCount || r.OmittedLines != 0 || lines != r.TotalLines {
				t.Errorf("%+v by %s: groups add up to %d of %d; %d lines of %d, %d omitted", q, by, sum, r.TotalCount, lines, r.TotalLines, r.OmittedLines)
			}
		}
	}
}
