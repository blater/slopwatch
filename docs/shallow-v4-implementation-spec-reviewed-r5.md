# SHALLOW v4 implementation specification

Status: ready for implementation; engineering policy, not empirical calibration.
Date: 2026-09-17.
Parent: [remediation plan](shallow-measurement-remediation-plan.md).
Definition: `responsibility-burden-v4`. Normalized facts: version 3.

This document settles the scoring and implementation decisions left open in the
plan. Implement these rules and tests, including their limitations. Changing a
rule after finding a counterexample requires a documented policy/test change, not
an undisclosed adapter-specific adjustment. No research phase precedes coding.

## 1. Exact score

For one applicable boundary with complete evidence under the supported rules:

```text
B = O + 0.5*T + 0.25*A + P + S + 2*L
H = sum over service families of (U + V + 2*R + 2*C + 2*Y + 2*X)
SHALLOW = floor(100 * B / (B + 2*H) + 0.5)
```

Use rational/integer arithmetic: `B4 = 4*O + 2*T + A + 4*P + 4*S + 8*L`,
`denominator = B4 + 8*H`, result is integer round-half-up of
`100*B4 / denominator`. Do not round intermediates. Result is in [0,100].
An applicable callable boundary has O >= 1, so division by zero cannot occur.

| Symbol | Meaning | Counting rule |
| --- | --- | --- |
| O | Public operation choices | One per callable signature, including explicit public constructors; aliases count once. |
| T | Signature concepts | Distinct normalized concepts across the whole boundary, counted once regardless of occurrences or input/output direction. |
| A | Caller-supplied slots | One per declared parameter position across public signatures, excluding implicit receivers; add required public record-construction fields as defined below. |
| P | Required supplied policies | One per service-family policy slot that callers must supply on every available entry route to that family. |
| S | Caller sequencing obligations | One per distinct supported lifecycle relation left to callers. |
| L | Mutable representation leaks | One per distinct boundary-owned mutable storage root exposed by reference or public writable access. |
| U | Implemented outcome | Boolean per family: a normal result or externally observable effect is established from the body/summary. |
| V | Validation/error adaptation | Boolean per family, at most one despite multiple checks/translations. |
| R | Resource lifecycle handled | Boolean per family, at most one despite multiple resources. |
| C | Private state transition handled | Boolean per family, at most one despite multiple fields. |
| Y | Relevant synchronization handled | Boolean per family, at most one despite multiple locks. |
| X | Input/state transformation | Boolean per family, at most one despite multiple stages or loops. |

Responsibility categories deliberately saturate per family. They estimate breadth
of responsibility hidden, not implementation size or number of mechanisms. A family
has at most 10 H units. These initial weights are explicit engineering choices.
Validation is worth less than lifecycle/state/synchronization/transformation;
representation leakage adds twice an ordinary public choice to burden. There is
no language-specific multiplier, role prior, confidence multiplier or LOC term.

The old signature-based F/I calculation and role-dependent depth references are
not used by v4. Leakage is included in B; do not add a second leakage penalty.

### Signature concepts (T)

Normalize integers/floats/decimal to `number`, booleans to `boolean`, text/character
to `text`, byte sequences to `bytes`, and unit/void to no concept. Nullability,
pointers, references and ownership wrappers add no concept by themselves.
Arrays/lists/sets add `sequence` plus their element concepts (except byte sequences).
Maps add `map` plus key/value concepts. Tuples add their member concepts. A named
public domain type or enum adds its canonical contract ID once. An anonymous
record adds `record` plus its public member concepts. A callback adds `callback`
plus argument/result concepts; unions add `choice` plus their alternatives.

Memoize recursive types by symbol ID. The boundary's own receiver type is implicit
and adds nothing; explicit receiver arguments in free functions remain concepts.
Do not charge for class declarations themselves. For input records/domain objects whose construction is part of the public contract,
include their public field concepts recursively. A adds each required writable
construction field once per canonical input type, including nested required fields;
stop at recursive references. Explicit public constructors already account for their
parameters, so do not charge their assigned fields again. Optional/defaulted fields
add concepts but not required construction slots. Opaque values obtained from another
API do not incur invented construction costs. Mutable boundary-owned fields are
accounted for by L, not construction slots. Unresolved signature shapes
make burden partial, not a guessed primitive. A public declared type whose private
implementation is unavailable can still be a known opaque concept.

