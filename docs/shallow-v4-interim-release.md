# SHALLOW v4 interim release and capability backlog

> Superseded where conflicting: [P0 delivery directive](shallow-p0-delivery-directive.md).
> Applicable source must receive numeric SHALLOW ratings, including estimates,
> in reports, ranking and SCORE. The r20 `X` policy below is historical and must
> not guide implementation. Enterprise performance validation is required.

Status: language conformance and uncertainty correction, 2026-09-17.

## Current delivery amendment: provisional evidence and source acceptance (r20)

A bounded numeric estimate is recognition evidence, not a complete SHALLOW
measurement. In particular, no recognized responsibility does not establish that
delegated or unsupported behavior has zero responsibility. Estimated boundaries
retain their numeric estimate, obligations and precise-analysis limitations in
the JSON ledger and detail view. Files containing such boundaries show incomplete
SHALLOW (`X`) in the main table, sort after numeric SHALLOW measurements, and
receive no SHALLOW contribution to SCORE. SCORE still uses the other available
components. This supersedes earlier amendments that admitted bounded estimates
to normal numeric ranking. Neither a score cap nor a replacement zero is applied;
the formula and weights are unchanged.

`StoredTableRowCodec` delegates to `StoredTableRowEncoder` and
`StoredTableRowDecoder`. The current bounded evaluator does not establish the
responsibility hidden by those cross-class calls. Its recognition-only 100 is
therefore provisional, not a maximum-shallowness conclusion. Following those
dependencies remains a capability gap; changing the presentation does not claim
to implement their behavior.

`make test-shallow-adapters` now executes the normative adapter sources through
the packaged CLI and fails on unmet assertions, including unsupported cases.
The JSON report separates numeric coverage, estimated coverage and conformance.
Per-family hidden-responsibility diagnostics support creation assertions without
changing the aggregate calculation. Policy r20 invalidates previous cached
projections.

The r20 language slice adds proven Go forwarding/default route families and
conditional checked/unchecked validation; TypeScript named/default/arrow/export-list
functions, bounded same-file helpers, scalar branches and rejection guards; and
Rust boolean negation, local calls, inline module/reexport resolution, cfg selection
uncertainty and private-function reachability. Out-of-line Rust modules remain
partial. TypeScript default parameters/records, classes and state remain outside
this slice. These additions do not close full language conformance.

[Executable fixture results](evidence/shallow-v4/adapter-r20-acceptance.json):
Go 8/14 conforming, Java 5/15, Rust 2/14, TypeScript 5/16. Numeric results alone
are not acceptance: the report distinguishes estimates and wrong numeric results.
The acceptance command intentionally remains failing for open requirements.

[River evidence](evidence/shallow-v4/river-r20-coverage.json) records 2,204 boundaries:
3 definitive numeric, 2,122 provisional numeric and 66 N/A, in one 11.84-second
uncached run over 2,207 files. [Other project runs](evidence/shallow-v4/language-repository-r20-coverage.json)
record Rust module/behavior limitations and Go/TypeScript type-resolution blockers;
those blockers must not be represented as a clean semantic-coverage benchmark.


## Previous delivery amendment: bounded Java scoring (r19)

The r19 control-flow contract uses explicit transfers instead of sequential AST
fallback. Boolean short-circuit and conditional expressions evaluate only reachable
effects and preserve alternatives for unknown conditions. Switch statements model
case entry, fallthrough, default/no-match, constant selectors and target-bound
breaks. Known explicit exceptions enter matching catches; finally executes before
restoring a pending return/throw/break, including nested finally blocks. Paths that
completed before entering a try statement are never revived by its finally.

The bounded statement dispatcher permits only implemented transfers. Other
constructs—including labelled statements, continue, do-while, assert, switch
expressions and try-with-resources cleanup—produce named uncertainty and an opaque
completion alternative; their descendants are not credited through sequential
traversal. Unknown exception matching is also explicitly limited. This is bounded
source coverage, not full Java execution semantics. Focused regressions passed for
the reported short-circuit, switch and catch/finally cases and these transfer guards.

