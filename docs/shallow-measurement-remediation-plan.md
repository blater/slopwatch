# SHALLOW remediation: caller burden and hidden responsibility

> Controlling amendment: [P0 delivery directive](shallow-p0-delivery-directive.md).
> Deliver useful numeric coverage first, then enterprise performance. Earlier
> permission to withhold ratings or defer 30K-file validation is superseded.

Status: revision 6; ready for implementation with the normative specification below.
Date: 2026-09-17.

This replaces the delivery requirements of [revision 2](shallow-measurement-remediation-plan-r2.md)
and the interface-warning emphasis of revision 3. Retain the original purpose:
identify abstractions that expose too much caller burden for the useful
responsibility they hide. Deliver an explainable engineering approximation using
bounded static analysis and ordinary regression tests.

The motivating jname examples can remain unknown where generated Lombok members or
external clock/random behavior are unavailable. Source-complete equivalents must
produce numeric results; unknown is not a substitute for implementing the supported slice.

## Implementation contract

The [v4 implementation specification](shallow-v4-implementation-spec.md) now fixes
the exact formula, weights, recognizers, language slices, unknown handling, profiles,
file-level tasks and acceptance cases. Its [numeric/state fixtures](evidence/shallow-v4/scoring-cases.json)
are machine-readable. The specification takes precedence over the overview below
where it supplies more precise rules. No research or further formula-design phase
is required before implementation.

The fixed burden terms are public choices, distinct concepts, caller-supplied
argument/construction slots, required policy inputs, sequencing and representation
leaks. Hidden obligations are deduplicated across the whole boundary; service
families describe access routes, not independent credit. Required and optional
slots have distinct costs, and equivalent parameter/record packaging is normalized.
A convenience default route cannot worsen burden when it introduces no new concepts
or exposed slots. Outcome relevance alone earns no hidden-responsibility credit. The score is
`round-half-up(100 * B / (B + 2*H))`; the specification defines every term and the
integer implementation. These are versioned engineering weights, not calibrated
claims about universal software quality.

The [flow contract](shallow-v4-flow-contract.md), concrete [built-in registry](evidence/shallow-v4/builtin-registry.json)
and [source/flow cases](evidence/shallow-v4/flow-cases.json) define shared semantics.
The [adapter contract](shallow-v4-adapter-contract.md) specifies Go, Java, Rust and
TypeScript AST lowering, native edge cases and implementation ownership. Its
[all-language source matrix](evidence/shallow-v4/adapter-cases.json) supplies concrete
variants for every common case; completing translations is no longer left implicit.
The [review disposition](shallow-measurement-review-r5-disposition.md) records all 22
review findings and their resolution. These are implementation inputs, not a new
research gate.

## Metric contract

SHALLOW estimates the inverse of module depth:

```text
estimated depth = supported hidden responsibility / caller-visible burden
SHALLOW = a bounded penalty derived from that balance and representation leakage
```

The two sides must have independent evidence. Public signatures help describe
caller burden; implementation and resolved delegation establish hidden
responsibility. Parameter and return counts cannot supply both sides.

Higher SHALLOW means the supported evidence suggests a relatively burdensome
interface for what it hides. A low score is not proof of good design. Every numeric
result must explain the burden, the responsibilities and the evidence used.
Unsupported essential behavior produces an unknown assessment, not zero hidden
responsibility or a maximal shallow penalty.

Keep a numeric metric where evidence supports it. Availability fixes, interface
warnings and a findings-only prototype are intermediate work, not completion of
this plan. Do not substitute code size, complexity, private-method counts or a
collection of unrelated lint rules for the intended depth estimate.

## What is measured

### Usable abstraction, with source-file navigation

Measure an externally usable source boundary:

- A reachable named type includes its accessible constructors, methods and locally
  resolved inherited/promoted operations.
- Exported free functions form a language namespace view: Go package, Rust module
  or TypeScript module. Compare equivalent view kinds in cross-language tests.
- Resolve local re-exports as access paths to the same contract, not duplicated
  capability. Give exported callable objects a stable export identity where
  resolution is supported; otherwise explicitly qualify boundary coverage.
