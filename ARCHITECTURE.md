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

## 1. Pipeline and responsibility boundaries

```
Provider ──► index builder ──► resolver ──► graph + completeness ──► Context Engine / impact / repomap ──► MCP adapter
(1 file)     (NewFileIndex)    (evidence →   (edges only for          (consume the index;                 (target lookup,
                               confidence)    unique Strong+)          never resolve names)                 index reuse, output)
```

| Layer | Owns | Must not |
|---|---|---|
| Provider (`internal/languages/<lang>`) | Everything language-specific. Parses one file, consults nothing else, and states what the language's own rules prove as **generic evidence** (`language.ReferenceDraft`: `ReceiverType`, `NameQualified`, `ReceiverTypeQualified`, `ConfidenceCap`, `Dynamic`; bindings, exports, `ModuleScoped`). | Read other files or the repository; claim evidence it cannot prove (`""` means "not proven"). |
| Resolver (`internal/resolver`) | Turning evidence into candidates + confidence. Language-neutral. | Encode one language's rules; guess past authoritative evidence. |
| Index (`internal/index`) | Building the immutable `RepositoryIndex`: edges, completeness, candidate samples, fingerprint. | Create an edge the resolver did not make unique. |
| Context / impact / repomap | Selecting and ranking from graph edges. | Look names up themselves (a second resolver without the evidence). |
| MCP (`internal/mcp`) | Target lookup, index reuse, path containment, serialization. | Pick one of several targets; reuse an index it has not proven current. |

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

## 2. Confidence

Confidence is an **ordered set of evidence classes, not a probability or a
score**. Never average, add, or promote it; caps (provider `ConfidenceCap`,
untyped receiver, module scope, unknown participant) only lower it.

| Class | Known | Unknown | Graph edge |
|---|---|---|---|
| Exact | The one target, by the language's own scoping as modelled: same container/file, import or module binding, qualified identity, member lexically contained in an Exact-identified type. | Nothing within the model — but it is static evidence, not compiler proof. | yes (if the only candidate) |
| Strong | The one target, by weaker evidence: unique name in the repository, same directory, receiver-name match, receiver-name attachment, provider cap. | Whether something outside the repository/model shadows it. | yes (if the only candidate) |
| Candidate | Plausible targets (one or several), kept as evidence. | Which one, or whether another unseen target exists. | **never** |
| Unresolved | No candidate. | The target. This is a correct, sound answer — not a gap to fill. | never |
| `OutsideRepository` (flag on Unresolved) | Authoritative evidence (qualified identity, declared receiver type, import binding) places the referent outside the repository's declarations. | — | never; and it is *known*, so it is not counted as unattributed (it is reported as `outsideRepository`) |

Strong is a closed-world claim ("the only `X` here"). That is why module-scoped
files cap proximity/uniqueness at Candidate and why `OutsideRepository`
exists: a vendor `Request` must never become the repository's own `Request`.
Authority: `resolver/confidence.go`, `resolver/result.go`
(`HasUniqueTarget`), `resolver/evidence.go`, `TestConfidenceCap_OnlyLowers`,
`TestOutsideRepository`, `TestR4_*`, `TestModuleScoped_*`.

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
`TestEngine_Deterministic`, `TestMCP_Ambiguity_Deterministic`,
`TestIndexReuse_Deterministic`, `internal/golden` (`go test ./internal/golden -update` rewrites snapshots; review every diff as a behavior change).

## 7. Limits: non-goals vs. not yet implemented

**Architectural non-goals** (changing these is a design decision, not a feature):

- Executing anything from the analyzed repository — code, package managers,
  compilers, configuration (`SECURITY.md`).
- Framework runtime semantics (Laravel container, Symfony, Doctrine, ...)
  inside a language provider. If ever added, it is separate evidence, not
  PHP language support.
- Compiler-equivalent module resolution: providers rank only a
  config-independent lexical subset (`language.ModuleCandidate`); `tsconfig`,
  Composer/PSR-4 and `package.json` are not interpreted.
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
general type inference and return-type propagation; control/data-flow analysis;
`ReferenceKind × SymbolKind` compatibility in name-based stages; trait
adaptations (`insteadof`/`as`) beyond stopping; reverse typed-relation context
(`extended_by`, ...); relation-aware ranking weights; deeper context traversal.
Per-language limits: `README.md`, "Language Support". Provider conformance
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