### Required policy slots (P)

A slot is established only by data flow from a public input to either (a) invocation
of a caller-supplied callback, or (b) a policy argument of a recognized built-in
summary. Deduplicate by resolved callee and formal policy position, not parameter
name. Inputs merely being transformed are not policies. Mutating an ordinary
caller-provided output buffer is not policy selection or an owned-state leak.

For equivalent entry routes, a supplied default makes that slot optional: it adds
no P, although its public overload still adds O and its exposed type contributes T.
Do not infer policies from names such as `strategy` or `options`.

### Service families and duplication

Start with one family per callable executable operation; constructors add O/T/P
but no U or H simply for creating the receiver. Constructor initialization can
support a method's C/R evidence; acquisition alone is not lifecycle handling.

Merge operation families only for aliases, inherited references to the same method,
or proven forwarding to the same resolved operation. A forwarding proof allows
renamed parameters, fixed defaults, constructor wiring and identity conversions;
it requires the same normal result and effect sequence and no additional escaping
state or effects. Defaults remain burden facts. Two unrelated methods with the
same return type or similar names are not merged.

Validation or error translation around a delegated operation adds V to that
family. An enclosing cleanup wrapper adds R only when it closes the caller-facing
lifecycle. Do not import all operations of a delegated type. For any other local
call graph, collect reachable category evidence once per root family. Adding
public APIs changes contract inventory and cannot satisfy a like-for-like fix goal,
even if it changes the raw ratio.

## 2. Recognizers: positive rules and exclusions

Extract a small def-use/control-flow representation from each parser. Track public
inputs, constants, owned fields, local aliases, known returned handles, call targets,
normal/error exits and observable sinks. Evidence must be reachable from an entry
and relevant to an observable outcome or its governing state/resource.

| Rule | Positive trigger | Exclusions / unknown cases |
| --- | --- | --- |
| U | Body returns a value normally, writes caller-visible output/state, or invokes a summarized output/effect sink. | A declaration or constructor alone is insufficient. A throw-only or no-op body has U=0 when fully resolved. Unknown callee effects are not guessed. |
| V | A condition depending on public input or outcome-relevant state selects a failure exit before the affected outcome; or a caught/returned lower-level failure is translated into a different exposed error type/tag. | Logging and unreachable checks do not count. Repeated checks still V=1. Bare rethrow, unchanged error forwarding and naming a helper `validate` do not count. |
| R | Successful acquisition of a recognized resource is matched to cleanup on every supported exit path before ownership returns to the caller. | Opening alone, returning a live handle, cleanup on only one path, and unknown ownership/exception paths do not establish R. |
| C | A private owned storage root is read and updated; the update depends on its previous value or controls a later observable result, and the caller cannot write/alias that root. | Local accumulators, mere construction, write-only assignment setters, backing-state leaks and unrelated counters do not count. This observes a state transition, not a proof of arbitrary invariants. |
| Y | The outcome-relevant state accesses for the family are enclosed by a recognized monitor/guard or use a recognized atomic read-modify-write. Lock release follows the supported exit semantics. | An unused lock, locking unrelated data or guarding only part of the relevant access set does not count. This is evidence of internal coordination, not a thread-safety certification. |
| X | A normal outcome or its governing state update depends on public input/prior state through a non-identity arithmetic, Boolean, selection, indexing/lookup, text/record construction, encoding or reduction expression. | Direct copies, getter returns, error-message formatting, local machinery not reaching an outcome and identity conversions do not count. A method named `encode` is not evidence. |
| S | A caller-visible resource produced by one operation must be supplied to a use operation (acquire→use), or a public close/release operation remains required (use→release). Establish each relation from matching resource identity and recognized operations. | Never infer arbitrary protocols from method names or order. Automatic cleanup closes the corresponding relation. Unsupported resource escape makes this dimension partial. |
| L | A public writable field exposes owned storage, or a public return/accessor provides a writable alias to a private backing root. | Immutable snapshots, defensive copies, read-only views with proven no mutable alias, constants, input records and protocol declarations are not leaks. Unresolved aliasing stays unknown. |