- Runtime entry points are identified separately. Launcher boilerplate does not
  itself establish shallowness; inspect reachable behavior where supported.
- Data-only and protocol-only boundaries have interface observations, with
  implementation depth marked not applicable.

Source files remain navigation locations. Findings carry a stable boundary ID and
source links. Display the highest supported boundary SHALLOW value for a file,
with every associated boundary inspectable. File scores and contributions are
non-additive navigation projections. Aggregate SCORE and fix verification use an
authoritative ledger of unique boundary IDs, never sums of file projections.
Moving anchors or regrouping unchanged boundaries cannot count as improvement;
legacy path-only comparison consumers must refuse comparison until rebaselined. If an associated applicable boundary is unresolved,
qualify the file assessment and block a complete aggregate pass.

Moving a helper between files within the same boundary must not change its depth.
Changing or deleting the public contract is a scope change requiring comparison
reconciliation, not automatically an improvement. A public generator and its
package-private base implementation must be assessed through the usable generator
contract rather than receiving unrelated file-signature verdicts.

### Caller-visible burden

Record supported evidence of what a caller must learn, supply or coordinate:

| Burden | Examples of evidence |
| --- | --- |
| Interface choices and concepts | Distinct operations, caller-facing types and configuration choices; deduplicate aliases and normalize equivalent type wrappers. |
| Exposed representation | Mutable fields, backing structures or internal handles that the caller must manipulate. |
| Sequencing and lifecycle | Separately exposed initialization, acquisition, use and cleanup operations with resolved state/resource relationships. |
| Error and policy decisions | Low-level failures or policy inputs passed through for the caller to interpret or coordinate. |

A parameter, callback or public method is not inherently bad. Record its role and
bounded cost; avoid penalizing language syntax or packaging alone. Do not infer a
sequencing obligation from method names or declaration order. Local boolean/enum
selectors count as policy only when they choose distinct recipes over independent
payload or resources. A boolean used as data is not automatically policy.
Explicit constructors, implicit creation and equivalent factories expose the same
creation family; passive record construction earns no transformation credit.

### Hidden responsibility

Trace public operations through source-resolved helpers and delegation. Start with
five bounded responsibility categories:

| Responsibility | Required connection to observable behavior |
| --- | --- |
| Validation and error translation | Checks or translations govern accepted inputs, observable failures or recovery at the boundary. |
| Resource lifecycle | Acquisition and cleanup are connected to the operation's resource and supported exit paths. |
| State consistency and ordering | State transitions enforce an observable invariant or ordering relationship, within the recognizer's explicitly supported pattern. |
| Synchronization | Coordination protects relevant state accessed through the boundary; a lock elsewhere is insufficient. |
| Transformation | Public inputs are transformed into meaningful results or outputs; include pure computation and encoding/decoding, not only I/O. |

These are distinct supported responsibilities, not counts of branches, statements
or calls. Deduplicate by boundary, responsibility and governed state/resource or
outcome. Several checks for one obligation do not create several capabilities.
A general transformation recognizer may support an approximate finding without
claiming to prove the algorithm's domain-specific correctness. Its resolution is
limited: a primitive calculation and substantial pure computation with the same
interface can both receive one X obligation and the same score. Paired fixtures
preserve this explicit limitation; loops/operators do not buy extra credit.

Resolved alternatives retain obligation sets. Compute the minimum weighted union
across boundary alternatives, deduplicating before scoring. Do not intersect away
a C-versus-X choice, or lend validation credit to an unvalidated route.

The supported-pattern table must say what each recognizer establishes and what it
does not. For example, recognizing guarded allocation is not proof that arbitrary
concurrent code is correct, and observing cleanup on one path is not proof of
cleanup on every path.

### Delegation and wrappers

Follow delegated behavior through supplied source context. A wrapper can provide
substantial capability through a dependency; implementation ownership supplies no
quality bonus. Carry only responsibilities reachable through that wrapper's public
contract, not every capability of the dependency.

- Direct forwarding earns neither an automatic penalty nor extra capability.
  Assess the behavior and burden actually exposed through it.
