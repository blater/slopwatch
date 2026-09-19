# SHALLOW v4 implementation specification

> Controlling amendment: [P0 delivery directive](shallow-p0-delivery-directive.md).
> Semantic uncertainty must remain visible separately from the mandatory numeric
> rating. Older score-withholding and enterprise-benchmark deferrals below are
> superseded; semantic conformance and automated-fix safeguards remain distinct.

Status: revision 6; incorporates the independent revision-5 review.
Current release boundary: [interim release and capability backlog](shallow-v4-interim-release.md).
The user-approved interim release makes v4 the default, retains legacy behind an
explicit comparison flag and defers new semantic capabilities. Its scope amendment
governs release timing; the full-conformance requirements below remain open.
Date: 2026-09-17.
Definition: `responsibility-burden-v4`; r6 formula/flow baseline, opt-in policy revision
`r7` adds the implemented `supporting-contract-v1` applicability rule below;
`r8` adds bounded Java source scoring; `r9` corrects neutral-operation and
reachability credit, preserves helper extraction and marks bounded results as estimates;
normalized facts 3.
Parent: [remediation plan](shallow-measurement-remediation-plan.md).
Approved scope addition (implementation pending):
[legitimate structural roles and actionable scores](shallow-v4-legitimate-roles-backlog.md).
This backlog amends the applicability/no-op baseline below for supported cases and
must be integrated, versioned and tested before enabling the new default. The r6
formulas and fixtures remain the implementation baseline, not evidence that this
new work is complete.
Review: [findings](shallow-measurement-review-r5.json),
[dispositions](shallow-measurement-review-r5-disposition.md).

This is a bounded engineering policy, not empirical calibration. The motivating
jname checkout uses Lombok-generated members and injected clock/random interfaces;
without supplied definitions or resolved receiver sets its behavioral score may
remain unknown. Boundary/visibility fixes must still work. Numeric inherited-
generator behavior is required on a source-complete fixture, not guessed for jname.

Normative companions are the [adapter implementation contract](shallow-v4-adapter-contract.md),
[all-language adapter cases](evidence/shallow-v4/adapter-cases.json),
the [flow contract](shallow-v4-flow-contract.md),
[built-in registry](evidence/shallow-v4/builtin-registry.json),
[source/flow fixtures](evidence/shallow-v4/flow-cases.json) and
[scoring cases](evidence/shallow-v4/scoring-cases.json). Their revision/hash enters
the policy identity. The [reviewed specification](shallow-v4-implementation-spec-reviewed-r5.md)
and [old cases](evidence/shallow-v4/scoring-cases-reviewed-r5.json) are historical,
not alternative current rules. No research phase precedes implementation.

## Current Java delivery policy

The r8 interim amendment takes precedence over complete-flow requirements below
for ordinary Java boundaries: retain precise results when available, otherwise
score declared-source burden and observed connected responsibility patterns using
the same integer formula. Detail evidence identifies `bounded-static-v1` and its
semantic limits. This bounded measurement does not certify complete flow coverage
or authorize fixes requiring a complete inventory fingerprint. The full semantic
contract remains the incremental capability target.

## 1. Exact score and intended baseline

```text
B = O + 0.5*T + 0.25*A + 0.125*E + P + S + 2*L
H = minimum weighted union of hidden obligations across supported alternatives
SHALLOW = floor(100 * B / (B + 2*H) + 0.5)
```

Integer calculation: `B8 = 8*O + 4*T + 2*A + E + 8*P + 8*S + 16*L`,
`D = B8 + 16*H`, `SHALLOW = (200*B8 + D) div (2*D)`.
No intermediate rounding. Applicable boundaries have O >= 1; result is [0,100].

| Term | Normative meaning |
| --- | --- |
| O | Number of distinct service families, including normalized meaningful-type creation; not number of overload spellings. |
| T | Distinct normalized concepts exposed across all routes, counted once per boundary. |
| A | Required caller-supplied leaf slots on the selected usable route for each family. |
| E | Optional exposed leaf slots not required on that selected route, deduplicated across equivalent routes. |
| P | Required policy selections on the selected routes. |
| S | Caller lifecycle relations left by the selected routes. |
| L | Distinct boundary-owned mutable roots exposed through any public route. |