C describes state responsibility, including allocation/ordering when the recognized
transition supplies that behavior. Do not award a separate bonus for inferred
domain-specific promises such as “monotonic ULID”. Explain the actual transition.
X includes small useful pure computations; it does not require persistence or I/O.

Perform constant propagation and the following identity reductions before X:
parentheses, alias copies, widening identity casts, `x+0`, `x-0`, `x*1`, `x/1`,
`x|0`, `x^0`, `x&&true`, `x||false`, and branches selecting the same normalized
value. Apply an identity only when the language/type semantics make it valid
(e.g. do not treat JavaScript `x|0` as identity for an arbitrary number). Constant
conditions prune unreachable branches. Fold literal-only primitive expressions.
Do not claim general algebraic or semantic equivalence. Unsupported arithmetic
semantics retain an explicit approximation note, not fabricated precision.

Adding the first genuine input-dependent transformation can change X. Adding
another transform, loop or helper to that family cannot increase X beyond one.
The heuristic does not prove that a changed algorithm is useful or correct;
ordinary tests/review remain responsible for behavioral correctness.

## 3. Resolution and built-in summaries

Use source and preexisting declaration metadata only. Resolve imports/aliases by
symbol and qualified owner, never by a method's short name. Constructor injection
with a uniquely known implementation resolves to that implementation. Finite local
receiver sets may be joined: only categories true for every reachable alternative
are guaranteed; known absent/present disagreement is a supported conditional fact
with weight zero. An unresolved alternative is unknown, not absent.

Local helper summaries carry parameter/result bindings, category evidence,
owned-state/resource roots, escaping aliases and unresolved calls. Collapse call
cycles and propagate finite sets to a fixed point; recursion alone earns nothing.
Store the summary once per function and substitute bindings for each caller.

Built-in registry `shallow-builtins-v1` initially covers these exact symbol families:

| Language | Output/failure and pure operations | Resource and coordination patterns |
| --- | --- | --- |
| Java | `System.out/err.print/println`, `PrintWriter.print/println`; `Objects.requireNonNull`; primitive operations; `String.concat/substring/charAt/toLowerCase/toUpperCase/length`; `Arrays.copyOf`; `BigInteger(byte[])` and `BigInteger(int,byte[])`, `valueOf/add/subtract/multiply/divideAndRemainder`; typed throw/catch. | `try`-with-resources for a resolved `AutoCloseable` or local known close contract; `synchronized`; local lock/unlock protected by `finally`; `AtomicInteger/Long` RMW methods. |
| Go | Built-in primitive operations; `fmt.Print/Printf/Println/Fprint/Fprintf/Fprintln`; `errors.New`; `fmt.Errorf`; standard error-return branches; `copy` on distinct buffers. | `os.Open/Create/OpenFile` and `(*os.File).Read/Write/Close`; matching `defer Close`; `sync.Mutex/RWMutex` acquisition and deferred unlock; `sync/atomic` RMW. |
| Rust | Primitive operations; built-in `print!/println!/eprint!/eprintln!`; Result/Option construction, matching and `?`; slice copies. | `std::fs::File` acquisition/read/write and scoped Drop; `std::sync::Mutex/RwLock` guards and scoped Drop; standard atomic RMW. |
| TypeScript | Primitive operations; resolved standard `console` output; typed throw/catch; `String.concat/slice/substring/toLowerCase/toUpperCase/trim`; `Array.slice/map/filter/reduce` (callbacks must resolve), with explicit copy-versus-alias semantics. | Node `fs/promises.open` FileHandle acquisition/read/write/close with awaited `try/finally`; no implicit synchronization assumption for JavaScript execution. |

Registry entries specify normal outputs, exceptional exits, side effects, aliasing
and formal policy slots. They do not silently treat arbitrary library calls as
pure. For Go OpenFile, flags/permission are policy slots; for other entries the
registry explicitly lists slots or an empty set. No clock/random callback contract
is guessed. An unknown callback governing allocation leaves that behavior partial.

