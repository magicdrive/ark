package typescript_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/typescript"
)

func tsProviders() []language.Provider {
	return []language.Provider{typescript.NewProvider(), typescript.NewTSXProvider()}
}

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func buildIndex(t *testing.T, files map[string]string) *index.RepositoryIndex {
	t.Helper()
	idx, err := index.New(context.Background(), writeRepo(t, files), tsProviders())
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

// edgeSet renders every forward graph edge as "file:From -kind-> file:To conf".
func edgeSet(t *testing.T, idx *index.RepositoryIndex) []string {
	t.Helper()
	var out []string
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			for _, e := range idx.GetCallees(s.ID) {
				to, ok := idx.GetSymbol(e.To)
				if !ok {
					t.Errorf("fabricated edge: target %s is not a real symbol (from %s)", e.To, s.Qualified)
					continue
				}
				out = append(out, fmt.Sprintf("%s:%s -%s-> %s:%s %s", f, s.Qualified, e.Kind, to.Location.File, to.Qualified, e.Confidence))
			}
		}
	}
	sort.Strings(out)
	return out
}

// edgesFrom returns the edges whose source is file:container.
func edgesFrom(edges []string, fileAndContainer string) []string {
	var out []string
	for _, e := range edges {
		if strings.HasPrefix(e, fileAndContainer+" ") {
			out = append(out, strings.TrimPrefix(e, fileAndContainer+" "))
		}
	}
	return out
}

func wantEdges(t *testing.T, edges []string, from string, want ...string) {
	t.Helper()
	got := edgesFrom(edges, from)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("edges from %s:\n  got:\n    %s\n  want:\n    %s", from,
			strings.Join(got, "\n    "), strings.Join(want, "\n    "))
	}
}

// checkGraphInvariants enforces the certification gates over a whole index:
// no fabricated targets (checked in edgeSet), no non-unique-confidence edges,
// and no duplicate (From, To, Kind).
func checkGraphInvariants(t *testing.T, idx *index.RepositoryIndex) {
	t.Helper()
	seen := map[string]bool{}
	for _, f := range idx.Files() {
		for _, s := range idx.SymbolsByFile(f) {
			for _, e := range idx.GetCallees(s.ID) {
				if e.Confidence.String() != "exact" && e.Confidence.String() != "strong" {
					t.Errorf("edge %s -> %s has confidence %s; only Exact/Strong may form edges", s.Qualified, e.To, e.Confidence)
				}
				k := fmt.Sprintf("%s|%s|%s", e.From, e.To, e.Kind)
				if seen[k] {
					t.Errorf("duplicate edge %s", k)
				}
				seen[k] = true
			}
		}
	}
}

const (
	userRepoTS  = "export class UserRepository { save(u: unknown): void {} }\n"
	orderRepoTS = "export class OrderRepository { save(o: unknown): void {} }\n"
)

// --- same-name members -------------------------------------------------------

func TestGraph_SameNameSaveAcrossClasses(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"repos/user.ts":  userRepoTS,
		"repos/order.ts": orderRepoTS,
		"svc.ts": `import { UserRepository } from "./repos/user";
import { OrderRepository } from "./repos/order";
export function typed(repo: UserRepository) { repo.save(1); }
export function typedOrder(repo: OrderRepository) { repo.save(1); }
export function unknown(repo: any) { repo.save(1); }
export function untyped(repo) { repo.save(1); }
export function viaNew() { const r = new UserRepository(); r.save(1); }
export function union(repo: UserRepository | OrderRepository) { repo.save(1); }
export function shadowed(repo: UserRepository) { const f = (repo: OrderRepository) => repo.save(2); repo.save(1); f(null as any); }
`,
	})
	edges := edgeSet(t, idx)
	checkGraphInvariants(t, idx)
	user := "repos/user.ts:UserRepository.save exact"
	order := "repos/order.ts:OrderRepository.save exact"
	typeUse := func(file, cls string) string { return fmt.Sprintf("-uses_type-> %s:%s exact", file, cls) }
	callsCls := func(file, cls string) string { return fmt.Sprintf("-calls-> %s:%s exact", file, cls) }
	_ = typeUse
	_ = callsCls

	wantEdges(t, edges, "svc.ts:typed", "-calls-> "+user, "-uses_type-> repos/user.ts:UserRepository exact")
	wantEdges(t, edges, "svc.ts:typedOrder", "-calls-> "+order, "-uses_type-> repos/order.ts:OrderRepository exact")
	// No type evidence: Candidate at most → no edge to ANY save.
	wantEdges(t, edges, "svc.ts:unknown")
	wantEdges(t, edges, "svc.ts:untyped")
	wantEdges(t, edges, "svc.ts:union", "-uses_type-> repos/user.ts:UserRepository exact", "-uses_type-> repos/order.ts:OrderRepository exact")
	wantEdges(t, edges, "svc.ts:viaNew", "-calls-> "+user, "-calls-> repos/user.ts:UserRepository exact")
	// The closure's own annotated parameter is proven; the outer poisoned one is not.
	got := edgesFrom(edges, "svc.ts:shadowed")
	for _, g := range got {
		if g == "-calls-> "+user {
			t.Errorf("shadowed outer receiver must not resolve to UserRepository.save: %v", got)
		}
	}
}

