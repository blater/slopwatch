# SHALLOW measurement remediation plan

Status: revision 2; adversarial findings incorporated; implementation and validation pending.
Date: 2026-09-16.
Evidence: slopwatch checkout `4861224`, installed slopmark from Homebrew
`slopwatch/0.1.18`, and jname checkout `2db1a24`.

The installed binary and source checkout are separate baselines. Their recorded
identities and the complete cross-language probes are preserved in the
[baseline archive](evidence/shallow-2026-09-16/README.md).
This revision incorporates all eight findings from the
[adversarial review](shallow-measurement-adversarial-review.md).

## Intended outcome

SHALLOW should help identify abstractions that require callers to understand
or coordinate too much relative to the behavior and decisions they hide.
It should not encourage users to remove useful APIs, add meaningless overloads,
change visibility, merge unrelated classes, or move methods between files just
to pass a threshold.

The immediate recommendation is to stop treating signature-only evidence as a
measurement of depth. Retain interface observations, repair boundary and
availability handling, then validate a behavior-aware assessment before
shipping another numeric depth score. Do not tune the formula to make jname
pass 30.

This plan revises the delivery priorities in [depth-delivery-plan.md](depth-delivery-plan.md)
and the fallback policy in [depth-design.md](depth-design.md). It builds on the
movement/effect work in [depth-evidence-design.md](depth-evidence-design.md), but
requires additional validation: data movements alone do not establish how much
useful behavior an abstraction hides. These are proposed changes, not claims
that the existing approved design has already been replaced.

## Reproduction and observed failures

Run from the jname project root:

```sh
slopmark -format json -include-tests -limit 0 . > /tmp/jname-shallow.json
```

Read `files[].components.module_shallowness.evidence[].value` and its
`attributes`, rather than the weighted overall `score`.

| Source file | Raw SHALLOW | Observed evidence problem |
| --- | ---: | --- |
| `JnameCliMain.java` | 61 | One `main(String[])` entry, no output; implementation is not examined for output or delegated behavior. |
| `JnameGenerator.java` | 43 | Public constructors are counted, but inherited generation operations are absent from this file's surface. |
| `JnameGeneratorBase.java` | 36 | Public operations declared on the package-private superclass are scored separately from the externally usable generator. |
| `JnameCli.java` | 35 | Forwarding `run` signature is scored without assessing the boundary's actual responsibility or the inherited entry point. |
| `JnameStrategyTest.java` | 32 | Public overrides in private random-source helpers are counted as public operations. |

Many other files report value `0` with `available: false` and reason
`no statically identifiable public operations`. That is missing or inapplicable
evidence, not proof of deep design. The analyzer currently measures files, not
individual classes, despite the natural user expectation of class scores.

### Confirmed implementation causes

1. In [shallowness_evidence.go](../analyzers/structural/internal/metrics/shallowness_evidence.go),
   `entries += max(1, len(parameters))`, exits come from results/output flags,
   and `functionality = entries + exits`. Interface cost is also derived from
   operation counts and signature shapes. Reads and writes are always zero.
   Two implementations with identical signatures and exposure facts therefore
   receive the same score, even if one hides substantial behavior and the other
   merely forwards arguments.
2. [JavaClassFacts.java](../analyzers/structural/adapters/java/src/dev/slopslap/structural/JavaClassFacts.java)
   records explicit public methods and every interface method without checking
   effective enclosing-type visibility. Its `emitsOutput` is simply whether a
   non-void result exists. It does not assemble an inherited API. Operation IDs
   use path, simple owner name and method name, so overloads need stronger IDs
   before ownership/deduplication can safely rely on them.
3. [shallowness.go](../analyzers/structural/internal/metrics/shallowness.go)
   groups operations by declaration file. Public-type counts in the scorer and
   role classifier still use initial capitalization, which is not a Java
   visibility rule. The existing visibility test covers absent operation
   evidence, not effective visibility of types or nested public overrides.
4. [model.go](../analyzers/structural/internal/facts/model.go) lacks effective
   boundary visibility on types and uses booleans for effects. It cannot
   distinguish an absent effect from an effect the adapter cannot resolve.
5. [metrics.go](../go/internal/scoring/metrics.go) marks a component available
   when the component exists, without checking evidence-level `available`.
   SHALLOW uses sum-of-subjects aggregation. Both behaviors need revision before
   introducing multiple module subjects per file.

