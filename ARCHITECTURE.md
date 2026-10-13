# Ark Architecture

Ark is a static code-intelligence engine: language providers extract symbols
and references from source, a language-neutral resolver grades what each
reference denotes, and the resulting symbol graph feeds ranked,
token-budgeted context to its CLI and MCP clients. It never executes the code
it analyzes.

Code tells WHAT. Tests prove BEHAVIOR. This file records WHY, and what must
not change: the invariants a plausible-looking "improvement" can silently
break. It is deliberately short. Every rule names its authority — the code
comment or test that defines it. If this file and its authority disagree, the
authority wins and this file is the bug.

Ark's priority order is **trustworthy context before useful context**: an
honest "unknown" is a correct answer; a confident wrong answer is a defect.

This file is for contributors. Users' documentation — what the confidence
levels mean for an answer, per-language limits, operations — is in `docs/`
(`docs/resolution-model.md`, `docs/language-support.md`,
`docs/operations.md`).

## 1. Pipeline and responsibility boundaries

```
Provider ──► index builder ──► resolver ──► graph + completeness ──► Context Engine / impact / repomap ──► MCP adapter
(1 file)     (NewFileIndex)    (evidence →   (edges only for          (consume the index;                 (target lookup,
                               confidence)    unique Strong+)          never resolve names)                 index reuse, output)
```

| Layer | Owns | Must not |
|---|---|---|
| Provider (`internal/languages/<lang>`) | Everything language-specific. Parses one file, consults nothing else, and states what the language's own rules prove as **generic evidence** (`language.ReferenceDraft`: `ReceiverType`, `ReceiverChain`, `Speculative`, `NameQualified`, `ReceiverTypeQualified`, `IdentityInRepository`, `ConfidenceCap`, `Dynamic`, `TargetKinds`; `SymbolDraft.MemberScope` / `MembersOutside`; `Extraction.Aliases`, `Extraction.TypeFacts`; bindings, exports, `ModuleScoped`, `IdentityOnly`, `Package`, `PackageScoped`; `language.Dialect` for a provider whose files share another language's name space). | Read other files or the repository; claim evidence it cannot prove (`""` means "not proven"). |
| Resolver (`internal/resolver`) | Turning evidence into candidates + confidence. Language-neutral. | Encode one language's rules; guess past authoritative evidence. |
| Index (`internal/index`) | Building the immutable `RepositoryIndex`: edges, completeness, candidate samples, fingerprint. | Create an edge the resolver did not make unique. |
| Context / impact / repomap | Selecting and ranking from graph edges and provider-stated declaration facts (repomap: `language.MapHinter` roles, declared types and member-scope units; `repomap/policy.go`). | Look names up themselves (a second resolver without the evidence); branch on a language name. |
| MCP (`internal/mcp`) | Target lookup, index reuse, path containment, serialization, output secret masking. | Pick one of several targets; reuse an index it has not proven current; return a result that has not passed the output sanitizer. |

Composition: `internal/languages` is the one place that wires providers into
the registry (adding a language = one spec). `internal/testfiles` is the one
authority on "is this a test file?" — no consumer keeps naming rules of its
own. It is the one deliberate place outside providers that knows per-language
conventions (file names only, no semantics).

**Language knowledge enters only as provider evidence.** Example: PHP
constructor injection (`$this->policy->m()` where the constructor assigns a
typed parameter) is proven by the PHP provider and emitted as `ReceiverType`
plus `ConfidenceCap: "strong"` when code outside the class could also write
the property; the generic resolver needed no change. Adding
`if lang == "php"` to the resolver instead would make every future language
inherit PHP's special cases and their soundness bugs. Naming a language is
not itself a violation: identities are compared within one language
(`resolver/identity.go`), completeness matches by language, and search
filters by it. The violation is generic code changing its behavior for one
language.
Authority: `language/provider.go` (field contracts),
`languages/php/ctor_properties.go`. No test can tell identifying a language
from depending on its semantics, so this boundary is kept by design review;
tests verify the evidence contracts it relies on (the resolver's tests drive
it with synthetic, provider-independent evidence).

**Scope that a name match cannot see** (Terraform). A Terraform address is
unique only within its module, and a module is a directory, so the provider
qualifies every declaration and every reference with the module directory it
derives from the file's own path — still one file, nothing else consulted.
Three generic extensions carry it, and none names a language:
`Extraction.IdentityOnly` (the file's symbols are reached only by qualified
identity, R0; no name stage reaches them, and a reference of the file without
identity is Unresolved), `ReferenceDraft.IdentityInRepository` (the identity
names a repository scope: no declaration there is Unresolved, not
`OutsideRepository`, and no other declaration can be its target), and
`SymbolDraft.MemberScope` / `ParameterScope` / `MembersOutside` (a
declaration states where its members and parameters live: a module call
with a local `source` names the child module's outputs, and — for a
reference marked `NamedArgument` — its input variables; a remote `source`
places both outside the repository; a named argument is never looked up as
a member). A module call
is therefore a member scope, not an import binding and not a call: bindings are
file-scoped and name files, while a module call is visible to the whole module
directory and names a directory. Dependencies form their own edge kinds
(`references`, `depends_on`), never `calls`. A module input argument is a
reference from the call to the child's variable declaration — the interface
it binds, like `module.x.out` to the output — so a change to the variable
reaches every call that passes it; the value flowing from the argument into
the child is not an edge (`TestInputBinding_*`). Authority:
`languages/terraform/provider.go` (header), `resolver/identity.go`. Tests:
`TestIdentityOnly_*`, `TestMemberScope_*`, `TestParameterScope_*`,
`TestIsolation_OtherLanguagesAreUnaffected`,
`TestGraph_NoEdgeCrossesAModuleBoundaryWithoutBinding`.
Danger: resolving a Terraform address by name "when the module has no match" —
`aws_vpc.main` of another module is a different resource.