The r18 correction distinguishes exceptional exits from successful alternatives,
so rejected inputs establish validation without reducing the successful service's
state/transformation responsibility. Returned caller paths remain unchanged by
later helper composition. Potentially skipped while/for/enhanced-for bodies retain
the skipped alternative; repeated-iteration behavior remains explicitly bounded.
Initializer and assignment expressions now traverse the same effect evaluator as
returns, preserving state effects when helper results are stored in locals.
Focused regressions cover validation plus update, returned-path helper extraction,
if/while/for zero-iteration equivalence, and direct/local helper-result storage.

The r17 corrections preserve bounded per-route branch obligation alternatives
(up to 32), including common suffixes and termination, for the shared minimum-
weight scorer. A guarded transition retains its no-op alternative; validation
credit describes the checked input on both acceptance and rejection outcomes.
Helper effects preserve alternatives without importing unused return credit.
Coordination credit requires a recognized stable monitor and consistently guarded
accesses to the relevant private state root. Fresh monitors and mixed guarded/
unguarded accesses earn no Y. This remains a declared-source recognition rule,
not a thread-safety certificate. Straight-line helper locals preserve returned
dependencies, and numeric identity rules no longer remove string concatenation.

Focused Java regressions passed for these four corrections, common suffixes,
positive/negative monitor cases, state responsibility, no-argument helper extraction
and the prior false-transformation pairs in both evaluators. No broad suite or
capacity run was performed for this correction.

The r16 returned-value summary retains resolved owned-field dependencies through
canonical expressions and actual helper substitution. No-argument helpers retain
transformation credit and field read/update responsibility when their result is
stored back. Focused regressions passed for implicit/explicit `this` calls and
state write-back, alongside both-evaluator conditional/unused-argument pairs.

The r15 correction removes the remaining unconditional conditional-expression
credit and any-connected-argument helper heuristic from the expression scanner.
Both use normalized returned computations with actual helper parameter binding.
The focused `TestJavaFalseTransformationsInBothEvaluators` passed: identical-arm
conditional versus identity, and `twice(0,x)` versus zero, each through precise
and bounded evaluation. Bounded variants carry the same stateful `touch()` method;
assertions check evaluator selection, H, burden and SHALLOW. This focused pass
does not establish broader acceptance or supersede the remaining gaps below.

The r14 correction preserves proven supporting-provider N/A with inventoried
implementation signatures: only routes covered by the complete internal contract
proof qualify; independently exposed or unmatched routes still prevent exemption.
State transformations now share one X obligation per governed field outcome,
independent of intermediate update statements. Conditional return expressions and
private helper arguments are normalized before assigning returned transformation
credit; identity results do not inherit credit from operator syntax. Focused
regressions include the same stateful method across expression variants and the
combined-versus-split state update reported in review. They have not been run.

The r13 correction makes declared public method inventory a separate pass before
behavior collection, including methods on supporting-role boundaries. Stateful,
synchronized and reference-returning operations remain in the surface even when
precise evaluation declines their bodies. Inventory regressions cover increment
and backing-array/copy getters; this does not claim copy-versus-alias semantics.

The r12 correction keys recognized transformation obligations by normalized
computation, input position/type and governed field/output, rather than public
method signature. Duplicate public exposure shares responsibility credit while
retaining its surface burden. Different computations remain distinct. Unresolved
computation identities remain conservative and are not arbitrarily merged.

The r11 bounded state recognizer handles passive constructor storage and accessors
without responsibility credit, connected arithmetic field updates, guarded Boolean
state transitions and source validation. Exact private helper calls preserve
observable state/validation effects even for void/no-argument helpers; unused
returned transformations earn no credit. Local assignment tracking discards stale
facts and conservatively merges branches. Receiver aliases and external effects
remain uncertainty, not invented state responsibility. These remain explicitly
marked estimates, not complete semantic measurements. Focused state/neutral/helper
regressions were added but not executed in this delivery.

The r10 correction inventories declared public methods before behavioral admission,
retains nontrivial constructor signatures and parameters, and includes exposed
fields. Ordinary fields, initializer blocks and nested declarations no longer
trigger blanket unsupported-member rejection. Precision limitations identify the
affected method, constructor, field or initializer; external-call limitations
identify their containing method and resolved target. Unknown behavior retains a
numeric bounded estimate and does not erase the remaining declared surface.
Inherited surface resolution and alias proofs remain explicit limitations. This
change adds inventory and uncertainty handling, not new behavioral recognizers.

