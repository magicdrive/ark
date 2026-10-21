package java_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/contextquality"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/java"
)

// Context-quality certification scenarios. They drive the REAL pipeline
// (filesystem → provider → index → resolver → graph → context engine) over
// testdata/cq_java through the language-neutral harness; nothing semantic is
// mocked.
//
// Gates for every scenario: required-context recall = 1.00 and no irrelevant
// symbol included. Overloads share their qualified name and their file, so
// they are pinned by the line their declaration starts on ("Qualified@line").
// The expectations are javac's: javap of the fixture compiled with JDK 21,
// once with src and module-a and once with src and module-b, beside a stub
// of the external class (package org.vendor; public class User { public
// void touch() {} }), which the fixture deliberately does not hold.
// Irrelevant entries are what the code does not use — another overload, a
// same-name member of another type, an implementation a dispatch may reach,
// an external type's namesake. A true relation the graph does not prove (an
// overload told apart only by a reference argument's type, a receiver typed
// by a method's result) is Optional: the context may show it once the graph
// proves it, and never shows it before.

type cqCase struct {
	name string
	contextquality.Scenario
}

const cqJava = "testdata/cq_java"

func javaProviders() []language.Provider { return []language.Provider{java.NewProvider()} }

func (c cqCase) run(t *testing.T) contextquality.Result {
	t.Helper()
	res, err := contextquality.Evaluate(filepath.FromSlash(cqJava), javaProviders(), c.Scenario)
	if err != nil {
		t.Fatalf("[%s] %v", c.name, err)
	}
	if !res.TargetFound || !res.TargetIncluded {
		t.Fatalf("[%s] target %q found=%v included=%v", c.name, c.TargetQualified, res.TargetFound, res.TargetIncluded)
	}
	order := make([]string, len(res.Selected))
	for i, s := range res.Selected {
		order[i] = fmt.Sprintf("%s(%s)", s.Qualified, s.Reason)
	}
	t.Logf("[%s] recall=%.2f irrelevant=%.2f (%d/%d selected) tokens=%d/%d order: %s",
		c.name, res.RequiredRecall, res.IrrelevantRatio, res.SelectedCount, res.TotalCandidates,
		res.EstimatedTokens, res.BudgetTokens, strings.Join(order, ", "))
	if res.RequiredRecall != 1 {
		t.Errorf("[%s] required recall = %.2f, missing %v", c.name, res.RequiredRecall, res.MissingRequired)
	}
	if len(res.IrrelevantIncluded) != 0 {
		t.Errorf("[%s] irrelevant context included: %v", c.name, res.IrrelevantIncluded)
	}
	if len(res.UnknownEntries) != 0 {
		t.Errorf("[%s] entries that name no symbol: %v", c.name, res.UnknownEntries)
	}
	return res
}

const (
	svc      = "com.shop.service.UserService"
	user     = "com.shop.model.User"
	userRepo = "com.shop.repo.UserRepository"
	texts    = "com.shop.util.Texts"
	configA  = "module-a/src/main/java/com/shop/shared/Config.java#com.shop.shared.Config"
	configB  = "module-b/src/main/java/com/shop/shared/Config.java#com.shop.shared.Config"
)

