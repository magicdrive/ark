package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/commandline"
)

// search_code's groupBy counts the matches, from the index, instead of
// listing them; without it the result is the listing it always was.
func TestSearchCode_GroupBy(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"modules/app/main.tf": "resource \"aws_ecs_cluster\" \"this\" {}\nresource \"aws_ecs_service\" \"web\" {}\nresource \"aws_ecs_service\" \"worker\" {}\n",
		"modules/db/main.tf":  "resource \"aws_rds_cluster\" \"aurora\" {}\ndata \"aws_rds_cluster\" \"shared\" {}\n",
		"envs/stg/main.tf":    "resource \"aws_ecs_service\" \"stg\" {}\n",
	})
	_, serveOpt, err := commandline.ServerOptParse("test", []string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	h := NewToolsHandlerWithCache(root, serveOpt.GeneralOption, nil)
	call := func(args map[string]interface{}) (string, bool) {
		t.Helper()
		args["path"] = "."
		return callText(t, h, "search_code", args)
	}

	text, isErr := call(map[string]interface{}{"kind": "resource", "groupBy": "resourceType"})
	if isErr {
		t.Fatal(text)
	}
	want := "5 symbols matching kind=resource, counted by resourceType\n\n     3  aws_ecs_service\n     1  aws_ecs_cluster\n     1  aws_rds_cluster\n"
	if text != want {
		t.Errorf("text:\n%q\nwant\n%q", text, want)
	}

	// Two keys as an array, in JSON.
	text, isErr = call(map[string]interface{}{"namePattern": "aws_rds", "groupBy": []interface{}{"resourceType", "directory"}, "format": "json"})
	if isErr {
		t.Fatal(text)
	}
	var r struct {
		TotalCount int      `json:"totalCount"`
		GroupBy    []string `json:"groupBy"`
		Groups     []struct {
			Name   string `json:"name"`
			Count  int    `json:"count"`
			Groups []struct {
				Name  string `json:"name"`
				Count int    `json:"count"`
			} `json:"groups"`
		} `json:"groups"`
		TotalGroups   int `json:"totalGroups"`
		OmittedGroups int `json:"omittedGroups"`
	}
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	if r.TotalCount != 2 || len(r.Groups) != 2 || r.TotalGroups != 4 || r.OmittedGroups != 0 ||
		r.Groups[0].Name != "aws_rds_cluster" || r.Groups[1].Name != "data.aws_rds_cluster" ||
		len(r.Groups[0].Groups) != 1 || !strings.HasSuffix(r.Groups[0].Groups[0].Name, "db") {
		t.Errorf("json: %+v\n%s", r, text)
	}

	for _, bad := range []interface{}{"type", "kind,kind,kind", []interface{}{"kind", 1}} {
		if text, isErr := call(map[string]interface{}{"groupBy": bad}); !isErr {
			t.Errorf("groupBy %v accepted: %s", bad, text)
		}
	}

	// Without groupBy: the listing.
	text, _ = call(map[string]interface{}{"kind": "resource", "namePattern": "aws_ecs_service", "maxResults": float64(2)})
	if !strings.HasPrefix(text, "Found 3 symbols (showing 2) matching kind=resource, name~\"aws_ecs_service\"\n") {
		t.Errorf("listing:\n%s", text)
	}
}
