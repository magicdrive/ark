package language_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

var at = source.Location{File: "src/a.ts"}

func TestValidateModuleBindings_Valid(t *testing.T) {
	mod := language.ModuleSpec{Specifier: "./user", Candidates: []language.ModuleCandidate{
		{File: "src/user.ts"}, {File: "src/user.tsx", Priority: 1}, {File: "src/user/index.ts", Priority: 2},
	}}
	ex := language.Extraction{
		Bindings: []language.BindingDraft{
			{Local: "U", Kind: language.BindingNamed, Imported: "User", Module: mod, Location: at},
			{Local: "z", Kind: language.BindingNamespace, Module: language.ModuleSpec{Specifier: "zod"}, Location: at},
		},
		Exports: []language.ExportDraft{
			{Kind: language.ExportLocal, Exported: "default", Local: "User", Location: at},
			{Kind: language.ExportFrom, Exported: "A", Local: "B", Module: mod, Location: at},
			{Kind: language.ExportAll, Module: mod, Except: []string{"default"}, Location: at},
			{Kind: language.ExportNamespace, Exported: "ns", Module: mod, Location: at},
		},
	}
	if errs := language.ValidateModuleBindings(ex); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestValidateModuleBindings_Invalid(t *testing.T) {
	cases := map[string]language.Extraction{
		"empty local":            {Bindings: []language.BindingDraft{{Kind: language.BindingNamespace, Module: language.ModuleSpec{Specifier: "m"}, Location: at}}},
		"named without imported": {Bindings: []language.BindingDraft{{Local: "x", Kind: language.BindingNamed, Module: language.ModuleSpec{Specifier: "m"}, Location: at}}},
		"unknown kind":           {Bindings: []language.BindingDraft{{Local: "x", Kind: "weird", Module: language.ModuleSpec{Specifier: "m"}, Location: at}}},
		"root escape": {Bindings: []language.BindingDraft{{Local: "x", Kind: language.BindingNamespace, Location: at,
			Module: language.ModuleSpec{Specifier: "../../x", Candidates: []language.ModuleCandidate{{File: "../x.ts"}}}}}},
		"absolute": {Bindings: []language.BindingDraft{{Local: "x", Kind: language.BindingNamespace, Location: at,
			Module: language.ModuleSpec{Specifier: "/x", Candidates: []language.ModuleCandidate{{File: "/x.ts"}}}}}},
		"unclean": {Bindings: []language.BindingDraft{{Local: "x", Kind: language.BindingNamespace, Location: at,
			Module: language.ModuleSpec{Specifier: "./a/../b", Candidates: []language.ModuleCandidate{{File: "src/a/../b.ts"}}}}}},
		"unsorted": {Bindings: []language.BindingDraft{{Local: "x", Kind: language.BindingNamespace, Location: at,
			Module: language.ModuleSpec{Specifier: "./b", Candidates: []language.ModuleCandidate{{File: "b.tsx", Priority: 1}, {File: "b.ts"}}}}}},
		"duplicate": {Bindings: []language.BindingDraft{{Local: "x", Kind: language.BindingNamespace, Location: at,
			Module: language.ModuleSpec{Specifier: "./b", Candidates: []language.ModuleCandidate{{File: "b.ts"}, {File: "b.ts"}}}}}},
		"export-all with name":     {Exports: []language.ExportDraft{{Kind: language.ExportAll, Exported: "x", Module: language.ModuleSpec{Specifier: "m"}, Location: at}}},
		"local export with module": {Exports: []language.ExportDraft{{Kind: language.ExportLocal, Exported: "x", Local: "x", Module: language.ModuleSpec{Specifier: "m"}, Location: at}}},
		"from without module":      {Exports: []language.ExportDraft{{Kind: language.ExportFrom, Exported: "x", Local: "x", Location: at}}},
		"except on from":           {Exports: []language.ExportDraft{{Kind: language.ExportFrom, Exported: "x", Local: "x", Except: []string{"d"}, Module: language.ModuleSpec{Specifier: "m"}, Location: at}}},
	}
	for name, ex := range cases {
		if errs := language.ValidateModuleBindings(ex); len(errs) == 0 {
			t.Errorf("%s: expected a contract violation", name)
		}
	}
}