Responsibility weights: V (validation/error adaptation) = 1; R (resource lifecycle),
C (private state transition), Y (coordination), X (transformation) = 2 each.
Outcome/relevance facts U remain in evidence but have **zero scoring weight**.
A resolved identity, constant-return function or callable no-op has H=0 and scores
100: v4 finds no hidden responsibility. That is an explicit limited heuristic
baseline, not a claim those APIs can never serve a useful architectural purpose.
A data carrier or allocation-only creation contract is instead not applicable.

There are no role priors, confidence multipliers, LOC terms or language multipliers.
Leakage is charged in B only. Existing SHALLOW catalog threshold/weight/contribution
formula stays unchanged; raw-score and aggregation policy changes are versioned.

### Route-aware burden and overload monotonicity

A route is an externally callable entry signature plus its normalized bindings to
a service. Aliases, inherited references, transparent forwarding and fixed-default
convenience overloads share a family when they resolve to the same service. A guard
or cleanup wrapper around that service may be another route; keep its distinct
behavior alternatives. Similar names or return types alone never merge families.

For each family, let K be the union of exposed slot identities across its routes.
Select the route minimizing `2*A_route + E_route + 8*P_route + 8*S_route`, where
A counts required slots and E counts K minus that route's required slots. Resolve
ties by canonical route ID. Sum the selected terms across families; union T and L
across the whole boundary. A new default route covering the same service adds no O,
no new K or T, and cannot worsen burden: the old route remains a candidate. An
exact duplicate overload adds no cost and no responsibility. A genuinely different
service changes O and contract inventory and is not a comparable optimization.

Required means callers must provide a value on that route. Optional/defaulted
parameters and optional record members follow the same rule: they add E, not A.
Supplied fixed defaults are not caller slots on that route, but the configurable
route still exposes them in K. Slots are canonical downstream formal/field paths,
not parameter names. Equivalent record-packaging fixtures must preserve these keys.

Flatten transparent input data carriers into required/optional leaf slots; do not
charge both a record argument and its fields. Transparent means resolved plain
fields with no construction validation, behavior or identity-dependent operations.
Do not add a nominal/record-wrapper concept for such packaging. A scalar is one
slot; a collection is one slot regardless of runtime length. Opaque behavior-bearing
objects remain one concept/slot; do not invent their construction burden. Recursive
or unresolved shapes beyond the flow contract's finite limits are partial.

### Concepts and supplied policies

T normalizes numeric primitives to `number`, character/text to `text`, booleans to
`boolean`, bytes to `bytes`, unit/void to none. Pointer/reference/nullability wrappers
add no concept. Collections add `sequence` plus element concepts (bytes excepted);
maps add `map` plus key/value concepts; callbacks add `callback` plus signature
concepts; unions add `choice` plus alternatives. Behavior-bearing named types and
enums add canonical contract IDs. Flatten transparent carrier fields as above.
The implicit receiver is free. Memoize recursive types by symbol identity.

P is one per selected-route required slot flowing to a callback invocation, registry
policy position, or this bounded local selector pattern: a public Boolean/enum
selects between distinct, fully resolved normal-result/effect recipes, both consuming
an independent payload or resource. Direct Boolean/enum data encoding, a predicate
computed from payload, and input rejection are not policy selection. A selector
whose only effect is copying itself into the result is data. A mode choosing two
encoders for the same payload is policy. Deduplicate the same slot when several
recognizers identify it. Unsupported branch/target resolution is partial.

## 2. Hidden obligations, deduplication and alternatives

An obligation ID is `(boundary, category, governed_root_or_outcome, normalized_rule)`.
It excludes public route/family ID and source coordinates. Families are access
routes, not independent owners of shared behavior. Two public roots using the same
private state/guard/validator or transformation cannot multiply its credit.