**Paths, not names** (Rust). A Rust item is named by its path within its
crate, and which crate a file belongs to, and where in it, follows from
Cargo's default target layout — so the provider derives a crate key and a
module path from the file's own repository-relative path, reads no other
file (Cargo.toml included) and runs nothing. It reuses the Terraform
extensions unchanged: every file is `IdentityOnly`, every declaration's
`Qualified` is its path (`crate::storage::Repository`; another package's
crate `crates/util/crate::…`, a binary's `src/bin/x.rs/crate::…`), and a
reference carries `NameQualified` only when the language's lexical rules fix
the path — `crate` / `self` / `super` / `Self`, items and `use` bindings of
the enclosing block or module (never of an enclosing module), aliases, one
glob of a module or an enum of the same file (only what the module makes
visible at the glob: `pub` names from outside it). A module's `use` bindings are
stated as aliases (`Extraction.Aliases`: identity m::C names a::b::C), a
generic extension: R0 resolves an identity with no declaration of its own
through its aliases, or through an alias of a leading part of its path, so a
re-export of another file reaches the declaration; two aliases of one
identity leave both targets (Candidate), a chain longer than
`maxAliasDepth` or a cycle names nothing, and a declaration is never
replaced by an alias. An inherent method is `TYPE::name`, a
trait implementation's `<TYPE as TRAIT>::name`: two declarations, as in
Rust, and `Type::name` names the inherent one by Rust's own inherent-first
rule. A method call on a value (`v.m()`) never has an identity: its target
needs type inference and trait resolution. What the provider cannot see is
not guessed: a name an item-level macro, a derive or attribute macro of
another crate, a macro statement binding a name its invocation supplies, a
glob of another file or a `#[path]` module may bring is unresolved; `#[cfg]` alternatives share one
identity and resolve to Candidates; a `use` whose reading depends on the
edition (in Cargo.toml) is capped at Candidate — and is no alias; token trees
are not parsed.
Authority: `languages/rust/provider.go` (header), `languages/rust/layout.go`.
Tests: `TestIdentity_*`, `TestShadowing`, `TestMacros*`, `TestGlob*`,
`TestGraph_Soundness`, `TestGraph_CfgAlternativesAreCandidates`,
`TestAlias_*` (resolver), and the opt-in compiler comparison
`TestOracle_*` (docs/scripts/rust-oracle.sh).
Danger: binding a method call or an unresolved path by its last name — a
trait implementation's method and an inherent one of the same name are
different functions, and so are two crates' `crate::helper`.

## 2. Confidence

Confidence is an **ordered set of evidence classes, not a probability or a
score**. Never average, add, or promote it; caps (provider `ConfidenceCap`,
untyped receiver, module scope, unknown participant) only lower it.

| Class | Known | Unknown | Graph edge |
|---|---|---|---|
| Exact | The one target, by the language's own scoping as modelled: same container/file, import or module binding, qualified identity (incl. a module-directory scope), a member scope the receiver declaration states, member lexically contained in an Exact-identified type. | Nothing within the model — but it is static evidence, not compiler proof. | yes (if the only candidate) |
| Strong | The one target, by weaker evidence: unique name in the repository, same directory, receiver-name match, receiver-name attachment, provider cap — except in package-scoped files, where only the file's own package (directory and `Package`) is Strong evidence. | Whether something outside the repository/model shadows it. | yes (if the only candidate) |
| Candidate | Plausible targets (one or several), kept as evidence. | Which one, or whether another unseen target exists. | **never** |
| Unresolved | No candidate. | The target. This is a correct, sound answer — not a gap to fill. | never |
| `OutsideRepository` (flag on Unresolved) | Authoritative evidence (qualified identity, declared receiver type, import binding, a receiver whose members are declared outside) places the referent outside the repository's declarations. An identity that names a repository scope (`IdentityInRepository`) never does. | — | never; and it is *known*, so it is not counted as unattributed (it is reported as `outsideRepository`) |

Strong is a closed-world claim ("the only `X` here"). That is why module-scoped
files cap proximity/uniqueness at Candidate and why `OutsideRepository`
exists: a vendor `Request` must never become the repository's own `Request`.
Authority: `resolver/confidence.go`, `resolver/result.go`
(`HasUniqueTarget`), `resolver/evidence.go`, `TestConfidenceCap_OnlyLowers`,
`TestOutsideRepository`, `TestR4_*`, `TestModuleScoped_*`.

**Package-scoped files (Go) resolve by package scoping, never by spelling.**
A provider that sets `PackageScoped` states its language's rules: a name
without a receiver is a declaration of the file's own package (same file:
Exact; same directory and `Package`: Strong) or of a dot-imported package
(Exact, exported only); a receiver naming an import is that package (Exact,
exported, non-test files only). An import path names the repository directory
it ends with, under a prefix two import paths establish (or the only candidate
prefix ending in the root directory's name); importing the prefix itself
counts as one of the two when the root directory's package is named after its
last element, so a module checked out under another name still maps. A
prefix without a host (no dot in its first element: `encoding`, `log`) may be
the standard library's, whose paths coincide with repository directories in
twos (`encoding/json`, `encoding/xml` beside `json/`, `xml/`): only the root
directory's own name establishes it. An
import path naming no repository directory is `OutsideRepository` only when
nothing in the repository could be it: one ending in a repository directory,
or naming the root package, without an established prefix is missing
evidence — Unresolved, without that claim. A receiver declared as `pkg.T`
(R2) is looked up through the import of `pkg` the same way. No
repository-wide name stage applies: a local function value, a builtin or an
external declaration is never a same-named repository declaration. The
provider caps (Candidate) a name a local declaration or a type parameter
shadows at its position (`languages/golang/scopes.go`); a capped receiver is a
value, so the resolver reads it neither as an import nor as a type. Besides
calls and constructions the provider records type uses, value references and
writes (`languages/golang/refs_values.go`) — only names that can denote a
package-level declaration: no local, no predeclared name, no declaring
position, no struct-literal key, no field selection, nothing inside a parse
ERROR. Measured against independent `go/types` oracles: the callee oracle
(`languages/golang/oracle_test.go`) and the reference-level oracle
(`languages/golang/oracle_refs_test.go`, every identifier the type checker
resolves, by role). Authority: `resolver/packagescope.go`. Tests:
`TestPackageScope_*`, `TestGoOracle_SyntheticModule`,
`TestGoRefOracle_SyntheticModule`, `TestScopes_ShadowingFollowsGoScopes`,
`TestGoReceiverType_*`.
Danger: "it is the only `cancel` in the repository" — in Go an unqualified
name never denotes another package's declaration; and "the import found no
package, so it is external" — an unproven prefix is not proof.

**A receiver's type is derived from declarations, never guessed.** Where a
provider cannot prove a receiver's type from local syntax, it states how the
receiver is built (`ReferenceDraft.ReceiverChain`: `x := NewServer()` is
"the result of calling NewServer", `s.repo` "field repo of s", a range
variable "an element of xs") and, for every declaration, the types its own
text writes (`Extraction.TypeFacts`: struct fields, embedded types, result
types, underlying and aliased types, a package variable's type or
initializer). The resolver evaluates a chain step by step by the file's own
scoping; every step must name exactly one declaration — a name declared
twice in its package (build-tag variants, several programs in one
directory), declared outside the repository, or a result typed by a type
parameter ends the evaluation, and the reference resolves as an untyped
receiver (at most Candidate). A chain proves at most Strong. Member lookup
on a proven type finds the type's own member first; otherwise the members
its embedded types promote, depth by depth: the shallowest depth decides,
two members there are ambiguous, a field hides a method, and an embedded
type whose members are unknown stops the search above that depth and leaves
a member found beside it a Candidate. An interface's methods are symbols of
their own (the static target of a call through the interface; dynamic
dispatch is not guessed), and its members include those of the interfaces
it embeds. A selector that may be a field read (`x.f`, fields are no
symbols) is recorded `Speculative`: kept only if it resolves to a candidate
(a method value, a method expression), dropped by the index otherwise —
never counted as unresolved, never resolved by a name match. Authority:
`resolver/typefacts.go`, `language/typefacts.go`,
`languages/golang/chains.go`, `languages/golang/typefacts.go`. Tests:
`TestTypeFacts_*`, `TestGoRefOracle_MethodMatrix`, `TestGoReceiverChains`,
`TestGoTypeFacts_*`, `TestSpeculativeReferences`.
Danger: "the variable's type is probably the function's", or resolving a
promoted method past an embedded type of unknown members — a type parameter's
instantiation, a build-tag variant or an external embedded type decides
which declaration it is.

**Names resolve within one name space.** Every name-based stage (same
directory, receiver-name match, type receiver, repository-wide uniqueness and
candidate sets, qualified-name suffix, legacy import paths, members attached
by receiver name, package scoping) sees only declarations of files of the
referencing file's name space: its language, or — for a provider implementing
`language.Dialect` — the language it is a dialect of (TSX: TypeScript). The
lookups are keyed by name space (`resolver/lookup.go`, `nameKey`), so another
language's declaration is never a candidate at any confidence and never
counts toward a candidate set; a reference only another language declares is
Unresolved. Explicit evidence names its targets itself: module bindings
resolve to the files the provider lists (TypeScript lists `.ts`/`.tsx`, so
TypeScript and JavaScript files do not import each other) and qualified
identity is keyed by language. `FileIndex.NameSpace` is
`language.NameSpace` of the file's provider, set by the one conversion
(`index/builder.go`, `newFileIndex`) that the index builder and
`index.NewFileIndex` (which takes the provider) share; a hand-built
`FileIndex` without it has its language as its name space. Tests: `TestNameSpace_*`,
`TestNameSpaces_SameForEveryFileIndexPath`,
`TestNameSpaces_NoRelationJoinsTwoNameSpaces`,
`TestMCPNameSpaces_NoCrossLanguageRelations`.
Danger: "JavaScript and TypeScript interoperate, so let names match across
them" — a name match is no import; a JavaScript `run()` is not a TypeScript
`run`.

## 3. Soundness invariants

Each is enforced by tests; the "danger" line is the change that looks like an
improvement and is not.

1. **Never choose among candidates.** Several viable targets are ambiguity,
   and ambiguity is evidence. Picking `Candidates[0]` is fabrication.
   Applies to resolution (`pickBest` downgrades to Candidate), MCP target
   lookup (`mcp/target_lookup.go`), and the context-quality harness.
   Tests: `TestAmbiguity_*`, `TestMCP_*_AmbiguousRejected`,
   `TestMCP_Relations_AmbiguousNotMerged`, `TestEvaluate_AmbiguousTargetIsError`.
   Danger: "helpfully" returning the first/most likely match.

2. **Only a unique Strong-or-Exact resolution from an identified container
   becomes a graph edge.** Candidates are reported (samples, counts), never
   edges; an unknown reference kind never forms an edge.
   Authority: `index/builder.go` (`resolve`, `containerSymbol`, `edgeKindFor`).
   Tests: `TestUniqueTarget_AmbiguousResolutionProducesNoEdge`,
   `TestEdgeContainerAmbiguousInFileCreatesNoEdge`, `TestGraphBuilder_KindMapping`.
   Danger: lowering the edge threshold to "improve recall".

3. **Authoritative evidence never falls back to heuristics.** Qualified
   identity (R0), import bindings (R1) and declared receiver types (R2) decide
   alone: zero matches is Unresolved (usually `OutsideRepository`), several is
   Candidate. Similarity must not override an identity the language fixed —
   `use Vendor\Request` is not `App\Models\Request`.
   Authority: `resolver/identity.go`, `resolver/receivers.go`, `resolver/resolver.go`
   (`ResolveReference` doc). Tests: `TestQualifiedIdentity_NoFallbackToHeuristics`,
   `TestR2_UnresolvedTypeDoesNotFallBack`, `TestBinding_ExternalAndPathAliasNeverFallBack`.
   Danger: "if exact lookup finds nothing, try the old name match".

4. **Receivers constrain.** An explicit type receiver (`User::create()`) only
   reaches members of that type; a receiverless name never resolves to a member;
   an untyped receiver is capped at Candidate even when one match exists.
   Tests: `TestKnownIssue_StaticReceiverFalseExact` (pins the fix),
   `TestFreeNameNeverResolvesToMember`, `TestR4_UntypedReceiverUniqueRepoIsNotStrong`.

5. **An unknown structural participant must not disappear behind a known
   candidate.** Inherited/trait member lookup follows only uniquely
   identified, repository-declared relations, nearest first: own → traits →
   nearest parent → interfaces. That order mirrors PHP member dispatch
   (language semantics); restricting it to identified participants is Ark's
   approximation; the **stop conditions are the soundness boundary**: a
   participant outside the repository or ambiguous, a trait adaptation
   (`insteadof`/`as`) naming the member, a private supertype member, or a
   cycle → no target. An identified trait member beside an unidentified trait
   is only a Candidate (the unseen trait may declare it too).
   Authority: `resolver/inheritance.go` (header). Tests:
   `languages/php/inheritance_test.go` (`TestInheritance_UnknownTraitParticipant`,
   `TestInheritance_EvidenceGaps`, `TestInheritance_PrivateParentMemberIsNotInherited`).
   Danger: skipping an unknown parent/trait "to find the member anyway".

6. **Unknown is not empty.** An empty caller list is a true zero only when the
   graph is complete for that symbol. Completeness counts references that may
   involve a symbol without being its edge (candidate, unresolved same-name,
   sourceless); every graph result reports it (`unattributed`). Its
   population is the repository's declarations: `unattributed: 0` means no
   *repository* symbol is missing from the edges, not that nothing else is
   called. So every observed edge-kind reference inside a symbol is exactly
   one of: edge, unattributed, **unresolved** (no candidate, not proven
   external, and no indexed symbol can be its target — an external, built-in
   or run-time computed name, or a declaration the provider does not extract)
   or **outside repository**. The last two are reported for the symbol's
   outgoing direction (`unresolved`, `outsideRepository`, a bounded
   `unresolvedReferences` sample) and never attributed to any symbol: they
   cannot be anyone's caller. A call whose name is computed at run time is
   observed as a `Dynamic` reference (Unresolved by construction) where the
   provider sees the syntax. Syntax a provider does not observe is in no
   count; no output claims to bound it (provider gap Q7).
   Authority: `index/completeness.go`. Tests: `TestCompleteness_Rules`,
   `TestCompleteness_OutgoingPartition`, `TestDynamicName_NeverResolves`,
   `TestGetCallers_ResultSemantics`, `TestGetContext_CallersAndCompleteness`,
   `TestUnresolved_FieldCase_MakeWithIsReported`, `TestUnresolved_CrossLanguage`.
   An unresolved reference with `IdentityInRepository` is never same-name
   attributed: its identity already excludes every other declaration
   (`TestCompleteness_IdentityInRepositoryIsNeverSameNameAttributed`).
   Danger: dropping the count when it is 0 or "noisy"; adding unresolved
   references to `unattributed` (or to a caller's count) to "be safe" —
   that attributes a reference to symbols it cannot denote; resolving a
   `Dynamic` name by its display text.

7. **Truncation is evidence.** Every bounded output says it was cut
   (`total`/`truncated`, `TargetTruncated`, `TruncatedItems`), and bounded
   samples still count everything. A bounded traversal (export chains,
   structural depth) that hits its bound contributes nothing, never a guess.
   Tests: `TestRelations_CandidateSampleBounded`, `TestFindSymbol_TruncationAndOrder`,
   `TestTargetTruncatedFlagSet`, `TestReExport_DepthBounded`, `TestReExport_CycleIsSafe`.

8. **A declaration is its own symbol.** A `SymbolID` is derived from
   (language, file, kind, qualified name), plus — for the second and later of
   a file's namesakes (several Go `init`, a function defined twice) — their
   position-ordered ordinal (`symbol.NewDeclarationID`); no position enters
   it otherwise, so editing other code never changes an ID. A reference's
   container is the declaration of that name whose range contains it, and
   only if exactly one does. Identity is not resolution: a reference to a name
   declared twice stays ambiguous. The cache stores extraction drafts, never
   IDs. Tests: `TestDeclarationIdentity_*`, `TestSymbolIdentity_*`,
   `FuzzDeclarationIdentity`.
   `Symbol.Parent` is an identity too: the ID of the one declaration of the
   file that carries the provider's parent name and encloses the symbol, or
   empty (no such declaration, several, or a cycle); `ParentQualified` keeps
   the provider's statement for the resolver. Tests: `TestParentIdentity_*`, `FuzzParentIdentity`.
   IDs stay 64 bits (stable, short, already exposed); should two distinct
   declarations ever share one, the index is not built
   (`index.IdentityCollisionError`, diagnostics `symbol_id_collision`) — no
   map, edge, parent or context is ever derived from a merged ID, and keeping
   one of the two would change what names resolve to. Authority:
   `index/identity.go`. Tests: `TestIdentityCollision_*`, `FuzzIdentityCollision`.
   Danger: keying anything by (file, qualified name) alone — it merges
   namesakes, mixing their sources, callees and callers; deriving an ID from
   a name instead of finding the declaration.
   A reference's `ReferenceID` is derived from (language, file, kind, name,
   span) — its end included: references of one name and kind may start at
   one position (Go `f(x).V.f()`, JavaScript `$(a).data().data()`), and an
   ID two references shared would join one's resolution to the other
   (`reference.NewReferenceID`). It is unique within an index — the builder
   leaves out a reference whose ID another already has (a provider stating
   one reference twice, or a hash collision) with a
   `reference_id_collision` warning — and the same
   for the same sources, built cold, from the cache or not (IDs are not
   cached; they are derived from the cached drafts); it is no identity
   across Ark versions — it changes when an extraction states another span,
   name or kind — and no tool takes it as input: `find_references` reports
   it as an opaque `id`, never a `symbolId`. Authority: `index/builder.go`
   (`uniqueReferences`). Tests: `TestReferenceIdentity_*`,
   `TestNewReferenceID_*`, `TestFindReferences_IDContract`.
   Danger: deriving a reference's ID from its start alone, or from anything
   two references can share.

**Diagnostics are what the index reports, not a completeness proof.** A
diagnostic says Ark could not analyze part of a file (`parse_error`: the
parser rejected a region, which is then not analyzed as written — the source
itself may be valid, as grammars reject some valid code) or a whole file (unreadable or a provider failure:
the file is skipped, never counted as indexed). It names the
repository-relative file and never an OS path; its `Code` is set only by the
producer that knows it (`treediag`, a provider, the index) and is otherwise
unclassified — never inferred from the message. Graph tools add an
`indexDiagnostics` summary only when there are diagnostics, and its absence
claims nothing: files of formats no provider handles are not examined at
all. Diagnostics are distinct from unresolved references (parsed, no known
target) and from tool errors (`isError`). Every provider reports its
parser's ERROR / MISSING nodes, and none on the contract's valid corpus
(provider contract). Authority: `languages/internal/treediag/treediag.go`,
`mcp/tools_diagnostics.go`. Tests: `TestDiagnostics_*`,
`TestIndexFailureDiagnostics`, `TestNewWithCache_WarmKeepsDiagnostics`,
`TestNewWithCache_DiagnosticsFollowContent`.
Danger: reading "no diagnostics" or "unresolved: 0" as "everything was
analyzed"; dropping a diagnostic from a warm cache.

**One parse path, measured against the reference runtime.** Every provider
and the syntax tools parse through `tsparse.Parse`: gotreesitter's
production route, and — only when that tree has an error — its admission
candidate route, then its forest route, each kept only if it has none.
Differential testing against the reference Tree-sitter runtime (same grammar
commits) is the evidence: the production route rejects some valid code (lost
declarations, false `parse_error`) that the other routes parse identically
to the reference, field names included, while switching routes everywhere
changes more correct trees than it fixes. A tree without an error is a
complete derivation by the grammar, so the fallback recovers a derivation
and never invents one; when every route fails, the production tree and its
diagnostics stand. Trees it does not return it releases. The forest route's
cost is not predictable (a run of comments after an ambiguous prefix makes
it cubic, tens of seconds per file), so for Go and Rust it runs only after a
probe — the forest route on a copy with the production tree's comments
(outside any ERROR) blanked, offsets kept — accepts. The claim the skip rests
on: a comment is an extra, admitted between any two tokens like whitespace,
and Go and Rust define their tokens independently of the parse, so every
derivation has the same tokens and the copy derives exactly when the source
does — provided a comment's line breaks are blanked too (the comment is one
extra; a kept line break would be Go's statement terminator, which a review
found). Languages whose parse decides the tokens (JavaScript regular
expressions, JSX text, PHP `?>`, Python indentation) are not probed. The
probe only decides whether to run the forest route; its tree is never
returned, so a wrong probe could only keep the production tree. The
production route's parsers come from gotreesitter's ParserPool (building a
parser costs more than parsing a small file); a panic drops the pool.
Authority:
`tsparse/tsparse.go`. Tests: `TestParse_*` (the contract), `TestRoute_*`
(gotreesitter v0.55.1 route behavior — the upgrade gate), `TestProbe_*`
(the probe agrees with the forest route, never skips a derivable source,
runs only for Go and Rust; linear cost), `TestBlankComments*`,
`TestParse_PooledParsersShareNoState`,
`TestParserRecovery_PHPDestructuring`.
Danger: switching the process-wide route "because it fixed a file", or
dropping diagnostics instead of recovering the parse; returning the probe's
tree (it has no comments) or blanking what an ERROR holds (that can make
underivable source derivable).