The standard `main(String[])` shape explains the observed 61; this is not a
universal score for all Java entry points. The larger failure is that changing
its body cannot change the signature-derived capability evidence.

## Cross-language audit: this is not a Java-only issue

Source inspection and 20 small probes against the installed slopmark confirm
both a shared conceptual limitation and adapter inconsistencies. Go, Java and
Rust feed the shared structural scorer. TypeScript implements a separate
version of the same signature-based formula in
[structural.ts](../analyzers/typescript/src/structural.ts).

| Probe | Java | Go | Rust | TypeScript |
| --- | --- | --- | --- | --- |
| One integer input/result, identity implementation | 41 | 27 | 27 | 27 |
| Same signature, negative-input rejection and iterative summation | 41 | 27 | 27 | 27 |
| One integer input, void function printing the input | 55; exits 0 | 41; exits 0 | 41; exits 0 | 41; exits 0 |
| Integer input, boolean result | 41; query | 27; query | 27; query | 28; command |

Java probes use a public class with one public method; the other columns use
exported/free functions. That packaging difference is material, so the first
row alone is not evidence that identical normalized surfaces get different
scores. A separate **TypeScript exported class** with the same method scores
**28**, with interface cost **3**, versus Java **41**, interface cost **4**.
The shared scorer adds the public type to interface cost; TypeScript records
`public_types` for reference selection but does not add it to interface cost.

The identity and summation pair is a sensitivity probe, not a claim that a loop
makes an abstraction deep. It establishes that even input validation and changed
observable behavior do not supply independent capability evidence. Readiness
for a new depth score still requires the reviewed corpus below.

Additional probes:

- Go: an exported `Run` method on an unexported `hidden` receiver with no exposed
  construction/return path is scored **28**. Name export alone does not establish
  external reachability. Conversely, an unexported receiver *can* be reachable
  through an exported factory or interface; filtering all such methods is wrong.
- Rust: `mod hidden { pub fn run(value: i32) -> i32 { value } }` is scored **27**
  despite no external crate path. The collector recurses into modules without
  propagating their visibility. Restricted visibility, re-exports and trait
  reachability need explicit boundary semantics.
- TypeScript: `function run(value: number): number { return value; } export { run };`
  reports **unavailable**, whereas inline `export function` is scored. Its surface
  collector inspects declaration export modifiers, not the resolved export graph.

### Source-level parity findings

| Area | Shared/Go/Java/Rust | TypeScript | Required convergence |
| --- | --- | --- | --- |
| Capability | Parameter/return counts; effects flags generally derived from return syntax | Parameter counts and declared/body return detection | Independent behavior evidence and explicit unknown effects in every adapter. |
| Public type cost | Shared scorer adds capitalized type names | Recorded type count omitted from interface-cost sum | Explicit reachable type facts and one common cost policy. |
| Role classification | Query uses at least half of operations with results | Query requires two thirds; booleans are status-like; syntax mutation detection | One classifier over normalized facts, including consistent status and mutation rules. |
| Visibility | Java member modifiers; Go method name export; Rust local `pub` | Top-level inline exports and locally declared class members | Audience-aware effective reachability, exports/re-exports and inherited/promoted surfaces. |
| State-only surface | Shared measurement path requires public operations before scoring | Exposed state can make assessment available with no operations | One applicability rule; data/interface assessment separate from implementation depth. |
| Type shapes | Go pointer/generic wrappers, Rust references, Java arrays/generics have differing shapes | Own type-cost and exposure implementation | Normalize caller concepts; document justified language-specific costs. |
| Exposure | Shared normalized facts and adapter recognizers | Separate interface/member/accessor heuristics | Do not count immutable contracts or method signatures as mutable representation. |

Relevant extraction code:
[Go syntax](../analyzers/structural/internal/goadapter/syntax.go),
[Rust surface](../analyzers/structural/adapters/rust/src/surface.rs),
[TypeScript operations](../analyzers/typescript/src/operations.ts),
[TypeScript signature facts](../analyzers/typescript/src/surface.ts),
[TypeScript roles](../analyzers/typescript/src/roles.ts), and
[shared roles](../analyzers/structural/internal/metrics/role.go).
These are active implementations; several files also contain commented-out
older implementations that should not be used as evidence of current behavior.

### Reproduce the cross-language probes