Ordinary Java boundaries that exceed the precise scalar flow evaluator now use
`bounded-static-v1`: declared callable surface burden and connected source patterns
for validation, transformation and state responsibility, scored by the existing v4
formula. Precise measured results and established N/A roles take precedence. This
is a numeric source metric, not a claim of complete semantic proof and not a legacy
score substitution. Numeric bounded results are explicitly marked as estimates, preserve precise-analysis
reasons and do not make a file complete, passed or eligible for automated fixes.
The delivery target is numeric SHALLOW for at least 90% of Java
files; the historical counts below precede this change and do not measure it.

Identity operations such as `x + 0` earn no transformation credit. Constant-false
branches earn no validation credit. Returned transformations retain credit through
bounded exact private-helper calls; unused helpers do not earn credit.
Calls alone earn no responsibility. Resource lifecycles, general alias analysis,
external delegation and full path proofs remain capability gaps, described in the
file detail evidence. Syntax/source failures remain unavailable. Bounded results
carry no complete-inventory fingerprint for automated fix acceptance. Policy r19
separates cached results from the earlier policy. Non-SHALLOW measures and the
aggregate formula are unchanged. The follow-up review adds focused paired regressions for neutral arithmetic,
unreachable validation and private-helper extraction. They have not been run;
only compilation is performed in this delivery.

Release blockers identified in product use: unavailable SHALLOW must not hide the
aggregate SCORE (P0), and ordinary Java must produce useful SHALLOW results (P1).
The earlier packaged scalar smoke and passing suites did not establish those
requirements. Both need verification in the built application before release.

P0 correction: aggregate availability now depends on usable component measurements,
not the whole-file completeness flag. Scoring, CLI, TUI, sorting and distribution
regressions pass. The rebuilt CLI on isolated River source displays
`IndexedTransactionSession.java` at SCORE 38.2417 with SHALLOW X, above
`PagedBooleanArray.java` at SCORE 0 with SHALLOW X. Completeness, pass eligibility
and scoring formulas remain unchanged. P1 Java capability coverage remains open.

The Java repository-wide payload failure is fixed: response schema 5 sends bounded
per-boundary chunks, merging package flows at the receiver. An oversized boundary
becomes explicitly partial without discarding healthy siblings. Source attribution
still runs once over the supplied inventory. Shared ledger reasons/evidence are
deduplicated across file associations. The oversized-boundary regression checks
that a healthy sibling retains a numeric final measurement.

The interim release delivers the existing bounded analysis and its supporting
framework. It does not claim completion of the full SHALLOW remediation plan.
New semantic capabilities are deferred while current correctness, cache, reporting,
comparison and UI integration are finished. This release boundary was requested by
the user after reviewing the implementation's real-code coverage.

## What the interim release provides

- The `responsibility-v4` profile as the default. The established metric remains
  available explicitly as `--score-profile=legacy-signature-v3` for comparison.
- A shared responsibility/burden scorer and bounded flow evaluator, supported
  numeric/Boolean flows, exact local helper summaries and basic numeric loops.
  Go text concatenation now distinguishes composition from empty-string identity.
- Supported validation and transformation evidence. The shared engine can prove
  closed resource lifecycles, including exact helper witnesses; this is not yet
  end-to-end resource recognition across the language adapters.
- Java scalar instance methods can call exact same-owner stateless helpers,
  including private helpers and explicit `this` calls. Purity checks are cached
  and bounded; stateful helpers, open dispatch and recursion remain incomplete.
  Helper extraction preserves the public surface and constructor burden.
- Java guarded throws of resolved standard `IllegalArgumentException` constructors
  support validation evidence for empty or compile-time String messages. Message
  formatting grants no transformation credit; unsupported constructors and message
  effects remain incomplete. Empty `void` methods are applicable with H=0, not
  mistaken for an unknown return value or a data-carrier exemption.
- Java supporting-contract applicability based on resolved implementation,
  production binding and use. This can exempt a legitimate internal provider
  without relying on class names or suffixes.