**An error-free tree is not the language's reading of ambiguous syntax.**
Where the grammar is ambiguous the parser may pick a derivation the language
does not, with no error (differential testing: Go 33, TypeScript 37 files of
the real corpora). The extractors therefore read those shapes by the
language's own rules, whichever derivation the tree holds:

- Go `f[x](...)` / `r.f[x](...)` / `T[X](v)` (generic call, conversion, or a
  call of an element of a slice / array / map of functions): only what the
  file proves decides — never the parser's choice of reading. A subscript or
  callee that is a value (a local, a var / const of the file) makes it an
  element call: no reference. A subscript that is a type (predeclared, a type
  or type parameter in scope, type syntax, several subscripts), or a callee
  that is a function / type of the file, makes it a call. Otherwise the
  reference can denote only a function, method or type
  (`ReferenceDraft.TargetKinds`): resolved to a variable or constant — an
  element call after all — it is Unresolved. `T[X]{...}` constructs `T`. Tests:
  `TestExtract_SubscriptedCallsFollowGoRules`,
  `TestGraph_SubscriptedCallOfALocalIsNoEdge`,
  `TestGraph_ElementCallsOfPackageValuesAreNoEdges`.
- TypeScript `f<T>(x)` derived as the comparisons `(f < T) > (x)`: a call
  when the text is type arguments followed by `(` — TypeScript's own rule,
  checked with a type grammar narrower than TypeScript's (lexical rules,
  reserved words and line-break rules included), so a comparison is never
  made a call; `TestFidelity_TypeArgumentsMatchCompiler` checks every text it
  accepts against the compiler (`languages/typescript/generic_call.go`). Type parameters of nested
  signatures, mapped-type keys and `infer` names scope like type parameters
  and are never type uses. Tests: `TestExtract_GenericCallReadAsComparison`,
  `TestExtract_ComparisonsAreNotGenericCalls`,
  `TestExtract_TypeScopedNamesAreNotTypeUses`,
  `TestGraph_TypeScopedNamesAreNoEdgesToSameNamedTypes`.