All 20 complete sources, the raw legacy report, extracted observations and
checksums are preserved in the [baseline archive](evidence/shallow-2026-09-16/README.md).
They are observed legacy behavior, not corrected expectations or a semantic-depth
validation corpus. Replay with an unused output path:

```sh
python3 docs/evidence/shallow-2026-09-16/replay.py --binary slopmark --output /tmp/shallow-replayed.json
```

The replay materializes sources in an isolated temporary analysis root. It does
not execute fixture code or overwrite the archived baseline. Binary provenance
was recorded during the session after the initial scan; see the manifest's
qualification rather than treating it as a contemporaneous attestation.

The identity probes are:

```java
public class Service { public int calculate(int value) { return value; } }
```

```go
package example
func Calculate(value int) int { return value }
```

```rust
pub fn calculate(value: i32) -> i32 { value }
```

```typescript
export function calculate(value: number): number { return value; }
// The separate class probe replaces the function with:
export class Service { calculate(value: number): number { return value; } }
```

The behavior variants retain these signatures, reject negative input, accumulate
integers from 1 through the input in a loop, and return the total. Void variants
print one integer through `System.out.println`, `fmt.Println`, `println!` and
`console.log`. Boolean variants return whether the integer is greater than zero.
All probes are independent source files; do not combine class/function variants
into one measured file.

## Measurement contract

The following are revision-2 design decisions. They resolve the review at the
planning level; implementation and empirical validation are still required.
The replacement initially ships findings, not an assumed numeric SHALLOW score.

### Canonical boundary inventory and comparison scope

Separate measurement subjects from navigation files. The default subject is a
**consumer contract view**, selected by these deterministic rules:

1. Enumerate externally reachable named types with callable operations. Each
   type's accessible instance, static and creation API is one view, including
   inherited/promoted members. Constructors are not independent capabilities.
2. Group exported free functions by their language-declared namespace: Go import
   package, Rust module export path, TypeScript resolved module export path.
   Java static methods remain with their declaring reachable type. These are
   namespace views, not automatically comparable to type views.
3. Represent executable/runtime entry contracts separately, with their runtime
   audience. Keep data-only and protocol-only views as interface assessments;
   do not invent implementation-depth scores for them.
4. The default audience is an external consumer of the selected build artifact.
   Internal-package and subclass-extension views require an explicitly selected
   audience and form separate inventories. Unreachable declarations are not
   external API merely because a member is public.

Use a boundary ID derived from artifact identity, audience, view kind and
qualified export/type identity, not its source file. Track implementation owners
and declaration locations separately. Re-export aliases point to the same
contract identity when they resolve to the same symbol; list aliases as access
paths, not duplicate capabilities. Distinct wrappers retain distinct views.
Nested public types have their own views; a parent lists the type reference
without absorbing its methods. A namespace lists exposed type references without
also claiming their operations. Shared implementation can support several views.

A configured component can group views for exploration, but it is a separate
assessment scope and cannot replace defaults in a historical comparison. Do not
sum parent, child, alias and component assessments. Initially there is no scalar
project-depth aggregate: show the boundary inventory, findings and coverage.
Any later aggregate needs its own reviewed ownership/weighting contract.

Cross-language parity has two tests: identical normalized evidence gives identical
assessment, and complete equivalent consumer projects produce the expected
boundary inventory. Compare type views to type views, namespace views to namespace
views, or explicit equivalent components. Do not compare a whole Go package with
one Java class by default. Genuine packaging/caller-contract differences must be
shown, not normalized away to force equal scores.

A boundary-policy or configured-scope change creates a new comparison series.
Within an unchanged view, moving helpers between files must preserve behavior
findings. API removal is an inventory/contract change, not a depth improvement.

### Caller tasks establish what is hidden

Before writing behavior recognizers, define task cards with: consumer audience,
required outcome, relevant failure cases, caller decisions and sequencing,
provider promises, implementation evidence, and unresolved obligations. These
cards describe contracts; they do not assign score overrides. Source evidence
alone cannot prove all promises or correctness.

Start with three task families, each implemented in all four languages:

| Task | Caller burden to assess | Evidence that can support transferred responsibility | Adversarial control |
| --- | --- | --- | --- |
| Allocate ordered identifiers under rollback/concurrency | Who selects clock behavior, orders updates and handles entropy exhaustion? | Resolved allocation/state-transition contract plus bounded evidence or reviewed dependency summary. | Add locks/validation while still requiring callers to coordinate ordering. |
| Encode/decode a defined format | Who knows encoding rules, validates inputs and maps failures? | Public format/error contract and evidence of delegated or local implementation. | Add a loop/checksum without relieving caller-side format work. |
| Perform an operation with resource cleanup/error translation | Who constructs resources, sequences cleanup and interprets low-level errors? | Lifecycle/error contract and observable cleanup/translation evidence. | Transparent wrapper that still requires manual caller cleanup. |

For each task compare a burden-reducing wrapper, a transparent wrapper and a
machinery-heavy implementation leaving decisions with callers. Recognizers must
link a finding to a task obligation and explain which decision is hidden. A lock,
loop, persistent read, retry or validation branch alone is not depth credit.
Known outcomes and caller costs remain separate findings until the validation
gate justifies combining them. Do not count overlapping obligations as independent
capability merely because several recognizers fire.

### Implementation ownership is not contract ownership

Maintain three identities: where code is implemented, what the boundary promises
to callers, and what callers must still decide. Copying a dependency's encoder
into the repository must not improve the assessment when the caller contract,
behavior evidence and build context are equivalent. Delegation to a pinned,
reviewed summary can support the same finding as source-resolved implementation.
Unknown dependency behavior reduces evidence coverage, not intrinsic design quality.

Do not transitively credit every dependency feature to a wrapper. Only behavior
reachable through and relevant to that wrapper's contract is assessed. A thin
wrapper hiding configuration or sequencing can be valuable; a transparent wrapper
preserving the caller's work earns no extra hidden-obligation finding. Deduplicate
observations within a task/view, not by withholding promised behavior from all
but one source owner. Cross-view findings are never summed into project capability.

Required three-way test: local implementation, contract-equivalent dependency
delegation, and transparent coordination-leaking wrapper. The first two have equal
contract findings when evidence is equivalent; the third retains caller burden.
Repeat with an unknown dependency summary and preserve the contract facts while
marking behavior evidence unknown.

### Evidence and coverage contract

Every finding records boundary/task identity, evidence provenance, dependencies,
knowledge state and resolution reason. Use `measured`, `partial`, `unavailable`
and `not_applicable` per dimension; “measured” means the defined observation is
supported, not that arbitrary semantic correctness has been proven.

| Dimension | Required inventory/denominator | Effect of unresolved evidence |
| --- | --- | --- |
| Boundary | Expected source/build inventory and reachable export/type graph | Missing files or unresolved exports block claims of a complete boundary inventory. |
| Caller burden | Enumerated operations and task-card decisions for that view | Missing generated members/signatures or unknown sequencing leave this dimension partial. |
| Promised/hidden behavior | All obligations on the selected task cards and their supporting dependency graph | An unknown essential call blocks the affected obligation, even if most calls resolve. |
| Exposure/effects | Relevant reachable state and effect paths for the selected task | Unknown effects remain unknown; absence of a recognizer is not evidence of no effect. |

Report counts as `supported / enumerated` alongside unknown inventory flags; never
present a percentage when the denominator itself is unresolved. Resolve task cards
and source inventory before declaring obligations covered. Do not let recognizer
output define its own denominator or allow configuration to silently drop hard
obligations. Coverage of predefined tasks is not coverage of all program behavior.

Propagate unknowns through finding dependencies. An unknown diagnostic call may be
nonessential only with evidence that it cannot alter the task's result, failure,
state or ordering obligations (for example, an isolated diagnostic sink with a
reviewed summary). An arbitrary logger can throw or block and is not automatically
irrelevant. Unknown dynamic dispatch implementing the task is essential. Preserve
known facts in either case; omit any composite numeric observation requiring an
unknown dimension. A partial composite never passes a depth threshold.

For interface-only subjects, implementation depth is `not_applicable`. For an
implementation with unsupported analysis it is `partial` or `unavailable`.
Changing applicable-to-inapplicable status must be reconciled as a scope/contract
change, not accepted as an improvement.

### Aggregate SCORE and fix verification

Introduce an assessment profile fingerprint containing metric definitions and
weights, boundary/audience policy, scope configuration, build context and analyzer
capabilities. Track baseline boundary/operation inventories and required evidence
coverage in fix snapshots, separately from numeric values.

- Measured-to-unknown coverage loss never satisfies a component or aggregate fix
  goal, even if the displayed sum falls. Previously applicable subjects disappearing
  require inventory reconciliation; unexplained missing subjects are incomplete.