// A unique `save` in the whole repository is still not evidence for repo.save().
func TestGraph_UniqueNameIsNotReceiverEvidence(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"only.ts": "export class OnlyRepo { save() {} }\n",
		"use.ts":  "export function f(repo) { repo.save(); }\nexport function g(onlyRepo: any) { onlyRepo.save(); }\n",
	})
	edges := edgeSet(t, idx)
	checkGraphInvariants(t, idx)
	wantEdges(t, edges, "use.ts:f")
	wantEdges(t, edges, "use.ts:g")
}

func TestGraph_ThisMemberAndStaticReceiver(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"a.ts": `export class UserService {
  save() {}
  create() { this.save(); this.missing(); return UserService.make(); }
  static make() { return new UserService(); }
  other() { Other.make(); }
}
export class Other { }
export class OrderService { save() {} }
`,
		"b.ts": `export class Base { log() {} }
export class Child extends Base { run() { this.log(); } }
`,
	})
	edges := edgeSet(t, idx)
	checkGraphInvariants(t, idx)
	wantEdges(t, edges, "a.ts:UserService.create",
		"-calls-> a.ts:UserService.save exact",
		"-calls-> a.ts:UserService.make exact")
	// Explicit static receiver is constrained to that type: Other has no make.
	wantEdges(t, edges, "a.ts:UserService.other")
	// Inherited member: not resolved (no inheritance-aware lookup) — never guessed.
	wantEdges(t, edges, "b.ts:Child.run")
}

// --- modules -------------------------------------------------------------------

func TestGraph_ImportsAliasesDefaultNamespace(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"a/user.ts": `export class User { static create() { return new User(); } }
export default function makeUser() {}
export function helper() {}
export type UserId = string;
`,
		"b/user.ts": "export class User {}\nexport function helper() {}\n",
		"app.ts": `import makeUser, { User, User as DomainUser, helper } from "./a/user";
import * as ns from "./a/user";
import type { UserId } from "./a/user";
export function named() { return new User(); }
export function alias() { return new DomainUser(); }
export function dflt() { return makeUser(); }
export function viaNs() { ns.helper(); return new ns.User(); }
export function staticAlias() { return DomainUser.create(); }
export function typeOnly(id: UserId) {}
export function fn() { helper(); }
`,
	})
	edges := edgeSet(t, idx)
	checkGraphInvariants(t, idx)
	// Every edge targets a/user.ts, never the same-name b/user.ts symbols.
	for _, e := range edges {
		if strings.Contains(e, "-> b/user.ts") {
			t.Errorf("edge to the unrelated duplicate module: %s", e)
		}
	}
	wantEdges(t, edges, "app.ts:named", "-calls-> a/user.ts:User exact")
	wantEdges(t, edges, "app.ts:alias", "-calls-> a/user.ts:User exact")
	wantEdges(t, edges, "app.ts:dflt", "-calls-> a/user.ts:makeUser exact")
	wantEdges(t, edges, "app.ts:viaNs", "-calls-> a/user.ts:helper exact", "-calls-> a/user.ts:User exact")
	wantEdges(t, edges, "app.ts:staticAlias", "-calls-> a/user.ts:User.create exact")
	wantEdges(t, edges, "app.ts:typeOnly", "-uses_type-> a/user.ts:UserId exact")
	wantEdges(t, edges, "app.ts:fn", "-calls-> a/user.ts:helper exact")
}