The independent oracles are the languages' own front ends:
`TestFidelity_RepositoryReferencesMatchGoAST` (go/ast + go/types, this
repository; `ARK_GO_FIDELITY_ROOTS` for others) and
`TestFidelity_TypeScriptCompiler` with `TestFidelity_TypeArgumentsMatchCompiler`
(the TypeScript compiler; opt-in locally via `ARK_TYPESCRIPT_MODULE`,
required in CI: `.github/ts-oracle/run.sh` installs the pinned compiler and
fails on a missing or wrong-version compiler, a skip or a mismatch — `make
ts-oracle` runs it locally). Remaining gaps:
`internal/conformance/IMPROVEMENTS.md`, Q8.

**Secrets are masked at the output boundary, and only there.** Every tool
result, resource and error leaves through one sanitizer
(`mcp/sanitize.go`) that applies the repository-dump rules
(`secrets/mod.go`) to repository text. Providers, the index, the resolver,
the graph and the cache see the source as written, so masking never changes
a symbol, an ID, a resolution or an edge. JSON results are rewritten token by
token: keys, numbers, structure and order are kept; identifier values (paths,
names, IDs, enumerations) are never masked, because clients pass them back;
a result with nothing to mask is returned byte for byte; unknown keys and
unknown tools are masked. Masking is on unless the server is started with
`--mask-secrets off` (with a warning); a request cannot turn it off
(`mcp/masking.go`, `TestFileContent_SecurityOverridesStillIgnored`).
Tests: `TestSanitize_*`,
`TestMCPSecretMasking_NoDetectableSecretLeaves`,
`TestMCPSecretMasking_IdentifiersAreNotMasked`,
`TestMCPSecretMasking_HTTPConcurrent`, `TestMCPSecretMasking_Settings`.
A value that only names another value is kept as written: one GitHub
Actions context reference (`${{ secrets.NAME }}`) anywhere, and a Terraform
reference (`var.x`, `TYPE.NAME.attr`, …) only in a result built from one file
the request read as a `.tf` file — decided from the gate's symlink-free
resolution of the file read (`mcp/masking.go` `fileMaskSyntax`,
`CallToolResult.maskSyntax`), never from the requested name or the content;
any other value, an expression included, is masked whole, and every secret
pattern still applies to the line, as written, before any value is masked
(`secrets.MaskAllIn`). No part of a value is left out of its mask: an
unquoted value runs to the end of its line short of a separator that ends
it, a quoted one ends at its quote only when nothing continues it, a key may
be quoted or an index; a value never ends inside a line break (`\r\n`) nor
runs past one, and nothing written after a kept reference is kept with it; a
heredoc or YAML block scalar value is masked on every line of its body
(`valueBlock`). Only text whose `␤`s are line breaks — arklite output, each
file masked before its lines are joined — is masked with `␤` as a line break
(`secrets.MaskCompactIn`). Tests: `TestMaskAllIn_*`,
`TestMaskCompactIn_LineBreakSymbol`, `TestMasking_KeepsReferencesNotValues`,
`TestMasking_ValueBoundariesAtTheOutput`,
`TestMasking_NoPartOfAValueLeaksThroughAnyTool`.
Danger: masking during extraction "to be safe" — it changes what names
resolve to and what the cache holds; or a string replace over the
serialized response — it can break JSON and IDs; or trusting a file's name
or content for its syntax — a `.tf` link to a `.env` file would unmask it.