- Proven allocation-only creation with no independent behavior is N/A. The
  shared rule rejects incomplete facts, mixed services, failures, obligations
  and lifecycle/leak burden; the Java adapter supplies facts for trivial
  constructors, including overloads. Final top-level Java classes containing only
  private final primitive/String fields, passive construction and exact read-only
  getters also qualify. Getter routes and signatures remain in the inventory;
  renaming a getter changes its comparison fingerprint. Validation, computed
  getters, mutable references and side-effectful initialization prevent this
  exemption. This does not yet classify general records.
- Validated immutable Java carriers with private final `int`/`long`/`boolean` fields
  and exact visible getters now retain constructor validation in the shared flow
  proof. Construction and getter routes remain in the inventory; passive storage
  adds no transformation credit. An unvalidated constructor alternative does not
  inherit credit from another overload. This is bounded immutable data handling,
  not general mutable receiver/state analysis.
- A boundary evidence ledger, explicit incomplete/N/A states, profile-separated
  caching, indexed file details and guarded fix comparisons.

The user explicitly approved making v4 the default for this interim release, with
capabilities added iteratively afterward. Java uses the bounded r8 policy above
when precise evaluation is incomplete; other unsupported cases remain incomplete.
No case silently falls back to legacy. Non-SHALLOW definitions, weights and the
aggregate SCORE formula remain unchanged; v4 may alter SCORE through its SHALLOW
contribution.

## Required paired acceptance cases and unresolved evidence

The following pairs are mandatory before accepting the bounded Java policy:

| Pair | Required outcome |
| --- | --- |
| Identity versus `+ 0`, `- 0`, `* 1`, `/ 1` | No extra responsibility or improved score |
| No check versus constant-unreachable validation | No validation credit |
| Inline transformation versus exact private helper extraction | Equal responsibility and score with unchanged public surface |
| Declared versus inherited public service | Preserve resolved surface; no silent exclusion or invented completeness |
| Defensive copy versus returning/storing an alias | Distinguish ownership evidence; unsupported alias semantics remain explicitly uncertain |

The review reports nine Java adapter test failures under r8, including unknown
divisors, dispatch, attribution, surfaces, carriers, exception shapes and void
effects. These require explicit separation of precise-analysis expectations from
numeric-estimate expectations; they are not accepted by simply weakening assertions.
The shared metrics/depth test pass reported by the reviewer does not resolve them.

Post-amendment numeric coverage, ranking usefulness and refactoring stability have
not been measured. Sustained idle behavior and 30K capacity are also unverified;
broad Java invalidation remains documented below. These are unresolved acceptance
evidence, not completed work. No further recognizer expansion is implied by this
fix. The current code change addresses identity/reachability/helper incentives and
retains estimate provenance; it does not establish full inheritance or copy/alias
semantics.

## Historical coverage before bounded Java scoring

The rebuilt whole-repository Java analysis of `~/src/river` covers 2,207 files and
produces 2,204 boundaries: **6 measured, 2 N/A and 2,196 partial boundaries**.
At file level this is **6 measured, 2 N/A, 2,195 partial and 4 unavailable**;
the unavailable files are package declarations. Before the fixes, a global
payload-limit failure could replace the entire repository with one partial result.
That failure is resolved, but this coverage remains inadequate for general Java
use and P1 remains open.

Numeric examples from the rebuilt default CLI are `LockDeadline` (31),
`SessionPermissions` (36), `TransactionProgramAction` (36) and
`CatalogBuildIntentProgressValidation` (45), `ProtocolContinuationLimits` (29)
and `LockMemoryEnvelope` (58). The latter has one validation obligation and no
getter transformation credit, with its immutable-carrier proof in detail evidence.
Both heap allocator examples receive
their supporting-contract exemption. Constants, scalar branching/short-circuit
control flow, trivial constructor families and bounded stateless instance/package
surfaces now work. Division/remainder by proven nonzero integer constants is
supported; zero or unresolved divisors remain incomplete. Constant field access
through a value expression cannot discard receiver effects. Ordinary
stateful/object behavior is still largely unsupported.
An explicit legacy run on the same 2,207 files produced exactly identical
non-SHALLOW component data. No legacy fallback has been introduced.
Before validated-carrier lowering, the passive-carrier, instance-helper,
argument-error and void fixes retained 5 numeric and 2 N/A River boundaries.
A packaged CLI fixture combining a private instance helper,
input rejection and arithmetic scores SHALLOW 31; a void no-op scores 100, while
an unsupported side-effecting method remains partial with numeric aggregate SCORE.
These are capability checks, not evidence of adequate general Java coverage.
Repeated reasons are now deduplicated with their fact witnesses retained, reducing
River's boundary reason entries from 84,747 to 13,712 without changing score state.
The later validated-carrier whole-River run measured 11.93 s wall time once and
retained identical non-SHALLOW component data across all 2,207 files. It adds the
one real `LockMemoryEnvelope` result; it does not resolve the general Java P1 or
establish a performance distribution.

