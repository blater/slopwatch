# Adversarial review: SHALLOW remediation plan

Date: 2026-09-16.
Reviewer: independent Codex sub-agent, explicitly requested by the project owner.
Status: review comments; not yet incorporated into the plan.

Reviewed [shallow-measurement-remediation-plan.md](shallow-measurement-remediation-plan.md)
(the 447-line draft), with checks against the current facts schema, scoring
projection and fix verifier. Line references below refer to that draft.
The reviewer made no implementation changes.

## Verdict

The plan needs amendments before it is an implementable replacement measurement
contract. Four blockers concern coverage-based score gaming, undefined boundary
selection, the validity of hidden-obligation evidence, and dependency ownership.
The reporting repair can be separated from the research work, provided aggregate
score and fix behavior receive explicit coverage rules first.

## Blockers before implementation or release

### 1. P1 — Removing unavailable contributions can reward losing coverage

Plan lines 274–284 and 383–386 require coverage labels but do not define aggregate
SCORE comparisons and fixes when coverage changes. A previously measured shallow
component becoming unresolved lowers its contribution and therefore SCORE.

Current [scoring/projection.go](../go/internal/scoring/projection.go) sums
contributions. [verification.go](../go/internal/fixanalysis/nativeadapter/verification.go)
and [verification_checks.go](../go/internal/fixanalysis/nativeadapter/verification_checks.go)
explicitly skip focused `MetricScore` completeness checks. Existing regression
checks protect some cases: a previously complete nonfocused metric becoming
incomplete is rejected. That protection does not replace an explicit contract
for aggregate coverage, incomplete baselines and metric composition changes.

**Requested amendment:** Require stable assessment scope and coverage for
improvement claims. A measured-to-unknown transition must never satisfy an
aggregate-score improvement or fix goal. Add fixtures for lost source files,
newly unresolved calls, changed boundary configuration, and a migration removing
SHALLOW from default SCORE. Version aggregate SCORE as well as SHALLOW when its
composition changes.

### 2. P1 — No canonical unit establishes cross-language comparability

Plan lines 199–205 permit Java types/components, Go packages/type views, Rust
crates/modules/type views, and TypeScript modules/classes. These overlap and
differ in granularity. Identical normalized facts yielding identical scores is
necessary, but does not establish equivalent boundary selection.

For example, one Go package containing several APIs can hide more behavior than
one Java class solely because its default boundary is larger. Explicit component
selection also lets users redraw the boundary around a poor abstraction.

**Requested amendment:** Define deterministic default boundary selection, audience,
nesting, overlapping views and ownership before checkpoint B. Provide complete
cross-language fixture projects with equivalent consumer-facing contracts and
expected boundary inventories, not merely normalized-fact golden tests. Make
configuration changes break historical comparability.

### 3. P1 — Hidden obligations remain an unvalidated substitute for depth

Plan lines 226–246 and 346–356 describe recognizers without operationalizing how
the findings demonstrate reduced caller burden. A tiny wrapper can eliminate
difficult configuration and sequencing. A large implementation can perform
validation and recovery while exposing those decisions to callers. Recognizing
implementation features does not distinguish these cases.

The “or inconclusive” acceptance condition also lets the pilot pass while
resolving almost nothing.

**Requested amendment:** Define a small set of caller tasks, their required
decisions, and the obligations transferred behind the boundary. Include pairs
where implementation machinery increases without reducing caller burden, and
simple wrappers substantially reduce it. Set minimum informative coverage and
discrimination criteria before building recognizers. Permit a findings-only
outcome if these fail.

### 4. P1 — Source ownership risks becoming another packaging bias

Plan lines 332–351 preserve internally delegated capability but withhold automatic
credit for dependencies outside the boundary. The same abstraction could appear
deeper when its encoder is copied into the repository than when the identical
implementation comes from a dependency. Blindly inheriting dependency behavior
would instead reward transparent wrappers. Project-wide deduplication does not
answer the per-boundary question.

**Requested amendment:** Separate implementation ownership, behavior promised to
callers, and decisions hidden from callers. Add a three-way fixture: owned
implementation, contract-preserving dependency delegation, and a transparent
wrapper that leaves coordination with the caller. Explain which assessments
must remain invariant and which may differ.

## Required before checkpoint acceptance

### 5. P2 — Coverage has states but no denominator or scope

Plan lines 234–259, 324–328 and 413–415 do not specify what “partial” means for a
boundary or when a numeric observation remains usable. One unknown diagnostic
call should not necessarily erase known behavior; one unknown dynamic dispatch
could contain nearly all relevant behavior. Counts of resolved declarations
cannot distinguish these cases.

**Requested amendment:** Define per-dimension completeness requirements and
explicit unknown dependencies. Specify rules for missing bodies, calls, exported
symbols, generated members and failed source inventory. Test unknown nonessential
diagnostics and unknown essential implementations. Keep known findings visible
without presenting partial totals as complete scores.

### 6. P2 — Semantic resolution lacks a build-context and resource contract

Plan lines 291–328 promise generics, overrides, promoted methods, re-exports and
trait views across four adapters. Current normalized facts are predominantly
source-structural facts, not a complete symbol-resolution graph. Rust features
and macro-generated exports, TypeScript module resolution, Java classpaths and
Go build tags can change the effective API without changing the inspected file.

Marking unsupported cases partial is sensible, but does not define an estimable,
acceptably bounded deliverable.

**Requested amendment:** Add a per-language resolution capability matrix, required
inputs, selected build configuration, dependency policy and time/memory limits.
Stage B into named supported slices. Include resolved build context in provenance
and cache keys. Measure coverage on representative real projects before promising
“all-adapter effective APIs.”

### 7. P2 — The corpus could validate recognizer familiarity rather than depth

Plan lines 360–376 call for blinded reviewers and a holdout set, but leave reviewer
instructions, sampling, disagreement treatment and success thresholds unspecified.
A holdout of minor variations on recognizer development examples may look
successful while missing unfamiliar libraries. Overall accuracy can conceal
much lower coverage in one language.

**Requested amendment:** Freeze a review rubric and sampling protocol before
recognizer work. Split by project or abstraction family, not individual fixture.
Report coverage and errors by language and boundary kind. Include adversarial
lookalikes, external-library wrappers and difficult disagreements. Require a
useful measured fraction so abstaining on all hard cases cannot pass.

## Optional improvement

### 8. P3 — The empirical basis is not durable

Plan lines 90–114 and 154–162 rely on 20 probes whose complete inputs and outputs
are in `/tmp`; only part of the corpus is embedded in the document. Important
claims therefore depend on temporary files and two different baselines.

**Requested amendment:** Preserve complete probe sources, invocation, binary
version/hash and raw results in the repository before implementation starts.
Keep baseline observations distinct from expected corrected behavior.

## Suggested disposition

Resolve comments 1–5 as measurement-contract decisions, then use comments 6–7 to
bound the implementation and validation work. Preserve the evidence in comment 8
before temporary files disappear. Re-review the amended contract before assigning
numeric SHALLOW targets or authorizing automated fixes against the replacement.