func TestGraph_ExternalAndUnsupportedAliasNeverFallBack(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		// Repository symbols that share names with the external imports.
		"z.ts":             "export function z() {}\nexport function string() {}\n",
		"shared/helper.ts": "export function helper() {}\n",
		"react.ts":         "export function useState() {}\n",
		"app.ts": `import { z } from "zod";
import { helper } from "@/shared/helper";
import { useState } from "react";
import { fs } from "node:fs";
import { esc } from "../../outside";
export function f() { z(); helper(); useState(); fs(); esc(); z.string(); }
`,
	})
	edges := edgeSet(t, idx)
	checkGraphInvariants(t, idx)
	wantEdges(t, edges, "app.ts:f")
}

func TestGraph_TypeOnlyBindingUsedAsValue(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"user.ts": "export class User {}\n",
		"app.ts": `import type { User } from "./user";
export function f(u: User) { return new User(); }
`,
	})
	edges := edgeSet(t, idx)
	checkGraphInvariants(t, idx)
	// Type position resolves; the value-position `new User()` does not.
	wantEdges(t, edges, "app.ts:f", "-uses_type-> user.ts:User exact")
}

func TestGraph_BarrelChainAndCycle(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"domain/user.ts":  "export class Account { open() {} }\n",
		"domain/order.ts": "export class Order {}\n",
		"domain/index.ts": `export { Account } from "./user";
export { Account as DomainAccount } from "./user";
export * from "./order";
export * as orders from "./order";
`,
		"index.ts": "export * from \"./domain\";\n",
		"app.ts": `import { Account, DomainAccount, Order, orders } from "./index";
export function run() {
  const a = new Account();
  a.open();
  new DomainAccount();
  new Order();
  new orders.Order();
}
`,
		// Cyclic barrels: terminate, and names declared in the cycle resolve.
		"cyc/a.ts": "export * from \"./b\";\nexport class Alpha {}\n",
		"cyc/b.ts": "export * from \"./a\";\nexport class Beta {}\n",
		"cyc/app.ts": `import { Alpha, Beta, Ghost } from "./a";
export function f() { new Alpha(); new Beta(); new Ghost(); }
`,
	})
	edges := edgeSet(t, idx)
	checkGraphInvariants(t, idx)
	wantEdges(t, edges, "app.ts:run",
		"-calls-> domain/user.ts:Account exact",
		"-calls-> domain/user.ts:Account.open exact",
		"-calls-> domain/order.ts:Order exact")
	wantEdges(t, edges, "cyc/app.ts:f",
		"-calls-> cyc/a.ts:Alpha exact",
		"-calls-> cyc/b.ts:Beta exact")
	// The barrel files themselves carry no symbol edges.
	for _, e := range edges {
		if strings.HasPrefix(e, "index.ts:") || strings.HasPrefix(e, "domain/index.ts:") {
			t.Errorf("barrel produced an edge: %s", e)
		}
	}
}

// Two modules exporting the same name through `export *` → ambiguity, no edge.
func TestGraph_DuplicateExportedUserThroughBarrel(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"a.ts":     "export class User {}\n",
		"b.ts":     "export class User {}\n",
		"index.ts": "export * from \"./a\";\nexport * from \"./b\";\n",
		"app.ts":   "import { User } from \"./index\";\nexport function f() { return new User(); }\n",
		// A direct import is unambiguous.
		"direct.ts": "import { User } from \"./a\";\nexport function g() { return new User(); }\n",
	})
	edges := edgeSet(t, idx)
	checkGraphInvariants(t, idx)
	wantEdges(t, edges, "app.ts:f")
	wantEdges(t, edges, "direct.ts:g", "-calls-> a.ts:User exact")
}

// Module candidate priority within Ark's supported lexical subset: .ts beats .tsx; `.js`
// substitution with both present stays ambiguous.
func TestGraph_ModuleCandidatePriority(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"m/user.ts":  "export class User {}\n",
		"m/user.tsx": "export class User {}\n",
		"a.ts":       "import { User } from \"./m/user\";\nexport function f() { return new User(); }\n",
		"b.ts":       "import { User } from \"./m/user.js\";\nexport function g() { return new User(); }\n",
		"c.ts":       "import { User } from \"./m/user.tsx\";\nexport function h() { return new User(); }\n",
	})
	edges := edgeSet(t, idx)
	checkGraphInvariants(t, idx)
	wantEdges(t, edges, "a.ts:f", "-calls-> m/user.ts:User exact")
	wantEdges(t, edges, "b.ts:g") // ./user.js with both .ts and .tsx present: not ranked
	wantEdges(t, edges, "c.ts:h", "-calls-> m/user.tsx:User exact")
}