Only syntactically built-in Rust macros listed above are expanded as patterns;
other macros are not executed. User-defined lookalikes and shadowed imports must
fail the built-in match. Pure library methods not explicitly enumerated in registry
data remain unknown. Registry implementation must list concrete overloads/methods,
not use wildcard name matching; its checked-in data is part of the policy hash.

For a shared language-neutral coordination fixture, TypeScript uses a source-local
lock implementation with a uniquely resolved contract, or reports Y unavailable
for a genuinely unsupported construct. Do not manufacture language parity by
claiming an ordinary asynchronous function is synchronized.

## 4. Boundary and language slice

Identity is `(artifact, audience=external, view_kind, canonical_symbol)`; it excludes
source coordinates and body hashes. Artifact identity is the normalized workspace-
relative descriptor: Go go.mod/module path, Java supplied module/source-root group,
Rust Cargo.toml/crate target, TS tsconfig/package root. With no descriptor use one
explicit `source-root:<language>:<relative-root>` artifact and mark build selection
inferred. Analyze independently discovered artifacts separately. Ambiguous ownership
produces an inventory diagnostic and partial boundary, never arbitrary assignment.

| Adapter | Must implement | Explicit limit |
| --- | --- | --- |
| Java | Effective visibility through enclosing classes; qualified owner+erased parameter signature IDs; local class/interface hierarchy; public inherited members of nonpublic bases through reachable subclasses; constructor-selected local dispatch. | Unprovided generated members, unresolved superclasses and open external dispatch remain partial. Method bodies use supported structured control flow; exception uncertainty blocks affected R/Y evidence. |
| Go | Selected package files; export objects using go/types with local/source or preexisting export data; exported methods on unexported receiver types reachable through exported returns; local embedding/promotion and finite interface receivers. | Missing import data, unsupported build selection and escaping unknown implementations remain partial. No downloads or implicit `go build`. |
| Rust | Explicit module tree, `pub`, `pub(crate)`/restricted visibility, local `use` aliases/re-exports, inherent methods and locally resolved trait impls. | Unexpanded non-built-in macros, unknown cfg choices, external trait dispatch and ownership escapes outside supported patterns remain partial. |
| TypeScript | Existing compiler API for local symbols, named/default exports/barrels, classes and inherited members; exported functions/objects and factory-returned anonymous callable views keyed by export path. | Unresolved modules, generated declarations, dynamic property dispatch and ambiguous assignments remain partial. Declaration files provide context, not scored implementation rows. |

Closed literal build selections are honored; unresolved choices cannot be claimed
as a complete union. Do not add general multi-configuration exploration in v4.

Type anchors are their reachable public declarations. Namespace anchors are the
lexically first exported declaration path. Anchor changes only affect navigation:
history and comparison use boundary identity. Emit each boundary measurement once
at its anchor; other participating files carry boundary references/source links.
A file with several anchored boundaries displays their maximum. References alone
do not add SCORE contribution. Runtime-entry bodies are analyzed for supporting
summaries but have depth `not_applicable`; their usable library APIs are separate.

Constructor-only objects, interfaces without implementation, and data-only records
are `not_applicable`. A fully resolved callable no-op is applicable with H=0.
Private-only implementation files refer to reachable owning boundaries; otherwise
their SHALLOW is not applicable. Do not label “no public operations found” as proof
of inapplicability when export/build inventory itself is incomplete.

## 5. Knowledge states and numeric eligibility

Each boundary has independently tracked `inventory`, `burden`, `behavior`, and
`alias_effects` dimensions. Each is measured, partial, unavailable or not_applicable,
with stable reason codes and evidence locations. Category facts also distinguish
supported-present, supported-absent, supported-conditional and unknown.

“Measured” means complete for the documented v4 rule set, not complete semantic
knowledge. An expression outside a recognizer is not automatically unknown if its
effects and data flow are fully represented and prove the category absent.
An opaque call, missing body, unresolved export or alias can make relevant absence
unprovable. Preserve known facts in either case.