Governed roots use the flow contract's canonical storage/resource identities. Pure
outcomes use normalized data-flow expression DAGs with typed formal placeholders;
private helper boundaries, local variable names and identity wrappers are erased.
The same computation exposed by two public roots is one X obligation. Distinct
owned roots or different typed computations can remain different obligations.
Within a connected transformation outcome, ten operators or helper stages are
one X obligation. Repeated V checks for the same governed input/outcome are one V.
The scorer never counts nodes, loops, logging, statements or lock acquisitions.

Retain up to 32 distinct obligation sets for each family's resolved route/dispatch
alternatives. Compose family alternatives by set union, deduplicating obligation
IDs after each step; collapse identical resulting sets. H is the minimum sum of
weights over the resulting boundary alternative sets. Sort canonical IDs before
composition. Never take a component-wise category intersection as the numeric
basis. If composition exceeds 32 sets, emit `alternative_limit` and no numeric
score; do not keep a scheduling-dependent subset or silently fall back to zero.
An unresolved essential alternative is unknown and also withholds the score.

The info view separately reports guaranteed IDs (intersection) and conditional IDs
(union minus intersection). Thus a validated and an unvalidated route cannot report
V as guaranteed; when both otherwise hide X, H=2, not 3. A resolved choice between
C and X yields H=2, not 0. For several families the minimum is taken **after union**,
so a shared obligation is counted once even when alternatives overlap.

Explicit conditional dispatch and finite receiver dispatch use exactly these joins.
Failure exits are used to prove/check guarantees, not treated as empty successful
implementations: a validation guard does not lose V simply because its rejection
path throws. The flow contract defines normal alternatives and exceptional exits.

### Recognizer table

| Category | Positive evidence | Exclusions |
| --- | --- | --- |
| U (weight 0) | Normal result or observable effect from a resolved body/summary. | Signature alone; it establishes relevance, never hidden-responsibility credit. |
| V (1) | An input/state-dependent failure guard governs the relevant normal outcome; or lower-level failure is converted to a different exposed error type/tag. | Unreachable or unrelated checks, bare rethrow, unchanged error forwarding, helper names. Guard must cover the relevant outcomes for the route. |
| R (2) | Successful acquisition is paired with a matching cleanup attempt on every supported normal/exceptional exit before ownership is returned. | Acquisition alone, live handle returned to caller, cleanup on one path only, unknown escape/exit. A failed close is an explicit exceptional exit, not proof of successful release. |
| C (2) | Private owned storage is read and updated with a dependence on prior state or a later observable result; callers cannot write/alias that root. | Local accumulators, construction/field assignment alone, write-only setters, leaked backing state, unrelated counters. Not a proof of arbitrary invariants. |
| Y (2) | Relevant accesses to a governed root are protected by a recognized monitor/guard on all supported paths or a recognized atomic RMW. | Unused/unrelated locks, partial guarding, open dispatch. Evidence of coordination, not a thread-safety certificate. |
| X (2) | Input/prior state reaches an outcome through supported non-identity arithmetic, selection, lookup, encoding, text composition or reduction. | Direct copies, passive record packing, allocation, getters, error-message formatting and unrelated machinery. |

R/Y require every relevant exit/access to satisfy their guarantee. V applies to the
outcome the guard governs, not every unrelated operation. C/X are attached to the
normal behavior alternatives that actually perform them. Unknown effect/alias facts
remain unknown; one successful path does not establish a universal guarantee.

Normalize aliases and safe constant/identity expressions before X, including `x+0`,
`x-0`, `x*1`, `x/1`, `x|0`, `x^0`, `x&&true`, `x||false` and identical branch arms,
**only when valid for the resolved language/type semantics**. Do not apply JS bitwise
identities to arbitrary numbers or floating-point identities ignoring signed zero,
NaN or exceptions. Constant conditions prune unreachable branches. No general
algebraic-equivalence solver is required.

**Accepted resolution limit:** an increment wrapper and a substantial pure reduction
with the same interface both have one X obligation and may tie. Add source fixtures
for that tie and show `transformation breadth, not algorithmic sophistication` in
the metric explanation. Do not manufacture extra credit by counting operators,
loops or inventing unreviewed transformation subcategories. This is narrower than
universal semantic depth, but still independent of signature-only evidence.