A cache check after the chunked-transport repair, before the arithmetic extension,
reproduced all 2,204
boundary records byte-for-byte on warm reload, including the four numeric results.
Cold analysis took 8.41 s; warm reload took 461 ms with zero analyzer calls. This
is one real-repository observation, not a repeated latency or idle benchmark.

Merged incomplete/unavailable ledger entries clear their numeric SHALLOW value,
independent of duplicate-record order. The CLI retains aggregate SCORE from the
remaining measurements. A rebuilt-CLI creation fixture reports N/A and SCORE 0
for trivial creation alone, while creation plus a transformation remains numeric.
A rebuilt-CLI immutable-carrier fixture reports N/A, numeric SCORE 0, a nonempty
inventory fingerprint and accessor proof in the evidence ledger. A corresponding
constructor-validation fixture remains partial with numeric aggregate SCORE.

An earlier run reused its persistent cache without analyzer calls; the reported warm
load was approximately 357 ms. That is cache evidence, not proof of completed
semantic coverage or 30K–80K-file performance. Final acceptance must preserve the
distinction. Partial measurements must never appear as zero, complete, or passing.
An unavailable SHALLOW component must not suppress the aggregate SCORE: SCORE
continues to sum the available components and remains sortable. Only a file with
no usable measurements has an unavailable aggregate. Analysis completeness and
automated-fix verification remain separate from displaying that partial aggregate.

An isolated River change check subsequently recorded cold 9.18 s, warm 350 ms,
incremental edit 7.78 s and fresh post-edit analysis 7.33 s. Editing a relevant
same-package consumer changed the allocator from N/A to partial; incremental and
fresh eligibility agreed. Warm reuse invoked no analyzer. The edit invalidated
all 2,207 Java paths: Java invalidation is currently broad, and finer dependency
granularity remains a performance follow-up. These individual timings are not
median/p95 claims and were not a controlled comparison with legacy.

A subsequent cold/warm run recorded 8.63 s / 416 ms with zero warm analyzer
calls. Sampling the process tree at nominal 100 ms intervals observed a maximum
aggregate RSS of 613,040 KB (about 599 MiB) across up to four processes, in 83
samples. Sampling can miss brief peaks. This is neither a controlled legacy
comparison nor a sustained idle measurement; idle CPU and scan behavior have not
yet been verified by this run.

## Capability gaps and planned implementation

| Gap | Planned behavior | Evidence required before acceptance |
| --- | --- | --- |
| **C: private state transitions** | Track persistent owned roots through reads and updates; prove callers cannot write or alias them; connect the transition to relevant behavior. | Increment/swap/stateful-service examples receive C; getters, write-only setters, local accumulators, leaked state and unrelated counters do not. |
| **Y: coordination** | Recognize resolved monitor/guard protection and atomic read-modify-write operations over the relevant root, across supported paths. | Guarded accesses receive Y; unused locks, incomplete guarding, unresolved dispatch and unrelated synchronization do not. |
| **R: source-language resources** | Lower acquisition, cleanup attempts, ownership transfer and exceptional exits from each language into the shared proof. Complete helper/policy effect alternatives. | Positive and missing-cleanup cases from real source in each language; returned or escaped ownership does not receive closed-lifecycle credit. |
| **Broader X/V behavior** | Support connected collection lookup/reduction, encoding/text operations and error adaptation with resolved contracts. | Observable transformation or failure policy is distinguished from copying, packing, error-message formatting and unchanged error forwarding. |
| **Objects and public contracts** | Complete ordinary instance/receiver behavior, fields, construction, inherited APIs, overload normalization, transparent carriers and export visibility. | Source-level fixtures preserve equivalent interfaces and distinguish added responsibilities from exposed implementation burden. |
| **Calls, aliases and loops** | Add bounded receiver sets, effect/alias summaries, recursive fixed points, closures and remaining loop forms. | Helper extraction preserves results; unresolved targets and exhausted limits remain explicit; shared graphs do not expand per caller. |
| **Legitimate structural roles** | Implement the remaining approved applicability rules using structural evidence, consistently across languages. | Main-display results account for the role; detail view explains the evidence; misleading names confer no exemption. |
| **Language parity** | Complete the agreed Go, Java, Rust and TypeScript adapter cases, including ownership, exceptions, exports and relevant built-in contracts. | All required translations pass the shared source-to-fact-to-score cases; missing coverage is reported rather than substituted with another language's result. |