Numeric eligibility requires complete applicable inventory and all applicable
scoring dimensions measured. Unknown essential behavior withholds the score.
A call is nonessential only if its resolved summary proves it cannot affect the
normal result, failure paths, relevant state, resource lifecycle or ordering. An
arbitrary logger is not assumed harmless. Optional diagnostic coverage is reported
separately and never used to inflate H.

Use nullable v4 boundary values; unknown is not serialized as numeric zero. Keep
legacy numeric values in explicitly named legacy fields/profile output. At file
level retain a supported-component subtotal if useful, with `score_complete=false`,
`valid_zero_score=false` and no pass when any required applicable evidence is missing.
Explicit not-applicable boundaries do not make an otherwise complete file fail.
Loss from measured to not-applicable is an inventory change during verification.

JSON and the TUI info view include O/T/A/P/S/L, every family's U/V/R/C/Y/X, B, H,
rule version, source links, unknown reasons and boundary IDs. Text/TUI display X for
unresolved metrics and – for inapplicable metrics. Sorting must distinguish numeric,
unknown and inapplicable rows consistently, retaining stable path tie-breaking.

## 6. Exact acceptance cases

The machine-readable [scoring cases](evidence/shallow-v4/scoring-cases.json) are
normative normalized-fact inputs and numeric outputs. Key examples:

| Case | O,T,A,P,S,L | H | SHALLOW |
| --- | --- | ---: | ---: |
| `number identity(number)` | 1,1,1,0,0,0 | U=1 → 1 | 47 |
| Same signature, nonidentity pure transformation | 1,1,1,0,0,0 | U+2X=3 | 23 |
| Same transformation with supported rejection of invalid input | 1,1,1,0,0,0 | U+V+2X=4 | 18 |
| Atomic `swap(number)` returns the previous private value, with internal synchronization | 1,1,1,0,0,0 | U+2C+2Y=5 | 15 |
| Same swap without established synchronization | 1,1,1,0,0,0 | U+2C=3 | 23 |
| `next()` increments private numeric state under synchronization | 1,1,0,0,0,0 | U+2C+2Y+2X=7 | 10 |
| Fully resolved callable no-op | 1,0,0,0,0,0 | 0 | 100 |
| One text→bytes service handles acquisition/cleanup | 1,2,1,0,0,0 | U+2R=3 | 27 |
| Three acquire/use/release operations, three concepts/arguments, two caller relations | 3,3,3,0,2,0 | three U outcomes=3 | 55 |
| Transformation with one backing-state leak | 1,1,1,0,0,1 | U+2X=3 | 38 |
| Duplicate forwarding overload added to transformation | 2,1,2,0,0,0 | same U+2X=3 | 33 |

These examples define observations, not universal scores for method names. For
example, a public setter may create a different family; include that O/A/H separately.
The swap fixture returns the old scalar value and stores the supplied value: C is
present and X is absent. Incrementing the value also establishes X. No recognizer
may invent or omit a category to force the expected score of a superficially similar
source example.

Each numeric source fixture includes its expected normalized facts as well as score.
Translate core fixtures into Go/Java/Rust/TypeScript. The original roughly 12 scenario
families remain required, including aliases, helpers moved between files, useful
wrappers, shadowed built-ins, unrelated locks, repeated checks, neutral arithmetic,
unknown dependency, parser failure and profile/coverage loss. For unsupported
language-specific synchronization, assert partial rather than exclude the fixture.
Core pure transformation, validation, delegation and lifecycle fixtures must produce
numeric results in every language; all-unknown output fails acceptance.

jname check: Generator/Base must yield one usable type contract, private random
helpers must not enter the external inventory, and CLI entry points must not get
signature-only penalties. The checkout contains Lombok-generated options and
RandomGenerator/LongSupplier dispatch. Without supplied definitions or a supported
receiver set, report those specific gaps; do not promise a numeric jname result
by guessing their behavior. Verify numeric inherited-generator behavior separately
with a source-complete fixture. No target-score expectation for the real repository.

## 7. Profiles, comparisons and caching

Default profile: `responsibility-v4`. Explicit reproduction profile:
`legacy-signature-v3`, retaining the archived ousterhout-v3 formula and its existing
role-reference policy. Add `--score-profile` to both executables and thread the
choice through native options, fix baseline and verification. Legacy results are
labelled signature-based, not measured responsibility. Never select legacy silently
because v4 is unavailable. Invalid profile names are rejected before analysis.