func javaCases() []cqCase {
	return []cqCase{
		{name: "A service method", Scenario: contextquality.Scenario{
			// new User(normalize(name)) is the one-argument constructor and the
			// statically imported one-argument normalize; repo.save is the
			// field's declared type. audit(u) is audit(User), which the graph
			// does not tell from audit(Order) by argument count.
			TargetQualified: svc + ".create", Depth: 2, MaxTokens: 4000,
			Required: []string{
				svc + ".create", user + ".<init>@7", texts + ".normalize@6", userRepo + ".save",
				user, "com.shop.model.Audited", svc + ".repo",
			},
			Optional: []string{texts + ".normalize@10", "com.shop.model.Entity.touch", svc + ".audit@30"},
			Irrelevant: []string{
				user + ".<init>@11", svc + ".audit@34",
				"com.shop.repo.OrderRepository.save", "com.shop.service.OrderService.create",
				"com.shop.service.UnrelatedService.create", "com.shop.service.OrderService.audit",
			},
		}},
		{name: "B overload by argument type", Scenario: contextquality.Scenario{
			TargetQualified: svc + ".lookup", Depth: 2, MaxTokens: 4000,
			Required:   []string{svc + ".lookup", userRepo + ".find@10"},
			Irrelevant: []string{userRepo + ".find@14", "com.shop.repo.OrderRepository.find"},
		}},
		{name: "B2 the other overload by argument type", Scenario: contextquality.Scenario{
			// The same call shape as B, the other overload: neither the first
			// nor the last declaration is the answer for both.
			TargetQualified: svc + ".lookupByName", Depth: 2, MaxTokens: 4000,
			Required:   []string{svc + ".lookupByName", userRepo + ".find@14"},
			Irrelevant: []string{userRepo + ".find@10", "com.shop.repo.OrderRepository.find"},
		}},
		{name: "B3 primitive overloads: ofLong", Scenario: contextquality.Scenario{
			// code(long), code(int), code(boolean): each call has its own
			// answer (javap), so no fixed choice among them passes all three.
			TargetQualified: "com.shop.service.Coder.ofLong", Depth: 1, MaxTokens: 4000,
			Required:   []string{"com.shop.service.Coder.ofLong", "com.shop.util.Codes.code@6"},
			Irrelevant: []string{"com.shop.util.Codes.code@10", "com.shop.util.Codes.code@14"},
		}},
		{name: "B3 primitive overloads: ofInt", Scenario: contextquality.Scenario{
			// code(long), code(int), code(boolean): each call has its own
			// answer (javap), so no fixed choice among them passes all three.
			TargetQualified: "com.shop.service.Coder.ofInt", Depth: 1, MaxTokens: 4000,
			Required:   []string{"com.shop.service.Coder.ofInt", "com.shop.util.Codes.code@10"},
			Irrelevant: []string{"com.shop.util.Codes.code@6", "com.shop.util.Codes.code@14"},
		}},
		{name: "B3 primitive overloads: ofFlag", Scenario: contextquality.Scenario{
			// code(long), code(int), code(boolean): each call has its own
			// answer (javap), so no fixed choice among them passes all three.
			TargetQualified: "com.shop.service.Coder.ofFlag", Depth: 1, MaxTokens: 4000,
			Required:   []string{"com.shop.service.Coder.ofFlag", "com.shop.util.Codes.code@14"},
			Irrelevant: []string{"com.shop.util.Codes.code@6", "com.shop.util.Codes.code@10"},
		}},
		{name: "C unknown receiver", Scenario: contextquality.Scenario{
			// var r = locate(): the receiver's type is a method's result, not
			// declared, so r.save is not proven to be UserRepository.save.
			TargetQualified: svc + ".untyped", Depth: 2, MaxTokens: 4000,
			Required:   []string{svc + ".untyped", svc + ".locate"},
			Optional:   []string{userRepo + ".save"},
			Irrelevant: []string{"com.shop.repo.OrderRepository.save", "com.shop.repo.OrderRepository"},
		}},
		{name: "D inheritance", Scenario: contextquality.Scenario{
			TargetQualified: user, Depth: 1, MaxTokens: 4000,
			Required:   []string{user, "com.shop.model.Entity", "com.shop.model.Named"},
			Irrelevant: []string{"com.shop.model.Order", "com.shop.service.UnrelatedService"},
		}},
		{name: "E interface default method", Scenario: contextquality.Scenario{
			// n.label() is Named's default method; what it calls is Named.name,
			// never an implementation picked by dynamic dispatch.
			TargetQualified: svc + ".describe", Depth: 2, MaxTokens: 4000,
			Required:   []string{svc + ".describe", "com.shop.model.Named.label", "com.shop.model.Named.name"},
			Irrelevant: []string{user + ".name", "com.shop.model.Order.name"},
		}},
		{name: "F interface method, not its implementations", Scenario: contextquality.Scenario{
			TargetQualified: svc + ".nameOf", Depth: 2, MaxTokens: 4000,
			Required:   []string{svc + ".nameOf", "com.shop.model.Named.name"},
			Irrelevant: []string{user + ".name", "com.shop.model.Order.name"},
		}},
		{name: "G inherited member", Scenario: contextquality.Scenario{
			// user.touch() is declared by Entity, inherited by User.
			TargetQualified: userRepo + ".save", Depth: 1, MaxTokens: 4000,
			Required:   []string{userRepo + ".save", "com.shop.model.Entity.touch", user},
			Irrelevant: []string{"com.shop.repo.OrderRepository.save", "com.shop.model.Order"},
		}},
		{name: "H callers of a repository method", Scenario: contextquality.Scenario{
			// The typed caller is context; the unknown receiver is a candidate
			// caller only, the same-name OrderService.create is unrelated, and
			// the test is left out unless asked for.
			TargetQualified: userRepo + ".save", Depth: 2, MaxTokens: 4000,
			Required: []string{userRepo + ".save", svc + ".create"},
			Optional: []string{svc + ".untyped"},
			Irrelevant: []string{
				"com.shop.service.OrderService.create",
				"com.shop.service.UserServiceTest.creates", "com.shop.repo.OrderRepository.save",
			},
		}},
		{name: "I test callers on request", Scenario: contextquality.Scenario{
			TargetQualified: svc + ".create", Depth: 1, MaxTokens: 4000, IncludeTests: true,
			Required:   []string{svc + ".create", "com.shop.service.UserServiceTest.creates"},
			Irrelevant: []string{"com.shop.service.OrderService.create"},
		}},
		{name: "J tests left out", Scenario: contextquality.Scenario{
			TargetQualified: svc + ".create", Depth: 1, MaxTokens: 4000,
			Required:   []string{svc + ".create"},
			Irrelevant: []string{"com.shop.service.UserServiceTest.creates"},
		}},
		{name: "K constructor overload and its callers", Scenario: contextquality.Scenario{
			// User(String) is called by create, by of, and by this("anonymous")
			// in User(); the no-argument constructor is not context.
			TargetQualified: user + ".<init>", TargetLine: 7, Depth: 1, MaxTokens: 4000,
			Required:   []string{user + ".<init>@7", svc + ".create", user + ".of", user + ".<init>@11"},
			Irrelevant: []string{"com.shop.gateway.Gateway.fetch"},
		}},
		{name: "L record", Scenario: contextquality.Scenario{
			TargetQualified: svc + ".total", Depth: 2, MaxTokens: 4000,
			Required:   []string{svc + ".total", "com.shop.model.Money.plus", "com.shop.model.Money"},
			Optional:   []string{"com.shop.model.Money.<init>"},
			Irrelevant: []string{user, "com.shop.model.Order"},
		}},
		{name: "M enum constant", Scenario: contextquality.Scenario{
			// Status.ACTIVE.allows() invokes Status.allows; BLOCKED's body
			// overrides it, and that override is never the context of the call.
			TargetQualified: svc + ".active", Depth: 1, MaxTokens: 4000,
			Required:   []string{svc + ".active", "com.shop.model.Status.ACTIVE", "com.shop.model.Status"},
			Optional:   []string{"com.shop.model.Status.allows"},
			Irrelevant: []string{"com.shop.model.Status.<anonymous>.allows"},
		}},
		{name: "N method reference", Scenario: contextquality.Scenario{
			TargetQualified: svc + ".factory", Depth: 1, MaxTokens: 4000,
			Required:   []string{svc + ".factory", user + ".of"},
			Irrelevant: []string{user + ".<init>@11", "com.shop.gateway.Gateway.fetch"},
		}},
		{name: "O one name in two source roots", Scenario: contextquality.Scenario{
			// com.shop.shared.Config is declared in module-a and module-b: the
			// graph cannot choose, so neither declaration is context.
			TargetQualified: svc + ".setting", Depth: 2, MaxTokens: 4000,
			Required:   []string{svc + ".setting"},
			Irrelevant: []string{configA, configB, configA + ".get", configB + ".get"},
		}},
		{name: "P one declaration of a duplicated name", Scenario: contextquality.Scenario{
			TargetQualified: "com.shop.shared.Config.get", TargetFile: "module-a/src/main/java/com/shop/shared/Config.java",
			Depth: 1, MaxTokens: 4000,
			Required:   []string{configA + ".get"},
			Irrelevant: []string{configB + ".get", svc + ".setting"},
		}},
		{name: "Q nested class calls its enclosing class", Scenario: contextquality.Scenario{
			TargetQualified: svc + ".Cache.get", Depth: 1, MaxTokens: 4000,
			Required:   []string{svc + ".Cache.get", svc + ".lookup"},
			Irrelevant: []string{userRepo + ".find@14"},
		}},
		{name: "R external import is not a repository dependency", Scenario: contextquality.Scenario{
			// org.vendor.User shares its simple name with com.shop.model.User.
			TargetQualified: "com.shop.gateway.Gateway.fetch", Depth: 2, MaxTokens: 4000,
			Required:   []string{"com.shop.gateway.Gateway.fetch"},
			Irrelevant: []string{user, user + ".<init>@11", user + ".<init>@7", "com.shop.model.Entity.touch"},
		}},
		{name: "S on-demand import stays strong", Scenario: contextquality.Scenario{
			// User comes from `import com.shop.model.*`, which a type of the
			// package could shadow: the graph's edges are Strong, and context
			// shows them as strong, never as exact.
			TargetQualified: "com.shop.service.Reports.title", Depth: 1, MaxTokens: 4000,
			// User.name is a field and a method; u.name() is the method.
			Required:   []string{"com.shop.service.Reports.title", user + ".name@19", user},
			Irrelevant: []string{"com.shop.model.Named.name", "com.shop.model.Order.name"},
		}},
	}
}