## 3. Creation equivalence and source boundaries

Normalize construction to `create:<canonical type>` facts with input bindings,
initial field assignments, possible failures and additional behavior. Explicit and
language-synthesized constructors, valid struct/record literals, and a resolved
factory returning that same initialized object are routes of the same creation
family. An implicit default constructor is a real zero-required-slot route if the
language permits external construction. No inaccessible constructor is invented.

Allocation and passive packing earn no X/H. Validation/transformation/resource work
inside creation earns only the corresponding obligation with the same rules as a
factory. Plain data-only creation is not applicable in all three spellings. Creation
of a behavior-bearing type adds one O, and its slots enter burden consistently.
Source-complete fixtures cover explicit/implicit/default constructors, factories,
private creation, validated creation and required/optional record fields.

Identity is `(artifact, external audience, view kind, canonical symbol)`, excluding
file position/body hash. Artifacts use workspace-relative supplied descriptors:
Go module, Java module/source-root group, Rust crate target, TS project/package.
Without descriptors use `source-root:<language>:<relative-root>` with inferred-build
provenance. Ambiguous ownership is partial, never arbitrarily assigned.

| Adapter | Required slice | Explicit gaps |
| --- | --- | --- |
| Java | Effective enclosing visibility; qualified overload IDs; local inheritance/interfaces; inherited public API of nonpublic bases; known constructor receivers and synthetic default constructors. | Unprovided generated members/superclasses, open dispatch and unsupported exceptional flow. |
| Go | Selected package exports using go/types and supplied local/export data; reachable unexported receiver types, embedding, finite interfaces, legal zero/literal construction. | Missing imports, unknown build selections and receiver/ownership escapes. No downloads or implicit builds. |
| Rust | Explicit module visibility/re-exports; local inherent/trait methods; known literal/constructor routes, standard ownership/Drop patterns. | Unexpanded macros, unknown cfg/trait dispatch or ownership escape; no macro execution. |
| TypeScript | Resolved local/default exports and barrels, classes/inheritance, default constructors, exported callable objects/anonymous factory results; compiler graph independent of type-safety metric toggles. | Missing declarations, dynamic properties/receivers and ambiguous assignments. Declarations remain context, not implementation rows. |

Runtime launchers have implementation depth not applicable; analyze their bodies
for supporting evidence and library APIs separately. Protocol/data-only views are
not applicable. A fully resolved non-creation callable no-op is applicable with H=0.
Missing exports cannot be called “no public API” merely to obtain N/A.

## 4. Shared flow and concrete built-ins

All adapters emit the [same normalized flow contract](shallow-v4-flow-contract.md),
including value bindings, aliases/roots, ownership, exception edges, joins and
provenance. Source→flow→fact fixtures run before independent adapter implementation;
scorer-only fixtures are insufficient. The TypeScript scorer may remain separate
only if it uses the same policy and exact common fixtures.

The [registry JSON](evidence/shallow-v4/builtin-registry.json) is the complete initial
allowlist. It supplies concrete symbol/signature matches, guard conditions, result
aliasing, effects, ordinary failure exits and policy positions. Its fixtures include
positive calls and shadowed or unmatched lookalikes. Unlisted overloads/operations
are unknown, not assumed pure. Expand only by adding a versioned entry and fixture.
Language intrinsics and source-proven local bodies use flow rules, not name guesses.

Registry matches require resolved standard provenance and either exact receivers
or resource tokens produced by a trusted registry factory. User overrides and user
formatting callbacks must resolve; a matching method name is insufficient. Process
termination/OOM is not an ordinary source exception; it produces a retryable worker
diagnostic. Standard output is an observable effect, not an automatic H bonus.

## 5. Knowledge, reporting and aggregation

Track `inventory`, `burden`, `behavior`, `alias_effects` as measured, partial,
unavailable or not_applicable, with source reasons. Measured means complete for
this rule set. Category evidence is present/absent/conditional/unknown. Essential
unknown dispatch, effects, signatures or aliases withhold the numeric boundary
score. A nonessential gap must be proven irrelevant to result/failure/state/resource/
ordering; arbitrary logging is not assumed harmless. Preserve supported facts.