Keep the current SHALLOW catalog weight/threshold/formula for converting its raw
0–100 value into SCORE, but change its file aggregator and displayed metric to
maximum anchored boundary. Do not tune other metrics. Version the definition,
report schema (4), profile identity and affected cache keys. Old numeric deep/SCORE
jobs require either their original legacy execution profile or an explicit new
baseline; old profiles with missing comparison metadata cannot auto-complete.

Fingerprint the canonical catalog/weights/enabled set, boundary policy, recognizer
and registry versions, build selections/context descriptor hashes and supplied
signature identities. Source-body edits invalidate analysis caches but do not by
themselves change the comparison profile. Public operation/boundary inventory is a
separate contract fingerprint. A changed public signature, added/removed operation
or changed applicability requires rebaselining, even if numeric scores improve.

Freeze these fingerprints and required evidence states at job start. Missing files,
coverage loss, changed policies and incomplete baselines never pass, including
SCORE-only focus and `RequireComplete=false` historical contracts. Numeric regression
allowances cannot authorize evidence loss. UI reweighting creates a new effective
profile; the verification policy must exactly match the frozen displayed policy.

Persist summaries and their actual dependency edges using the existing unit graph
and Merkle invalidation; do not walk the whole transitive closure per consumer.
Persist boundary/source indexes so no-change polling and rendering need no scans.
An implementation edit invalidates its unit/summary consumers; an export, superclass,
re-export or built-in registry change also invalidates the appropriate inventories.
Deterministic unsupported facts may be cached with dependency identities. Timeouts,
killed workers, transient I/O and memory failures are not durable successful facts.

Summary analysis is bounded per boundary: 2,000 reachable functions, 50,000 normalized
flow nodes and 100,000 worklist updates. Exceeding a bound gives partial evidence
with the precise limit; unrelated boundaries continue. There is no 10K-file cutoff.
Use at most two concurrent new summary tasks; retain immutable shared summaries
once and spill completed unit artifacts through existing cache storage. These limits
bound new work, not total parser memory; benchmark and report total memory separately.
A changed limit is part of capability/profile provenance.

## 8. Ordered implementation tasks and file ownership

Implement in these batches; the main agent owns design/review and Luna/high agents
own coding. Parallelize only independent adapter work after the normalized contract
and scorer tests exist. Preserve current uncommitted UI/parser fixes.