func TestContextQuality_Java(t *testing.T) {
	for _, c := range javaCases() {
		t.Run(c.name, func(t *testing.T) { c.run(t) })
	}
}

// Context keeps each relation's confidence: the on-demand import's Strong
// edges stay strong, and only the target is exact by definition.
func TestContextQuality_JavaConfidenceIsTheEdges(t *testing.T) {
	var c cqCase
	for _, k := range javaCases() {
		if strings.HasPrefix(k.name, "S ") {
			c = k
		}
	}
	res := c.run(t)
	for _, s := range res.Selected {
		want := "strong"
		if s.Reason == "target" {
			want = "exact"
		}
		if s.Confidence != want {
			t.Errorf("%s (%s) is %s, want %s", s.Qualified, s.Reason, s.Confidence, want)
		}
	}
}

// Budget pressure: the target is always included and first, items are whole
// semantic units, the budget is honoured, shrinking it never adds context,
// and an oversized target keeps the TargetTruncated contract.
func TestContextQuality_JavaBudgetPressure(t *testing.T) {
	base := contextquality.Scenario{TargetQualified: svc + ".create", Depth: 2}
	var prev []contextquality.Selected
	for _, budget := range []int{4000, 300, 150, 80} {
		s := base
		s.MaxTokens = budget
		res, err := contextquality.Evaluate(filepath.FromSlash(cqJava), javaProviders(), s)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("budget=%d selected=%d/%d tokens=%d truncatedItems=%d targetTruncated=%v",
			budget, res.SelectedCount, res.TotalCandidates, res.EstimatedTokens, res.Truncated, res.TargetTruncated)
		if !res.TargetIncluded || len(res.Selected) == 0 || res.Selected[0].Qualified != s.TargetQualified {
			t.Errorf("budget %d: the target must be included and first: %+v", budget, res.Selected)
		}
		if !res.TargetTruncated && res.EstimatedTokens > budget {
			t.Errorf("budget %d exceeded without target truncation: %d", budget, res.EstimatedTokens)
		}
		sum := 0
		for _, it := range res.Selected {
			if it.Tokens <= 0 {
				t.Errorf("budget %d: item %s has no tokens", budget, it.Qualified)
			}
			sum += it.Tokens
		}
		if sum != res.EstimatedTokens {
			t.Errorf("budget %d: items sum to %d tokens, stats say %d", budget, sum, res.EstimatedTokens)
		}
		if prev != nil {
			if len(res.Selected) > len(prev) {
				t.Errorf("budget %d selects %d items, more than %d at a larger budget", budget, len(res.Selected), len(prev))
			}
			// Pressure drops items; it never reorders or invents them.
			at := 0
			for _, it := range res.Selected {
				for at < len(prev) && prev[at] != it {
					at++
				}
				if at == len(prev) {
					t.Errorf("budget %d: %+v is not in the larger budget's selection, in order", budget, it)
					break
				}
			}
		}
		prev = res.Selected
	}
	s := base
	s.MaxTokens = 5
	res, err := contextquality.Evaluate(filepath.FromSlash(cqJava), javaProviders(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TargetIncluded || !res.TargetTruncated || res.SelectedCount != 1 {
		t.Errorf("oversized target: included=%v targetTruncated=%v selected=%d, want true, true, 1",
			res.TargetIncluded, res.TargetTruncated, res.SelectedCount)
	}
}

// A class larger than any budget (Ledger, 526 lines) is still the first item
// and the budget's first spending: over the budget it is the only item; with
// room, its supertype and its caller follow it.
func TestContextQuality_JavaLargeTargetFirst(t *testing.T) {
	for _, budget := range []int{100, 8000} {
		res, err := contextquality.Evaluate(filepath.FromSlash(cqJava), javaProviders(), contextquality.Scenario{
			TargetQualified: "com.shop.report.Ledger", Depth: 1, MaxTokens: budget,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Selected) == 0 || res.Selected[0].Reason != "target" {
			t.Fatalf("budget %d: the large target is not first: %+v", budget, res.Selected)
		}
		over := res.Selected[0].Tokens > budget
		if res.TargetTruncated != over {
			t.Errorf("budget %d: targetTruncated=%v for a %d-token target", budget, res.TargetTruncated, res.Selected[0].Tokens)
		}
		if over && len(res.Selected) != 1 {
			t.Errorf("budget %d: %d items beside a target over the budget: %+v", budget, len(res.Selected)-1, res.Selected)
		}
		if !over && len(res.Selected) != 3 {
			t.Errorf("budget %d: want Ledger, Entity and LedgerReport.total: %+v", budget, res.Selected)
		}
	}
}

// Determinism: identical output across repeated evaluations, ordering included.
func TestContextQuality_JavaDeterministic(t *testing.T) {
	for _, c := range javaCases() {
		render := func() string {
			res, err := contextquality.Evaluate(filepath.FromSlash(cqJava), javaProviders(), c.Scenario)
			if err != nil {
				t.Fatal(err)
			}
			return fmt.Sprintf("%v|%v|%v|%d", res.Selected, res.MissingRequired, res.IrrelevantIncluded, res.EstimatedTokens)
		}
		want := render()
		for i := range 10 {
			if got := render(); got != want {
				t.Fatalf("[%s] run %d non-deterministic:\n%s\n%s", c.name, i, got, want)
			}
		}
	}
}