- When required evidence is missing, an available-components subtotal may be shown
  explicitly as a subtotal, but `SCORE` pass/improvement and `valid_zero_score` are
  unknown/false for that assessment profile. Do not renormalize missing components
  into a supposedly comparable total.
- An incomplete baseline cannot authorize an aggregate improvement claim. Establish
  a fresh complete baseline under an explicitly chosen narrower profile if needed;
  never silently drop an unavailable metric or transfer the old improvement claim.
- Profile/boundary/build-policy changes invalidate automatic comparisons. Rebaseline
  and show a migration/configuration change, not code improvement. Version aggregate
  SCORE when removing legacy SHALLOW from its default composition.
- Keep the existing nonfocused-metric regression checks, but add these explicit
  aggregate checks in baseline creation, verification and fix completion. They must
  apply when `score` is the only focused metric and when no regression allowances
  were configured. Coverage loss is not a permitted numeric regression allowance.

Test file deletion, unresolved calls, lost generated members, narrowed configured
scope, `measured -> not_applicable`, incomplete baselines and default-profile
migration. Freeze scope/coverage requirements at job start. API changes can still
be accepted as product changes, but require a new contract baseline rather than
being credited as like-for-like depth optimization.

## Bounded resolution and implementation sequence

### A. Honest reporting and durable evidence

Implement evidence availability through adapter output, report projection,
JSON/text/dashboard, threshold checks and fix snapshots. Add the aggregate rules
above before excluding unavailable contributions. Separate legacy signature scores
from proposed depth findings, and version the default SCORE profile if its
composition changes. No semantic recognizer is required for this milestone.

Deliverable: archived probes, typed assessment states, migration notes and end-to-end
unknown-as-pass regressions. Legacy results remain reproducible under an explicit
legacy profile. Do not label a legacy signature observation measured semantic depth.

### B. Shared contract, then named resolution slices

Use one normalized schema, applicability policy, role classifier and scorer. Prefer
routing TypeScript facts through the shared scorer. Separate implementations are
allowed only with exact common-fixture parity and a single versioned specification.
Remove capitalization-based visibility from shared code; version adapter protocols
and reject or qualify older facts lacking required knowledge states.

| Adapter | Required context | B1: supported source slice | B2: contextual slice; explicit gaps |
| --- | --- | --- | --- |
| Java | Language level, source roots, supplied class/module path, JPMS metadata | Explicit source types, enclosing visibility, overload identity and source-local inheritance/interface views | Supplied compiled signatures and generic substitutions; missing classpaths, reflection and unprovided generated members stay partial. |
| Go | Module/workspace metadata, GOOS/GOARCH, build tags, toolchain identity | Selected source package exports, type views, local embedding/promotion and interface reachability | Resolved imports from supplied/local cache; missing modules and generated files stay partial. |
| Rust | Crate roots, edition, target, features/cfg and dependency metadata | Explicit source modules, restricted visibility, `use` re-exports and local inherent/trait views | Provided expansion/compiler metadata for complex resolution; unexpanded macros, proc-macros and unresolved trait dispatch stay partial. |
| TypeScript | tsconfig, TS version, module-resolution mode, package exports and declaration inputs | Resolved local exports/defaults/barrels, classes and local inheritance | Supplied dependency declarations/path mappings; unresolved modules and generated declarations stay partial. |

B0 first defines whole-project fixture inventories and symbol identities. B1 is
accepted independently for each named slice only when every adapter's equivalent
slice passes; B2 extends that shared matrix rather than promising arbitrary project
resolution. The legacy probes are syntax diagnostics, not substitutes for compilable
fixture projects with explicit build contexts.

All resolution is read-only by default: no downloads, builds, annotation processors,
proc-macro execution or arbitrary project scripts. Consume preexisting outputs only
with provenance. Capture selected target/features/tags and dependency/signature hashes
in reports and cache keys. Analyze multiple build configurations as separate profiles;
never infer a complete union API from one configuration.

Initial prototype limits per analysis unit: 10,000 source files, 100,000 reachable
symbols, 1 GiB worker memory and 60 seconds of resolution/summarization. Use a bounded
monotone summary domain and a worklist with at most 1,000,000 updates; unresolved
remainder becomes partial with a budget reason. These are experimental analysis
limits, not timeouts on agent fix jobs. Changes to limits are recorded in provenance;
budget exhaustion cannot silently discard facts or improve scores. Measure cold/warm
p50/p95 and peak memory on at least three representative projects per language before
B2 acceptance; report budget failures and coverage, not just successful timings.