- A wrapper that supplies configuration, translates failures or handles cleanup
  gets evidence for the caller decisions it removes.
- A wrapper that still requires callers to acquire, order, synchronize or clean up
  retains those burden observations.
- Unknown dependency behavior remains unknown. Preserve resolved interface facts,
  but withhold a composite score if that behavior is essential to the assessment.

No hand-authored task cards are required from repository users. Built-in bounded
recognizers and supplied source context provide the initial evidence. General
third-party behavioral summaries are future work, not a dependency of this fix.

## Bounded implementation

Use the existing parsers, analysis units and supplied build context. Support
source-local resolution first:

- Java: effective enclosing visibility, overload identity and local inheritance.
- Go: exported package API, local receiver reachability and embedding.
- Rust: module visibility, restricted exports and resolvable local re-exports.
- TypeScript: named/default exports, local barrels and class inheritance.

Do not download dependencies, execute builds, annotation processors, macros or
project scripts. Unresolved dynamic dispatch, unavailable generated code and
external behavior have explicit coverage reasons. Multiple build configurations
remain separate analysis profiles; do not claim a complete union API.

Use one normalized evidence schema and one scoring policy. If TypeScript retains
a separate implementation, run the same normalized-fact fixtures against both.
Each responsibility and burden observation records its boundary, category,
source evidence, dependency identities, knowledge state and explanation.

The normative specification supplies the recognizer triggers, exclusions,
deduplication rules, bounded weights, exact numeric combination and worked examples.
Implement that policy and its tests. Do not substitute per-language formulas or
silently tune weights to force a desired real-project score.

## Delivery outcomes