| Batch | Files / new files | Required change and acceptance |
| --- | --- | --- |
| 1: contract and scorer | `analyzers/structural/internal/facts/model.go`; new `depth_facts.go`, `metrics/depth_v4.go`, `metrics/depth_v4_test.go`; common fixtures under `analyzers/conformance/shallow-v4/` | Add typed boundaries/families/dimensions/facts with schema 3; implement exact rational score and golden cases. Given unknown essential evidence, scorer emits no numeric boundary result. |
| 2: flow and summaries | New `analyzers/structural/internal/depth/` package; adapters' normalized flow additions; versioned built-in registry under `analyzers/conformance/shallow-v4/` | Implement bounded def-use, exit tracking, local-call SCC summaries, deduplication and recognizers. Given repeated/irrelevant machinery, H does not increase; unknowns survive joins. |
| 3a: Go | `analyzers/structural/internal/goadapter/{adapter,syntax}.go`; new `depth_surface.go`, `depth_flow.go`, `depth_test.go` | Implement the Go slice and feed shared facts; existing structural metrics retain their definitions. |
| 3b: Java | `analyzers/structural/adapters/java/src/dev/slopslap/structural/{Facts,JavaParser,JavaClassFacts,JavaMethodBodyFacts,JavaExpressions}.java`; new `JavaDepthFacts.java`; `internal/javaadapter/adapter_test.go` | Emit normalized visibility, inherited API, qualified operations and flow. Generator/Base fixture has one reachable contract and correct inherited responsibilities. |
| 3c: Rust | `analyzers/structural/adapters/rust/src/{model,parser,surface,control,expression}.rs`; new `depth.rs` | Propagate module visibility, local aliases/traits and normalized ownership/control facts; unsupported expansion produces a reason instead of a guess. |
| 3d: TypeScript | `analyzers/typescript/src/{model,context,operations,surface,structural}.ts`; new `depth.ts`, `depth_flow.ts`, `depth_score.ts`; `test/depth.test.ts` | Resolve the local export graph and emit the same facts. A small TS scorer is permitted only with the same versioned policy and exact common-fixture parity. SHALLOW graph building is independent of optional type-safety metrics; enabling/disabling those metrics must not change SHALLOW facts. |
| 4: native/report | `go/internal/report/model.go`; new `assessment.go`; `go/internal/native/{scoring,scoring_files,scoring_components,cache_artifacts,cache_prepare,cache_execution,analyzer_changes}.go`; `component-catalog.json` | Preserve typed evidence and boundary references through fresh and cached analysis; nullable boundary scores, correct max aggregation, N/A versus unknown and safe subtotal/pass behavior. Given warm cache, results match fresh analysis. |
| 5: profile/verification | `go/internal/native/{analyzer,catalog}.go`; `go/internal/analysiscache/{key,schema}.go`; `go/internal/scoring/{catalog,metrics,projection}.go`; `go/internal/fix/scoring.go`; `go/internal/fixanalysis/nativeadapter/{baseline,scoring,verification,verification_checks,verify_service,clone}.go` | Freeze/copy/persist profile, inventory and coverage; enforce exact policy and coverage for all completion paths. Given lower subtotal caused by lost evidence or scope, target is not met. |
| 6: product integration | `go/cmd/slopslap-go/{main,report,fix_runtime}.go`; `go/cmd/slopwatch/main.go`; `go/internal/follow/{table_format,row_view,weights,score_distribution}.go` and detail view; fix contract persistence/clone consumers | Add profile selection, explanations and unavailable sorting; histogram excludes unknown totals. Given saved legacy goal under v4, show an actionable rebaseline message. |
| 7: validation/docs | `docs/depth-{design,evidence-design,reference-rules,delivery-plan}.md`; common fixture runner; cache benchmarks | Supersede conflicting current claims, replay legacy archive, run source fixtures, before/after real repos and 30K smoke benchmark. No default-profile migration is presented as improvement. |

Each batch includes focused tests. Run full structural Go, Java/Rust adapters,
TypeScript tests and application Go suite at integration. Rebuild both executables.
Run quality checks on new/changed code using the explicit legacy profile so changing
SHALLOW does not hide a complexity regression: SCORE <50 and raw NPATH <=100.

## 9. Finite validation and completion

The fixture table, normalized scorer cases and source negative controls are the
acceptance contract. The main agent reviews extraction and expected outcomes; no
external reviewers or new research data are required. Run jname, SlopWatch and ap
before/after, preserve JSON with binary/profile identity and record explanations
for changed jname files plus ten new high-score findings per other repository
(or all if fewer). Partial results are inspected, not dropped from the report.

A generated 30K-file project contains independent packages/modules plus a known
small dependency chain. Assert zero analyzer calls on idle refresh, reuse on warm
refresh, and exactly the changed unit/reverse-dependent closure for a local edit.
Also change a public contract and a supplied dependency summary. Record elapsed
startup/warm/edit times, analyzed units and peak process-tree memory against the
pre-change binary. Fix unexpected whole-workspace reanalysis before release; no
arbitrary wall-clock assertion in unit tests. 80K capacity is follow-up work.

Completion requires a working numeric v4 in the supported slice, not just plumbing,
interface lint, an unimplemented formula or blanket abstention. Broader semantic
inference and statistical calibration are optional future work.

## Design checks performed

The 16 golden cases were evaluated with the exact integer formula. Review corrected
an initial same-type-argument omission by introducing A, and separated identity swap
from increment so X follows the actual data flow. The current source facts lack
resolved def-use/resource semantics; batch 2 deliberately adds them instead of
pretending existing signature flags establish behavior. jname's inherited methods,
Lombok calls and injected clock/random interfaces were inspected directly; their
specified source-coverage limitations are intentional, not promised resolved facts.