Serialize nullable boundary scores and the alternative obligation sets, H, burden
route choices and B. Display X for unknown, – for inapplicable, and concrete values
otherwise. Show relevance-only outcomes without presenting them as hidden credit.
Unknown required evidence blocks SCORE pass and valid-zero claims; N/A does not,
but measured→N/A requires contract reconciliation during fixes.

### Boundary ledger is authoritative; files are projections

Store one `boundary_assessment` and one SHALLOW contribution per stable boundary ID.
Compute contribution with the unchanged catalog scalar formula **per boundary**.
Any aggregate SHALLOW contribution is the sum over unique selected boundary IDs,
never a sum of file maxima or repeated references. Aggregate completeness also
uses that ledger. Do not combine other metrics with deduplicated SHALLOW by summing
file SCORE values; combine their own established totals with the boundary ledger.

For table navigation only, a file's SHALLOW display is the maximum linked boundary
score, and its displayed SCORE uses the maximum linked boundary contribution plus
that file's other contributions. Mark JSON fields as projections (`score_scope:
file_projection`, `boundary_refs`); these projections are not additive or sufficient
for a like-for-like aggregate improvement claim. Render the scope explanation in
info/help, not as extra header text. File ranking remains useful but is not a stable
cross-layout measurement.

Freeze the selected boundary IDs with fix scope. Verification evaluates each frozen
boundary's threshold/coverage and the unique boundary subtotal, never only the
file projection. A default deep threshold applies to every frozen applicable
boundary. A SCORE-only job must pass the frozen component/boundary contract and
cannot complete because regrouping reduced a file maximum. Follow boundary IDs
through anchor movement; path-only consumers lacking ledger metadata must refuse
comparison and request a fresh baseline. File deletion still requires reconciliation
of file-scoped metrics; this change does not authorize their disappearance.

Test two unchanged boundaries moved from separate files to one, the reverse split,
namespace-anchor changes, overlapping file selection and helper relocation. Their
boundary contributions, frozen-scope totals and fix verdicts must remain identical.
Display projections may change and must never be reported as metric improvement.

## 6. Normative validation cases

The [scoring JSON](evidence/shallow-v4/scoring-cases.json) defines exact integer
results, explicit alternatives, obligation IDs and route-burden cases. It replaces
the historical U-weighted cases. Required cases include:

- Identity/constant/no-op: H=0, score 100; passive creation: N/A.
- Simple arithmetic and a source-complete reduction: same X and same score for
  equivalent signatures, with the documented resolution limit.
- Validated versus unvalidated routes: V conditional; no credit borrowed by bypass.
- C-versus-X resolved alternatives: H=2, independent of conditional/receiver syntax.
- Two public roots sharing state/validation/lock: shared IDs count once.
- Required policy versus a fixed-default convenience route: default cannot worsen B.
- Optional parameter versus transparent record field: same A/E/T and score.
- Explicit/implicit constructor and equivalent factory: same creation assessment.
- Local enum/Boolean mode selection versus data transformation: only former adds P.
- Per-boundary contribution ledger: invariant under file regrouping and anchor moves.
- Unknown alternatives/cap exhaustion: no numeric result; no warm-cache exemption.

Core pure transformation, validation, delegation and lifecycle source fixtures must
produce numeric results in every language. Language-specific unsupported coordination
must produce explicit partial facts; all-unknown output is not acceptable. Include
shadowed built-ins, alias escapes, error exits, neutral arithmetic, irrelevant locks,
helper movement, parser failures, profile drift and loss of required coverage.

For jname, verify Generator/Base inventory and exclusion of private random helpers,
and replace CLI signature-only penalties with runtime-entry applicability. Use the
source-complete inherited-generator fixture for numeric validation. Record actual
Lombok/injected-dispatch gaps rather than guessing a target score.

## 7. Profiles and comparison migration

Default profile `responsibility-v4`; explicit reproduction `legacy-signature-v3`.
Add `--score-profile` to both executables, native options and fix execution. Never
fall back silently. Legacy retains archived raw ousterhout-v3 behavior and is labelled
signature-based. Report schema 4 carries the boundary ledger and flow-policy version.

Fingerprint catalog/weights/enabled set, rule/registry/flow versions, boundary policy,
build selections and supplied signatures. Source-body edits invalidate caches, not
comparison profile by themselves. Public route/boundary inventory and applicability
are a separate frozen contract. Additions/removals/signature changes need rebaseline,
even if the new score is lower. UI reweighting creates an effective profile that
verification must reproduce exactly.

Unknown required evidence, missing targets, inventory loss and profile drift cannot
satisfy even SCORE-only or historical `RequireComplete=false` contracts. Regression
allowances apply to numbers, not evidence loss. Legacy jobs either use the original
supported profile or receive an actionable rebaseline error. Do not announce profile
migration or file projection changes as code improvement.

## 8. Cache, finite domain and performance acceptance

The finite abstract domain, logical work accounting and limits are normative in the
[flow contract](shallow-v4-flow-contract.md). They apply identically cold and warm.
Cache residency can save execution work but never buy extra numeric eligibility.
Deterministic cap failures include policy/limit identities; transient worker/OOM/
timeout failures are retryable and not successful durable assessments.

Intern summaries/evidence once, persist their dependency edges, and use existing
Merkle invalidation. Do not materialize transitive closures per consumer. Store
boundary/source indexes; idle polling/rendering performs no scans or analysis.
Invalidate actual summary consumers on implementation changes and inventory consumers
on export/superclass/registry changes. Shared graphs must reuse parameterized summaries.

### Runtime and resource gates

Release-scope amendment, approved by the user on 2026-09-17: benchmark the smaller
`~/src/river` repository for this release. The generated 30K-file benchmark and its
large synthetic workload runs move to follow-up capacity validation, alongside 80K.
They are not gates for this release. Keep deterministic graph/work-limit and cache
invalidation tests; this amendment does not remove the engine's resource limits.

Use the same local machine, toolchains and worker limits for baseline and candidate,
with no concurrent builds. Record actual OS/CPU/RAM/toolchains, repository revision
and any dirty-source snapshot, supplied build selection, and analyzed file/unit
counts. Use an isolated River snapshot for repeatable edits rather than changing
the user's working tree. The previously prescribed 8-CPU/16GiB runner is not required
for this release. Use the frozen pre-v4 binary with legacy profile as the performance
baseline; baseline and candidate must analyze identical source snapshots.

Run five cold trials (fresh application caches) and twenty warm/edit trials after
three unmeasured warmups. Record median/p95 (nearest-rank), process-tree peak RSS and
logical/physical work counts; report failures, not only successful trials. Source
and fixture generation, machine preparation and edit scheduling are outside timed
analysis. In-process warmups may not reuse a different source revision's result.

| River release benchmark gate | Required result |
| --- | --- |
| Cold startup median/p95 | Both <=3x matching baseline +2s, and p95 <=120s. |
| Warm no-change p95 | <=1.25x baseline +200ms and <=2s; zero source/analyzer work. |
| Representative implementation edit p95 | <=2x baseline +500ms and <=5s; record and verify its actual affected-unit closure. |
| Representative public-contract/summary edit p95 | <=2x baseline +1s and <=10s; record and verify its actual affected-unit closure. |
| Whole process-tree peak RSS | <=1.5x baseline +256MiB and <=2GiB. |
| 60-second idle poll window | Zero source scans/analyzer work; CPU time <=baseline +100ms. |

For River, require expected invalidation sets and cold/warm eligibility parity.
Exercise actual watcher cache reuse, not just repeated standalone CLI runs. Keep
deterministic shared-helper, diamond, chain, SCC and widening tests separately;
their large generated wall-time/RSS runs are follow-up work. A performance regression
fails acceptance even if the correct units were selected. Diagnose/fix failures;
changing a budget requires an explicit documented amendment, not dropping trials.
No wall-clock assertions in unit tests. Report this as River-scale validation, not
proof of 30K/80K capacity. Other real-repository correctness checks remain in scope.

## 9. Sole execution order and file ownership

The plan's four stages are outcome groups. This table is the **sole execution
order**. All comparison safety must be integrated before enabling the v4 default.
Main agent owns policy/review; Luna/high agents implement and test. Preserve unrelated
uncommitted work. Adapter work begins only after the flow/registry fixtures exist. Every adapter batch
must implement its lowering table and conformance column in the
[adapter contract](shallow-v4-adapter-contract.md); the all-language source matrix
is mandatory, with no missing translations or unexpected unknown results.

| Batch | Files / new files | Acceptance |
| --- | --- | --- |
| 1: policy and normalized contract | `analyzers/structural/internal/facts/model.go`; new `depth_facts.go`, `metrics/depth_v4.go`, tests; `analyzers/conformance/shallow-v4/` | Implement route burden, obligation unions/minimum, creation facts, typed states and scorer golden cases. |
| 2: common flow and summaries | New `analyzers/structural/internal/depth/`; checked-in registry and source→flow→fact fixtures | Implement finite roots, transfers/joins, exits, SCCs, provenance, limits and recognizers; no adapter-local reinterpretations. |
| 3a: Go | `internal/goadapter/{adapter,syntax}.go`; new `depth_surface.go`, `depth_flow.go`, tests | Visibility, embedding, creation and local resolution; required source/flow fixtures. |
| 3b: Java | `adapters/java/src/dev/slopslap/structural/{Facts,JavaParser,JavaClassFacts,JavaMethodBodyFacts,JavaExpressions}.java`; new `JavaDepthFacts.java`; adapter tests | Effective/inherited API, creation, exceptions and source-flow parity. |
| 3c: Rust | `adapters/rust/src/{model,parser,surface,control,expression}.rs`; new `depth.rs` | Modules/aliases/traits, creation, ownership/Drop, explicit unsupported facts. |
| 3d: TypeScript | `analyzers/typescript/src/{model,context,operations,surface,structural}.ts`; new `depth.ts`, `depth_flow.ts`, `depth_score.ts`; tests | Export graph, creation, flow facts and exact scorer parity; no dependence on optional type-safety enablement. |
| 4: report/ledger/cache | `go/internal/report/model.go`, new `assessment.go`; native scoring/cache/changes files; `component-catalog.json` | Boundary ledger, nullable scores, nonadditive file projections, cache persistence and fresh/warm parity. |
| 5: profiles/verification | native options/catalog; `analysiscache/{key,schema}.go`; scoring catalog/metrics/projection; `fix/scoring.go`; fixanalysis nativeadapter baseline/scoring/verification/clone | Frozen boundary/profile/coverage contract, route/regrouping invariance and all completion paths guarded. |
| 6: product integration | both CLI mains; `slopslap-go/{report,fix_runtime}.go`; follow table/detail/weights/distribution; fix persistence | Profile flag, evidence explanations, unknown sorting, scope-aware SCORE display and actionable migration. |
| 7: validation/docs/default switch | depth design/reference/delivery docs, conformance runner and benchmarks | Full suites, real-repo inspection, all resource gates; enable default only after batches 4–6 pass. |

Paths for Go/Java/Rust adapter batches are under `analyzers/structural/`.
Run structural Go/Java/Rust, TypeScript and application Go suites; rebuild both
executables. Check new code with explicit legacy profile for SCORE <50 and raw
NPATH <=100 so the metric change cannot hide implementation complexity regressions.

## 10. Finite validation and completion

Run jname, SlopWatch and ap before/after with binary/profile provenance. Explain
changed jname cases and inspect ten new high-score findings in each other repo
(or all if fewer); retain unsupported cases in the report. This is bounded
engineering validation, not an external panel or research corpus.

Completion requires the numeric supported slice, ledger-safe comparisons, normalized
source-flow parity, stated limitations, migration and passing performance gates.
It cannot be satisfied by infrastructure alone, blanket unknowns, or a new formula
whose shared semantics are left for individual implementers to invent.