### C. Caller-task behavioral pilot

Freeze the task rubric, sampling split and minimum gates below before recognizer
implementation. Start with the three task families above, not a general claim to
infer arbitrary functionality. Build source-resolved call summaries, known output/
effect summaries and reviewed dependency summaries with finding provenance. Resolve
recursive components within the bounded worklist; unsupported dispatch stays unknown.

Recognize standard output in all four languages, but distinguish task output from
diagnostics and do not assign automatic capability points. Deduplicate repeated
movements and proven forwarding overloads by task/outcome. Local assignments, loops,
logging and helper extraction must not manufacture hidden obligations. Any obligation
recognizer must pass a lookalike that contains similar machinery but leaves caller
burden unchanged. Configured summaries are versioned evidence, never score overrides.

Deliverable: task-linked findings, coverage reasons and consumer-contract examples.
A finding must state the caller decision removed or still required. Same-signature
pairs may legitimately tie or remain unresolved, but the pilot cannot pass solely
by abstaining: use the predeclared coverage and discrimination gates.

### D. Validation protocol and scalar go/no-go

Start with at least 48 independent abstraction families, each expressed in all four
languages where equivalent contracts are possible. Preassign 24 development, 12
validation and 12 sealed holdout families. Keep every translation, minor variation
and adversarial derivative of a family in the same split; hold out related projects
together. Add the real-project coverage sample from B2 separately to expose failures
outside recognizer-friendly fixtures. Record sampling criteria and all exclusions.

Two reviewers independently assess each task without seeing candidate scores:
what must the caller decide, what outcome is promised, what obligation is hidden,
and whether evidence supports an ordering, a tie or an unresolved judgment. Require
written contract-based explanations. A third reviewer adjudicates disagreements;
retain original labels, disagreement rate and unresolved cases. If fewer than 80%
of initial pair judgments agree on ordering/tie/unknown, revise the rubric using
only development data before progressing. Do not discard difficult disagreements.

Proposed minimum research gates, fixed before opening holdout results:

- All deterministic safety, scope, coverage-loss and packaging-invariance fixtures
  pass in all four adapters.
- At least 70% of eligible task pairs yield informative supported assessments in
  each language, and at least 50% within each task family/boundary-kind stratum.
  Abstentions remain in the coverage denominator; unsupported exports cannot be
  removed from that denominator after seeing results.
- At least 85% ordering/tie agreement with adjudicated labels among informative
  pairs in each language; no more than 5% false shallow verdicts on the labelled
  burden-reducing controls. Report counts and confidence intervals with rates;
  small strata require additional families before a production claim.
- No raw accuracy pooling may conceal a failing language or view kind. Publish
  differences versus the legacy baseline, coverage by build context, unresolved
  essential dependencies, performance and failure examples.

These are initial go/no-go criteria, not evidence that any equation is calibrated.
The small holdout supports a research decision only. A production scalar additionally
requires an independent expanded project-family holdout with at least 100 eligible
pairs per language and published uncertainty; preregister any revised statistical
criteria before collecting/opening it. Do not retry models against an exposed holdout
and continue calling it unseen validation.

If gates fail, ship useful interface/task findings and explicit unknowns; do not ship
a replacement numeric SHALLOW by lowering thresholds after inspection. If they pass,
review a proposed equation and reference policy separately, then validate it on new
holdout data. Numeric thresholds such as 30 cannot be promised by this plan.

### E. Release and comparison migration

Ship experimental findings before any scalar. Keep module subjects and source links
separate; file views do not sum overlapping boundaries. Version definition, schema,
profile composition, recognizers and boundary policy in reports and history. The
comparison engine must detect configuration/build-context changes and require a new
baseline. A change to source contents within a fixed contract is expected; disappearing
contracts still require reconciliation under the aggregate rules.

All four languages must meet the same named capability-slice contract at each release.
Publish unsupported constructs and actual coverage. Update component catalog, verifier,
fix policies and documents together. Reconcile old fallback claims and the contradictory
reference-confidence statements in `depth-reference-rules.md`. Existing approved docs
remain historical until that release explicitly supersedes their affected contracts.

## Acceptance matrix

