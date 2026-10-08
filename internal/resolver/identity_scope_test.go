package resolver_test

import (
	"testing"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Synthetic, provider-independent evidence for the identity-scope extensions:
// FileIndex.IdentityOnly, Reference.IdentityInRepository and
// Symbol.MemberScope / MembersOutside. The language name "cfg" is arbitrary.

func cfgSym(file, name, qualified string, kind symbol.SymbolKind) symbol.Symbol {
	return symbol.Symbol{
		ID:        symbol.NewSymbolID("cfg", file, kind, qualified),
		Name:      name,
		Qualified: qualified,
		Kind:      kind,
		Language:  "cfg",
		Location:  source.Location{File: source.FileID(file)},
	}
}

func cfgRef(file, name string) reference.Reference {
	return reference.Reference{
		ID:       reference.ReferenceID(file + "#" + name),
		Name:     name,
		Kind:     reference.KindValueReference,
		Language: "cfg",
		Location: source.Location{File: source.FileID(file)},
	}
}

func resolveIn(fis []resolver.FileIndex, file string, ref reference.Reference) resolver.Resolution {
	r := resolver.New(fis)
	for _, fi := range fis {
		if string(fi.FileID) == file {
			return r.ResolveReference(ref, fi)
		}
	}
	panic("no file " + file)
}

func hasEvidence(res resolver.Resolution, k resolver.EvidenceKind) bool {
	for _, ev := range res.Evidence {
		if ev.Kind == k {
			return true
		}
	}
	return false
}

// An identity-only symbol is reached by no name-based stage: not by same
// directory, unique name or qualified-name suffix, from any language.
func TestIdentityOnly_NotReachableByName(t *testing.T) {
	vpc := cfgSym("mod/main.tf", "aws_vpc.main", "mod/aws_vpc.main", symbol.KindResource)
	fis := []resolver.FileIndex{
		{FileID: "mod/main.tf", Language: "cfg", Symbols: []symbol.Symbol{vpc}, IdentityOnly: true},
		{FileID: "mod/app.py", Language: "python"},
	}
	for _, name := range []string{"aws_vpc.main", "main"} {
		ref := cfgRef("mod/app.py", name)
		ref.Language = "python"
		ref.Kind = reference.KindCall
		res := resolveIn(fis, "mod/app.py", ref)
		if res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 {
			t.Errorf("%q reached an identity-only symbol by name: %s %v", name, res.Confidence, res.Evidence)
		}
	}
}

func TestIdentityOnly_ReferenceWithoutIdentityIsUnresolved(t *testing.T) {
	fis := []resolver.FileIndex{
		{FileID: "a/main.tf", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{
			cfgSym("a/main.tf", "var.x", "a/var.x", symbol.KindVariable),
		}},
		{FileID: "a/other.go", Language: "cfg", Symbols: []symbol.Symbol{
			cfgSym("a/other.go", "var.x", "var.x", symbol.KindVariable),
		}},
	}
	res := resolveIn(fis, "a/main.tf", cfgRef("a/main.tf", "var.x"))
	if res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 || !hasEvidence(res, resolver.EvidenceIdentityOnly) {
		t.Errorf("got %s %v, want Unresolved by identity_only", res.Confidence, res.Evidence)
	}
}

// R0 reaches every declaration of an identity-only file, but still only
// type-like declarations of any other file.
func TestIdentityOnly_R0FindsAnyDeclarationKind(t *testing.T) {
	fis := []resolver.FileIndex{
		{FileID: "a/main.tf", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{
			cfgSym("a/main.tf", "var.x", "a/var.x", symbol.KindVariable),
		}},
		{FileID: "b/lib.cfg", Language: "cfg", Symbols: []symbol.Symbol{
			cfgSym("b/lib.cfg", "y", "b/y", symbol.KindVariable),
		}},
	}
	ref := cfgRef("a/main.tf", "var.x")
	ref.NameQualified = "a/var.x"
	ref.IdentityInRepository = true
	if res := resolveIn(fis, "a/main.tf", ref); res.Confidence != resolver.ConfidenceExact || !res.HasUniqueTarget() {
		t.Errorf("identity-only declaration: %s %v", res.Confidence, res.Evidence)
	}
	other := cfgRef("a/main.tf", "y")
	other.NameQualified = "b/y"
	other.IdentityInRepository = true
	if res := resolveIn(fis, "a/main.tf", other); res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 {
		t.Errorf("a non-type declaration of an ordinary file was matched by identity: %s", res.Confidence)
	}
}

func TestIdentityInRepository_MissIsNotOutside(t *testing.T) {
	fis := []resolver.FileIndex{{FileID: "a/main.tf", Language: "cfg", IdentityOnly: true}}
	ref := cfgRef("a/main.tf", "aws_vpc.main")
	ref.NameQualified = "a/aws_vpc.main"

	if res := resolveIn(fis, "a/main.tf", ref); !res.OutsideRepository {
		t.Error("without IdentityInRepository a qualified-identity miss is outside the repository (unchanged R0)")
	}
	ref.IdentityInRepository = true
	res := resolveIn(fis, "a/main.tf", ref)
	if res.OutsideRepository || res.Confidence != resolver.ConfidenceUnresolved {
		t.Errorf("an identity of a repository scope with no declaration: outside=%t %s", res.OutsideRepository, res.Confidence)
	}
}

func TestIdentity_DuplicateDeclarationsAreCandidates(t *testing.T) {
	fis := []resolver.FileIndex{
		{FileID: "a/x.tf", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{cfgSym("a/x.tf", "n.x", "a/n.x", symbol.KindResource)}},
		{FileID: "a/y.tf", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{cfgSym("a/y.tf", "n.x", "a/n.x", symbol.KindResource)}},
	}
	ref := cfgRef("a/x.tf", "n.x")
	ref.NameQualified, ref.IdentityInRepository = "a/n.x", true
	res := resolveIn(fis, "a/x.tf", ref)
	if res.Confidence != resolver.ConfidenceCandidate || len(res.Candidates) != 2 || res.HasUniqueTarget() {
		t.Errorf("duplicates: %s with %d candidates", res.Confidence, len(res.Candidates))
	}
}

func memberScopeFixture(call symbol.Symbol) []resolver.FileIndex {
	return []resolver.FileIndex{
		{FileID: "main.tf", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{call}},
		{FileID: "child/out.tf", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{
			cfgSym("child/out.tf", "output.id", "child/output.id", symbol.KindOutput),
		}},
		{FileID: "other/out.tf", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{
			cfgSym("other/out.tf", "output.id", "other/output.id", symbol.KindOutput),
		}},
	}
}

func memberRef(name string) reference.Reference {
	ref := cfgRef("main.tf", name)
	ref.ReceiverExpr = "module.m"
	ref.ReceiverTypeQualified = "module.m"
	ref.IdentityInRepository = true
	return ref
}

func TestMemberScope_ResolvesToTheStatedDeclaration(t *testing.T) {
	call := cfgSym("main.tf", "module.m", "module.m", symbol.KindModule)
	call.MemberScope = "child/output."
	fis := memberScopeFixture(call)

	res := resolveIn(fis, "main.tf", memberRef("id"))
	if res.Confidence != resolver.ConfidenceExact || !res.HasUniqueTarget() || res.Candidates[0].Qualified != "child/output.id" {
		t.Fatalf("got %s %v", res.Confidence, res.Candidates)
	}
	if !hasEvidence(res, resolver.EvidenceMemberScope) {
		t.Errorf("evidence %v", res.Evidence)
	}

	missing := resolveIn(fis, "main.tf", memberRef("nope"))
	if missing.Confidence != resolver.ConfidenceUnresolved || missing.OutsideRepository || len(missing.Candidates) != 0 {
		t.Errorf("undeclared member: %s outside=%t", missing.Confidence, missing.OutsideRepository)
	}
}

func TestMemberScope_Outside(t *testing.T) {
	call := cfgSym("main.tf", "module.m", "module.m", symbol.KindModule)
	call.MembersOutside = true
	res := resolveIn(memberScopeFixture(call), "main.tf", memberRef("id"))
	if res.Confidence != resolver.ConfidenceUnresolved || !res.OutsideRepository || len(res.Candidates) != 0 {
		t.Errorf("got %s outside=%t %v", res.Confidence, res.OutsideRepository, res.Candidates)
	}
}

// Without a statement the members are looked up as before — and a module call
// has no lexically contained members, so nothing is found (and no output of
// some other directory is chosen by name).
func TestMemberScope_AbsentStatesNothing(t *testing.T) {
	call := cfgSym("main.tf", "module.m", "module.m", symbol.KindModule)
	res := resolveIn(memberScopeFixture(call), "main.tf", memberRef("id"))
	if res.Confidence != resolver.ConfidenceUnresolved || res.OutsideRepository || len(res.Candidates) != 0 {
		t.Errorf("got %s outside=%t %v", res.Confidence, res.OutsideRepository, res.Candidates)
	}
}

func TestMemberScope_AmbiguousReceiverPicksNone(t *testing.T) {
	a := cfgSym("main.tf", "module.m", "module.m", symbol.KindModule)
	a.MemberScope = "child/output."
	b := cfgSym("dup.tf", "module.m", "module.m", symbol.KindModule)
	b.MemberScope = "other/output."
	fis := append(memberScopeFixture(a), resolver.FileIndex{FileID: "dup.tf", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{b}})
	res := resolveIn(fis, "main.tf", memberRef("id"))
	if res.Confidence != resolver.ConfidenceCandidate || len(res.Candidates) != 2 || res.HasUniqueTarget() {
		t.Errorf("an ambiguous module call must yield both members as candidates: %s %v", res.Confidence, res.Candidates)
	}
}

// Two declarations of one module call, only one of whose scopes declares the
// member: still ambiguous — the other declaration's module may be the one
// meant, and its missing output is no evidence for the first.
func TestMemberScope_AmbiguousReceiverWithOneMatchIsNotExact(t *testing.T) {
	a := cfgSym("main.tf", "module.m", "module.m", symbol.KindModule)
	a.MemberScope = "child/output."
	b := cfgSym("dup.tf", "module.m", "module.m", symbol.KindModule)
	b.MemberScope = "absent/output."
	fis := append(memberScopeFixture(a), resolver.FileIndex{FileID: "dup.tf", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{b}})
	res := resolveIn(fis, "main.tf", memberRef("id"))
	if res.Confidence != resolver.ConfidenceCandidate || res.HasUniqueTarget() {
		t.Errorf("got %s %v, want Candidate", res.Confidence, res.Candidates)
	}
}

// A member missing from the stated scope is Unresolved even when another
// scope declares it: the statement is authoritative, there is no fallback.
func TestMemberScope_MissingMemberNeverFallsBackToAnotherScope(t *testing.T) {
	call := cfgSym("main.tf", "module.m", "module.m", symbol.KindModule)
	call.MemberScope = "child/output."
	fis := append(memberScopeFixture(call), resolver.FileIndex{FileID: "other/extra.tf", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{
		cfgSym("other/extra.tf", "output.only_other", "other/output.only_other", symbol.KindOutput),
	}})
	res := resolveIn(fis, "main.tf", memberRef("only_other"))
	if res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 || res.OutsideRepository {
		t.Errorf("got %s outside=%t %v", res.Confidence, res.OutsideRepository, res.Candidates)
	}
}

// A receiver of an ordinary (not identity-only) language with no member
// scope keeps the unchanged R0 member lookup: its own lexical member is
// Exact.
func TestMemberScope_OrdinaryTypesAreUnchanged(t *testing.T) {
	typ := symbol.Symbol{ID: "T", Name: "User", Qualified: `App\User`, Kind: symbol.KindClass, Language: "php", Location: source.Location{File: "u.php"}}
	m := symbol.Symbol{ID: "M", Name: "save", Qualified: `App\User.save`, Kind: symbol.KindMethod, Language: "php", Receiver: "User", ParentQualified: `App\User`, Location: source.Location{File: "u.php"}}
	fis := []resolver.FileIndex{{FileID: "u.php", Language: "php", Symbols: []symbol.Symbol{typ, m}}}
	ref := reference.Reference{ID: "r", Name: "save", Kind: reference.KindCall, Language: "php", ReceiverExpr: "$u", ReceiverTypeQualified: `App\User`, Location: source.Location{File: "u.php"}}
	res := resolveIn(fis, "u.php", ref)
	if res.Confidence != resolver.ConfidenceExact || !res.HasUniqueTarget() || res.Candidates[0].SymbolID != "M" || hasEvidence(res, resolver.EvidenceMemberScope) {
		t.Errorf("got %s %v %v", res.Confidence, res.Candidates, res.Evidence)
	}
}

func argRef(name string) reference.Reference {
	ref := memberRef(name)
	ref.NamedArgument = true
	return ref
}

func paramFixture(call symbol.Symbol) []resolver.FileIndex {
	return append(memberScopeFixture(call), resolver.FileIndex{FileID: "child/vars.tf", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{
		cfgSym("child/vars.tf", "var.id", "child/var.id", symbol.KindVariable),
		cfgSym("child/vars.tf", "var.cidr", "child/var.cidr", symbol.KindVariable),
	}})
}

// A named argument denotes the receiver's parameter (ParameterScope+Name),
// never a member of the same name, and never a parameter of another scope.
func TestParameterScope_NamedArgumentResolvesToTheParameter(t *testing.T) {
	call := cfgSym("main.tf", "module.m", "module.m", symbol.KindModule)
	call.MemberScope, call.ParameterScope = "child/output.", "child/var."
	fis := paramFixture(call)

	res := resolveIn(fis, "main.tf", argRef("id"))
	if res.Confidence != resolver.ConfidenceExact || !res.HasUniqueTarget() || res.Candidates[0].Qualified != "child/var.id" {
		t.Fatalf("argument: %s %v", res.Confidence, res.Candidates)
	}
	// The same name as a member is the output.
	if res := resolveIn(fis, "main.tf", memberRef("id")); res.Candidates[0].Qualified != "child/output.id" {
		t.Errorf("member: %v", res.Candidates)
	}
	if res := resolveIn(fis, "main.tf", argRef("nope")); res.Confidence != resolver.ConfidenceUnresolved || res.OutsideRepository {
		t.Errorf("undeclared parameter: %s outside=%t", res.Confidence, res.OutsideRepository)
	}
}

func TestParameterScope_NoStatementIsUnresolved(t *testing.T) {
	// A member scope says nothing about parameters: no fallback to outputs.
	call := cfgSym("main.tf", "module.m", "module.m", symbol.KindModule)
	call.MemberScope = "child/output."
	res := resolveIn(paramFixture(call), "main.tf", argRef("id"))
	if res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 || res.OutsideRepository {
		t.Errorf("got %s %v", res.Confidence, res.Candidates)
	}
	// A plain type with lexical members is no parameter scope either.
	typ := symbol.Symbol{ID: "T", Name: "User", Qualified: `App\User`, Kind: symbol.KindClass, Language: "php", Location: source.Location{File: "u.php"}}
	m := symbol.Symbol{ID: "M", Name: "save", Qualified: `App\User.save`, Kind: symbol.KindMethod, Language: "php", Receiver: "User", ParentQualified: `App\User`, Location: source.Location{File: "u.php"}}
	ref := reference.Reference{ID: "r", Name: "save", Kind: reference.KindCall, Language: "php", ReceiverExpr: "$u", ReceiverTypeQualified: `App\User`, NamedArgument: true, Location: source.Location{File: "u.php"}}
	if res := resolveIn([]resolver.FileIndex{{FileID: "u.php", Language: "php", Symbols: []symbol.Symbol{typ, m}}}, "u.php", ref); res.Confidence != resolver.ConfidenceUnresolved || len(res.Candidates) != 0 {
		t.Errorf("a named argument resolved to a member: %s %v", res.Confidence, res.Candidates)
	}
}

func TestParameterScope_OutsideAndAmbiguous(t *testing.T) {
	call := cfgSym("main.tf", "module.m", "module.m", symbol.KindModule)
	call.MembersOutside = true
	if res := resolveIn(paramFixture(call), "main.tf", argRef("id")); !res.OutsideRepository || len(res.Candidates) != 0 {
		t.Errorf("remote: %+v", res)
	}
	a := cfgSym("main.tf", "module.m", "module.m", symbol.KindModule)
	a.ParameterScope = "child/var."
	b := cfgSym("dup.tf", "module.m", "module.m", symbol.KindModule)
	b.ParameterScope = "absent/var."
	fis := append(paramFixture(a), resolver.FileIndex{FileID: "dup.tf", Language: "cfg", IdentityOnly: true, Symbols: []symbol.Symbol{b}})
	if res := resolveIn(fis, "main.tf", argRef("id")); res.Confidence != resolver.ConfidenceCandidate || res.HasUniqueTarget() {
		t.Errorf("two calls: %s %v", res.Confidence, res.Candidates)
	}
}