The following four groups describe required outcomes, not execution order.
[Specification section 9](shallow-v4-implementation-spec.md#9-sole-execution-order-and-file-ownership)
is the sole dependency-ordered implementation sequence. Enable the new default
only after reporting, comparison and verifier integration is complete.

### 1. Honest evidence and safe comparisons

Carry measured, partial, unavailable and not-applicable states through JSON, text,
the dashboard, threshold checks and fix snapshots. Keep measured zero distinct
from missing evidence. Preserve useful results when another file or language fails.

An available-components subtotal may be displayed as such. Missing required
evidence must block aggregate SCORE pass/improvement and valid-zero claims,
including jobs whose only focus is SCORE. Freeze baseline scope, metric policy and
required coverage; reject unexplained target/contract loss and profile drift.
Incomplete baselines require an explicitly chosen new complete baseline.

### 2. Boundary extraction and responsibility summaries

Required approved extension: implement the
[legitimate structural-role backlog](shallow-v4-legitimate-roles-backlog.md).
Recognized legitimate uses must affect applicability, boundary selection or
supported responsibility before producing the main verdict; explaining away an
unchanged high score in a role label is insufficient. Detail evidence accompanies
that decision. Complete this extension before default rollout, including updated
policy identity, counterexample fixtures and cached dependency invalidation.

Implement the supported boundary slice and normalized evidence. Build bounded,
cycle-safe summaries for reachable local helpers; preserve unknowns instead of
recursing indefinitely. Deduplicate inherited/delegated evidence and capture its
cache dependencies. Add positive and negative fixtures with each recognizer.

### 3. Numeric replacement and explanations

Implement the reviewed rule table. The info panel must show caller burden,
responsibilities hidden, leakage, unresolved evidence and the calculation's basis.
For example: “One public operation; allocation and synchronization handled
internally; rollback behavior unresolved.” If that unresolved behavior is essential,
show the findings but no complete numeric verdict.

Give the replacement a new definition and SCORE profile identity. Preserve the
old signature implementation under an explicit legacy profile for reproduction.
Reject saved legacy numeric goals under the new profile with an actionable
rebaseline message; never silently reinterpret their thresholds. Update catalog,
verifier, UI explanations and affected depth-design documents together.

### 4. Regression validation and release

Use approximately 12 small scenario families, translated across the four languages
where contracts are equivalent. This is an ordinary engineering regression suite:

| Scenario | Expected behavior |
| --- | --- |
| Private helper or unreachable public member added | External boundary and assessment unchanged. |
| Inheritance, embedding or re-export | Equivalent usable contract; no duplicate responsibilities. |
| Overloads and aliases | Stable identities; redundant surface does not create new capability. |
| Runtime entry point | No boilerplate-only shallow verdict; inspect supported delegated behavior. |
| Allocation with internal coordination versus caller-coordinated allocation | Evidence distinguishes responsibility hidden from burden left to callers. |
| Direct delegation versus a wrapper hiding configuration/cleanup | Follow delegated capability; credit supported reduction in caller burden. |
| Exposed mutable representation versus encapsulation | Greater exposed burden for equivalent provided responsibility. |
| Useful pure transformation | Recognize supported functionality without requiring persistence or I/O. |
| Extra loops, logging, checks unrelated to outcomes or unused locks | No automatic depth improvement. |
| Helpers moved or split within a boundary | Same supported responsibilities and numeric assessment. |
| Unknown essential dependency, coverage loss, target deletion or profile change | No manufactured improvement or automatic fix completion. |
| Partial parser failure | Affected evidence unavailable; other supported results survive. |

Include local-versus-delegated equivalents and negative lookalikes that contain
similar machinery without hiding the stated responsibility. Tests must exercise
both supported numeric outcomes and explicit abstention; making every behavioral
case unavailable does not satisfy the plan.

The main agent defines/reviews expected outcomes from the actual source and
reviews the code. Luna/high agents implement the changes and tests. No external
reviewer panel, blind labelling exercise or statistical accuracy gate is required.

Run before/after reports on jname, SlopWatch and ap. Explain the original jname
examples individually and inspect a bounded sample of new high scores elsewhere.
Correct demonstrable false positives while retaining counterexamples. These checks
validate intended behavior, not a claim of universal semantic accuracy.

## Cache and scale constraints

Reuse existing caches and dependency invalidation for boundary and responsibility
summaries. Changes in relevant helpers, exports or supplied dependency context
invalidate their consumers. Unchanged inputs reuse summaries. Transient worker or
resource failures remain retryable and must not become permanent cached unknowns.

No source scans or semantic analysis in rendering or idle polling. No unrestricted
worker fan-out. Add deterministic work-count checks for idle refresh and an edit
that must leave unrelated units untouched. For this release, benchmark `~/src/river`
for cold startup, warm refresh, idle polling and representative implementation and
public-contract edits, comparing the same snapshots with the frozen legacy baseline.
This user-approved scope amendment replaces the generated 30K release benchmark.
Retain deterministic fan-in, diamond, chain, SCC, widening and work-limit tests.
Enforce logical work, retained payload and the River runtime gates in specification
section 8 and flow-contract sections 6–7. Cached summary consumption has the same
logical cost as fresh summaries; warm caches cannot change availability or scores.
Benchmark wall-time and process-tree memory separately from deterministic unit tests.
Generated 30K and 80K capacity runs, including large synthetic timing/RSS workloads,
are follow-up work, not release prerequisites. River results must not be presented
as validation at those larger scales. Larger unsupported units must report their
actual limitation rather than being silently skipped.

## Definition of done

- SHALLOW numerically relates independently evidenced caller burden and hidden
  responsibility within its documented supported slice.
- A useful thin abstraction can receive credit for what it hides; implementation
  machinery alone cannot make an abstraction look deeper.
- All four adapters pass equivalent boundary and normalized-evidence fixtures,
  with explicit reasons for unsupported language constructs.
- Users can inspect the evidence and calculation, including inherited/delegated
  behavior and unresolved dependencies.
- The known jname failures have explained outcomes without target-score tuning.
- Unknown evidence, contract loss and profile migration cannot manufacture a pass.
- Regression tests, real-project checks and bounded cache/scale checks pass.
- The versioned numeric replacement and migration are delivered. Infrastructure
  alone or permanent deferral to findings-only output does not complete the work.

General semantic inference, comprehensive artifact discovery, arbitrary dependency
summaries and scientifically calibrated accuracy remain future work. They are not
release gates for this engineering approximation.