**One masking policy, one pipeline, for masking and finding.** The rules
are compiled once into an immutable `secrets.Policy` — the built-in rules
(`secrets/rules.go`, each with a stable ID pinned by
`TestBuiltinRules_StableIDs` and `TestBuiltinRules_Documented`) less those a
repository disables, plus those it adds — and applied by one pipeline
(`secrets/engine.go`): PEM blocks, then per line the pattern rules in order
and the assignment rules, each step finding spans of the text the earlier
steps left. `Policy.Mask` replaces them; `Policy.Find` reports them, mapped
back to the original text — so `ark secrets-scan` finds exactly what masking
masks, never by a second detector. Without configuration the policy is
`secrets.Recommended()`, byte for byte v6.1.0's masking
(`TestRecommended_MatchesV61`, `FuzzRecommended_MatchesV61` against the frozen
oracle `secrets/internal/v61`). The configuration (`.ark-secrets-rules.toml`
or `ark-secrets-rules.toml` at the root) is read by `internal/secretpolicy`
from the pinned root without following a symlink, for the dump once per run,
for the MCP server per request and compiled only when its bytes change
(`mcp/secret_policy.go`; a request masks everything with the policy it took:
`TestSecretPolicy_RequestsNeverMixConfigurations`). The MCP server applies
the entries that disable built-in rules only with the operator's
`--secret-rule-overrides on` — repository text must not weaken the masking
its operator runs — and fails closed on a configuration it cannot read or
compile: results with repository text are replaced by the error, and an
error message is withheld rather than masked by fewer rules
(`TestSecretPolicy_*`, `TestSecretPolicy_ErrorMessages`). A file's syntax for masking comes from
`secrets.SyntaxForFile` on the regular file read, in the dump
(`core/masking.go`) as in the server (`fileMaskSyntax`). Masking never
decides access and never reaches analysis. Danger: a second detector for
the scan (it drifts from what masking masks); falling back to the
recommended rules on a broken configuration (rules a user added vanish
silently); reading the configuration through a symlink or by path (a swapped
link would choose the rules).