| Change or fixture | Required result |
| --- | --- |
| Private helper or unreachable public override added | External inventory unchanged. |
| Helper moved within the same contract view | Supported task findings unchanged. |
| Direct versus inherited/promoted/re-exported API | Equivalent consumer inventory; aliases do not duplicate behavior. |
| Same normalized facts in different adapters | Same applicability, role, costs and findings. |
| Whole equivalent projects in four languages | Expected comparable view inventories, including build context. |
| Void task output through resolved delegation | Recognized output; launcher boilerplate alone produces no shallow verdict. |
| Redundant overload, parameter, loop, logging or machinery | No new hidden obligation without a caller-contract explanation. |
| Burden-reducing thin wrapper versus machinery-heavy leaky implementation | Caller-task findings reflect who must make the decisions. |
| Local implementation versus equivalent dependency summary | Same contract findings; ownership location supplies no quality credit. |
| Transparent wrapper | Dependency complexity is not inherited as hidden caller burden. |
| Unknown diagnostic versus unknown essential dispatch | Proven nonessential gaps are isolated; essential obligations remain unknown. |
| Lost source/generated export or narrowed scope | Inventory/coverage change, never a score improvement. |
| Unknown depth with SCORE as sole fix focus | No aggregate pass/completion from the lower subtotal. |
| Incomplete baseline or changed profile | New explicit baseline required; no historical improvement claim. |
| Removing a public API or selecting a larger component | Contract/scope change, not comparable optimization. |
| Unsupported macros/annotation output/dynamic dispatch or budget exhaustion | Explicit coverage reason, not measured zero. |
| Data/protocol-only view | Interface findings; implementation-depth not applicable. |
| Pure algorithm without I/O | Assessed through caller obligations, not punished for zero persistence. |
| Mutable representation leak with fixed behavior | Greater supported caller burden/exposure finding. |
| Multiple views share implementation | No project/file sum of duplicated capability. |

## Delivery gates and review disposition

| Gate | Deliverable | Completion requirement |
| --- | --- | --- |
| A | Honest availability, aggregate comparability and baseline archive | No unknown-as-pass or coverage-loss improvement in reporting/fixes. |
| B0/B1 | Contract inventories and first common resolution slice | Whole-project and normalized-fact parity; explicit gaps. |
| B2 | Supplied-context resolution and resource measurements | Build profiles, capability matrix and real-project coverage published. |
| C | Task-linked behavioral findings | Frozen rubric; controls show caller burden, not machinery counts. |
| D | Independent assessment/scalar decision | Predeclared coverage/discrimination gates; findings-only fallback. |
| E | Versioned experimental release/migration | No silent score-composition or scope comparability changes. |

A and B1 are independently useful. B2 and C are bounded prototypes; estimate them
against the named slices, not a promise of general semantic depth inference.

| Review comment | Incorporated resolution |
| --- | --- |
| 1: coverage gaming | Aggregate SCORE contract, fixed job scope, explicit incomplete-baseline/profile migration rules. |
| 2: undefined boundary | Deterministic consumer views, nonoverlap/alias rules, whole-project parity and separate comparison strata. |
| 3: obligation validity | Task cards and caller decisions, negative controls, minimum informative coverage before a scalar. |
| 4: ownership bias | Separate implementation/promises/caller decisions; local/dependency/transparent-wrapper tests. |
| 5: coverage denominator | Per-dimension inventory and essential-dependency rules; no unknown denominator percentages. |
| 6: resolution scope | B0/B1/B2 slices, four-language context matrix, read-only policy and measured resource budgets. |
| 7: corpus bias | Family/project splits, independent rubric, disagreement retention and preregistered per-language gates. |
| 8: temporary evidence | Complete durable source/report/checksum archive and isolated replay tool. |

These dispositions mean the comments have been addressed in this design revision,
not that the reviewer has approved the revision or that tests already pass.
The original [adversarial review](shallow-measurement-adversarial-review.md) remains
unchanged; its line references apply to revision 1.

## Definition of done

A user can inspect the selected consumer boundary, caller decisions, supported
hidden obligations and unresolved evidence in every supported language. Packaging,
extra implementation machinery and disappearing coverage cannot manufacture an
improvement. Comparable contracts and normalized evidence behave consistently
across adapters, with explained semantic differences. Findings remain useful even
where a number is not supportable. Numeric SHALLOW gates are introduced only after
separate empirical validation; making every jname file score below 30 is not the
acceptance criterion.