func TestGraph_DirectoryIndexModule(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"lib/index.ts": "export class Lib {}\n",
		"app.ts":       "import { Lib } from \"./lib\";\nexport function f() { return new Lib(); }\n",
	})
	wantEdges(t, edgeSet(t, idx), "app.ts:f", "-calls-> lib/index.ts:Lib exact")
}

// --- types: same name in type and value positions --------------------------------

func TestGraph_TypeValueSameNameStaysAmbiguous(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"merged.ts": `export interface Foo { a: number }
export class Foo { b = 1 }
export function use(x: Foo) { return new Foo(); }
`,
	})
	edges := edgeSet(t, idx)
	checkGraphInvariants(t, idx)
	// Declaration merging is not resolved: two plausible declarations.
	wantEdges(t, edges, "merged.ts:use")
}

// --- relations ---------------------------------------------------------------------

func TestGraph_ExtendsAndImplements(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"base.ts":  "export class BaseService {}\nexport interface Service { run(): void }\nexport interface Parent {}\n",
		"other.ts": "export class BaseService {}\n",
		"svc.ts": `import { BaseService, Service, Parent } from "./base";
export class UserService extends BaseService implements Service {}
export interface Child extends Parent {}
export class Missing extends Nowhere implements Absent {}
`,
	})
	edges := edgeSet(t, idx)
	checkGraphInvariants(t, idx)
	wantEdges(t, edges, "svc.ts:UserService",
		"-extends-> base.ts:BaseService exact",
		"-implements-> base.ts:Service exact")
	wantEdges(t, edges, "svc.ts:Child", "-extends-> base.ts:Parent exact")
	wantEdges(t, edges, "svc.ts:Missing") // unresolved targets: no fabricated SymbolID
}

// --- TSX ---------------------------------------------------------------------------

func TestGraph_TSXComponents(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"ui/card.tsx": `export function UserCard(p: { n: string }) { return <span>{p.n}</span>; }
export const Badge = () => <i />;
export function useUser() { return "u"; }
export const Layout = { Header: () => <header /> };
`,
		"ui/button.tsx":  "export function Button() { return <button />; }\n",
		"other/card.tsx": "export function UserCard() { return <b />; }\n",
		"page.tsx": `import { UserCard, Badge, useUser, Layout } from "./ui/card";
import * as UI from "./ui/button";
export function UserPage() {
  const name = useUser();
  return (
    <div>
      <UserCard n={name} />
      <Badge />
      <Layout.Header />
      <UI.Button />
      <span /><button />
    </div>
  );
}
export const Home = () => <UserCard n="x" />;
`,
	})
	edges := edgeSet(t, idx)
	checkGraphInvariants(t, idx)
	wantEdges(t, edges, "page.tsx:UserPage",
		"-calls-> ui/card.tsx:UserCard exact",
		"-calls-> ui/card.tsx:Badge exact",
		"-calls-> ui/card.tsx:useUser exact",
		"-calls-> ui/button.tsx:Button exact")
	wantEdges(t, edges, "page.tsx:Home", "-calls-> ui/card.tsx:UserCard exact")
	for _, e := range edges {
		if strings.Contains(e, "-> other/card.tsx") {
			t.Errorf("edge to unrelated same-name component: %s", e)
		}
	}
}

// --- determinism ---------------------------------------------------------------------

func TestGraph_Deterministic(t *testing.T) {
	files := map[string]string{
		"a.ts":     "export class User { save() {} }\n",
		"b.ts":     "export class User { save() {} }\n",
		"index.ts": "export * from \"./a\";\nexport * from \"./b\";\n",
		"app.ts": `import { User } from "./a";
import * as all from "./index";
export function f(u: User) { u.save(); new User(); new all.User(); }
`,
	}
	root := writeRepo(t, files)
	render := func() string {
		idx, err := index.New(context.Background(), root, tsProviders())
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(edgeSet(t, idx), "\n")
	}
	want := render()
	for i := range 30 {
		if got := render(); got != want {
			t.Fatalf("run %d differs:\n%s\n---\n%s", i, got, want)
		}
	}
}