**`.arkignore` decides what the MCP server may read, where files are
reached.** One policy (`mcp/access_policy.go`) — every `.arkignore` under the
root, read with the dump's matcher (`libgitignore`) — is applied at the path
gate (`resolveToolPath`, also for a symlink's target), in every file walk
(`core.CanBoaded` / `CanEnterDir` through `Option.AccessExclude`, and the
symbol and reference searches) and in the index walk
(`index.NewWithCacheExcluding`): an excluded file is never read, so nothing
derived from it exists to leak. The index's freshness fingerprint uses the
same filtered walk, so a rule change that alters the file set rebuilds it. It
is independent of masking. Tests: `TestMCPArkignore_ExcludedFilesAreUnreachable`,
`TestMCPArkignore_FollowsRuleChanges`, `TestMCPArkignore_DumpExcludesTheSameFiles`.
Danger: filtering excluded files out of responses — callers, counts,
candidates, search hits and context would still be derived from them.

**The rules match names as the file system lists them.** The rules are
case-sensitive; a case-insensitive file system (macOS by default, Windows)
reaches `secret.txt` by `SECRET.TXT`. So the path gate decides — and hands
the tool — the path's canonical form, and walks decide a symlink's target
by its canonical form (`fsroot.Canonicalize`, used by `canonicalCheck`,
`accessPolicy.excludesCanonical` and `core.aliasIgnored`): each component
is renamed, through the directory handle the lookup used, to the listed
entry whose identity the lookup reached (`internal/fsroot/canon.go`); the
comparison of names is the file system's own, never guessed. Directories
known to compare names exactly (a case-sensitive volume and an ASCII name on
macOS; ext4, xfs, btrfs, tmpfs without casefolding on Linux) are not
listed. A name no entry, or two entries, account for is refused. Walk
entries are named from listings, so a walk is canonical once its start is.
Tests: `TestC1_*`, `TestCase_PathGate`, `TestMCPCase_ExcludedFilesUnreachable`,
`TestCLICase_DumpExcludes`. Danger: lower-casing names or rules — Unicode
case and normalization, 8.3 short names and per-directory case sensitivity
are the file system's to decide; or canonicalizing a string and opening
another path — the name decided would not be the object read.

**A file a client names is decided and read in one operation.** The path
gate decides a path by name; opening it by path afterwards could open
another object (a symlink swapped in, a root link retargeted). So the tools
that read a named file, and the snippets index-based tools read back, read
through the request's access (`mcp/request_access.go`): the root pinned once
per request (`fsroot.Pin`) and a scoped policy snapshot over it
(`accesspolicy.BuildScoped`), which captures the `.arkignore` files of the
directories a decision passes through those directories' handles
(`fsroot.ReadChain`) and decides as the whole tree's rules would
(`TestScopedDecidesAsFull`); `Snapshot.ReadFile` / `Stat` resolve, open,
decide and read the same object. The gate stays in front (its messages,
its checks); the snapshot has the last word, and nothing falls back to a
read by path. The access is released when the request ends
(`beginRequest`). Tests: `TestB1_*`…`TestB10_*`, `TestB4_SwapBackBeforeTheDecision`,
`TestB5_*`, `TestB6_*`, `TestRequestAccess_ReleasedWhenRequestEnds`. Danger:
checking with the snapshot and then opening by path — the window is the one
this closes; building the whole tree's snapshot per request — tens of
milliseconds per file read on a large repository; or a request that never
ends (a handler from `forRequest` without `beginRequest`'s end) — its pinned
root stays open, which on Windows keeps the repository from being deleted.

**Walks, the index and the dump see only what the policy decided.** They
keep their own logic and output but reach files through
`commandline.Option.FS` — a walker of `internal/safewalk` over a pinned tree
and a policy snapshot (the request's, an index build's own, the dump's) —
in the shapes they used (`Walk`, `WalkDir`, `ReadDir`, `ReadFile`, `Stat`):
entries listed through directory handles are decided before the caller
sees them, and a reported file is read through the walk's resolution
(`fsroot.Entry.Resolved`, `Snapshot.ReadResolved`); the entry being
reported is read through the handle it was listed from, opened only if it
is still the object its examination found (`fsroot.Entry.ReadAs`: the
identity `Describe` found, compared again on the opened file). The index
reads its sources the same way (`index.WithFS`); a request's freshness check
walks under the request's snapshot, so an index is served only when its
fingerprint is what the request may read, and the text the request then
reads back from the index's files (snippets, generated-package markers) is
read through that walk's resolutions (`safewalk.Walker.ReadReported`).
Tests: `TestC3_*`, `TestWalkMatchesFilepathWalk`, `TestEntryReadAs`,
`TestWarmIndexReads_*`. Danger: deciding a walk entry and then reading its
path again (`os.ReadFile(path)` in a walk callback) — the window this
closes; reading through a resolution without comparing identities; or
serving an index on its build-time check alone after the rules changed.

**The rules are read for every request, once, and never trusted from a
cache.** Each request (`ToolsHandler.forRequest`) has one
`libgitignore.IgnoreReader`, which reads every rule file at most once: the
path gate reads only the rule files that can apply to the named path
(`IgnoreReader.For`: the root's, those of the directories above it, its own),
walks read the whole repository's (`All`), and both see the same version of
each file. Compiled rules are reused across requests only under the
fingerprint (paths and SHA-256 of the contents) of the bytes they were
compiled from. Matching is indexed by pattern directory
(`GitIgnore.MatchesRel`), deciding exactly as the reference loop
(`MatchesPathHow`). Tests: `TestAccessPolicyCache_*`,
`TestIgnoreReader_ForDecidesAsAll`, `TestIgnoreReader_OneVersionPerReader`,
`TestIgnoreFiles_SourcesCompileTheirOwnFiles`, `TestMatchesRel_EqualsMatchesPath`.
Danger: skipping the per-request read (a timer, mtimes, a watcher) — a rule
change would not apply to the next request; or compiling from a second read
— a fingerprint would name another version's rule.

**A request walks the repository once.** The walk that reads the rule files
(`IgnoreReader.All`, which also walks a symlinked root's target, in the
root's spelling) records its entries outside directories no index enters
(`CollectEntries(index.SkipDirName)`); the request's index freshness check
replays that listing with the walk's own decisions (`index.sourceStep`,
`SourceFingerprintListed`) instead of listing the directories again, so the
access policy and the fingerprint describe one walk. File contents are still
read and hashed: content, not metadata, decides freshness. Where the listing
cannot stand in — the walk failed, or the index root lies below a directory
whose entries were not recorded — the check walks as before; so does the
post-build verification (`indexCache.run`), whose timestamp must be its own
walk's. Tests: `TestSourceFingerprintListed_*`, `TestListedFingerprint_*`.
Danger: deciding freshness from a listing of another walk or request — an
index would be checked against directories the policy was not read from.

**A symlinked root is the directory it leads to; nothing else is followed.**
Walks that start at the processed root use `common.WalkRoot` /
`WalkDirRoot`: they walk the root's target and report paths in the root's
spelling, so rules anchored at the root match (`IgnoreReader.All`, the MCP
file tools, symbol and reference search, the dump). A walk that starts at
any other directory link does not enter it. Tests: `TestSymlinkedRoot_*`.
Danger: entering a directory link below the root — its entries' paths would
not be the paths the rules name (a file excluded as `sub/x` would be
listed as `dirlink/x`).

**The dump and the MCP server read ignore files one way.** Both build their
rules with `libgitignore` from the ignore files at and below the processed
directory (`Option.IgnoreRoot`: the dump's target, the server's root —
never the working directory), as two sources compiled separately
(`IgnoreFiles.CompileSource`, `RuleSet`): `.arkignore` (with additional rule
files) and `.gitignore`; either ignores, and a negation never crosses
sources. The access policy uses the `.arkignore` source only. Walk entries
are matched by absolute path, and a symlink is also matched by its in-root
target (`core.aliasIgnored`). Tests: `TestIgnoreSemantics_*`,
`TestCLIIgnore_*`. Danger: matching a path relative to the working directory
— rules from one directory applied to another; or one merged pattern list —
a `.gitignore` `!` would re-include what `.arkignore` excludes.

**Symlinks leading outside the root are not followed unless the operator
allows it.** By default the gate refuses a path that resolves outside the
root and walks skip a symlink whose target lies outside it (so it is not
indexed or cached). `mcp-server --allow-external-symlinks on`
(`Option.AllowExternalSymlinks`; no request can set it) admits paths inside
the root that resolve outside, with `.arkignore` applied to the path in the
repository. Tests: `TestMCPExternalSymlinks_*`. Danger: admitting a path
because its target is a symlink target — only paths inside the root, reached
through a link in the repository, may lead outside.

## 4. RepositoryIndex and index reuse

- **`RepositoryIndex` is immutable after `freeze`.** Accessors return copies;
  there is no lazy or query-time mutation. The MCP server shares one index
  across concurrent requests, so a mutation would be a data race and would
  make answers depend on query history. Tests: `index/immutability_test.go`,
  `TestIndex_ConcurrentReads`, `go test -race`.
- **An index is a deterministic function of (canonical root, provider
  configuration, every source file's path and content).** Its `Fingerprint`
  hashes exactly those inputs. This is why reuse is sound — and why
  determinism (§6) is load-bearing.
- **A stale index must never be returned as current.** Freshness is
  build → post-build fingerprint → publish. Timestamps are not evidence:
  mtimes can be preserved or coarse while content changes, and a build reads
  files one by one, so a file edited mid-build yields an index of no real
  repository state. Only a fingerprint taken *after* the build, equal to what
  it read, proves consistency; a reused index is re-fingerprinted per request.
- The shared build runs detached from any requester's context: one caller
  giving up must not kill the build others wait for. Failed, panicked or
  stale builds are never published; query options are never part of the key.
- **A build extracts in parallel and ingests in walk order.** The walk
  reads every source and adds it to the fingerprint in order; extraction
  (cache lookup, parse, cache store) runs on a small worker pool, bounded
  also by the source bytes in flight (a parse's memory grows with its file);
  results are
  ingested strictly in walk order and a provider's panic is raised again on
  the building goroutine (`index/extract.go`). The server's build is the
  sequential build's index, byte for byte. Tests:
  `TestParallelBuildMatchesSequential`, `TestParallelExtraction_PanicAndOrder`.
  Danger: ingesting in completion order, or reading sources off the walk.
- **The parse memo saves parsing, never reading or deciding.**
  `find_symbol` and `find_references` walk, decide and read every file
  through the request's walker on every request; the server's memo
  (`mcp/parse_memo.go`) keeps what parsing those bytes yields, keyed by the
  SHA-256 of the bytes and what else the result depends on (language,
  provider cache version, the file's name where the result embeds it).
  Owned by the server's handler, shared by its requests under a mutex,
  bounded (LRU over the bytes parsed); content is the key, so an entry is
  never stale and is never invalidated; a lookup needs the bytes, so it
  reveals nothing a request did not read. Tests: `TestParseMemo_*`.
  Danger: keying by path or metadata, or looking up before the read is
  decided.
- Extraction cache: a cache miss is preferable to a cache that lies. Change
  the provider's `CacheVersion` whenever extraction semantics change, and
  `cache.CurrentSchemaVersion` when the cached shape changes; read errors are
  misses.

Authority: `mcp/index_cache.go` (header comment), `index/sources.go`,
`cache/key.go`. Tests: `mcp/index_cache_test.go`
(`TestIndexCache_MidBuildChangeIsNeverPublished`,
`TestIndexCache_CancelledWaiterDoesNotBreakTheBuild`, `TestIndexReuse_*`).

## 5. Context Engine (`get_context`)

- The target is resolved unambiguously or the call returns candidates
  (§3.1); the target is always included, even over budget (`TargetTruncated`).
- Items come **only from graph edges** (Strong/Exact). Candidate callers and
  callees are never context; they surface only as `Unattributed*` counts
  (and outgoing references with no target as `UnresolvedCallees` /
  `OutsideCallees`).
  Type dependencies arrive as `uses_type` edges; the engine never searches
  names itself (`context/engine.go`, `collectCandidates`).
- Test files are excluded unless `IncludeTests`, via `internal/testfiles`;
  a test caller stays a caller in the graph and in completeness.
- Ranking is deterministic (score desc, then `SymbolID`). Ranker weights are
  tunable; the explainable, additive structure is the contract
  (`context/ranker.go`). The budget is an estimate (`EstimateTokens`).
- `depth` is not general BFS depth (see `Request.MaxDepth`).

Tests: `context/contract_test.go`, `TestContext_CandidateCallerIsCompletenessOnly`,
`TestContext_AmbiguousNoFabrication` (PHP), `internal/contextquality` scenarios.
Danger: filling spare budget with candidates or name matches.

**Search relevance is not resolution** (`search_context`). It ranks indexed
symbols by how their names match a partial identifier
(`search/symbols.go`, `MatchSymbols`: match type, then a stable key — never
input order) and attaches each top candidate's context, built by the engine
from that candidate's `SymbolID`. A rank never selects a target, never
enters target lookup, the resolver or confidence, and same-named candidates
stay separate; should two declarations ever share a `SymbolID` (§3.8), or
the engine return context about another declaration, none is given. `contextLimit`
(default 1) only chooses how many top candidates get context; the others
keep their rank, ID and location (`not_requested`, `context_limit`).
`maxTokens` bounds the whole serialized response: metadata first, then
contexts in rank order, trimmed or marked `omitted_budget`.
Authority: `mcp/tools_search_context.go` (header comment). Tests:
`TestSearchContext_SameNameNeverMerged`, `TestSearchContext_BudgetIsNeverExceeded`,
`TestSearchContext_MatchesGetContext`, `TestMatchSymbols_DeterministicUnderInputOrder`,
`TestSearchContext_ContextLimitSemantics`, `TestSearchContext_IdentityGuards`.
Danger: resolving the query text to "the best match" and building context
for it, treating context on rank 1 as a verdict, or passing the
per-response budget to each candidate.

**Impact traverses the graph, never names.** `analyze_change_impact` takes
direct dependents and dependencies from the target's edges and transitive
dependents from `graph.TransitiveCallerHops`. In both traversal directions the
next symbol is `edge.To` (a reverse edge stores the caller in `To`); each symbol
is reached once, the start never, breadth-first, and its distance is its real
hop count. A transitive entry's confidence is that of its path — the weakest
edge on it, never promoted; of several shortest paths the strongest is
chosen (a longer path never shortens the distance). Its evidence is its own
first hop only, never the path's evidence joined into one claim. Authority:
`graph/graph.go` (`Hop`), `impact/impact.go` (`ImpactEntry`). Tests:
`TestTraversal_*`, `TestPathConfidence_*`, `TestAnalyze_Transitive*`,
`TestImpact_ThroughModuleOutputs`.
Danger: choosing the next symbol by edge kind — it silently stops every
transitive walk at depth 1; labelling a transitive dependent with the
confidence of one edge of its path.

## 6. Determinism

Contract: identical inputs give identical output — resolver order and
candidate order, edge dedupe/sort, bounded samples (first N in resolution
order, totals count all), context ranking, MCP ambiguity listings, golden
serializers (byte-identical).

Why: (1) index reuse is only sound if a rebuild would produce the same index;
(2) golden snapshots detect semantic drift only if output is stable; (3) a
client compares answers across calls — samples and ambiguity lists must not
flap; (4) "the first one" under nondeterminism is a hidden candidate zero.

Not a rule: "sort everything". Iterate maps freely where the result does not
depend on order; sort or use a defined order where it does — at outputs, at
"first N" bounds, and at tie-breaks.
Tests: `TestDeterminism`, `TestIndex_DeterministicRepeated`,
`TestEngine_Deterministic`, `TestDefaultRanker_TotalIsBitIdentical` (a score is summed in key order, never map order), `TestMCP_Ambiguity_Deterministic`,
`TestIndexReuse_Deterministic`, `internal/golden` (`go test ./internal/golden -update` rewrites snapshots; review every diff as a behavior change).

## 7. Limits: non-goals vs. not yet implemented

**Architectural non-goals** (changing these is a design decision, not a feature):

- Executing anything from the analyzed repository — code, package managers,
  compilers, configuration (`SECURITY.md`) — or fetching what it refers to
  (Terraform registry / Git modules, providers): a remote module's outputs
  are `OutsideRepository`.
- Framework runtime semantics (Laravel container, Symfony, Doctrine, ...)
  inside a language provider. If ever added, it is separate evidence, not
  PHP language support.
- Compiler-equivalent module resolution: providers rank only a
  config-independent lexical subset (`language.ModuleCandidate`); `tsconfig`,
  Composer/PSR-4, `package.json` and `Cargo.toml` are not interpreted, and
  `cargo metadata` is never run.
- Expanding macros (Rust `macro_rules!`, procedural macros, derives): what
  an expansion declares or calls is not observed; a scope a macro may add
  names to is not trusted to be complete.
- Guessing dynamic dispatch (`$obj->$m()`, `new $c()`, `a[k]()`): Unresolved
  or Candidate by design. A provider may prove the one class a variable holds
  (`$c = Foo::class; new $c()`) from the same local rules as receiver types.
- Following class-strings into a consumer: `Foo::class` is a type use of
  `Foo`. Passing it to a container or factory (`make(Foo::class)`) is a call
  to that function, never a call to, or construction of, `Foo` or the class
  a binding would substitute.
- Inference results passed as proof: `ReceiverType` / `NameQualified` carry
  proven evidence only. Without control-flow analysis, a provider's local
  evidence comes only from an assignment that syntactically dominates the use
  (PHP: a whole statement of a `{ ... }` block, for uses after it in that
  block) and from no variable that code outside the function can bind;
  "earlier in the source" is not dominance. Tests: `TestFlowSoundness_*`. Any future inference must arrive as capped provider
  evidence, never as resolver guessing.

**Not implemented yet** (may be added if the invariants above still hold):
general type inference (a receiver's type is derived only through
declarations' written types — Go receiver chains — never inferred from
uses, assignments or generic instantiation); control/data-flow analysis;
explicit cross-language references (JavaScript importing a TypeScript file —
JavaScript emits no module bindings — or any other language pair); Rust
paths into another crate of the repository (the crate name is in
Cargo.toml) and glob re-exports through another file;
`ReferenceKind × SymbolKind` compatibility in name-based stages; trait
adaptations (`insteadof`/`as`) beyond stopping; reverse typed-relation context
(`extended_by`, ...); relation-aware ranking weights; deeper context traversal.
Per-language limits: `docs/language-support.md`. Provider conformance
gaps: `internal/conformance/IMPROVEMENTS.md`.

## 8. Verifying a change

CI (`.github/workflows/ci.yml`; `make lint` runs most of it locally) is the
quality contract: gofmt, `go vet`, `go test`, `go test -race`, staticcheck on
the intelligence core, and fuzz smoke tests. Within it:

- `internal/golden` freezes extraction and index output.
  `go test ./internal/golden -update` rewrites the snapshots; every diff is a
  behavior change to justify, not noise to accept.
- `internal/conformance`: `RunContract` must pass for every provider;
  `go test ./internal/conformance -run TestQualityCandidates -v` reports the
  open gaps in `internal/conformance/IMPROVEMENTS.md`.
- `internal/contextquality` and the per-language context tests measure
  context recall and fabrication.
- A change to a provider's extraction semantics bumps its `CacheVersion`
  (§4).

## 9. Keeping this file true

- Write here only invariants, reasons, and boundaries. Not counts, scores,
  benchmark numbers, test-case lists, roadmaps or history — they rot, and git
  keeps history.
- Changing an invariant on purpose: change its authority test and this file
  in the same change, and say so in the commit.
- Code comments mentioning "plan §N", "Phase N", "PR N", "PHP-N" or "D4"
  refer to working plans that are not in the repository; the comment's own
  text is the authority.