Implement these as bounded source-to-display deliveries: one specified behavior,
positive and adversarial source cases, shared proof, adapter lowering, visible
evidence and a real-code check. Do not expand a delivery to arbitrary program
understanding. The full requirements remain in the
[implementation specification](shallow-v4-implementation-spec.md),
[adapter contract](shallow-v4-adapter-contract.md) and
[role backlog](shallow-v4-legitimate-roles-backlog.md).

## Framework required before the interim release

1. **Finish existing correctness fixes.** Preserve resource-proof failures through
   cached helper summaries; retain exceptional-exit witnesses; bound proof work;
   localize Java attribution uncertainty without granting unsupported exemptions.
2. **Cache and change handling.** Preserve the ledger and nullable states on cold,
   warm and startup projections. Changes to a helper, signature or supporting-role
   consumer must invalidate its affected analysis. Watching active edits must
   recover normally. Rendering and idle polling must not trigger source analysis.
3. **Comparison safety.** Freeze profile/policy and reliable boundary inventory.
   Missing evidence, route deletion, signature/applicability drift or conflicting
   results cannot count as improvement, including SCORE-only verification.
4. **Product delivery.** Build both executables and bundled analyzers; preserve
   explicit profile selection, deterministic unknown/N/A sorting and readable
   detail evidence. Errors use the existing popup flow. Document current coverage.
5. **Lean acceptance.** Run the relevant existing module suites and focused
   regressions; compare non-SHALLOW outputs on identical source; exercise River
   cold/warm, idle and representative edit behavior on an isolated snapshot.
   Record file/boundary counts, cache/analyzer work, timing and memory limitations.
   Do not describe one-run timings as statistical or large-monorepo validation.

Default selection and safe profile migration are required for this interim release.
Full semantic conformance and the repeated performance/capacity study remain
follow-up work. They are not silently declared satisfied by this interim release.
Current functionality still must not introduce stale
results, incorrect numeric eligibility or an unexplained performance regression.

### Interim verification completed

- `make build` builds both executables and the bundled analyzers.
- Application Go and structural Go/Java adapter suites pass; Rust passes all
  10 tests and TypeScript passes all 24 tests.
- Packaged supported-source fixtures for Go, Java, Rust and TypeScript produce
  schema 4, the default `responsibility-v4` profile and numeric SHALLOW 30.
  Explicit legacy on those identical fixtures produces byte-identical normalized
  non-SHALLOW component output. This proves profile isolation for those fixtures,
  not semantic coverage of arbitrary source.
- Default/explicit-v4 equivalence, explicit legacy reproduction, startup cache
  profile separation, missing-ledger handling and frozen-inventory verification
  have focused regression coverage.
- Resource summary limit failures survive summary caching; cleanup witnesses
  survive helper exceptional exits. Unsupported/truncated proof does not become a
  complete numeric result.
- CLI text and TUI distinguish numeric, N/A and incomplete results. Partial v4
  boundaries cannot render as zero or a misleading measured maximum in CLI text.
- River cold/warm, isolated edit and sampled memory evidence is recorded above.
  Sustained idle behavior remains unverified; no 30K–80K capacity claim is made
  for this release.

## Relationship to the original six work areas

The interim release closes the existing-capability integration path through the
ledger/cache, profiles/comparison and product layers. Engine responsibilities,
legitimate-role coverage and language conformance remain explicitly unfinished.
Default migration is included in the interim release; full capability acceptance
remains open. Neither an interim tag nor green focused tests mean that all six
original tasks are complete.
